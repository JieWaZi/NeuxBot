// Package catalog 读取 HTTP 或本地目录元数据，按摘要下载不可变安装包。
package catalog

import (
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
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

var currentReads sync.Map

// Entry 描述一个本地目录项及其不可变来源。
type Entry struct {
	// SourceKey 是来源限定的逻辑身份，不凭同名合并。
	SourceKey string `json:"sourceKey"`
	// Key 是目录项稳定身份。
	Key string `json:"key"`
	// Directory 是分发目录内的包路径，不回传绝对位置。
	Directory string `json:"-"`
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
		DetailsURL json.RawMessage `json:"detailsURL"`
		// DetailsSHA256 是公开资料的完整摘要。
		DetailsSHA256 json.RawMessage `json:"detailsSHA256"`
		// DetailsSize 是公开资料的完整字节数。
		DetailsSize json.RawMessage `json:"detailsSize"`
		// PackagePath 是逻辑入口目录。
		PackagePath string `json:"packagePath"`
		// 图标
		Icon string `json:"icon"`
		// 本地名称
		DisplayNames map[string]string `json:"displayNames"`
		// 本地简介
		LocalizedDescriptions map[string]string `json:"localizedDescriptions"`
		// Name 是目录显示名称。
		Name string `json:"name"`
		// Version 是入口版本。
		Version string `json:"version"`
		// Description 是目录短描述。
		Description string `json:"description"`
		// Format 是包格式。
		Format string `json:"format"`
		// Key 是稳定目录键。
		Key string `json:"key"`
		// Directory 是受控相对位置。
		Directory string `json:"directory"`
		// SourceCommit 是固定源提交。
		SourceCommit string `json:"sourceCommit"`
		// ArchiveSHA256 是构建产物摘要。
		ArchiveSHA256 string `json:"archiveSHA256"`
		// Package 是由正式包解析器生成的构建元数据。
		Package plugins.Package `json:"package"`
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
	var index document
	if json.Unmarshal(data, &index) != nil || (index.SchemaVersion != 1 && index.SchemaVersion != 2) {
		return nil, status_error.PluginContentInvalid
	}
	seen := map[string]bool{}
	for _, item := range index.Entries {
		if !keyPattern.MatchString(item.Key) || (index.SchemaVersion == 1 && item.Directory != "catalog/"+item.Key+".zip") || seen[item.Key] || len(item.SourceCommit) != 40 || len(item.ArchiveSHA256) != 64 {
			return nil, status_error.PluginContentInvalid
		}
		if _, err := hex.DecodeString(item.SourceCommit); err != nil {
			return nil, status_error.PluginContentInvalid
		}
		if _, err := hex.DecodeString(item.ArchiveSHA256); err != nil {
			return nil, status_error.PluginContentInvalid
		}
		var detailsURL, detailsSHA256 string
		var detailsSize int64
		if len(item.DetailsURL) != 0 || len(item.DetailsSHA256) != 0 || len(item.DetailsSize) != 0 {
			if json.Unmarshal(item.DetailsURL, &detailsURL) != nil || json.Unmarshal(item.DetailsSHA256, &detailsSHA256) != nil || json.Unmarshal(item.DetailsSize, &detailsSize) != nil || !httpAddress(detailsURL) || len(detailsSHA256) != 64 || detailsSize <= 0 || detailsSize > maxDetailsBytes {
				return nil, status_error.PluginContentInvalid
			}
			if _, err := hex.DecodeString(detailsSHA256); err != nil {
				return nil, status_error.PluginContentInvalid
			}
		}
		seen[item.Key] = true
		if index.SchemaVersion == 2 {
			if !httpAddress(item.ArchiveURL) || item.PackagePath == "" || filepath.IsAbs(item.PackagePath) || strings.Contains(item.PackagePath, "..") {
				return nil, status_error.PluginContentInvalid
			}
			item.Package = plugins.Package{Manifest: plugins.Manifest{Name: item.Name, Version: item.Version, Description: item.Description}, Format: item.Format}
			item.Package.Icon = item.Icon
			item.Package.Presentation = plugins.Presentation{DisplayName: item.Name, ShortDescription: item.Description}
			item.Package.Presentation.DisplayNames = item.DisplayNames
			item.Package.Presentation.LocalizedDescriptions = item.LocalizedDescriptions
		}
		item.Package.Icon = plugins.SanitizeIcon(item.Package.Icon)
		if item.Package.Presentation.SetLocalization(item.Package.Presentation.DisplayNames, item.Package.Presentation.LocalizedDescriptions) != nil {
			return nil, status_error.PluginContentInvalid
		}
		if item.Package.Manifest.Name == "" {
			return nil, status_error.PluginContentInvalid
		}
		result = append(result, Entry{Key: item.Key, Directory: item.Directory, SourceCommit: item.SourceCommit, ArchiveSHA256: item.ArchiveSHA256, Package: item.Package, ArchiveURL: item.ArchiveURL, PackagePath: item.PackagePath, DetailsURL: detailsURL, DetailsSHA256: detailsSHA256, DetailsSize: detailsSize})
	}
	return result, nil
}

// Resolve 在创建受管快照之前校验标准 ZIP 的完整来源摘要。
func Resolve(root, key string) (string, error) {
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
		return ResolveEntry(root, entry)
	}
	return "", status_error.PluginNotFound
}

