package plugins

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStandardSourceSelectionKeepsSamePinnedPackage(t *testing.T) {
	root := t.TempDir()
	standardInput(t, filepath.Join(root, "entry"), nil)
	pkg, err := LoadSource(root, "entry")
	if err != nil {
		t.Fatal(err)
	}
	for _, cli := range []string{"codex", "claude"} {
		selected, path, err := SelectTarget(root, pkg, cli)
		if err != nil || selected.Manifest.Name != pkg.Manifest.Name || !strings.HasSuffix(path, "entry") {
			t.Fatalf("target drift: %+v %s %v", selected, path, err)
		}
	}

}
func TestStandardReadmeBudgetPreservesOversizeState(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, nil)
	fixtureFile(t, root, "README.md", strings.Repeat("x", 300*1024))
	pkg, err := Load(root)
	if err != nil || len(pkg.Readme) != 300*1024 {
		t.Fatal("readme truncated", err)
	}
	fixtureFile(t, root, "README.md", strings.Repeat("x", 1024*1024+1))
	pkg, err = Load(root)
	if err != nil || pkg.ReadmeStatus != "too-large" || pkg.Readme != "" {
		t.Fatalf("oversize state: %s %v", pkg.ReadmeStatus, err)
	}
}
func TestStandardDriverEntryRejectsEscapingNodeScript(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": constants.PluginExtensionSchemaVersion, "authentication": Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "node", Args: []string{"../outside.mjs"}}}}})
	if _, err := Load(root); err == nil {
		t.Fatal("driver escaped package root")
	}
}
func TestCodexWorkspaceRefusesSymlinkContainer(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	fixtureFile(t, outside, "plugins/keep.txt", "outside must remain")
	if err := os.Symlink(outside, filepath.Join(workspace, ".agents")); err != nil {
		t.Skip(err)
	}
	if err := PrepareCodexWorkspace(workspace, nil); err == nil {
		t.Fatal("followed workspace symlink")
	}
	if body, err := os.ReadFile(filepath.Join(outside, "plugins/keep.txt")); err != nil || string(body) != "outside must remain" {
		t.Fatal("outside modified")
	}
}
func TestStandardNativeDerivationUsesDeclaredComponentsOnly(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"extensions": map[string]any{"com.openai": map[string]any{"interface": map[string]string{"displayName": "Native tools"}}}})
	fixtureFile(t, root, "skills/tool/SKILL.md", "---\nname: tool\ndescription: Use tool\n---\nInstructions")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.TempDir(), "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readObject(filepath.Join(target.Root, ".claude-plugin/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["skills"]; !exists {
		t.Fatal("derived target lost skill")
	}
	if _, exists := raw["hooks"]; exists {
		t.Fatal("invented hooks")
	}
	if _, exists := raw["interface"]; exists {
		t.Fatal("OpenAI presentation leaked to Claude")
	}
}

func TestPublishedStandardTargetRetainsExecutionMetadataWithoutSourceReads(t *testing.T) {
	root := t.TempDir()
	standardInput(t, filepath.Join(root, "entry"), nil)
	fixtureFile(t, root, "entry/skills/review/SKILL.md", "---\nname: review\ndescription: Review changes\n---\nFull guide")
	fixtureFile(t, root, "entry/mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"service":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
	pkg, err := LoadSource(root, "entry")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	var published Package
	if err := json.Unmarshal(stored, &published); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	for _, cli := range []string{"codex", "claude"} {
		target, path, err := DeclaredTarget(published, cli)
		if err != nil || path != "entry" || len(target.Skills) != 1 || target.Skills[0].Name != "review" || target.Skills[0].Path != "skills/review" || len(target.Skills[0].Digest) != 64 || len(target.Servers) != 1 || target.Servers[0].URL != "https://example.test/mcp" {
			t.Fatalf("published target lost components: %+v %q %v", target, path, err)
		}
	}
}
