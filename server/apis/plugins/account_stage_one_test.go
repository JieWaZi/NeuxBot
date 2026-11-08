package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
)

const stageOneAPIDriver = `import readline from'node:readline';import{existsSync,appendFileSync,writeFileSync}from'node:fs';import path from'node:path';
for await(const line of readline.createInterface({input:process.stdin})){
 const r=JSON.parse(line),p=r.params;appendFileSync(path.join(p.dataDirectory,'calls'),r.method+'\n');let result={};
 if(r.method==='auth/initialize')result={protocol:'neuxbot-auth/1',context:p};
 else if(r.method==='auth/status')result={state:'authorized',account:{configured:true,available:true,id:'reader',scopes:[]}};
 else if(r.method==='auth/resolve')result={bindings:[]};
 console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));
 if(r.method==='auth/start'){
 const waiting=existsSync(path.join(p.dataDirectory,'wait'));
 console.log(JSON.stringify({jsonrpc:'2.0',method:waiting?'auth/challenge':'auth/state',params:waiting?{operationId:p.operationId,sessionId:p.sessionId,challengeId:'one',actions:[{type:'copy-code',value:'ONE'}],deadlineAt:Date.now()+60000}:{operationId:p.operationId,sessionId:p.sessionId,state:'authorized'}}));
 }
 if(r.method==='auth/cancel'){writeFileSync(path.join(p.dataDirectory,'cancelled'),'yes');process.exit(0);}
}`

