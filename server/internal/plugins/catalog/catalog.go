// Package catalog 读取 HTTP 或本地目录元数据，按摘要下载不可变安装包。
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

var currentReads sync.Map

// Entry 描述一个本地目录项及其不可变来源。
type Entry struct {
	// cacheRoot 指向当前来源使用的受管公开资料与归档缓存。
	cacheRoot string
	// SourceKey 是来源限定的逻辑身份，不凭同名合并。
	SourceKey string `json:"sourceKey"`
	// Key 是目录项稳定身份。
	Key string `json:"key"`
	// SourceCommit 是独立插件源固定提交。
	SourceCommit string `json:"sourceCommit"`
	// ArchiveSHA256 是完整压缩包摘要。
	ArchiveSHA256 string `json:"archiveSHA256"`
	// ArchiveURL 是不可变 HTTP 下载地址。
	ArchiveURL string `json:"archiveURL,omitempty"`
	// PackagePath 是完整来源中的逻辑入口。
	PackagePath string `json:"packagePath,omitempty"`
	// DetailsURL 是目录明确声明的公开资料地址。
	DetailsURL string `json:"detailsURL,omitempty"`
	// DetailsSHA256 固定公开资料的完整字节摘要。
	DetailsSHA256 string `json:"detailsSHA256,omitempty"`
	// DetailsSize 是公开资料的完整字节数。
	DetailsSize int64 `json:"detailsSize,omitempty"`
	// Package 是本地包解析得到的能力与诊断。
	Package plugins.Package `json:"package"`
}

// document 是本地构建格式，不作为前端业务模型。
type document struct {
	// SchemaVersion 是目录格式版本。
	SchemaVersion int `json:"schemaVersion"`
	// Entries 是全部有限目录项。
	Entries []struct {
		// ArchiveURL 是不可变 HTTP 下载地址。
		ArchiveURL string `json:"archiveURL"`
		// DetailsURL 是公开资料的明确地址。
		DetailsURL string `json:"detailsURL"`
		// DetailsSHA256 是公开资料的完整摘要。
		DetailsSHA256 string `json:"detailsSHA256"`
		// DetailsSize 是公开资料的完整字节数。
		DetailsSize int64 `json:"detailsSize"`
		// PackagePath 是逻辑入口目录。
		PackagePath string `json:"packagePath"`
		// Icon 是默认主题图标的派生摘要。
		Icon string `json:"icon"`
		// Icons 是经过安全内嵌校验的明暗主题图标。
		Icons plugins.ThemeValues `json:"icons,omitempty"`
		// 本地名称
		DisplayNames map[string]string `json:"displayNames"`
		// 本地简介
		LocalizedDescriptions map[string]string `json:"localizedDescriptions"`
		// Name 是目录显示名称。
		Name string `json:"name"`
		// Version 是入口版本。
		Version string `json:"version"`
		// Category 是发布包声明的标准分类。
		Category string `json:"category,omitempty"`
		// Description 是目录短描述。
		Description string `json:"description"`
		// Format 是包格式。
		Format string `json:"format"`
		// Key 是稳定目录键。
		Key string `json:"key"`
		// SourceCommit 是固定源提交。
		SourceCommit string `json:"sourceCommit"`
		// ArchiveSHA256 是构建产物摘要。
		ArchiveSHA256 string `json:"archiveSHA256"`
	} `json:"entries"`
}

// List 读取HTTP或显式本地目录清单，不预取插件归档。
func List(root string) ([]Entry, error) {
	data, err := readIndex(root)
	if err != nil {
		return nil, err
	}
	entries, err := parseIndex(data)
	if err != nil {
		return nil, err
	}
	for index := range entries {
		entries[index].SourceKey = SourceKey(root, entries[index].Key)
		entries[index].cacheRoot = cacheRoot(root)
	}
	return entries, nil
}

