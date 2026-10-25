package accountconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants"
)

// TestNativeManagedProfiles 验证真实原生配置路径、协议地址与账号修订隔离。
func TestNativeManagedProfiles(t *testing.T) {
	for _, runtimeID := range []string{"opencode", "gemini", "grok"} {
		t.Run(runtimeID, func(t *testing.T) {
			workspace := t.TempDir()
			protocol := constants.AccountProtocol("google-generative-ai")
			if runtimeID == "grok" {
				protocol = constants.AccountProtocolOpenAIResponses
			}
			spec := Spec{ID: "account-a", Revision: 1, RuntimeID: runtimeID, AuthMode: constants.AccountAuthModeAPIKey, Protocol: protocol, BaseURL: "https://example.test/proxy/v1beta", APIKey: "secret-a", Model: "gemini-2.5-pro", SelectedModels: []string{"gemini-2.5-pro"}}
			env, err := Prepare(workspace, spec)
			if err != nil {
				t.Fatal(err)
			}
			var configPath, homeKey string
			switch runtimeID {
			case "opencode":
				homeKey = "XDG_DATA_HOME"
				configPath = env["OPENCODE_CONFIG"]
				for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
					if !strings.HasPrefix(env[key], workspace) {
						t.Fatalf("unmanaged %s: %q", key, env[key])
					}
				}
				var config map[string]any
				data, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &config); err != nil {
					t.Fatal(err)
				}
				if config["model"] != "ally/gemini-2.5-pro" {
					t.Fatalf("model boundary: %v", config)
				}
				provider := config["provider"].(map[string]any)["ally"].(map[string]any)
				options := provider["options"].(map[string]any)
				if options["baseURL"] != "https://example.test/proxy/v1beta" || options["apiKey"] != "secret-a" || provider["npm"] != "@ai-sdk/google" {
					t.Fatalf("Google provider: %v", provider)
				}
			case "gemini":
				homeKey = "GEMINI_CLI_HOME"
				configPath = filepath.Join(env[homeKey], ".gemini", "settings.json")
				if env["GOOGLE_GEMINI_BASE_URL"] != "https://example.test/proxy" || env["GEMINI_API_KEY"] != "secret-a" {
					t.Fatalf("Gemini endpoint: %v", env)
				}
			case "grok":
				homeKey = "GROK_HOME"
				configPath = filepath.Join(env[homeKey], "config.toml")
				if env["XAI_API_KEY"] != "secret-a" || env["GROK_MODELS_BASE_URL"] != spec.BaseURL {
					t.Fatal("catalog credential is not bound to the account endpoint")
				}
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) == 0 {
				t.Fatal("empty configuration")
			}
			spec.ID = "account-b"
			next, err := Prepare(workspace, spec)
			if err != nil {
				t.Fatal(err)
			}
			if next[homeKey] == env[homeKey] {
				t.Fatal("account state reused")
			}
			spec.Revision++
			revised, err := Prepare(workspace, spec)
			if err != nil {
				t.Fatal(err)
			}
			if next[homeKey] == revised[homeKey] {
				t.Fatal("revision state reused")
			}
		})
	}
}

// TestNativeEnvironmentIsolation 保证继承环境不能覆盖受管账号的凭据和目录。
func TestNativeEnvironmentIsolation(t *testing.T) {
	got := Environment([]string{"PATH=/bin", "GEMINI_CLI_HOME=/real", "GOOGLE_GENAI_API_VERSION=v9", "GOOGLE_APPLICATION_CREDENTIALS=/real/key", "GROK_HOME=/real", "XAI_TOKEN=key", "OPENCODE_CONFIG_CONTENT=secret", "XDG_DATA_HOME=/real", "XDG_CONFIG_HOME=/real", "XDG_CACHE_HOME=/real", "XDG_STATE_HOME=/real"})
	if !reflect.DeepEqual(got, []string{"PATH=/bin"}) {
		t.Fatalf("inherited overrides: %v", got)
	}
}

