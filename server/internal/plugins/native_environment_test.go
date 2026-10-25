package plugins

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeEnvironmentWorktreeScope(t *testing.T) {
	for _, cli := range []string{"codex", "claude"} {
		t.Run(cli, func(t *testing.T) {
			shared := t.TempDir()
			workspace := filepath.Join(shared, "worktrees", "checkout", "project", "workspace")
			if err := os.MkdirAll(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			original := "model = \"external-model\"\n"
			codexHomeWrite(t, filepath.Join(shared, "config.toml"), original, 0600)
			spec := NativeEnvironmentSpec{RuntimeID: cli, SharedPluginHome: shared, Executable: "/fixture/" + cli, AllowExternal: true, Workspace: workspace, StateDirectory: filepath.Join(workspace, constants.RuntimeStateDirectoryName, "native-plugins")}
			parent := t.TempDir()
			view, err := PrepareNativeEnvironment(parent, spec)
			if err != nil {
				t.Fatalf("managed worktree scope rejected: %v", err)
			}
			if view.CommitScope == nil || !view.ScopeChanged {
				t.Fatal("first admission did not prepare its scope marker")
			}
			if err := view.CommitScope(); err != nil {
				t.Fatal(err)
			}
			next, err := PrepareNativeEnvironment(parent, spec)
			if err != nil || next.ScopeChanged {
				t.Fatalf("unchanged worktree scope could not resume: %v", err)
			}
			if raw, _ := os.ReadFile(filepath.Join(shared, "config.toml")); string(raw) != original {
				t.Fatal("worktree scope changed the global configuration")
			}
		})
	}
}

func TestNativeEnvironmentWorktreeAccountModes(t *testing.T) {
	for _, cli := range []string{"codex", "claude"} {
		for _, mode := range []string{"native", "api", "subscription"} {
			for _, external := range []bool{false, true} {
				t.Run(cli+"/"+mode+"/"+map[bool]string{false: "restricted", true: "full"}[external], func(t *testing.T) {
					shared := t.TempDir()
					data := filepath.Join(shared, "worktrees", "checkout", "app-data")
					workspace := filepath.Join(data, "chat", "workspace")
					parent := filepath.Join(data, "plugins", "native-homes")
					if err := os.MkdirAll(workspace, 0700); err != nil {
						t.Fatal(err)
					}
					codexHomeWrite(t, filepath.Join(shared, "config.toml"), "mcp_oauth_credentials_store = \"file\"\n", 0600)
					codexHomeWrite(t, filepath.Join(shared, "auth.json"), `{"token":"native-model"}`, 0600)
					codexHomeWrite(t, filepath.Join(shared, "settings.json"), `{}`, 0600)
					account := ""
					if mode != "native" {
						account = filepath.Join(data, "accounts", mode)
						codexHomeWrite(t, filepath.Join(account, "config.toml"), "model = \"selected\"\n", 0600)
						codexHomeWrite(t, filepath.Join(account, "auth.json"), `{"token":"selected-model"}`, 0600)
						codexHomeWrite(t, filepath.Join(account, "settings.json"), `{"env":{"ANTHROPIC_AUTH_TOKEN":"selected-model"}}`, 0600)
						if mode == "api" {
							codexHomeWrite(t, filepath.Join(account, "auth.json"), `{"OPENAI_API_KEY":"selected-api-key"}`, 0600)
							codexHomeWrite(t, filepath.Join(account, "config.toml"), "model = \"selected\"\nmodel_provider = \"custom\"\n[model_providers.custom]\nbase_url = \"https://fixture.invalid\"\nwire_api = \"responses\"\n", 0600)
						}
					}
					spec := NativeEnvironmentSpec{RuntimeID: cli, AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/" + cli, AllowExternal: external, Workspace: workspace, StateDirectory: filepath.Join(workspace, constants.RuntimeStateDirectoryName, constants.PluginNativeScopeDirectory)}
					view, err := PrepareNativeEnvironment(parent, spec)
					if err != nil {
						t.Fatalf("worktree model environment rejected: %v", err)
					}
					if !external && view.Home == shared {
						t.Fatal("restricted execution inherited the global model home")
					}
					if err := view.CommitScope(); err != nil {
						t.Fatal(err)
					}
					if raw, _ := os.ReadFile(filepath.Join(shared, "auth.json")); string(raw) != `{"token":"native-model"}` {
						t.Fatal("selected account overwrote the native model identity")
					}
				})
			}
		}
	}
}

func TestNativeEnvironmentRejectsScopeInGlobalState(t *testing.T) {
	for _, cli := range []string{"codex", "claude"} {
		t.Run(cli, func(t *testing.T) {
			for _, location := range []string{".", "plugins/cache", "worktrees"} {
				t.Run(location, func(t *testing.T) {
					shared := t.TempDir()
					workspace := filepath.Join(shared, location)
					if err := os.MkdirAll(workspace, 0700); err != nil {
						t.Fatal(err)
					}
					spec := NativeEnvironmentSpec{RuntimeID: cli, SharedPluginHome: shared, Executable: "/fixture/" + cli, AllowExternal: true, Workspace: workspace, StateDirectory: filepath.Join(workspace, constants.RuntimeStateDirectoryName, "native-plugins")}
					if _, err := PrepareNativeEnvironment(t.TempDir(), spec); err != status_error.PluginContentInvalid {
						t.Fatalf("global state accepted as workspace: %v", err)
					}
				})
			}
		})
	}
}

func TestNativeEnvironmentRejectsWorktreeScopeSymlink(t *testing.T) {
	for _, cli := range []string{"codex", "claude"} {
		t.Run(cli, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("fixture requires symbolic links")
			}
			for _, redirect := range []string{"worktree", "runtime"} {
				t.Run(redirect, func(t *testing.T) {
					shared := t.TempDir()
					workspace := filepath.Join(shared, "worktrees", "checkout")
					if err := os.MkdirAll(filepath.Dir(workspace), 0700); err != nil {
						t.Fatal(err)
					}
					link := workspace
					if redirect == "runtime" {
						if err := os.Mkdir(workspace, 0700); err != nil {
							t.Fatal(err)
						}
						link = filepath.Join(workspace, constants.RuntimeStateDirectoryName)
					}
					if err := os.Symlink(shared, link); err != nil {
						t.Fatal(err)
					}
					spec := NativeEnvironmentSpec{RuntimeID: cli, SharedPluginHome: shared, Executable: "/fixture/" + cli, AllowExternal: true, Workspace: workspace, StateDirectory: filepath.Join(workspace, constants.RuntimeStateDirectoryName, "native-plugins")}
					if _, err := PrepareNativeEnvironment(t.TempDir(), spec); err != status_error.PluginContentInvalid {
						t.Fatalf("redirected worktree scope accepted: %v", err)
					}
					if _, err := os.Stat(filepath.Join(shared, "native-plugins")); !os.IsNotExist(err) {
						t.Fatal("rejected scope wrote to the global directory")
					}
				})
			}
		})
	}
}

