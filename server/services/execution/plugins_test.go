package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"github.com/JieWaZi/NeuxBot/server/internal/packagefiles"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
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

func (f installedPluginFixture) ExecutionDeclarations(_ context.Context, waker string) ([]models.PluginExecutionDeclaration, error) {
	result := []models.PluginExecutionDeclaration{}
	for _, state := range f.states {
		for _, installation := range state.Installations {
			if installation.WakerID != waker || !installation.Enabled {
				continue
			}
			for _, version := range state.Versions {
				if version.ID == installation.VersionID {
					configured := []string{}
					for _, secret := range state.Configuration.Secrets {
						if secret.Configured {
							configured = append(configured, secret.Name)
						}
					}
					result = append(result, models.PluginExecutionDeclaration{ID: state.Plugin.ID, Name: state.Plugin.Name, VersionID: version.ID, Digest: version.Digest, TrustedAt: version.TrustedAt, ConfigurationRevision: state.Configuration.Revision, AccountGeneration: state.Account.Generation, ContentPath: version.ContentPath, Package: version.Package, Values: state.Configuration.Values, ConfiguredSecrets: configured})
				}
			}
		}
	}
	return result, nil
}

func (f installedPluginFixture) Version(_ context.Context, id string) (models.PluginVersion, error) {
	for _, state := range f.states {
		for _, version := range state.Versions {
			if version.ID == id {
				return version, nil
			}
		}
	}
	return models.PluginVersion{}, status_error.PluginNotFound
}

func (f installedPluginFixture) ExecutionConfiguration(ctx context.Context, id string) (models.PluginConfiguration, error) {
	return f.Configuration(ctx, id)
}

func (f installedPluginFixture) Configuration(_ context.Context, id string) (models.PluginConfiguration, error) {
	for _, state := range f.states {
		if state.Plugin.ID == id {
			return state.Configuration, nil
		}
	}
	return models.PluginConfiguration{PluginID: id}, nil
}

func executionPlugin(t *testing.T, data string, id string, skillText string, server bool) models.PluginState {
	t.Helper()
	source := t.TempDir()
	manifest := map[string]any{"$schema": constants.PluginManifestSchema, "name": "office", "version": "1.0.0"}
	if server {
		manifest["extensions"] = map[string]any{constants.PluginExtensionNamespace: map[string]any{"schemaVersion": constants.PluginExtensionSchemaVersion, "configuration": map[string]any{"fields": []any{map[string]any{"key": "TOKEN", "secret": true, "required": true}}, "bindings": []any{map[string]any{"server": "office", "target": "env", "name": "API_TOKEN", "value": "${TOKEN}"}}}}}
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
	secrets := []models.MCPSecretDefinition{}
	if server {
		secrets = append(secrets, models.MCPSecretDefinition{Name: "TOKEN", Value: "private-token", Configured: true})
	}
	return models.PluginState{Plugin: models.Plugin{ID: id, Name: "办公"}, Versions: []models.PluginVersion{version}, Installations: []models.WakerPlugin{{WakerID: "w1", PluginID: id, VersionID: version.ID, Enabled: true}}, Configuration: models.PluginConfiguration{PluginID: id, Revision: 3, Values: map[string]string{}, Secrets: secrets}}
}

func TestPluginAdmissionPinsResourcesAndDoesNotSelectEveryComponent(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", true)
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginLauncher: "/core"}
	detection := agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
	prepared, err := s.preparePlugins(context.Background(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"p1"}}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.skills) != 1 || len(prepared.connectors) != 1 {
		t.Fatalf("components: %+v", prepared)
	}
	connector := prepared.connectors[0]
	if connector.Command != "/core" || connector.Arguments[0] != constants.PluginStdioMode || connector.RuntimeKey != "ally_plugin_"+connector.ID {
		t.Fatalf("stdio launch: %+v", connector)
	}
	if len(prepared.snapshots) != 1 || prepared.snapshots[0].VersionID != "p1-v1" || prepared.snapshots[0].ConfigurationRevision != 3 {
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

func TestPluginResourcesSurviveFailedProbeWithoutReplacingSessionIdentity(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", true)
	core := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginLauncher: "/core"}
	request := pendingMessage{message: userMessage{PluginIDs: []string{"p1"}}}
	detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, State: agentapi.DetectionStateReady, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true, agentapi.CapabilityMCPStreamableHTTP: true}}
	ready, err := core.freezePluginResources(t.Context(), "w1", request, detection)
	if err != nil {
		t.Fatal(err)
	}
	detection.State, detection.Capabilities = agentapi.DetectionStateUnavailable, nil
	failed, err := core.freezePluginResources(t.Context(), "w1", request, detection)
	if err != nil {
		t.Fatal(err)
	}
	if failed.key != ready.key {
		t.Error("diagnostic probe changed immutable resource identity")
	}
	selected, err := core.selectNativePluginSkills(t.Context(), failed)
	if err != nil || len(selected) != 1 || selected[0].Name != "office" {
		t.Fatalf("probe failure lost declared plugin skill: %+v %v", selected, err)
	}
	actual, err := core.acquirePluginResources(t.Context(), failed)
	if actual.release != nil {
		defer actual.release()
	}
	if err != nil || len(actual.skills) != 1 || len(actual.connectors) != 1 {
		t.Fatalf("probe failure lost plugin resources: %+v %v", actual, err)
	}
	if actual.connectors[0].Transport != constants.MCPTransportStdio || actual.connectors[0].Command != "/core" {
		t.Fatalf("plugin connector was not delivered through its configured launcher: %+v", actual.connectors[0])
	}
}

