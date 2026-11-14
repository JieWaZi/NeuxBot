package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 退出共享账号
type LogoutPluginAuthorization struct {
	ginx.MethodPost
	// 请求体
	Body PluginAccountRequest `in:"body"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回官方共享身份退出入口。
func (*LogoutPluginAuthorization) Path() string { return "/:pluginId/account/logout" }

// Output 在无执行占用时通过官方 CLI 退出共享身份。
func (o *LogoutPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	_, version, err := validateAccountRequest(request, o.PluginID, o.WakerID, o.VersionID, o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := closeIdlePlugin(request, o.PluginID); err != nil {
		return nil, ginx.WithStack(err)
	}
	guard, driver, err := installationService().BridgeChange(request, version)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer guard.Close()
	if err := checkOperationContext(driver.OperationContext(), o.Body); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := guard.Logout(request); err != nil {
		return nil, ginx.WithStack(err)
	}
	publishProviderChange("plugin:" + o.PluginID)
	return ginx.CommonSuccessResponse(), nil
}