func TestNativeEnvironmentRejectsAccountInGlobalState(t *testing.T) {
	for _, cli := range []string{"codex", "claude"} {
		for _, location := range []string{".", "plugins/cache/account", "worktrees"} {
			t.Run(cli+"/"+location, func(t *testing.T) {
				shared := t.TempDir()
				account := filepath.Join(shared, location)
				if err := os.MkdirAll(account, 0700); err != nil {
					t.Fatal(err)
				}
				spec := NativeEnvironmentSpec{RuntimeID: cli, AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/" + cli, AllowExternal: true}
				if _, err := PrepareNativeEnvironment(t.TempDir(), spec); err != status_error.PluginContentInvalid {
					t.Fatalf("global state accepted as model account: %v", err)
				}
			})
		}
	}
}

func TestNativeEnvironmentSharesCodexPluginsWithoutChangingModelAccount(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	shared := t.TempDir()
	original := "model = \"external-model\"\n[plugins.\"outside@community\"]\nenabled = true\n[marketplaces.community]\nsource_type = \"local\"\nsource = \"/fixture/community\"\n"
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), original, 0600)
	codexHomeWrite(t, filepath.Join(shared, "auth.json"), `{"token":"external-model-secret"}`, 0600)
	codexHomeWrite(t, filepath.Join(shared, "plugins", "cache", "community", "outside", "local", "skills", "probe", "SKILL.md"), "external skill", 0600)
	codexHomeWrite(t, filepath.Join(account, "config.toml"), "model = \"selected-model\"\n", 0600)
	codexHomeWrite(t, filepath.Join(account, "auth.json"), `{"token":"selected-account"}`, 0600)
	spec := NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex", Targets: []NativeTarget{target}, AllowExternal: true}
	prepared, err := PrepareNativeEnvironment(parent, spec)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Home != account {
		t.Fatalf("account refresh path changed: %q", prepared.Home)
	}
	config := codexHomeConfig(t, prepared.Home)
	if config["model"] != "selected-model" {
		t.Fatalf("selected model changed: %#v", config)
	}
	installed := config["plugins"].(map[string]any)
	if len(installed) != 2 || installed["outside@community"].(map[string]any)["enabled"] != true {
		t.Fatalf("external plugin not delivered: %#v", installed)
	}
	loaded, err := os.ReadFile(filepath.Join(prepared.Home, "plugins", "cache", "community", "outside", "local", "skills", "probe", "SKILL.md"))
	if err != nil || string(loaded) != "external skill" {
		t.Fatalf("external skill unavailable: %q %v", loaded, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(shared, "config.toml")); string(raw) != original {
		t.Fatal("external config changed")
	}
	if raw, _ := os.ReadFile(filepath.Join(shared, "auth.json")); string(raw) != `{"token":"external-model-secret"}` {
		t.Fatal("external model auth changed")
	}
	if raw, _ := os.ReadFile(filepath.Join(account, "auth.json")); string(raw) != `{"token":"selected-account"}` {
		t.Fatal("selected model auth changed")
	}
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), strings.Replace(original, "enabled = true", "enabled = false", 1), 0600)
	next, err := PrepareNativeEnvironment(parent, spec)
	if err != nil {
		t.Fatal(err)
	}
	if next.Fingerprint == prepared.Fingerprint {
		t.Fatal("external enable change did not invalidate capability identity")
	}
	if codexHomeConfig(t, next.Home)["plugins"].(map[string]any)["outside@community"].(map[string]any)["enabled"] != false {
		t.Fatal("external disable was overwritten")
	}
}

