package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/plugininstallation"
)

func authenticatedPluginFixture(t *testing.T, override ...string) (*service, models.PluginState, string) {
	t.Helper()
	return authenticatedPluginSourceFixture(t, authenticationFixtureShape{}, override...)
}

type authenticationFixtureShape struct {
	// RejectStatus 使桥接拒绝聊天不应触发的远端身份预检。
	RejectStatus bool
	// Canonical 使用单一标准包及自身授权绑定。
	Canonical bool
	// Skills 为该单包加入可读取的业务技能。
	Skills bool
	// LegacySkillBridge 使用未声明目标绑定的既有技能授权协议。
	LegacySkillBridge bool
	// UnboundHeader 为单包加入未被授权桥接接管的配置模板头。
	UnboundHeader string
}

func authenticatedPluginSourceFixture(t *testing.T, shape authenticationFixtureShape, override ...string) (*service, models.PluginState, string) {
	t.Helper()
	data, source := t.TempDir(), t.TempDir()
	data, err := filepath.EvalSymlinks(data)
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		path = filepath.Join(source, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("logical/.codex-plugin/plugin.json", `{"name":"logical","version":"2"}`)
	write("logical/.neuxally/integration.json", `{"nativePlugins":{"codex":{"path":"codex"},"claude":{"path":"claude"}},"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"codex","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"},{"runtime":"claude","server":"service","url":"https://example.invalid/mcp/","header":"Authorization"}]}}`)
	script := `import readline from 'node:readline';import {writeFileSync} from 'node:fs';import path from 'node:path';
for await (const line of readline.createInterface({input:process.stdin})) {
 const request=JSON.parse(line);
 writeFileSync(path.join(request.params.dataDirectory,'fixture-bridge-started'),'started');
 let result;
 if(request.method==='auth/status') result={state:'authorized',account:{configured:true,available:true,id:'oauth-user',scopes:[]}};
 else if(request.method==='auth/resolve') result={bindings:request.params.components.map(c=>{if(c.key!=='service'||c.target!=='mcp'||c.url!=='https://example.invalid/mcp/')throw Error('unexpected target');return {componentId:c.componentId,headers:{Authorization:'Bearer oauth-device-token'}};})};
 else result={state:'cancelled'};
 console.log(JSON.stringify({jsonrpc:'2.0',id:request.id,result}));
}`
	if shape.RejectStatus {
		script = strings.Replace(script, "if(request.method==='auth/status') result=", "if(request.method==='auth/status') throw Error('unexpected online preflight'); else if(false) result=", 1)
	}
	if len(override) > 0 {
		script = strings.Replace(script, "headers:{Authorization:'Bearer oauth-device-token'}", override[0], 1)
	}
	write("logical/bridge.mjs", script)
	if shape.LegacySkillBridge {
		write("logical/.neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"]}}`)
		write("logical/skills/review/SKILL.md", "---\nname: review\ndescription: Review changes\n---\n# Legacy complete guide\n")
		skillScript := `import readline from 'node:readline';
for await(const line of readline.createInterface({input:process.stdin})){
const r=JSON.parse(line);let result;
if(r.method==='auth/status')result={state:'authorized',account:{configured:true,available:true,id:'legacy-user',scopes:[]}};
else if(r.method==='auth/resolve')result={bindings:r.params.components.map(c=>{if(c.key!=='skills/review'||c.target!=='skill')throw Error('unexpected target');return {componentId:c.componentId,environment:{BUSINESS_TOKEN:'legacy-secret'}};})};
console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));}`
		if len(override) > 0 {
			skillScript = strings.Replace(skillScript, "environment:{BUSINESS_TOKEN:'legacy-secret'}", override[0], 1)
		}
		write("logical/bridge.mjs", skillScript)
	} else if shape.Canonical {
		write("logical/.codex-plugin/plugin.json", `{"name":"github","version":"0.1.0"}`)
		write("logical/.mcp.json", `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/"}}}`)
		if shape.UnboundHeader != "" {
			write("logical/.mcp.json", fmt.Sprintf(`{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"X-Business":%q}}}}`, shape.UnboundHeader))
		}
		write("logical/.neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"codex","server":"github","url":"https://api.githubcopilot.com/mcp/","header":"Authorization"},{"runtime":"claude","server":"github","url":"https://api.githubcopilot.com/mcp/","header":"Authorization"}]}}`)
		write("logical/bridge.mjs", strings.NewReplacer("c.key!=='service'", "c.key!=='github'", "https://example.invalid/mcp/", "https://api.githubcopilot.com/mcp/").Replace(script))
		if shape.Skills {
			write("logical/skills/review/SKILL.md", "---\nname: review\ndescription: Review changes\n---\n# Canonical complete guide\n")
		}
	} else {
		for _, cli := range []string{"codex", "claude"} {
			write(cli+"/."+cli+"-plugin/plugin.json", `{"name":"native-target","version":"1"}`)
			write(cli+"/.mcp.json", `{"mcpServers":{"service":{"type":"http","url":"https://example.invalid/mcp/","headers":{"Authorization":"Bearer ${OLD_TOKEN}"},"oauth":{"clientId":"upstream-public-client"}}}}`)
			write(cli+"/.app.json", `{"apps":{"optional":{"id":"separate-hosted-app","required":false}}}`)
		}
	}
	stage, err := plugins.StageSource(filepath.Join(data, "plugins/staging"), source, "logical")
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
	registry := commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(registry.Close)
	manager := &service{pluginController: installedPluginFixture{states: []models.PluginState{state}}, skillDataDirectory: data, pluginInstallations: plugininstallation.New(plugininstallation.Config{DataDirectory: data, Bridges: registry, BridgeEnvironment: os.Environ()})}
	state = resolveFixtureDelivery(t, manager, state)
	manager.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	return manager, state, root
}

