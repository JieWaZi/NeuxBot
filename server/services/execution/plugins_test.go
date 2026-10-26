package execution

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// installedPluginFixture 只替换资源读取端口，内容解析和执行解析走真实实现。
type installedPluginFixture struct {
	// PluginController 保留本测试未消费的控制器端口。
	controllers.PluginController
	// states 是已固定关系和配置的同一事务快照。
	states []models.PluginState
}

func (f installedPluginFixture) InstalledStates(context.Context, string) ([]models.PluginState, error) {
	return f.states, nil
}

func executionPlugin(t *testing.T, data string, id string, skillText string, server bool) models.PluginState {
	t.Helper()
	source := t.TempDir()
	manifest := map[string]any{"$schema": constants.PluginManifestSchema, "name": "office", "version": "1.0.0"}
	if server {
		manifest["extensions"] = map[string]any{constants.PluginExtensionNamespace: map[string]any{"bindings": []any{map[string]any{"server": "office", "target": "env", "name": "API_TOKEN", "value": "${TOKEN}"}}}}
	}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if skillText != "" {
		if err := os.MkdirAll(filepath.Join(source, "skills", "office"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "skills", "office", "SKILL.md"), []byte("---\nname: office\ndescription: Office documents\n---\n"+skillText), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if server {
		if err := os.WriteFile(filepath.Join(source, "mcp.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"office":{"type":"stdio","command":"native-tool","args":["a b"],"env":{}}}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := plugins.Stage(filepath.Join(data, "plugins", "staging"), source)
	if err != nil {
		t.Fatal(err)
	}
	content, err := plugins.CopyContent(filepath.Join(data, "plugins", "content"), stage.Root, stage.Digest)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(data, content)
	if err != nil {
		t.Fatal(err)
	}
	version := models.PluginVersion{ID: id + "-v1", PluginID: id, Version: "1.0.0", ContentPath: relative, Digest: stage.Digest, Package: stage.Package, TrustedAt: 1}
	return models.PluginState{Plugin: models.Plugin{ID: id, Name: "办公"}, Versions: []models.PluginVersion{version}, Installations: []models.WakerPlugin{{WakerID: "w1", PluginID: id, VersionID: version.ID, Enabled: true}}, Configuration: models.PluginConfiguration{PluginID: id, Revision: 3, Values: map[string]string{}, Secrets: []models.MCPSecretDefinition{{Name: "TOKEN", Value: "private-token", Configured: true}}}}
}

func TestPluginAdmissionPinsResourcesAndDoesNotSelectEveryComponent(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", true)
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginLauncher: "/core"}
	detection := runtimes.DetectionResult{State: runtimes.DetectionStateReady, Capabilities: runtimes.CapabilitySet{runtimes.CapabilitySkillDirectory: true}}
	prepared, err := s.preparePlugins(context.Background(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"p1"}}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.skills) != 1 || prepared.skills[0].Selected || len(prepared.connectors) != 1 {
		t.Fatalf("components: %+v", prepared)
	}
	connector := prepared.connectors[0]
	if connector.Command != "/core" || connector.Arguments[0] != constants.PluginStdioMode || connector.RuntimeKey != "ally_plugin_"+connector.ID {
		t.Fatalf("stdio launch: %+v", connector)
	}
	if len(prepared.snapshots) != 1 || prepared.snapshots[0].VersionID != "p1-v1" || prepared.snapshots[0].ConfigurationRevision != 3 || len(prepared.selected) != 1 {
		t.Fatalf("snapshot: %+v", prepared)
	}
	body, _ := json.Marshal(prepared.snapshots)
	if strings.Contains(string(body), "private-token") {
		t.Fatal("snapshot leaked credential")
	}
	state.Installations[0].Enabled = false
	if _, err = s.preparePlugins(context.Background(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"p1"}}}, detection); !errors.Is(err, status_error.PluginNotReady) {
		t.Fatalf("disabled explicit reference accepted: %v", err)
	}
}

func TestPluginSkillCollisionDeduplicatesOnlyEqualContent(t *testing.T) {
	data := t.TempDir()
	a := executionPlugin(t, data, "a", "first", false)
	a.Configuration.Secrets = nil
	b := executionPlugin(t, data, "b", "second", false)
	b.Configuration.Secrets = nil
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{a, b}}, skillDataDirectory: data}
	detection := runtimes.DetectionResult{State: runtimes.DetectionStateReady, Capabilities: runtimes.CapabilitySet{runtimes.CapabilitySkillDirectory: true}}
	result, err := s.preparePlugins(context.Background(), "w1", pendingMessage{}, detection)
	if err != nil {
		t.Fatal(err)
	}
	defer result.release()
	if _, err = mergePluginSkills(nil, result.skills); !errors.Is(err, status_error.PluginSkillConflict) {
		t.Fatalf("different same-name skills accepted: %v", err)
	}
	if merged, err := mergePluginSkills(nil, append(result.skills[:1], result.skills[0])); err != nil || len(merged) != 1 {
		t.Fatalf("identical skills not deduplicated: %v %v", merged, err)
	}
}

