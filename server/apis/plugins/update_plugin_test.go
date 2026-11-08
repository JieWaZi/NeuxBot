package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"slices"
	"testing"
)

func TestUpdatePluginSavesUserCategoriesWithoutChangingVersions(t *testing.T) {
	ctx := pluginAPIContext(t)
	if err := global.PluginControllerManager.Create(ctx, models.Plugin{ID: "tagged", Name: "Plugin", SourceKey: "tagged-source"}); err != nil {
		t.Fatal(err)
	}
	categories := []string{"  开发  ", "写作", "开发"}
	if _, err := (&UpdatePlugin{PluginID: "tagged", Body: UpdatePluginRequest{Name: "Plugin", Categories: &categories}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := global.PluginControllerManager.Get(ctx, "tagged")
	if err != nil || !slices.Equal(stored.Categories, []string{"开发", "写作"}) {
		t.Fatalf("plugin tags=%+v error=%v", stored, err)
	}
	// 名称单独保存不丢失分类，清空标签不删除词库。
	if _, err := (&UpdatePlugin{PluginID: "tagged", Body: UpdatePluginRequest{Name: "Renamed"}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err = global.PluginControllerManager.Get(ctx, "tagged")
	if err != nil || len(stored.Categories) != 2 {
		t.Fatalf("rename lost tags: %+v %v", stored, err)
	}
	empty := []string{}
	if _, err := (&UpdatePlugin{PluginID: "tagged", Body: UpdatePluginRequest{Name: "Renamed", Categories: &empty}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err = global.PluginControllerManager.Get(ctx, "tagged")
	if err != nil || len(stored.Categories) != 0 {
		t.Fatalf("clear failed: %+v %v", stored, err)
	}
	options, err := global.ResourceCategoryControllerManager.List(ctx, constants.ResourceCategoryTypePlugin)
	if err != nil || len(options) != 2 {
		t.Fatalf("dictionary lost: %+v %v", options, err)
	}
}
