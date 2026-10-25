package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListIMBindings 获取聊天授权。
type ListIMBindings struct {
	ginx.MethodGet
	// 所属连接过滤
	ConnectionID string `in:"query" name:"connectionId"`
	// 分页窗口
	Offset int `in:"query" name:"offset" validate:"min=0,max=100000"`
}

func (*ListIMBindings) Path() string { return "/bindings" }
func (o *ListIMBindings) Output(ctx *gin.Context) (interface{}, error) {
	values, total, err := global.IMControllerManager.Bindings(ctx, o.ConnectionID, 50, o.Offset)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListIMBindingsResponse{Resources: values, TotalSize: total}, nil
}
