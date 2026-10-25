package plugins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// NativeEnvironmentSpec 分开声明模型账号目录与原生插件唯一权威目录。
type NativeEnvironmentSpec struct {
	// RuntimeID 是提供原生插件能力的内置智能体标识。
	RuntimeID string
	// AccountHome 是已隔离的受管模型账号目录；为空时沿用原生模型身份。
	AccountHome string
	// SharedPluginHome 是外部同一 CLI 使用的原生配置根目录。
	SharedPluginHome string
	// Executable 是已经完成探测的真实 CLI 绝对路径。
	Executable string
	// PrefixArgs 是已经通过 Runtime 校验的前置启动参数。
	PrefixArgs []string
	// Targets 是本轮附加交付且不登记到外部原生环境的自有包。
	Targets []NativeTarget
	// AllowExternal 表示本轮能力范围允许加载和管理外部原生插件。
	AllowExternal bool
	// ModelEnvironmentKeys 是路由原生管理命令时必须移除的本轮模型账号覆盖。
	ModelEnvironmentKeys []string
	// StateDirectory 是聊天所有的能力摘要缓存目录，不能指向原生全局配置目录。
	StateDirectory string
	// Workspace 是本轮自有 marketplace 所在的受管工作区。
	Workspace string
}

// NativeEnvironment 是只用于一次执行接纳的原生插件派生视图。
type NativeEnvironment struct {
	// Home 是保留所选模型身份且附加允许插件的运行目录。
	Home string
	// CommandDirectory 是固定真实 CLI 的原生管理命令入口目录。
	CommandDirectory string
	// Fingerprint 是原生插件配置摘要，外部启停变化不能复用旧能力前缀。
	Fingerprint string
	// ScopeChanged 表示此运行目录上次接纳的外部插件能力摘要已经变化。
	ScopeChanged bool
	// CodexConfiguration 是仅在当前进程生效的 Codex 原生点路径配置覆盖。
	CodexConfiguration map[string]string
	// Marketplace 是本轮自有缓存的稳定独占命名空间；空值使用默认交付命名空间。
	Marketplace string
	// CommitScope 在执行接纳成功后保存可重建摘要，准备失败不能吞掉下一次会话重建。
	CommitScope func() error
	// ClaudeSettingsPath 是仅当前进程读取的受管模型配置，不覆盖原生插件与认证目录。
	ClaudeSettingsPath string
	// Environment 是仅传给当前进程的模型认证值，不写入 argv 或原生配置文件。
	Environment map[string]string
}

