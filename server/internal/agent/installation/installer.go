package installation

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/shrewx/ginx/pkg/statuserror"
)

const metadataLimit int64 = 4 << 20
const downloadLimit int64 = 1 << 30

// Progress reports bounded transfer and installation phases without owning a background task.
type Progress struct {
	// Phase is resolving, downloading, extracting, dependencies, verifying, or publishing.
	Phase string
	// ReceivedBytes is the verified stream's downloaded byte count.
	ReceivedBytes int64
	// TotalBytes is the HTTP length, or zero when the publisher streams an unknown length.
	TotalBytes int64
}

// Result identifies the immutable executable published by a completed installation.
type Result struct {
	// Binary is an absolute executable path, or the trusted Node interpreter for npm packages.
	Binary string
	// PrefixArgs contains managed script paths required before the CLI arguments.
	PrefixArgs []string
	// Version is the publisher release confirmed by the executable's version command.
	Version string
	// SourceURL is the exact official artifact downloaded for this version.
	SourceURL string
}

// Command is a fixed installer or version-probe invocation, never a user-supplied shell command.
type Command struct {
	// Binary is the executable resolved before starting the invocation.
	Binary string
	// Args preserves argument boundaries without passing through a shell.
	Args []string
	// Dir confines the command's working files to private staging.
	Dir string
	// Env is a minimal environment with all writable homes and caches inside staging.
	Env []string
}

// Runner permits real process execution in production and deterministic no-exec tests.
type Runner interface {
	Run(context.Context, Command) (string, error)
}

// Options contains host capabilities and test seams, never publisher URLs from API input.
type Options struct {
	// Client supplies transport configuration while HTTPS and redirect guards remain enforced.
	Client *http.Client
	// Runner executes fixed npm and version commands.
	Runner Runner
	// OS selects the artifact platform; empty uses runtime.GOOS.
	OS string
	// Arch selects the artifact architecture; empty uses runtime.GOARCH.
	Arch string
	// LookPath resolves the Node and npm prerequisites; empty uses exec.LookPath.
	LookPath func(string) (string, error)
	// RemoveProgramDirectory removes an already validated uninstall tombstone; tests inject failures.
	RemoveProgramDirectory func(string) error
}

// Installer publishes complete versions; it does not own account data or runtime task state.
type Installer struct {
	// root contains only managed program versions, separate from account homes.
	root string
	// client reads official manifests and artifacts with guarded redirects.
	client *http.Client
	// runner owns the process boundary for installation and verification.
	runner Runner
	// os is the artifact operating system.
	os string
	// arch is the artifact CPU architecture.
	arch string
	// lookPath resolves trusted host prerequisites.
	lookPath func(string) (string, error)
	// removeProgramDirectory preserves retryable uninstall ownership on filesystem failure.
	removeProgramDirectory func(string) error
	// mu protects the per-runtime installation admission map.
	mu sync.Mutex
	// active prevents duplicate installation within this owner.
	active map[string]bool
}

type installRecord struct {
	// RuntimeID binds the record to its program directory.
	RuntimeID string
	// Version identifies the upstream release.
	Version string
	// SourceURL records the official artifact location.
	SourceURL string
	// Binary is relative to this immutable version's payload directory.
	Binary string
	// Args contains managed paths relative to the same payload directory.
	Args []string
	// Node marks a managed JavaScript entry launched by the host's Node interpreter.
	Node bool
	// Digests verifies the entry point and interpreter arguments when reading the active install.
	Digests map[string]string
	// PublishedAt orders fully published versions without overwriting a live executable or pointer.
	PublishedAt int64
}

// New creates an installer using the system HTTP transport and fixed process runner.
func New(root string) *Installer { return NewWithOptions(root, Options{}) }

