package plugins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// 同进程准备串行化，避免两个执行接纳同时更新同一份账号快照。
var codexHomePrepareMu sync.Mutex

// NativeTarget 描述已冻结、受管的原生插件安装内容。
type NativeTarget struct {
	// ID 是应用内的插件逻辑身份，不用作 Codex 原生插件标识。
	ID string
	// Name 是原生插件清单声明的名称。
	Name string
	// Version 是当前安装固定的原生插件版本。
	Version string
	// Root 是完整原生插件包的受管根目录。
	Root string
	// Digest 是安装内容摘要，用于隔离不同执行产物。
	Digest string
	// headerTemplates 只保存派生请求头的无秘密模板，运行时展开值不进入插件文件。
	headerTemplates map[string]string
}

// PrepareCodexHome 为选定账号和插件快照准备独立 CODEX_HOME，不修改原账号目录。
// 相同快照复用目录以支持冷恢复；源认证未变化时保留 Codex 在私有目录刷新后的凭据。
// accountHome 为空时不推断账号目录，认证可由调用方通过进程环境提供；本函数不证明已登录。
// 仅存于系统钥匙串且未落入 auth.json 的认证不会迁移，调用方须保留账号接纳与登录诊断。
func PrepareCodexHome(parent, accountHome string, targets []NativeTarget) (string, error) {
	return prepareCodexHome(parent, accountHome, targets, false)
}

func prepareCodexHome(parent, accountHome string, targets []NativeTarget, useAccountHome bool) (string, error) {
	codexHomePrepareMu.Lock()
	defer codexHomePrepareMu.Unlock()
	if strings.TrimSpace(parent) == "" {
		return "", status_error.PluginContentInvalid
	}
	parent, err := filepath.Abs(parent)
	if err != nil {
		return "", storageFailure(err)
	}
	if accountHome != "" {
		accountHome, err = filepath.Abs(accountHome)
		if err != nil {
			return "", storageFailure(err)
		}
		info, err := os.Lstat(accountHome)
		if err != nil {
			return "", codexAccountFailure(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.AIAccountConfigurationInvalid
		}
		if (codexPathWithin(accountHome, parent) && !nativeWorktreeDirectory(accountHome, parent)) || codexPathWithin(parent, accountHome) {
			return "", status_error.PluginContentInvalid
		}
	}
	targets = append([]NativeTarget(nil), targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	names := map[string]bool{}
	for i := range targets {
		target := &targets[i]
		if target.ID == "" || target.Digest == "" || !codexPathSegment(target.Name) || !codexPathSegment(target.Version) || names[target.Name] {
			return "", status_error.PluginContentInvalid
		}
		names[target.Name] = true
		if !filepath.IsAbs(target.Root) {
			return "", status_error.PluginContentInvalid
		}
		target.Root = filepath.Clean(target.Root)
		if codexPathWithin(target.Root, parent) || codexPathWithin(parent, target.Root) {
			return "", status_error.PluginContentInvalid
		}
		if err := codexValidatePackage(target.Root); err != nil {
			return "", err
		}
	}
	config, auth, err := codexAccountSnapshot(accountHome)
	if err != nil {
		return "", err
	}
	enabled := map[string]any{}
	for _, target := range targets {
		enabled[target.Name+"@neuxally-managed"] = map[string]any{"enabled": true}
	}
	config["plugins"] = enabled
	config["features"] = map[string]any{"plugins": true}
	config["cli_auth_credentials_store"] = "file"
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(config); err != nil {
		return "", codexAccountFailure(err)
	}
	identity, err := json.Marshal([]any{accountHome, targets})
	if err != nil {
		return "", storageFailure(err)
	}
	digest := sha256.Sum256(identity)
	home := filepath.Join(parent, "codex-"+hex.EncodeToString(digest[:]))
	if useAccountHome {
		home = accountHome
	}
	if err := codexPrivateDir(home); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return "", storageFailure(err)
	}
	defer root.Close()
	// 相同快照按完整内容验证后复用，避免替换其他活跃会话正在读取的原生包。
	cache := filepath.Join(home, "plugins", "cache")
	if err := codexPrivateSubdir(root, filepath.Join("plugins", "cache", constants.PluginNativeMarketplace)); err != nil {
		return "", err
	}
	for _, target := range targets {
		destination := filepath.Join(cache, constants.PluginNativeMarketplace, target.Name, target.Version)
		if err := codexPrivateSubdir(root, filepath.Join("plugins", "cache", constants.PluginNativeMarketplace, target.Name)); err != nil {
			return "", err
		}
		if _, err := os.Lstat(destination); err == nil {
			expected, err := codexPackageFingerprint(target.Root)
			if err != nil {
				return "", err
			}
			actual, err := codexPackageFingerprint(destination)
			if err != nil {
				return "", err
			}
			if expected != actual {
				if !useAccountHome {
					return "", status_error.PluginVersionConflict
				}
				// 账号目录只由同一聊天的接纳占位持有；上一轮进程已退出后才能替换派生包。
				if err := root.RemoveAll(filepath.Join("plugins", "cache", constants.PluginNativeMarketplace, target.Name, target.Version)); err != nil {
					return "", storageFailure(err)
				}
			} else {
				continue
			}
		} else if !os.IsNotExist(err) {
			return "", storageFailure(err)
		}
		staging, err := os.MkdirTemp(filepath.Dir(destination), ".neuxally-package-")
		if err != nil {
			return "", storageFailure(err)
		}
		err = codexCopyPackage(target.Root, filepath.Join(staging, "package"))
		if err == nil {
			if renameErr := os.Rename(filepath.Join(staging, "package"), destination); renameErr != nil {
				err = storageFailure(renameErr)
			}
		}
		cleanupErr := os.RemoveAll(staging)
		if err != nil {
			return "", err
		}
		if cleanupErr != nil {
			return "", storageFailure(cleanupErr)
		}
	}
	if err := codexWritePrivate(root, "config.toml", encoded.Bytes()); err != nil {
		return "", err
	}
	if useAccountHome {
		// 保留账号模块拥有的原始认证位置，使订阅刷新仍能被租约释放流程回填。
		return home, nil
	}
	authHash := sha256.Sum256(auth)
	fingerprint := []byte(hex.EncodeToString(authHash[:]))
	previous, err := codexReadPrivate(root, ".neuxally-auth-source")
	if err != nil {
		return "", err
	}
	if !bytes.Equal(previous, fingerprint) {
		if auth == nil {
			if err := root.Remove("auth.json"); err != nil && !os.IsNotExist(err) {
				return "", storageFailure(err)
			}
		} else if err := codexWritePrivate(root, "auth.json", auth); err != nil {
			return "", err
		}
		if err := codexWritePrivate(root, ".neuxally-auth-source", fingerprint); err != nil {
			return "", err
		}
	} else {
		// 即使原生进程改写过文件，也维持凭据权限；不跟随被替换成链接的认证文件。
		info, err := root.Lstat("auth.json")
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", status_error.PluginContentInvalid
			}
			if err := root.Chmod("auth.json", 0600); err != nil {
				return "", storageFailure(err)
			}
		} else if !os.IsNotExist(err) {
			return "", storageFailure(err)
		}
	}
	return home, nil
}

func codexPathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._-+", char)) {
			return false
		}
	}
	return true
}

func codexPathWithin(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func codexPrivateDir(path string) error {
	// 只收紧明确指定的私有目录，创建缺失父目录时不修改已有用户目录权限。
	var ensure func(string) error
	ensure = func(current string) error {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := ensure(filepath.Dir(current)); err != nil {
				return err
			}
			if err := os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
				return storageFailure(err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return storageFailure(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return status_error.PluginContentInvalid
		}
		return nil
	}
	if err := ensure(path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0700); err != nil {
		return storageFailure(err)
	}
	return nil
}

func codexValidatePackage(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return storageFailure(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return status_error.PluginContentInvalid
	}
	return filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return storageFailure(err)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return status_error.PluginContentInvalid
		}
		return nil
	})
}

func codexCopyPackage(source, destination string) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return storageFailure(err)
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return storageFailure(err)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return status_error.PluginContentInvalid
		}
		info, err := entry.Info()
		if err != nil {
			return storageFailure(err)
		}
		output := filepath.Join(destination, filepath.FromSlash(path))
		if entry.IsDir() {
			if err := os.Mkdir(output, 0700|(info.Mode().Perm()&0111)); err != nil {
				return storageFailure(err)
			}
			return nil
		}
		input, err := root.Open(path)
		if err != nil {
			return storageFailure(err)
		}
		defer input.Close()
		file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return storageFailure(err)
		}
		_, copyErr := io.Copy(file, input)
		closeErr := file.Close()
		if copyErr != nil {
			return storageFailure(copyErr)
		}
		if closeErr != nil {
			return storageFailure(closeErr)
		}
		if err := os.Chmod(output, info.Mode().Perm()); err != nil {
			return storageFailure(err)
		}
		return nil
	})
}

func codexReadPrivate(root *os.Root, path string) ([]byte, error) {
	info, err := root.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, storageFailure(err)
	}
	if !info.Mode().IsRegular() {
		return nil, status_error.PluginContentInvalid
	}
	data, err := root.ReadFile(path)
	if err != nil {
		return nil, storageFailure(err)
	}
	return data, nil
}

