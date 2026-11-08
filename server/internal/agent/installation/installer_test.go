package installation

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

type fakeRunner struct {
	// run implements a no-exec process boundary supplied by the test.
	run func(context.Context, Command) (string, error)
}

func (r fakeRunner) Run(ctx context.Context, command Command) (string, error) {
	return r.run(ctx, command)
}

type sourceTransport struct {
	// base serves fake official responses from the local TLS server.
	base http.RoundTripper
	// target is the test server's address, never used by production source selection.
	target *url.URL
}

func (t sourceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	location := *request.URL
	location.Scheme, location.Host = t.target.Scheme, t.target.Host
	clone.URL = &location
	clone.Header.Set("X-Official-URL", request.URL.String())
	return t.base.RoundTrip(clone)
}

type fakePublisher struct {
	// mu protects route updates and request observations used by concurrent HTTP handlers.
	mu sync.Mutex
	// routes maps official URLs to deterministic response bodies.
	routes map[string][]byte
	// handler optionally streams or redirects a request for boundary tests.
	handler func(http.ResponseWriter, *http.Request) bool
	// client redirects official requests into an isolated local TLS server.
	client *http.Client
}

func publisher(t *testing.T) *fakePublisher {
	t.Helper()
	publisher := &fakePublisher{routes: make(map[string][]byte)}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		publisher.mu.Lock()
		handler := publisher.handler
		publisher.mu.Unlock()
		if handler != nil && handler(w, request) {
			return
		}
		publisher.mu.Lock()
		data, found := publisher.routes[request.Header.Get("X-Official-URL")]
		publisher.mu.Unlock()
		if !found {
			http.NotFound(w, request)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	location, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	publisher.client = &http.Client{Transport: sourceTransport{target: location, base: server.Client().Transport}}
	return publisher
}

func (p *fakePublisher) put(location string, value any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if data, ok := value.([]byte); ok {
		p.routes[location] = data
		return
	}
	p.routes[location], _ = json.Marshal(value)
}

func tarArtifact(t *testing.T, files map[string]string, links map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	gzipWriter := gzip.NewWriter(&data)
	writer := tar.NewWriter(gzipWriter)
	for name, contents := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(contents))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		if err := writer.WriteHeader(&tar.Header{Name: name, Linkname: target, Typeflag: tar.TypeSymlink, Mode: 0o777}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func codexRelease(p *fakePublisher, version string, archive []byte) {
	name := "codex-package-aarch64-apple-darwin.tar.gz"
	digest := sha256.Sum256(archive)
	p.put("https://api.github.com/repos/openai/codex/releases/latest", map[string]any{"tag_name": "rust-v" + version, "assets": []any{map[string]any{"name": name, "digest": "sha256:" + hex.EncodeToString(digest[:])}}})
	p.put("https://github.com/openai/codex/releases/download/rust-v"+version+"/"+name, archive)
}

func TestInstallPublishesCompleteVersionsAndPreservesPreviousOnFailure(t *testing.T) {
	p := publisher(t)
	version := "1.2.3"
	root := t.TempDir()
	i := NewWithOptions(root, Options{OS: "darwin", Arch: "arm64", Client: p.client, Runner: fakeRunner{run: func(_ context.Context, command Command) (string, error) {
		if !strings.HasPrefix(command.Binary, root+string(filepath.Separator)) || !reflect.DeepEqual(command.Args, []string{"--version"}) {
			t.Errorf("unexpected command: %#v", command)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(command.Binary)), "codex-path", "rg")); err != nil {
			t.Errorf("companion file missing: %v", err)
		}
		for _, value := range command.Env {
			if strings.HasPrefix(value, "HOME=") && !strings.HasPrefix(value, "HOME="+root) {
				t.Errorf("probe used real HOME: %s", value)
			}
		}
		return "codex-cli " + version, nil
	}}})
	archive := tarArtifact(t, map[string]string{"bin/codex": "first executable", "codex-path/rg": "companion"}, nil)
	codexRelease(p, version, archive)
	var phases []string
	first, err := i.Install(context.Background(), "codex", func(progress Progress) { phases = append(phases, progress.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != version || !strings.Contains(strings.Join(phases, ","), "downloading") {
		t.Fatalf("missing release or progress: %#v %v", first, phases)
	}
	installed, err := i.Installed("codex")
	if err != nil || installed.Binary != first.Binary {
		t.Fatalf("published result differs: %#v %v", installed, err)
	}
	// A publisher's newer version that executes the old binary is rejected before publication.
	codexRelease(p, "1.2.4", archive)
	if _, err := i.Install(context.Background(), "codex", nil); !hasInstallationFailure(err) {
		t.Fatalf("version mismatch accepted: %v", err)
	}
	installed, err = i.Installed("codex")
	if err != nil || installed.Version != "1.2.3" {
		t.Fatalf("failed update replaced prior version: %#v %v", installed, err)
	}
	version = "1.2.4"
	second, err := i.Install(context.Background(), "codex", nil)
	if err != nil || second.Binary == first.Binary {
		t.Fatalf("new version was not immutable: %#v %v", second, err)
	}
	if _, err := os.Stat(first.Binary); err != nil {
		t.Fatalf("active old executable removed: %v", err)
	}
	assertNoStaging(t, root, "codex")
}

func TestChecksumFailureDoesNotExecuteOrPublish(t *testing.T) {
	p := publisher(t)
	archive := tarArtifact(t, map[string]string{"bin/codex": "binary"}, nil)
	codexRelease(p, "1.2.3", archive)
	p.put("https://github.com/openai/codex/releases/download/rust-v1.2.3/codex-package-aarch64-apple-darwin.tar.gz", []byte("tampered"))
	i := NewWithOptions(t.TempDir(), Options{OS: "darwin", Arch: "arm64", Client: p.client, Runner: fakeRunner{run: func(context.Context, Command) (string, error) {
		t.Fatal("unverified executable was started")
		return "", nil
	}}})
	if _, err := i.Install(context.Background(), "codex", nil); !hasInstallationFailure(err) {
		t.Fatalf("checksum mismatch accepted: %v", err)
	}
	if _, err := i.Installed("codex"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed install became visible: %v", err)
	}
	assertNoStaging(t, i.root, "codex")
}

func TestCanceledDownloadCleansStagingAndRetainsOldVersion(t *testing.T) {
	p := publisher(t)
	archive := tarArtifact(t, map[string]string{"bin/codex": "binary"}, nil)
	codexRelease(p, "1.2.3", archive)
	i := NewWithOptions(t.TempDir(), Options{OS: "darwin", Arch: "arm64", Client: p.client, Runner: fakeRunner{run: func(context.Context, Command) (string, error) { return "1.2.3", nil }}})
	first, err := i.Install(context.Background(), "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	codexRelease(p, "1.2.4", archive)
	p.mu.Lock()
	p.handler = func(w http.ResponseWriter, request *http.Request) bool {
		if !strings.Contains(request.Header.Get("X-Official-URL"), "/releases/download/") {
			return false
		}
		w.Header().Set("Content-Length", "1000000")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1000))
		w.(http.Flusher).Flush()
		<-request.Context().Done()
		return true
	}
	p.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = i.Install(ctx, "codex", func(progress Progress) {
		if progress.ReceivedBytes > 0 {
			cancel()
		}
	})
	if err != status_error.OperationCanceled {
		t.Fatalf("cancellation did not propagate: %v", err)
	}
	current, err := i.Installed("codex")
	if err != nil || current.Binary != first.Binary {
		t.Fatalf("cancellation lost prior version: %#v %v", current, err)
	}
	assertNoStaging(t, i.root, "codex")
}

func TestArchiveInternalLinksAndTraversal(t *testing.T) {
	for _, target := range []string{"library", "../outside", "/tmp/outside", "C:/outside"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			archive := tarArtifact(t, map[string]string{"kimi/_internal/library": "runtime"}, map[string]string{"kimi/_internal/link": target})
			file := filepath.Join(t.TempDir(), "archive.tgz")
			if err := os.WriteFile(file, archive, 0o600); err != nil {
				t.Fatal(err)
			}
			err := extract(context.Background(), file, root, "tar.gz", "")
			// ../outside still lies inside the payload and is rejected because no such target exists.
			if target == "library" {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(root, "kimi", "_internal", "link"))
				if err != nil || string(data) != "runtime" {
					t.Fatalf("internal dependency link broken: %s %v", data, err)
				}
			} else if err == nil {
				t.Fatal("unsafe/dangling link accepted")
			}
		})
	}
	root := t.TempDir()
	file := filepath.Join(t.TempDir(), "archive.tgz")
	if err := os.WriteFile(file, tarArtifact(t, map[string]string{"../escape": "payload"}, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extract(context.Background(), file, root, "tar.gz", ""); err == nil {
		t.Fatal("parent traversal accepted")
	}
	var zipData bytes.Buffer
	zipWriter := zip.NewWriter(&zipData)
	entry, err := zipWriter.Create("C:/escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("payload"))
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, zipData.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extract(context.Background(), file, root, "zip", ""); err == nil {
		t.Fatal("Windows drive traversal accepted")
	}
}

func TestNPMUsesPrivatePrefixAndFakeRunnerOnly(t *testing.T) {
	p := publisher(t)
	data := []byte("verified official package fixture")
	digest := sha512.Sum512(data)
	p.put("https://registry.npmjs.org/@google/gemini-cli/latest", map[string]any{"name": "@google/gemini-cli", "version": "1.2.3", "bin": map[string]any{"gemini": "bundle/gemini.js"}, "dist": map[string]any{"tarball": "https://registry.npmjs.org/@google/gemini-cli/-/gemini-cli-1.2.3.tgz", "integrity": "sha512-" + base64.StdEncoding.EncodeToString(digest[:])}})
	p.put("https://registry.npmjs.org/@google/gemini-cli/-/gemini-cli-1.2.3.tgz", data)
	root := t.TempDir()
	var commands []Command
	i := NewWithOptions(root, Options{OS: "darwin", Arch: "arm64", Client: p.client, LookPath: func(name string) (string, error) { return "/trusted/" + name, nil }, Runner: fakeRunner{run: func(_ context.Context, command Command) (string, error) {
		commands = append(commands, command)
		if command.Binary == "/trusted/node" && reflect.DeepEqual(command.Args, []string{"--version"}) {
			return "v22.0.0", nil
		}
		if command.Binary == "/trusted/npm" {
			joined := strings.Join(command.Args, " ")
			if strings.Contains(joined, " -g ") || !strings.Contains(joined, "--ignore-scripts") || !strings.Contains(joined, "--omit=optional") {
				t.Fatalf("unsafe npm command: %v", command.Args)
			}
			prefix := ""
			for index, arg := range command.Args {
				if arg == "--prefix" {
					prefix = command.Args[index+1]
				}
			}
			if !strings.HasPrefix(prefix, root+string(filepath.Separator)) || !strings.HasPrefix(command.Dir, root+string(filepath.Separator)) {
				t.Fatal("npm escaped managed staging")
			}
			entry := filepath.Join(prefix, "node_modules", "@google", "gemini-cli", "bundle", "gemini.js")
			if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(entry, []byte("fake bundled entry"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "installed", nil
		}
		if command.Binary != "/trusted/node" || len(command.Args) != 2 || command.Args[1] != "--version" {
			t.Fatalf("unexpected CLI probe: %#v", command)
		}
		return "1.2.3", nil
	}}})
	result, err := i.Install(context.Background(), "gemini", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 || result.Binary != "/trusted/node" || len(result.PrefixArgs) != 1 || !strings.HasPrefix(result.PrefixArgs[0], root+string(filepath.Separator)) {
		t.Fatalf("bad managed Node launch: %#v", result)
	}
	for _, command := range commands {
		for _, environment := range command.Env {
			if strings.HasPrefix(environment, "HOME=") && !strings.HasPrefix(environment, "HOME="+root) {
				t.Fatalf("npm saw system home: %s", environment)
			}
		}
	}
	assertNoStaging(t, root, "gemini")
}

func TestRejectsForeignURLsAndUnsupportedTargets(t *testing.T) {
	for _, location := range []string{"http://registry.npmjs.org/package", "https://registry.npmjs.org.evil.test/package", "https://user:secret@github.com/release", "https://github.com:443/release", "https://127.0.0.1/release"} {
		value, _ := url.Parse(location)
		if officialURL(value) {
			t.Errorf("unsafe publisher accepted: %s", location)
		}
	}
	i := NewWithOptions(t.TempDir(), Options{OS: "windows", Arch: "arm64"})
	if i.Supported("codex") {
		t.Fatal("unsupported platform advertised")
	}
	if _, err := i.Install(context.Background(), "../../arbitrary", nil); err != status_error.RuntimeInstallationUnsupported {
		t.Fatalf("unknown CLI accepted: %v", err)
	}
}

func TestBusyAndCanceledProbeDoNotPublish(t *testing.T) {
	p := publisher(t)
	codexRelease(p, "1.2.3", tarArtifact(t, map[string]string{"bin/codex": "binary"}, nil))
	entered := make(chan struct{})
	i := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: "darwin", Arch: "arm64", Runner: fakeRunner{run: func(ctx context.Context, _ Command) (string, error) {
		close(entered)
		<-ctx.Done()
		return "", ctx.Err()
	}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() { _, err := i.Install(ctx, "codex", nil); completed <- err }()
	<-entered
	if _, err := i.Install(context.Background(), "codex", nil); err != status_error.RuntimeInstallationBusy {
		t.Fatalf("duplicate install admitted: %v", err)
	}
	cancel()
	if err := <-completed; err != status_error.OperationCanceled {
		t.Fatalf("probe cancellation lost: %v", err)
	}
	if _, err := i.Installed("codex"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled probe became visible: %v", err)
	}
	assertNoStaging(t, i.root, "codex")
}

func TestForeignRedirectAndTamperedInstalledEntryAreRejected(t *testing.T) {
	p := publisher(t)
	p.handler = func(w http.ResponseWriter, request *http.Request) bool {
		http.Redirect(w, request, "https://evil.test/artifact", http.StatusFound)
		return true
	}
	i := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: "darwin", Arch: "arm64"})
	if _, err := i.read(context.Background(), "https://x.ai/cli/stable", metadataLimit); !hasInstallationFailure(err) {
		t.Fatalf("foreign redirect allowed: %v", err)
	}
	p.mu.Lock()
	p.handler = nil
	p.mu.Unlock()
	codexRelease(p, "1.2.3", tarArtifact(t, map[string]string{"bin/codex": "binary"}, nil))
	i.runner = fakeRunner{run: func(context.Context, Command) (string, error) { return "1.2.3", nil }}
	result, err := i.Install(context.Background(), "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.Binary, []byte("replaced"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Installed("codex"); !hasInstallationFailure(err) {
		t.Fatalf("corrupt executable trusted: %v", err)
	}
}

func TestPrefixedVersionReceiptAndCurrentVersionAvoidDownload(t *testing.T) {
	p := publisher(t)
	archive := tarArtifact(t, map[string]string{"bin/codex": "binary"}, nil)
	codexRelease(p, "2.2.6", archive)
	probes := 0
	i := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: "darwin", Arch: "arm64", Runner: fakeRunner{run: func(context.Context, Command) (string, error) {
		probes++
		return "opencode v2.2.6", nil
	}}})
	first, err := i.Install(t.Context(), "codex", nil)
	if err != nil {
		t.Fatalf("real native v-prefix receipt was rejected: %v", err)
	}
	var downloads atomic.Int32
	p.handler = func(w http.ResponseWriter, request *http.Request) bool {
		if strings.Contains(request.Header.Get("X-Official-URL"), "/releases/download/") {
			downloads.Add(1)
		}
		return false
	}
	for _, upstream := range []string{"2.2.6", "2.2.5"} {
		codexRelease(p, upstream, archive)
		result, err := i.Install(t.Context(), "codex", nil)
		if err != nil || result.Binary != first.Binary || result.Version != "2.2.6" {
			t.Fatalf("current/newer installed version was replaced: %#v error=%v", result, err)
		}
	}
	if probes != 1 || downloads.Load() != 0 {
		t.Fatalf("current/newer version downloaded or executed again: probes=%d downloads=%d", probes, downloads.Load())
	}
	codexRelease(p, "2.2.6", archive)
	other := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: "darwin", Arch: "arm64", Runner: fakeRunner{run: func(context.Context, Command) (string, error) {
		return "opencode v2.2.6+unexpected-build", nil
	}}})
	if _, err := other.Install(t.Context(), "codex", nil); !hasInstallationFailure(err) {
		t.Fatalf("different build metadata was accepted as the requested release: %v", err)
	}
}