func TestPluginTargetDigestReusesVerifiedSourceOnlyForSameRoot(t *testing.T) {
	digest := "verified-source-digest"
	root := filepath.Join(t.TempDir(), "removed-after-verification")
	actual, err := pluginTargetDigest(root, root, digest)
	if err != nil || actual != digest {
		t.Fatalf("same verified root digest = %q, %v; want %q", actual, err, digest)
	}
	if _, err := pluginTargetDigest(root, filepath.Join(root, "different-target"), digest); err == nil {
		t.Fatal("different target skipped its own content scan")
	}
}

func TestPluginSkillCollisionDeduplicatesOnlyEqualContent(t *testing.T) {
	data := t.TempDir()
	a := executionPlugin(t, data, "a", "first", false)
	a.Configuration.Secrets = nil
	b := executionPlugin(t, data, "b", "second", false)
	b.Configuration.Secrets = nil
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{a, b}}, skillDataDirectory: data}
	detection := agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
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

func TestPluginRestrictedEntryFiltersMCPAndRejectsProviderCLI(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", true)
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginLauncher: "/core"}
	detection := agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
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
	if !errors.Is(err, status_error.PluginManifestInvalid) {
		t.Fatal("legacy contract was not rejected", err)
	}
	if result.release != nil {
		result.release()
	}
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
	manager := newMessageTestService(t, t.TempDir(), messageTestOptions{plugins: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data})
	manager.execution.(*service).pluginLauncher = "/core"
	intent := WorkIntent{ClientRequestID: "plugin-intent", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "整理文档", PluginIDs: []string{"p1"}}, Options: WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: agentapi.PermissionLevelDefault}}
	accepted, err := manager.execution.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	snapshot, err := manager.chats.(chatservice.Module).ReadMessageHistory(context.Background(), accepted.Chat.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Resources) < 1 || len(snapshot.Resources[0].Plugins) != 1 || snapshot.Resources[0].Plugins[0].VersionID != "p1-v1" {
		t.Fatalf("history lost plugin snapshot: %+v", snapshot.Resources)
	}
	replay, err := manager.execution.Start(context.Background(), intent)
	if err != nil || replay.Chat.ID != accepted.Chat.ID {
		t.Fatalf("retry mismatch: %+v %v", replay, err)
	}
	intent.Input.PluginIDs = []string{"changed"}
	if _, err = manager.execution.Start(context.Background(), intent); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("plugin intent absent from idempotency hash: %v", err)
	}
	if len(manager.nativeCalls(t, "turn/start")) != 1 || !strings.Contains(string(manager.nativeCalls(t, "turn/start")[0]), "p1") {
		t.Fatalf("explicit intent missing from native frame: %+v", manager.nativeCalls(t, "turn/start"))
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
	derived := agentapi.Skill{Name: "office", Path: filepath.Join(root, "skills", "office"), Digest: version.Package.Skills[0].Digest}
	content, err := os.ReadFile(filepath.Join(derived.Path, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	independent := t.TempDir()
	if err := os.WriteFile(filepath.Join(independent, "SKILL.md"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(independent, independent+"-offline"); err != nil {
		t.Fatal(err)
	}
	merged, err := mergePluginSkills([]agentapi.Skill{{Name: "office", Path: independent, Digest: derived.Digest}}, []agentapi.Skill{derived})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || merged[0].Path != derived.Path {
		t.Fatalf("dedup lost selection or immutable path: %+v", merged)
	}
	if err := os.RemoveAll(independent + "-offline"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(merged[0].Path, "SKILL.md")); err != nil {
		t.Fatal("independent resource deletion broke plugin snapshot", err)
	}
}

func TestIndependentSkillDuplicatesUseTheSameContentRule(t *testing.T) {
	makeSkill := func(body string) agentapi.Skill {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		digest, err := plugins.ContentDigest(root)
		if err != nil {
			t.Fatal(err)
		}
		return agentapi.Skill{Name: "same", Path: root, Digest: digest}
	}
	first, second := makeSkill("same body"), makeSkill("same body")
	merged, err := mergePluginSkills([]agentapi.Skill{first, second}, nil)
	if err != nil || len(merged) != 1 {
		t.Errorf("same independent content did not merge: %+v %v", merged, err)
	}
	if _, err := mergePluginSkills([]agentapi.Skill{first, makeSkill("different body")}, nil); !errors.Is(err, status_error.PluginSkillConflict) {
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
	detection := agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
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

func TestPluginConfigurationIsValidatedAndOnlyBoundSecretsInjected(t *testing.T) {
	for _, cli := range []string{"codex", "claude"} {
		t.Run(cli, func(t *testing.T) {
			data, source := t.TempDir(), t.TempDir()
			manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"configured","version":"1","extensions":{"com.neuxbot":{"schemaVersion":1,"configuration":{"fields":[{"key":"TARGET_TOKEN","secret":true,"required":true},{"key":"UNUSED_TOKEN","secret":true}],"bindings":[{"server":"service","target":"header","name":"Authorization","value":"Bearer ${TARGET_TOKEN}"}]}}}}`
			if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(manifest), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "mcp.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"service":{"type":"streamable-http","url":"https://example.invalid/mcp"}}}`), 0600); err != nil {
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
			version := models.PluginVersion{ID: "v", PluginID: "p", Version: "1", Digest: stage.Digest, ContentPath: relative, Package: stage.Package, TrustedAt: 1}
			state := models.PluginState{Plugin: models.Plugin{ID: "p"}, Versions: []models.PluginVersion{version}, Installations: []models.WakerPlugin{{WakerID: "w", PluginID: "p", VersionID: "v", Enabled: true}}, Configuration: models.PluginConfiguration{Values: map[string]string{}}}
			manager := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
			detection := authenticatedPluginDetection(cli, map[string]string{"codex": "0.161.0", "claude": "2.1.159"}[cli])
			request := pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}
			if result, err := manager.preparePlugins(context.Background(), "w", request, detection); !errors.Is(err, status_error.PluginNotReady) {
				if result.release != nil {
					result.release()
				}
				t.Fatal("missing required secret accepted", err)
			}
			state.Configuration.Secrets = []models.MCPSecretDefinition{{Name: "TARGET_TOKEN", Value: "configured-secret", Configured: true}, {Name: "UNUSED_TOKEN", Value: "unused-secret", Configured: true}}
			manager.pluginController = installedPluginFixture{states: []models.PluginState{state}}
			prepared, err := manager.preparePlugins(context.Background(), "w", request, detection)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.release()
			if len(prepared.connectors) != 1 || len(prepared.environment) != 0 {
				t.Fatal("explicit configuration binding did not use scoped projection")
			}
			headers := prepared.connectors[0].Headers
			if len(headers) != 1 || headers[0].Name != "Authorization" || headers[0].Value != "Bearer configured-secret" {
				t.Fatal("only the declared secret should reach its MCP header")
			}
			encoded, _ := json.Marshal(prepared.snapshots)
			if strings.Contains(string(encoded), "configured-secret") || strings.Contains(string(encoded), "unused-secret") {
				t.Fatal("snapshot leaked private configuration")
			}
			if err := plugins.VerifyContent(root, version.Digest); err != nil {
				t.Fatal("configuration modified immutable source", err)
			}
		})
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
			detection := agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
			result, err := s.preparePlugins(context.Background(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"legacy"}}}, detection)
			if !errors.Is(err, status_error.PluginManifestInvalid) {
				t.Fatalf("legacy provider accepted: %v", err)
			}
			if len(result.skills) != 0 || len(result.connectors) != 0 || len(result.environment) != 0 {
				t.Fatalf("legacy capability delivered: %+v", result)
			}
			result, err = s.preparePlugins(context.Background(), "w1", pendingMessage{}, detection)
			if !errors.Is(err, status_error.PluginManifestInvalid) || len(result.snapshots) != 0 || len(result.skills) != 0 || len(result.connectors) != 0 || len(result.environment) != 0 {
				t.Fatalf("automatic legacy capability was not rejected: %+v %v", result, err)
			}
			if result.release != nil {
				result.release()
			}
		})
	}
}