func TestPluginRetryRejectsChangedConfiguration(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", false)
	state.Configuration.Secrets = nil
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
	_, err := s.preparePlugins(context.Background(), "w1", pendingMessage{retry: true, pluginSnapshots: []plugins.Snapshot{{ID: "p1", VersionID: "p1-v1", ConfigurationRevision: 2}}}, runtimes.DetectionResult{})
	if !errors.Is(err, status_error.PluginConfigurationConflict) {
		t.Fatalf("retry silently changed config: %v", err)
	}
}

func TestPluginRestrictedEntryFiltersMCPAndRejectsProviderCLI(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", true)
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginLauncher: "/core"}
	detection := runtimes.DetectionResult{State: runtimes.DetectionStateReady, Capabilities: runtimes.CapabilitySet{runtimes.CapabilitySkillDirectory: true}}
	empty := []string{}
	result, err := s.preparePlugins(context.Background(), "w1", pendingMessage{allowedConnectorIDs: &empty}, detection)
	if err != nil {
		t.Fatal(err)
	}
	result.release()
	if len(result.connectors) != 0 || len(result.skills) != 1 {
		t.Fatalf("MCP whitelist bypass: %+v", result)
	}
	extension := json.RawMessage(`{"provider":"feishu"}`)
	state.Versions[0].Package.Manifest.Extensions = map[string]json.RawMessage{constants.PluginExtensionNamespace: extension}
	state.Configuration.Secrets = nil
	s.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	result, err = s.preparePlugins(context.Background(), "w1", pendingMessage{allowedConnectorIDs: &empty}, detection)
	if err != nil {
		t.Fatal(err)
	}
	result.release()
	if len(result.skills) > 0 || len(result.environment) > 0 {
		t.Fatal("restricted entry received CLI skills or provider environment")
	}
	if _, err = s.preparePlugins(context.Background(), "w1", pendingMessage{allowedConnectorIDs: &empty, message: userMessage{PluginIDs: []string{"p1"}}}, detection); !errors.Is(err, status_error.PluginManifestInvalid) {
		t.Fatalf("explicit provider bypass: %v", err)
	}
}

func TestPluginIntentAndVersionSurviveAdmissionAndHistory(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "", true)
	state.Installations[0].WakerID = "waker-1"
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime, messageTestOptions{plugins: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data})
	manager.execution.(*service).pluginLauncher = "/core"
	intent := WorkIntent{ClientRequestID: "plugin-intent", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "整理文档", PluginIDs: []string{"p1"}}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: runtimes.PermissionLevelDefault}}
	accepted, err := manager.execution.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	snapshot, err := manager.execution.(*service).journal.LoadExecutionContext(context.Background(), accepted.Chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.PreviousMessages) < 1 || len(snapshot.PreviousMessages[0].Plugins) != 1 || snapshot.PreviousMessages[0].Plugins[0].VersionID != "p1-v1" {
		t.Fatalf("history lost plugin snapshot: %+v", snapshot.PreviousMessages)
	}
	records, err := manager.execution.(*service).journal.ReadRecords(accepted.Chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	recorded, _, err := executionFromRecords(records, accepted.Execution.ID)
	if err != nil || len(recorded.PluginResources) != 1 || recorded.PluginResources[0].ConfigurationRevision != 3 {
		t.Fatalf("execution lost immutable resources: %+v %v", recorded, err)
	}
	replay, err := manager.execution.Start(context.Background(), intent)
	if err != nil || replay.Chat.ID != accepted.Chat.ID {
		t.Fatalf("retry mismatch: %+v %v", replay, err)
	}
	intent.Input.PluginIDs = []string{"changed"}
	if _, err = manager.execution.Start(context.Background(), intent); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("plugin intent absent from idempotency hash: %v", err)
	}
	if len(runtime.requests) != 1 || !strings.Contains(runtime.requests[0].Message.Text, "p1") {
		t.Fatalf("explicit intent missing: %+v", runtime.requests)
	}
}

