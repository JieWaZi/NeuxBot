package accountconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// IsNativeAccountRuntime 标识必须按真实选中模型探测能力的新原生运行时。
func IsNativeAccountRuntime(runtimeID string) bool {
	return runtimeID == constants.RuntimeIDOpenCode || runtimeID == constants.RuntimeIDGemini || runtimeID == constants.RuntimeIDGrok
}

// NativeModelID 将账号原始请求 ID 转为 OpenCode 的受管服务商标识。
func NativeModelID(runtimeID, model string) string {
	if runtimeID == constants.RuntimeIDOpenCode && model != "" {
		return constants.AIAccountOpenCodeProvider + "/" + model
	}
	return model
}

// AccountModelID 仅剥离 NeuxBot 自己创建的 OpenCode 命名空间，保留原始服务端 ID。
func AccountModelID(runtimeID, model string) string {
	if runtimeID == constants.RuntimeIDOpenCode {
		return strings.TrimPrefix(model, constants.AIAccountOpenCodeProvider+"/")
	}
	return model
}

// GoogleEndpoint 区分 Gemini SDK 的根地址与 OpenCode SDK 的版本化地址。
func GoogleEndpoint(endpoint string) (string, string) {
	base := strings.TrimRight(endpoint, "/")
	for _, version := range []string{"v1beta", "v1"} {
		if strings.HasSuffix(base, "/"+version) {
			return strings.TrimSuffix(base, "/"+version), version
		}
	}
	return base, "v1beta"
}

