package plugins

import (
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func standardInput(t *testing.T, root string, fields map[string]any) {
	t.Helper()
	manifest := map[string]any{"$schema": constants.PluginManifestSchema, "name": "standard-tools", "version": "1.0.0"}
	for key, value := range fields {
		manifest[key] = value
	}
	raw, _ := json.Marshal(manifest)
	fixtureFile(t, root, "plugin.json", string(raw))
}
func standardTestPackage() Package {
	return Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: Manifest{Schema: constants.PluginManifestSchema, Name: "standard-tools"}}
}

func TestStandardPackageUsesOnlyFixedComponents(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, nil)
	fixtureFile(t, root, "skills/reports/SKILL.md", "---\nname: reports\ndescription: Make reports\n---\nRead references/rules.md\n")
	fixtureFile(t, root, "skills/reports/references/rules.md", "Complete instructions")
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"reports":{"type":"stdio","command":"node","args":["${PLUGIN_ROOT}/server.js"]}}}`)
	pkg, err := Load(root)
	if err != nil || ValidateContract(pkg) != nil || len(pkg.Skills) != 1 || len(pkg.Servers) != 1 {
		t.Fatalf("standard discovery: %+v %v", pkg, err)
	}
	resources, file, err := ReadSkillResources(root, pkg, "skills/reports", "references/rules.md")
	if err != nil || len(resources) != 3 || file == nil {
		t.Fatalf("fixed resources: %+v %v", resources, err)
	}
	if _, _, err := ReadSkillResources(root, pkg, "skills/reports", "../../plugin.json"); err == nil {
		t.Fatal("read outside skill")
	}
}
func TestStandardPackageRejectsLegacyInputsAndUnsupportedDeclarations(t *testing.T) {
	for _, legacy := range []string{".codex-plugin/plugin.json", ".claude-plugin/plugin.json", ".neuxally/integration.json"} {
		t.Run(legacy, func(t *testing.T) {
			root := t.TempDir()
			standardInput(t, root, nil)
			fixtureFile(t, root, legacy, `{"name":"old"}`)
			if _, err := Load(root); err == nil {
				t.Fatal("accepted old input")
			}
		})
	}
	for _, fields := range []map[string]any{{"interface": map[string]string{"displayName": "old"}}, {"extensions": map[string]any{"com.ally": map[string]any{}}}, {"extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": 2}}}, {"extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": 1, "management": map[string]any{}}}}, {"extensions": map[string]any{"com.openai": map[string]any{"hooks": "./hooks/hooks.json"}}}} {
		root := t.TempDir()
		standardInput(t, root, fields)
		if _, err := Load(root); err == nil {
			t.Fatalf("accepted unsupported input: %+v", fields)
		}
	}
}
func TestStandardPackageRejectsMalformedOrEscapingComponents(t *testing.T) {
	for _, body := range []string{`{"mcpServers":{}}`, `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"bad":{"type":"stdio","command":"../outside"}}}`, `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"bad":{"type":"http","url":"https://example.com"}}}`} {
		root := t.TempDir()
		standardInput(t, root, nil)
		fixtureFile(t, root, "mcp.json", body)
		if _, err := Load(root); err == nil {
			t.Fatal("malformed MCP became empty capability")
		}
	}
	root, outside := t.TempDir(), t.TempDir()
	standardInput(t, root, nil)
	fixtureFile(t, outside, "SKILL.md", "---\nname: outside\ndescription: outside\n---\n")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "skills", "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("escaping skill accepted")
	}
}
func TestStandardContractRejectsOldPersistedSnapshots(t *testing.T) {
	pkg := standardTestPackage()
	if ValidateContract(pkg) != nil {
		t.Fatal("current rejected")
	}
	pkg.ContractVersion = ""
	if ValidateContract(pkg) == nil {
		t.Fatal("old snapshot admitted")
	}
	for _, read := range []func() error{func() error { _, _, err := SelectTarget(t.TempDir(), pkg, "codex"); return err }, func() error { _, err := ClientExtension(pkg); return err }, func() error { _, err := LocalizeReadme(t.TempDir(), pkg, "en"); return err }} {
		if read() == nil {
			t.Fatal("old snapshot accessor admitted")
		}
	}
}
func TestStandardMCPAuthenticationNeverDowngradesUnsupportedModes(t *testing.T) {
	for _, mode := range []string{"oauth", "mixed", "unknown"} {
		root := t.TempDir()
		standardInput(t, root, nil)
		fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.com/mcp","extensions":{"com.openai":{"auth":{"type":"`+mode+`"}}}}}}`)
		if _, err := Load(root); err == nil {
			t.Fatalf("%s downgraded to anonymous", mode)
		} else if (mode == "oauth" || mode == "mixed") && !errors.Is(err, status_error.PluginCapabilityUnsupported) {
			t.Fatalf("unsupported mode returned wrong failure: %v", err)
		}
	}
	root := t.TempDir()
	standardInput(t, root, nil)
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.com/mcp","extensions":{"com.openai":{"auth":{"type":"none"}}}}}}`)
	pkg, err := Load(root)
	if err != nil || pkg.Servers[0].Authentication.Type != "none" {
		t.Fatalf("none: %+v %v", pkg, err)
	}
}
func TestStandardMCPAPIKeyRequiresExplicitSecretBinding(t *testing.T) {
	for _, declared := range []bool{false, true} {
		root := t.TempDir()
		extensions := map[string]any{}
		if declared {
			extensions["com.neuxbot"] = map[string]any{"schemaVersion": 1, "configuration": map[string]any{"fields": []any{map[string]any{"key": "TOKEN", "label": "Token", "secret": true, "required": true}}, "bindings": []any{map[string]any{"server": "remote", "target": "header", "name": "Authorization", "value": "Bearer ${TOKEN}"}}}}
		}
		standardInput(t, root, map[string]any{"extensions": extensions})
		fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.com/mcp","extensions":{"com.openai":{"auth":{"type":"api_key","headerScheme":"bearer"}}}}}}`)
		pkg, err := Load(root)
		if declared {
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := ResolveServer(pkg, pkg.Servers[0], root, t.TempDir(), map[string]string{"TOKEN": "fictional-secret"})
			if err != nil || resolved.Server.Headers["Authorization"] != "Bearer fictional-secret" {
				t.Fatalf("key not delivered: %+v %v", resolved, err)
			}
		} else if err == nil {
			t.Fatal("missing key binding became anonymous")
		}
	}
}
func TestStandardIconRejectsActiveSVG(t *testing.T) {
	for _, svg := range []string{`<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`, `<svg xmlns="http://www.w3.org/2000/svg" onload="x"/>`} {
		if _, err := sanitizeIcon([]byte(svg), true); err == nil {
			t.Fatal("active svg accepted")
		}
	}
	if _, err := sanitizeIcon([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h1"/></svg>`), true); err != nil {
		t.Fatal(err)
	}
}
func TestStandardSnapshotPreservesPinnedRoot(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "plugins", "entry")
	standardInput(t, entry, nil)
	pkg, err := LoadSource(root, "plugins/entry")
	if err != nil || pkg.SourcePath != "plugins/entry" {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(pkg)
	var frozen Package
	_ = json.Unmarshal(raw, &frozen)
	actual, err := PackageRoot(root, frozen)
	if err != nil || !strings.HasSuffix(actual, "plugins/entry") {
		t.Fatal(actual, err)
	}
}

func TestStandardManagementDeclarationReportsUnsupported(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": 1, "management": map[string]any{}}}})
	if _, err := Load(root); !errors.Is(err, status_error.PluginCapabilityUnsupported) {
		t.Fatalf("management did not report unsupported: %v", err)
	}
}

func TestFailedStandardParseCannotPublishCurrentContractMarker(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, nil)
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"a-valid":{"type":"stdio","command":"node"},"z-invalid":{"type":"unknown"}}}`)
	pkg, err := Load(root)
	if err == nil || pkg.ContractVersion != "" || ValidateContract(pkg) == nil {
		t.Fatalf("partial parse could look current: %+v %v", pkg, err)
	}
}