func TestDeduplicatedPluginSkillKeepsLeasedImmutableSource(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", false)
	version := state.Versions[0]
	root, err := plugins.ContentPath(data, version.ContentPath)
	if err != nil {
		t.Fatal(err)
	}
	derived := runtimes.Skill{Name: "office", Path: filepath.Join(root, "skills", "office")}
	content, err := os.ReadFile(filepath.Join(derived.Path, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	independent := t.TempDir()
	if err := os.WriteFile(filepath.Join(independent, "SKILL.md"), content, 0600); err != nil {
		t.Fatal(err)
	}
	merged, err := mergePluginSkills([]runtimes.Skill{{Name: "office", Path: independent, Selected: true}}, []runtimes.Skill{derived})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || !merged[0].Selected || merged[0].Path != derived.Path {
		t.Fatalf("dedup lost selection or immutable path: %+v", merged)
	}
	if err := os.RemoveAll(independent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(merged[0].Path, "SKILL.md")); err != nil {
		t.Fatal("independent resource deletion broke plugin snapshot", err)
	}
}

func TestIndependentSkillDuplicatesUseTheSameContentRule(t *testing.T) {
	makeSkill := func(body string) runtimes.Skill {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return runtimes.Skill{Name: "same", Path: root}
	}
	first, second := makeSkill("same body"), makeSkill("same body")
	second.Selected = true
	merged, err := mergePluginSkills([]runtimes.Skill{first, second}, nil)
	if err != nil || len(merged) != 1 || !merged[0].Selected {
		t.Errorf("same independent content did not merge: %+v %v", merged, err)
	}
	if _, err := mergePluginSkills([]runtimes.Skill{first, makeSkill("different body")}, nil); !errors.Is(err, status_error.PluginSkillConflict) {
		t.Errorf("conflicting independent content admitted: %v", err)
	}
}

func TestPluginAdmissionPreservesLegacyNestedPackageRoot(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "legacy", "legacy skill", true)
	version := &state.Versions[0]
	oldRoot := filepath.Join(data, version.ContentPath)
	repository := t.TempDir()
	packageRoot := filepath.Join(repository, "plugins", "office")
	if err := os.MkdirAll(filepath.Dir(packageRoot), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldRoot, packageRoot); err != nil {
		t.Fatal(err)
	}
	archive := packagefiles.Archive{PreserveExecutable: true}
	digest, err := archive.Digest(repository)
	if err != nil {
		t.Fatal(err)
	}
	content, err := plugins.CopyContent(filepath.Join(data, "plugins", "content"), repository, digest)
	if err != nil {
		t.Fatal(err)
	}
	version.ContentPath, err = filepath.Rel(data, content)
	if err != nil {
		t.Fatal(err)
	}
	version.Digest = digest
	raw, err := json.Marshal(version.Package)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot["sourcePath"] = json.RawMessage(`"plugins/office"`)
	raw, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &version.Package); err != nil {
		t.Fatal(err)
	}
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginLauncher: "/core"}
	detection := runtimes.DetectionResult{State: runtimes.DetectionStateReady, Capabilities: runtimes.CapabilitySet{runtimes.CapabilitySkillDirectory: true}}
	prepared, err := s.preparePlugins(context.Background(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"legacy"}}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.skills) != 1 || len(prepared.connectors) != 1 {
		t.Fatalf("components: %+v", prepared)
	}
	if _, err := os.Stat(filepath.Join(prepared.skills[0].Path, "SKILL.md")); err != nil {
		t.Fatalf("legacy skill was delivered outside package root: %v", err)
	}
	actualRoot, err := filepath.EvalSymlinks(filepath.Join(content, "plugins", "office"))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.connectors[0].Arguments[2] != actualRoot {
		t.Fatalf("legacy MCP cwd = %q, want %q", prepared.connectors[0].Arguments[2], actualRoot)
	}
}

