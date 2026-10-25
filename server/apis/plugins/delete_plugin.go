package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除插件
type DeletePlugin struct {
	ginx.MethodDelete
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 请求体
	Body DeletePluginRequest `in:"body"`
}

// Path 返回资源整体删除入口。
func (*DeletePlugin) Path() string { return "/:pluginId" }

// Output 确认实际影响集合后删除元数据，运行内容留至安全回收。
func (o *DeletePlugin) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := visiblePlugin(ctx, o.PluginID, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if !o.Body.Confirmed {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	versions, err := global.PluginControllerManager.Versions(ctx, o.PluginID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	validate := func(txctx context.Context) error {
		relations, err := global.PluginControllerManager.Installations(txctx, o.PluginID, "")
		if err != nil {
			return err
		}
		expected := map[string]string{}
		for _, item := range o.Body.Installations {
			expected[item.WakerID] = item.ExpectedVersionID
		}
		if len(expected) != len(relations) {
			return status_error.PluginVersionConflict
		}
		for _, r := range relations {
			if expected[r.WakerID] != r.VersionID {
				return status_error.PluginVersionConflict
			}
		}
		return nil
	}
	err = removePluginReferences(ctx, o.PluginID, "", versions, validate, func(txctx context.Context) error {
		return global.PluginControllerManager.Delete(txctx, o.PluginID)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
