package accountconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestNativeConfigurationUsesWorkspaceAndNativeShape(t *testing.T) {
	workspace := t.TempDir()
	system := t.TempDir()
	systemAuth := filepath.Join(system, "auth.json")
	if err := os.WriteFile(systemAuth, []byte("system-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", system)
	spec := Spec{ID: "account-a", RuntimeID: constants.RuntimeIDCodex, AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: "https://api.kimi.com/coding/v1", APIKey: "account-a-secret", Revision: 1}
	env, err := Prepare(workspace, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(env["CODEX_HOME"], workspace+string(filepath.Separator)) {
		t.Fatal("configuration escaped workspace")
	}
	data, err := os.ReadFile(filepath.Join(env["CODEX_HOME"], "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var auth map[string]string
	if err := json.Unmarshal(data, &auth); err != nil {
		t.Fatal(err)
	}
	if auth["OPENAI_API_KEY"] != spec.APIKey {
		t.Fatal("selected key not projected")
	}
	config, _ := os.ReadFile(filepath.Join(env["CODEX_HOME"], "config.toml"))
	if !strings.Contains(string(config), "requires_openai_auth = true") || !strings.Contains(string(config), spec.BaseURL) {
		t.Fatal("native config fields missing")
	}
	spec.ID, spec.APIKey, spec.Revision = "account-b", "account-b-secret", 2
	next, err := Prepare(workspace, spec)
	if err != nil {
		t.Fatal(err)
	}
	if next["CODEX_HOME"] != env["CODEX_HOME"] {
		t.Fatal("switch changed native session home")
	}
	data, _ = os.ReadFile(filepath.Join(next["CODEX_HOME"], "auth.json"))
	if strings.Contains(string(data), "account-a-secret") || !strings.Contains(string(data), "account-b-secret") {
		t.Fatal("account switch did not replace live auth")
	}
	systemData, _ := os.ReadFile(systemAuth)
	if string(systemData) != "system-credential" || os.Getenv("CODEX_HOME") != system {
		t.Fatal("system CLI configuration changed")
	}
	info, _ := os.Stat(filepath.Join(next["CODEX_HOME"], "auth.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("credential permissions too broad")
	}
}

func TestSameKeyCanConfigureClaudeAndCodex(t *testing.T) {
	workspace := t.TempDir()
	for _, runtime := range []string{constants.RuntimeIDClaude, constants.RuntimeIDCodex} {
		spec := Spec{ID: "kimi-plan", RuntimeID: runtime, AuthMode: constants.AccountAuthModeAPIKey, APIKey: "same-key", BaseURL: "https://api.kimi.com/coding", Revision: 1}
		if runtime == constants.RuntimeIDCodex {
			spec.BaseURL += "/v1"
		}
		environment, err := Prepare(workspace, spec)
		if err != nil {
			t.Fatal(err)
		}
		if len(environment) == 0 {
			t.Fatal("missing native home")
		}
	}
	claude, _ := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDClaude), "settings.json"))
	if !strings.Contains(string(claude), "ANTHROPIC_AUTH_TOKEN") || !strings.Contains(string(claude), "same-key") {
		t.Fatal("Claude did not receive shared account")
	}
}

func TestEnvironmentRemovesInheritedAuthentication(t *testing.T) {
	actual := Environment([]string{"PATH=/usr/bin", "HOME=/home/user", "HTTPS_PROXY=http://proxy", "PI_MCP_MODULE_PATH=/opt/ally/pi-bridge.mjs", "OPENAI_API_KEY=system", "ANTHROPIC_AUTH_TOKEN=system", "CODEX_HOME=/system", "PI_CODING_AGENT_DIR=/system", "KIMI_API_KEY=system"})
	if len(actual) != 4 || !strings.Contains(strings.Join(actual, "\n"), "PI_MCP_MODULE_PATH=/opt/ally/pi-bridge.mjs") || strings.Contains(strings.Join(actual, "\n"), "system") {
		t.Fatalf("unexpected environment: %#v", actual)
	}
}

func TestDeepSeekCodexAccountRegistersSelectedModel(t *testing.T) {
	workspace := t.TempDir()
	_, err := Prepare(workspace, Spec{ID: "deepseek", RuntimeID: constants.RuntimeIDCodex,
		AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolOpenAIResponses,
		BaseURL: "https://api.deepseek.com", APIKey: "test-key", Model: "deepseek-flash"})
	if err != nil {
		t.Fatal(err)
	}
	home := Home(workspace, constants.RuntimeIDCodex)
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "model_catalog_json") {
		t.Fatal("Codex account does not load a model catalog")
	}
	data, err := os.ReadFile(filepath.Join(home, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		// Models 是生成的 Codex 模型目录。
		// Models 保存该提供方允许使用的模型。
		Models []struct {
			// Slug 是实际请求模型 ID。
			Slug string `json:"slug"`
			// ApplyPatchToolType 是官方 DeepSeek Codex 工具协议形态。
			ApplyPatchToolType string `json:"apply_patch_tool_type"`
			// BaseInstructions 是该模型目录附带的官方运行时说明。
			BaseInstructions string `json:"base_instructions"`
			// SupportedReasoningLevels 是原生菜单可用的推理等级。
			SupportedReasoningLevels []struct {
				// Effort 是 Codex 原生推理等级标识。
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) == 0 || catalog.Models[0].Slug != "deepseek-flash" || catalog.Models[0].ApplyPatchToolType != "freeform" || catalog.Models[0].BaseInstructions == "" || len(catalog.Models[0].SupportedReasoningLevels) != 3 {
		t.Fatalf("Codex DeepSeek model metadata missing: %+v", catalog.Models)
	}
}

func TestCodexCatalogUsesAccountModelMappings(t *testing.T) {
	workspace := t.TempDir()
	_, err := Prepare(workspace, Spec{
		ID: "mapped", RuntimeID: constants.RuntimeIDCodex, AuthMode: constants.AccountAuthModeAPIKey,
		Protocol: constants.AccountProtocolOpenAIResponses, BaseURL: "https://example.com/v1", APIKey: "secret",
		Model: "vendor-model", SelectedModels: []string{"vendor-model"},
		ModelDisplayNames:    map[string]string{"vendor-model": "工作模型"},
		ModelContextSizes:    map[string]int{"vendor-model": 262144},
		ModelReasoningLevels: map[string][]string{"vendor-model": {"low", "high"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDCodex), "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		// Models 是该账号在 Codex 中可选的实际模型。
		Models []struct {
			// Slug 是发送到上游的模型 ID。
			Slug string `json:"slug"`
			// DisplayName 是 Codex 模型菜单显示名。
			DisplayName string `json:"display_name"`
			// ContextWindow 是模型上下文窗口。
			ContextWindow int `json:"context_window"`
			// SupportedReasoningLevels 是账号声明的推理等级。
			SupportedReasoningLevels []struct {
				// Effort 是原生等级标识。
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].Slug != "vendor-model" || catalog.Models[0].DisplayName != "工作模型" || catalog.Models[0].ContextWindow != 262144 || len(catalog.Models[0].SupportedReasoningLevels) != 2 {
		t.Fatalf("unexpected native mapping: %+v", catalog.Models)
	}
}

func TestCodexCatalogDefaultsUnknownModelToCommonReasoningLevels(t *testing.T) {
	workspace := t.TempDir()
	_, err := Prepare(workspace, Spec{
		ID: "doubao", RuntimeID: constants.RuntimeIDCodex, AuthMode: constants.AccountAuthModeAPIKey,
		Protocol: constants.AccountProtocolOpenAIResponses,
		BaseURL:  "https://ark.cn-beijing.volces.com/api/v3", APIKey: "test-key",
		Model: "doubao-seed-1-6-flash-250615", SelectedModels: []string{"doubao-seed-1-6-flash-250615"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDCodex), "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		// Models 是生成的 Codex 模型目录。
		Models []struct {
			// SupportedReasoningLevels 是当前模型展示的思考档位。
			SupportedReasoningLevels []struct {
				// Effort 是 Codex 原生档位。
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	levels := catalog.Models[0].SupportedReasoningLevels
	if len(levels) != 3 || levels[0].Effort != "low" || levels[1].Effort != "medium" || levels[2].Effort != "high" {
		t.Fatalf("unknown model has wrong default reasoning levels: %+v", levels)
	}
}

func TestKimiDeepSeekAccountDeclaresVerifiedThinkingLevels(t *testing.T) {
	workspace := t.TempDir()
	_, err := Prepare(workspace, Spec{ID: "deepseek", RuntimeID: constants.RuntimeIDKimi,
		AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolOpenAICompletions,
		BaseURL: "https://api.deepseek.com", APIKey: "test-key", Model: "deepseek-flash"})
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDKimi), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "capabilities = [\"thinking\"]") || !strings.Contains(string(config), "support_efforts = [\"low\", \"high\", \"max\"]") {
		t.Fatalf("Kimi DeepSeek model has no verified Thinking metadata: %s", config)
	}
}

func TestKimiThirdPartyAccountCapsCompletionWithoutShrinkingContext(t *testing.T) {
	workspace := t.TempDir()
	environment, err := Prepare(workspace, Spec{ID: "deepseek", RuntimeID: constants.RuntimeIDKimi,
		AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolOpenAIResponses,
		BaseURL: "https://api.deepseek.com", APIKey: "test-key", Model: "deepseek-flash", MaxContextSize: 1048576})
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDKimi), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "max_context_size = 1048576") {
		t.Fatal("Kimi lost the configured 1M context window")
	}
	if environment["KIMI_MODEL_MAX_COMPLETION_TOKENS"] != "32768" {
		t.Fatal("Kimi third-party request has no bounded output-token budget")
	}
}

func TestManagedHomeRefusesLinkToSystemConfiguration(t *testing.T) {
	workspace := t.TempDir()
	system := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(system, filepath.Join(workspace, "runtime", "accounts")); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureHome(workspace, constants.RuntimeIDCodex); err == nil {
		t.Fatal("accepted a system configuration symlink")
	}
	entries, _ := os.ReadDir(system)
	if len(entries) != 0 {
		t.Fatal("wrote into system configuration")
	}
}

func TestClaudeOpenCodeGoUsesAPIKeyHeader(t *testing.T) {
	workspace := t.TempDir()
	for _, baseURL := range []string{"https://opencode.ai/zen/go", "https://opencode.ai/zen/go/"} {
		_, err := Prepare(workspace, Spec{
			ID: "opencode-go", RuntimeID: constants.RuntimeIDClaude,
			AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolAnthropicMessages,
			BaseURL: baseURL, APIKey: "go-key",
		})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDClaude), "settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		config := map[string]map[string]string{}
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		if config["env"]["ANTHROPIC_API_KEY"] != "go-key" || config["env"]["ANTHROPIC_AUTH_TOKEN"] != "" {
			t.Fatal("OpenCode Go requires API Key authentication instead of Bearer")
		}
	}
}

func TestClaudeRolesUseSelectedAccountModel(t *testing.T) {
	workspace := t.TempDir()
	_, err := Prepare(workspace, Spec{ID: "claude-account", RuntimeID: constants.RuntimeIDClaude, AuthMode: constants.AccountAuthModeAPIKey,
		Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://api.example.com", APIKey: "secret", Model: "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDClaude), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		// Env 保存生成配置中的受管环境变量。
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL"} {
		if config.Env[key] != "deepseek-chat" {
			t.Fatalf("Claude role %s escaped selected account model", key)
		}
	}
}

func TestClaudeRolesCanMapToOtherSelectedAccountModels(t *testing.T) {
	workspace := t.TempDir()
	_, err := Prepare(workspace, Spec{
		ID: "claude-account", RuntimeID: constants.RuntimeIDClaude, AuthMode: constants.AccountAuthModeAPIKey,
		Protocol: constants.AccountProtocolAnthropicMessages, BaseURL: "https://api.example.com", APIKey: "secret",
		Model: "fast", SelectedModels: []string{"fast", "strong"},
		ClaudeModelRoles:  map[string]string{"opus": "strong"},
		ModelDisplayNames: map[string]string{"strong": "强模型"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDClaude), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		// Env 是 Claude Code 使用的原生环境变量映射。
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Env["ANTHROPIC_MODEL"] != "fast" || config.Env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "strong" || config.Env["ANTHROPIC_DEFAULT_OPUS_MODEL_NAME"] != "强模型" || config.Env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "fast" {
		t.Fatalf("Claude role mapping did not follow account selection: %+v", config.Env)
	}
}

func TestPiUsesInternalProviderAliasForRawAccountModel(t *testing.T) {
	workspace := t.TempDir()
	_, err := Prepare(workspace, Spec{ID: "pi-account", RuntimeID: constants.RuntimeIDPi, AuthMode: constants.AccountAuthModeAPIKey,
		Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: "https://api.deepseek.com/v1", APIKey: "secret", Model: "deepseek-flash"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(Home(workspace, constants.RuntimeIDPi), "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		// Providers 保存生成的提供方配置。
		Providers map[string]struct {
			// Models 保存该提供方可用的模型配置。
			Models []struct {
				// ID 是写入配置的模型标识。
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Providers["ally"].Models) != 1 || config.Providers["ally"].Models[0].ID != "deepseek-flash" {
		t.Fatal("Pi 配置未保留服务端原始模型 ID")
	}
}

func TestKimiUsesNativeChatProviderWithEditableContext(t *testing.T) {
	workspace := t.TempDir()
	env, err := Prepare(workspace, Spec{ID: "kimi-account", RuntimeID: constants.RuntimeIDKimi, AuthMode: constants.AccountAuthModeAPIKey,
		Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: "https://api.deepseek.com/v1", APIKey: "secret", Model: "deepseek-flash"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env["KIMI_CODE_HOME"], "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "type = \"openai\"") || !strings.Contains(string(data), "max_context_size = 262144") || !strings.Contains(string(data), "model = \"deepseek-flash\"") {
		t.Fatal("Kimi Chat provider or default context is incorrect")
	}
	_, err = Prepare(workspace, Spec{ID: "kimi-account", RuntimeID: constants.RuntimeIDKimi, AuthMode: constants.AccountAuthModeAPIKey,
		Protocol: constants.AccountProtocolOpenAICompletions, BaseURL: "https://api.deepseek.com/v1", APIKey: "secret", Model: "deepseek-flash", MaxContextSize: 131072})
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(env["KIMI_CODE_HOME"], "config.toml"))
	if !strings.Contains(string(data), "max_context_size = 131072") {
		t.Fatal("Kimi context override was not projected")
	}
}