// PrepareNativeEnvironment 在受管账号隔离内读取原生插件状态，写入仅由原生管理命令完成。
func PrepareNativeEnvironment(parent string, spec NativeEnvironmentSpec) (NativeEnvironment, error) {
	result := NativeEnvironment{}
	if spec.RuntimeID != constants.RuntimeIDCodex && spec.RuntimeID != constants.RuntimeIDClaude {
		return result, status_error.PluginContentInvalid
	}
	if !filepath.IsAbs(spec.SharedPluginHome) || !filepath.IsAbs(spec.Executable) {
		return result, status_error.PluginContentInvalid
	}
	shared := filepath.Clean(spec.SharedPluginHome)
	if info, err := os.Lstat(shared); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return result, status_error.PluginContentInvalid
		}
	} else if !os.IsNotExist(err) {
		return result, storageFailure(err)
	}
	if spec.AccountHome != "" && ((codexPathWithin(shared, spec.AccountHome) && !nativeWorktreeDirectory(shared, spec.AccountHome)) || codexPathWithin(spec.AccountHome, shared)) {
		return result, status_error.PluginContentInvalid
	}
	configuration := map[string]any{}
	sourceConfiguration := map[string]any{}
	externalEnabled := true
	secretAuthStorage := runtime.GOOS == "windows"
	filename := "config.toml"
	keys := []string{"plugins", "marketplaces", "mcp_oauth_credentials_store", "mcp_oauth_callback_port", "mcp_oauth_callback_url"}
	if spec.RuntimeID == constants.RuntimeIDClaude {
		filename = "settings.json"
		keys = []string{"enabledPlugins", "extraKnownMarketplaces", "pluginConfigs"}
	}
	if spec.AllowExternal {
		source, err := nativeReadConfiguration(shared, filename)
		if err != nil {
			return result, err
		}
		sourceConfiguration = source
		for _, key := range keys {
			if value, exists := source[key]; exists {
				configuration[key] = value
			}
		}
		if features, ok := source["features"].(map[string]any); ok {
			if enabled, ok := features["plugins"].(bool); ok {
				externalEnabled = enabled
			}
			if enabled, ok := features["secret_auth_storage"].(bool); ok {
				secretAuthStorage = enabled
			}
		}
		configuration["neuxallyExternalPluginsEnabled"] = externalEnabled
		configuration["neuxallySecretAuthStorage"] = secretAuthStorage
		// 注册表和缓存内容变化也会改变能力；不只观察用户启用开关。
		stamp, err := nativePluginFilesFingerprint(filepath.Join(shared, "plugins"))
		if err != nil {
			return result, err
		}
		configuration["neuxallyExecutionFilesFingerprint"] = stamp
	}
	identity, err := json.Marshal([]any{shared, spec.RuntimeID, spec.PrefixArgs, configuration, spec.AllowExternal})
	if err != nil {
		return result, storageFailure(err)
	}
	digest := sha256.Sum256(identity)
	result.Fingerprint = hex.EncodeToString(digest[:])
	delete(configuration, "neuxallyExecutionFilesFingerprint")
	delete(configuration, "neuxallyExternalPluginsEnabled")
	delete(configuration, "neuxallySecretAuthStorage")
	apiConfiguration, apiEnvironment, apiAccount, err := nativeCodexAPIAccount(spec)
	if err != nil {
		return result, err
	}
	if apiAccount {
		spec.ModelEnvironmentKeys = append(spec.ModelEnvironmentKeys, constants.AIAccountCodexAPIKeyEnvironment)
	}
	if spec.RuntimeID == constants.RuntimeIDCodex && spec.AccountHome != "" && !apiAccount && spec.AllowExternal {
		effectiveSecret, effectiveStore, supported := nativeSubscriptionStorage(spec, sourceConfiguration, secretAuthStorage)
		if !supported {
			return result, status_error.PluginNotReady
		}
		secretAuthStorage = effectiveSecret
		configuration["mcp_oauth_credentials_store"] = effectiveStore
	}
	if spec.RuntimeID == constants.RuntimeIDCodex && spec.AccountHome != "" && !apiAccount && spec.AllowExternal && secretAuthStorage && configuration["mcp_oauth_credentials_store"] != "file" {
		// home 绑定的加密后端连首次授权也不能安全共享，不能等第二份身份已经创建才拒绝。
		return result, status_error.PluginNotReady
	}
	if spec.RuntimeID == constants.RuntimeIDCodex && (spec.AccountHome == "" || apiAccount) && spec.AllowExternal {
		// 原生普通执行无需派生模型认证；刷新、钥匙串和插件均沿 CLI 自己的原生环境。
		result.Home = shared
		result.CodexConfiguration = apiConfiguration
		result.Environment = apiEnvironment
		if len(spec.Targets) > 0 {
			managed, err := PrepareCodexHome(parent, "", spec.Targets)
			if err != nil {
				return result, err
			}
			fixedTargets, _ := json.Marshal(spec.Targets)
			targetDigest := sha256.Sum256(fixedTargets)
			result.Marketplace = constants.PluginNativeMarketplace + "-" + hex.EncodeToString(targetDigest[:16])
			destination := filepath.Join(shared, "plugins", "cache", result.Marketplace)
			source := filepath.Join(managed, "plugins", "cache", constants.PluginNativeMarketplace)
			if err := nativeCopyManagedCache(destination, source); err != nil {
				return result, err
			}
			if result.CodexConfiguration == nil {
				result.CodexConfiguration = map[string]string{}
			}
			result.CodexConfiguration["features.plugins"] = "true"
			pluginOverrides := map[string]bool{}
			for _, target := range spec.Targets {
				pluginOverrides[target.Name+"@"+result.Marketplace] = true
			}
			if !externalEnabled {
				if installed, ok := configuration["plugins"].(map[string]any); ok {
					for name := range installed {
						if _, exists := pluginOverrides[name]; exists {
							return result, status_error.PluginVersionConflict
						}
						pluginOverrides[name] = false
					}
				}
			}
			entries := make([]string, 0, len(pluginOverrides))
			for name, enabled := range pluginOverrides {
				entries = append(entries, strconv.Quote(name)+" = { enabled = "+strconv.FormatBool(enabled)+" }")
			}
			sort.Strings(entries)
			// CLI 的点路径按字符切分，不识别带引号的路径段；插件身份必须作为 TOML 表键传递。
			result.CodexConfiguration["plugins"] = "{" + strings.Join(entries, ", ") + "}"
			if spec.Workspace == "" {
				return result, status_error.PluginContentInvalid
			}
			result.CodexConfiguration["marketplaces."+result.Marketplace+".source_type"] = strconv.Quote("local")
			result.CodexConfiguration["marketplaces."+result.Marketplace+".source"] = strconv.Quote(spec.Workspace)
		}
	} else if spec.RuntimeID == constants.RuntimeIDCodex {
		account := spec.AccountHome
		if account == "" {
			if _, err := os.Stat(shared); err == nil {
				account = shared
			}
		}
		home, err := prepareCodexHome(parent, account, spec.Targets, spec.AccountHome != "")
		if err != nil {
			return result, err
		}
		result.Home = home
		config, err := nativeReadConfiguration(home, filename)
		if err != nil {
			return result, err
		}
		managed := config["plugins"]
		for key, value := range configuration {
			config[key] = value
		}
		installed, _ := config["plugins"].(map[string]any)
		if installed == nil {
			installed = map[string]any{}
		}
		if !externalEnabled {
			for name, item := range installed {
				settings, ok := item.(map[string]any)
				if !ok {
					return result, status_error.PluginContentInvalid
				}
				settings["enabled"] = false
				installed[name] = settings
			}
		}
		for name, value := range managed.(map[string]any) {
			if _, exists := installed[name]; exists {
				return result, status_error.PluginVersionConflict
			}
			installed[name] = value
		}
		config["plugins"] = installed
		config["features"] = map[string]any{"plugins": spec.AllowExternal && externalEnabled || len(spec.Targets) > 0, "secret_auth_storage": secretAuthStorage}
		if err := nativeWriteConfiguration(home, filename, config); err != nil {
			return result, err
		}
		if err := nativeShareCodexPlugins(home, shared, spec.AllowExternal); err != nil {
			return result, err
		}
		if err := nativeShareCodexCredentials(home, shared, spec.AllowExternal, configuration["mcp_oauth_credentials_store"] != "keyring"); err != nil {
			return result, err
		}
		if spec.AllowExternal {
			// 工具子进程也使用原生插件环境，绝对路径调用真实 CLI 不会落入账号私有安装表。
			policy, _ := config["shell_environment_policy"].(map[string]any)
			if policy == nil {
				policy = map[string]any{}
			}
			policy["set"] = map[string]string{"CODEX_HOME": shared}
			config["shell_environment_policy"] = policy
			if err := nativeWriteConfiguration(home, filename, config); err != nil {
				return result, err
			}
		}
	} else {
		result.Home = spec.AccountHome
		if spec.AllowExternal && spec.AccountHome != "" {
			// Claude 的受管账号只有 API 配置，原生 --settings 可独立覆盖模型，无需搬迁插件认证。
			account, err := nativeReadConfiguration(spec.AccountHome, "settings.json")
			if err != nil {
				return result, err
			}
			if environment, ok := account["env"].(map[string]any); ok {
				for key := range environment {
					spec.ModelEnvironmentKeys = append(spec.ModelEnvironmentKeys, key)
				}
			}
			result.ClaudeSettingsPath = filepath.Join(spec.AccountHome, "settings.json")
			result.Home = shared
		}
		if result.Home == "" {
			// 未选择受管模型账号时保留原生登录；受限执行仍使用独立目录避免插件隐式加载。
			if spec.AllowExternal {
				result.Home = shared
			} else {
				result.Home = filepath.Join(parent, "claude-restricted-"+result.Fingerprint)
			}
		}
		if result.Home != shared {
			if err := codexPrivateDir(result.Home); err != nil {
				return result, err
			}
			config, err := nativeReadConfiguration(result.Home, filename)
			if err != nil {
				return result, err
			}
			for _, key := range keys {
				delete(config, key)
			}
			for key, value := range configuration {
				config[key] = value
			}
			if environment, ok := config["env"].(map[string]any); ok {
				for key := range environment {
					spec.ModelEnvironmentKeys = append(spec.ModelEnvironmentKeys, key)
				}
			}
			if err := nativeWriteConfiguration(result.Home, filename, config); err != nil {
				return result, err
			}
			if err := nativeSharePath(filepath.Join(result.Home, "plugins"), filepath.Join(shared, "plugins"), spec.AllowExternal); err != nil {
				return result, err
			}
		}
	}
	if spec.AllowExternal {
		directory, err := nativeCommandDirectory(parent, spec)
		if err != nil {
			return result, err
		}
		result.CommandDirectory = directory
	}
	// 这是可重建的执行缓存，不是插件安装事实；共享原生配置始终是读取来源。
	stateDirectory := spec.StateDirectory
	if stateDirectory == "" && result.Home != shared {
		stateDirectory = result.Home
	}
	if stateDirectory != "" {
		if codexPathWithin(shared, stateDirectory) {
			workspaceScope := nativeWorktreeDirectory(shared, spec.Workspace) && filepath.Clean(stateDirectory) == filepath.Join(spec.Workspace, constants.RuntimeStateDirectoryName, constants.PluginNativeScopeDirectory)
			isolatedHome := spec.StateDirectory == "" && stateDirectory == result.Home && result.Home != shared
			if (!workspaceScope && !isolatedHome) || !nativeWorktreeDirectory(shared, stateDirectory) {
				return result, status_error.PluginContentInvalid
			}
		}
		if err := codexPrivateDir(stateDirectory); err != nil {
			return result, err
		}
		marker := filepath.Join(stateDirectory, ".neuxally-native-plugin-scope-"+spec.RuntimeID)
		previous, err := os.ReadFile(marker)
		if err != nil && !os.IsNotExist(err) {
			return result, storageFailure(err)
		}
		result.ScopeChanged = string(previous) != result.Fingerprint
		fingerprint := result.Fingerprint
		result.CommitScope = func() error {
			root, err := os.OpenRoot(stateDirectory)
			if err != nil {
				return storageFailure(err)
			}
			defer root.Close()
			return codexWritePrivate(root, filepath.Base(marker), []byte(fingerprint))
		}
	}
	return result, nil
}

