package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 核验插件账号
type CheckPluginAccount struct {
	ginx.MethodPost
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 请求体
	Body PluginAccountRequest `in:"body"`
}

// Path 返回显式核验账号入口。
func (*CheckPluginAccount) Path() string { return "/:pluginId/account/check" }

// Output 显式启动固定上下文的核验，并在完成后拒绝配置或账号已经变化的结果。
func (o *CheckPluginAccount) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	_, version, err := validateAccountRequest(request, o.PluginID, o.WakerID, o.VersionID, o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	auth, driver, release, err := installationService().BridgeLease(request, version)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer release()
	if err := checkOperationContext(driver.OperationContext(), o.Body); err != nil {
		return nil, ginx.WithStack(err)
	}
	if _, err := auth.Status(request, true); err != nil {
		return nil, ginx.WithStack(err)
	}
	result, _, err := validateAccountRequest(request, o.PluginID, o.WakerID, o.VersionID, o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
