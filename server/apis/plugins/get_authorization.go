package plugins

import (
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 插件授权状态
type GetPluginAuthorization struct {
	ginx.MethodGet
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
	// 在线验证
	Verify bool `in:"query" name:"verify"`
}

// Path 返回共享授权查询入口。
func (*GetPluginAuthorization) Path() string { return "/:pluginId/authorization" }

// Output 读取官方 CLI 身份，不复制凭据。
func (o *GetPluginAuthorization) Output(ctx *gin.Context) (interface{}, error) {
	auth, extension, err := pluginAuthorizer(ctx, o.PluginID, o.WakerID, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	account, err := auth.Status(ctx, o.Verify)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	required := auth.RequiredScopes(extension.Scopes...)
	missing := []string{}
	for _, scope := range required {
		if !auth.ScopeGranted(account.Scopes, scope) {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		account.Available = false
	}
	return PluginAuthorizationResponse{Provider: extension.Provider, RequiredScopes: required, MissingScopes: missing, ActiveUses: auth.Usages(), Account: account, Session: auth.Snapshot()}, nil
}
