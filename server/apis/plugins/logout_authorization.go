package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 退出共享账号
type LogoutPluginAuthorization struct {
	ginx.MethodDelete
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回官方共享身份退出入口。
func (*LogoutPluginAuthorization) Path() string { return "/:pluginId/account" }

// Output 在无执行占用时通过官方 CLI 退出共享身份。
func (o *LogoutPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	auth, extension, err := pluginAuthorizer(ctx, o.PluginID, o.WakerID, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := auth.Logout(ctx); err != nil {
		return nil, ginx.WithStack(err)
	}
	publishProviderChange(extension.Provider)
	return ginx.CommonSuccessResponse(), nil
}