func codexWritePrivate(root *os.Root, path string, data []byte) error {
	// 私有目录内使用独立临时文件再替换，失败时不留下半份凭据或配置。
	temporary, err := os.CreateTemp(root.Name(), ".neuxally-write-")
	if err != nil {
		return storageFailure(err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return storageFailure(err)
	}
	if err := temporary.Close(); err != nil {
		return storageFailure(err)
	}
	if err := root.Rename(filepath.Base(name), path); err != nil {
		return storageFailure(err)
	}
	return nil
}

func codexAccountSnapshot(home string) (map[string]any, []byte, error) {
	config := map[string]any{}
	if home == "" {
		return config, nil, nil
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, nil, codexAccountFailure(err)
	}
	defer root.Close()
	read := func(path string) ([]byte, error) {
		info, err := root.Lstat(path)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, codexAccountFailure(err)
		}
		if !info.Mode().IsRegular() {
			return nil, status_error.AIAccountConfigurationInvalid
		}
		data, err := root.ReadFile(path)
		if err != nil {
			return nil, codexAccountFailure(err)
		}
		return data, nil
	}
	raw, err := read("config.toml")
	if err != nil {
		return nil, nil, err
	}
	source := map[string]any{}
	if _, err := toml.Decode(string(raw), &source); err != nil {
		return nil, nil, codexAccountFailure(err)
	}
	codexModelFields(source, config)
	if profile, ok := source["profile"].(string); ok {
		if profiles, ok := source["profiles"].(map[string]any); ok {
			if selected, ok := profiles[profile].(map[string]any); ok {
				codexModelFields(selected, config)
			}
		}
	}
	if providers, ok := source["model_providers"].(map[string]any); ok {
		filtered := map[string]any{}
		for id, value := range providers {
			provider, ok := value.(map[string]any)
			if !ok {
				continue
			}
			allowed := map[string]any{}
			for _, key := range []string{"name", "base_url", "env_key", "env_key_instructions", "experimental_bearer_token", "wire_api", "requires_openai_auth", "supports_websockets", "request_max_retries", "stream_max_retries", "stream_idle_timeout_ms", "query_params", "http_headers", "env_http_headers"} {
				if value, ok := provider[key]; ok {
					allowed[key] = value
				}
			}
			filtered[id] = allowed
		}
		config["model_providers"] = filtered
	}
	auth, err := read("auth.json")
	if err != nil {
		return nil, nil, err
	}
	if auth != nil && !json.Valid(auth) {
		return nil, nil, status_error.AIAccountConfigurationInvalid
	}
	return config, auth, nil
}

func codexModelFields(source, destination map[string]any) {
	for _, key := range []string{"model", "model_provider", "model_reasoning_effort", "model_reasoning_summary", "model_verbosity", "model_context_window", "model_auto_compact_token_limit", "model_catalog_json", "preferred_auth_method", "forced_login_method", "forced_chatgpt_workspace_id", "openai_base_url", "chatgpt_base_url", "disable_response_storage"} {
		if value, ok := source[key]; ok {
			destination[key] = value
		}
	}
}

func codexAccountFailure(err error) error {
	// TOML 解析错误可能含原始认证字段，日志只记录错误类型，禁止记录配置内容。
	logx.Errorf("codex selected account configuration could not be read: %T", err)
	return status_error.AIAccountConfigurationInvalid
}

func codexPackageFingerprint(path string) (string, error) {
	if err := codexValidatePackage(path); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return "", storageFailure(err)
	}
	defer root.Close()
	digest := sha256.New()
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return storageFailure(err)
		}
		info, err := entry.Info()
		if err != nil {
			return storageFailure(err)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return status_error.PluginContentInvalid
		}
		fmt.Fprintf(digest, "%d:%s:%t:%o:", len(path), path, entry.IsDir(), info.Mode().Perm()&0111)
		if entry.IsDir() {
			return nil
		}
		file, err := root.Open(path)
		if err != nil {
			return storageFailure(err)
		}
		fmt.Fprintf(digest, "%d:", info.Size())
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return storageFailure(copyErr)
		}
		if closeErr != nil {
			return storageFailure(closeErr)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func codexPrivateSubdir(root *os.Root, relative string) error {
	current := ""
	for _, segment := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, segment)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			if err := root.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
				return storageFailure(err)
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return storageFailure(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return status_error.PluginContentInvalid
		}
		if err := root.Chmod(current, 0700); err != nil {
			return storageFailure(err)
		}
	}
	return nil
}