func resolveFixtureDelivery(t *testing.T, manager *service, state models.PluginState) models.PluginState {
	t.Helper()
	_, driver, err := manager.pluginInstallations.Bridge(state.Versions[0])
	if err != nil {
		t.Fatal(err)
	}
	materials, err := plugininstallation.ResolveDeliveryMaterials(context.Background(), driver, state.Versions[0], state.Configuration, "oauth-user")
	if err == nil {
		state.Configuration.DeliveryMaterials = materials
	}
	_ = os.Remove(filepath.Join(manager.skillDataDirectory, "plugins/data", state.Plugin.ID, "fixture-bridge-started"))
	return state
}

func TestBoundAuthenticationRejectsUndeclaredRuntimeBeforeStartingBridge(t *testing.T) {
	for _, required := range []bool{true, false} {
		t.Run(fmt.Sprintf("required=%t", required), func(t *testing.T) {
			manager, _, _ := authenticatedPluginSourceFixture(t, authenticationFixtureShape{Canonical: true})
			request := pendingMessage{}
			if required {
				request.message.PluginIDs = []string{"p"}
			}
			detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "pi", SkillDirectoryName: ".pi"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true}}
			prepared, err := manager.preparePlugins(context.Background(), "w", request, detection)
			if required && !errors.Is(err, status_error.PluginForbidden) {
				prepared.release()
				t.Fatal("undeclared runtime was not explicitly refused", err)
			}
			if !required {
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.release()
				if len(prepared.connectors) != 0 || len(prepared.environment) != 0 || len(prepared.snapshots) != 0 {
					t.Fatal("optional undeclared runtime received authenticated components")
				}
			}
			if _, err := os.Stat(filepath.Join(manager.skillDataDirectory, "plugins", "data", "p", "fixture-bridge-started")); !os.IsNotExist(err) {
				t.Fatal("undeclared runtime started bridge before rejection")
			}
		})
	}
}

func TestUnboundLegacySkillBridgeStillProjectsForOtherRuntime(t *testing.T) {
	manager, _, _ := authenticatedPluginSourceFixture(t, authenticationFixtureShape{LegacySkillBridge: true})
	detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "pi", SkillDirectoryName: ".pi"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
	prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.skills) != 1 || len(prepared.connectors) != 0 || prepared.environment["BUSINESS_TOKEN"] != "legacy-secret" {
		t.Fatal("target binding gate changed the unbound legacy skill bridge")
	}
}

func TestSkillCommandBridgeProjectsManagedCLIForOtherRuntimes(t *testing.T) {
	for _, cli := range []string{"pi", "cursor", "gemini", "grok", "kimi", "opencode", "workbuddy"} {
		t.Run(cli, func(t *testing.T) {
			binding := `environment:{PATH:r.params.dataDirectory+(process.env.PATH?process.platform==='win32'?';':':':'')+(process.env.PATH??''),WECOM_CLI_CONFIG_DIR:r.params.dataDirectory}`
			manager, _, _ := authenticatedPluginSourceFixture(t, authenticationFixtureShape{LegacySkillBridge: true}, binding)
			data, err := filepath.EvalSymlinks(manager.skillDataDirectory)
			if err != nil {
				t.Fatal(err)
			}
			manager.skillDataDirectory = data
			registry := commandbridge.NewRegistry(context.Background(), nil)
			t.Cleanup(registry.Close)
			manager.pluginInstallations = plugininstallation.New(plugininstallation.Config{DataDirectory: data, Bridges: registry, BridgeEnvironment: os.Environ()})
			detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: cli, SkillDirectoryName: ".agents"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
			prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, detection)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.release()
			private := filepath.Join(data, "plugins/data/p")
			paths := filepath.SplitList(prepared.environment["PATH"])
			if len(prepared.skills) != 1 || len(prepared.connectors) != 0 || len(paths) == 0 || paths[0] != private || prepared.environment["WECOM_CLI_CONFIG_DIR"] != private {
				t.Fatal("skill projection lost the managed CLI or its shared profile")
			}
		})
	}
}

