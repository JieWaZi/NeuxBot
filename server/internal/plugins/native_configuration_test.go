package plugins_test

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginapi "github.com/JieWaZi/NeuxBot/server/apis/plugins"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func nativeConfigurationFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func nativeConfigurationSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	nativeConfigurationFile(t, root, "neuxally-plugin/github/.codex-plugin/plugin.json", `{"name":"github","version":"1.0.0"}`)
	nativeConfigurationFile(t, root, "neuxally-plugin/github/.neuxally/integration.json", `{"nativePlugins":{"codex":{"path":"codex-plugins/github"},"claude":{"path":"claude-plugins/github"}}}`)
	nativeConfigurationFile(t, root, "codex-plugins/github/.codex-plugin/plugin.json", `{"name":"github-codex","version":"1.0.0"}`)
	nativeConfigurationFile(t, root, "claude-plugins/github/.claude-plugin/plugin.json", `{"name":"github-claude","version":"1.0.0"}`)
	// 保留公开 Claude GitHub 包的直接服务映射与真实配置键，使用虚构秘密值验证保存边界。
	nativeConfigurationFile(t, root, "claude-plugins/github/.mcp.json", `{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","headers":{"Authorization":"Bearer ${GITHUB_PERSONAL_ACCESS_TOKEN}"}}}`)
	return root
}

func TestNativeDirectMCPMapCollectsServersAndTrust(t *testing.T) {
	root := nativeConfigurationSource(t)
	pkg, err := plugins.Load(filepath.Join(root, "claude-plugins/github"))
	if err != nil || len(pkg.Servers) != 1 || pkg.Servers[0].Key != "github" || pkg.Servers[0].Type != "streamable-http" {
		t.Fatalf("direct native MCP map lost its service: %+v %v", pkg, err)
	}
	if !pkg.RequiresTrust() || len(pkg.Diagnostics) != 0 {
		t.Fatalf("direct native MCP map lost its trust requirement: %+v", pkg)
	}
	extension, err := plugins.ClientExtension(pkg)
	if err != nil || len(extension.Config) != 1 || extension.Config[0].Key != "GITHUB_PERSONAL_ACCESS_TOKEN" || !extension.Config[0].Secret || !extension.Config[0].Required {
		t.Fatalf("native MCP credential was not recognized: %+v %v", extension, err)
	}
}

