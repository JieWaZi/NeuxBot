package plugins

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeTargetsCoverRegisteredCLIWithoutChangingSource(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "multi", "version": "1.0.0"})
	fixtureFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: Review changes\n---\nReview")
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ContentDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	for _, cli := range []string{"codex", "claude", "cursor", "workbuddy", "qoder", "kimi", "gemini", "grok", "opencode", "pi"} {
		t.Run(cli, func(t *testing.T) {
			target, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, cli)
			if err != nil {
				t.Fatalf("missing target: %v", err)
			}
			again, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, cli)
			if err != nil || again.Root != target.Root || again.Digest != target.Digest {
				t.Fatalf("non deterministic target: %+v %v", again, err)
			}
			if cli == "grok" {
				body, err := os.ReadFile(filepath.Join(target.Root, "plugin.json"))
				if err != nil {
					t.Fatal(err)
				}
				var manifest map[string]any
				_ = json.Unmarshal(body, &manifest)
				if _, ok := manifest["$schema"]; ok {
					t.Fatal("portable manifest shadows grok native manifest")
				}
				if _, err := os.Stat(filepath.Join(target.Root, ".mcp.json")); err != nil {
					t.Fatal("grok missing native mcp path", err)
				}
			}
		})
	}
	after, err := ContentDigest(root)
	if err != nil || after != before {
		t.Fatal("generator modified source", err)
	}
}

func TestLoadAcceptsMatchingNativeEntryAndRejectsExtraExecution(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "multi"})
	fixtureFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\nReview")
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"multi","skills":["./skills/review"]}`)
	fixtureFile(t, root, ".cursor-plugin/plugin.json", `{"name":"multi","skills":["./skills/review"]}`)
	if _, err := Load(root); err != nil {
		t.Fatalf("matching native entries rejected: %v", err)
	}
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"multi","skills":["./skills/review"],"hooks":{"SessionStart":["execute"]}}`)
	if _, err := Load(root); err == nil {
		t.Fatal("untrusted native hooks accepted")
	}
}

func TestComponentAuthenticationWithoutRuntimeCoversEveryCLI(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": 1, "authentication": map[string]any{"strategy": "plugin-command", "protocol": "neuxbot-auth/1", "command": "node", "args": []string{"auth.mjs"}, "bindings": []map[string]any{{"server": "remote", "url": "https://example.test/mcp", "header": "Authorization"}}, "skillBindings": []map[string]any{{"skill": "skills/review", "environmentKeys": []string{"ACCESS_TOKEN"}}}}}}})
	fixtureFile(t, root, "auth.mjs", "// fixture")
	fixtureFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\nReview")
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatalf("component authorization rejected: %v", err)
	}
	for _, cli := range []string{"codex", "claude", "cursor", "workbuddy", "qoder", "kimi", "gemini", "grok", "opencode", "pi"} {
		if len(TargetAuthenticationBindings(pkg, cli)) != 1 || len(TargetSkillAuthenticationBindings(pkg, cli)) != 1 {
			t.Fatalf("component authorization unavailable for %s", cli)
		}
	}
}

func TestTargetCacheRepairsCorruptionAndReturnsCompleteOrderedComponents(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "cached"})
	fixtureFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\nOriginal")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	targets, err := PrepareTargets(t.Context(), cache, "p", pkg, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 10 {
		t.Fatal("missing targets")
	}
	for index, runtime := range []string{"codex", "claude", "cursor", "workbuddy", "qoder", "kimi", "gemini", "grok", "opencode", "pi"} {
		target := targets[index]
		if target.Runtime != runtime || len(target.Skills) != 1 || target.Skills[0].Key != "skills/review" || target.Profile == "" {
			t.Fatalf("incomplete target: %+v", target)
		}
	}
	damaged := targets[1]
	fixtureFile(t, damaged.Root, "skills/review/SKILL.md", "damaged")
	repaired, err := PrepareNativeTarget(t.Context(), cache, "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := ContentDigest(repaired.Root)
	if err != nil || actual != damaged.Digest {
		t.Fatal("corrupted target not repaired", err)
	}
}