func TestKimiCodeUsesCurrentOfficialManifestAndVerifiedNativeBinary(t *testing.T) {
	for _, target := range [][3]string{{"darwin", "amd64", "darwin-x64"}, {"darwin", "arm64", "darwin-arm64"}, {"linux", "amd64", "linux-x64"}, {"windows", "amd64", "win32-x64"}} {
		t.Run(target[2], func(t *testing.T) {
			p := publisher(t)
			binary := []byte("verified Kimi Code native fixture")
			digest := sha256.Sum256(binary)
			ext := ""
			if target[0] == "windows" {
				ext = ".exe"
			}
			filename := "kimi-code-" + target[2] + ext
			base := "https://code.kimi.com/kimi-code"
			p.put(base+"/latest", []byte("2.5.2\n"))
			p.put(base+"/binaries/2.5.2/manifest.json", map[string]any{"version": "2.5.2", "platforms": map[string]any{
				target[2]: map[string]any{"filename": filename, "checksum": hex.EncodeToString(digest[:])},
			}})
			p.put(base+"/binaries/2.5.2/"+filename, binary)
			i := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: target[0], Arch: target[1], Runner: fakeRunner{run: func(_ context.Context, command Command) (string, error) {
				if filepath.Base(command.Binary) != "kimi"+ext || !reflect.DeepEqual(command.Args, []string{"--version"}) {
					t.Errorf("Kimi native artifact required an unexpected wrapper: %#v", command.Args)
				}
				return "kimi, version 2.5.2", nil
			}}})
			result, err := i.Install(t.Context(), "kimi", nil)
			if err != nil || result.Version != "2.5.2" || result.SourceURL != base+"/binaries/2.5.2/"+filename {
				t.Fatalf("current Kimi Code release was not installed: result=%+v error=%v", result, err)
			}
		})
	}
}

