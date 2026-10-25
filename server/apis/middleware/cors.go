package middleware

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"net/http"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/gin-gonic/gin"
)

// CORS 返回桌面 Renderer 跨源访问本地 Core 所需的响应头中间件。
func CORS() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if global.LocalWeb != nil {
			if !strings.HasPrefix(ctx.Request.URL.Path, "/api/") && ctx.Request.URL.Path != "/api" {
				global.LocalWeb.ServeStatic(ctx.Writer, ctx.Request)
				ctx.Abort()
				return
			}
			if strings.HasPrefix(ctx.Request.URL.Path, "/api/web/") {
				if !global.LocalWeb.TrustedHost(ctx.Request) {
					ctx.AbortWithStatus(http.StatusForbidden)
					return
				}
				ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4096)
			}
			// Cookie 请求不提供跨域授权；桌面 Bearer 仍使用原独立认证。
			if ctx.GetHeader("Authorization") == "" && !(ctx.Request.Method == http.MethodOptions && strings.Contains(strings.ToLower(ctx.GetHeader("Access-Control-Request-Headers")), "authorization")) {
				ctx.Next()
				return
			}
		}
		ctx.Header("Access-Control-Allow-Origin", "*")

		ctx.Header("Access-Control-Allow-Headers", constants.LocalCORSAllowedHeaders)
		ctx.Header("Access-Control-Allow-Methods", constants.LocalCORSAllowedMethods)

		if ctx.Request.Method == http.MethodOptions {
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}

		ctx.Next()
	}
}