// nativeWorktreeDirectory 区分原生配置与其 worktrees 下的独立工作区，拒绝中间链接和工作区集合根。
func nativeWorktreeDirectory(shared, directory string) bool {
	if !filepath.IsAbs(directory) {
		return false
	}
	worktrees := filepath.Join(shared, constants.NativeWorktreesDirectory)
	directory = filepath.Clean(directory)
	if directory == worktrees || !codexPathWithin(worktrees, directory) {
		return false
	}
	relative, err := filepath.Rel(shared, directory)
	if err != nil {
		return false
	}
	current := shared
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			// 后续目录由私有目录创建器建立；已有父链必须全部是真实目录。
			return true
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func nativeSubscriptionStorage(spec NativeEnvironmentSpec, source map[string]any, secret bool) (bool, string, bool) {
	// 隔离 home 无法忠实装载另一 home 的 profile 或管理层，不能猜测其认证后端。
	if profile, exists := source["profile"]; exists && profile != "" {
		return false, "", false
	}
	if profiles, exists := source["profiles"]; exists {
		table, ok := profiles.(map[string]any)
		if !ok || len(table) > 0 {
			return false, "", false
		}
	}
	if features, exists := source["features"]; exists {
		table, ok := features.(map[string]any)
		if !ok {
			return false, "", false
		}
		if value, exists := table["secret_auth_storage"]; exists {
			switch setting := value.(type) {
			case bool:
				secret = setting
			case map[string]any:
				var ok bool
				secret, ok = setting["enabled"].(bool)
				if !ok {
					return false, "", false
				}
			default:
				return false, "", false
			}
		}
	}
	managedPaths := []string{filepath.Join(spec.SharedPluginHome, "requirements.toml"), filepath.Join(spec.SharedPluginHome, "managed_config.toml"), "/etc/codex/config.toml", "/etc/codex/requirements.toml"}
	if runtime.GOOS == "windows" {
		if directory := os.Getenv("ProgramData"); directory != "" {
			managedPaths = append(managedPaths, filepath.Join(directory, "OpenAI", "Codex", "config.toml"), filepath.Join(directory, "OpenAI", "Codex", "requirements.toml"))
		}
	}
	for _, path := range managedPaths {
		if _, err := os.Stat(path); err == nil || !os.IsNotExist(err) {
			return false, "", false
		}
	}
	store := "auto"
	if value, exists := source["mcp_oauth_credentials_store"]; exists {
		var ok bool
		store, ok = value.(string)
		if !ok {
			return false, "", false
		}
	}
	var enableSecret, disableSecret bool
	for index := 0; index < len(spec.PrefixArgs); index++ {
		argument := spec.PrefixArgs[index]
		option, value, inline := strings.Cut(argument, "=")
		if strings.HasPrefix(argument, "-c") && !strings.HasPrefix(argument, "--") && argument != "-c" {
			option, value, inline = "-c", argument[2:], true
		}
		if option == "-p" || option == "--profile" || strings.HasPrefix(option, "-p") {
			return false, "", false
		}
		if option != "--enable" && option != "--disable" && option != "-c" && option != "--config" {
			continue
		}
		if !inline {
			index++
			if index >= len(spec.PrefixArgs) {
				return false, "", false
			}
			value = spec.PrefixArgs[index]
		}
		if option == "--enable" || option == "--disable" {
			for _, feature := range strings.Split(value, ",") {
				if feature == "secret_auth_storage" {
					if option == "--enable" {
						enableSecret = true
					} else {
						disableSecret = true
					}
				}
			}
			continue
		}
		key, raw, exists := strings.Cut(value, "=")
		if !exists {
			return false, "", false
		}
		parsed := map[string]any{}
		if _, err := toml.Decode("value = "+raw, &parsed); err != nil {
			return false, "", false
		}
		switch strings.TrimSpace(key) {
		case "features.secret_auth_storage":
			var ok bool
			secret, ok = parsed["value"].(bool)
			if !ok {
				return false, "", false
			}
		case "mcp_oauth_credentials_store":
			var ok bool
			store, ok = parsed["value"].(string)
			if !ok {
				return false, "", false
			}
		default:
			// 未完整解析的配置可改变默认 profile 或认证路由，仅隔离订阅分支准确拒绝。
			return false, "", false
		}
	}
	// Codex 先合并全部配置覆盖，再合并 enable，最后合并 disable，与参数出现顺序无关。
	if enableSecret {
		secret = true
	}
	if disableSecret {
		secret = false
	}
	if store != "auto" && store != "file" && store != "keyring" {
		return false, "", false
	}
	return secret, store, true
}

func nativeCodexAPIAccount(spec NativeEnvironmentSpec) (map[string]string, map[string]string, bool, error) {
	if spec.RuntimeID != constants.RuntimeIDCodex || spec.AccountHome == "" || !spec.AllowExternal {
		return nil, nil, false, nil
	}
	auth, err := nativeReadConfiguration(spec.AccountHome, "auth.json")
	if err != nil {
		return nil, nil, false, err
	}
	key, _ := auth["OPENAI_API_KEY"].(string)
	if key == "" {
		return nil, nil, false, nil
	}
	config, err := nativeReadConfiguration(spec.AccountHome, "config.toml")
	if err != nil {
		return nil, nil, false, err
	}
	provider, _ := config["model_provider"].(string)
	providers, _ := config["model_providers"].(map[string]any)
	settings, _ := providers[provider].(map[string]any)
	if !configurationKeyPattern.MatchString(provider) || settings == nil {
		return nil, nil, false, status_error.AIAccountConfigurationInvalid
	}
	accountIdentity := sha256.Sum256([]byte(spec.AccountHome))
	runtimeProvider := "neuxally-account-" + hex.EncodeToString(accountIdentity[:16])
	sharedConfig, err := nativeReadConfiguration(spec.SharedPluginHome, "config.toml")
	if err != nil {
		return nil, nil, false, err
	}
	if existing, ok := sharedConfig["model_providers"].(map[string]any); ok {
		if _, exists := existing[runtimeProvider]; exists {
			return nil, nil, false, status_error.PluginConfigurationConflict
		}
	}
	overrides := map[string]string{}
	put := func(name string, value any) error {
		// 受管账号只声明标量模型配置；不把可能含凭据的任意对象编码为启动参数。
		switch typed := value.(type) {
		case string:
			overrides[name] = strconv.Quote(typed)
		case bool:
			overrides[name] = strconv.FormatBool(typed)
		case int64:
			overrides[name] = strconv.FormatInt(typed, 10)
		case float64:
			overrides[name] = strconv.FormatFloat(typed, 'g', -1, 64)
		default:
			return status_error.AIAccountConfigurationInvalid
		}
		return nil
	}
	for _, name := range []string{"model", "model_provider", "model_reasoning_effort", "model_reasoning_summary", "model_verbosity", "model_context_window", "model_auto_compact_token_limit", "model_catalog_json", "disable_response_storage"} {
		if value, ok := config[name]; ok {
			if err := put(name, value); err != nil {
				return nil, nil, false, err
			}
		}
	}
	overrides["model_provider"] = strconv.Quote(runtimeProvider)
	prefix := "model_providers." + runtimeProvider + "."
	for _, name := range []string{"name", "base_url", "wire_api", "supports_websockets", "request_max_retries", "stream_max_retries", "stream_idle_timeout_ms"} {
		if value, ok := settings[name]; ok {
			if err := put(prefix+name, value); err != nil {
				return nil, nil, false, err
			}
		}
	}
	overrides[prefix+"env_key"] = strconv.Quote(constants.AIAccountCodexAPIKeyEnvironment)
	overrides[prefix+"requires_openai_auth"] = "false"
	excluded := []string{constants.AIAccountCodexAPIKeyEnvironment}
	if policy, ok := sharedConfig["shell_environment_policy"].(map[string]any); ok {
		if values, ok := policy["exclude"].([]any); ok {
			for _, value := range values {
				if text, ok := value.(string); ok {
					excluded = append(excluded, text)
				} else {
					return nil, nil, false, status_error.AIAccountConfigurationInvalid
				}
			}
		}
	}
	sort.Strings(excluded)
	encoded, _ := json.Marshal(excluded)
	overrides["shell_environment_policy.exclude"] = string(encoded)
	return overrides, map[string]string{constants.AIAccountCodexAPIKeyEnvironment: key}, true, nil
}

func nativeCopyManagedCache(destination, source string) error {
	expected, err := codexPackageFingerprint(source)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		actual, err := codexPackageFingerprint(destination)
		if err != nil {
			return err
		}
		if actual != expected {
			return status_error.PluginVersionConflict
		}
		return nil
	} else if !os.IsNotExist(err) {
		return storageFailure(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return storageFailure(err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".neuxally-cache-")
	if err != nil {
		return storageFailure(err)
	}
	defer os.RemoveAll(stage)
	if err := codexCopyPackage(source, filepath.Join(stage, "content")); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(stage, "content"), destination); err != nil {
		// 另一聊天可并发发布同一不可变摘要；只能复用内容完全一致的成品。
		actual, verifyErr := codexPackageFingerprint(destination)
		if verifyErr == nil && actual == expected {
			return nil
		}
		return storageFailure(err)
	}
	return nil
}

func nativeReadConfiguration(home, filename string) (map[string]any, error) {
	config := map[string]any{}
	path := filepath.Join(home, filename)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return config, nil
	}
	if err != nil {
		return nil, storageFailure(err)
	}
	if !info.Mode().IsRegular() {
		return nil, status_error.AIAccountConfigurationInvalid
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, storageFailure(err)
	}
	if filename == "config.toml" {
		if _, err := toml.Decode(string(data), &config); err != nil {
			return nil, codexAccountFailure(err)
		}
	} else if err := json.Unmarshal(data, &config); err != nil {
		return nil, codexAccountFailure(err)
	}
	if config == nil {
		return nil, status_error.AIAccountConfigurationInvalid
	}
	return config, nil
}

