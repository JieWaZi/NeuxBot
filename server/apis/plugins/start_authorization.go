package plugins

import (
	"regexp"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 发起插件授权
type StartPluginAuthorization struct {
	ginx.MethodPost
	// 请求体
	Body AuthorizationScopeRequest `in:"body"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回共享授权发起入口。
func (*StartPluginAuthorization) Path() string { return "/:pluginId/account/authorizations" }

// Output 创建或恢复唯一设备等待，由 Core 生命周期继续等待官方结果。
func (o *StartPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	request := ctx.Request.Context()
	_, version, err := validateAccountRequest(request, o.PluginID, o.WakerID, o.VersionID, o.Body.PluginAccountRequest)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	declaration := version.Package.Integration.Authentication
	if declaration == nil {
		return nil, ginx.WithStack(status_error.PluginNotReady)
	}
	for _, scope := range o.Body.AdditionalScopes {
		if !slices.Contains(declaration.Scopes, scope) {
			return nil, ginx.WithStack(status_error.PluginForbidden)
		}
	}
	if err := closeIdlePlugin(request, o.PluginID); err != nil {
		return nil, ginx.WithStack(err)
	}
	service := installationService()
	auth, driver, err := service.Bridge(request, version)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	operation := driver.OperationContext()
	if err := checkOperationContext(operation, o.Body.PluginAccountRequest); err != nil {
		return nil, ginx.WithStack(err)
	}
	scopes := auth.RequiredScopes(declaration.Scopes...)
	scopes = append(scopes, o.Body.AdditionalScopes...)
	result, created, err := auth.LoginSelected(request, driver, func() error {
		if _, err := visiblePlugin(request, o.PluginID, o.WakerID); err != nil {
			return err
		}
		return service.ValidateOperation(request, operation)
	}, o.Body.OwnershipNonce, scopes...)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return StartPluginAuthorizationResponse{Session: result, Created: created}, nil
}

// Validate 只允许明确的单项权限标识，禁止空白、超量或命令文本。
func (o *StartPluginAuthorization) Validate(*gin.Context) error {
	if len(o.Body.AdditionalScopes) > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	pattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,199}$`)
	for _, scope := range o.Body.AdditionalScopes {
		if !pattern.MatchString(scope) {
			return ginx.WithStack(status_error.BadRequest)
		}
	}
	return nil
}