func TestPublisherHTTPRetryIsBoundedAndRespectsCooldown(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504, 403} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			p := publisher(t)
			var calls atomic.Int32
			p.handler = func(w http.ResponseWriter, _ *http.Request) bool {
				count := calls.Add(1)
				if count == 1 || code == 504 || code == 403 {
					if code == 429 {
						w.Header().Set("Retry-After", "1")
					}
					w.WriteHeader(code)
				} else {
					_, _ = w.Write([]byte("1.2.3"))
				}
				return true
			}
			i := NewWithOptions(t.TempDir(), Options{Client: p.client})
			started := time.Now()
			_, err := i.read(t.Context(), "https://x.ai/cli/stable", metadataLimit)
			if code == 403 || code == 504 {
				_, reason, status := Diagnosis(err)
				if reason != "http_status" || status != code || calls.Load() != map[int]int32{403: 1, 504: 3}[code] {
					t.Fatalf("terminal HTTP failure lost bounded diagnostic: reason=%s status=%d calls=%d", reason, status, calls.Load())
				}
			} else if err != nil || calls.Load() != 2 || code == 429 && time.Since(started) < time.Second {
				t.Fatalf("transient HTTP recovery ignored retries/cooldown: calls=%d elapsed=%v error=%v", calls.Load(), time.Since(started), err)
			}
		})
	}
}

