package plugins

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取插件授权会话
type GetPluginAuthorizationSession struct {
	ginx.MethodGet
	// 插件标识
	PluginID string `in:"path" name:"pluginId" validate:"required"`
	// 会话标识
	SessionID string `in:"path" name:"sessionId" validate:"required"`
	// 所选版本
	VersionID string `in:"query" name:"versionId" validate:"required"`
	// 所属伙伴
	WakerID string `in:"query" name:"wakerId"`
}

// Path 返回精确会话的缓存读取入口。
func (*GetPluginAuthorizationSession) Path() string {
	return "/:pluginId/account/authorizations/:sessionId"
}

// Output 不创建会话或驱动，拒绝其他版本和已被替换的会话。
func (o *GetPluginAuthorizationSession) Output(ctx *gin.Context) (interface{}, error) {
	result, _, err := cachedAccount(ctx, o.PluginID, o.WakerID, o.VersionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if result.Session.ID != o.SessionID {
		return nil, ginx.WithStack(status_error.PluginNotFound)
	}
	return result.Session, nil
}
