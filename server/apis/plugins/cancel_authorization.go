package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消插件授权
type CancelPluginAuthorization struct {
	ginx.MethodDelete
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 等待标识
	SessionID string `in:"path" name:"sessionId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回共享设备等待取消入口。
func (*CancelPluginAuthorization) Path() string { return "/:pluginId/authorization/:sessionId" }

// Output 取消等待并等待旧进程退出，不撤销已完成登录。
func (o *CancelPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	auth, _, err := pluginAuthorizer(ctx, o.PluginID, o.WakerID, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := auth.CancelAndWait(ctx, o.SessionID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
