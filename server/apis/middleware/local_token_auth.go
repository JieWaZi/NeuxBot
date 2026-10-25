// Package middleware 定义 NeuxBot Core 的 HTTP 中间件。
package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/shrewx/ginx"
)

// LocalTokenAuth 校验桌面进程启动 Core 时协商的本地令牌。
type LocalTokenAuth struct {
	// MiddlewareType 声明该 Operator 是 ginx 中间件。
	ginx.MiddlewareType
}

// Output 返回使用常量时间比较的 Bearer Token 中间件。
func (a *LocalTokenAuth) Output(*gin.Context) (interface{}, error) {
	return gin.HandlerFunc(func(ctx *gin.Context) {
		if ctx.Request.Method == http.MethodPost && strings.HasPrefix(ctx.Request.URL.Path, "/api/v1/workflows/") && strings.HasSuffix(ctx.Request.URL.Path, "/files") {
			ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, constants.WorkflowFileMaximumBytes+(1<<20))
		}
		expected := global.Config.Security.LocalToken
		provided, found := strings.CutPrefix(
			ctx.GetHeader("Authorization"),
			"Bearer ",
		)
		// 仅固定事件升级入口接受浏览器可设置的子协议令牌，普通 REST 仍要求 Bearer。
		if ctx.GetHeader("Authorization") == "" && !found && ctx.Request.Method == http.MethodGet && (ctx.Request.URL.Path == "/api/v1/events" || strings.HasPrefix(ctx.Request.URL.Path, "/api/v1/terminals/") && strings.HasSuffix(ctx.Request.URL.Path, "/stream")) && websocket.IsWebSocketUpgrade(ctx.Request) {
			for _, protocol := range strings.Split(ctx.GetHeader("Sec-WebSocket-Protocol"), ",") {
				if token, ok := strings.CutPrefix(strings.TrimSpace(protocol), "ally.auth."); ok {
					provided = token
					found = true
					break
				}
			}
		}
		if !found && ctx.GetHeader("Authorization") == "" && global.LocalWeb != nil {
			if session, ok := global.LocalWeb.Authenticate(ctx.Request, websocket.IsWebSocketUpgrade(ctx.Request)); ok {
				requestContext, cancel := context.WithCancel(ctx.Request.Context())
				stop := context.AfterFunc(session.Context, cancel)
				defer stop()
				defer cancel()
				ctx.Request = ctx.Request.WithContext(requestContext)
				ctx.Set("ally.auth.subject", "web:"+session.ID)
				ctx.Set("ally.auth.lifetime", session.Context)
				ctx.Next()
				return
			}
		}
		if !found ||
			expected == "" ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "本地认证失败",
			})
			return
		}
		ctx.Set("ally.auth.subject", "desktop")
		ctx.Next()
	}), nil
}

// AuthSubject 返回已认证管理主体，用于资源句柄的跨会话隔离。
func AuthSubject(ctx *gin.Context) string { return ctx.GetString("ally.auth.subject") }

// AuthLifetime 绑定 Web 会话撤销；桌面终端由 Core 总生命周期释放。
func AuthLifetime(ctx *gin.Context) context.Context {
	if value, ok := ctx.Get("ally.auth.lifetime"); ok {
		if lifetime, ok := value.(context.Context); ok {
			return lifetime
		}
	}
	return context.Background()
}