func nativeWriteConfiguration(home, filename string, config map[string]any) error {
	var buffer bytes.Buffer
	if filename == "config.toml" {
		if err := toml.NewEncoder(&buffer).Encode(config); err != nil {
			return codexAccountFailure(err)
		}
	} else {
		if err := json.NewEncoder(&buffer).Encode(config); err != nil {
			return codexAccountFailure(err)
		}
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return storageFailure(err)
	}
	defer root.Close()
	return codexWritePrivate(root, filename, buffer.Bytes())
}

func nativeShareCodexPlugins(home, shared string, allowed bool) error {
	root := filepath.Join(home, "plugins")
	// 外部缓存只做目录引用；自有包继续保存在执行目录的独占 marketplace 中。
	for _, level := range []string{"", "cache"} {
		destination := filepath.Join(root, level)
		entries, err := os.ReadDir(destination)
		if err != nil && !os.IsNotExist(err) {
			return storageFailure(err)
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			if err := os.Remove(filepath.Join(destination, entry.Name())); err != nil {
				return storageFailure(err)
			}
		}
		if !allowed {
			continue
		}
		source := filepath.Join(shared, "plugins", level)
		entries, err = os.ReadDir(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return storageFailure(err)
		}
		for _, entry := range entries {
			if level == "" && entry.Name() == "cache" {
				continue
			}
			if level == "cache" && (entry.Name() == constants.PluginNativeMarketplace || strings.HasPrefix(entry.Name(), constants.PluginNativeMarketplace+"-")) {
				continue
			}
			if err := nativeSharePath(filepath.Join(destination, entry.Name()), filepath.Join(source, entry.Name()), true); err != nil {
				return err
			}
		}
	}
	return nil
}