func TestGitHubAssetAPIFallbackPreservesDigestAndNeverMasksChecksumFailure(t *testing.T) {
	for _, checksumFailure := range []bool{false, true} {
		t.Run(strconv.FormatBool(checksumFailure), func(t *testing.T) {
			p := publisher(t)
			archive := tarArtifact(t, map[string]string{"bin/codex": "binary"}, nil)
			digest := sha256.Sum256(archive)
			api := "https://api.github.com/repos/openai/codex/releases/assets/123"
			primary := "https://github.com/openai/codex/releases/download/rust-v1.2.3/codex-package-aarch64-apple-darwin.tar.gz"
			p.put("https://api.github.com/repos/openai/codex/releases/latest", map[string]any{
				"tag_name": "rust-v1.2.3", "assets": []any{map[string]any{
					"name": "codex-package-aarch64-apple-darwin.tar.gz", "digest": "sha256:" + hex.EncodeToString(digest[:]), "url": api,
				}},
			})
			p.put(primary, []byte("bad checksum"))
			p.put(api, archive)
			var fallbackCalls atomic.Int32
			p.handler = func(w http.ResponseWriter, request *http.Request) bool {
				source := request.Header.Get("X-Official-URL")
				if source == primary && !checksumFailure {
					w.WriteHeader(504)
					return true
				}
				if source == api {
					fallbackCalls.Add(1)
					if request.Header.Get("Accept") != "application/octet-stream" {
						t.Error("GitHub asset API did not request the actual artifact")
					}
				}
				return false
			}
			i := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: "darwin", Arch: "arm64", Runner: fakeRunner{run: func(context.Context, Command) (string, error) {
				return "codex-cli 1.2.3", nil
			}}})
			result, err := i.Install(t.Context(), "codex", nil)
			if checksumFailure {
				_, reason, _ := Diagnosis(err)
				if reason != "checksum" || fallbackCalls.Load() != 0 {
					t.Fatalf("checksum failure was retried or hidden: reason=%s calls=%d", reason, fallbackCalls.Load())
				}
			} else if err != nil || result.SourceURL != api || fallbackCalls.Load() != 1 {
				t.Fatalf("official fallback was not verified and recorded: result=%+v calls=%d error=%v", result, fallbackCalls.Load(), err)
			}
		})
	}
}

