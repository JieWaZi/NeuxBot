// Package agent 提供与桌面管理认证隔离的执行能力协议入口。
package agent

import (
	"net/http"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Router 只装配当前执行令牌可以访问的固定本机命名空间。
var Router = ginx.NewRouter(ginx.Group("/agent/v1"))

func init() { Router.Register(&Read{}); Router.Register(&Command{}) }

// 读取执行能力契约
type Read struct {
	// MethodGet 声明本接口以 GET 查询执行契约或回执。
	ginx.MethodGet
}

// Path 将读取子路径交给同一个执行协议处理器。
func (*Read) Path() string { return "/*path" }

// Output 使用执行令牌处理查询并直接写入单个协议信封。
func (*Read) Output(ctx *gin.Context) (interface{}, error) { serve(ctx); return nil, nil }

func serve(ctx *gin.Context) {
	if global.AgentCapabilities == nil {
		ctx.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	global.AgentCapabilities.ServeHTTP(ctx.Writer, ctx.Request)
	ctx.Abort()
}