// SourceKey 将来源目录与受支持的条目键绑定，供固定版本与配置目录核验来源身份。
func SourceKey(root, key string) string {
	if !keyPattern.MatchString(key) {
		return ""
	}
	if root == constants.PluginCatalogHTTPSource {
		return "catalog:" + key
	}
	sum := sha256.Sum256([]byte(root))
	return "catalog:" + hex.EncodeToString(sum[:8]) + ":" + key
}

func parseIndex(data []byte) ([]Entry, error) {
	result := []Entry{}
	if len(data) > 4*1024*1024 {
		return nil, status_error.PluginContentLimitExceeded
	}
	object, err := publicObject(data, "schemaVersion", "entries")
	if err != nil {
		return nil, err
	}
	var rawEntries []json.RawMessage
	if json.Unmarshal(object["entries"], &rawEntries) != nil || rawEntries == nil {
		return nil, status_error.PluginContentInvalid
	}
	for _, entry := range rawEntries {
		fields, err := publicObject(entry, "key", "name", "version", "description", "format", "category", "sourceCommit", "archiveSHA256", "archiveURL", "packagePath", "detailsURL", "detailsSHA256", "detailsSize", "icon", "icons", "displayNames", "localizedDescriptions")
		if err != nil {
			return nil, err
		}
		if value := fields["icons"]; len(value) > 0 {
			if _, err := publicObject(value, "default", "dark"); err != nil {
				return nil, err
			}
		}
	}
	var index document
	if json.Unmarshal(data, &index) != nil || index.SchemaVersion != constants.PluginCatalogSchemaVersion {
		return nil, status_error.PluginContentInvalid
	}
	seen := map[string]bool{}
	for _, item := range index.Entries {
		if !keyPattern.MatchString(item.Key) || seen[item.Key] || len(item.SourceCommit) != 40 || len(item.ArchiveSHA256) != 64 {
			return nil, status_error.PluginContentInvalid
		}
		if _, err := hex.DecodeString(item.SourceCommit); err != nil {
			return nil, status_error.PluginContentInvalid
		}
		if _, err := hex.DecodeString(item.ArchiveSHA256); err != nil {
			return nil, status_error.PluginContentInvalid
		}
		detailsURL, detailsSHA256, detailsSize := item.DetailsURL, item.DetailsSHA256, item.DetailsSize
		if !httpAddress(detailsURL) || len(detailsSHA256) != 64 || detailsSize <= 0 || detailsSize > maxDetailsBytes || item.Format != constants.PluginFormatPortable {
			return nil, status_error.PluginContentInvalid
		}
		if _, err := hex.DecodeString(detailsSHA256); err != nil {
			return nil, status_error.PluginContentInvalid
		}
		seen[item.Key] = true
		if !httpAddress(item.ArchiveURL) || (item.PackagePath != "." && !safeDetailsPath(item.PackagePath)) {
			return nil, status_error.PluginContentInvalid
		}
		pkg := plugins.Package{ContractVersion: constants.PluginTargetContractVersion, Manifest: plugins.Manifest{Schema: constants.PluginManifestSchema, Name: item.Name, Version: item.Version, Description: item.Description}, Format: item.Format, Icon: item.Icon, Presentation: plugins.Presentation{DisplayName: item.Name, ShortDescription: item.Description, Category: item.Category, Icons: item.Icons, DisplayNames: item.DisplayNames, LocalizedDescriptions: item.LocalizedDescriptions}}
		pkg.Presentation.Icons.Default = plugins.SanitizeIcon(pkg.Presentation.Icons.Default)
		pkg.Presentation.Icons.Dark = plugins.SanitizeIcon(pkg.Presentation.Icons.Dark)
		if pkg.Presentation.Icons.Default == "" {
			pkg.Presentation.Icons.Default = plugins.SanitizeIcon(pkg.Icon)
		}
		pkg.Icon = pkg.Presentation.Icons.Default
		if pkg.Presentation.SetLocalization(pkg.Presentation.DisplayNames, pkg.Presentation.LocalizedDescriptions) != nil {
			return nil, status_error.PluginContentInvalid
		}
		if pkg.Manifest.Name == "" {
			return nil, status_error.PluginContentInvalid
		}
		result = append(result, Entry{Key: item.Key, SourceCommit: item.SourceCommit, ArchiveSHA256: item.ArchiveSHA256, Package: pkg, ArchiveURL: item.ArchiveURL, PackagePath: item.PackagePath, DetailsURL: detailsURL, DetailsSHA256: detailsSHA256, DetailsSize: detailsSize})
	}
	return result, nil
}

