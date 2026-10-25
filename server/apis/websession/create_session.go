package websession

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"net/http"
)

// 建立本机会话
type CreateSession struct {
	// MethodPost 声明建立会话接口。
	ginx.MethodPost
}

func (*CreateSession) Path() string { return "/session" }
func (*CreateSession) Output(ctx *gin.Context) (interface{}, error) {
	ctx.Header("Cache-Control", "no-store")
	if global.LocalWeb == nil {
		ctx.AbortWithStatus(http.StatusNotFound)
		return nil, nil
	}
	s, status := global.LocalWeb.CreateSession(ctx.Writer, ctx.Request)
	if status != http.StatusOK {
		ctx.AbortWithStatus(status)
		return nil, nil
	}
	return response(s), nil
}