// TestNativeRawModelIdentity 保留带斜杠的服务端模型 ID，受管边界只添加自己的命名空间。
func TestNativeRawModelIdentity(t *testing.T) {
	for _, raw := range []string{"model", "org/model", "ally/model"} {
		if native := NativeModelID("opencode", raw); native != "ally/"+raw || AccountModelID("opencode", native) != raw {
			t.Fatalf("identity lost: %q", raw)
		}
	}
}

// TestGeminiManagedTrustPolicy 保留显式拒绝并拒绝读取受管目录以外的链接文件。
func TestGeminiManagedTrustPolicy(t *testing.T) {
	for _, mode := range []string{"denied", "denied-root", "denied-parent-alias", "symlink", "null"} {
		t.Run(mode, func(t *testing.T) {
			workspace := t.TempDir()
			spec := Spec{ID: "account", Revision: 1, RuntimeID: "gemini", AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: constants.AIAccountGoogleURL, APIKey: "key", Model: "gemini-3.8-flash", SelectedModels: []string{"gemini-3.8-flash"}}
			env, err := Prepare(workspace, spec)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(env["GEMINI_CLI_HOME"], ".gemini", "trustedFolders.json")
			canonical, _ := filepath.EvalSymlinks(workspace)
			deniedPath := canonical
			if mode == "denied-root" {
				deniedPath = filepath.VolumeName(canonical) + string(filepath.Separator)
			}
			if mode == "denied-parent-alias" {
				deniedPath = filepath.Join(t.TempDir(), "denied-parent")
				if err := os.Symlink(filepath.Dir(canonical), deniedPath); err != nil {
					t.Fatal(err)
				}
			}
			content, _ := json.Marshal(map[string]string{deniedPath: "DO_NOT_TRUST"})
			if mode == "symlink" {
				content = []byte("{}")
			}
			if mode == "null" {
				content = []byte("null")
			}
			if err := os.WriteFile(path, content, 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "symlink" {
				target := filepath.Join(t.TempDir(), "trust.json")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Prepare(workspace, spec); err == nil {
				t.Fatal("explicit trust boundary ignored")
			}
		})
	}
}

// TestGrokManagedCredentialContract 验证可被原生 TOML 解析的模型、协议及目录认证绑定，拒绝未实现的 Messages。
func TestGrokManagedCredentialContract(t *testing.T) {
	for _, protocol := range []constants.AccountProtocol{constants.AccountProtocolOpenAIResponses, constants.AccountProtocolOpenAICompletions} {
		spec := Spec{ID: "account", Revision: 1, RuntimeID: constants.RuntimeIDGrok, AuthMode: constants.AccountAuthModeAPIKey, Protocol: protocol, BaseURL: "https://proxy.example/v1", APIKey: "third-party-key", Model: "org/model", SelectedModels: []string{"org/model"}, ModelReasoningLevels: map[string][]string{"org/model": {"provider-fast"}}}
		env, err := Prepare(t.TempDir(), spec)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if _, err := toml.DecodeFile(filepath.Join(env["GROK_HOME"], "config.toml"), &config); err != nil {
			t.Fatal(err)
		}
		model := config["model"].(map[string]any)["org/model"].(map[string]any)
		backend := "responses"
		if protocol == constants.AccountProtocolOpenAICompletions {
			backend = "chat_completions"
		}
		if model["model"] != "org/model" || model["api_key"] != spec.APIKey || model["base_url"] != spec.BaseURL || model["api_backend"] != backend {
			t.Fatalf("incorrect native config: %v", model)
		}
		if _, invented := model["reasoning_efforts"]; invented {
			t.Fatal("opaque choice ID became invented wire enum")
		}
		if env["GROK_MODELS_BASE_URL"] != spec.BaseURL || env["XAI_API_KEY"] != spec.APIKey {
			t.Fatal("global catalog credentials not scoped to account host")
		}
		if Supports(constants.RuntimeIDGrok, constants.AccountProtocolAnthropicMessages) {
			t.Fatal("unsupported Messages authentication advertised")
		}
	}
}
