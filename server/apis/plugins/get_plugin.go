package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/catalog"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 插件详情
type GetPlugin struct {
	ginx.MethodGet
	// 展示语言
	Locale string `in:"query" name:"locale"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回插件详情入口。
func (*GetPlugin) Path() string { return "/:pluginId" }

// Output 返回固定版本的公开组成、真实安装与缓存账号摘要。
func (o *GetPlugin) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := visiblePlugin(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	states, err := global.PluginControllerManager.ReadStates(ctx, []string{resource.ID})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	selected := o.VersionID
	if selected == "" && o.WakerID != "" {
		for _, relation := range states[resource.ID].Installations {
			if relation.WakerID == o.WakerID {
				selected = relation.VersionID
			}
		}
	}
	result, err := detailFromState(ctx, resource, states[resource.ID], selected)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	entries, source := catalog.Snapshot(global.PluginCatalogSource())
	applyPluginUpdate(&result, entries, source)
	// 不受支持的保留版本只提供管理事实，不尝试读取或重解析包内容。
	if result.PackageStatus != constants.PluginPackageStatusAvailable {
		return result, nil
	}
	for index, version := range result.Versions {
		if version.ID != result.ResolvedVersionID {
			continue
		}
		source, release, readErr := acquirePluginPresentation(version)
		if readErr != nil {
			return nil, ginx.WithStack(readErr)
		}
		parsed, readErr := packageplugin.LocalizeReadme(source, version.Package, o.Locale)
		release()
		if readErr != nil {
			return nil, ginx.WithStack(readErr)
		}
		result.Versions[index].Package = parsed
	}
	return result, nil
}