func TestSkillCommandBridgesMergePrivatePathsAndKeepOtherConflicts(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("conflict=%t", conflict), func(t *testing.T) {
			binding := `environment:{PATH:r.params.dataDirectory+(process.env.PATH?process.platform==='win32'?';':':':'')+(process.env.PATH??''),BUSINESS_TOKEN:'shared'}`
			manager, first, _ := authenticatedPluginSourceFixture(t, authenticationFixtureShape{LegacySkillBridge: true}, binding)
			data, err := filepath.EvalSymlinks(manager.skillDataDirectory)
			if err != nil {
				t.Fatal(err)
			}
			manager.skillDataDirectory = data
			if conflict {
				binding = strings.Replace(binding, "BUSINESS_TOKEN:'shared'", "BUSINESS_TOKEN:'different'", 1)
			}
			_, second, secondRoot := authenticatedPluginSourceFixture(t, authenticationFixtureShape{LegacySkillBridge: true}, binding)
			content, err := plugins.CopyContent(filepath.Join(manager.skillDataDirectory, "plugins/content"), secondRoot, second.Versions[0].Digest)
			if err != nil {
				t.Fatal(err)
			}
			second.Plugin.ID = "second"
			second.Versions[0].PluginID = "second"
			second.Versions[0].ID = "second-version"
			second.Versions[0].ContentPath, err = filepath.Rel(manager.skillDataDirectory, content)
			if err != nil {
				t.Fatal(err)
			}
			second.Installations[0].PluginID = "second"
			second.Installations[0].VersionID = "second-version"
			manager.pluginController = installedPluginFixture{states: []models.PluginState{first, second}}
			registry := commandbridge.NewRegistry(context.Background(), nil)
			t.Cleanup(registry.Close)
			manager.pluginInstallations = plugininstallation.New(plugininstallation.Config{DataDirectory: manager.skillDataDirectory, Bridges: registry, BridgeEnvironment: os.Environ()})
			first = resolveFixtureDelivery(t, manager, first)
			second = resolveFixtureDelivery(t, manager, second)
			manager.pluginController = installedPluginFixture{states: []models.PluginState{first, second}}
			detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "pi", SkillDirectoryName: ".pi"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}}
			prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p", "second"}}}, detection)
			defer prepared.release()
			if conflict {
				if !errors.Is(err, status_error.PluginConfigurationConflict) {
					t.Fatalf("non-PATH collision was relaxed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			paths := filepath.SplitList(prepared.environment["PATH"])
			wantPrivate := []string{filepath.Join(manager.skillDataDirectory, "plugins/data/p"), filepath.Join(manager.skillDataDirectory, "plugins/data/second")}
			if len(paths) < 2 || !slices.Equal(paths[:2], wantPrivate) || len(prepared.skills) != 2 || len(prepared.snapshots) != 2 || prepared.environment["BUSINESS_TOKEN"] != "shared" {
				t.Fatalf("shared execution lost a private CLI or skill: %q", paths)
			}
			for _, inherited := range filepath.SplitList(os.Getenv("PATH")) {
				if !slices.Contains(paths[2:], inherited) {
					t.Fatalf("trusted inherited path missing: %q", inherited)
				}
			}
		})
	}
}

func TestBoundAuthenticationAllowsMissingCredentialDuringChat(t *testing.T) {
	for _, reply := range []string{`headers:{}`, `headers:{Authorization:''}`} {
		t.Run(reply, func(t *testing.T) {
			manager, _, _ := authenticatedPluginFixture(t, reply)
			prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "codex"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true}})
			if err != nil {
				t.Fatalf("missing authorization prevented plugin delivery: %v", err)
			}
			defer prepared.release()
			if len(prepared.connectors) != 1 || len(prepared.snapshots) != 1 {
				t.Fatalf("plugin delivery was lost without credentials: %+v", prepared.snapshots)
			}
			for _, header := range prepared.connectors[0].Headers {
				value := header.Value
				if value != "" {
					t.Fatal("missing credential invented an authorization header")
				}
			}
		})
	}
}

func TestAuthorizationManagementRejectsUndeclaredSecrets(t *testing.T) {
	for _, reply := range []string{`headers:{'X-Other':'secret'}`, `headers:{Authorization:'Bearer token','X-Other':'secret'}`, `headers:{Authorization:'Bearer token'},environment:{TOKEN:'secret'}`, `headers:{}`, `headers:{Authorization:''}`} {
		t.Run(reply, func(t *testing.T) {
			manager, state, _ := authenticatedPluginFixture(t, reply)
			_, driver, err := manager.pluginInstallations.Bridge(state.Versions[0])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := plugininstallation.ResolveDeliveryMaterials(context.Background(), driver, state.Versions[0], state.Configuration, "oauth-user"); err == nil {
				t.Fatal("management accepted incomplete or undeclared secret binding")
			}
		})
	}
}

