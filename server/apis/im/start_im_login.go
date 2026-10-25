package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// StartIMLogin 开始扫码登录。
type StartIMLogin struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
}

func (*StartIMLogin) Path() string { return "/connections/:connectionId/login" }
func (o *StartIMLogin) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.IMService.BeginLogin(ctx, o.ConnectionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
