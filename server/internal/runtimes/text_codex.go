package runtimes

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
)

// codexTextNeutralInstructions 满足原生CLI非空基础指令约束，不包含业务规则或代码助手角色。
const codexTextNeutralInstructions = "Respond to the user request."

// prepareCodexText 隔离单次模型的原生配置，只沿用认证和服务连接；普通Chat不经过此边界。
func prepareCodexText(prepared *preparedRuntime, workspace string, messages []TextMessage) (string, func() error, error) {
	sourceHome, err := codexTextSourceHome(prepared)
	if err != nil {
		return "", nil, err
	}
	nativeHome := filepath.Join(workspace, "native", "codex")
	home := filepath.Join(workspace, "home")
	if err := os.MkdirAll(nativeHome, 0700); err != nil {
		return "", nil, status_error.RuntimeUnavailable
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return "", nil, status_error.RuntimeUnavailable
	}
	config := map[string]any{}
	data, err := readTextCredentialSeed(filepath.Join(sourceHome, "config.toml"), 1<<20)
	if err != nil {
		return "", nil, err
	}
	if len(data) > 0 {
		if _, err = toml.Decode(string(data), &config); err != nil {
			return "", nil, status_error.RuntimeConfigurationInvalid
		}
	}
	// 只拷贝服务连接及认证配置，绝不携带instructions/hooks/plugins/MCP等原生代理行为。
	keep := func(source map[string]any) map[string]any {
		out := map[string]any{}
		for _, key := range []string{"model", "model_provider", "model_providers", "model_catalog_json", "preferred_auth_method", "forced_login_method", "cli_auth_credentials_store", "chatgpt_base_url", "openai_base_url"} {
			if value, ok := source[key]; ok {
				out[key] = value
			}
		}
		return out
	}
	minimal := keep(config)
	profileName, err := textCodexProfileName(prepared.definition.prefixArgs)
	if err != nil {
		return "", nil, err
	}
	if profileName != "" {
		profileData, e := readTextCredentialSeed(filepath.Join(sourceHome, profileName+".config.toml"), 1<<20)
		if e != nil {
			return "", nil, e
		}
		if len(profileData) == 0 {
			return "", nil, status_error.RuntimeConfigurationInvalid
		}
		profile := map[string]any{}
		if _, e = toml.Decode(string(profileData), &profile); e != nil {
			return "", nil, status_error.RuntimeConfigurationInvalid
		}
		for key, value := range keep(profile) {
			minimal[key] = value
		}
	}

	if catalog, ok := minimal["model_catalog_json"].(string); ok && catalog != "" {
		if !filepath.IsAbs(catalog) {
			catalog = filepath.Join(sourceHome, catalog)
		}
		catalogData, e := readTextCredentialSeed(catalog, 16<<20)
		if e != nil || len(catalogData) == 0 {
			return "", nil, status_error.RuntimeConfigurationInvalid
		}
		var document map[string]any
		if json.Unmarshal(catalogData, &document) != nil {
			return "", nil, status_error.RuntimeConfigurationInvalid
		}
		if models, ok := document["models"].([]any); ok {
			for _, raw := range models {
				if model, ok := raw.(map[string]any); ok {
					model["base_instructions"] = ""
					model["model_messages"] = nil
				}
			}
		}
		catalogData, e = json.Marshal(document)
		if e != nil {
			return "", nil, status_error.RuntimeConfigurationInvalid
		}
		catalog = filepath.Join(nativeHome, "models.json")
		if e = os.WriteFile(catalog, catalogData, 0600); e != nil {
			return "", nil, status_error.RuntimeUnavailable
		}
		minimal["model_catalog_json"] = catalog
	}
	if store, _ := minimal["cli_auth_credentials_store"].(string); store == "keyring" {
		return "", nil, status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "native keyring-only authentication cannot be transferred into an isolated text session; select a managed account or file-backed authentication"})
	}
	sourceAuth := filepath.Join(sourceHome, "auth.json")
	auth, err := readTextCredentialSeed(sourceAuth, 1<<20)
	if err != nil {
		return "", nil, err
	}
	if len(auth) > 0 && !json.Valid(auth) {
		return "", nil, status_error.RuntimeConfigurationInvalid
	}
	if len(auth) == 0 && !textProviderHasCredential(minimal, prepared.environment) {
		return "", nil, status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{"Detail": "isolated text generation requires file-backed or provider credentials; native keyring credentials cannot be addressed from a temporary Codex home"})
	}
	if len(auth) > 0 {
		minimal["cli_auth_credentials_store"] = "file"
		if err = os.WriteFile(filepath.Join(nativeHome, "auth.json"), auth, 0600); err != nil {
			return "", nil, status_error.RuntimeUnavailable
		}
	}
	var rendered bytes.Buffer
	if err = toml.NewEncoder(&rendered).Encode(minimal); err != nil {
		return "", nil, status_error.RuntimeConfigurationInvalid
	}
	if err = os.WriteFile(filepath.Join(nativeHome, "config.toml"), rendered.Bytes(), 0600); err != nil {
		return "", nil, status_error.RuntimeUnavailable
	}
	if profileName != "" {
		if err = os.WriteFile(filepath.Join(nativeHome, profileName+".config.toml"), rendered.Bytes(), 0600); err != nil {
			return "", nil, status_error.RuntimeUnavailable
		}
	}
	systems := []string{}
	remaining := []TextMessage{}
	for _, message := range messages {
		if message.Role == "system" {
			systems = append(systems, message.Content)
		} else {
			remaining = append(remaining, message)
		}
	}
	instructions := filepath.Join(nativeHome, "model-instructions.md")
	baseInstructions := strings.Join(systems, "\n\n")
	if strings.TrimSpace(baseInstructions) == "" {
		baseInstructions = codexTextNeutralInstructions
	}
	if err = os.WriteFile(instructions, []byte(baseInstructions), 0600); err != nil {
		return "", nil, status_error.RuntimeUnavailable
	}
	prepared.definition.prefixArgs = append(cloneStrings(prepared.definition.prefixArgs), "-c", "project_doc_max_bytes=0", "-c", "skills.bundled.enabled=false", "-c", "skills.include_instructions=false", "-c", `developer_instructions=""`, "-c", "model_instructions_file="+strconv.Quote(instructions))
	prepared.environment, err = buildEnvironment(prepared.environment.list(), map[string]string{"HOME": home, "USERPROFILE": home, "CODEX_HOME": nativeHome, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"), "XDG_STATE_HOME": filepath.Join(home, ".local", "state")})
	if err != nil {
		return "", nil, err
	}
	prompt := compileTextMessages(remaining)
	if len(remaining) == 1 && remaining[0].Role == "user" {
		prompt = remaining[0].Content
	}
	// 只回写同进程文本调用未被其他调用替换的认证；跨进程原生CLI不受此锁保护。
	persist := func() error {
		return persistCodexTextAuth(sourceAuth, auth, filepath.Join(nativeHome, "auth.json"), accountconfig.WriteFile)
	}

	return prompt, persist, nil
}

// readTextCredentialSeed 只接受有界普通文件，不沿符号链接复制认证或配置。
func readTextCredentialSeed(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) > limit {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	return data, nil
}

func textProviderHasCredential(config map[string]any, environment environmentSnapshot) bool {
	if environment.get("OPENAI_API_KEY") != "" || environment.get("CODEX_API_KEY") != "" {
		return true
	}
	provider, _ := config["model_provider"].(string)
	providers, _ := config["model_providers"].(map[string]any)
	selected, _ := providers[provider].(map[string]any)
	if key, _ := selected["env_key"].(string); key != "" && environment.get(key) != "" {
		return true
	}
	if token, _ := selected["experimental_bearer_token"].(string); token != "" {
		return true
	}
	auth, _ := selected["auth"].(map[string]any)
	command, _ := auth["command"].(string)
	return command != ""
}

// textCodexProfileName 按官方CLI参数形态识别当前profile，拒绝把名称解释成路径。
func textCodexProfileName(args []string) (string, error) {
	name := ""
	for i, arg := range args {
		switch {
		case arg == "-p" || arg == "--profile":
			if i+1 >= len(args) {
				return "", status_error.RuntimeConfigurationInvalid
			}
			name = args[i+1]
		case strings.HasPrefix(arg, "--profile="):
			name = strings.TrimPrefix(arg, "--profile=")
		case strings.HasPrefix(arg, "-p") && !strings.HasPrefix(arg, "--") && len(arg) > 2:
			name = arg[2:]
		default:
			continue
		}
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return "", status_error.RuntimeConfigurationInvalid
		}
	}
	return name, nil
}

