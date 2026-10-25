package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查看插件原文
type GetPluginReadme struct {
	ginx.MethodGet
	// 展示语言
	Locale string `in:"query" name:"locale"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 版本标识
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回固定版本原文入口。
func (*GetPluginReadme) Path() string { return "/:pluginId/readme" }

// Output 复验完整快照后返回原文，正文不执行HTML或脚本。
func (o *GetPluginReadme) Output(ctx *gin.Context) (interface{}, error) {
	resource, err := visiblePlugin(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	version, err := global.PluginControllerManager.Version(ctx, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if version.PluginID != resource.ID {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	source, release, err := acquirePluginReadme(version)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	content, err := packageplugin.ReadRawReadme(source, version.Package, o.Locale)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return PluginReadmeResponse{Content: content}, nil
}

// 插件原文响应
type PluginReadmeResponse struct {
	// 完整原文
	Content string `json:"content"`
}