func TestClaudeNativePluginDoesNotAlsoProjectSkills(t *testing.T) {
	data := t.TempDir()
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".claude-plugin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".claude-plugin/plugin.json"), []byte(`{"name":"native","version":"2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "skills/probe"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skills/probe/SKILL.md"), []byte("---\nname: probe\ndescription: Probe native loading\n---\nProbe"), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := plugins.Stage(filepath.Join(data, "plugins/staging"), source)
	if err != nil {
		t.Fatal(err)
	}
	root, err := plugins.CopyContent(filepath.Join(data, "plugins/content"), stage.Root, stage.Digest)
	if err != nil {
		t.Fatal(err)
	}
	relative, _ := filepath.Rel(data, root)
	version := models.PluginVersion{ID: "v", PluginID: "p", Version: "2", Digest: stage.Digest, ContentPath: relative, Package: stage.Package, TrustedAt: 1}
	state := models.PluginState{Plugin: models.Plugin{ID: "p"}, Versions: []models.PluginVersion{version}, Installations: []models.WakerPlugin{{WakerID: "w", PluginID: "p", VersionID: "v", Enabled: true}}, Configuration: models.PluginConfiguration{Values: map[string]string{}}}
	manager := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
	detection := runtimes.DetectionResult{Runtime: runtimes.RuntimeSummary{BaseRuntimeID: runtimes.RuntimeIDClaude}, Version: "2.1.159", Capabilities: runtimes.CapabilitySet{runtimes.CapabilitySkillDirectory: true}}
	prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{}, detection)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.native) != 1 || len(prepared.skills) != 0 || len(prepared.connectors) != 0 {
		t.Fatalf("duplicate or missing native delivery: %+v", prepared)
	}
	retryDetection := detection
	retryDetection.Version = ""
	if replay, err := manager.preparePlugins(context.Background(), "w", pendingMessage{pluginSnapshots: prepared.snapshots}, retryDetection); err == nil {
		replay.release()
		t.Fatal("native retry silently became Skill projection")
	}
	if err := os.WriteFile(filepath.Join(data, "plugins/native"), []byte("block target generation"), 0600); err != nil {
		t.Fatal(err)
	}
	codex := detection
	codex.Runtime.BaseRuntimeID = runtimes.RuntimeIDCodex
	codex.Version = "0.161.0"
	if fallback, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, codex); err == nil {
		fallback.release()
		t.Fatal("native preparation failure silently became Skill projection")
	}

}

func TestNativeScopeTracksContentAndIgnoresOrdering(t *testing.T) {
	a := plugins.Snapshot{ID: "a", VersionID: "v1", Delivery: "native", TargetDigest: "one"}
	b := plugins.Snapshot{ID: "b", VersionID: "v2", Delivery: "native", TargetDigest: "two"}
	if nativePluginScope([]plugins.Snapshot{a, b}) != nativePluginScope([]plugins.Snapshot{b, a}) {
		t.Fatal("reordering changed native scope")
	}
	changed := a
	changed.TargetDigest = "changed"
	if nativePluginScope([]plugins.Snapshot{a}) == nativePluginScope([]plugins.Snapshot{changed}) || nativePluginScope([]plugins.Snapshot{a}) == nativePluginScope(nil) {
		t.Fatal("native content change kept old session scope")
	}
}

func TestUnknownOrOlderNativeCLIUsesProjection(t *testing.T) {
	for _, item := range []struct {
		// cli 是实际目标。
		cli string
		// version 是探测版本。
		version string
		// supported 表示固定原生契约是否可用。
		supported bool
	}{{"codex", "codex-cli 0.161.0", true}, {"claude", "2.1.159 (Claude Code)", true}, {"codex", "0.100.0", false}, {"claude", "", false}, {"kimi", "2.1.159", false}} {
		if nativePluginVersionSupported(item.cli, item.version) != item.supported {
			t.Fatalf("wrong capability: %+v", item)
		}
	}
}