func TestSharedAuthenticationSlotSurvivesOneTargetSpecialization(t *testing.T) {
	pkg := Package{Integration: Integration{Authentication: &Authentication{Bindings: []AuthenticationBinding{{Server: "remote", URL: "https://example.test/mcp", Header: "Authorization"}, {Runtime: "claude", Server: "remote", URL: "https://example.test/mcp", Header: "X-Token"}}, SkillBindings: []SkillAuthenticationBinding{{Skill: "skills/review", EnvironmentKeys: []string{"TOKEN"}}, {Runtime: "claude", Skill: "skills/review", EnvironmentKeys: []string{"CLAUDE_TOKEN"}}}}}}
	if slot := AuthenticationTargetRuntime(pkg, "cursor"); slot != "" {
		t.Fatalf("shared authorization material slot not selected: %q", slot)
	}
	if bindings := TargetAuthenticationBindings(pkg, "claude"); len(bindings) != 1 || bindings[0].Header != "X-Token" {
		t.Fatal("target specialization merged shared permissions", bindings)
	}
	if bindings := TargetSkillAuthenticationBindings(pkg, "claude"); len(bindings) != 1 || bindings[0].EnvironmentKeys[0] != "CLAUDE_TOKEN" {
		t.Fatal("target skill specialization merged shared permissions", bindings)
	}
}

func TestNativeMCPFormatsFollowActualTransportSchemas(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "transports"})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp","headers":{"Authorization":"${TOKEN}"}},"local":{"type":"stdio","command":"node","args":["${PLUGIN_ROOT}/server.mjs"],"env":{"PUBLIC":"value"}}}}`)
	fixtureFile(t, root, "server.mjs", "// local tool")
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	gemini, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "gemini")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readObject(filepath.Join(gemini.Root, "gemini-extension.json"))
	if err != nil {
		t.Fatal(err)
	}
	var servers map[string]map[string]any
	if json.Unmarshal(raw["mcpServers"], &servers) != nil {
		t.Fatal("missing Gemini servers")
	}
	if servers["remote"]["httpUrl"] != "https://example.test/mcp" || servers["remote"]["url"] != nil || servers["remote"]["type"] != nil {
		t.Fatalf("Gemini HTTP mistaken for SSE: %+v", servers["remote"])
	}
	opencode, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = readObject(filepath.Join(opencode.Root, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcp map[string]json.RawMessage
	if json.Unmarshal(raw["mcp"], &mcp) != nil || json.Unmarshal(mcp["servers"], &servers) != nil {
		t.Fatal("missing OpenCode servers")
	}
	if servers["remote"]["type"] != "remote" || servers["local"]["type"] != "local" || servers["local"]["env"] != nil {
		t.Fatal("OpenCode portable MCP incorrectly reused", servers)
	}
	if command, ok := servers["local"]["command"].([]any); !ok || len(command) != 2 || command[0] != "node" {
		t.Fatal("OpenCode missing command array", servers["local"])
	}
}

func TestPiMCPArtifactRegistersServersWithoutCore(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "pi-registration"})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp","headers":{"Authorization":"${TOKEN}"}}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "pi")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readObject(filepath.Join(target.Root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declaration map[string][]string
	if json.Unmarshal(raw["pi"], &declaration) != nil || len(declaration["extensions"]) != 1 {
		t.Fatal("Pi MCP package has no actual registration entry")
	}
	entry := filepath.Join(target.Root, filepath.FromSlash(declaration["extensions"][0]))
	code := `const {pathToFileURL}=await import('node:url');const module=await import(pathToFileURL(process.argv[1]));const calls=[];module.default({registerMcpServer:(name,config)=>calls.push({name,config})});process.stdout.write(JSON.stringify(calls));`
	command := exec.Command("node", "--input-type=module", "-e", code, entry)
	command.Env = append(os.Environ(), "TOKEN=Bearer fixture")
	body, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Pi entry cannot load: %v %s", err, body)
	}
	var calls []map[string]any
	if json.Unmarshal(body, &calls) != nil || len(calls) != 1 || calls[0]["name"] != "remote" {
		t.Fatalf("Pi MCP not registered: %s", body)
	}
	config := calls[0]["config"].(map[string]any)
	if config["url"] != "https://example.test/mcp" || config["headers"].(map[string]any)["Authorization"] != "Bearer fixture" {
		t.Fatal("Pi registration changed endpoint or copied secret", config)
	}
}

