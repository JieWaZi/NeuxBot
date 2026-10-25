package plugins

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatibilityDefaultDeclaredSkillsNotDuplicated(t *testing.T) {
	for _, format := range []string{"codex", "claude"} {
		for _, declared := range []string{"./skills/", "./skills/reports/", "./skills/../skills/reports"} {
			t.Run(format+"/"+declared, func(t *testing.T) {
				root := t.TempDir()
				manifest, _ := json.Marshal(map[string]string{"name": "compatible-tools", "skills": declared})
				fixtureFile(t, root, "."+format+"-plugin/plugin.json", string(manifest))
				fixtureFile(t, root, "skills/reports/SKILL.md", "---\nname: reports\ndescription: Create reports\n---\nInstructions\n")
				pkg, err := Load(root)
				if err != nil || len(pkg.Skills) != 1 || pkg.Skills[0].Key != "skills/reports" {
					t.Fatalf("default skill discovered more than once: %+v %v", pkg, err)
				}
			})
		}
	}
}

func TestCodexReportsDefaultHooksWithoutEnablingThem(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"default-hooks"}`)
	fixtureFile(t, root, "hooks/hooks.json", `{"hooks":{"SessionStart":[]}}`)
	pkg, err := Load(root)
	if err != nil || len(pkg.Diagnostics) != 1 || pkg.Diagnostics[0].Component != "hooks" || pkg.Diagnostics[0].Code != "unsupported_component" {
		t.Fatalf("default hooks were silently ignored: %+v %v", pkg.Diagnostics, err)
	}
}

func TestCompatibilityNullSkillPathsDoNotSelectPluginRoot(t *testing.T) {
	for _, declaration := range []string{"null", "[null]"} {
		t.Run(declaration, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"null-path","skills":`+declaration+`}`)
			fixtureFile(t, root, "SKILL.md", "---\nname: root\ndescription: Root skill\n---\nInstructions\n")
			pkg, err := Load(root)
			if err != nil || len(pkg.Skills) != 0 || len(pkg.Diagnostics) != 1 || pkg.Diagnostics[0].Component != "skills" {
				t.Fatalf("null declaration selected a component: %+v %v", pkg, err)
			}
		})
	}
}