type retryTransport func(*http.Request) (*http.Response, error)

func (r retryTransport) RoundTrip(request *http.Request) (*http.Response, error) { return r(request) }

func TestPublisherTransportRetryAndCanceledCooldown(t *testing.T) {
	p := publisher(t)
	p.put("https://x.ai/cli/stable", []byte("1.2.3"))
	var calls atomic.Int32
	base := p.client.Transport
	p.client.Transport = retryTransport(func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, &net.DNSError{Err: "temporary resolution failure", IsTemporary: true}
		}
		return base.RoundTrip(request)
	})
	i := NewWithOptions(t.TempDir(), Options{Client: p.client})
	if _, err := i.read(t.Context(), "https://x.ai/cli/stable", metadataLimit); err != nil || calls.Load() != 2 {
		t.Fatalf("temporary transport failure did not retry: calls=%d error=%v", calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	p.handler = func(w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(503)
		cancel()
		return true
	}
	started := time.Now()
	if _, err := i.read(ctx, "https://x.ai/cli/stable", metadataLimit); err != status_error.OperationCanceled || time.Since(started) > time.Second {
		t.Fatalf("cancel waited for retry cooldown: error=%v elapsed=%v", err, time.Since(started))
	}
}

func TestInstallationDiagnosisDoesNotExposeRawCause(t *testing.T) {
	for _, sample := range []struct {
		// operation names one fixed installer boundary.
		operation string
		// cause contains a synthetic secret that must never reach the public error.
		cause error
		// reason is the controlled diagnosis expected by the HTTP adapter.
		reason string
	}{
		{"download official CLI artifact", &url.Error{Op: "Get", URL: "https://user:secret@proxy.invalid", Err: &net.DNSError{Err: "secret"}}, "network"},
		{"download official CLI artifact", x509.UnknownAuthorityError{}, "tls"},
		{"publisher artifact checksum", nil, "checksum"},
		{"unsafe archive entry or expansion limit", nil, "archive"},
		{"installed CLI version mismatch", nil, "version"},
		{"Node.js prerequisite missing", nil, "prerequisite"},
	} {
		err := failure(sample.operation, sample.cause)
		operation, reason, _ := Diagnosis(err)
		if operation != sample.operation || reason != sample.reason || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe or missing diagnosis: operation=%s reason=%s error=%v", operation, reason, err)
		}
	}
}