// Resolve 在创建受管快照之前校验标准 ZIP 的完整来源摘要。
func Resolve(ctx context.Context, root, key string) (string, error) {
	if !keyPattern.MatchString(key) {
		return "", status_error.PluginContentInvalid
	}
	entries, err := List(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		return ResolveEntry(ctx, entry)
	}
	return "", status_error.PluginNotFound
}

func httpAddress(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func download(ctx context.Context, entry Entry) (string, error) {
	parent := filepath.Join(entryStorageRoot(entry), "archives")
	if os.MkdirAll(parent, 0700) != nil {
		return "", status_error.PluginStorageError
	}
	location := filepath.Join(parent, entry.ArchiveSHA256+".zip")
	state := artifactState(&archiveLocks, location)
	if err := state.acquire(ctx); err != nil {
		return "", err
	}
	defer state.release()
	if data, err := os.ReadFile(location); err == nil {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) == entry.ArchiveSHA256 {
			return location, nil
		}
	}
	// 后台预取与安装兜底共享同一受管根的两路网络许可；缓存命中不占传输额度。
	actual, _ := archiveTransfers.LoadOrStore(entryStorageRoot(entry), make(chan struct{}, 2))
	transfers := actual.(chan struct{})
	select {
	case transfers <- struct{}{}:
		defer func() { <-transfers }()
	case <-ctx.Done():
		return "", status_error.PluginDistributionUnavailable
	}
	client := http.Client{Timeout: 2 * time.Minute}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.ArchiveURL, nil)
	if err != nil {
		return "", status_error.PluginContentInvalid
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		logDistributionFailure("archive_get", entry.Key, entry.ArchiveURL, response, started, err)
		return "", state.fail(entry.ArchiveURL, status_error.PluginDistributionUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		logDistributionFailure("archive_status", entry.Key, entry.ArchiveURL, response, started, nil)
		return "", state.fail(entry.ArchiveURL, status_error.PluginDistributionUnavailable)
	}
	temporary, err := os.CreateTemp(parent, "download-*.zip")
	if err != nil {
		return "", status_error.PluginStorageError
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, 100*1024*1024+1))
	if err != nil {
		logDistributionFailure("archive_body", entry.Key, entry.ArchiveURL, response, started, err)
		return "", state.fail(entry.ArchiveURL, status_error.PluginDistributionUnavailable)
	}
	if size > 100*1024*1024 {
		return "", status_error.PluginContentLimitExceeded
	}
	if hex.EncodeToString(hash.Sum(nil)) != entry.ArchiveSHA256 {
		return "", status_error.PluginContentInvalid
	}
	if temporary.Close() != nil || os.Rename(temporary.Name(), location) != nil {
		return "", status_error.PluginStorageError
	}
	state.failure = nil
	return location, nil
}

// artifactLoadState 在同一路径合并下载，并允许关闭时取消锁等待。
type artifactLoadState struct {
	// gate 串行化文件核验和原子发布。
	gate chan struct{}
	// failedURL 绑定短暂失败缓存的具体分发地址。
	failedURL string
	// failedAt 记录同地址最近失败时间。
	failedAt time.Time
	// failure 复用短冷却期内的稳定错误。
	failure error
}

var archiveLocks sync.Map
var archiveTransfers sync.Map

func artifactState(states *sync.Map, location string) *artifactLoadState {
	value, _ := states.LoadOrStore(location, &artifactLoadState{gate: make(chan struct{}, 1)})
	return value.(*artifactLoadState)
}

