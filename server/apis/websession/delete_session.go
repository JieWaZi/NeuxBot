package websession

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"net/http"
)

// 退出本机会话
type DeleteSession struct {
	// MethodDelete 声明撤销接口。
	ginx.MethodDelete
}

func (*DeleteSession) Path() string { return "/session" }
func (*DeleteSession) Output(ctx *gin.Context) (interface{}, error) {
	ctx.Header("Cache-Control", "no-store")
	if global.LocalWeb == nil {
		ctx.AbortWithStatus(http.StatusNotFound)
		return nil, nil
	}
	if _, ok := global.LocalWeb.Authenticate(ctx.Request, false); !ok {
		ctx.AbortWithStatus(http.StatusUnauthorized)
		return nil, nil
	}
	global.LocalWeb.Logout(ctx.Writer, ctx.Request)
	return ginx.CommonSuccessResponse(), nil
}