func stageOneAPIFixture(t *testing.T, ctx *gin.Context, name, version string, auth bool) PluginDetailResponse {
	t.Helper()
	source := t.TempDir()
	manifest := map[string]any{"$schema": constants.PluginManifestSchema, "name": name, "version": version}
	if auth {
		manifest["extensions"] = map[string]any{constants.PluginExtensionNamespace: map[string]any{"schemaVersion": constants.PluginExtensionSchemaVersion, "authentication": map[string]any{"strategy": constants.PluginAuthCommandStrategy, "protocol": constants.PluginAuthProtocol, "command": "node", "args": []string{".neuxbot/auth.mjs"}}}}
		if err := os.MkdirAll(filepath.Join(source, ".neuxbot"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, ".neuxbot/auth.mjs"), []byte(stageOneAPIDriver), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(source, "skills/task"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skills/task/SKILL.md"), []byte("---\nname: task\ndescription: Test task\n---\nPerform a test task."), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", true)
	if err != nil {
		t.Fatal(err)
	}
	return detail
}

func stageOneRegistry(t *testing.T) {
	previous := global.PluginBridges
	global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(func() { global.PluginBridges.Close(); global.PluginBridges = previous })
}

func TestStageOneAccountGetIsCachedAndCheckIsExplicit(t *testing.T) {
	ctx := pluginAPIContext(t)
	stageOneRegistry(t)
	detail := stageOneAPIFixture(t, ctx, "cached-account", "1.0.0", true)
	id, version := detail.Plugin.ID, detail.ResolvedVersionID
	private := filepath.Join(global.Config.DataDirectory, "plugins/data", id, "calls")
	value, err := (&GetPluginAuthorization{PluginID: id, VersionID: version}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if value.(PluginAuthorizationResponse).Account.CheckedAt != 0 {
		t.Fatal("unknown identity was marked checked")
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatal("ordinary GET executed driver", err)
	}
	checked, err := (&CheckPluginAccount{PluginID: id, VersionID: version}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !checked.(PluginAuthorizationResponse).Account.Available || checked.(PluginAuthorizationResponse).Account.CheckedAt == 0 {
		t.Fatal("explicit check did not cache verified identity")
	}
	calls, err := os.ReadFile(private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&GetPluginAuthorization{PluginID: id, VersionID: version}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(private)
	if string(after) != string(calls) {
		t.Fatal("cached GET repeated external status")
	}
	if _, err := (&CheckPluginAccount{PluginID: id, VersionID: version, Body: PluginAccountRequest{ExpectedAccountGeneration: 1}}).Output(ctx); !errors.Is(err, status_error.PluginAccountConflict) {
		t.Fatal("stale identity was checked", err)
	}
	if err := global.PluginControllerManager.UpdateConfiguration(ctx, models.PluginConfiguration{PluginID: id}, 0); err != nil {
		t.Fatal(err)
	}
	value, err = (&GetPluginAuthorization{PluginID: id, VersionID: version}).Output(ctx)
	if err != nil || value.(PluginAuthorizationResponse).Account.CheckedAt != 0 {
		t.Fatal("new configuration inherited an old checked identity", err)
	}
	checked, err = (&CheckPluginAccount{PluginID: id, VersionID: version, Body: PluginAccountRequest{ExpectedConfigurationRevision: 1}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session, err := (&StartPluginAuthorization{PluginID: id, VersionID: version, Body: AuthorizationScopeRequest{PluginAccountRequest: PluginAccountRequest{ExpectedConfigurationRevision: 1}}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if session.(authorization.Session).State != constants.PluginAuthorizationStateReady {
		t.Fatal("authorization did not reach ready")
	}
	generation, err := global.PluginControllerManager.AccountGeneration(ctx, id)
	if err != nil || generation != 1 {
		t.Fatal("completed authorization did not publish account generation", generation, err)
	}
	value, err = (&GetPluginAuthorization{PluginID: id, VersionID: version}).Output(ctx)
	if err != nil || !value.(PluginAuthorizationResponse).Account.Available || value.(PluginAuthorizationResponse).AccountGeneration != 1 {
		t.Fatal("new account summary was lost after generation advance", err)
	}
	if _, err := (&LogoutPluginAuthorization{PluginID: id, VersionID: version, Body: PluginAccountRequest{ExpectedConfigurationRevision: 1, ExpectedAccountGeneration: 1}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	value, err = (&GetPluginAuthorization{PluginID: id, VersionID: version}).Output(ctx)
	if err != nil || value.(PluginAuthorizationResponse).Account.Available || value.(PluginAuthorizationResponse).Account.CheckedAt == 0 || value.(PluginAuthorizationResponse).AccountGeneration != 2 {
		t.Fatal("logout did not cache a known disconnected identity", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, id, "")
	if err != nil || len(relations) != 0 {
		t.Fatal("authorization created an installation", err)
	}
}

func TestStageOneOldCancelStopsOnlyTheOriginalWaitingSession(t *testing.T) {
	ctx := pluginAPIContext(t)
	stageOneRegistry(t)
	detail := stageOneAPIFixture(t, ctx, "cancel-account", "1.0.0", true)
	id, version := detail.Plugin.ID, detail.ResolvedVersionID
	private := filepath.Join(global.Config.DataDirectory, "plugins/data", id)
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "wait"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	value, err := (&StartPluginAuthorization{PluginID: id, VersionID: version}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	var session struct {
		// ID 是本次授权会话。
		ID string `json:"id"`
	}
	if err := json.Unmarshal(encoded, &session); err != nil || session.ID == "" {
		t.Fatal("missing waiting session", err)
	}
	if err := global.PluginControllerManager.UpdateConfiguration(ctx, models.PluginConfiguration{PluginID: id}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := (&CancelPluginAuthorization{PluginID: id, VersionID: version, SessionID: session.ID}).Output(ctx); err != nil {
		t.Fatal("old exact session could not be cancelled after configuration change", err)
	}
	if _, err := os.Stat(filepath.Join(private, "cancelled")); err != nil {
		t.Fatal("cancel returned before the old driver terminated", err)
	}
	next, err := (&StartPluginAuthorization{PluginID: id, VersionID: version, Body: AuthorizationScopeRequest{PluginAccountRequest: PluginAccountRequest{ExpectedConfigurationRevision: 1}}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(next)
	var nextSession struct {
		// ID 是新的授权会话。
		ID string `json:"id"`
	}
	_ = json.Unmarshal(encoded, &nextSession)
	if _, err := (&CancelPluginAuthorization{PluginID: id, VersionID: version, SessionID: session.ID}).Output(ctx); !errors.Is(err, status_error.PluginAccountConflict) {
		t.Fatal("old context cancelled the new driver", err)
	}
	if _, err := (&GetPluginAuthorizationSession{PluginID: id, VersionID: version, SessionID: nextSession.ID}).Output(ctx); err != nil {
		t.Fatal("new session was lost", err)
	}
	if _, err := (&LogoutPluginAuthorization{PluginID: id, VersionID: version, Body: PluginAccountRequest{ExpectedConfigurationRevision: 1}}).Output(ctx); !errors.Is(err, status_error.PluginBusy) {
		t.Fatal("logout interrupted active authorization", err)
	}
	if _, err := (&CancelPluginAuthorization{PluginID: id, VersionID: version, SessionID: nextSession.ID, Body: PluginAccountRequest{ExpectedConfigurationRevision: 1}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStageOneInstallAndUpdateKeepExactPartnerFacts(t *testing.T) {
	ctx := pluginAPIContext(t)
	detail := stageOneAPIFixture(t, ctx, "fixed-install", "1.0.0", false)
	value, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1", "w2", "w1"}}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.(InstallPluginResponse).Results) != 2 {
		t.Fatal("duplicate partner was installed twice")
	}
	if err := global.PluginControllerManager.SetEnabled(ctx, "w2", detail.Plugin.ID, false); err != nil {
		t.Fatal(err)
	}
	target := stageOneAPIFixture(t, ctx, "fixed-install", "2.0.0", false)
	if _, err := (&UpdatePluginConfiguration{PluginID: detail.Plugin.ID, Body: UpdateConfigurationRequest{}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	relations, _ := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "")
	for _, r := range relations {
		if r.VersionID != detail.ResolvedVersionID {
			t.Fatal("configuration save switched a version")
		}
	}
	changes := []VersionSwitch{{WakerID: "w1", ExpectedVersionID: detail.ResolvedVersionID, VersionID: target.ResolvedVersionID}, {WakerID: "w2", ExpectedVersionID: detail.ResolvedVersionID, VersionID: target.ResolvedVersionID}}
	if _, err := (&UpdatePluginInstallations{PluginID: detail.Plugin.ID, Body: UpdatePluginInstallationsRequest{ExpectedRevision: 1, Switches: changes}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	relations, _ = global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "")
	for _, r := range relations {
		if r.VersionID != target.ResolvedVersionID || (r.WakerID == "w2" && r.Enabled) {
			t.Fatal("update did not preserve fixed version and enabled state", r)
		}
	}
	if _, err := (&UpdatePluginInstallations{PluginID: detail.Plugin.ID, Body: UpdatePluginInstallationsRequest{ExpectedRevision: 1, Switches: changes}}).Output(ctx); !errors.Is(err, status_error.PluginConfigurationConflict) {
		t.Fatal("stale update was accepted", err)
	}
}

func TestStageOneCachedAccountRejectsOldPersistedContract(t *testing.T) {
	ctx := pluginAPIContext(t)
	store := global.PluginControllerManager
	if err := store.Create(ctx, models.Plugin{ID: "old", SourceKey: "old", Name: "Old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutVersion(ctx, models.PluginVersion{ID: "old-v", PluginID: "old", Version: "1", Package: packageplugin.Package{Manifest: packageplugin.Manifest{Name: "old"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&GetPluginAuthorization{PluginID: "old", VersionID: "old-v"}).Output(ctx); !errors.Is(err, status_error.PluginPackageUnsupported) {
		t.Fatal("old persisted package was accepted", err)
	}
	body, _ := json.Marshal(PluginAuthorizationResponse{})
	if strings.Contains(string(body), "deliveryMaterials") {
		t.Fatal("account material entered HTTP contract")
	}
}

func TestStageOneConnectorDiagnosticsCannotSurviveAccountGenerationChange(t *testing.T) {
	ctx := pluginAPIContext(t)
	detail := stageOneAPIFixture(t, ctx, "diagnostic-epoch", "1.0.0", false)
	version := detail.Versions[0]
	identity := connectorDiagnosticKey(detail.Plugin.ID, version.ID, "component")
	pluginConnectorDiagnostics.put(identity, connectorDiagnostic{Connector: mcp.ConnectorResource{TestedAt: 123}, Revision: 0, AccountGeneration: 0})
	response := mcp.ConnectorResponse{}
	if err := pluginConnectorDiagnostics.apply(ctx, version, "component", &response); err != nil || response.Connector.TestedAt != 123 {
		t.Fatalf("current diagnostic unavailable: %+v %v", response, err)
	}
	if err := global.PluginControllerManager.UpdateAccount(ctx, models.PluginAccount{PluginID: detail.Plugin.ID}, 0); err != nil {
		t.Fatal(err)
	}
	response = mcp.ConnectorResponse{}
	if err := pluginConnectorDiagnostics.apply(ctx, version, "component", &response); err != nil || response.Connector.TestedAt != 0 {
		t.Fatalf("previous account generation diagnostic resurfaced: %+v %v", response, err)
	}
}