func TestLogicalNativeConfigurationIsEditableWithoutRequiringOtherTargets(t *testing.T) {
	root := nativeConfigurationSource(t)
	pkg, err := plugins.LoadSource(root, "neuxally-plugin/github")
	if err != nil {
		t.Fatal(err)
	}
	extension, err := plugins.ClientExtension(pkg)
	if err != nil || len(extension.Config) != 1 || extension.Config[0].Key != "GITHUB_PERSONAL_ACCESS_TOKEN" || !extension.Config[0].Secret || extension.Config[0].Required {
		t.Fatalf("logical plugin did not expose optional native credentials: %+v %v", extension, err)
	}
	if err := plugins.ValidateValues(pkg, nil, nil); err != nil {
		t.Fatalf("Claude-only PAT blocked installation for another target: %v", err)
	}
	if err := plugins.ValidateValues(pkg, nil, map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "fixture-pat"}); err != nil {
		t.Fatalf("logical plugin rejected its native credential: %v", err)
	}
	if err := plugins.ValidateValues(pkg, map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "fixture-pat"}, nil); !errors.Is(err, status_error.PluginConfigurationConflict) {
		t.Fatalf("native credential accepted outside protected secrets: %v", err)
	}
	claude, _, err := plugins.SelectTarget(root, pkg, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := plugins.ValidateValues(claude, nil, nil); !errors.Is(err, status_error.PluginNotReady) {
		t.Fatalf("selected Claude target lost its required PAT: %v", err)
	}
	codex, _, err := plugins.SelectTarget(root, pkg, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := plugins.ValidateValues(codex, nil, nil); err != nil {
		t.Fatalf("Codex target inherited another target's PAT requirement: %v", err)
	}
	if !pkg.RequiresTrust() || pkg.NativeImplementations["claude"].ServerCount != 1 {
		t.Fatalf("logical plugin did not inherit native MCP trust: %+v", pkg)
	}
	if fields := pkg.NativeImplementations["claude"].Config; len(fields) != 1 || fields[0].Key != "GITHUB_PERSONAL_ACCESS_TOKEN" || !fields[0].Required {
		t.Fatalf("native implementation summary lost its own required fields: %+v", fields)
	}
}

func TestDeclaredAuthenticationRequiresTrustBeforePrepare(t *testing.T) {
	for _, strategy := range []string{"plugin-command", "future-plugin-command"} {
		t.Run(strategy, func(t *testing.T) {
			ctx, _, _ := nativeConfigurationContext(t)
			root := t.TempDir()
			nativeConfigurationFile(t, root, ".codex-plugin/plugin.json", `{"name":"skills-and-auth"}`)
			nativeConfigurationFile(t, root, "skills/demo/SKILL.md", "---\nname: demo\ndescription: Example skill\n---\nExample instructions\n")
			nativeConfigurationFile(t, root, ".neuxally/integration.json", `{"authentication":{"strategy":"`+strategy+`","protocol":"neuxally-auth/1","command":"node","args":["./.neuxally/auth/check.js"]}}`)
			nativeConfigurationFile(t, root, ".neuxally/auth/check.js", "process.exit(0)\n")
			stage, err := plugins.Stage(filepath.Join(global.Config.DataDirectory, "plugins/staging"), root)
			if err != nil {
				t.Fatal(err)
			}
			if !stage.Package.RequiresTrust() {
				t.Fatal("executable authentication declaration bypassed trust")
			}
			if _, err := nativeCatalogPreparation(t, ctx, stage, false); !errors.Is(err, status_error.MCPConnectorTrustRequired) {
				t.Fatalf("authentication package was prepared without trust: %v", err)
			}
		})
	}
}

func TestReferencedNativeAuthenticationRequiresLogicalTrust(t *testing.T) {
	root := nativeConfigurationSource(t)
	if err := os.Remove(filepath.Join(root, "claude-plugins/github/.mcp.json")); err != nil {
		t.Fatal(err)
	}
	nativeConfigurationFile(t, root, "claude-plugins/github/.neuxally/integration.json", `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["./.neuxally/auth/check.js"]}}`)
	nativeConfigurationFile(t, root, "claude-plugins/github/.neuxally/auth/check.js", "process.exit(0)\n")
	pkg, err := plugins.LoadSource(root, "neuxally-plugin/github")
	if err != nil {
		t.Fatal(err)
	}
	if !pkg.RequiresTrust() {
		t.Fatal("logical reference hid an executable native authentication entry")
	}
}

func TestLogicalNativeConfigurationRejectsConflictingSharedKeys(t *testing.T) {
	for name, configs := range map[string][2]string{
		"default": {`{"key":"SHARED","secret":false,"default":"one"}`, `{"key":"SHARED","secret":false,"default":"two"}`},
		"secret":  {`{"key":"SHARED","secret":true}`, `{"key":"SHARED","secret":false}`},
	} {
		t.Run(name, func(t *testing.T) {
			root := nativeConfigurationSource(t)
			nativeConfigurationFile(t, root, "codex-plugins/github/.codex-plugin/plugin.json", `{"name":"github-codex","extensions":{"com.ally":{"config":[`+configs[0]+`]}}}`)
			nativeConfigurationFile(t, root, "claude-plugins/github/.claude-plugin/plugin.json", `{"name":"github-claude","extensions":{"com.ally":{"config":[`+configs[1]+`]}}}`)
			if _, err := plugins.LoadSource(root, "neuxally-plugin/github"); !errors.Is(err, status_error.PluginManifestInvalid) {
				t.Fatalf("conflicting target definitions were silently combined: %v", err)
			}
		})
	}
}

func nativeConfigurationContext(t *testing.T) (*gin.Context, controllers.PluginController, func(string) models.PluginConfiguration) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "native-configuration.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousPlugins, previousWakers, previousTx, previousDirectory := global.PluginControllerManager, global.WakerControllerManager, global.TransactionControllerManager, global.Config.DataDirectory
	store := controllers.NewPluginController(database.DB)
	global.PluginControllerManager = store
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	global.Config.DataDirectory = t.TempDir()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", t.TempDir())
	t.Cleanup(func() {
		global.PluginControllerManager, global.WakerControllerManager, global.TransactionControllerManager, global.Config.DataDirectory = previousPlugins, previousWakers, previousTx, previousDirectory
		_ = sqlDB.Close()
	})
	ctx, _ := gin.CreateTestContext(nil)
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "native-waker", Name: "配置测试"}); err != nil {
		t.Fatal(err)
	}
	return ctx, store, func(id string) models.PluginConfiguration {
		var raw models.PluginConfiguration
		if err := database.DB.Where("plugin_id = ?", id).First(&raw).Error; err != nil {
			t.Fatal(err)
		}
		return raw
	}
}

