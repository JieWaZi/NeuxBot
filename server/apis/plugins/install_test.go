package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/shrewx/ginx"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func pluginAPIContext(t *testing.T) *gin.Context {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "plugins.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousPlugins, previousWakers, previousTx, previousDir := global.PluginControllerManager, global.WakerControllerManager, global.TransactionControllerManager, global.Config.DataDirectory
	global.PluginControllerManager = controllers.NewPluginController(database.DB)
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	global.Config.DataDirectory = t.TempDir()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", t.TempDir())
	t.Cleanup(func() {
		global.PluginControllerManager = previousPlugins
		global.WakerControllerManager = previousWakers
		global.TransactionControllerManager = previousTx
		global.Config.DataDirectory = previousDir
		_ = sqlDB.Close()
	})
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	for _, id := range []string{"w1", "w2"} {
		if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	return ctx
}

func TestFirstInstallationRequiresConfigurationAndReportsPartnerFailures(t *testing.T) {
	ctx := pluginAPIContext(t)
	source := t.TempDir()
	content := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"test-tools","version":"1.0.0","extensions":{"com.ally":{"config":[{"key":"TOKEN","label":"访问密钥","secret":true,"required":true}]}}}`
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Plugin.ID
	version := detail.Versions[0].ID
	request := InstallPluginRequest{VersionID: version, WakerIDs: []string{"w1", "missing"}, ExpectedConfigurationRevision: 0}
	if _, err := (&InstallPlugin{PluginID: id, Body: request}).Output(ctx); !errors.Is(err, status_error.PluginNotReady) {
		t.Fatalf("installed before configuration: %v", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, id, "")
	if err != nil || len(relations) != 0 {
		t.Fatalf("failed auth created placeholder installation: %+v %v", relations, err)
	}
	if _, err := (&UpdatePluginConfiguration{PluginID: id, Body: UpdateConfigurationRequest{ExpectedRevision: 0, Secrets: []SecretValue{{Key: "TOKEN", Value: "private-token"}}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	request.ExpectedConfigurationRevision = 1
	value, err := (&InstallPlugin{PluginID: id, Body: request}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(InstallPluginResponse)
	if len(result.Results) != 2 || !result.Results[0].Installed || result.Results[1].Installed || result.Results[1].ErrorCode == "" {
		t.Fatalf("partial results incorrect: %+v", result)
	}
	got, err := global.PluginControllerManager.Configuration(ctx, id)
	if err != nil || got.Secrets[0].Value != "private-token" {
		t.Fatalf("shared config unavailable: %+v %v", got, err)
	}
}

func TestConfigurationUpgradeRequiresAllInstallationsIncludingDisabled(t *testing.T) {
	ctx := pluginAPIContext(t)
	store := global.PluginControllerManager
	if err := store.Create(ctx, models.Plugin{ID: "p", Name: "Plugin", SourceKey: catalog.SourceKey(global.PluginCatalogSource(), "tools")}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old", "new"} {
		semanticVersion := map[string]string{"old": "1.0.0", "new": "2.0.0"}[name]
		root := t.TempDir()
		manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"tools","version":"` + semanticVersion + `","extensions":{"com.ally":{"config":[{"key":"` + name + `","required":true}]}}}`
		if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		staged, err := packageplugin.Stage(t.TempDir(), root)
		if err != nil {
			t.Fatal(err)
		}
		content, err := packageplugin.CopyContent(filepath.Join(global.Config.DataDirectory, "plugins", "content"), staged.Root, staged.Digest)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(global.Config.DataDirectory, content)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutVersion(ctx, models.PluginVersion{ID: name, PluginID: "p", Version: semanticVersion, Digest: staged.Digest, ContentPath: filepath.ToSlash(relative), Package: staged.Package, Provenance: map[string]any{"sourceCommit": strings.Repeat("a", 40), "archiveSHA256": strings.Repeat("b", 64)}}); err != nil {
			t.Fatal(err)
		}
		if _, err := prepareCatalogFixture(t, ctx, staged, "", "p", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpdateConfiguration(ctx, models.PluginConfiguration{PluginID: "p", Values: map[string]string{"old": "value"}}, 0); err != nil {
		t.Fatal(err)
	}
	for _, waker := range []string{"w1", "w2"} {
		if err := store.Install(ctx, models.WakerPlugin{ID: waker, PluginID: "p", VersionID: "old", WakerID: waker, Enabled: waker == "w1"}); err != nil {
			t.Fatal(err)
		}
	}
	body := UpdateConfigurationRequest{ExpectedRevision: 1, Values: map[string]string{"new": "value"}, Switches: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: "old", VersionID: "new"}}}
	if _, err := (&UpdatePluginConfiguration{PluginID: "p", Body: body}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("incomplete installation set accepted: %v", err)
	}
	got, err := store.Configuration(ctx, "p")
	if err != nil || got.Revision != 1 || got.Values["old"] != "value" {
		t.Fatalf("failed upgrade changed config: %+v %v", got, err)
	}
	body.Switches = append(body.Switches, VersionSwitch{WakerID: "w2", ExpectedVersionID: "old", VersionID: "new"})
	if _, err := (&UpdatePluginConfiguration{PluginID: "p", Body: body}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	relations, err := store.Installations(context.Background(), "p", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range relations {
		if r.VersionID != "new" {
			t.Fatalf("partial upgrade: %+v", relations)
		}
	}
}

func TestPrepareMCPPluginRequiresExplicitContentTrust(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	for name, content := range map[string]string{"plugin.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"mcp-tools"}`, "mcp.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"tool":{"type":"stdio","command":"node"}}}`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareCatalogFixture(t, ctx, stage, "", "", false); !errors.Is(err, status_error.MCPConnectorTrustRequired) {
		t.Fatalf("untrusted MCP content accepted: %v", err)
	}
	if _, err := prepareCatalogFixture(t, ctx, stage, "", "", true); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRejectsMutatedManagedVersion(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"integrity"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	version := detail.Versions[0]
	if err := os.WriteFile(filepath.Join(global.Config.DataDirectory, version.ContentPath, "injected.sh"), []byte("arbitrary mutation"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: version.ID, WakerIDs: []string{"w1"}}}).Output(ctx)
	if !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("mutated managed content accepted: %v", err)
	}
}

func TestNativePluginUpgradeUpdatesAllPartnerPinsAndReturnsPreparedVersion(t *testing.T) {
	ctx := pluginAPIContext(t)
	prepare := func(target, version, body string) (PluginDetailResponse, error) {
		source := t.TempDir()
		for filename, content := range map[string]string{
			".codex-plugin/plugin.json": `{"name":"upstream-tools","version":"` + version + `","skills":"./skills/","author":{"name":"Upstream"}}`,
			"skills/reports/SKILL.md":   "---\nname: reports\ndescription: Reports\n---\n" + body,
		} {
			path := filepath.Join(source, filename)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		stage, err := packageplugin.Stage(stagedRoot(), source)
		if err != nil {
			return PluginDetailResponse{}, err
		}
		value, err := prepareCatalogFixture(t, ctx, stage, "", target, false)
		if err != nil {
			return PluginDetailResponse{}, err
		}
		return value, nil
	}
	old, err := prepare("", "1.0.0", "Original instructions")
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := old.ResolvedVersionID
	if _, err := (&InstallPlugin{PluginID: old.Plugin.ID, Body: InstallPluginRequest{VersionID: oldVersion, WakerIDs: []string{"w1", "w2"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if err := global.PluginControllerManager.SetEnabled(ctx, "w2", old.Plugin.ID, false); err != nil {
		t.Fatal(err)
	}
	next, err := prepare(old.Plugin.ID, "2.0.0", "New instructions")
	if err != nil || len(next.Versions) != 2 {
		t.Fatalf("new version not retained: %+v %v", next, err)
	}
	newVersion := next.ResolvedVersionID
	for _, relation := range next.Installations {
		if relation.VersionID != oldVersion {
			t.Fatal("preparation silently updated an installed partner")
		}
	}
	if _, err := (&UpdatePluginConfiguration{PluginID: old.Plugin.ID, Body: UpdateConfigurationRequest{Switches: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: oldVersion, VersionID: newVersion}, {WakerID: "w2", ExpectedVersionID: oldVersion, VersionID: newVersion}}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, old.Plugin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, relation := range relations {
		if relation.WakerID == "w1" && (relation.VersionID != newVersion || !relation.Enabled) {
			t.Fatalf("selected partner did not update: %+v", relation)
		}
		if relation.WakerID == "w2" && (relation.VersionID != newVersion || relation.Enabled) {
			t.Fatalf("disabled partner did not update while preserving disabled state: %+v", relation)
		}
	}
	if _, err := prepare(old.Plugin.ID, "2.0.0", "Changed content under the same version"); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("same version overwrote immutable content: %v", err)
	}
	preparedOld, err := prepare(old.Plugin.ID, "1.0.0", "Original instructions")
	if err != nil || preparedOld.ResolvedVersionID != oldVersion {
		t.Fatalf("prepared response selected another version: %+v %v", preparedOld, err)
	}
}

func TestSupplementalAuthorizationScopesMustBeExplicitIdentifiers(t *testing.T) {
	for _, scope := range []string{"", "scope with spaces", "scope\n--domain all", "permission;command"} {
		operator := StartPluginAuthorization{Body: AuthorizationScopeRequest{AdditionalScopes: []string{scope}}}
		if err := operator.Validate(nil); err == nil {
			t.Fatalf("invalid supplemental scope accepted: %q", scope)
		}
	}
	if err := (&StartPluginAuthorization{Body: AuthorizationScopeRequest{AdditionalScopes: []string{"calendar:calendar.event:create", "task:task:write"}}}).Validate(nil); err != nil {
		t.Fatal(err)
	}
}

func TestVersionSwitchRejectsUnreadyTargetsWithoutChangingInstallation(t *testing.T) {
	for _, kind := range []string{"missing-component", "changed-content"} {
		t.Run(kind, func(t *testing.T) {
			ctx := pluginAPIContext(t)
			prepare := func(id, version, extension string) PluginDetailResponse {
				root := t.TempDir()
				body := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"upgrade","version":"` + version + `","extensions":{"com.ally":` + extension + `}}`
				if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				stage, err := packageplugin.Stage(stagedRoot(), root)
				if err != nil {
					t.Fatal(err)
				}
				value, err := prepareCatalogFixture(t, ctx, stage, "", id, false)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			old := prepare("", "1.0.0", `{}`)
			oldID := old.Versions[0].ID
			if _, err := (&InstallPlugin{PluginID: old.Plugin.ID, Body: InstallPluginRequest{VersionID: oldID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
				t.Fatal(err)
			}
			extension := `{}`
			if kind == "missing-component" {
				extension = `{"requiredComponents":["missing"]}`
			}
			next := prepare(old.Plugin.ID, "2.0.0", extension)
			var target models.PluginVersion
			for _, v := range next.Versions {
				if v.Version == "2.0.0" {
					target = v
				}
			}
			if target.ID == "" {
				t.Fatal("target missing")
			}
			if kind == "changed-content" {
				root, err := packageplugin.ContentPath(global.Config.DataDirectory, target.ContentPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte("changed"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := (&UpdatePluginConfiguration{PluginID: old.Plugin.ID, Body: UpdateConfigurationRequest{ExpectedRevision: 0, Switches: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: oldID, VersionID: target.ID}}}}).Output(ctx)
			if err == nil {
				t.Error("unready version replaced working installation")
			}
			installs, err := global.PluginControllerManager.Installations(ctx, old.Plugin.ID, "")
			if err != nil || len(installs) != 1 || installs[0].VersionID != oldID {
				t.Errorf("failed switch changed version: %+v %v", installs, err)
			}
			config, err := global.PluginControllerManager.Configuration(ctx, old.Plugin.ID)
			if err != nil || config.Revision != 0 {
				t.Errorf("failed switch changed config: %+v %v", config, err)
			}
		})
	}
}

