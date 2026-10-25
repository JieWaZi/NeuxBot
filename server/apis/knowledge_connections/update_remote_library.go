package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 修改远端知识库
type UpdateRemoteLibrary struct {
	ginx.MethodPut
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body knowledge.UpdateLibraryInput `in:"body"`
}

func (o *UpdateRemoteLibrary) Path() string { return "/:connectionId/libraries/:libraryId" }
func (o *UpdateRemoteLibrary) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "library.update", o.Body, func() (*core.Receipt, error) {
		return c.custom.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, Name: o.Body.Name, Purpose: o.Body.Purpose, Archived: o.Body.Archived, PolicyVersion: o.Body.PolicyVersion, WikiConfig: o.Body.WikiConfig, ChunkingConfig: o.Body.ChunkingConfig, IndexingStrategy: o.Body.IndexingStrategy, EmbeddingModelID: o.Body.EmbeddingModelID})
	})
}
