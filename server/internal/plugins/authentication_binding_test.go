package plugins

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func authenticationInput(t *testing.T, root string, bindings []AuthenticationBinding, skillBindings []SkillAuthenticationBinding) {
	t.Helper()
	standardInput(t, root, map[string]any{"extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": 1, "authentication": Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "node", Args: []string{"./.neuxbot/auth.mjs"}, Bindings: bindings, SkillBindings: skillBindings}}}})
	fixtureFile(t, root, ".neuxbot/auth.mjs", "// package driver")
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.com/mcp","headers":{"Authorization":"old-placeholder","X-Literal":"retained"}}}}`)
}
func TestStandardAuthenticationBindsExactCurrentPackageTargets(t *testing.T) {
	root := t.TempDir()
	bindings := []AuthenticationBinding{{Runtime: "codex", Server: "remote", URL: "https://example.com/mcp", Header: "Authorization"}, {Runtime: "claude", Server: "remote", URL: "https://example.com/mcp", Header: "Authorization"}}
	authenticationInput(t, root, bindings, nil)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.RequiresTrust() {
		t.Fatal("driver must require trust")
	}
	for _, cli := range []string{"codex", "claude"} {
		selected, _, err := SelectTarget(root, pkg, cli)
		if err != nil || len(selected.Servers) != 1 || selected.Servers[0].Headers["Authorization"] != "" || selected.Servers[0].Headers["X-Literal"] != "retained" {
			t.Fatalf("wrong exact binding: %+v %v", selected, err)
		}
	}
	if AuthenticationEnvironmentKey("one", bindings[0]) == AuthenticationEnvironmentKey("two", bindings[0]) {
		t.Fatal("identity not isolated")
	}
}
func TestStandardAuthenticationRejectsMissingComponentEndpointAndLegacyProtocol(t *testing.T) {
	for _, binding := range []AuthenticationBinding{{Runtime: "codex", Server: "missing", URL: "https://example.com/mcp", Header: "Authorization"}, {Runtime: "codex", Server: "remote", URL: "https://other.example/mcp", Header: "Authorization"}, {Runtime: "other", Server: "remote", URL: "https://example.com/mcp", Header: "Authorization"}} {
		root := t.TempDir()
		authenticationInput(t, root, []AuthenticationBinding{binding}, nil)
		if _, err := Load(root); err == nil {
			t.Fatal("bad exact target binding admitted")
		}
	}
	root := t.TempDir()
	standardInput(t, root, map[string]any{"extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": 1, "authentication": map[string]any{"strategy": "plugin-command", "protocol": "neuxally-auth/1", "command": "node", "args": []string{"./auth.mjs"}}}}})
	if _, err := Load(root); err == nil {
		t.Fatal("old protocol accepted")
	}
}
func TestStandardSkillAuthenticationRestrictsEnvironmentAndPrivatePaths(t *testing.T) {
	for _, binding := range []SkillAuthenticationBinding{{Runtime: "codex", Skill: "skills/tool", EnvironmentKeys: []string{"TOKEN", "TOOL_HOME"}, PrivatePathKeys: []string{"TOOL_HOME"}}, {Runtime: "codex", Skill: "skills/missing", EnvironmentKeys: []string{"TOKEN"}}, {Runtime: "codex", Skill: "skills/tool", EnvironmentKeys: []string{"HOME"}}, {Runtime: "codex", Skill: "skills/tool", EnvironmentKeys: []string{"PATH"}}, {Runtime: "codex", Skill: "skills/tool", EnvironmentKeys: []string{"TOKEN"}, PrivatePathKeys: []string{"OTHER"}}} {
		root := t.TempDir()
		authenticationInput(t, root, nil, []SkillAuthenticationBinding{binding})
		fixtureFile(t, root, "skills/tool/SKILL.md", "---\nname: tool\ndescription: Use tool\n---\nInstructions")
		pkg, err := Load(root)
		valid := binding.Skill == "skills/tool" && len(binding.EnvironmentKeys) == 2
		if valid {
			if err != nil || len(TargetSkillAuthenticationBindings(pkg, "codex")) != 1 {
				t.Fatal(err)
			}
		} else if err == nil {
			raw, _ := json.Marshal(binding)
			t.Fatalf("unsafe skill environment admitted: %s", raw)
		}
	}
}

func TestStandardClaudeAuthenticationDerivesOnlyBoundHeaderTemplate(t *testing.T) {
	root := t.TempDir()
	binding := AuthenticationBinding{Runtime: "claude", Server: "remote", URL: "https://example.com/mcp", Header: "Authorization"}
	authenticationInput(t, root, []AuthenticationBinding{binding}, nil)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	native, err := PrepareNativeTarget(t.Context(), t.TempDir(), "plugin-id", pkg, root, "claude", binding)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readObject(filepath.Join(native.Root, ".claude-plugin/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var servers map[string]struct {
		// Headers 是派生模板中唯一受检的公开头部定义。
		Headers map[string]string `json:"headers"`
	}
	if json.Unmarshal(manifest["mcpServers"], &servers) != nil {
		t.Fatal("missing derived MCP")
	}
	if servers["remote"].Headers["Authorization"] != "${"+AuthenticationEnvironmentKey("plugin-id", binding)+"}" || servers["remote"].Headers["X-Literal"] != "retained" {
		t.Fatalf("wrong header template: %+v", servers)
	}
	source, err := os.ReadFile(filepath.Join(root, "mcp.json"))
	if err != nil || !strings.Contains(string(source), "old-placeholder") {
		t.Fatal("source package changed")
	}
}

func TestStandardCodexKeepsPortableRootAndRejectsUnprovedSecretOverlay(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, nil)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	native, err := PrepareNativeTarget(t.Context(), t.TempDir(), "plugin-id", pkg, root, "codex")
	expectedRoot, resolveErr := filepath.EvalSymlinks(root)
	if err != nil || resolveErr != nil || native.Root != expectedRoot {
		t.Fatalf("Codex standard root replaced: %+v %v", native, err)
	}
	binding := AuthenticationBinding{Runtime: "codex", Server: "remote", URL: "https://example.com/mcp", Header: "Authorization"}
	if _, err := PrepareNativeTarget(t.Context(), t.TempDir(), "plugin-id", pkg, root, "codex", binding); err == nil {
		t.Fatal("unproved Codex secret overlay was admitted")
	}
}

func TestStandardProjectionSharesOnlyComponentAuthenticationDeclarations(t *testing.T) {
	mcp := []AuthenticationBinding{{Server: "remote", URL: "https://example.com/mcp", Header: "Authorization"}}
	skills := []SkillAuthenticationBinding{{Skill: "skills/tool", EnvironmentKeys: []string{"TOKEN", "TOOL_HOME"}, PrivatePathKeys: []string{"TOOL_HOME"}}}
	root := t.TempDir()
	authenticationInput(t, root, mcp, skills)
	fixtureFile(t, root, "skills/tool/SKILL.md", "---\nname: tool\ndescription: Use tool\n---\nInstructions")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []string{"codex", "claude", "workbuddy", "qoder", "pi", "cursor", "gemini", "grok", "kimi", "opencode"} {
		if len(TargetAuthenticationBindings(pkg, runtime)) != 1 || len(TargetSkillAuthenticationBindings(pkg, runtime)) != 1 || AuthenticationTargetRuntime(pkg, runtime) != "" {
			t.Fatal("shared component declarations withheld", runtime)
		}
	}
}
