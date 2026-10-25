package plugins

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 卸载伙伴插件
type UninstallWakerPlugin struct {
	ginx.MethodDelete
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
}

// Path 返回伙伴插件关系操作入口。
func (*UninstallWakerPlugin) Path() string { return "/:wakerId/plugins/:pluginId" }

// Output 移除当前伙伴关系，最后提供者引用同时清理本机共享授权。
func (o *UninstallWakerPlugin) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if _, err := visiblePlugin(ctx, o.PluginID, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	store := global.PluginControllerManager
	relations, err := store.Installations(ctx, o.PluginID, o.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if len(relations) == 0 {
		return ginx.CommonSuccessResponse(), nil
	}
	version, err := store.Version(ctx, relations[0].VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	validate := func(txctx context.Context) error {
		current, err := store.Installations(txctx, o.PluginID, o.WakerID)
		if err != nil {
			return err
		}
		if len(current) != 1 || current[0].VersionID != version.ID {
			return status_error.PluginVersionConflict
		}
		return nil
	}
	if err := removePluginReferences(ctx, o.PluginID, o.WakerID, []models.PluginVersion{version}, validate, func(txctx context.Context) error {
		return store.Uninstall(txctx, o.WakerID, o.PluginID)
	}); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