func TestFreezePluginResourcesDefersContentPreparationAndPinsConfiguration(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", false)
	state.Configuration.Secrets = nil
	s := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
	detection := agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
	plan, err := s.freezePluginResources(context.Background(), "w1", pendingMessage{}, detection)
	if err != nil {
		t.Fatal(err)
	}
	// 尚未接纳的冷准备必须重核当前安装，不能继续取得已经停用的版本。
	state.Installations[0].Enabled = false
	prepared, err := s.acquirePluginResources(context.Background(), plan)
	if prepared.release != nil {
		defer prepared.release()
	}
	if !errors.Is(err, status_error.PluginVersionConflict) || len(prepared.snapshots) != 0 {
		t.Fatalf("cold preparation acquired a disabled installation: %+v %v", prepared.snapshots, err)
	}
	if err := os.RemoveAll(filepath.Join(data, state.Versions[0].ContentPath)); err != nil {
		t.Fatal(err)
	}
	state.Installations[0].Enabled = true
	plan, err = s.freezePluginResources(context.Background(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"p1"}}}, detection)
	if err != nil {
		t.Fatalf("freezing performed content work: %v", err)
	}
	if _, err = s.acquirePluginResources(context.Background(), plan); err == nil {
		t.Fatal("acquiring missing immutable content succeeded")
	}
}