func TestNativeEnvironmentPreservesGlobalPluginDisable(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	shared := t.TempDir()
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), "[features]\nplugins = false\n[plugins.\"outside@community\"]\nenabled = true\n", 0600)
	prepared, err := PrepareNativeEnvironment(parent, NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex", Targets: []NativeTarget{target}, AllowExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	config := codexHomeConfig(t, prepared.Home)
	if config["plugins"].(map[string]any)["outside@community"].(map[string]any)["enabled"] != false {
		t.Fatal("global plugin disable was overridden by managed delivery")
	}
	if config["plugins"].(map[string]any)["acceptance@neuxally-managed"].(map[string]any)["enabled"] != true {
		t.Fatal("managed plugin was not delivered")
	}
}

func TestCodexAPIAccountKeepsNativeOAuthHomeAndDoesNotPutKeyInArguments(t *testing.T) {
	parent, account, _ := codexHomeFixture(t)
	shared := t.TempDir()
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), "[features]\nsecret_auth_storage = true\n", 0600)
	codexHomeWrite(t, filepath.Join(shared, "secrets", "mcp_oauth.age"), "native encrypted oauth", 0600)
	codexHomeWrite(t, filepath.Join(account, "auth.json"), `{"OPENAI_API_KEY":"selected-secret"}`, 0600)
	codexHomeWrite(t, filepath.Join(account, "config.toml"), "model = \"selected-model\"\nmodel_provider = \"custom\"\n[model_providers.custom]\nbase_url = \"https://fixture.invalid\"\nrequires_openai_auth = true\nexperimental_bearer_token = \"selected-secret\"\nwire_api = \"responses\"\n", 0600)
	view, err := PrepareNativeEnvironment(parent, NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex", AllowExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if view.Home != shared {
		t.Fatal("API account moved native plugin OAuth to another home")
	}
	arguments, _ := json.Marshal(view.CodexConfiguration)
	if strings.Contains(string(arguments), "selected-secret") {
		t.Fatal("model API key leaked into argv")
	}
	provider := view.CodexConfiguration["model_provider"]
	if provider == `"custom"` {
		t.Fatal("selected account can merge with unrelated global provider credentials")
	}
	if view.Environment["ALLY_CODEX_MODEL_API_KEY"] != "selected-secret" || view.CodexConfiguration["model_providers."+strings.Trim(provider, `"`)+".requires_openai_auth"] != "false" {
		t.Fatal("API account did not use its private process credential")
	}
}