func nativeSharePath(destination, source string, allowed bool) error {
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return status_error.PluginContentInvalid
		}
		if allowed {
			if target, err := os.Readlink(destination); err == nil && target == source {
				return nil
			}
		}
		if err := os.Remove(destination); err != nil {
			return storageFailure(err)
		}
	} else if !os.IsNotExist(err) {
		return storageFailure(err)
	}
	if !allowed {
		return nil
	}
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return storageFailure(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return storageFailure(err)
	}
	if err := os.Symlink(source, destination); err != nil {
		return storageFailure(err)
	}
	return nil
}

func nativeShareCodexCredentials(home, shared string, allowed, fileStore bool) error {
	destination := filepath.Join(home, ".credentials.json")
	source := filepath.Join(shared, ".credentials.json")
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return status_error.PluginContentInvalid
		}
		if allowed {
			if original, err := os.Lstat(source); err == nil && original.Mode().IsRegular() && os.SameFile(info, original) {
				return nil
			}
		}
		if err := os.Remove(destination); err != nil {
			return storageFailure(err)
		}
	} else if !os.IsNotExist(err) {
		return storageFailure(err)
	}
	if !allowed {
		return nativeSharePath(filepath.Join(home, "mcp-oauth-locks"), filepath.Join(shared, "mcp-oauth-locks"), false)
	}
	info, err := os.Lstat(source)
	if os.IsNotExist(err) && fileStore {
		// file 后端首次登录也必须写同一文件，不能等私有 CLI 创建后再复制凭据。
		if err := os.MkdirAll(shared, 0700); err != nil {
			return storageFailure(err)
		}
		file, createErr := os.OpenFile(source, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			_, createErr = file.Write([]byte("{}"))
			closeErr := file.Close()
			if createErr == nil {
				createErr = closeErr
			}
		}
		if createErr != nil && !os.IsExist(createErr) {
			return storageFailure(createErr)
		}
		info, err = os.Lstat(source)
	}
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return storageFailure(err)
	}
	if !info.Mode().IsRegular() {
		return status_error.PluginContentInvalid
	}
	// 0.161.0 文件存储原地写入并拒绝符号链接；硬链接保留同一凭据事实，跨卷直接失败。
	if err := os.Link(source, destination); err != nil {
		return storageFailure(err)
	}
	if err := os.MkdirAll(filepath.Join(shared, "mcp-oauth-locks"), 0700); err != nil {
		return storageFailure(err)
	}
	return nativeSharePath(filepath.Join(home, "mcp-oauth-locks"), filepath.Join(shared, "mcp-oauth-locks"), true)
}

