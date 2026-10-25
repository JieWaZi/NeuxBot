package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建远端知识库
type CreateRemoteLibrary struct {
	ginx.MethodPost
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 请求体
	Body knowledge.CreateLibraryInput `in:"body"`
}

func (o *CreateRemoteLibrary) Path() string { return "/:connectionId/libraries" }
func (o *CreateRemoteLibrary) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, "")
	if err != nil {
		return nil, err
	}
	if c.custom == nil || o.Body.ExecutionDefaults != nil || o.Body.EmbeddingProfile != nil {
		return nil, status_error.KnowledgeUnsupported
	}
	return remoteInvoke(ctx, c, o.Body.RequestID, "library.create", o.Body, func() (*core.Receipt, error) {
		return c.custom.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: o.Body.RequestID, Name: o.Body.Name, Kind: "general", WikiLanguage: o.Body.WikiLanguage, Purpose: o.Body.Purpose, WikiConfig: o.Body.WikiConfig, ChunkingConfig: o.Body.ChunkingConfig, IndexingStrategy: o.Body.IndexingStrategy, EmbeddingModelID: o.Body.EmbeddingModelID})
	})
}