// TestHotPluginDeclarationsIgnoreManagementPayloads 防止热声明解码配置、旧版本或展开其他伙伴关系。
func TestHotPluginDeclarationsIgnoreManagementPayloads(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "plugins.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	db := database.DB
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, resource := range []models.Plugin{{ID: "active", Name: "办公", SourceKey: "active"}, {ID: "disabled", Name: "停用", SourceKey: "disabled"}} {
		if err := db.Create(&resource).Error; err != nil {
			t.Fatal(err)
		}
	}
	versions := []models.PluginVersion{{ID: "pinned", PluginID: "active", Version: "1", Digest: "fixed-digest", TrustedAt: 7}, {ID: "disabled-version", PluginID: "disabled", Version: "1"}}
	for index := 0; index < 150; index++ {
		versions = append(versions, models.PluginVersion{ID: fmt.Sprintf("old-%03d", index), PluginID: "active", Version: fmt.Sprintf("old-%03d", index)})
	}
	if err := db.Create(&versions).Error; err != nil {
		t.Fatal(err)
	}
	// 未选历史版本不可解码，当前版本只读取目标与组件声明。
	if err := db.Model(&models.PluginVersion{}).Where("plugin_id = ? AND id <> ?", "active", "pinned").Update("package", "invalid-package-json").Error; err != nil {
		t.Fatal(err)
	}
	installations := []models.WakerPlugin{{ID: "mine", PluginID: "active", VersionID: "pinned", WakerID: "w1", Enabled: true}, {ID: "off", PluginID: "disabled", VersionID: "disabled-version", WakerID: "w1", Enabled: false}}
	for index := 0; index < 150; index++ {
		installations = append(installations, models.WakerPlugin{ID: fmt.Sprintf("other-%03d", index), PluginID: "active", VersionID: fmt.Sprintf("old-%03d", index), WakerID: fmt.Sprintf("other-%03d", index), Enabled: true})
	}
	if err := db.Create(&installations).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"active", "disabled"} {
		if err := db.Create(&models.PluginConfiguration{PluginID: id, Revision: 3, Values: map[string]string{}, Secrets: []models.MCPSecretDefinition{}}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.PluginConfiguration{}).Where("plugin_id = ?", id).Updates(map[string]any{"secrets": `[{"name":"TOKEN","configured":true,"ciphertext":"damaged"}]`, "values": `{}`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"active", "disabled"} {
		if err := db.Create(&models.PluginAccount{PluginID: id, Generation: 7}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&models.PluginAccount{}).Where("plugin_id = ?", id).Update("delivery_materials", "invalid-material-json").Error; err != nil {
			t.Fatal(err)
		}
	}
	rows := map[string]int64{}
	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("plugin-declaration-budget", func(tx *gorm.DB) {
		if tx.DryRun {
			return
		}
		queries++
		rows[tx.Statement.Table] += tx.RowsAffected
		for _, forbidden := range []string{"package", "secrets", "delivery_materials", "avatar"} {
			if len(tx.Statement.Selects) == 0 || slices.Contains(tx.Statement.Selects, forbidden) {
				t.Errorf("hot declaration reads management payload: table=%s selects=%v", tx.Statement.Table, tx.Statement.Selects)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("plugin-declaration-budget")
	core := &service{pluginController: controllers.NewPluginController(db)}
	request := pendingMessage{message: userMessage{PluginIDs: []string{"active"}}}
	frozen, err := core.freezePluginResources(context.Background(), "w1", request, agentapi.DetectionResult{})
	if err != nil {
		t.Fatalf("hot declaration accessed unused payload: %v", err)
	}
	if !slices.Equal(frozen.pluginIDs, []string{"active"}) || len(frozen.selected) != 1 || frozen.selected[0].Name != "办公" {
		t.Fatalf("wrong enabled fixed resources: %+v", frozen)
	}
	if len(frozen.declarations) != 1 || frozen.declarations[0].VersionID != "pinned" || frozen.declarations[0].Digest != "fixed-digest" || frozen.declarations[0].TrustedAt != 7 || frozen.declarations[0].ConfigurationRevision != 3 || frozen.declarations[0].AccountGeneration != 7 {
		t.Fatalf("static declaration lost fixed version, trust or revision: %+v", frozen.declarations)
	}
	if !slices.Equal(frozen.declarations[0].ConfiguredSecrets, []string{"TOKEN"}) {
		t.Fatalf("hot declaration lost configured secret presence: %+v", frozen.declarations[0])
	}
	if queries != 5 || rows["waker_plugins"] != 1 || rows["plugin_versions"] != 1 || rows["plugins"] != 1 || rows["plugin_configurations"] != 1 || rows["plugin_accounts"] != 1 || len(rows) != 5 {
		t.Fatalf("hot declaration expanded management data: queries=%d rows=%v", queries, rows)
	}
	if err := db.Model(&models.PluginConfiguration{}).Where("plugin_id = ?", "active").Update("revision", 4).Error; err != nil {
		t.Fatal(err)
	}
	changed, err := core.freezePluginResources(context.Background(), "w1", request, agentapi.DetectionResult{})
	if err != nil || changed.key == frozen.key {
		t.Fatalf("revision did not change static identity: %v", err)
	}
}

// TestColdPluginResourcesRejectConfigurationChangedAfterDeclaration 防止热声明后的配置更新静默进入冷会话。
func TestColdPluginResourcesRejectConfigurationChangedAfterDeclaration(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "plugins.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	db := database.DB
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "fixed skill", false)
	state.Configuration.Secrets = nil
	for _, model := range []any{&state.Plugin, &state.Versions[0], &state.Installations[0], &state.Configuration} {
		if err := db.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	core := &service{pluginController: controllers.NewPluginController(db), skillDataDirectory: data}

	detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
	frozen, err := core.freezePluginResources(context.Background(), "w1", pendingMessage{}, detection)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.PluginConfiguration{}).Where("plugin_id = ?", "p1").Update("revision", 4).Error; err != nil {
		t.Fatal(err)
	}
	partial, err := core.acquirePluginResources(context.Background(), frozen)
	if partial.release != nil {
		defer partial.release()
	}
	if !errors.Is(err, status_error.PluginConfigurationConflict) {
		t.Fatalf("cold acquisition silently used changed configuration: %v", err)
	}
}

func TestColdPluginResourcesRejectInstallationChangesBeforeAdmission(t *testing.T) {
	for _, change := range []string{"disable", "uninstall", "switch-version"} {
		t.Run(change, func(t *testing.T) {
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "plugins.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := database.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			data := t.TempDir()
			state := executionPlugin(t, data, "p1", "fixed skill", false)
			state.Configuration.Secrets = nil
			for _, model := range []any{&state.Plugin, &state.Versions[0], &state.Installations[0], &state.Configuration} {
				if err := database.DB.Create(model).Error; err != nil {
					t.Fatal(err)
				}
			}
			controller := controllers.NewPluginController(database.DB)
			core := &service{pluginController: controller, skillDataDirectory: data}
			frozen, err := core.freezePluginResources(t.Context(), "w1", pendingMessage{}, agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "disable":
				err = controller.SetEnabled(t.Context(), "w1", "p1", false)
			case "uninstall":
				err = controller.Uninstall(t.Context(), "w1", "p1")
			case "switch-version":
				next := state.Versions[0]
				next.ID, next.Version = "p1-v2", "2.0.0"
				if err := database.DB.Create(&next).Error; err != nil {
					t.Fatal(err)
				}
				err = controller.SwitchVersion(t.Context(), "w1", "p1", "p1-v1", "p1-v2")
			}
			if err != nil {
				t.Fatal(err)
			}
			actual, err := core.acquirePluginResources(t.Context(), frozen)
			if actual.release != nil {
				defer actual.release()
			}
			if !errors.Is(err, status_error.PluginVersionConflict) || len(actual.snapshots) != 0 {
				t.Fatalf("stale pre-admission resource acquired: %+v %v", actual.snapshots, err)
			}
			if _, err := os.Stat(filepath.Join(data, "plugins", "data", "p1")); !os.IsNotExist(err) {
				t.Fatalf("stale acquisition materialized plugin data: %v", err)
			}
		})
	}
}

func TestFrozenPluginResourceKeyUsesStableResourceFacts(t *testing.T) {
	data := t.TempDir()
	a := executionPlugin(t, data, "a", "first", false)
	b := executionPlugin(t, data, "b", "second", false)
	states := []models.PluginState{a, b}
	s := &service{pluginController: installedPluginFixture{states: states}, skillDataDirectory: data}
	first, err := s.freezePluginResources(context.Background(), "w1", pendingMessage{message: userMessage{Text: "first", PluginIDs: []string{"a"}}}, agentapi.DetectionResult{})
	if err != nil {
		t.Fatal(err)
	}
	s.pluginController = installedPluginFixture{states: []models.PluginState{b, a}}
	second, err := s.freezePluginResources(context.Background(), "w1", pendingMessage{message: userMessage{Text: "second", PluginIDs: []string{"b"}}}, agentapi.DetectionResult{})
	if err != nil {
		t.Fatal(err)
	}
	if first.key == "" || first.key != second.key {
		t.Fatalf("same resources changed identity: %q %q", first.key, second.key)
	}
	a.Plugin.Name = "new display name"
	s.pluginController = installedPluginFixture{states: []models.PluginState{a, b}}
	labeled, err := s.freezePluginResources(context.Background(), "w1", pendingMessage{}, agentapi.DetectionResult{Diagnostics: []agentapi.Diagnostic{{Code: "catalog-only", Message: "changed diagnostic"}}})
	if err != nil || labeled.key != first.key {
		t.Fatalf("display metadata changed resource identity: %q %v", labeled.key, err)
	}
	b.Configuration.Revision++
	s.pluginController = installedPluginFixture{states: []models.PluginState{a, b}}
	changed, err := s.freezePluginResources(context.Background(), "w1", pendingMessage{}, agentapi.DetectionResult{})
	if err != nil {
		t.Fatal(err)
	}
	if first.key == changed.key {
		t.Fatal("configuration change reused resource identity")
	}
	b.Configuration.Revision--
	b.Installations[0].Enabled = false
	disabled, err := s.freezePluginResources(context.Background(), "w1", pendingMessage{}, agentapi.DetectionResult{})
	if err != nil {
		t.Fatal(err)
	}
	if first.key == disabled.key {
		t.Fatal("disabled plugin reused resource identity")
	}
}

// preparePlugins 为资源行为测试顺序调用生产冻结与取得端口。
func (m *service) preparePlugins(ctx context.Context, waker string, request pendingMessage, detection agentapi.DetectionResult, usage ...authorization.Usage) (preparedPlugins, error) {
	plan, err := m.freezePluginResources(ctx, waker, request, detection)
	if err != nil {
		return preparedPlugins{}, err
	}
	prepared, err := m.acquirePluginResources(ctx, plan, usage...)
	if err != nil && prepared.release != nil {
		prepared.release()
	}
	return prepared, err
}

// TestNativePluginSkillSelectionIsReadOnly 防止接纳前技能预选创建实例目录或提前取得身份租约。
func TestNativePluginSkillSelectionIsReadOnly(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "p1", "use office", true)
	core := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginLauncher: "/core"}
	detection := agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: agentapi.RuntimeSummary{ID: "kimi", BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
	frozen, err := core.freezePluginResources(t.Context(), "w1", pendingMessage{message: userMessage{PluginIDs: []string{"p1"}}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	// 发布后的声明是热路径权威；源内容只在新的冷获取时重新核验。
	source := filepath.Join(data, state.Versions[0].ContentPath)
	if err := os.Rename(source, source+"-offline"); err != nil {
		t.Fatal(err)
	}
	selected, err := core.selectNativePluginSkills(t.Context(), frozen)
	if err != nil || len(selected) != 1 || selected[0].Name != "office" {
		t.Fatalf("pre-admission skills = %+v %v", selected, err)
	}
	if _, err := os.Stat(filepath.Join(data, "plugins", "data", "p1")); !os.IsNotExist(err) {
		t.Fatalf("read-only selection created plugin data: %v", err)
	}
	if err := os.Rename(source+"-offline", source); err != nil {
		t.Fatal(err)
	}
	actual, err := core.acquirePluginResources(t.Context(), frozen)
	if actual.release != nil {
		defer actual.release()
	}
	selectedPath, pathErr := filepath.EvalSymlinks(selected[0].Path)
	if err != nil || pathErr != nil || len(actual.skills) != 1 || selected[0].Name != actual.skills[0].Name || selected[0].Digest != actual.skills[0].Digest || !sameFilesystemPath(selectedPath, actual.skills[0].Path) {
		t.Fatalf("preflight/acquisition skills differ: %+v %+v %v", selected, actual.skills, err)
	}
}

func TestOptionalPluginDeclarationsSkipUndeliverableComponents(t *testing.T) {
	for _, kind := range []string{"native-only", "missing-skill-support"} {
		t.Run(kind, func(t *testing.T) {
			data := t.TempDir()
			state := executionPlugin(t, data, "p1", "fixed skill", false)
			state.Configuration.Secrets = nil
			detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "codex", SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
			if kind == "native-only" {
				detection.Version = "0.161.0"
			} else {
				detection.Runtime.SkillDirectoryName = ""
			}
			core := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
			frozen, err := core.freezePluginResources(t.Context(), "w1", pendingMessage{}, detection)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := core.selectNativePluginSkills(t.Context(), frozen)
			if err != nil || len(selected) != 0 {
				t.Fatalf("optional undeliverable declaration entered native skills: %+v %v", selected, err)
			}
		})
	}
}

// Account 为测试保留与配置读取分离的真实账号代次和材料边界。
func (f installedPluginFixture) Account(_ context.Context, id string) (models.PluginAccount, error) {
	for _, state := range f.states {
		if state.Plugin.ID == id {
			account := state.Account
			account.PluginID = id
			return account, nil
		}
	}
	return models.PluginAccount{PluginID: id}, nil
}

// AccountGeneration 为测试提供不读取秘密的身份代次。
func (f installedPluginFixture) AccountGeneration(ctx context.Context, id string) (int64, error) {
	account, err := f.Account(ctx, id)
	return account.Generation, err
}

func TestPluginAccountGenerationInvalidatesFrozenResources(t *testing.T) {
	data := t.TempDir()
	state := executionPlugin(t, data, "account-generation", "perform work", false)
	state.Account = models.PluginAccount{PluginID: state.Plugin.ID, Generation: 1}
	core := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data}
	detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "kimi", SkillDirectoryName: ".agents"}}
	request := pendingMessage{message: userMessage{PluginIDs: []string{state.Plugin.ID}}}
	original, err := core.freezePluginResources(t.Context(), "w1", request, detection)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := core.acquirePluginResources(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.snapshots) != 1 || prepared.snapshots[0].AccountGeneration != 1 {
		t.Fatal("account generation was not frozen", prepared.snapshots)
	}
	state.Account.Generation = 2
	core.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	next, err := core.freezePluginResources(t.Context(), "w1", request, detection)
	if err != nil || next.key == original.key {
		t.Fatal("changed account reused declaration", err)
	}
	if _, err := core.acquirePluginResources(t.Context(), original); !errors.Is(err, status_error.PluginAccountConflict) {
		t.Fatal("cold acquisition accepted changed account", err)
	}
	state.Versions[0].Package.ContractVersion = ""
	core.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	if _, err := core.preparePlugins(t.Context(), "w1", request, detection); !errors.Is(err, status_error.PluginPackageUnsupported) {
		t.Fatal("old contract executed", err)
	}
}