func TestLogicalNativeConfigurationRequiresTrustAndEncryptsPAT(t *testing.T) {
	ctx, store, rawConfiguration := nativeConfigurationContext(t)
	source := nativeConfigurationSource(t)
	stage, err := plugins.StageSource(filepath.Join(global.Config.DataDirectory, "plugins/staging"), source, "neuxally-plugin/github")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nativeCatalogPreparation(t, ctx, stage, false); !errors.Is(err, status_error.MCPConnectorTrustRequired) {
		t.Fatalf("logical native MCP bypassed explicit trust: %v", err)
	}
	prepared, err := nativeCatalogPreparation(t, ctx, stage, true)
	if err != nil {
		t.Fatal(err)
	}
	detail := prepared.(pluginapi.PluginDetailResponse)
	if len(detail.Fields) != 1 || detail.Fields[0].Key != "GITHUB_PERSONAL_ACCESS_TOKEN" || !detail.Fields[0].Secret || detail.Fields[0].Required || detail.Versions[0].TrustedAt == 0 {
		t.Fatalf("prepared logical resource lost native configuration or trust: %+v", detail)
	}
	installed, err := (&pluginapi.InstallPlugin{PluginID: detail.Plugin.ID, Body: pluginapi.InstallPluginRequest{VersionID: detail.Versions[0].ID, WakerIDs: []string{"native-waker"}}}).Output(ctx)
	if err != nil || !installed.(pluginapi.InstallPluginResponse).Results[0].Installed {
		t.Fatalf("unused target credential blocked logical installation: %+v %v", installed, err)
	}
	if _, err := (&pluginapi.UpdatePluginConfiguration{PluginID: detail.Plugin.ID, Body: pluginapi.UpdateConfigurationRequest{Values: map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "fixture-private-pat"}}}).Output(ctx); !errors.Is(err, status_error.PluginConfigurationConflict) {
		t.Fatalf("native PAT accepted as an ordinary visible value: %v", err)
	}
	if _, err := (&pluginapi.UpdatePluginConfiguration{PluginID: detail.Plugin.ID, Body: pluginapi.UpdateConfigurationRequest{Secrets: []pluginapi.SecretValue{{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Value: "fixture-private-pat"}}}}).Output(ctx); err != nil {
		t.Fatalf("native PAT could not use the protected configuration save path: %v", err)
	}
	raw := rawConfiguration(detail.Plugin.ID)
	if len(raw.Secrets) != 1 || raw.Secrets[0].Value != "" || raw.Secrets[0].Ciphertext == "" || strings.Contains(raw.Secrets[0].Ciphertext, "fixture-private-pat") {
		t.Fatalf("native PAT was not encrypted at rest: %+v", raw)
	}
	config, err := store.Configuration(ctx, detail.Plugin.ID)
	if err != nil || config.Secrets[0].Value != "fixture-private-pat" || config.Secrets[0].Ciphertext != "" {
		t.Fatalf("protected native PAT could not be resolved for execution: %+v %v", config, err)
	}
	encoded, err := json.Marshal(config)
	if err != nil || strings.Contains(string(encoded), "fixture-private-pat") || strings.Contains(string(encoded), "GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Fatalf("configuration response exposed protected native PAT: %s %v", encoded, err)
	}
}

// nativeCatalogPreparation 通过临时固定目录保留原生配置、信任与秘密保存的真实 API 链路。
func nativeCatalogPreparation(t *testing.T, ctx *gin.Context, stage plugins.StagedPackage, trusted bool) (interface{}, error) {
	t.Helper()
	root := global.PluginCatalogSource()
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "catalog/native-config.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	if err := filepath.WalkDir(stage.Root, func(path string, entry fs.DirEntry, err error) error {
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
	}); err != nil {
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
	index, err := json.Marshal(map[string]any{"schemaVersion": 1, "entries": []map[string]any{{"key": "native-config", "directory": "catalog/native-config.zip", "sourceCommit": strings.Repeat("a", 40), "archiveSHA256": fmt.Sprintf("%x", sum), "packagePath": stage.Package.SourcePath, "package": stage.Package}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), index, 0600); err != nil {
		t.Fatal(err)
	}
	return (&pluginapi.PrepareCatalogPlugin{Key: "native-config", Body: pluginapi.PrepareCatalogPluginRequest{TrustConfirmed: trusted}}).Output(ctx)
}
