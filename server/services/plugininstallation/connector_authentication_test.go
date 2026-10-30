package plugininstallation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestManagementPersistsDeliveryBeforeReadyAndReloadsAfterRestart(t *testing.T) {
	ctx := context.Background()
	data, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(data, "plugins.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := controllers.NewPluginController(db.DB)
	if err := store.Create(ctx, models.Plugin{ID: "managed", Name: "测试插件", SourceKey: "managed-source"}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	script := `import readline from 'node:readline';import {existsSync,readFileSync,writeFileSync} from 'node:fs';import path from 'node:path';
for await(const line of readline.createInterface({input:process.stdin})){
 const r=JSON.parse(line),identity=path.join(r.params.dataDirectory,'identity');let result;
 if(r.method==='auth/start'){if(!existsSync(path.join(r.params.dataDirectory,'fail-start')))writeFileSync(identity,existsSync(path.join(r.params.dataDirectory,'next-account'))?readFileSync(path.join(r.params.dataDirectory,'next-account'),'utf8'):'second-user');result={state:'authorized'};}
 else if(r.method==='auth/status'){result={state:'authorized',account:{configured:true,available:true,id:existsSync(identity)?readFileSync(identity,'utf8'):'first-user',scopes:[]}};}
 else if(r.method==='auth/resolve'){result={bindings:existsSync(path.join(r.params.dataDirectory,'fail-resolve'))?[]:r.params.components.map(c=>({componentId:c.componentId,headers:{Authorization:'Bearer private-managed-token-'+(existsSync(identity)?readFileSync(identity,'utf8'):'first-user')}}))};}
 else result={state:'cancelled'};
 console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));
 if(r.method==='auth/start')console.log(JSON.stringify({jsonrpc:'2.0',method:'auth/state',params:{sessionId:r.params.sessionId,state:existsSync(path.join(r.params.dataDirectory,'fail-start'))?'failed':'authorized'}}));
}`
	if err := os.WriteFile(filepath.Join(source, "bridge.mjs"), []byte(script), 0600); err != nil {
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
	version := models.PluginVersion{ID: "version", PluginID: "managed", Digest: digest, ContentPath: relative, TrustedAt: 1, Package: plugins.Package{Servers: []plugins.Server{{Key: "service", Type: "http", URL: "https://example.invalid/mcp"}}, Integration: plugins.Integration{Authentication: &plugins.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "node", Args: []string{"bridge.mjs"}, Bindings: []plugins.AuthenticationBinding{{Runtime: "codex", Server: "service", URL: "https://example.invalid/mcp", Header: "Authorization"}}}}}}
	registry := commandbridge.NewRegistry(ctx, nil)
	t.Cleanup(registry.Close)
	owner := New(Config{DataDirectory: data, Bridges: registry, BridgeEnvironment: os.Environ(), Store: store})
	t.Run("proposed-endpoint", func(t *testing.T) {
		dynamic := version
		dynamic.ID = "dynamic-version"
		dynamic.PluginID = "dynamic-plugin"
		dynamic.Package.Servers = []plugins.Server{{Key: "service", Type: "http", URL: "${ENDPOINT}"}}
		declaration := *version.Package.Integration.Authentication
		declaration.Bindings = nil
		dynamic.Package.Integration.Authentication = &declaration
		dynamic.Package.Manifest.Extensions = map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(`{"config":[{"key":"ENDPOINT","required":true}]}`)}
		if err := store.Create(ctx, models.Plugin{ID: dynamic.PluginID, Name: "动态端点", SourceKey: "dynamic-source"}); err != nil {
			t.Fatal(err)
		}
		old := models.PluginConfiguration{PluginID: dynamic.PluginID, Values: map[string]string{"ENDPOINT": "https://first.invalid/mcp"}}
		if err := store.UpdateConfiguration(ctx, old, 0); err != nil {
			t.Fatal(err)
		}
		proposed := models.PluginConfiguration{PluginID: dynamic.PluginID, Revision: 1, Values: map[string]string{"ENDPOINT": "https://second.invalid/mcp"}}
		release, err := owner.PrepareVersions(ctx, []models.PluginVersion{dynamic}, &proposed)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		bindings, _, err := DeliveryAuthentication(dynamic, proposed, "*")
		if err != nil || len(bindings) != 1 {
			t.Fatal("version preparation did not return material for the proposed endpoint", err)
		}
		current, err := store.Configuration(ctx, dynamic.PluginID)
		if err != nil {
			t.Fatal(err)
		}
		if len(current.DeliveryMaterials) != 0 {
			t.Fatal("preflight changed persisted material before the configuration transaction")
		}
		if err := store.UpdateConfiguration(ctx, proposed, 1); err != nil {
			t.Fatal(err)
		}
		current, err = store.Configuration(ctx, dynamic.PluginID)
		if err != nil {
			t.Fatal(err)
		}
		bindings, _, err = DeliveryAuthentication(dynamic, current, "*")
		if err != nil || len(bindings) != 1 {
			t.Fatal("committed proposed configuration lost prepared material", err)
		}
	})
	preparedConfig := models.PluginConfiguration{PluginID: version.PluginID}
	release, err := owner.PrepareVersions(ctx, []models.PluginVersion{version}, &preparedConfig)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := store.UpdateDeliveryMaterials(ctx, preparedConfig); err != nil {
		t.Fatal(err)
	}
	config, err := store.Configuration(ctx, version.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if config.Revision != 0 || len(config.DeliveryMaterials) != 1 {
		t.Fatal("installation did not save material independently of ordinary configuration")
	}
	var raw models.PluginConfiguration
	if err := db.DB.Where("plugin_id = ?", version.PluginID).First(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if raw.DeliveryMaterials[0].Value != "" || raw.DeliveryMaterials[0].Ciphertext == "" {
		t.Fatal("management saved plaintext delivery material")
	}
	oldVersion := version
	secondSource := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondSource, "bridge.mjs"), []byte(script+"\n// second immutable version\n"), 0600); err != nil {
		t.Fatal(err)
	}
	secondDigest, err := plugins.ContentDigest(secondSource)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot, err := plugins.CopyContent(filepath.Join(data, "plugins/content"), secondSource, secondDigest)
	if err != nil {
		t.Fatal(err)
	}
	secondRelative, err := filepath.Rel(data, secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	version.ID, version.Digest, version.ContentPath = "version-2", secondDigest, secondRelative
	release, err = owner.PrepareVersions(ctx, []models.PluginVersion{version}, &config)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := store.UpdateDeliveryMaterials(ctx, config); err != nil {
		t.Fatal(err)
	}
	if bindings, account, err := DeliveryAuthentication(oldVersion, config, "codex"); err != nil || account != "first-user" || len(bindings) != 1 {
		t.Fatal("ordinary preparation discarded another fixed version material", err, account)
	}
	auth, _, err := owner.Bridge(version)
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login(ctx)
	if err != nil || session.State != constants.PluginAuthorizationStateReady {
		t.Fatal("asynchronous authorization did not finish", err, session.State)
	}
	registry.Close()
	// 新Core只读取仓储，不需要创建授权Registry或运行原有桥接。
	config, err = controllers.NewPluginController(db.DB).Configuration(ctx, version.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	bindings, account, err := DeliveryAuthentication(version, config, "codex")
	if err != nil || account != "second-user" || len(bindings) != 1 || bindings[0].Headers["Authorization"] != "Bearer private-managed-token-second-user" {
		t.Fatal("completed authorization material was not durable", err, account)
	}
	if bindings, account, err := DeliveryAuthentication(oldVersion, config, "codex"); err != nil || account != "" || len(bindings) != 0 {
		t.Error("reauthorization retained the previous account on another fixed version", err, account)
	}
	if err := store.UpdateConfiguration(ctx, config, 0); err != nil {
		t.Fatal(err)
	}
	config, err = store.Configuration(ctx, version.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if _, account, err := DeliveryAuthentication(version, config, "codex"); err != nil || account != "second-user" {
		t.Fatal("ordinary configuration save discarded fixed authorization material", err)
	}
	registry = commandbridge.NewRegistry(ctx, nil)
	t.Cleanup(registry.Close)
	owner = New(Config{DataDirectory: data, Bridges: registry, BridgeEnvironment: os.Environ(), Store: store})
	t.Run("ordinary-management-keeps-fixed-materials", func(t *testing.T) {
		release, err := owner.PrepareVersions(ctx, []models.PluginVersion{oldVersion}, &config)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if err := store.UpdateDeliveryMaterials(ctx, config); err != nil {
			t.Fatal(err)
		}
		for _, fixed := range []models.PluginVersion{oldVersion, version} {
			if bindings, account, err := DeliveryAuthentication(fixed, config, "codex"); err != nil || account != "second-user" || len(bindings) != 1 {
				t.Fatal("ordinary version preparation discarded a valid material", err, account)
			}
		}
	})
	auth, bridge, err := owner.Bridge(version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Status(ctx, true); err != nil {
		t.Fatal(err)
	}
	config, err = store.Configuration(ctx, version.PluginID)
	if err != nil || len(config.DeliveryMaterials) != 2 {
		t.Fatal("status verification changed fixed delivery materials", err)
	}
	private := filepath.Join(data, "plugins", "data", version.PluginID)
	t.Run("failed-login-keeps-existing-identity", func(t *testing.T) {
		flag := filepath.Join(private, "fail-start")
		if err := os.WriteFile(flag, nil, 0600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(flag)
		if _, err := auth.Login(ctx); err == nil {
			t.Fatal("failed login was reported ready")
		}
		current, err := store.Configuration(ctx, version.PluginID)
		if err != nil {
			t.Fatal(err)
		}
		for _, fixed := range []models.PluginVersion{oldVersion, version} {
			if bindings, account, err := DeliveryAuthentication(fixed, current, "codex"); err != nil || account != "second-user" || len(bindings) != 1 {
				t.Fatal("failed login discarded the unchanged identity", err, account)
			}
		}
	})
	if err := os.WriteFile(filepath.Join(private, "next-account"), []byte("third-user"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "fail-resolve"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("failed-invalidation-returns-persistence-error", func(t *testing.T) {
		if err := db.DB.Exec("CREATE TRIGGER deny_delivery_material_update BEFORE UPDATE OF delivery_materials ON plugin_configurations BEGIN SELECT RAISE(ABORT, 'delivery write denied'); END").Error; err != nil {
			t.Fatal(err)
		}
		defer db.DB.Exec("DROP TRIGGER deny_delivery_material_update")
		if _, err := auth.Login(ctx); err == nil {
			t.Fatal("failed identity material persistence was reported ready")
		}
		if err := bridge.CompleteLogin(ctx, authorization.Account{ID: "third-user", Available: true}); err != status_error.DatabaseError {
			t.Error("material invalidation swallowed the persistence failure", err)
		}
	})
	t.Run("failed-new-material-retires-prior-identity", func(t *testing.T) {
		if _, err := auth.Login(ctx); err == nil {
			t.Fatal("unavailable new material was reported ready")
		}
		current, err := store.Configuration(ctx, version.PluginID)
		if err != nil || len(current.DeliveryMaterials) != 0 || current.Revision != 1 {
			t.Error("failed new material retained the previous shared identity", err)
		}
	})
	registry.Close()
	restartedRegistry := commandbridge.NewRegistry(ctx, nil)
	defer restartedRegistry.Close()
	restarted := New(Config{DataDirectory: data, Bridges: restartedRegistry, BridgeEnvironment: os.Environ(), Store: store})
	auth, _, err = restarted.Bridge(version)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	config, err = store.Configuration(ctx, version.PluginID)
	if err != nil || len(config.DeliveryMaterials) != 0 || config.Revision != 1 {
		t.Fatal("logout retained material or changed configuration revision", err)
	}
}

func TestDeliveryMaterialIsBoundToConfiguredEndpoint(t *testing.T) {
	version := models.PluginVersion{ID: "v", PluginID: "p", Digest: "fixed", Package: plugins.Package{Integration: plugins.Integration{Authentication: &plugins.Authentication{}}, Servers: []plugins.Server{{Key: "service", URL: "${ENDPOINT}"}}}}
	first := models.PluginConfiguration{Values: map[string]string{"ENDPOINT": "https://first.invalid/mcp"}}
	second := models.PluginConfiguration{Values: map[string]string{"ENDPOINT": "https://second.invalid/mcp"}}
	if materialKey(version, first, "*") == materialKey(version, second, "*") {
		t.Fatal("changed endpoint retained the old credential identity")
	}
	first.DeliveryMaterials = []models.MCPSecretDefinition{{Name: materialKey(version, first, "*"), Configured: true, Value: `{"accountId":"first-user","bindings":[{"componentId":"component","headers":{"Authorization":"private"}}]}`}}
	second.DeliveryMaterials = first.DeliveryMaterials
	bindings, account, err := DeliveryAuthentication(version, second, "*")
	if err != nil || len(bindings) != 0 || account != "" {
		t.Fatal("a credential crossed the configured endpoint boundary", err)
	}
}