// privateSubdirectory 逐层拒绝链接并建立仅当前用户可读写的受管子目录。
func privateSubdirectory(root string, parts ...string) (string, error) {
	directory := root
	for _, part := range parts {
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

// prepareNative 按账号身份与修订固定原生配置和持久缓存，避免切换账号复用旧状态。
func prepareNative(workspace string, spec Spec) (map[string]string, error) {
	if spec.AuthMode != constants.AccountAuthModeAPIKey || !Supports(spec.RuntimeID, spec.Protocol) || strings.TrimSpace(spec.Model) == "" {
		return nil, status_error.AIAccountConfigurationInvalid
	}
	if err := ValidateEndpoint(spec.Protocol, spec.BaseURL); err != nil {
		return nil, err
	}
	root, err := EnsureHome(workspace, spec.RuntimeID)
	if err != nil {
		return nil, err
	}
	identity := spec.ID + ":" + strconv.FormatInt(spec.Revision, 10)
	digest := sha256.Sum256([]byte(identity))
	home, err := privateSubdirectory(root, hex.EncodeToString(digest[:16]))
	if err != nil {
		return nil, err
	}
	env := map[string]string{constants.AIAccountIdentityEnvironment: identity}
	switch spec.RuntimeID {
	case constants.RuntimeIDOpenCode:
		for key, directory := range map[string]string{"XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state"} {
			path, err := privateSubdirectory(home, directory)
			if err != nil {
				return nil, err
			}
			env[key] = path
		}
		npm := map[constants.AccountProtocol]string{constants.AccountProtocolOpenAIResponses: "@ai-sdk/openai", constants.AccountProtocolOpenAICompletions: "@ai-sdk/openai-compatible", constants.AccountProtocolAnthropicMessages: "@ai-sdk/anthropic", constants.AccountProtocolGoogleGenerativeAI: "@ai-sdk/google"}[spec.Protocol]
		endpoint := strings.TrimRight(spec.BaseURL, "/")
		if spec.Protocol == constants.AccountProtocolGoogleGenerativeAI {
			base, version := GoogleEndpoint(endpoint)
			endpoint = base + "/" + version
		}
		if spec.Protocol == constants.AccountProtocolAnthropicMessages && !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		models := map[string]any{}
		for _, id := range spec.SelectedModels {
			model := map[string]any{"name": id}
			if name := spec.ModelDisplayNames[id]; name != "" {
				model["name"] = name
			}
			models[id] = model
		}
		provider := map[string]any{"npm": npm, "name": "NeuxBot", "options": map[string]any{"baseURL": endpoint, "apiKey": spec.APIKey}, "models": models, "whitelist": spec.SelectedModels}
		content, err := json.Marshal(map[string]any{"model": NativeModelID(spec.RuntimeID, spec.Model), "enabled_providers": []string{constants.AIAccountOpenCodeProvider}, "provider": map[string]any{constants.AIAccountOpenCodeProvider: provider}})
		if err != nil {
			return nil, storageError(err)
		}
		env["OPENCODE_CONFIG"] = filepath.Join(home, "opencode.json")
		if err := WriteFile(env["OPENCODE_CONFIG"], content); err != nil {
			return nil, err
		}
	case constants.RuntimeIDGemini:
		profile, err := privateSubdirectory(home, ".gemini")
		if err != nil {
			return nil, err
		}
		content, _ := json.Marshal(map[string]any{"security": map[string]any{"auth": map[string]any{"selectedType": "gemini-api-key"}}, "model": map[string]any{"name": spec.Model}})
		if err := WriteFile(filepath.Join(profile, "settings.json"), content); err != nil {
			return nil, err
		}
		// 仅信任 NeuxBot 所有的当前工作目录；原生系统策略仍可拒绝启动。
		trustedWorkspace, err := filepath.EvalSymlinks(workspace)
		if err != nil {
			return nil, storageError(err)
		}
		trustPath := filepath.Join(profile, "trustedFolders.json")
		if info, err := os.Lstat(trustPath); err == nil {
			if !info.Mode().IsRegular() {
				return nil, status_error.AIAccountWorkspaceError
			}
		} else if !os.IsNotExist(err) {
			return nil, storageError(err)
		}
		trusts := map[string]string{}
		if data, err := os.ReadFile(trustPath); err == nil {
			if json.Unmarshal(data, &trusts) != nil || trusts == nil {
				return nil, status_error.AIAccountWorkspaceError
			}
		} else if !os.IsNotExist(err) {
			return nil, storageError(err)
		}
		for path, trust := range trusts {
			if trust == "TRUST_FOLDER" || trust == "TRUST_PARENT" {
				continue
			}
			// 原生 CLI 先解析真实路径；根目录及父目录别名也必须保留拒绝规则。
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				path = resolved
			}
			relative, err := filepath.Rel(path, trustedWorkspace)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, status_error.RuntimeUnavailable
			}
		}
		trusts[trustedWorkspace] = "TRUST_FOLDER"
		trust, _ := json.Marshal(trusts)
		if err := WriteFile(trustPath, trust); err != nil {
			return nil, err
		}
		env["GEMINI_CLI_HOME"] = home
		env["GEMINI_API_KEY"] = spec.APIKey
		env["GOOGLE_GEMINI_BASE_URL"], env["GOOGLE_GENAI_API_VERSION"] = GoogleEndpoint(spec.BaseURL)
	case constants.RuntimeIDGrok:
		env["GROK_HOME"] = home
		// 官方目录请求使用全局 Key，但地址固定为同一账号端点，绝不发送到默认 xAI 服务。
		env["GROK_MODELS_BASE_URL"] = strings.TrimRight(spec.BaseURL, "/")
		env["XAI_API_KEY"] = spec.APIKey
		backend := "chat_completions"
		if spec.Protocol == constants.AccountProtocolOpenAIResponses {
			backend = "responses"
		}
		selected, _ := json.Marshal(spec.SelectedModels)
		var content strings.Builder
		fmt.Fprintf(&content, "[models]\ndefault = %s\nallowed_models = %s\n", strconv.Quote(spec.Model), selected)
		for _, id := range spec.SelectedModels {
			fmt.Fprintf(&content, "\n[model.%s]\nmodel = %s\nbase_url = %s\napi_key = %s\napi_backend = %s\n", strconv.Quote(id), strconv.Quote(id), strconv.Quote(spec.BaseURL), strconv.Quote(spec.APIKey), strconv.Quote(backend))
		}
		if err := WriteFile(filepath.Join(home, "config.toml"), []byte(content.String())); err != nil {
			return nil, err
		}
	}
	return env, nil
}
