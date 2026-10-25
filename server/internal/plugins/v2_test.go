package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV2SourceSelectsPinnedNativeAndRejectsMissingTarget(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "neuxally-plugin/github/.codex-plugin/plugin.json", `{"name":"github","version":"2"}`)
	fixtureFile(t, root, "neuxally-plugin/github/.neuxally/integration.json", `{"nativePlugins":{"codex":{"path":"codex-plugins/github"}}}`)
	fixtureFile(t, root, "codex-plugins/github/.codex-plugin/plugin.json", `{"name":"github-native","version":"1","hooks":{"SessionStart":[]}}`)
	pkg, err := LoadSource(root, "neuxally-plugin/github")
	if err != nil {
		t.Fatal(err)
	}
	selected, target, err := SelectTarget(root, pkg, "codex")
	if err != nil || selected.Manifest.Version != "1" || !strings.HasSuffix(target, "codex-plugins/github") {
		t.Fatalf("wrong target: %+v %s %v", selected, target, err)
	}
	if _, _, err := SelectTarget(root, pkg, "claude"); err == nil {
		t.Fatal("reference-only package fabricated a fallback")
	}
	fixtureFile(t, root, "neuxally-plugin/github/.neuxally/integration.json", `{"nativePlugins":{"codex":{"path":"../outside"}}}`)
	if _, err := LoadSource(root, "neuxally-plugin/github"); err == nil {
		t.Fatal("accepted escaping reference")
	}
}

func TestReadme300KiBAndExplicitOversizeState(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"readme"}`)
	fixtureFile(t, root, "README.md", strings.Repeat("x", 300*1024))
	pkg, err := Load(root)
	if err != nil || len(pkg.Readme) != 300*1024 || pkg.ReadmeStatus != "available" {
		t.Fatalf("readme truncated: %d %s %v", len(pkg.Readme), pkg.ReadmeStatus, err)
	}
	fixtureFile(t, root, "README.md", strings.Repeat("x", 1024*1024+1))
	pkg, err = Load(root)
	if err != nil || pkg.ReadmeStatus != "too-large" || pkg.Readme != "" {
		t.Fatalf("oversize inaccurate: %+v %v", pkg, err)
	}
}

func TestBridgeNodeEntryIsContained(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"bridge"}`)
	fixtureFile(t, root, ".neuxally/auth/authorize.mjs", "// entry")
	fixtureFile(t, root, ".neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["./.neuxally/auth/authorize.mjs"]}}`)
	if _, err := Load(root); err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, root, ".neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["../outside.mjs"]}}`)
	if _, err := Load(root); err == nil {
		t.Fatal("accepted outside bridge script")
	}
}

func TestCodexWorkspaceRefusesSymlinkContainer(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	fixtureFile(t, outside, "plugins/keep.txt", "outside must remain")
	if err := os.Symlink(outside, filepath.Join(workspace, ".agents")); err != nil {
		t.Skip(err)
	}
	if err := PrepareCodexWorkspace(workspace, nil); err == nil {
		t.Fatal("followed workspace .agents symlink")
	}
	if content, err := os.ReadFile(filepath.Join(outside, "plugins/keep.txt")); err != nil || string(content) != "outside must remain" {
		t.Fatal("modified external plugin directory")
	}
}

