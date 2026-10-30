package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"regexp"
)

// 发起插件授权
type StartPluginAuthorization struct {
	ginx.MethodPost
	// 请求体
	Body AuthorizationScopeRequest `in:"body"`
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回共享授权发起入口。
func (*StartPluginAuthorization) Path() string { return "/:pluginId/authorization" }

// Output 创建或恢复唯一设备等待，由 Core 生命周期继续等待官方结果。
func (o *StartPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	if err := closeIdlePlugin(ctx, o.PluginID); err != nil {
		return nil, ginx.WithStack(err)
	}
	auth, extension, err := pluginAuthorizer(ctx, o.PluginID, o.WakerID, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	scopes := auth.RequiredScopes(extension.Scopes...)
	scopes = append(scopes, o.Body.AdditionalScopes...)
	result, err := auth.Login(ctx, scopes...)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
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
