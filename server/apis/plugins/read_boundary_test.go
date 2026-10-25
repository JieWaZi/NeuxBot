package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
)

func TestOrdinaryPluginReadsDoNotExecuteAuthorizationOrVerifyWholeContent(t *testing.T) {
	ctx := pluginAPIContext(t)
	previous := global.PluginBridges
	global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
	t.Cleanup(func() { global.PluginBridges.Close(); global.PluginBridges = previous })
	source := t.TempDir()
	for name, body := range map[string]string{
		"plugin.json":                `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"read-boundary","version":"1.0.0"}`,
		".neuxally/integration.json": `{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"./auth"}}`,
		"auth":                       "#!/bin/sh\nprintf x >> \"$0.calls\"\nwhile IFS= read -r line; do printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"state\":\"authorized\",\"account\":{\"configured\":true,\"available\":true,\"id\":\"reader\",\"scopes\":[]}}}'; done\n",
		"README.md":                  "English guide",
		"README.zh-CN.md":            "中文说明",
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
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", true)
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
	marker := filepath.Join(root, "auth.calls")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("preparing display metadata executed authorization", err)
	}
	verified, err := (&GetPluginAuthorization{PluginID: detail.Plugin.ID, Verify: true}).Output(ctx)
	if err != nil || !verified.(PluginAuthorizationResponse).Account.Available || verified.(PluginAuthorizationResponse).Account.CheckedAt == 0 {
		t.Fatal("explicit authorization did not verify real identity", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("explicit authorization did not execute bridge", err)
	}
	// 测试桥接的调用标记不属于发布内容，移除后再验证普通读取没有再次执行。
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
	if _, err := (&GetPluginAuthorization{PluginID: detail.Plugin.ID, Verify: true}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatal("authorization skipped content verification", err)
	}
	if _, err := (&InstallPlugin{PluginID: detail.Plugin.ID, Body: InstallPluginRequest{VersionID: version.ID, WakerIDs: []string{"w1"}}}).Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatal("installation skipped content verification", err)
	}
}
