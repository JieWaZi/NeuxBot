package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取远端操作引用
type GetRemoteOperation struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求标识
	RequestID string `in:"path" name:"requestId" validate:"required"`
}

func (o *GetRemoteOperation) Path() string {
	return "/:connectionId/libraries/:libraryId/operations/:requestId"
}
func (o *GetRemoteOperation) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeControllerManager == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, err := global.KnowledgeControllerManager.GetOperation(ctx, o.RequestID)
	if err != nil {
		return nil, err
	}
	if v.Backend == "local" || v.ConnectionID != o.ConnectionID || v.KnowledgeBaseID != o.LibraryID {
		return nil, status_error.KnowledgeNotFound
	}
	return operationDTO(v), nil
}