func TestNativeEntriesRejectConflictingMCPAndImplicitExecution(t *testing.T) {
	for _, path := range []string{".claude-plugin/plugin.json", ".cursor-plugin/plugin.json"} {
		root := t.TempDir()
		standardInput(t, root, map[string]any{"name": "guarded"})
		fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
		fixtureFile(t, root, path, `{"name":"guarded","mcpServers":{"remote":{"type":"http","url":"https://other.test/mcp"}}}`)
		if _, err := Load(root); err == nil {
			t.Fatal("native endpoint conflict admitted", path)
		}
	}
	for path, body := range map[string]string{"package.json": `{"name":"guarded","pi":{"extensions":["./untrusted.mjs"]}}`, "opencode.json": `{"plugin":["./untrusted.mjs"]}`, "extensions/untrusted.mjs": "// implicit Pi code", "GEMINI.md": "unmodeled system instructions"} {
		root := t.TempDir()
		standardInput(t, root, map[string]any{"name": "guarded"})
		fixtureFile(t, root, path, body)
		if _, err := Load(root); err == nil {
			t.Fatal("unmodeled implicit execution admitted", path)
		}
	}
}

func TestNativeEntryReusesVerifiedMCPSidecarAndSkillDirectory(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "sidecar"})
	fixtureFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\nReview")
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
	fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"remote":{"type":"http","url":"https://example.test/mcp"}}}`)
	original := `{"name":"sidecar","skills":"./skills","mcpServers":"./.mcp.json"}`
	fixtureFile(t, root, ".claude-plugin/plugin.json", original)
	pkg, err := Load(root)
	if err != nil {
		t.Fatalf("valid native sidecar rejected: %v", err)
	}
	target, err := PrepareNativeTarget(t.Context(), t.TempDir(), "p", pkg, root, "claude")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(target.Root, ".claude-plugin/plugin.json"))
	if err != nil || string(body) != original {
		t.Fatal("verified author entry overwritten", err)
	}
	fixtureFile(t, root, ".mcp.json", `{"mcpServers":{"remote":{"type":"http","url":"https://other.test/mcp"}}}`)
	if _, err := Load(root); err == nil {
		t.Fatal("conflicting native MCP sidecar admitted")
	}
}

func TestExplicitAuthenticationTargetDoesNotAuthorizeOtherCLI(t *testing.T) {
	pkg := Package{Format: "agent-plugins", Integration: Integration{Authentication: &Authentication{Bindings: []AuthenticationBinding{{Runtime: "codex", Server: "remote", URL: "https://example.test/mcp", Header: "Authorization"}}, SkillBindings: []SkillAuthenticationBinding{{Runtime: "codex", Skill: "skills/review", EnvironmentKeys: []string{"TOKEN"}}}}}}
	for _, cli := range []string{"claude", "cursor", "workbuddy", "qoder", "kimi", "gemini", "grok", "opencode", "pi"} {
		if len(TargetAuthenticationBindings(pkg, cli)) != 0 || len(TargetSkillAuthenticationBindings(pkg, cli)) != 0 {
			t.Fatalf("explicit codex authorization broadened to %s", cli)
		}
	}
}

func TestConfiguredPluginPreparesProjectionTargetsWithoutSecrets(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "configured-projection", "extensions": map[string]any{"com.neuxbot": map[string]any{"schemaVersion": 1, "configuration": map[string]any{"fields": []map[string]any{{"key": "TOKEN", "label": "Token", "secret": true, "required": true}}, "bindings": []map[string]any{{"server": "remote", "target": "header", "name": "Authorization", "value": "Bearer ${TOKEN}"}}}}}})
	fixtureFile(t, root, "mcp.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"remote":{"type":"streamable-http","url":"https://example.test/mcp"}}}`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := PrepareTargets(t.Context(), t.TempDir(), "configured", pkg, root)
	if err != nil {
		t.Fatalf("valid configured components unavailable: %v", err)
	}
	if len(targets) != 10 {
		t.Fatal("configured plugin lost targets")
	}
	for _, target := range targets {
		if target.Format != "projection" || len(target.Servers) != 1 {
			t.Fatal("configured plugin mislabeled native", target)
		}
		body, err := os.ReadFile(filepath.Join(target.Root, target.ManifestPath))
		if err != nil {
			t.Fatal(err)
		}
		var descriptor map[string]any
		if json.Unmarshal(body, &descriptor) != nil || descriptor["schemaVersion"] != float64(1) || descriptor["format"] != "projection" {
			t.Fatal("missing explicit projection contract")
		}
	}
}

func TestNativeEntryAcceptsConventionalSkillDiscovery(t *testing.T) {
	root := t.TempDir()
	standardInput(t, root, map[string]any{"name": "convention"})
	fixtureFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\nReview")
	fixtureFile(t, root, ".claude-plugin/plugin.json", `{"name":"convention"}`)
	if _, err := Load(root); err != nil {
		t.Fatalf("valid native conventional skills rejected: %v", err)
	}
}
