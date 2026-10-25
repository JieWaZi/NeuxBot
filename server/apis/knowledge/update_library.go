package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"

	"github.com/JieWaZi/neux-knowledge/core"
)

// 修改知识库请求
type UpdateLibraryInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"required,min=1"`
	// 名称
	Name string `json:"name" validate:"required"`
	// 用途
	Purpose string `json:"purpose"`
	// 已归档
	Archived bool `json:"archived"`
	// 权限版本
	PolicyVersion int64 `json:"policyVersion" validate:"min=1"`
	// 分块设置
	ChunkingConfig *core.ChunkingConfig `json:"chunkingConfig,omitempty"`
	// Wiki 整理配置
	WikiConfig *core.WikiConfig `json:"wikiConfig,omitempty"`
	// 索引策略
	IndexingStrategy *core.IndexingStrategy `json:"indexingStrategy,omitempty"`
	// 嵌入模型标识
	EmbeddingModelID *string `json:"embeddingModelId,omitempty"`
}

// 修改知识库
type UpdateLibrary struct {
	ginx.MethodPut
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body UpdateLibraryInput `in:"body"`
}

func (o *UpdateLibrary) Path() string { return "/libraries/:libraryId" }
func (o *UpdateLibrary) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	activityContext := bridge.LocalUserActivity(ctx)
	r, err := executeWrite(activityContext, o.Body.RequestID, "library.update", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.UpdateLibrary(activityContext, core.UpdateLibraryInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, Name: o.Body.Name, Purpose: o.Body.Purpose, Archived: o.Body.Archived, PolicyVersion: o.Body.PolicyVersion, WikiConfig: o.Body.WikiConfig, ChunkingConfig: o.Body.ChunkingConfig, IndexingStrategy: o.Body.IndexingStrategy, EmbeddingModelID: o.Body.EmbeddingModelID})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}