// NewWithOptions creates an installer with injected host boundaries for deterministic tests.
func NewWithOptions(root string, options Options) *Installer {
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Minute}
	}
	guarded := *client
	guarded.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 8 || !officialURL(request.URL) {
			return status_error.RuntimeInstallationFailed
		}
		return nil
	}
	if options.Runner == nil {
		options.Runner = commandRunner{}
	}
	if options.OS == "" {
		options.OS = runtime.GOOS
	}
	if options.Arch == "" {
		options.Arch = runtime.GOARCH
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.RemoveProgramDirectory == nil {
		options.RemoveProgramDirectory = os.RemoveAll
	}
	absolute, err := filepath.Abs(root)
	if err != nil || strings.TrimSpace(root) == "" {
		absolute = ""
	}
	return &Installer{root: absolute, client: &guarded, runner: options.Runner, os: options.OS, arch: options.Arch, lookPath: options.LookPath, removeProgramDirectory: options.RemoveProgramDirectory, active: make(map[string]bool)}
}

// Install downloads, verifies and atomically publishes a complete version of one built-in CLI.
// All account homes remain untouched; cancellation removes only this operation's staging files.
func (i *Installer) Install(ctx context.Context, runtimeID string, notify func(Progress)) (Result, error) {
	return i.InstallAt(ctx, runtimeID, "", nil, notify)
}

func (i *Installer) installManaged(ctx context.Context, runtimeID string, notify func(Progress), forceFresh bool) (Result, error) {
	return i.prepareProgram(ctx, runtimeID, notify, forceFresh, nil)
}

func (i *Installer) prepareProgram(ctx context.Context, runtimeID string, notify func(Progress), forceFresh bool, publish func(string, installRecord) (Result, error)) (Result, error) {
	if !i.Supported(runtimeID) {
		return Result{}, status_error.RuntimeInstallationUnsupported
	}
	if i.root == "" {
		return Result{}, failure("installation root is not configured", nil)
	}
	if ctx.Err() != nil {
		return Result{}, canceled(ctx)
	}
	if notify == nil {
		notify = func(Progress) {}
	}
	notify(Progress{Phase: "resolving"})
	artifact, err := i.resolve(ctx, runtimeID)
	if err != nil {
		return Result{}, err
	}
	if !forceFresh && publish == nil {
		if current, err := i.Installed(runtimeID); err == nil {
			if order, known := CompareVersions(runtimeID, current.Version, artifact.version); known && order >= 0 {
				return current, nil
			}
		}
	}
	parent := filepath.Join(i.root, runtimeID)
	for _, directory := range []string{i.root, parent} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return Result{}, failure("create program store", err)
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Result{}, failure("program store directory", err)
		}
	}
	staging, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return Result{}, failure("create installation staging", err)
	}
	defer func() {
		if err := os.RemoveAll(staging); err != nil {
			logx.Errorf("clean CLI installation staging: runtime=%s error=%v", runtimeID, err)
		}
	}()
	payload := filepath.Join(staging, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		return Result{}, failure("create artifact payload", err)
	}
	for _, name := range []string{"home", "cache", "tmp"} {
		if err := os.Mkdir(filepath.Join(staging, name), 0o700); err != nil {
			return Result{}, failure("create private probe environment", err)
		}
	}
	download := filepath.Join(staging, "artifact.tgz")
	if err := i.download(ctx, &artifact, download, notify); err != nil {
		return Result{}, err
	}
	notify(Progress{Phase: "extracting"})
	if artifact.npmPackage == "" {
		if err := extract(ctx, download, payload, artifact.format, artifact.binary); err != nil {
			return Result{}, err
		}
	} else {
		notify(Progress{Phase: "dependencies"})
		if err := i.installNPM(ctx, artifact, download, payload, staging); err != nil {
			return Result{}, err
		}
		artifact.binary = filepath.ToSlash(filepath.Join("node_modules", artifact.npmPackage, artifact.binary))
	}
	record := installRecord{RuntimeID: runtimeID, Version: artifact.version, SourceURL: artifact.url, Binary: artifact.binary, Args: artifact.args, Node: artifact.npmPackage != "", Digests: make(map[string]string), PublishedAt: time.Now().UnixNano()}
	for _, relative := range append([]string{record.Binary}, record.Args...) {
		file, err := confinedFile(payload, relative)
		if err != nil {
			return Result{}, err
		}
		if !record.Node && relative == record.Binary {
			if err := os.Chmod(file, 0o755); err != nil {
				return Result{}, failure("make CLI executable", err)
			}
		}
		digest, err := digestFile(file)
		if err != nil {
			return Result{}, err
		}
		record.Digests[relative] = digest
	}
	notify(Progress{Phase: "verifying"})
	result, err := i.result(payload, record)
	if err != nil {
		return Result{}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	output, probeErr := i.runner.Run(probeCtx, Command{Binary: result.Binary, Args: append(append([]string(nil), result.PrefixArgs...), "--version"), Dir: staging, Env: privateEnvironment(staging)})
	probeTimeout := errors.Is(probeCtx.Err(), context.DeadlineExceeded)
	cancel()
	if ctx.Err() != nil {
		return Result{}, canceled(ctx)
	}
	if probeTimeout {
		return Result{}, status_error.OperationTimeout
	}
	if probeErr != nil {
		return Result{}, failure("verify installed CLI version", probeErr)
	}
	// Ordering ignores SemVer build metadata; publication must verify the exact
	// release token instead, allowing only the native v/rust-v display prefixes.
	observedVersion := strings.TrimPrefix(strings.TrimPrefix(versionToken(output), "rust-"), "v")
	expectedVersion := strings.TrimPrefix(strings.TrimPrefix(versionToken(record.Version), "rust-"), "v")
	if observedVersion == "" || observedVersion != expectedVersion {
		return Result{}, failure("installed CLI version mismatch", nil)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return Result{}, failure("encode installation record", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "installation.json"), data, 0o600); err != nil {
		return Result{}, failure("write installation record", err)
	}
	// Remove transport data and private probe homes before making the version visible.
	if err := os.Remove(download); err != nil {
		return Result{}, failure("remove artifact staging", err)
	}
	for _, name := range []string{"home", "cache", "tmp", ".npmrc", "global.npmrc"} {
		if err := os.RemoveAll(filepath.Join(staging, name)); err != nil {
			return Result{}, failure("remove installation scratch", err)
		}
	}
	if ctx.Err() != nil {
		return Result{}, canceled(ctx)
	}
	notify(Progress{Phase: "publishing"})
	if publish != nil {
		return publish(staging, record)
	}
	final := filepath.Join(parent, "version-"+strconv.FormatInt(record.PublishedAt, 10)+"-"+filepath.Base(staging)[len(".staging-"):])
	if err := os.Rename(staging, final); err != nil {
		return Result{}, failure("publish installed CLI", err)
	}
	return i.result(filepath.Join(final, "payload"), record)
}

