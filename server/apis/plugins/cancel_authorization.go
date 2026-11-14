package plugins

import (
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消插件授权
type CancelPluginAuthorization struct {
	ginx.MethodDelete
	// 请求体
	Body CancelPluginAuthorizationRequest `in:"body"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 等待标识
	SessionID string `in:"path" name:"sessionId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回共享设备等待取消入口。
func (*CancelPluginAuthorization) Path() string {
	return "/:pluginId/account/authorizations/:sessionId"
}

// Output 取消等待并等待旧进程退出，不撤销已完成登录。
func (o *CancelPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	if _, err := visiblePlugin(request, o.PluginID, o.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	version, err := global.PluginControllerManager.Version(request, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if version.PluginID != o.PluginID {
		return nil, ginx.WithStack(status_error.PluginVersionConflict)
	}
	if err := packageplugin.ValidateContract(version.Package); err != nil {
		return nil, ginx.WithStack(err)
	}
	if global.PluginBridges == nil {
		return nil, ginx.WithStack(status_error.PluginNotFound)
	}
	err = global.PluginBridges.CancelAuthorization(request, o.PluginID, version.Digest+":"+filepath.ToSlash(version.Package.SourcePath), o.SessionID, commandbridge.OperationContext{PluginID: o.PluginID, VersionID: o.VersionID, Digest: version.Digest, ConfigurationRevision: o.Body.ExpectedConfigurationRevision, AccountGeneration: o.Body.ExpectedAccountGeneration}, o.Body.OwnershipNonce)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