func TestNativeTargetConfigurationIsValidatedAndOnlySelectedSecretsInjected(t *testing.T) {
	data, source := t.TempDir(), t.TempDir()
	write := func(path, content string) {
		t.Helper()
		path = filepath.Join(source, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(".codex-plugin/plugin.json", `{"name":"logical","version":"1"}`)
	write(".neuxally/integration.json", `{"nativePlugins":{"codex":{"path":"codex"},"claude":{"path":"claude"}}}`)
	write("codex/.codex-plugin/plugin.json", `{"name":"target","version":"1","mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp"}}}`)
	write("claude/.claude-plugin/plugin.json", `{"name":"target","version":"1"}`)
	write("claude/.mcp.json", `{"service":{"type":"http","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer ${TARGET_TOKEN}"}}}`)
	stage, err := plugins.Stage(filepath.Join(data, "plugins/staging"), source)
	if err != nil {
		t.Fatal(err)
	}
	root, err := plugins.CopyContent(filepath.Join(data, "plugins/content"), stage.Root, stage.Digest)
	if err != nil {
		t.Fatal(err)
	}
	relative, _ := filepath.Rel(data, root)
	version := models.PluginVersion{ID: "v", PluginID: "p", Version: "1", Digest: stage.Digest, ContentPath: relative, Package: stage.Package, TrustedAt: 1}
	state := models.PluginState{Plugin: models.Plugin{ID: "p"}, Versions: []models.PluginVersion{version}, Installations: []models.WakerPlugin{{WakerID: "w", PluginID: "p", VersionID: "v", Enabled: true}}, Configuration: models.PluginConfiguration{Values: map[string]string{}}}
	manager := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
	detection := runtimes.DetectionResult{Runtime: runtimes.RuntimeSummary{BaseRuntimeID: runtimes.RuntimeIDCodex}, Version: "0.161.0"}
	request := pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}
	prepared, err := manager.preparePlugins(context.Background(), "w", request, detection)
	if err != nil {
		t.Fatalf("unrelated Claude required config blocked Codex: %v", err)
	}
	prepared.release()
	if len(prepared.native) != 1 || prepared.environment["TARGET_TOKEN"] != "" {
		t.Fatalf("unexpected Codex delivery: %+v", prepared)
	}
	detection.Runtime.BaseRuntimeID = runtimes.RuntimeIDClaude
	detection.Version = "2.1.159"
	if result, err := manager.preparePlugins(context.Background(), "w", request, detection); err == nil {
		result.release()
		t.Fatal("target missing secret accepted")
	}
	state.Configuration.Secrets = []models.MCPSecretDefinition{{Name: "TARGET_TOKEN", Value: "configured-secret", Configured: true}}
	manager.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	prepared, err = manager.preparePlugins(context.Background(), "w", request, detection)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.native) != 1 || len(prepared.connectors) != 0 || prepared.environment["TARGET_TOKEN"] != "configured-secret" {
		t.Fatalf("target native secret not privately injected: %+v", prepared)
	}
}

func TestNativeConfigurationRejectsHostAndModelIdentityKeys(t *testing.T) {
	for _, key := range []string{"HOME", "CODEX_HOME", "PATH", "NODE_OPTIONS", "ALLY_EXECUTION_ID", "ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY", "GEMINI_API_KEY", "XDG_CONFIG_HOME", "USERPROFILE"} {
		if nativeConfigurationKeyAllowed(key) {
			t.Fatalf("host environment override accepted: %s", key)
		}
	}
	if !nativeConfigurationKeyAllowed("GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Fatal("target credential unexpectedly rejected")
	}
}

func TestPluginAdmissionRejectsLegacyProviderWithoutDeliveringComponents(t *testing.T) {
	for _, provider := range []string{"feishu", "github", "unsupported"} {
		t.Run(provider, func(t *testing.T) {
			data := t.TempDir()
			state := executionPlugin(t, data, "legacy", "legacy capability", false)
			state.Configuration.Secrets = nil
			raw, _ := json.Marshal(map[string]any{"provider": provider})
			state.Versions[0].Package.Manifest.Extensions = map[string]json.RawMessage{constants.PluginExtensionNamespace: raw}
			s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
			detection := runtimes.DetectionResult{State: runtimes.DetectionStateReady, Capabilities: runtimes.CapabilitySet{runtimes.CapabilitySkillDirectory: true}}
			result, err := s.preparePlugins(context.Background(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"legacy"}}}, detection)
			if !errors.Is(err, status_error.PluginManifestInvalid) {
				t.Fatalf("legacy provider accepted: %v", err)
			}
			if len(result.skills) != 0 || len(result.connectors) != 0 || len(result.environment) != 0 {
				t.Fatalf("legacy capability delivered: %+v", result)
			}
			result, err = s.preparePlugins(context.Background(), "w1", pendingMessage{}, detection)
			if err != nil || len(result.snapshots) != 0 {
				t.Fatalf("automatic legacy capability delivered: %+v %v", result, err)
			}
			result.release()
		})
	}
}
