package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	imservice "github.com/JieWaZi/NeuxBot/server/services/im"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// CreateIMConnection 创建 IM 连接。
type CreateIMConnection struct {
	ginx.MethodPost
	// 连接配置
	Body imservice.ConnectionConfiguration `in:"body"`
}

func (*CreateIMConnection) Path() string { return "/connections" }
func (o *CreateIMConnection) Output(ctx *gin.Context) (interface{}, error) {
	value, err := global.IMService.SaveConfiguration(ctx, "", o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return value, nil
}