func TestCrossNativeDerivationPreservesInlineAndDefaultComponents(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"native-mixed","version":"1","commands":"./custom-commands","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo preserved"}]}]},"agents":"./agents"}`)
	fixtureFile(t, root, "custom-commands/probe.md", "Preserved command")
	fixtureFile(t, root, "agents/reviewer.md", "Preserved agent")
	fixtureFile(t, root, ".lsp.json", `{"probe":{"command":"probe-lsp"}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.TempDir(), "plugin", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := readObject(filepath.Join(target.Root, ".claude-plugin/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := declaration["author"]; exists {
		t.Fatal("invented invalid empty author for target CLI")
	}
	for _, key := range []string{"commands", "hooks", "agents"} {
		if _, ok := declaration[key]; !ok {
			t.Fatalf("lost inline %s", key)
		}
	}
	for _, path := range []string{".codex-plugin/plugin.json", "custom-commands/probe.md", "agents/reviewer.md", ".lsp.json"} {
		if _, err := os.Stat(filepath.Join(target.Root, path)); err != nil {
			t.Fatalf("lost native file %s", path)
		}
	}
}

func TestSinglePackageStageValidatesNativeReferencesInsideSnapshot(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"entry","version":"1"}`)
	fixtureFile(t, root, ".neuxally/integration.json", `{"nativePlugins":{"codex":{"path":"native"}}}`)
	if _, err := Stage(t.TempDir(), root); err == nil {
		t.Fatal("missing native reference accepted during import")
	}
	fixtureFile(t, root, "native/.codex-plugin/plugin.json", `{"name":"native","version":"2"}`)
	stage, err := Stage(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	if stage.Package.NativeImplementations["codex"].Version != "2" {
		t.Fatal("single-package target source metadata missing")
	}
}

func TestPortableNativeDerivationHonorsWholeOpenAIOverlaySelection(t *testing.T) {
	for _, inline := range []bool{false, true} {
		t.Run(fmt.Sprint(inline), func(t *testing.T) {
			root := t.TempDir()
			extension := ""
			if inline {
				extension = `,"extensions":{"com.openai":{}}`
			}
			fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"common-identity","version":"1"`+extension+`}`)
			fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"overlay-name","hooks":{"SessionStart":[]},"interface":{"displayName":"Overlay"}}`)
			pkg, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			target, err := PrepareNativeTarget(t.TempDir(), "plugin", pkg, root, "codex")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := readObject(filepath.Join(target.Root, ".codex-plugin/plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			_, hooks := raw["hooks"]
			if hooks == inline || !stringEquals(raw["name"], "common-identity") {
				t.Fatalf("mixed overlay or replaced identity: %+v", raw)
			}
		})
	}
}

func TestPortableNativeMCPRequiresTrustWithoutProjection(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"overlay-mcp","version":"1","extensions":{"com.openai":{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp"}}}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.RequiresTrust() {
		t.Fatal("native-only MCP bypassed explicit trust")
	}
}

func TestNativeCapabilitySummariesAreReadOnlyAndDoNotExposeConfiguration(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "entry/.codex-plugin/plugin.json", `{"name":"logical"}`)
	fixtureFile(t, root, "entry/.neuxally/integration.json", `{"nativePlugins":{"codex":{"path":"upstream/codex"},"claude":{"path":"upstream/claude"}}}`)
	fixtureFile(t, root, "upstream/codex/.codex-plugin/plugin.json", `{"name":"upstream"}`)
	fixtureFile(t, root, "upstream/codex/skills/review/SKILL.md", "---\nname: review\ndescription: Review changes\n---\nFull upstream instructions")
	fixtureFile(t, root, "upstream/claude/.claude-plugin/plugin.json", `{"name":"upstream"}`)
	fixtureFile(t, root, "upstream/claude/.mcp.json", `{"github":{"type":"http","url":"https://api.example.test/mcp?secret=hidden","headers":{"Authorization":"Bearer private-token"},"env":{"PRIVATE_TOKEN":"another-secret"}}}`)
	pkg, err := LoadSource(root, "entry")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pkg.NativeImplementations)
	if !strings.Contains(string(data), `"skills":[{"key":"skills/review","name":"review","description":"Review changes"}]`) || !strings.Contains(string(data), `"endpoint":"https://api.example.test"`) {
		t.Fatalf("read-only capability index missing: %s", data)
	}
	if strings.Contains(string(data), "private-token") || strings.Contains(string(data), "another-secret") || strings.Contains(string(data), "secret=hidden") {
		t.Fatal("capability summary exposed configuration")
	}
	if len(pkg.Skills) != 0 || len(pkg.Servers) != 0 {
		t.Fatal("read-only index changed logical runtime components")
	}
}

func TestActualGitHubZIPCapabilityIndex(t *testing.T) {
	archive := os.Getenv("NEUXALLY_UPSTREAM_CAPABILITIES_ZIP")
	if archive == "" {
		t.Skip("explicit immutable upstream archive acceptance only")
	}
	entry := os.Getenv("NEUXALLY_CAPABILITIES_PACKAGE_PATH")
	if entry == "" {
		entry = "plugins/github"
	}
	stage, err := StageSource(t.TempDir(), archive, entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(stage.Package.Integration.NativePlugins) == 0 {
		if len(stage.Package.Servers) != 1 || len(stage.Package.Skills) != 0 || stage.Package.Servers[0].Key != "github" || stage.Package.Servers[0].URL != "https://api.githubcopilot.com/mcp/" {
			t.Fatal("canonical package must expose its one real GitHub MCP")
		}
		for _, cli := range []string{"codex", "claude"} {
			selected, _, err := SelectTarget(stage.Root, stage.Package, cli)
			if err != nil || len(selected.Servers) != 1 || len(TargetAuthenticationBindings(stage.Package, cli)) != 1 {
				t.Fatal("canonical package does not bind its own MCP", err)
			}
		}
		t.Log("canonical package: 0 skills, 1 github MCP, no duplicated runtime implementation")
		return
	}
	if len(stage.Package.Servers) != 0 || len(stage.Package.Skills) != 0 {
		t.Fatal("upstream index modified logical runtime components")
	}
	for _, target := range []string{"codex", "claude"} {
		summary := stage.Package.NativeImplementations[target]
		if len(summary.Servers) != 1 || summary.Servers[0].Name != "github" || summary.Servers[0].Endpoint != "https://api.githubcopilot.com" {
			t.Fatalf("real upstream MCP not visible: %s %+v", target, summary.Servers)
		}
		t.Logf("%s: %d skills, %d github MCP, transport=%s, endpoint=%s", target, len(summary.Skills), len(summary.Servers), summary.Servers[0].Transport, summary.Servers[0].Endpoint)
	}
}
