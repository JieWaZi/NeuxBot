package websession

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"net/http"
)

// 本机配对请求
type PairRequest struct {
	// 配对码
	Code string `json:"code" validate:"required,max=64"`
}

// 配对本机浏览器
type PairSession struct {
	// ginx.MethodPost 声明接口方法。
	ginx.MethodPost
	// 请求体
	Body PairRequest `in:"body"`
}

func (*PairSession) Path() string { return "/session" }
func (p *PairSession) Output(ctx *gin.Context) (interface{}, error) {
	ctx.Header("Cache-Control", "no-store")
	if global.LocalWeb == nil {
		ctx.AbortWithStatus(http.StatusNotFound)
		return nil, nil
	}
	s, status := global.LocalWeb.Pair(ctx.Writer, ctx.Request, p.Body.Code)
	if status != http.StatusOK {
		ctx.AbortWithStatus(status)
		return nil, nil
	}
	return response(s), nil
}