func (state *artifactLoadState) acquire(ctx context.Context) error {
	select {
	case state.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return status_error.PluginDistributionUnavailable
	}
}
func (state *artifactLoadState) release() { <-state.gate }
func (state *artifactLoadState) fail(address string, err error) error {
	state.failedURL, state.failedAt, state.failure = address, time.Now(), err
	return err
}

func entryStorageRoot(entry Entry) string {
	if entry.cacheRoot != "" {
		return entry.cacheRoot
	}
	return filepath.Join(os.TempDir(), "neuxbot-plugin-catalog")
}

func cacheRoot(source string) string {
	if value, ok := remoteIndexes.Load(source); ok {
		return value.(*remoteIndex).directory
	}
	return filepath.Join(os.TempDir(), "neuxbot-plugin-catalog")
}

// logDistributionFailure 在底层错误首次转换时保留阶段、耗时与上游状态，不输出重定向签名或查询凭据。
func logDistributionFailure(operation, key, address string, response *http.Response, started time.Time, err error) {
	location, host, status := "", "", 0
	if parsed, parseErr := url.Parse(address); parseErr == nil {
		location = parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
		host = parsed.Host
	}
	if response != nil {
		status = response.StatusCode
		if response.Request != nil && response.Request.URL != nil {
			host = response.Request.URL.Host
		}
	}
	// url.Error 自带完整请求地址，必须只记录不含 URL 的底层连接或读取错误。
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		err = requestErr.Err
	}
	var timeout interface{ Timeout() bool }
	timedOut := errors.As(err, &timeout) && timeout.Timeout()
	logx.Warnf("plugin distribution request failed: operation=%s plugin_key=%s url=%s upstream_host=%s upstream_status=%d elapsed_ms=%d timeout=%t error=%v", operation, key, location, host, status, time.Since(started).Milliseconds(), timedOut, err)
}

// Find 返回已验证目录中的精确入口，安装不得自行猜测包根。
func Find(root, key string) (Entry, error) {
	entries, err := List(root)
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range entries {
		if entry.Key == key {
			return entry, nil
		}
	}
	return Entry{}, status_error.PluginNotFound
}

// ResolveEntry 下载已经选定的目录版本，不在下载前重新查询latest。
func ResolveEntry(ctx context.Context, entry Entry) (string, error) {
	if !httpAddress(entry.ArchiveURL) {
		return "", status_error.PluginContentInvalid
	}
	location, err := download(ctx, entry)
	if err != nil {
		return "", err
	}
	bytes, err := os.ReadFile(location)
	if err != nil {
		return "", status_error.PluginStorageError
	}
	if len(bytes) > 100*1024*1024 {
		return "", status_error.PluginContentLimitExceeded
	}
	sum := sha256.Sum256(bytes)
	if hex.EncodeToString(sum[:]) != entry.ArchiveSHA256 {
		return "", status_error.PluginContentInvalid
	}
	return location, nil
}

// ResolveCurrent 仅为未固定版本的当前详情和准备操作恢复已失效的目录分发；固定读取仍使用 ResolveEntry。
func ResolveCurrent(ctx context.Context, root, key string) (Entry, string, error) {
	return resolveCurrent(ctx, root, key, false)
}

// ResolveDocumentation 仅解析固定公开资料，缺失或失败不能回退安装归档。
func ResolveDocumentation(ctx context.Context, root, key string) (Entry, string, error) {
	return resolveCurrent(ctx, root, key, true)
}

