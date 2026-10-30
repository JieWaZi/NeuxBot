package workspace

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// TestPluginEnvironmentKeepsNativeIdentitySeparateFromModelAccount 防止模型账号覆盖原生插件唯一配置源。
func TestPluginEnvironmentKeepsNativeIdentitySeparateFromModelAccount(t *testing.T) {
	for _, cli := range []string{constants.RuntimeIDCodex, constants.RuntimeIDClaude} {
		t.Run(cli, func(t *testing.T) {
			shared, account, workspace := t.TempDir(), t.TempDir(), t.TempDir()
			variable := "CODEX_HOME"
			if cli == constants.RuntimeIDClaude {
				variable = "CLAUDE_CONFIG_DIR"
			}
			for name, body := range map[string]string{"config.toml": "model = \"selected\"\nmodel_provider = \"custom\"\n[model_providers.custom]\nbase_url = \"https://provider.invalid\"\nwire_api = \"responses\"\n", "settings.json": `{"env":{"ANTHROPIC_AUTH_TOKEN":"selected-token"}}`, "auth.json": `{"OPENAI_API_KEY":"selected-key"}`} {
				if err := os.WriteFile(filepath.Join(account, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			original := `{"token":"native-identity"}`
			if err := os.WriteFile(filepath.Join(shared, "auth.json"), []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			// 当前测试程序仅供解析可执行路径；准备阶段不得执行它。
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			target := runtimeconfig.RuntimeTarget{RuntimeID: cli, AccountID: "managed", Environment: map[string]string{variable: account, "PATH": "/fixture/model-path", "MODEL_SECRET": "model-secret"}}
			result, err := PreparePluginEnvironment(PluginEnvironmentRequest{Target: target, Source: runtimeconfig.RuntimeTarget{RuntimeID: cli, ConfigHome: shared}, Detection: runtimeconfig.DetectionResult{Runtime: runtimeconfig.RuntimeSummary{ID: cli, BaseRuntimeID: cli}, ResolvedBinary: binary}, Workspace: workspace, DataDirectory: t.TempDir(), Environment: map[string]string{"PATH": "/fixture/plugin-bin", "PLUGIN_SECRET": "plugin-secret"}, AllowExternal: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Target.ConfigHome != shared || result.Target.Environment[variable] != shared || result.Target.NativePluginScope == "" || result.CommitScope == nil {
				t.Fatalf("native source was replaced by model account: %+v", result.Target)
			}
			path := result.Target.Environment["PATH"]
			if !strings.HasSuffix(path, "/fixture/plugin-bin"+string(os.PathListSeparator)+"/fixture/model-path") {
				t.Fatalf("plugin and model command paths lost ordering: %q", path)
			}
			if result.Target.Environment["PLUGIN_SECRET"] != "plugin-secret" || result.Target.Environment["MODEL_SECRET"] != "model-secret" {
				t.Fatal("private execution environment was lost")
			}
			if cli == constants.RuntimeIDCodex && (result.Target.Environment[constants.AIAccountCodexAPIKeyEnvironment] != "selected-key" || result.Target.CodexPluginConfiguration["model"] != `"selected"`) {
				t.Fatal("selected Codex model account was lost")
			}
			if cli == constants.RuntimeIDClaude && result.Target.ClaudeSettingsPath != filepath.Join(account, "settings.json") {
				t.Fatal("selected Claude model settings were lost")
			}
			if target.Environment[variable] != account || target.Environment["PATH"] != "/fixture/model-path" {
				t.Fatal("assembly rewrote the caller's model environment")
			}
			if err := result.CommitScope(); err != nil {
				t.Fatal(err)
			}
			if body, _ := os.ReadFile(filepath.Join(shared, "auth.json")); string(body) != original {
				t.Fatal("model credentials overwrote native identity")
			}
		})
	}
}

// TestPluginEnvironmentLeavesOtherRuntimeHomesUnchanged 防止无原生插件能力的智能体被套用厂商目录约定。
func TestPluginEnvironmentLeavesOtherRuntimeHomesUnchanged(t *testing.T) {
	result, err := PreparePluginEnvironment(PluginEnvironmentRequest{Target: runtimeconfig.RuntimeTarget{RuntimeID: "pi", ConfigHome: "/fixture/pi", Environment: map[string]string{"PI_CODING_AGENT_DIR": "/fixture/account", "PATH": "/fixture/base"}}, Detection: runtimeconfig.DetectionResult{Runtime: runtimeconfig.RuntimeSummary{ID: "pi", BaseRuntimeID: "pi"}}, Environment: map[string]string{"PATH": "/fixture/plugin"}})
	if err != nil || result.Target.ConfigHome != "/fixture/pi" || result.Target.Environment["PI_CODING_AGENT_DIR"] != "/fixture/account" || result.Target.Environment["PATH"] != "/fixture/plugin"+string(os.PathListSeparator)+"/fixture/base" || result.CommitScope != nil {
		t.Fatalf("unrelated native environment changed: %+v %v", result, err)
	}
}