func TestRootSkillResourcesExcludePluginMetadataAndOtherSkills(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"root-tools","skills":"."}`)
	fixtureFile(t, root, "SKILL.md", "---\nname: root\ndescription: Root skill\n---\nRead references/rules.md\n")
	fixtureFile(t, root, "references/rules.md", "Skill reference")
	fixtureFile(t, root, "README.md", "Plugin overview")
	fixtureFile(t, root, "skills/other/SKILL.md", "---\nname: other\ndescription: Another skill\n---\nOther\n")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	resources, file, err := ReadSkillResources(root, pkg, ".", "")
	if err != nil || len(resources) != 3 || file != nil || resources[0].Path != "SKILL.md" || resources[2].Path != "references/rules.md" {
		t.Fatalf("unrelated plugin files exposed: %+v %+v %v", resources, file, err)
	}
	if _, _, err := ReadSkillResources(root, pkg, ".", ".codex-plugin/plugin.json"); err == nil {
		t.Fatal("plugin manifest read as a skill resource")
	}
}

func TestPluginIconAcceptsLocalImagesAndRejectsActiveOrEscapingSVG(t *testing.T) {
	for _, fixture := range []struct {
		// name 是图片边界测试场景。
		name string
		// content 是待验证的图片字节。
		content string
		// valid 是预期可展示性。
		valid bool
	}{
		{"static", `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h24v24H0z"/></svg>`, true},
		{"xml-declaration", `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0h24v24H0z"/></svg>`, true},
		{"stylesheet-instruction", `<?xml-stylesheet href="https://example.com/a.css"?><svg xmlns="http://www.w3.org/2000/svg"/>`, false},
		{"script", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`, false},
		{"event", `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`, false},
		{"external", `<svg xmlns="http://www.w3.org/2000/svg"><image href="https://example.com/a.png"/></svg>`, false},
		{"foreign", `<svg xmlns="http://www.w3.org/2000/svg"><foreignObject/></svg>`, false},
		{"external-style", `<svg xmlns="http://www.w3.org/2000/svg"><path fill="url(https://example.com/a)"/></svg>`, false},
		{"doctype", `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]><svg xmlns="http://www.w3.org/2000/svg">&x;</svg>`, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"icon-tools","icon":"./icon.svg"}`)
			fixtureFile(t, root, "icon.svg", fixture.content)
			pkg, err := Load(root)
			if err != nil || (pkg.Icon != "") != fixture.valid {
				t.Fatalf("invalid icon acceptance: %+v %v", pkg, err)
			}
		})
	}
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"png-tools","interface":{"logo":"./icon.png"}}`)
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	fixtureFile(t, root, "icon.png", string(data))
	pkg, err := Load(root)
	if err != nil || !strings.HasPrefix(pkg.Icon, "data:image/png;base64,") {
		t.Fatalf("PNG software icon rejected: %+v %v", pkg, err)
	}
	outside := t.TempDir()
	fixtureFile(t, outside, "icon.png", string(data))
	if err := os.Remove(filepath.Join(root, "icon.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "icon.png"), filepath.Join(root, "icon.png")); err != nil {
		t.Fatal(err)
	}
	pkg, err = Load(root)
	if err != nil || pkg.Icon != "" {
		t.Fatalf("icon escaped package root: %+v %v", pkg, err)
	}
}

func TestLoadCodexPluginNormalizesPresentationAndImportsSkillsAndMCP(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".codex-plugin/plugin.json", `{"name":"codex-tools","version":"2.1.0","author":{"name":"Original author"},"skills":"./custom/","mcpServers":"./mcp/servers.json","apps":"./.app.json","interface":{"displayName":"Codex Tools","longDescription":"Complete plugin instructions","developerName":"Upstream","websiteURL":"https://example.com","privacyPolicyURL":"https://example.com/privacy","logo":"./assets/icon.svg"}}`)
	fixtureFile(t, root, "custom/reports/SKILL.md", "---\nname: reports\ndescription: Create reports\n---\nRead references/rules.md\n")
	fixtureFile(t, root, "custom/reports/references/rules.md", "Full reference")
	fixtureFile(t, root, "mcp/servers.json", `{"mcpServers":{"reports":{"command":"node","args":["${PLUGIN_ROOT}/server.js","${API_TOKEN}"]}}}`)
	fixtureFile(t, root, "assets/icon.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#123456" d="M0 0h24v24H0z"/></svg>`)
	fixtureFile(t, root, "README.md", "# Codex Tools\nComplete plugin documentation\n")
	pkg, err := Load(root)
	if err != nil {
		t.Fatalf("Codex plugin was not imported: %v", err)
	}
	if pkg.Format != "codex" || len(pkg.Skills) != 1 || len(pkg.Servers) != 1 || pkg.Servers[0].Type != "stdio" || pkg.Manifest.Author.Name != "Original author" {
		t.Fatalf("Codex facts lost: %+v", pkg)
	}
	encoded, _ := json.Marshal(pkg)
	var normalized map[string]any
	_ = json.Unmarshal(encoded, &normalized)
	presentation, ok := normalized["presentation"].(map[string]any)
	if !ok || presentation["displayName"] != "Codex Tools" || presentation["developerName"] != "Upstream" || normalized["readme"] == "" || normalized["icon"] == "" {
		t.Fatalf("missing normalized metadata: %s", encoded)
	}
	extension, err := ClientExtension(pkg)
	if err != nil || len(extension.Config) != 1 || extension.Config[0].Key != "API_TOKEN" {
		t.Fatalf("Codex configuration not recognized: %+v %v", extension, err)
	}
	if len(pkg.Diagnostics) != 1 || pkg.Diagnostics[0].Component != "apps" || pkg.Diagnostics[0].Code != "unsupported_component" {
		t.Fatalf("unsupported hosted apps must stay explicit: %+v", pkg.Diagnostics)
	}
}

