package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建知识连接
type CreateConnection struct {
	ginx.MethodPost
	// 请求体
	Body ConnectionInput `in:"body"`
}

func (o *CreateConnection) Path() string { return "" }
func (o *CreateConnection) Output(ctx *gin.Context) (interface{}, error) {
	if o.Body.ExpectedRevision != 0 {
		return nil, status_error.KnowledgeInvalid
	}
	return save(ctx, "", o.Body)
}