func TestZipContentsAndArchiveExpansionBounds(t *testing.T) {
	var bytes bytes.Buffer
	archive := zip.NewWriter(&bytes)
	file, err := archive.Create("bin/program.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("windows executable"))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	location := filepath.Join(t.TempDir(), "program.zip")
	if err := os.WriteFile(location, bytes.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := extract(context.Background(), location, root, "zip", ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "bin", "program.exe")); err != nil || string(data) != "windows executable" {
		t.Fatalf("bad zip extraction: %q %v", data, err)
	}
	bytes.Reset()
	archive = zip.NewWriter(&bytes)
	if _, err := archive.CreateRaw(&zip.FileHeader{Name: "bomb", UncompressedSize64: uint64(expandedLimit + 1)}); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(location, bytes.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extract(context.Background(), location, root, "zip", ""); !hasInstallationFailure(err) {
		t.Fatalf("oversized zip accepted: %v", err)
	}
}

func hasInstallationFailure(err error) bool {
	var detail interface{ Key() string }
	return errors.As(err, &detail) && detail.Key() == status_error.RuntimeInstallationFailed.Key()
}

func assertNoStaging(t *testing.T, root, id string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, id))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".staging-") {
			t.Errorf("abandoned staging: %s", entry.Name())
		}
	}
}

