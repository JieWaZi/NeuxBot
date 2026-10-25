package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	imservice "github.com/JieWaZi/NeuxBot/server/services/im"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// UpdateIMConnection 更新 IM 连接。
type UpdateIMConnection struct {
	ginx.MethodPut
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 连接配置
	Body imservice.ConnectionConfiguration `in:"body"`
}

func (*UpdateIMConnection) Path() string { return "/connections/:connectionId" }
func (o *UpdateIMConnection) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.IMService.SaveConfiguration(ctx, o.ConnectionID, o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
