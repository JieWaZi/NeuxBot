package plugininstallation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

const stageOneAccountScript = `import readline from 'node:readline';import{existsSync,readFileSync,writeFileSync,rmSync}from'node:fs';import path from'node:path';
for await(const line of readline.createInterface({input:process.stdin})){
 const r=JSON.parse(line),p=r.params,identity=path.join(p.dataDirectory,'identity');let result;
 if(r.method==='auth/initialize'){result={protocol:'neuxbot-auth/1',context:p};}
 else if(r.method==='auth/start'){writeFileSync(identity,'second-user');result={};}
 else if(r.method==='auth/status'){result={state:'authorized',account:{configured:true,available:!existsSync(path.join(p.dataDirectory,'deny-'+p.versionId)),id:existsSync(identity)?readFileSync(identity,'utf8'):'first-user',scopes:[]}};}
 else if(r.method==='auth/resolve'){result={bindings:p.components.map(c=>({componentId:c.componentId,headers:{Authorization:'Bearer private-'+(existsSync(identity)?readFileSync(identity,'utf8'):'first-user')}}))};}
 else if(r.method==='auth/logout'){rmSync(identity,{force:true});result={};}
 else result={};
 console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));
 if(r.method==='auth/start')console.log(JSON.stringify({jsonrpc:'2.0',method:'auth/state',params:{operationId:p.operationId,sessionId:p.sessionId,state:'authorized'}}));
}`

