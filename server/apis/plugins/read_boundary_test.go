package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestOrdinaryPluginReadsDoNotExecuteAuthorizationOrVerifyWholeContent(t *testing.T) {
	ctx := pluginAPIContext(t)
	previous := global.PluginBridges
	global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(func() { global.PluginBridges.Close(); global.PluginBridges = previous })
	source := t.TempDir()
	for name, body := range map[string]string{
		"plugin.json":       `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"read-boundary","version":"1.0.0","extensions":{"com.neuxbot":{"schemaVersion":1,"authentication":{"strategy":"plugin-command","protocol":"neuxbot-auth/1","command":"node","args":[".neuxbot/auth.mjs"]}}}}`,
		".neuxbot/auth.mjs": stageOneAPIDriver,
		"README.md":         "English guide",
		"README.zh-CN.md":   "中文说明",
	} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", true)
	if err != nil {
		t.Fatal(err)
	}
	version, err := global.PluginControllerManager.Version(ctx, detail.ResolvedVersionID)
	if err != nil {
		t.Fatal(err)
	}
	root, err := packageplugin.ContentPath(global.Config.DataDirectory, version.ContentPath)
	if err != nil {
		t.Fatal(err)
	}
	previousContents := global.PluginContents
	collections := 0
	global.PluginContents = packageplugin.NewContentStore(global.Config.DataDirectory, func() ([]string, error) {
		collections++
		return []string{version.ContentPath}, nil
	})
	t.Cleanup(func() { global.PluginContents = previousContents })
	marker := filepath.Join(global.Config.DataDirectory, "plugins/data", detail.Plugin.ID, "calls")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("preparing display metadata executed authorization", err)
	}
	verified, err := (&CheckPluginAccount{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID}).Output(ctx)
	if err != nil || !verified.(PluginAuthorizationResponse).Account.Available || verified.(PluginAuthorizationResponse).Account.CheckedAt == 0 {
		t.Fatal("explicit authorization did not verify real identity", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("explicit authorization did not execute bridge", err)
	}
	// 显式调用标记只在插件私有目录，普通读取不得追加。
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	// 与展示无关的文件损坏不能阻止普通读取，但安装和授权必须继续拒绝。
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	listed, err := (&ListPlugins{}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.(ListPluginsResponse).Resources) != 1 || !listed.(ListPluginsResponse).Resources[0].Account.Available {
		t.Fatal("missing saved plugin")
	}
	got, err := (&GetPlugin{PluginID: detail.Plugin.ID, Locale: "zh-CN"}).Output(ctx)
	if err != nil {
		t.Fatal("ordinary detail scanned or reparsed package", err)
	}
	if got.(PluginDetailResponse).Versions[0].Package.Readme != "中文说明" {
		t.Fatal("localized README was lost")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("ordinary query executed authorization", err)
	}
	if collections != 0 {
		t.Fatal("ordinary detail triggered global content collection", collections)
	}
	if _, err := (&CheckPluginAccount{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatal("authorization skipped content verification", err)
	}
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: version.ID, WakerIDs: []string{"w1"}}}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatal("installation skipped content verification", err)
	}
}

func TestPluginDetailRejectsUnsupportedPackageContracts(t *testing.T) {
	for _, scenario := range []struct {
		// name 是历史快照或无效声明的场景名称。
		name string
		// contract 是快照保存的解析契约。
		contract string
		// extension 是包清单内的真实扩展声明。
		extension string
		// expected 是严格包读取应返回的稳定业务错误。
		expected error
	}{
		{name: "legacy-provider", extension: `"com.ally":{"provider":"feishu"}`, expected: status_error.PluginPackageUnsupported},
		{name: "unmarked-standard", expected: status_error.PluginPackageUnsupported},
		{name: "invalid-current", contract: constants.PluginTargetContractVersion, extension: `"com.ally":{"provider":"feishu"}`, expected: status_error.PluginManifestInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := pluginAPIContext(t)
			source := t.TempDir()
			manifest := `{"$schema":"` + constants.PluginManifestSchema + `","name":"retained","version":"1.0.0","extensions":{` + scenario.extension + `}}`
			for name, body := range map[string]string{"plugin.json": manifest, "README.md": "Retained overview", "skills/reports/SKILL.md": "---\nname: reports\ndescription: Reports\n---\nRetained instructions"} {
				path := filepath.Join(source, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			digest, err := packageplugin.ContentDigest(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := packageplugin.CopyContent(filepath.Join(global.Config.DataDirectory, "plugins/content"), source, digest); err != nil {
				t.Fatal(err)
			}
			pkg := packageplugin.Package{ContractVersion: scenario.contract, Format: constants.PluginFormatPortable, Manifest: packageplugin.Manifest{Schema: constants.PluginManifestSchema, Name: "retained", Version: "1.0.0"}, Skills: []packageplugin.Skill{{Key: "skills/reports", Path: "skills/reports", Name: "reports"}}}
			if scenario.extension != "" {
				pkg.Manifest.Extensions = map[string]json.RawMessage{"com.ally": json.RawMessage(`{"provider":"feishu"}`)}
			}
			store := global.PluginControllerManager
			if err := store.Create(ctx, models.Plugin{ID: "retained", Name: "Retained", SourceKey: "catalog:retained"}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.PutVersion(ctx, models.PluginVersion{ID: "retained-v1", PluginID: "retained", Version: "1.0.0", Digest: digest, ContentPath: "plugins/content/" + digest, Package: pkg}); err != nil {
				t.Fatal(err)
			}
			if err := store.Install(ctx, models.WakerPlugin{PluginID: "retained", VersionID: "retained-v1", WakerID: "w1", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			if _, err := (&GetPlugin{PluginID: "retained", VersionID: "retained-v1", WakerID: "w1"}).Output(ctx); !errors.Is(err, scenario.expected) {
				t.Fatalf("unsupported package was returned as a detail response: %v", err)
			}
			if _, err := (&GetPluginSkill{PluginID: "retained", VersionID: "retained-v1", Key: "skills/reports", WakerID: "w1"}).Output(ctx); !errors.Is(err, scenario.expected) {
				t.Fatalf("unsupported skill read: %v", err)
			}
			saved, err := store.Version(ctx, "retained-v1")
			if err != nil || saved.Package.ContractVersion != scenario.contract || saved.Digest != digest {
				t.Fatal("ordinary read rewrote the immutable version", err)
			}
			if err := packageplugin.VerifyContent(filepath.Join(global.Config.DataDirectory, saved.ContentPath), digest); err != nil {
				t.Fatal("ordinary read rewrote original files", err)
			}
		})
	}
}
