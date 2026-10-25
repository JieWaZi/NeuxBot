package plugins

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestInstalledPluginListFiltersBeforeCountingAndPaging(t *testing.T) {
	ctx := pluginAPIContext(t)
	store := global.PluginControllerManager
	for _, id := range []string{"prepared", "installed-a", "installed-b"} {
		if err := store.Create(ctx, models.Plugin{ID: id, Name: "Office " + id, SourceKey: id, UpdatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutVersion(ctx, models.PluginVersion{ID: "v-" + id, PluginID: id, Version: "1", Package: packageplugin.Package{}}); err != nil {
			t.Fatal(err)
		}
	}
	request := &ListPlugins{PageNum: 1, PageSize: 1, InstalledOnly: true}
	value, err := request.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.(ListPluginsResponse); got.TotalSize != 0 || len(got.Resources) != 0 {
		t.Fatal("prepared-only plugins leaked into installed list", got.TotalSize)
	}
	for _, relation := range []models.WakerPlugin{
		{ID: "a1", PluginID: "installed-a", VersionID: "v-installed-a", WakerID: "w1", Enabled: false},
		{ID: "a2", PluginID: "installed-a", VersionID: "v-installed-a", WakerID: "w2", Enabled: true},
		{ID: "b1", PluginID: "installed-b", VersionID: "v-installed-b", WakerID: "w1", Enabled: true},
	} {
		if err := store.Install(ctx, relation); err != nil {
			t.Fatal(err)
		}
	}
	for page, want := range []string{"installed-a", "installed-b"} {
		request.PageNum = page + 1
		value, err = request.Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		got := value.(ListPluginsResponse)
		if got.TotalSize != 2 || len(got.Resources) != 1 || got.Resources[0].Plugin.ID != want {
			t.Fatalf("wrong installed page %d: %+v", page+1, got)
		}
	}
	request.PageNum = 1
	request.Query = "prepared"
	value, err = request.Output(ctx)
	if err != nil || value.(ListPluginsResponse).TotalSize != 0 {
		t.Fatal("search included uninstalled plugin", err)
	}
	if err := store.Uninstall(ctx, "w2", "installed-a"); err != nil {
		t.Fatal(err)
	}
	request.Query = "installed-a"
	value, err = request.Output(ctx)
	if err != nil || value.(ListPluginsResponse).TotalSize != 1 {
		t.Fatal("disabled installation was hidden", err)
	}
	if err := store.Uninstall(ctx, "w1", "installed-a"); err != nil {
		t.Fatal(err)
	}
	value, err = request.Output(ctx)
	if err != nil || value.(ListPluginsResponse).TotalSize != 0 {
		t.Fatal("last uninstall remained in installed list", err)
	}
	// 插件库安装准备仍需关联已登记但尚未安装的资源。
	all, err := (&ListPlugins{}).Output(ctx)
	if err != nil || all.(ListPluginsResponse).TotalSize != 3 {
		t.Fatal("installed filtering changed discovery", err)
	}
}