func TestChatPluginLoadingDoesNotPreflightProviderStatus(t *testing.T) {
	manager, _, _ := authenticatedPluginSourceFixture(t, authenticationFixtureShape{RejectStatus: true})
	prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "codex"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true}})
	if err != nil {
		t.Fatalf("chat performed provider status preflight: %v", err)
	}
	defer prepared.release()
	if len(prepared.connectors) != 1 || len(prepared.snapshots) != 1 || prepared.snapshots[0].ProviderAccountID != "oauth-user" {
		t.Fatalf("local plugin loading lost fixed resource or invented online identity: %+v", prepared.snapshots)
	}
}

func TestChatPluginLoadingNeverStartsAuthorizationBridge(t *testing.T) {
	manager, state, _ := authenticatedPluginFixture(t)
	state.Configuration.DeliveryMaterials = nil
	manager.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "codex"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if _, err := os.Stat(filepath.Join(manager.skillDataDirectory, "plugins/data/p/fixture-bridge-started")); !os.IsNotExist(err) {
		t.Fatal("chat started the authorization program")
	}
	if len(prepared.connectors) != 1 {
		t.Fatal("missing private material prevented capability delivery")
	}
	for _, header := range prepared.connectors[0].Headers {
		value := header.Value
		if value != "" {
			t.Fatal("chat invented a credential instead of loading existing material")
		}
	}
}

func TestChatPluginLoadingKeepsMaterialAcrossOrdinaryConfigurationRevision(t *testing.T) {
	manager, state, _ := authenticatedPluginFixture(t)
	state.Configuration.Revision++
	manager.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "codex"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.connectors) != 1 {
		t.Fatal("stale material blocked capability delivery")
	}
	for _, header := range prepared.connectors[0].Headers {
		value := header.Value
		if value != "Bearer oauth-device-token" {
			t.Fatal("ordinary configuration revision discarded the existing identity")
		}
	}
	if _, err := os.Stat(filepath.Join(manager.skillDataDirectory, "plugins/data/p/fixture-bridge-started")); !os.IsNotExist(err) {
		t.Fatal("stale material triggered authorization program")
	}
}

func TestChatDeliversLegacySkillWithoutPrivateMaterial(t *testing.T) {
	manager, state, _ := authenticatedPluginSourceFixture(t, authenticationFixtureShape{LegacySkillBridge: true})
	state.Configuration.DeliveryMaterials = nil
	manager.pluginController = installedPluginFixture{states: []models.PluginState{state}}
	prepared, err := manager.preparePlugins(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "pi", SkillDirectoryName: ".pi"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilitySkillDirectory: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.skills) != 1 || len(prepared.environment) != 0 {
		t.Fatal("missing material prevented skill delivery")
	}
}

// currentAuthenticationFixture 在冻结安装事实后仍返回当前权威身份材料。
type currentAuthenticationFixture struct {
	// installedPluginFixture 保留冻结安装与内容读取。
	installedPluginFixture
	// current 是身份管理刚完成写入的共享配置。
	current models.PluginConfiguration
}

func (f currentAuthenticationFixture) Configuration(context.Context, string) (models.PluginConfiguration, error) {
	return f.current, nil
}

func TestPluginIdentityIsLoadedAfterAcquiringFrozenResources(t *testing.T) {
	manager, state, _ := authenticatedPluginFixture(t)
	detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{BaseRuntimeID: "codex"}, Capabilities: agentapi.CapabilitySet{agentapi.CapabilityMCPStreamableHTTP: true}}
	plan, err := manager.freezePluginResources(context.Background(), "w", pendingMessage{message: userMessage{PluginIDs: []string{"p"}}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	_, driver, err := manager.pluginInstallations.Bridge(state.Versions[0])
	if err != nil {
		t.Fatal(err)
	}
	state.Configuration.DeliveryMaterials, err = plugininstallation.ResolveDeliveryMaterials(context.Background(), driver, state.Versions[0], state.Configuration, "reauthorized-after-freeze")
	if err != nil {
		t.Fatal(err)
	}
	manager.pluginController = currentAuthenticationFixture{installedPluginFixture: installedPluginFixture{states: []models.PluginState{state}}, current: state.Configuration}
	prepared, err := manager.acquirePluginResources(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.release()
	if len(prepared.snapshots) != 1 || prepared.snapshots[0].ProviderAccountID != "reauthorized-after-freeze" {
		t.Fatal("cold acquisition used credentials copied before identity ownership")
	}
}