func httpAddress(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func download(entry Entry) (string, error) {
	parent := filepath.Join(os.TempDir(), "neuxbot-plugin-downloads")
	if os.MkdirAll(parent, 0700) != nil {
		return "", status_error.PluginStorageError
	}
	location := filepath.Join(parent, entry.ArchiveSHA256+".zip")
	if data, err := os.ReadFile(location); err == nil {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) == entry.ArchiveSHA256 {
			return location, nil
		}
	}
	client := http.Client{Timeout: 2 * time.Minute}
	response, err := client.Get(entry.ArchiveURL)
	if err != nil {
		return "", status_error.PluginDistributionUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", status_error.PluginDistributionUnavailable
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
		return "", status_error.PluginDistributionUnavailable
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
	return location, nil
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
func ResolveEntry(root string, entry Entry) (string, error) {
	var err error
	location := filepath.Join(root, entry.Directory)
	if entry.ArchiveURL != "" {
		location, err = download(entry)
		if err != nil {
			return "", err
		}
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
func ResolveCurrent(root, key string) (Entry, string, error) {
	return resolveCurrent(root, key, false)
}

// ResolveDocumentation 优先解析固定公开资料，旧目录才读取完整归档；公开资料有误不能回退安装包。
func ResolveDocumentation(root, key string) (Entry, string, error) {
	return resolveCurrent(root, key, true)
}

func resolveCurrent(root, key string, documentation bool) (Entry, string, error) {
	resolve := ResolveEntry
	same := sameArchive
	if documentation {
		resolve = func(root string, entry Entry) (string, error) {
			if entry.DetailsURL == "" {
				return ResolveEntry(root, entry)
			}
			return detailsLocation(entry)
		}
		same = func(first, second Entry) bool {
			return sameArchive(first, second) && first.DetailsURL == second.DetailsURL && first.DetailsSHA256 == second.DetailsSHA256 && first.DetailsSize == second.DetailsSize
		}
	}
	var index *remoteIndex
	if httpAddress(root) {
		index = remoteSource(root)
	}
	entry, err := Find(root, key)
	if err != nil {
		return Entry{}, "", err
	}
	// 正常读取只按条目与操作合并，不等待同来源安装或其他资料的网络请求。
	location, firstErr := resolveCurrentRead(root, entry, documentation, resolve)
	if firstErr == nil || index == nil || (!errors.Is(firstErr, status_error.PluginDistributionUnavailable) && !errors.Is(firstErr, status_error.PluginContentInvalid) && !(documentation && errors.Is(firstErr, status_error.PluginVersionConflict))) {
		return entry, location, firstErr
	}
	index.recoveryMutex.Lock()
	defer index.recoveryMutex.Unlock()
	if index.recoveryError != nil && time.Since(index.recoveryAt) < 5*time.Second && documentation == index.recoveryDocumentation && same(entry, index.recoveryEntry) {
		return entry, "", index.recoveryError
	}
	// 等待已发起的启动同步，避免与显式恢复竞争发布同来源快照。
	index.refresh()
	current, err := Find(root, key)
	if err != nil {
		return Entry{}, "", err
	}
	rememberFailure := func(entry Entry, err error) (Entry, string, error) {
		index.recoveryAt, index.recoveryEntry, index.recoveryError, index.recoveryDocumentation = time.Now(), entry, err, documentation
		return entry, "", err
	}
	if same(current, entry) {
		// 冷却只限制失败恢复；稍后用户重试仍可重新核验，没有后台轮询。
		if time.Since(index.recoveryAt) < 5*time.Second {
			return rememberFailure(entry, firstErr)
		}
		index.recoveryAt = time.Now()
		if err := index.refreshCurrent(index.ctx); err != nil {
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
	location, err = resolveCurrentRead(root, current, documentation, resolve)
	if err != nil {
		return rememberFailure(current, err)
	}
	index.recoveryError = nil
	return current, location, nil
}

// resolveCurrentRead 仅合并相同来源条目和操作；失败短暂复用，成功仍复验磁盘内容。
func resolveCurrentRead(root string, entry Entry, documentation bool, resolve func(string, Entry) (string, error)) (string, error) {
	identity, _ := json.Marshal([]any{root, entry, documentation})
	var mutex sync.Mutex
	var failedAt time.Time
	var failure error
	read := func() (string, error) {
		mutex.Lock()
		defer mutex.Unlock()
		if failure != nil && time.Since(failedAt) < 5*time.Second {
			return "", failure
		}
		location, err := resolve(root, entry)
		failure = err
		if err != nil {
			failedAt = time.Now()
		}
		return location, err
	}
	actual, _ := currentReads.LoadOrStore(string(identity), read)
	return actual.(func() (string, error))()
}

func sameArchive(first, second Entry) bool {
	return first.SourceCommit == second.SourceCommit && first.ArchiveSHA256 == second.ArchiveSHA256 && first.ArchiveURL == second.ArchiveURL && first.PackagePath == second.PackagePath && first.Directory == second.Directory
}
