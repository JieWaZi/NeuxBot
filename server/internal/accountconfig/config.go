// Package accountconfig 生成受管账号的 CLI 配置并隔离继承的认证环境。
package accountconfig

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// Spec 是配置生成需要的最小账号快照。
type Spec struct {
	// Protocol 是账号声明的原生请求协议。
	Protocol constants.AccountProtocol
	// ID 是受管账号的路径安全标识。
	ID string
	// RuntimeID 是配置对应的内置智能体。
	RuntimeID string
	// AuthMode 是账号使用的认证方式。
	AuthMode constants.AccountAuthMode
	// BaseURL 是用户选择的服务地址。
	BaseURL string
	// APIKey 是只在执行准备内存中使用的解密凭据。
	APIKey string
	// Model 是当前调用选择的模型。
	Model string
	// MaxContextSize 是 Kimi 自定义模型的上下文窗口，零值使用 256K 默认值。
	MaxContextSize int
	// SelectedModels 是本账号在聊天中开放的实际请求模型 ID。
	SelectedModels []string
	// ModelContextSizes 保存各实际模型 ID 的上下文窗口。
	ModelContextSizes map[string]int
	// ModelDisplayNames 保存 Codex 菜单和聊天的模型显示名称。
	ModelDisplayNames map[string]string
	// ModelReasoningLevels 保存各模型支持的原生推理等级。
	ModelReasoningLevels map[string][]string
	// ClaudeModelRoles 保存 Claude Code 各内置角色的实际模型映射。
	ClaudeModelRoles map[string]string
	// Revision 隔离修改配置后的启动指纹。
	Revision int64
}

// Supports 声明原生配置可直接表达的协议，不用于限制账号关联。
func Supports(runtimeID string, protocol constants.AccountProtocol) bool {
	switch runtimeID {
	case constants.RuntimeIDCodex:
		return protocol == constants.AccountProtocolOpenAIResponses
	case constants.RuntimeIDClaude:
		return protocol == constants.AccountProtocolAnthropicMessages
	case constants.RuntimeIDGemini:
		return protocol == constants.AccountProtocolGoogleGenerativeAI
	case constants.RuntimeIDGrok:
		return protocol == constants.AccountProtocolOpenAIResponses || protocol == constants.AccountProtocolOpenAICompletions
	case constants.RuntimeIDOpenCode:
		return protocol == constants.AccountProtocolGoogleGenerativeAI || protocol == constants.AccountProtocolOpenAIResponses || protocol == constants.AccountProtocolOpenAICompletions || protocol == constants.AccountProtocolAnthropicMessages
	case constants.RuntimeIDPi:
		return protocol == constants.AccountProtocolOpenAIResponses || protocol == constants.AccountProtocolOpenAICompletions || protocol == constants.AccountProtocolAnthropicMessages
	case constants.RuntimeIDKimi:
		return protocol == constants.AccountProtocolOpenAIResponses || protocol == constants.AccountProtocolOpenAICompletions || protocol == constants.AccountProtocolAnthropicMessages
	default:
		return false
	}
}

// ValidateEndpoint 校验服务地址格式，不根据智能体限制账号。
func ValidateEndpoint(protocol constants.AccountProtocol, baseURL string) error {
	switch protocol {
	case constants.AccountProtocolOpenAIResponses, constants.AccountProtocolOpenAICompletions, constants.AccountProtocolAnthropicMessages, constants.AccountProtocolGoogleGenerativeAI:
	default:
		return status_error.AIAccountConfigurationInvalid
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return status_error.AIAccountConfigurationInvalid
	}
	return nil
}

