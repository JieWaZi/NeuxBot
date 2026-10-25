package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取文档分块
type GetSourceChunks struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 来源版本
	SourceID string `in:"path" name:"sourceId" validate:"required"`
}

func (o *GetSourceChunks) Path() string { return "/libraries/:libraryId/sources/:sourceId/chunks" }

// Output 只读实际解析产物；未解析原件返回空列表，不在 HTTP 请求中重新解析或运行模型。
func (o *GetSourceChunks) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	rows, err := global.KnowledgeEngine.ListDocumentChunks(ctx, o.LibraryID, o.SourceID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return rows, nil
}

func init() { Router.Register(&GetSourceChunks{}) }