func resolveCurrent(ctx context.Context, root, key string, documentation bool) (Entry, string, error) {
	resolve := func(_ string, entry Entry) (string, error) { return ResolveEntry(ctx, entry) }
	same := sameArchive
	if documentation {
		resolve = func(root string, entry Entry) (string, error) {
			if entry.DetailsURL == "" {
				return "", status_error.PluginContentInvalid
			}
			return detailsLocation(ctx, entry)
		}
		same = func(first, second Entry) bool {
			return sameArchive(first, second) && first.DetailsURL == second.DetailsURL && first.DetailsSHA256 == second.DetailsSHA256 && first.DetailsSize == second.DetailsSize
		}
	}
	var index *remoteIndex
	if httpAddress(root) {
		index = remoteSource(root)
	}
	if index != nil {
		index.mu.RLock()
		cold := len(index.snapshot.Content) == 0
		index.mu.RUnlock()
		if cold {
			if err := index.refreshCurrent(ctx); err != nil {
				return Entry{}, "", err
			}
		}
	}
	entry, err := Find(root, key)
	if err != nil {
		return Entry{}, "", err
	}
	// 正常读取只按条目与操作合并，不等待同来源安装或其他资料的网络请求。
	location, firstErr := resolveCurrentRead(ctx, root, entry, documentation, resolve)
	if firstErr == nil || index == nil || (!errors.Is(firstErr, status_error.PluginDistributionUnavailable) && !errors.Is(firstErr, status_error.PluginContentInvalid) && !(documentation && errors.Is(firstErr, status_error.PluginVersionConflict))) {
		return entry, location, firstErr
	}
	select {
	case index.recoveryGate <- struct{}{}:
		defer func() { <-index.recoveryGate }()
	case <-ctx.Done():
		return entry, "", status_error.PluginDistributionUnavailable
	}
	if ctx.Err() != nil {
		return entry, "", status_error.PluginDistributionUnavailable
	}
	if index.recoveryError != nil && time.Since(index.recoveryAt) < 5*time.Second && documentation == index.recoveryDocumentation && same(entry, index.recoveryEntry) {
		return entry, "", index.recoveryError
	}
	// 等待已发起的启动同步，避免与显式恢复竞争发布同来源快照。
	if err := index.refresh(ctx); err != nil {
		return entry, "", err
	}
	current, err := Find(root, key)
	if err != nil {
		return Entry{}, "", err
	}
	rememberFailure := func(entry Entry, err error) (Entry, string, error) {
		if ctx.Err() == nil {
			index.recoveryAt, index.recoveryEntry, index.recoveryError, index.recoveryDocumentation = time.Now(), entry, err, documentation
		}
		return entry, "", err
	}
	if same(current, entry) {
		// 冷却只限制失败恢复；稍后用户重试仍可重新核验，没有后台轮询。
		if time.Since(index.recoveryAt) < 5*time.Second {
			return rememberFailure(entry, firstErr)
		}
		if err := index.refreshCurrent(ctx); err != nil {
			return rememberFailure(entry, err)
		}
		current, err = Find(root, key)
		if err != nil {
			return Entry{}, "", err
		}
		// 元数据未声明另一个归档时，不对同一失败地址再等待一次网络超时。
		if same(current, entry) {
			return rememberFailure(entry, firstErr)
		}
	}
	location, err = resolveCurrentRead(ctx, root, current, documentation, resolve)
	if err != nil {
		return rememberFailure(current, err)
	}
	index.recoveryError = nil
	return current, location, nil
}

// resolveCurrentRead 仅合并相同来源条目和操作；失败短暂复用，成功仍复验磁盘内容。
func resolveCurrentRead(ctx context.Context, root string, entry Entry, documentation bool, resolve func(string, Entry) (string, error)) (string, error) {
	identity, _ := json.Marshal([]any{root, entry, entry.cacheRoot, documentation})
	state := artifactState(&currentReads, string(identity))
	if err := state.acquire(ctx); err != nil {
		return "", err
	}
	defer state.release()
	if state.failure != nil && time.Since(state.failedAt) < 5*time.Second {
		return "", state.failure
	}
	location, err := resolve(root, entry)
	if err != nil && ctx.Err() == nil {
		state.fail(root, err)
	} else {
		state.failure = nil
	}
	return location, err
}

func sameArchive(first, second Entry) bool {
	return first.SourceCommit == second.SourceCommit && first.ArchiveSHA256 == second.ArchiveSHA256 && first.ArchiveURL == second.ArchiveURL && first.PackagePath == second.PackagePath
}