// Environment 去掉宿主进程的模型服务凭据及配置覆盖，保留普通工具运行环境。
func Environment(base []string) []string {
	result := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if upper == "PI_MCP_MODULE_PATH" || upper == "GEMINI_RESTRICTED_MODE" || upper == "GEMINI_CLI_TRUST_WORKSPACE" || upper == "GEMINI_CLI_SYSTEM_SETTINGS_PATH" || upper == "GEMINI_CLI_SYSTEM_DEFAULTS_PATH" {
			result = append(result, entry)
			continue
		}
		if strings.HasSuffix(upper, "_API_KEY") || strings.HasSuffix(upper, "_AUTH_TOKEN") ||
			strings.HasSuffix(upper, "_BASE_URL") || strings.HasPrefix(upper, "CODEX_") ||
			strings.HasPrefix(upper, "CLAUDE_") || strings.HasPrefix(upper, "ANTHROPIC_") ||
			strings.HasPrefix(upper, "OPENAI_") || strings.HasPrefix(upper, "KIMI_") ||
			strings.HasPrefix(upper, "CURSOR_") || strings.HasPrefix(upper, "PI_") ||
			strings.HasPrefix(upper, "ALLY_ACCOUNT_") || strings.HasPrefix(upper, "GEMINI_") ||
			strings.HasPrefix(upper, "GOOGLE_") || strings.HasPrefix(upper, "GROK_") ||
			strings.HasPrefix(upper, "XAI_") || strings.HasPrefix(upper, "OPENCODE_") ||
			slices.Contains([]string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"}, upper) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

// Prepare 把所选账号投射到受管 workspace 的原生配置。
func Prepare(workspace string, spec Spec) (map[string]string, error) {
	if IsNativeAccountRuntime(spec.RuntimeID) {
		return prepareNative(workspace, spec)
	}
	home, err := EnsureHome(workspace, spec.RuntimeID)
	if err != nil {
		return nil, err
	}
	env := HomeEnvironment(spec.RuntimeID, home)
	env[constants.AIAccountIdentityEnvironment] = spec.ID + ":" + strconv.FormatInt(spec.Revision, 10)
	if spec.AuthMode == constants.AccountAuthModeSubscription {
		if spec.RuntimeID != constants.RuntimeIDCodex {
			return nil, status_error.AIAccountConfigurationInvalid
		}
		if err := WriteFile(filepath.Join(home, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n")); err != nil {
			return nil, err
		}
		return env, nil
	}
	var filename string
	var content []byte
	switch spec.RuntimeID {
	case constants.RuntimeIDCodex:
		filename = "config.toml"
		content = []byte(fmt.Sprintf("cli_auth_credentials_store = \"file\"\nmodel = %s\nmodel_provider = \"custom\"\ndisable_response_storage = true\n[model_providers.custom]\nname = \"NeuxBot\"\nbase_url = %s\nwire_api = \"responses\"\nrequires_openai_auth = true\n", strconv.Quote(spec.Model), strconv.Quote(spec.BaseURL)))
		if spec.Model != "" {
			catalog, err := codexCatalog(spec)
			if err != nil {
				return nil, storageError(err)
			}
			catalogPath := filepath.Join(home, "models.json")
			if err := WriteFile(catalogPath, catalog); err != nil {
				return nil, err
			}
			if isOfficialDeepSeekEndpoint(spec.BaseURL) {
				content = []byte(fmt.Sprintf("cli_auth_credentials_store = \"file\"\nmodel = %s\nmodel_provider = \"custom\"\nmodel_catalog_json = %s\npreferred_auth_method = \"apikey\"\nforced_login_method = \"api\"\ndisable_response_storage = true\n[model_providers.custom]\nname = \"DeepSeek\"\nbase_url = %s\nwire_api = \"responses\"\nexperimental_bearer_token = %s\n", strconv.Quote(spec.Model), strconv.Quote(catalogPath), strconv.Quote(spec.BaseURL), strconv.Quote(spec.APIKey)))
			} else {
				content = []byte(fmt.Sprintf("cli_auth_credentials_store = \"file\"\nmodel = %s\nmodel_provider = \"custom\"\nmodel_catalog_json = %s\ndisable_response_storage = true\n[model_providers.custom]\nname = \"NeuxBot\"\nbase_url = %s\nwire_api = \"responses\"\nrequires_openai_auth = true\n", strconv.Quote(spec.Model), strconv.Quote(catalogPath), strconv.Quote(spec.BaseURL)))
			}
		}
		auth, _ := json.Marshal(map[string]string{"OPENAI_API_KEY": spec.APIKey})
		if err := WriteFile(filepath.Join(home, "auth.json"), auth); err != nil {
			return nil, err
		}
	case constants.RuntimeIDClaude:
		filename = "settings.json"
		keyName := "ANTHROPIC_AUTH_TOKEN"
		// OpenCode Go 预设使用 API Key 请求头，网关不接受 Bearer。
		switch strings.TrimRight(spec.BaseURL, "/") {
		case constants.AIAccountAnthropicURL, constants.AIAccountOpenCodeGoAnthropicURL:
			keyName = "ANTHROPIC_API_KEY"
		}
		claudeEnvironment := map[string]string{keyName: spec.APIKey, "ANTHROPIC_BASE_URL": spec.BaseURL}
		if spec.Model != "" {
			claudeEnvironment["ANTHROPIC_MODEL"] = spec.Model
			for role, key := range map[string]string{"opus": "ANTHROPIC_DEFAULT_OPUS_MODEL", "sonnet": "ANTHROPIC_DEFAULT_SONNET_MODEL", "haiku": "ANTHROPIC_DEFAULT_HAIKU_MODEL", "fable": "ANTHROPIC_DEFAULT_FABLE_MODEL", "subagent": "CLAUDE_CODE_SUBAGENT_MODEL"} {
				mapped := spec.ClaudeModelRoles[role]
				if mapped == "" || !slices.Contains(spec.SelectedModels, mapped) {
					mapped = spec.Model
				}
				claudeEnvironment[key] = mapped
				if name := spec.ModelDisplayNames[mapped]; name != "" && role != "subagent" {
					claudeEnvironment[key+"_NAME"] = name
				}
			}
		}
		content, _ = json.Marshal(map[string]any{"env": claudeEnvironment})
	case constants.RuntimeIDPi:
		// 账号模型始终使用服务端原始 ID；适配器通过独立的 provider 字段选择原生模型。
		if strings.TrimSpace(spec.Model) == "" {
			return nil, status_error.AIAccountConfigurationInvalid
		}
		filename = "models.json"
		content, _ = json.Marshal(map[string]any{"providers": map[string]any{"ally": map[string]any{"name": "NeuxBot", "baseUrl": spec.BaseURL, "api": string(spec.Protocol), "apiKey": spec.APIKey, "models": []map[string]string{{"id": spec.Model}}}}})
		env["ACP_GO_PI_PREFERRED_PROVIDER"] = "ally"
	case constants.RuntimeIDKimi:
		if strings.TrimSpace(spec.Model) == "" {
			return nil, status_error.AIAccountConfigurationInvalid
		}
		providerType := map[constants.AccountProtocol]string{
			constants.AccountProtocolOpenAICompletions: "openai",
			constants.AccountProtocolOpenAIResponses:   "openai_responses",
			constants.AccountProtocolAnthropicMessages: "anthropic",
		}[spec.Protocol]
		if providerType == "openai" && strings.TrimRight(spec.BaseURL, "/") == constants.AIAccountKimiURL {
			providerType = "kimi"
		}
		if providerType == "" {
			return nil, status_error.AIAccountConfigurationInvalid
		}
		contextSize := spec.MaxContextSize
		if contextSize < 1 {
			contextSize = 262144
		}
		env["KIMI_MODEL_MAX_COMPLETION_TOKENS"] = strconv.Itoa(constants.AIAccountKimiCompletionTokenCap)
		filename = "config.toml"
		content = []byte(fmt.Sprintf("default_model = %s\n[providers.ally]\ntype = %s\nbase_url = %s\napi_key = %s\n[models.%s]\nprovider = \"ally\"\nmodel = %s\nmax_context_size = %d\n",
			strconv.Quote(spec.Model), strconv.Quote(providerType), strconv.Quote(spec.BaseURL), strconv.Quote(spec.APIKey), strconv.Quote(spec.Model), strconv.Quote(spec.Model), contextSize))
		levels := kimiReasoningLevels(spec)
		if len(levels) > 0 {
			efforts := make([]string, 0, len(levels))
			for _, level := range levels {
				efforts = append(efforts, strconv.Quote(level))
			}
			content = append(content, []byte("capabilities = [\"thinking\"]\nsupport_efforts = ["+strings.Join(efforts, ", ")+"]\ndefault_effort = "+strconv.Quote(levels[0])+"\n")...)
		}
	default:
		return nil, status_error.AIAccountConfigurationInvalid
	}
	if err := WriteFile(filepath.Join(home, filename), content); err != nil {
		return nil, err
	}
	return env, nil
}

// Home 返回聊天独有的原生配置目录，不因账号切换改动 CLI 的会话存储位置。
func Home(workspace, runtimeID string) string {
	return filepath.Join(workspace, constants.RuntimeStateDirectoryName, "accounts", runtimeID)
}

// WriteFile 以当前用户权限原子替换一份原生配置，失败保留已有文件。
func WriteFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "account-config-*")
	if err != nil {
		return storageError(err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return storageError(err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return storageError(err)
	}
	if err := file.Close(); err != nil {
		return storageError(err)
	}
	if err := os.Rename(name, path); err != nil {
		return storageError(err)
	}
	return nil
}

// HomeEnvironment 返回各 CLI 的受管配置目录映射，不修改系统 HOME。
func HomeEnvironment(runtimeID, home string) map[string]string {
	switch runtimeID {
	case constants.RuntimeIDCodex:
		return map[string]string{"CODEX_HOME": home}
	case constants.RuntimeIDClaude:
		return map[string]string{"CLAUDE_CONFIG_DIR": home}
	case constants.RuntimeIDPi:
		return map[string]string{"PI_CODING_AGENT_DIR": home}
	case constants.RuntimeIDKimi:
		return map[string]string{"KIMI_CODE_HOME": home}
	default:
		return map[string]string{}
	}
}

func storageError(err error) error {
	logx.Errorf("prepare AI account configuration failed: error=%v", err)
	return status_error.AIAccountWorkspaceError
}

// EnsureHome 拒绝受管目录中的符号链接，避免写入用户系统配置。
func EnsureHome(workspace, runtimeID string) (string, error) {
	switch runtimeID {
	case constants.RuntimeIDCodex, constants.RuntimeIDClaude, constants.RuntimeIDPi, constants.RuntimeIDKimi, constants.RuntimeIDOpenCode, constants.RuntimeIDGemini, constants.RuntimeIDGrok:
	default:
		return "", status_error.AIAccountConfigurationInvalid
	}
	directory := workspace
	for _, part := range []string{constants.RuntimeStateDirectoryName, "accounts", runtimeID} {
		directory = filepath.Join(directory, part)
		if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
			return "", storageError(err)
		}
		info, err := os.Lstat(directory)
		if err != nil {
			return "", storageError(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.AIAccountWorkspaceError
		}
		if err := os.Chmod(directory, 0700); err != nil {
			return "", storageError(err)
		}
	}
	return directory, nil
}
