package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListIMConnections 获取 IM 连接。
type ListIMConnections struct {
	ginx.MethodGet
}

func (*ListIMConnections) Path() string { return "/connections" }
func (o *ListIMConnections) Output(ctx *gin.Context) (interface{}, error) {
	values, err := global.IMControllerManager.Connections(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListIMConnectionsResponse{Resources: values}, nil
}
