package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 配置飞书应用
type ConfigurePluginAuthorization struct {
	ginx.MethodPost
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 请求体
	Body ConfigureAuthorizationRequest `in:"body"`
}

// Path 返回官方应用配置入口。
func (*ConfigurePluginAuthorization) Path() string { return "/:pluginId/authorization/configuration" }

// Output 通过官方 CLI 配置应用，密钥只在命令 stdin 中使用。
func (o *ConfigurePluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	auth, extension, err := pluginAuthorizer(ctx, o.PluginID, o.WakerID, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := auth.Configure(ctx, o.Body.AppID, o.Body.AppSecret); err != nil {
		return nil, ginx.WithStack(err)
	}
	publishProviderChange(extension.Provider)
	return ginx.CommonSuccessResponse(), nil
}