var _ io.Reader = (*contextReader)(nil)

func TestAllOfficialSourceResolvers(t *testing.T) {
	p := publisher(t)
	digest := strings.Repeat("a", 64)
	sha512 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 64))
	npm := func(name, version string) {
		p.put("https://registry.npmjs.org/"+name+"/"+version, map[string]any{"name": name, "version": "1.2.3", "bin": map[string]any{"gemini": "bundle/gemini.js"}, "dist": map[string]any{"tarball": "https://registry.npmjs.org/" + name + "/-/artifact.tgz", "integrity": "sha512-" + sha512}})
	}
	p.put("https://registry.npmjs.org/@anthropic-ai/claude-agent-sdk/latest", map[string]any{"version": "1.2.3", "claudeCodeVersion": "2.3.4"})
	npm("@anthropic-ai/claude-agent-sdk-darwin-arm64", "1.2.3")
	p.put("https://registry.npmjs.org/@opencode/cli/latest", map[string]any{"version": "1.2.3"})
	npm("@opencode/cli-darwin-arm64", "1.2.3")
	npm("@google/gemini-cli", "latest")
	p.put("https://cursor.com/install", []byte(`DOWNLOAD_URL="https://downloads.cursor.com/lab/2026.11.05-86047df/${OS}/${ARCH}/agent-cli-package.tar.gz"`))
	p.put("https://x.ai/cli/stable", []byte("1.2.3\n"))
	p.put("https://code.kimi.com/kimi-code/latest", []byte("2.5.2\n"))
	p.put("https://code.kimi.com/kimi-code/binaries/2.5.2/manifest.json", map[string]any{"version": "2.5.2", "platforms": map[string]any{
		"darwin-arm64": map[string]any{"filename": "kimi-code-darwin-arm64", "checksum": digest},
	}})
	for _, row := range [][4]string{
		{"openai/codex", "rust-v1.2.3", "codex-package-aarch64-apple-darwin.tar.gz", "codex"},
		{"earendil-works/pi", "v1.2.3", "pi-darwin-arm64.tar.gz", "pi"},
	} {
		p.put("https://api.github.com/repos/"+row[0]+"/releases/latest", map[string]any{"tag_name": row[1], "assets": []any{map[string]any{"name": row[2], "digest": "sha256:" + digest}}})
	}
	for _, row := range [][2]string{{"https://qoder-ide.oss-accelerate.aliyuncs.com/qodercli", "qodercli"}, {"https://static.qoder.com.cn/qoder-cli-cn", "qoderclicn"}} {
		p.put(row[0]+"/channels/manifest.json", map[string]any{"latest": "1.2.3", "files": []any{map[string]any{"os": "darwin", "arch": "arm64", "url": row[0] + "/releases/1.2.3/" + row[1] + "-darwin-arm64.tar.gz", "sha256": digest}}})
	}
	const workbuddy = "https://acc-1258344699.cos.accelerate.myqcloud.com/@tencent-ai/codebuddy-code/releases"
	p.put(workbuddy+"/latest", []byte("1.2.3"))
	p.put(workbuddy+"/download/1.2.3/checksums.txt", []byte(digest+"  codebuddy-code_Darwin_arm64.tar.gz\n"))
	i := NewWithOptions(t.TempDir(), Options{Client: p.client, OS: "darwin", Arch: "arm64"})
	for _, id := range []string{"codex", "claude", "cursor", "kimi", "opencode", "pi", "grok", "gemini", "qoder", "qoder-cn", "workbuddy"} {
		t.Run(id, func(t *testing.T) {
			artifact, err := i.resolve(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if artifact.version == "" || artifact.url == "" || artifact.binary == "" {
				t.Fatalf("incomplete official artifact: %#v", artifact)
			}
			if id != "cursor" && id != "grok" && artifact.digest == "" {
				t.Fatalf("publisher digest missing: %#v", artifact)
			}
			source, ok := i.Source(id)
			if !ok || source.RequiresNode != (id == "gemini") {
				t.Fatalf("wrong prerequisite matrix: %#v", source)
			}
		})
	}
}
