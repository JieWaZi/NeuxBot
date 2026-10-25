package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新知识连接
type UpdateConnection struct {
	ginx.MethodPut
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 请求体
	Body ConnectionInput `in:"body"`
}

func (o *UpdateConnection) Path() string { return "/:connectionId" }
func (o *UpdateConnection) Output(ctx *gin.Context) (interface{}, error) {
	if o.Body.ExpectedRevision < 1 {
		return nil, status_error.KnowledgeInvalid
	}
	return save(ctx, o.ConnectionID, o.Body)
}