func persistCodexTextAuth(source string, original []byte, temporary string, write func(string, []byte) error) error {
	if len(original) == 0 {
		return nil
	}
	unlock, err := codexFileCredentialGate(source).Lock(context.Background())
	if err != nil {
		return err
	}
	defer unlock()
	current, err := reconcileCodexFileAuth(source)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return nil
	}
	refreshed, err := readTextCredentialSeed(temporary, 1<<20)
	if err != nil {
		return err
	}
	if bytes.Equal(refreshed, original) {
		return nil
	}
	if !json.Valid(refreshed) {
		return status_error.RuntimeProtocolIncompatible
	}
	var previousAuth, refreshedAuth map[string]any
	_ = json.Unmarshal(original, &previousAuth)
	_ = json.Unmarshal(refreshed, &refreshedAuth)
	previousTokens, _ := previousAuth["tokens"].(map[string]any)
	refreshedTokens, _ := refreshedAuth["tokens"].(map[string]any)
	if accountID, _ := previousTokens["account_id"].(string); accountID != "" && refreshedTokens["account_id"] != accountID {
		return status_error.RuntimeProtocolIncompatible
	}
	return write(source, refreshed)
}

// codexTextSourceHome 定位认证来源，不改变普通Chat的原生目录。
func codexTextSourceHome(prepared *preparedRuntime) (string, error) {
	source := prepared.environment.get("CODEX_HOME")
	if source != "" {
		return source, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", status_error.RuntimeConfigurationInvalid
	}
	return filepath.Join(home, ".codex"), nil
}