// Installed reads the newest complete version and verifies its entry point without running a CLI.
// No version is represented by os.ErrNotExist; damaged records return an installation failure.
func (i *Installer) Installed(runtimeID string) (Result, error) {
	if !i.Supported(runtimeID) {
		return Result{}, status_error.RuntimeInstallationUnsupported
	}
	if err := i.validateStore(runtimeID); err != nil {
		return Result{}, err
	}
	parent := filepath.Join(i.root, runtimeID)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, os.ErrNotExist
	}
	if err != nil {
		return Result{}, failure("read program store", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "version-") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return Result{}, os.ErrNotExist
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	root := filepath.Join(parent, names[0])
	data, err := os.ReadFile(filepath.Join(root, "installation.json"))
	if err != nil {
		return Result{}, failure("read installation record", err)
	}
	var record installRecord
	if len(data) > int(metadataLimit) || json.Unmarshal(data, &record) != nil || record.RuntimeID != runtimeID || !versionPattern.MatchString(record.Version) {
		return Result{}, failure("installation record identity", nil)
	}
	return i.result(filepath.Join(root, "payload"), record)
}

func (i *Installer) result(payload string, record installRecord) (Result, error) {
	result := Result{Version: record.Version, SourceURL: record.SourceURL}
	for _, relative := range append([]string{record.Binary}, record.Args...) {
		file, err := confinedFile(payload, relative)
		if err != nil {
			return Result{}, err
		}
		digest, err := digestFile(file)
		if err != nil {
			return Result{}, err
		}
		if digest != record.Digests[relative] {
			return Result{}, failure("installed executable checksum", nil)
		}
		if relative == record.Binary && !record.Node && i.os != "windows" {
			info, err := os.Stat(file)
			if err != nil || info.Mode().Perm()&0o111 == 0 {
				return Result{}, failure("installed binary is not executable", err)
			}
		}
		if relative == record.Binary {
			result.Binary = file
		} else {
			result.PrefixArgs = append(result.PrefixArgs, file)
		}
	}
	if record.Node {
		node, err := i.lookPath("node")
		if err != nil {
			return Result{}, failure("Node.js prerequisite missing", err)
		}
		result.PrefixArgs = append([]string{result.Binary}, result.PrefixArgs...)
		result.Binary = node
	}
	return result, nil
}