func nativeCommandDirectory(parent string, spec NativeEnvironmentSpec) (string, error) {
	keys := append([]string(nil), spec.ModelEnvironmentKeys...)
	sort.Strings(keys)
	identity, err := json.Marshal([]any{spec.RuntimeID, spec.SharedPluginHome, spec.Executable, spec.PrefixArgs, keys})
	if err != nil {
		return "", storageFailure(err)
	}
	digest := sha256.Sum256(identity)
	directory := filepath.Join(parent, "native-commands-"+hex.EncodeToString(digest[:]))
	if err := codexPrivateDir(directory); err != nil {
		return "", err
	}
	variable := "CODEX_HOME"
	if spec.RuntimeID == constants.RuntimeIDClaude {
		variable = "CLAUDE_CONFIG_DIR"
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	var script strings.Builder
	script.WriteString("#!/bin/sh\nnative_route=\nnative_skip=\nfor native_arg do\nif [ -n \"$native_skip\" ]; then native_skip=; continue; fi\ncase \"$native_arg\" in\nplugin|plugins|mcp) native_route=1; break;;\n-c|--config|-p|--profile|--enable|--disable|--settings|--setting-sources) native_skip=1;;\n-*) ;;\n*) break;;\nesac\ndone\nif [ -n \"$native_route\" ]; then\n")
	for _, key := range keys {
		if !configurationKeyPattern.MatchString(key) {
			return "", status_error.PluginContentInvalid
		}
		script.WriteString("unset " + key + "\n")
	}
	script.WriteString("export " + variable + "=" + quote(spec.SharedPluginHome) + "\nfi\nexec " + quote(spec.Executable))
	for _, argument := range spec.PrefixArgs {
		script.WriteString(" " + quote(argument))
	}
	script.WriteString(" \"$@\"\n")
	if runtime.GOOS == "windows" {
		return nativeWindowsCommand(directory, variable, spec, keys)
	}
	if err := nativeWriteCommand(directory, spec.RuntimeID, []byte(script.String()), 0700); err != nil {
		return "", err
	}
	return directory, nil
}

func nativePluginFilesFingerprint(root string) (string, error) {
	digest := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return storageFailure(err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return storageFailure(err)
		}
		// 自有派生缓存不属于外部能力事实，避免准备自身缓存反复改变会话指纹。
		if relative == "." || relative == "cache" {
			return nil
		}
		if relative == filepath.Join("cache", constants.PluginNativeMarketplace) || strings.HasPrefix(relative, filepath.Join("cache", constants.PluginNativeMarketplace+"-")) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return storageFailure(err)
		}
		fmt.Fprintf(digest, "%s:%d:%d:%d\n", relative, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return storageFailure(err)
			}
			digest.Write([]byte(target))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func nativeWindowsCommand(directory, variable string, spec NativeEnvironmentSpec, keys []string) (string, error) {
	quote := func(value string) (string, bool) {
		if strings.ContainsAny(value, "\"\r\n") {
			return "", false
		}
		return strings.ReplaceAll(value, "%", "%%"), true
	}
	binary, valid := quote(spec.Executable)
	if !valid {
		return "", status_error.PluginContentInvalid
	}
	shared, valid := quote(spec.SharedPluginHome)
	if !valid {
		return "", status_error.PluginContentInvalid
	}
	var script strings.Builder
	script.WriteString("@echo off\r\nsetlocal DisableDelayedExpansion\r\n:scan\r\nif \"%~1\"==\"\" goto execute\r\nif \"%~1\"==\"plugin\" goto native\r\nif \"%~1\"==\"plugins\" goto native\r\nif \"%~1\"==\"mcp\" goto native\r\n")
	for _, option := range []string{"-c", "--config", "-p", "--profile", "--enable", "--disable", "--settings", "--setting-sources"} {
		script.WriteString("if \"%~1\"==\"" + option + "\" goto skipvalue\r\n")
	}
	script.WriteString("set \"native_arg=%~1\"\r\nif not \"%native_arg:~0,1%\"==\"-\" goto execute\r\nshift\r\ngoto scan\r\n:skipvalue\r\nshift\r\nshift\r\ngoto scan\r\n:native\r\n")
	for _, key := range keys {
		if !configurationKeyPattern.MatchString(key) {
			return "", status_error.PluginContentInvalid
		}
		script.WriteString("set \"" + key + "=\"\r\n")
	}
	script.WriteString("set \"" + variable + "=" + shared + "\"\r\n:execute\r\n\"" + binary + "\"")
	for _, argument := range spec.PrefixArgs {
		value, valid := quote(argument)
		if !valid {
			return "", status_error.PluginContentInvalid
		}
		script.WriteString(" \"" + value + "\"")
	}
	script.WriteString(" %*\r\nexit /b %errorlevel%\r\n")
	if err := nativeWriteCommand(directory, spec.RuntimeID+".cmd", []byte(script.String()), 0600); err != nil {
		return "", err
	}
	return directory, nil
}

func nativeWriteCommand(directory, name string, content []byte, mode os.FileMode) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return storageFailure(err)
	}
	defer root.Close()
	if err := codexWritePrivate(root, name, content); err != nil {
		return err
	}
	if err := root.Chmod(name, mode); err != nil {
		return storageFailure(err)
	}
	return nil
}