func TestClaudeDeclaredComponentsAddToDefaultDiscovery(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"claude-tools","skills":["./extra/"],"mcpServers":"./extra-mcp.json"}`)
	fixtureFile(t, root, "skills/default/SKILL.md", "---\nname: default\ndescription: Default skill\n---\nDefault\n")
	fixtureFile(t, root, "extra/custom/SKILL.md", "---\nname: custom\ndescription: Extra skill\n---\nExtra\n")
	fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"default":{"command":"node"},"shared":{"command":"old"}}}`)
	fixtureFile(t, root, "extra-mcp.json", `{"mcpServers":{"custom":{"command":"node"},"shared":{"command":"new"}}}`)
	pkg, err := Load(root)
	if err != nil || len(pkg.Skills) != 2 || len(pkg.Servers) != 3 {
		t.Fatalf("declared components replaced defaults: %+v %v", pkg, err)
	}
	for _, server := range pkg.Servers {
		if server.Key == "shared" && server.Command != "new" {
			t.Fatalf("declared MCP did not override default: %+v", server)
		}
	}
}

func fixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPortablePluginKeepsCompleteSkillAndSelectsPortableManifest(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"office.tools","version":"1.0.0","author":{"name":"Example"},"extensions":{"com.example.other":{"enabled":true}}}`)
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"ignored-claude"}`)
	fixtureFile(t, root, "skills/reports/SKILL.md", "---\nname: reports\ndescription: Create reports\n---\nRead references/rules.md\n")
	fixtureFile(t, root, "skills/reports/references/rules.md", "Complete instructions")
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"reports":{"type":"stdio","command":"node","args":["${PLUGIN_ROOT}/server.js"],"cwd":"./"},"bad":{"type":"stdio","command":"../escape"}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Format != "agent-plugins" || pkg.Manifest.Name != "office.tools" || len(pkg.Skills) != 1 || len(pkg.Servers) != 1 {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if pkg.Skills[0].Name != "reports" || pkg.Skills[0].Path != "skills/reports" || pkg.Servers[0].Key != "reports" || len(pkg.Diagnostics) == 0 {
		t.Fatalf("missing component facts: %+v", pkg)
	}
}

