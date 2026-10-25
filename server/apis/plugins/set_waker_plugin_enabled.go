package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 启停伙伴插件
type SetWakerPluginEnabled struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 请求体
	Body PluginEnabledRequest `in:"body"`
}

// Path 返回伙伴插件关系操作入口。
func (*SetWakerPluginEnabled) Path() string { return "/:wakerId/plugins/:pluginId" + "/enabled" }

// Output 只改变该伙伴的真实安装关系。
func (o *SetWakerPluginEnabled) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if _, err := visiblePlugin(ctx, o.PluginID, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.PluginControllerManager.SetEnabled(ctx, o.WakerID, o.PluginID, o.Body.Enabled); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