func TestStageOneAccountMaterialLifecycleAndRestart(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(data, "plugins.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sql, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sql.Close()
	store := controllers.NewPluginController(database.DB)
	if err := store.Create(ctx, models.Plugin{ID: "p", Name: "Account", SourceKey: "fixture"}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "auth.mjs"), []byte(stageOneAccountScript), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := plugins.ContentDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	root, err := plugins.CopyContent(filepath.Join(data, "plugins/content"), source, digest)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(data, root)
	if err != nil {
		t.Fatal(err)
	}
	version := models.PluginVersion{ID: "v", PluginID: "p", Version: "1.0.0", Digest: digest, ContentPath: relative, TrustedAt: 1, Package: plugins.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: plugins.Manifest{Schema: constants.PluginManifestSchema, Name: "fixture", Version: "1.0.0"}, Servers: []plugins.Server{{Key: "service", Type: "streamable-http", URL: "https://example.invalid/mcp"}}, Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "node", Args: []string{"auth.mjs"}, Bindings: []plugins.AuthenticationBinding{{Runtime: "codex", Server: "service", URL: "https://example.invalid/mcp", Header: "Authorization"}}}}}}
	if _, err := store.PutVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	second := version
	second.ID = "v2"
	second.Version = "2.0.0"
	if _, err := store.PutVersion(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := controllers.NewWakerController(database.DB).Create(ctx, models.Waker{ID: "w1", Name: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Install(ctx, models.WakerPlugin{ID: "install", PluginID: "p", VersionID: second.ID, WakerID: "w1", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	registry := commandbridge.NewRegistry(ctx, nil)
	defer registry.Close()
	owner := New(Config{Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)), DataDirectory: data, Bridges: registry, BridgeEnvironment: os.Environ(), Store: store})
	config := models.PluginConfiguration{PluginID: "p"}
	account := models.PluginAccount{PluginID: "p"}
	release, err := owner.PrepareVersions(ctx, []models.PluginVersion{version, second}, &config, &account)
	if err != nil {
		t.Fatal(err)
	}
	release()
	generation, err := store.AccountGeneration(ctx, "p")
	if err != nil || generation != 0 {
		t.Fatal("preparation persisted account or generation", generation, err)
	}
	if err := store.UpdateAccount(ctx, account, 0); err != nil {
		t.Fatal(err)
	}
	var raw models.PluginAccount
	if err := database.DB.Where("plugin_id = ?", "p").First(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if raw.Generation != 1 || len(raw.DeliveryMaterials) != 2 || raw.DeliveryMaterials[0].Value != "" || raw.DeliveryMaterials[0].Ciphertext == "" {
		t.Fatal("account material was not protected", raw.Generation)
	}
	auth, driver, err := owner.Bridge(context.Background(), version)
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login(ctx)
	if err != nil || session.State != constants.PluginAuthorizationStateReady {
		t.Fatal("new auth protocol did not finish", session.State, err)
	}
	saved, err := controllers.NewPluginController(database.DB).Account(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Generation != 2 {
		t.Fatal("authorization did not advance account generation", saved.Generation)
	}
	bindings, subject, err := DeliveryAuthentication(version, config, saved, "codex")
	if err != nil || subject != "second-user" || len(bindings) != 1 || bindings[0].Headers["Authorization"] != "Bearer private-second-user" {
		t.Fatal("new identity material was not durable", subject, err)
	}
	previousBindings, previousSubject, err := DeliveryAuthentication(second, config, saved, "codex")
	if err != nil || previousSubject != "second-user" || len(previousBindings) != 1 {
		t.Fatal("still installed disabled version was not revalidated", previousSubject, err)
	}
	if _, err := auth.Status(ctx, true); err != nil {
		t.Fatal(err)
	}
	if generation, _ := store.AccountGeneration(ctx, "p"); generation != 2 {
		t.Fatal("ordinary check changed identity generation")
	}
	if err := store.UpdateConfiguration(ctx, config, 0); err != nil {
		t.Fatal(err)
	}
	if err := owner.persistDelivery(ctx, version, driver, "second-user"); err != status_error.PluginConfigurationConflict {
		t.Fatal("stale operation did not reject changed configuration", err)
	}
	invalidated, err := store.Account(ctx, "p")
	if err != nil || invalidated.Generation != 3 || len(invalidated.DeliveryMaterials) != 0 {
		t.Fatal("failed post-login commit retained earlier identity material", invalidated.Generation, err)
	}
	auth, _, err = owner.Bridge(context.Background(), version)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err = store.Account(ctx, "p")
	if err != nil || saved.Generation != 4 || len(saved.DeliveryMaterials) != 0 {
		t.Fatal("logout did not retire identity material", saved.Generation, err)
	}
	if revision, _ := store.ConfigurationRevision(ctx, "p"); revision != 1 {
		t.Fatal("account operation changed configuration identity")
	}

	if err := os.WriteFile(filepath.Join(data, "plugins/data/p/deny-v2"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	auth, _, err = owner.Bridge(context.Background(), version)
	if err != nil {
		t.Fatal(err)
	}
	session, err = auth.Login(ctx)
	if err == nil && session.State == constants.PluginAuthorizationStateReady {
		t.Fatal("login claimed all installed versions were ready when one rejected identity")
	}
	saved, err = store.Account(ctx, "p")
	if err != nil || saved.Generation != 5 || len(saved.DeliveryMaterials) != 0 {
		t.Fatal("failed installed-version revalidation retained former identity material", saved.Generation, err)
	}
	relations, err := store.Installations(ctx, "p", "")
	if err != nil || len(relations) != 1 || relations[0].VersionID != second.ID || relations[0].Enabled {
		t.Fatal("external authorization failure changed installation", err)
	}
}

func TestStageOneMaterialBoundToExactEndpointAndVersion(t *testing.T) {
	version := models.PluginVersion{ID: "v", PluginID: "p", Digest: strings.Repeat("a", 64), Package: plugins.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: plugins.Manifest{Schema: constants.PluginManifestSchema}, Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Bindings: []plugins.AuthenticationBinding{{Server: "service", URL: "https://first.invalid/mcp", Header: "Authorization"}}}}, Servers: []plugins.Server{{Key: "service", URL: "https://first.invalid/mcp"}}}}
	config := models.PluginConfiguration{}
	key := materialKey(version, config, "")
	account := models.PluginAccount{PluginID: "p", Generation: 1, DeliveryMaterials: []models.MCPSecretDefinition{{Name: key, Configured: true, Value: `{"accountId":"first-user","bindings":[{"componentId":"component","headers":{"Authorization":"private"}}]}`}}}
	// 同一组件的公共约束只生成一个材料槽位。
	bindings, subject, err := DeliveryAuthentication(version, config, account, "workbuddy")
	if err != nil || len(bindings) != 1 || subject != "first-user" || bindings[0].Headers["Authorization"] != "private" {
		t.Fatal("equivalent standard target lost its existing encrypted material", subject, err)
	}
	version.Package.Servers[0].URL = "https://second.invalid/mcp"
	version.Package.Integration.Authentication.Bindings[0].URL = "https://second.invalid/mcp"
	bindings, subject, err = DeliveryAuthentication(version, config, account, "codex")
	if err != nil || len(bindings) != 0 || subject != "" {
		t.Fatal("credential crossed the endpoint boundary", subject, err)
	}
	version.Package.Servers[0].URL = "https://first.invalid/mcp"
	version.Package.Integration.Authentication.Bindings[0].URL = "https://first.invalid/mcp"
	version.ID = "other-version"
	bindings, _, err = DeliveryAuthentication(version, config, account, "codex")
	if err != nil || len(bindings) != 0 {
		t.Fatal("credential crossed the version boundary", err)
	}
	if _, _, err := New(Config{}).Bridge(context.Background(), models.PluginVersion{}); err != status_error.PluginPackageUnsupported {
		t.Fatal("old persisted package was accepted", err)
	}
}

func TestMaterialIdentityIncludesSkillComponentAndPermittedEnvironment(t *testing.T) {
	version := models.PluginVersion{ID: "v", Digest: "fixed", Package: plugins.Package{Format: constants.PluginFormatPortable, ContractVersion: constants.PluginTargetContractVersion, Integration: plugins.Integration{Authentication: &plugins.Authentication{SkillBindings: []plugins.SkillAuthenticationBinding{{Skill: "skills/review", EnvironmentKeys: []string{"PATH"}, PrivatePathKeys: []string{"PATH"}}}}}}}
	key := materialKey(version, models.PluginConfiguration{}, "")
	version.Package.Integration.Authentication.SkillBindings[0].EnvironmentKeys = append(version.Package.Integration.Authentication.SkillBindings[0].EnvironmentKeys, "EXTRA_KEY")
	if next := materialKey(version, models.PluginConfiguration{}, ""); next == key {
		t.Fatal("material reused after skill environment constraint changed")
	}
}