func TestLoadClaudePluginImportsDeclaredSkillsAndMCPWithoutHooks(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"legacy-tools","version":"2.0","skills":["./custom"],"mcpServers":{"remote":{"type":"http","url":"https://example.com/mcp"}},"hooks":{"SessionStart":[]}}`)
	fixtureFile(t, root, "custom/SKILL.md", "---\nname: custom\ndescription: A custom skill\n---\nUse this\n")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Format != "claude" || len(pkg.Skills) != 1 || len(pkg.Servers) != 1 || pkg.Servers[0].Type != "streamable-http" {
		t.Fatalf("unexpected Claude import: %+v", pkg)
	}
	if len(pkg.Diagnostics) == 0 {
		t.Fatal("unsupported hooks must remain visible")
	}
}

func TestLoadRejectsInvalidManifestButKeepsOtherComponentTypes(t *testing.T) {
	for _, manifest := range []string{`{"name":"missing-schema"}`, `{"$schema":"https://bad.example/schema","name":"tools"}`, `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"Bad Name"}`, `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"tools","version":3}`} {
		root := t.TempDir()
		fixtureFile(t, root, "plugin.json", manifest)
		if _, err := Load(root); err == nil {
			t.Fatalf("accepted invalid manifest: %s", manifest)
		}
	}
	root := t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"tools","unknown":true,"extensions":3}`)
	fixtureFile(t, root, "skills", "not a directory")
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"sse","url":"https://example.com/mcp"}}}`)
	pkg, err := Load(root)
	if err != nil || len(pkg.Servers) != 1 || len(pkg.Diagnostics) < 2 {
		t.Fatalf("component error spread: %+v %v", pkg, err)
	}
}

func TestLoadRejectsEscapingManifestAndSkipsEscapingSkill(t *testing.T) {
	outside := t.TempDir()
	fixtureFile(t, outside, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"outside"}`)
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "plugin.json"), filepath.Join(root, "plugin.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("accepted manifest outside package")
	}
	root = t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"safe"}`)
	fixtureFile(t, root, "skills/local/SKILL.md", "---\nname: local\ndescription: safe\n---\n")
	fixtureFile(t, outside, "SKILL.md", "---\nname: outside\ndescription: unsafe\n---\n")
	if err := os.Symlink(outside, filepath.Join(root, "skills", "external")); err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil || len(pkg.Skills) != 1 || len(pkg.Diagnostics) == 0 {
		t.Fatalf("unsafe skill loaded: %+v %v", pkg, err)
	}
}

func TestPortableSkillsDiscoverOnlyImmediateSubdirectories(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"discovery"}`)
	fixtureFile(t, root, "skills/SKILL.md", "---\nname: wrong-root\ndescription: not a portable component\n---\n")
	fixtureFile(t, root, "skills/child/SKILL.md", "---\nname: child\ndescription: valid component\n---\n")
	pkg, err := Load(root)
	if err != nil || len(pkg.Skills) != 1 || pkg.Skills[0].Name != "child" {
		t.Fatalf("nonstandard discovery: %+v %v", pkg, err)
	}
}
func TestPortableRejectsNullMetadataAndMalformedServerFields(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"metadata","keywords":null}`)
	if _, err := Load(root); err == nil {
		t.Fatal("accepted null keyword metadata")
	}
	fixtureFile(t, root, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"metadata"}`)
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"bad-args":{"type":"stdio","command":"node","args":null},"bad-env":{"type":"stdio","command":"node","env":{"TOKEN":null}},"safe":{"type":"stdio","command":"node","args":[]}}}`)
	pkg, err := Load(root)
	if err != nil || len(pkg.Servers) != 1 || pkg.Servers[0].Key != "safe" {
		t.Fatalf("malformed entries affected valid boundary: %+v %v", pkg, err)
	}
}

func TestClaudeNativeCommandAndWorkingDirectoryUseClaudeRootAlias(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"native-command"}`)
	fixtureFile(t, root, "scripts/server", "native fixture")
	fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"native":{"command":"${CLAUDE_PLUGIN_ROOT}/scripts/server","cwd":"${CLAUDE_PLUGIN_ROOT}"}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Servers) != 1 {
		t.Fatalf("Claude root alias rejected: %+v", pkg.Diagnostics)
	}
}

func TestPackageSnapshotRetainsLegacyRootWithoutChangingNewPackages(t *testing.T) {
	for _, raw := range []string{`{"manifest":{"name":"office"},"sourcePath":"plugins/office"}`, `{"manifest":{"name":"office"}}`} {
		var pkg Package
		if err := json.Unmarshal([]byte(raw), &pkg); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(pkg)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Package
		if err := json.Unmarshal(encoded, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if roundTrip.SourcePath != pkg.SourcePath || roundTrip.Manifest.Name != "office" {
			t.Fatalf("snapshot changed: %s", encoded)
		}
	}
}

func TestPackageRootRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"../outside", outside, "outside", "missing"} {
		pkg := Package{SourcePath: source}
		if _, err := PackageRoot(root, pkg); err == nil {
			t.Fatalf("unsafe or unavailable legacy root accepted: %q", source)
		}
	}
	pkg := Package{}
	if actual, err := PackageRoot(root, pkg); err != nil || actual != root {
		t.Fatalf("new package root changed: %q %v", actual, err)
	}
}