func (i *Installer) read(ctx context.Context, location string, limit int64) ([]byte, error) {
	response, err := i.request(ctx, location)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if ctx.Err() != nil {
		return nil, canceled(ctx)
	}
	if err != nil || int64(len(data)) > limit {
		return nil, failure("read publisher metadata", err)
	}
	return data, nil
}

func (i *Installer) request(ctx context.Context, location string) (*http.Response, error) {
	parsed, err := url.Parse(location)
	if err != nil || !officialURL(parsed) {
		return nil, failure("publisher URL rejected", err)
	}
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
		if err != nil {
			return nil, failure("create artifact request", err)
		}
		request.Header.Set("User-Agent", "NeuxBot-runtime-installer")
		if parsed.Hostname() == "api.github.com" && strings.Contains(parsed.Path, "/releases/assets/") {
			request.Header.Set("Accept", "application/octet-stream")
		}
		response, err := i.client.Do(request)
		if ctx.Err() != nil {
			if response != nil {
				response.Body.Close()
			}
			return nil, canceled(ctx)
		}
		if err == nil && response.StatusCode == http.StatusOK {
			return response, nil
		}
		var retry bool
		var retryAfter string
		if response != nil {
			retryAfter = response.Header.Get("Retry-After")
			retry = retryableHTTPStatus(response.StatusCode)
			response.Body.Close()
		} else if err != nil {
			var temporary net.Error
			retry = errors.As(err, &temporary) && (temporary.Timeout() || temporary.Temporary()) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
		}
		if attempt >= 2 || !retry {
			if err != nil {
				return nil, failure("download official CLI artifact", err)
			}
			return nil, failure("publisher HTTP status "+strconv.Itoa(response.StatusCode), nil)
		}
		delay := time.Duration(1<<attempt) * 250 * time.Millisecond
		if seconds, parseErr := strconv.ParseInt(retryAfter, 10, 32); parseErr == nil && seconds > 0 {
			delay = max(delay, time.Duration(seconds)*time.Second)
		} else if when, parseErr := http.ParseTime(retryAfter); parseErr == nil {
			delay = max(delay, time.Until(when))
		}
		// A long publisher cooldown is reported instead of sleeping indefinitely.
		if delay > 30*time.Second {
			return nil, failure("publisher HTTP status "+strconv.Itoa(response.StatusCode), nil)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, canceled(ctx)
		case <-timer.C:
		}
	}
}

func retryableHTTPStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

func (i *Installer) download(ctx context.Context, artifact *artifact, destination string, notify func(Progress)) error {
	notify(Progress{Phase: "downloading"})
	response, err := i.request(ctx, artifact.url)
	if err != nil && artifact.fallbackURL != "" {
		_, reason, status := Diagnosis(err)
		if reason == "network" || reason == "http_status" && retryableHTTPStatus(status) {
			response, err = i.request(ctx, artifact.fallbackURL)
			if err == nil {
				artifact.url = artifact.fallbackURL
			}
		}
	}
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.ContentLength > downloadLimit {
		return failure("artifact download limit", nil)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return failure("create artifact download", err)
	}
	defer file.Close()
	var digest hash.Hash = sha256.New()
	if artifact.algorithm == "sha512" {
		digest = sha512.New()
	}
	writer := io.MultiWriter(file, digest)
	var received int64
	buffer := make([]byte, 128<<10)
	last := time.Time{}
	for {
		if ctx.Err() != nil {
			return canceled(ctx)
		}
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			received += int64(n)
			if received > downloadLimit {
				return failure("artifact download limit", nil)
			}
			if _, err := writer.Write(buffer[:n]); err != nil {
				return failure("write artifact download", err)
			}
			if time.Since(last) >= 100*time.Millisecond {
				notify(Progress{Phase: "downloading", ReceivedBytes: received, TotalBytes: max(response.ContentLength, 0)})
				last = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return canceled(ctx)
			}
			return failure("read artifact stream", readErr)
		}
	}
	if received == 0 || (response.ContentLength >= 0 && received != response.ContentLength) {
		return failure("artifact download length", nil)
	}
	if artifact.digest != "" && hex.EncodeToString(digest.Sum(nil)) != artifact.digest {
		return failure("publisher artifact checksum", nil)
	}
	if err := file.Sync(); err != nil {
		return failure("sync artifact download", err)
	}
	notify(Progress{Phase: "downloading", ReceivedBytes: received, TotalBytes: received})
	return nil
}

