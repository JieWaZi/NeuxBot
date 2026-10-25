package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取知识连接
type ListConnections struct{ ginx.MethodGet }

// 连接列表
type ConnectionList struct {
	// 连接配置
	Items []Connection `json:"items"`
}

func (o *ListConnections) Path() string { return "" }
func (o *ListConnections) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeConnectionControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	rows, err := global.KnowledgeConnectionControllerManager.List(ctx)
	if err != nil {
		return nil, err
	}
	items := []Connection{}
	for _, v := range rows {
		items = append(items, connectionDTO(v))
	}
	return ConnectionList{Items: items}, nil
}