func TestNativeEnvironmentFileOAuthFirstWriteAndExternalReplacement(t *testing.T) {
	parent, account, _ := codexHomeFixture(t)
	shared := t.TempDir()
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), "mcp_oauth_credentials_store = \"file\"\n", 0600)
	spec := NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex", AllowExternal: true}
	view, err := PrepareNativeEnvironment(parent, spec)
	if err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(view.Home, ".credentials.json")
	source := filepath.Join(shared, ".credentials.json")
	if err := os.WriteFile(private, []byte(`{"fixture":"first native OAuth"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(source); err != nil || string(raw) != `{"fixture":"first native OAuth"}` {
		t.Fatal("first native OAuth created a private credential store")
	}
	replacement := filepath.Join(shared, "replacement")
	codexHomeWrite(t, replacement, `{"fixture":"external relogin"}`, 0600)
	if err := os.Rename(replacement, source); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNativeEnvironment(parent, spec); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(private); string(raw) != `{"fixture":"external relogin"}` {
		t.Fatal("new admission retained unlinked OAuth credentials")
	}
	a, err := os.Stat(filepath.Join(account, "mcp-oauth-locks"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(shared, "mcp-oauth-locks"))
	if err != nil || !os.SameFile(a, b) {
		t.Fatal("OAuth aggregate locks are not shared")
	}
}

func TestNativeEnvironmentRejectsHomeBoundOAuthInsteadOfCreatingPrivateIdentity(t *testing.T) {
	parent, account, _ := codexHomeFixture(t)
	shared := t.TempDir()
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), "[features]\nsecret_auth_storage = true\n[plugins.\"outside@community\"]\nenabled = true\n", 0600)
	codexHomeWrite(t, filepath.Join(shared, "secrets", "mcp_oauth.age"), "encrypted native credentials", 0600)
	_, err := PrepareNativeEnvironment(parent, NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex", AllowExternal: true})
	if err != status_error.PluginNotReady {
		t.Fatalf("home-bound OAuth incorrectly presented as shared: %v", err)
	}
}

func TestSubscriptionNativeEnvironmentRejectsSecretsFromRuntimeLayers(t *testing.T) {
	for _, test := range []struct {
		// name 是覆盖来源，失败时定位到具体配置层。
		name string
		// config 是原生用户层配置。
		config string
		// arguments 是选中 CLI 的前置配置覆盖。
		arguments []string
	}{
		{name: "enable argument", arguments: []string{"--enable", "secret_auth_storage"}},
		{name: "inline config", arguments: []string{"-c", "features.secret_auth_storage=true"}},
		{name: "long config", arguments: []string{"--config=features.secret_auth_storage=true"}},
		{name: "explicit profile", config: "[profiles.native.features]\nsecret_auth_storage = true\n", arguments: []string{"--profile", "native"}},
		{name: "default profile", config: "profile = \"native\"\n[profiles.native.features]\nsecret_auth_storage = true\n"},
		{name: "unresolved profile table", config: "[profiles.default.features]\nsecret_auth_storage = true\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, account, _ := codexHomeFixture(t)
			shared := t.TempDir()
			codexHomeWrite(t, filepath.Join(shared, "config.toml"), test.config, 0600)
			_, err := PrepareNativeEnvironment(parent, NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex", PrefixArgs: test.arguments, AllowExternal: true})
			if err != status_error.PluginNotReady {
				t.Fatalf("unshared subscription OAuth layer accepted: %v", err)
			}
			if _, err := os.Stat(filepath.Join(account, ".credentials.json")); !os.IsNotExist(err) {
				t.Fatal("rejected layer created a private OAuth identity")
			}
		})
	}
}

func TestSubscriptionNativeEnvironmentUsesCLIFeaturePrecedence(t *testing.T) {
	for _, test := range []struct {
		// name 说明命令行配置层的实际优先级。
		name string
		// arguments 保留真实 CLI 接收的参数顺序。
		arguments []string
		// secret 是 CLI 合并全部参数后的有效 Secrets 开关。
		secret bool
	}{
		{name: "enable precedes later config", arguments: []string{"--enable", "secret_auth_storage", "-c", "features.secret_auth_storage=false"}, secret: true},
		{name: "disable follows enable", arguments: []string{"--enable", "secret_auth_storage", "--disable", "secret_auth_storage"}},
		{name: "disable precedes enable", arguments: []string{"--disable", "secret_auth_storage", "--enable", "secret_auth_storage"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent, account, _ := codexHomeFixture(t)
			shared := t.TempDir()
			spec := NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex", PrefixArgs: test.arguments, AllowExternal: true}
			secret, _, complete := nativeSubscriptionStorage(spec, map[string]any{}, false)
			if !complete || secret != test.secret {
				t.Fatalf("effective Secrets = %v, complete = %v; want %v", secret, complete, test.secret)
			}
			_, err := PrepareNativeEnvironment(parent, spec)
			if test.secret {
				if err != status_error.PluginNotReady {
					t.Fatalf("enabled Secrets must reject isolated subscription home: %v", err)
				}
			} else if err != nil {
				t.Fatalf("CLI-disabled Secrets must retain native file sharing: %v", err)
			}
		})
	}
}

func TestClaudeNativeEnvironmentUsesSharedRegistryWithoutModelCredentials(t *testing.T) {
	parent, account, _ := codexHomeFixture(t)
	shared := t.TempDir()
	codexHomeWrite(t, filepath.Join(shared, "settings.json"), `{"enabledPlugins":{"outside@community":true},"env":{"ANTHROPIC_AUTH_TOKEN":"global-model"}}`, 0600)
	codexHomeWrite(t, filepath.Join(shared, "plugins", "installed_plugins.json"), `{"version":2,"plugins":{"outside@community":[]}}`, 0600)
	codexHomeWrite(t, filepath.Join(shared, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"global-model"}}`, 0600)
	codexHomeWrite(t, filepath.Join(account, "settings.json"), `{"env":{"ANTHROPIC_AUTH_TOKEN":"selected-model"}}`, 0600)
	view, err := PrepareNativeEnvironment(parent, NativeEnvironmentSpec{RuntimeID: "claude", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/claude", AllowExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if view.Home != shared || view.ClaudeSettingsPath != filepath.Join(account, "settings.json") {
		t.Fatal("Claude moved native plugin authentication to a private home")
	}
	raw, err := os.ReadFile(view.ClaudeSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config["env"].(map[string]any)["ANTHROPIC_AUTH_TOKEN"] != "selected-model" {
		t.Fatal("Claude plugins or model identity were overwritten")
	}
	if _, err := os.ReadFile(filepath.Join(view.Home, "plugins", "installed_plugins.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(account, ".credentials.json")); !os.IsNotExist(err) {
		t.Fatal("Claude model credentials were copied into the selected account")
	}
}

func TestNativeManagedCodexDeliveryRetainsOriginalModelHome(t *testing.T) {
	parent, _, target := codexHomeFixture(t)
	shared := t.TempDir()
	original := "cli_auth_credentials_store = \"keyring\"\n[plugins.\"outside@community\"]\nenabled = true\n"
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), original, 0600)
	spec := NativeEnvironmentSpec{RuntimeID: "codex", SharedPluginHome: shared, Executable: "/fixture/codex", Targets: []NativeTarget{target}, AllowExternal: true, Workspace: t.TempDir(), StateDirectory: t.TempDir()}
	view, err := PrepareNativeEnvironment(parent, spec)
	if err != nil {
		t.Fatal(err)
	}
	if view.Home != shared || view.Marketplace == "" || len(view.CodexConfiguration) == 0 {
		t.Fatal("native model keyring identity was moved to a private home")
	}
	if raw, _ := os.ReadFile(filepath.Join(shared, "config.toml")); string(raw) != original {
		t.Fatal("managed plugin was registered globally")
	}
	if view.CommitScope != nil {
		if err := view.CommitScope(); err != nil {
			t.Fatal(err)
		}
	}
	next, err := PrepareNativeEnvironment(parent, spec)
	if err != nil {
		t.Fatal(err)
	}
	if next.ScopeChanged || view.Marketplace != next.Marketplace {
		t.Fatal("unchanged managed cache invalidated external plugin scope")
	}
}

func TestNativeManagementCommandWritesSharedHomeAndKeepsOtherCommandsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture executable")
	}
	parent, account, _ := codexHomeFixture(t)
	shared := t.TempDir()
	binary := filepath.Join(t.TempDir(), "selected-cli")
	codexHomeWrite(t, binary, "#!/bin/sh\nprintf '%s|%s|%s|%s' \"$CODEX_HOME\" \"${OPENAI_API_KEY-unset}\" \"$GITHUB_TOKEN\" \"$*\"\ncase \"$1\" in plugin|mcp) printf native > \"$CODEX_HOME/installed-by-native\";; esac\n", 0700)
	prepared, err := PrepareNativeEnvironment(parent, NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: binary, AllowExternal: true, ModelEnvironmentKeys: []string{"OPENAI_API_KEY"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"plugin", "mcp", "exec"} {
		cmd := exec.Command(filepath.Join(prepared.CommandDirectory, "codex"), command, "literal argument")
		cmd.Env = []string{"PATH=/usr/bin:/bin", "CODEX_HOME=" + account, "OPENAI_API_KEY=model-secret", "GITHUB_TOKEN=plugin-secret"}
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %s %v", command, output, err)
		}
		want := shared + "|unset|plugin-secret|" + command + " literal argument"
		if command == "exec" {
			want = account + "|model-secret|plugin-secret|exec literal argument"
		}
		if string(output) != want {
			t.Fatalf("%s got %q want %q", command, output, want)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(shared, "installed-by-native")); err != nil || string(raw) != "native" {
		t.Fatal("native installation did not persist in shared home")
	}
}

func TestRestrictedNativeEnvironmentDoesNotExposeExternalPlugins(t *testing.T) {
	parent, account, _ := codexHomeFixture(t)
	shared := t.TempDir()
	codexHomeWrite(t, filepath.Join(shared, "config.toml"), "[plugins.\"outside@community\"]\nenabled = true\n", 0600)
	prepared, err := PrepareNativeEnvironment(parent, NativeEnvironmentSpec{RuntimeID: "codex", AccountHome: account, SharedPluginHome: shared, Executable: "/fixture/codex"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.CommandDirectory != "" {
		t.Fatal("restricted execution received native management routing")
	}
	if len(codexHomeConfig(t, prepared.Home)["plugins"].(map[string]any)) != 0 {
		t.Fatal("restricted execution received external plugins")
	}
	if _, err := os.Stat(filepath.Join(prepared.Home, "plugins", "cache", "community")); !os.IsNotExist(err) {
		t.Fatal("restricted execution mounted external plugin files")
	}
}