func officialURL(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Port() != "" || value.Fragment != "" {
		return false
	}
	switch value.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "registry.npmjs.org", "cursor.com", "downloads.cursor.com", "x.ai", "code.kimi.com", "cdn.kimi.com", "qoder-ide.oss-accelerate.aliyuncs.com", "static.qoder.com.cn", "acc-1258344699.cos.accelerate.myqcloud.com":
		return true
	}
	return false
}

func digestFile(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", failure("read installed executable", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", failure("hash installed executable", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func failure(operation string, err error) error {
	status, _ := strconv.Atoi(strings.TrimPrefix(operation, "publisher HTTP status "))
	reason := failureReason(operation, err)
	// Transport errors can contain signed redirect URLs or proxy credentials.
	logx.Errorf("CLI installation: operation=%s reason=%s http_status=%d error_type=%T", operation, reason, status, err)
	return status_error.RuntimeInstallationFailed.WithParams(map[string]interface{}{"operation": operation, "reason": reason, "status": status})
}

// Diagnosis exposes only controlled technical labels; raw commands, URLs and credentials stay private.
func Diagnosis(err error) (operation, reason string, status int) {
	var detail *statuserror.StatusErr
	if !errors.As(err, &detail) || detail.Key() != status_error.RuntimeInstallationFailed.Key() {
		return "", "", 0
	}
	operation, _ = detail.Params["operation"].(string)
	reason, _ = detail.Params["reason"].(string)
	status, _ = detail.Params["status"].(int)
	return operation, reason, status
}

func failureReason(operation string, err error) string {
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	if errors.As(err, &certificate) || errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalidCertificate) {
		return "tls"
	}
	var request *url.Error
	var operationError *net.OpError
	var nameError *net.DNSError
	if errors.As(err, &request) || errors.As(err, &operationError) || errors.As(err, &nameError) {
		if strings.Contains(strings.ToLower(err.Error()), "proxyconnect") {
			return "proxy"
		}
		return "network"
	}
	if strings.HasPrefix(operation, "publisher HTTP status ") {
		return "http_status"
	}
	switch {
	case strings.Contains(operation, "checksum") || strings.Contains(operation, "digest"):
		return "checksum"
	case strings.Contains(operation, "archive") || strings.Contains(operation, "gzip") || strings.Contains(operation, "tar ") || strings.Contains(operation, "zip "):
		return "archive"
	case strings.Contains(operation, "prerequisite") || strings.Contains(operation, "Node.js") || strings.Contains(operation, "npm Node entry"):
		return "prerequisite"
	case strings.Contains(operation, "npm package dependencies"):
		return "dependencies"
	case strings.Contains(operation, "installed CLI version"):
		return "version"
	case strings.Contains(operation, "manifest") || strings.Contains(operation, "release") || strings.Contains(operation, "metadata") || strings.Contains(operation, "npm artifact"):
		return "manifest"
	case strings.Contains(operation, "URL"):
		return "source"
	default:
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			return "filesystem"
		}
		return "unknown"
	}
}
func canceled(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return status_error.OperationTimeout
	}
	return status_error.OperationCanceled
}
