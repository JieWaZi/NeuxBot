package plugins

import (
	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexHomeCopiesCompleteNativePackage(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	codexHomeWrite(t, filepath.Join(target.Root, "bin", "run"), "#!/bin/sh\nprintf native\n", 0751)
	codexHomeWrite(t, filepath.Join(target.Root, "hooks", "hooks.json"), `{"hooks":{}}`, 0640)
	home, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if home == account || !strings.HasPrefix(home, parent+string(os.PathSeparator)) {
		t.Fatalf("home escaped private parent: %q", home)
	}
	cache := filepath.Join(home, "plugins", "cache", "neuxally-managed", "acceptance", "1.0.0")
	for _, rel := range []string{".codex-plugin/plugin.json", "bin/run", "hooks/hooks.json"} {
		got, err := os.ReadFile(filepath.Join(cache, rel))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(target.Root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("package changed: %s", rel)
		}
	}
	info, err := os.Stat(filepath.Join(cache, "bin/run"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0751 {
		t.Fatalf("executable mode = %o", info.Mode().Perm())
	}
	config := codexHomeConfig(t, home)
	plugins := config["plugins"].(map[string]any)
	if len(plugins) != 1 || plugins["acceptance@neuxally-managed"].(map[string]any)["enabled"] != true {
		t.Fatalf("enabled plugins = %#v", plugins)
	}
	for _, path := range []string{home, filepath.Join(home, "plugins"), filepath.Join(home, "plugins", "cache")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("private directory mode = %o", info.Mode().Perm())
		}
	}
}

func TestCodexHomeAllowsOnlySelectedAccountModelConfiguration(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	original := `model = "base"
model_provider = "custom"
profile = "selected"
instructions = "unwanted instruction"
model_instructions_file = "/unwanted"
[profiles.selected]
model = "selected-model"
[mcp_servers.unwanted]
command = "unwanted"
[plugins."unwanted@global"]
enabled = true
[features]
exec_server = true
[model_providers.custom]
name = "custom"
base_url = "https://example.invalid/v1"
experimental_bearer_token = "fixture-secret"
command = "unwanted"
`
	codexHomeWrite(t, filepath.Join(account, "config.toml"), original, 0644)
	codexHomeWrite(t, filepath.Join(account, "auth.json"), `{"token":"fixture-token"}`, 0644)
	codexHomeWrite(t, filepath.Join(account, "AGENTS.md"), "global instruction", 0644)
	codexHomeWrite(t, filepath.Join(account, "skills", "extra", "SKILL.md"), "global skill", 0644)
	home, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	config := codexHomeConfig(t, home)
	if config["model"] != "selected-model" || config["model_provider"] != "custom" || config["cli_auth_credentials_store"] != "file" {
		t.Fatalf("model/auth lost: %#v", config)
	}
	for _, key := range []string{"instructions", "model_instructions_file", "mcp_servers", "profiles", "profile", "hooks", "skills"} {
		if _, ok := config[key]; ok {
			t.Fatalf("unexpected capability: %s", key)
		}
	}
	provider := config["model_providers"].(map[string]any)["custom"].(map[string]any)
	if provider["experimental_bearer_token"] != "fixture-secret" || provider["base_url"] != "https://example.invalid/v1" {
		t.Fatalf("provider config lost: %#v", provider)
	}
	if _, ok := provider["command"]; ok {
		t.Fatal("provider capability copied")
	}
	for _, file := range []string{"config.toml", "auth.json"} {
		info, err := os.Stat(filepath.Join(home, file))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("secret mode = %o", info.Mode().Perm())
		}
	}
	for _, path := range []string{"AGENTS.md", "skills"} {
		if _, err := os.Lstat(filepath.Join(home, path)); !os.IsNotExist(err) {
			t.Fatalf("global capability copied: %s", path)
		}
	}
	got, err := os.ReadFile(filepath.Join(account, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatal("original config modified")
	}
}

func TestCodexHomePreservesRefreshedAuthAndColdSession(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	codexHomeWrite(t, filepath.Join(account, "auth.json"), `{"token":"source"}`, 0600)
	home, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	codexHomeWrite(t, filepath.Join(home, "auth.json"), `{"token":"refreshed"}`, 0600)
	codexHomeWrite(t, filepath.Join(home, "sessions", "session.jsonl"), "session", 0600)
	again, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if again != home {
		t.Fatal("cold home identity changed")
	}
	got, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"token":"refreshed"}` {
		t.Fatal("private refreshed auth overwritten")
	}
	if _, err := os.Stat(filepath.Join(home, "sessions", "session.jsonl")); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(account, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"token":"source"}` {
		t.Fatal("account auth modified")
	}
	codexHomeWrite(t, filepath.Join(account, "auth.json"), `{"token":"rotated"}`, 0600)
	if _, err := PrepareCodexHome(parent, account, []NativeTarget{target}); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"token":"rotated"}` {
		t.Fatal("selected account rotation ignored")
	}
	if err := os.Remove(filepath.Join(account, "auth.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCodexHome(parent, account, []NativeTarget{target}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("removed source auth remains active")
	}
}

func TestCodexHomeIsolatesDigestAccountAndTargetSelection(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	home, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	target.Digest = "new-content"
	changed, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if home == changed {
		t.Fatal("different digest shared home")
	}
	empty, err := PrepareCodexHome(parent, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if home == empty {
		t.Fatal("empty selection shared home")
	}
	if plugins, ok := codexHomeConfig(t, empty)["plugins"]; ok && len(plugins.(map[string]any)) != 0 {
		t.Fatal("empty selection retained plugins")
	}
	other, err := PrepareCodexHome(parent, t.TempDir(), []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if other == changed {
		t.Fatal("different account shared home")
	}
}

func TestCodexHomeRejectsUnsafePackagesAndPaths(t *testing.T) {
	for _, scenario := range []string{"nested-link", "root-link", "name-traversal", "version-traversal", "duplicate-name", "parent-in-source"} {
		t.Run(scenario, func(t *testing.T) {
			parent, account, target := codexHomeFixture(t)
			targets := []NativeTarget{target}
			switch scenario {
			case "nested-link":
				if err := os.Symlink(account, filepath.Join(target.Root, "outside")); err != nil {
					t.Fatal(err)
				}
			case "root-link":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(target.Root, link); err != nil {
					t.Fatal(err)
				}
				targets[0].Root = link
			case "name-traversal":
				targets[0].Name = "../escape"
			case "version-traversal":
				targets[0].Version = "../escape"
			case "duplicate-name":
				targets = append(targets, target)
			case "parent-in-source":
				parent = filepath.Join(target.Root, "home")
			}
			if _, err := PrepareCodexHome(parent, account, targets); err != status_error.PluginContentInvalid {
				t.Fatalf("unsafe input error = %v", err)
			}
		})
	}
}

func TestCodexHomeRejectsLinkedAccountSecretsAndMalformedConfig(t *testing.T) {
	for _, filename := range []string{"auth.json", "config.toml"} {
		t.Run(filename, func(t *testing.T) {
			parent, account, target := codexHomeFixture(t)
			source := filepath.Join(t.TempDir(), "secret")
			codexHomeWrite(t, source, "{}", 0600)
			if err := os.Symlink(source, filepath.Join(account, filename)); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareCodexHome(parent, account, []NativeTarget{target}); err != status_error.AIAccountConfigurationInvalid {
				t.Fatalf("secret link error = %v", err)
			}
		})
	}
	parent, account, target := codexHomeFixture(t)
	codexHomeWrite(t, filepath.Join(account, "config.toml"), "[broken", 0600)
	if _, err := PrepareCodexHome(parent, account, []NativeTarget{target}); err != status_error.AIAccountConfigurationInvalid {
		t.Fatalf("config error = %v", err)
	}
}

func codexHomeFixture(t *testing.T) (string, string, NativeTarget) {
	t.Helper()
	parent, account, source := t.TempDir(), t.TempDir(), t.TempDir()
	codexHomeWrite(t, filepath.Join(source, ".codex-plugin", "plugin.json"), `{"name":"acceptance","version":"1.0.0"}`, 0644)
	return parent, account, NativeTarget{ID: "logical-id", Name: "acceptance", Version: "1.0.0", Root: source, Digest: "content-digest"}
}
func codexHomeWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
func codexHomeConfig(t *testing.T, home string) map[string]any {
	t.Helper()
	config := map[string]any{}
	if _, err := toml.DecodeFile(filepath.Join(home, "config.toml"), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestCodexHomeRejectsMutatedActiveCacheWithoutReplacingFiles(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	home, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(home, "plugins", "cache", "neuxally-managed", "acceptance", "1.0.0", ".codex-plugin", "plugin.json")
	codexHomeWrite(t, manifest, `{"changed":true}`, 0644)
	if _, err := PrepareCodexHome(parent, account, []NativeTarget{target}); err != status_error.PluginVersionConflict {
		t.Fatalf("mutated cache error = %v", err)
	}
	got, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"changed":true}` {
		t.Fatal("active cache was replaced")
	}
}

func TestCodexHomeReusesNativeCacheWithoutReplacingPackageFiles(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	home, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(home, "plugins", "cache", "neuxally-managed", "acceptance", "1.0.0", ".codex-plugin", "plugin.json")
	before, err := os.Stat(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCodexHome(parent, account, []NativeTarget{target}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("shared active package file replaced")
	}
}

func TestCodexHomeRejectsReplacedCacheAncestor(t *testing.T) {
	parent, account, target := codexHomeFixture(t)
	home, err := PrepareCodexHome(parent, account, []NativeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(home, "plugins")
	outside := filepath.Join(t.TempDir(), "plugins")
	if err := os.Rename(original, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, original); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCodexHome(parent, account, []NativeTarget{target}); err != status_error.PluginContentInvalid {
		t.Fatalf("cache ancestor link error = %v", err)
	}
}