func TestInstallRejectsExistingLocalImportWithoutChangingResource(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"legacy-local","version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := persistPluginFixture(t, ctx, stage, "", "local:"+stage.Digest, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx)
	if !errors.Is(err, status_error.PluginForbidden) {
		t.Fatalf("local import remained installable: %v", err)
	}
	if _, err := global.PluginControllerManager.Get(ctx, detail.Plugin.ID); err != nil {
		t.Fatal("legacy resource was deleted", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "")
	if err != nil || len(relations) != 0 {
		t.Fatalf("rejected import created installation: %+v %v", relations, err)
	}
}

func TestPluginImportRoutesAreUnavailable(t *testing.T) {
	if raw := os.Getenv("NEUXALLY_PLUGIN_ROUTE_TEST_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatal(err)
		}
		pluginAPIContext(t)
		ginx.RunServer(&conf.Server{Host: "127.0.0.1", Port: port, Log: &conf.Log{ToStdout: true}}, Router)
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestPluginImportRoutesAreUnavailable$")
	command.Env = append(os.Environ(), "NEUXALLY_PLUGIN_ROUTE_TEST_PORT="+strconv.Itoa(port))
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	address := "http://127.0.0.1:" + strconv.Itoa(port)
	client := http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := client.Get(address + "/health")
		if err == nil {
			_ = response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("temporary HTTP server unavailable", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, path := range []string{"/plugins/scan", "/plugins/prepare"} {
		response, err := client.Post(address+path, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("removed import route %s returned %d", path, response.StatusCode)
		}
	}
}

// prepareCatalogFixture 发布临时固定清单与 ZIP，通过真实目录接口准备测试资源。
func prepareCatalogFixture(t *testing.T, ctx *gin.Context, stage packageplugin.StagedPackage, owner, target string, trusted bool) (PluginDetailResponse, error) {
	t.Helper()
	root := global.PluginCatalogSource()
	key := stage.Package.Manifest.Name
	if target != "" {
		resource, err := global.PluginControllerManager.Get(ctx, target)
		if err != nil {
			return PluginDetailResponse{}, err
		}
		parts := strings.Split(resource.SourceKey, ":")
		key = parts[len(parts)-1]
	}
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "catalog", key+".zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	err = filepath.WalkDir(stage.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(stage.Root, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		output, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = output.Write(body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	entries := []any{}
	if previous, err := os.ReadFile(filepath.Join(root, "catalog.json")); err == nil {
		var document map[string]any
		if json.Unmarshal(previous, &document) != nil {
			t.Fatal("invalid fixture catalog")
		}
		items, _ := document["entries"].([]any)
		for _, item := range items {
			entry, _ := item.(map[string]any)
			if entry["key"] != key {
				entries = append(entries, item)
			}
		}
	}
	entries = append(entries, map[string]any{"key": key, "directory": "catalog/" + key + ".zip", "sourceCommit": strings.Repeat("a", 40), "archiveSHA256": fmt.Sprintf("%x", sum), "package": stage.Package, "packagePath": stage.Package.SourcePath})
	index, err := json.Marshal(map[string]any{"schemaVersion": 1, "entries": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), index, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := (&PrepareCatalogPlugin{Key: key, Body: PrepareCatalogPluginRequest{OwnerWakerID: owner, TrustConfirmed: trusted}}).Output(ctx)
	if err != nil {
		return PluginDetailResponse{}, err
	}
	return output.(PluginDetailResponse), nil
}

// persistPluginFixture 只构造已有数据库记录，用于验证旧数据和旧解析元数据边界。
func persistPluginFixture(t *testing.T, ctx *gin.Context, stage packageplugin.StagedPackage, owner, source string, provenance map[string]any) (PluginDetailResponse, error) {
	t.Helper()
	id := util.New()
	resource := models.Plugin{ID: id, Name: stage.Package.Manifest.Name, OwnerWakerID: owner, SourceKey: source}
	if err := global.PluginControllerManager.Create(ctx, resource); err != nil {
		return PluginDetailResponse{}, err
	}
	content, err := packageplugin.CopyContent(filepath.Join(global.Config.DataDirectory, "plugins", "content"), stage.Root, stage.Digest)
	if err != nil {
		return PluginDetailResponse{}, err
	}
	relative, err := filepath.Rel(global.Config.DataDirectory, content)
	if err != nil {
		return PluginDetailResponse{}, err
	}
	version, err := global.PluginControllerManager.PutVersion(ctx, models.PluginVersion{ID: util.New(), PluginID: id, Version: stage.Package.Manifest.Version, Digest: stage.Digest, ContentPath: filepath.ToSlash(relative), Package: stage.Package, TrustedAt: 1, Provenance: provenance})
	if err != nil {
		return PluginDetailResponse{}, err
	}
	return pluginDetail(ctx, resource, version.ID)
}

func TestExistingPinSurvivesUnavailableCatalogAndNewInstallRejects(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"pinned","version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(global.PluginCatalogSource()); err != nil {
		t.Fatal(err)
	}
	visible, readErr := (&GetPlugin{PluginID: detail.Plugin.ID}).Output(ctx)
	if readErr != nil || visible.(PluginDetailResponse).CatalogSource.State != "unavailable" || visible.(PluginDetailResponse).Update.Available || len(visible.(PluginDetailResponse).Installations) != 1 {
		t.Fatalf("unavailable catalog blocked saved plugin detail: %+v %v", visible, readErr)
	}

	_, err = (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w2"}}}).Output(ctx)
	if !errors.Is(err, status_error.PluginStorageError) {
		t.Fatalf("new installation accepted an unverifiable source: %v", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "")
	if err != nil || len(relations) != 1 || relations[0].WakerID != "w1" || relations[0].VersionID != detail.ResolvedVersionID {
		t.Fatalf("unavailable catalog changed existing pin: %+v %v", relations, err)
	}

}

func TestInstallRejectsUnverifiedOrOtherCatalogSources(t *testing.T) {
	for _, kind := range []string{"other-catalog", "empty-source", "missing-provenance", "invalid-commit", "invalid-archive"} {
		t.Run(kind, func(t *testing.T) {
			ctx := pluginAPIContext(t)
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"stored","version":"1.0.0"}`), 0600); err != nil {
				t.Fatal(err)
			}
			stage, err := packageplugin.Stage(stagedRoot(), root)
			if err != nil {
				t.Fatal(err)
			}
			source := catalog.SourceKey(global.PluginCatalogSource(), "stored")
			provenance := map[string]any{"sourceCommit": strings.Repeat("a", 40), "archiveSHA256": strings.Repeat("b", 64)}
			switch kind {
			case "other-catalog":
				source = catalog.SourceKey(t.TempDir(), "stored")
			case "empty-source":
				source = ""
			case "missing-provenance":
				provenance = nil
			case "invalid-commit":
				provenance["sourceCommit"] = strings.Repeat("z", 40)
			case "invalid-archive":
				provenance["archiveSHA256"] = strings.Repeat("z", 64)
			}
			detail, err := persistPluginFixture(t, ctx, stage, "", source, provenance)
			if err != nil {
				t.Fatal(err)
			}
			_, err = (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx)
			if !errors.Is(err, status_error.PluginForbidden) {
				t.Fatalf("unverified source accepted: %v", err)
			}
			relations, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "")
			if err != nil || len(relations) != 0 {
				t.Fatalf("rejected source created installation: %+v %v", relations, err)
			}
		})
	}
}

func TestVersionSwitchRejectsLegacyImportedVersionWithoutChangingPin(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"switch","version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	legacy := detail.Versions[0]
	legacy.ID, legacy.Version, legacy.Provenance = "imported-version", "local-import", nil
	if _, err := global.PluginControllerManager.PutVersion(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	_, err = (&UpdatePluginConfiguration{PluginID: detail.Plugin.ID, Body: UpdateConfigurationRequest{Switches: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: detail.ResolvedVersionID, VersionID: legacy.ID}}}}).Output(ctx)
	if !errors.Is(err, status_error.PluginForbidden) {
		t.Fatalf("version switch installed an unverified import: %v", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "")
	if err != nil || len(relations) != 1 || relations[0].VersionID != detail.ResolvedVersionID {
		t.Fatalf("rejected switch changed pin: %+v %v", relations, err)
	}
}

func TestCatalogPreparationRejectsArchiveMismatchBeforeResourceRegistration(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := global.PluginCatalogSource()
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog/broken.zip"), []byte("tampered archive"), 0600); err != nil {
		t.Fatal(err)
	}
	index, _ := json.Marshal(map[string]any{"schemaVersion": 1, "entries": []map[string]any{{"key": "broken", "directory": "catalog/broken.zip", "sourceCommit": strings.Repeat("a", 40), "archiveSHA256": strings.Repeat("b", 64), "package": map[string]any{"manifest": map[string]any{"name": "broken"}}}}})
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), index, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PrepareCatalogPlugin{Key: "broken"}).Output(ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("unverified archive reached preparation: %v", err)
	}
	_, total, err := global.PluginControllerManager.List(ctx, "", "", "", 0, 100, false)
	if err != nil || total != 0 {
		t.Fatalf("archive mismatch registered resource: total=%d error=%v", total, err)
	}
}

func TestInstallRejectsLocalContentWithCopiedCatalogIdentityAndProvenance(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"catalog-owned","version":"1.0.0"}`
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	stage, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	version := detail.Versions[0]
	provenance, err := json.Marshal(version.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "provenance.json"), provenance, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(strings.Replace(manifest, `"version":"1.0.0"`, `"version":"2.0.0"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	imported, err := packageplugin.Stage(stagedRoot(), root)
	if err != nil {
		t.Fatal(err)
	}
	content, err := packageplugin.CopyContent(filepath.Join(global.Config.DataDirectory, "plugins/content"), imported.Root, imported.Digest)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(global.Config.DataDirectory, content)
	if err != nil {
		t.Fatal(err)
	}
	version.ID, version.Version, version.Digest, version.Package, version.ContentPath = "copied-provenance", "2.0.0", imported.Digest, imported.Package, filepath.ToSlash(relative)
	if _, err := global.PluginControllerManager.PutVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: version.ID, WakerIDs: []string{"w2"}}}).Output(ctx)
	if !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("copied catalog identity and package-controlled provenance authorized local bytes: %v", err)
	}
	_, err = (&UpdatePluginConfiguration{PluginID: detail.Plugin.ID, Body: UpdateConfigurationRequest{Switches: []VersionSwitch{{WakerID: "w1", ExpectedVersionID: detail.ResolvedVersionID, VersionID: version.ID}}}}).Output(ctx)
	if !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("copied provenance authorized an imported version switch: %v", err)
	}
	relations, err := global.PluginControllerManager.Installations(ctx, detail.Plugin.ID, "")
	if err != nil || len(relations) != 1 || relations[0].WakerID != "w1" || relations[0].VersionID != detail.ResolvedVersionID {
		t.Fatalf("unverified content changed installations: %+v %v", relations, err)
	}
}

func TestCatalogInstallRevalidatesCachedArchiveWhenDistributionIsUnavailable(t *testing.T) {
	ctx := pluginAPIContext(t)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, body := range map[string]string{".codex-plugin/plugin.json": `{"name":"cached-owned","version":"1.0.0"}`, "README.md": "Unique cache fixture " + t.TempDir()} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	var available atomic.Bool
	available.Store(true)
	var downloads atomic.Int32
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog.json" {
			_, _ = fmt.Fprintf(w, `{"schemaVersion":2,"entries":[{"key":"cached-owned","name":"cached-owned","version":"1.0.0","format":"codex","sourceCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","archiveSHA256":"%x","archiveURL":%q,"packagePath":"."}]}`, sum, source.URL+"/owned.zip")
			return
		}
		downloads.Add(1)
		if !available.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(archive.Bytes())
	}))
	defer source.Close()
	location := source.URL + "/catalog.json"
	stopCatalog := catalog.Start(context.Background(), location, t.TempDir(), nil)
	defer stopCatalog()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", location)
	value, err := (&PrepareCatalogPlugin{Key: "cached-owned"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	detail := value.(PluginDetailResponse)
	available.Store(false)
	source.Close()
	value, err = (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: detail.ResolvedVersionID, WakerIDs: []string{"w1"}}}).Output(ctx)
	if err != nil || !value.(InstallPluginResponse).Results[0].Installed {
		t.Fatalf("verified fixed archive cache could not prove catalog content: %+v %v", value, err)
	}
	if downloads.Load() != 1 {
		t.Fatalf("catalog installation redownloaded the validated fixed archive: %d", downloads.Load())
	}
}
