package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"

	"github.com/JieWaZi/neux-knowledge/core"
)

// 创建知识库请求
type CreateLibraryInput struct {
	// 独立嵌入服务
	EmbeddingProfile *LibraryEmbeddingInput `json:"embeddingProfile,omitempty"`

	// 执行偏好
	ExecutionDefaults *LibraryExecutionDefaultsInput `json:"executionDefaults,omitempty"`
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 名称
	Name string `json:"name" validate:"required,max=200"`
	// 正文语言
	WikiLanguage string `json:"wikiLanguage" validate:"required"`
	// 用途
	Purpose string `json:"purpose"`
	// 分块设置
	ChunkingConfig *core.ChunkingConfig `json:"chunkingConfig,omitempty"`
	// Wiki 整理配置
	WikiConfig *core.WikiConfig `json:"wikiConfig,omitempty"`
	// 索引策略
	IndexingStrategy *core.IndexingStrategy `json:"indexingStrategy,omitempty"`
	// 嵌入模型标识
	EmbeddingModelID string `json:"embeddingModelId,omitempty"`
}

// 创建知识库
type CreateLibrary struct {
	ginx.MethodPost
	// 请求体
	Body CreateLibraryInput `in:"body"`
}

func (o *CreateLibrary) Path() string { return "/libraries" }
func (o *CreateLibrary) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	embeddingModelID := o.Body.EmbeddingModelID
	if o.Body.EmbeddingProfile != nil {
		_, model, err := controllers.NormalizeLibraryEmbedding(o.Body.EmbeddingProfile.BaseURL, o.Body.EmbeddingProfile.Model)
		if err != nil {
			return nil, err
		}
		if o.Body.EmbeddingProfile.APIKey == "" {
			return nil, status_error.KnowledgeEmbeddingNotConfigured
		}
		if embeddingModelID != "" && embeddingModelID != model {
			return nil, status_error.KnowledgeConflict
		}
		embeddingModelID = model
	} else if o.Body.IndexingStrategy != nil && o.Body.IndexingStrategy.VectorEnabled {
		return nil, status_error.KnowledgeEmbeddingNotConfigured
	}
	var defaults models.LibraryExecutionDefaults
	if o.Body.ExecutionDefaults != nil {
		// 回执已提交时不重新解析后来变化的智能体/账号默认，保持原请求可确定重放。
		replayed := false
		if old, lookupErr := global.KnowledgeEngine.GetReceipt(ctx, o.Body.RequestID); lookupErr == nil {
			requestID := (requestidempotency.Identity{ID: o.Body.RequestID}).ResourceID("knowledge-execution-defaults", old.LibraryID)
			var replayErr error
			defaults, replayed, replayErr = global.KnowledgeControllerManager.ReplayLibraryExecutionDefaults(ctx, requestID, 0, libraryExecutionDefaultsValues(old.LibraryID, *o.Body.ExecutionDefaults))
			if replayErr != nil {
				return nil, replayErr
			}
		}
		if !replayed {
			var err error
			defaults, err = resolveLibraryExecutionDefaults(ctx, "", *o.Body.ExecutionDefaults)
			if err != nil {
				return nil, err
			}
		}
	}
	activityContext := bridge.LocalUserActivity(ctx)
	r, err := executeWrite(activityContext, o.Body.RequestID, "library.create", "", o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.CreateLibrary(activityContext, core.CreateLibraryInput{RequestID: o.Body.RequestID, Name: o.Body.Name, Kind: "general", WikiLanguage: o.Body.WikiLanguage, Purpose: o.Body.Purpose, WikiConfig: o.Body.WikiConfig, ChunkingConfig: o.Body.ChunkingConfig, IndexingStrategy: o.Body.IndexingStrategy, EmbeddingModelID: embeddingModelID})
	})
	if err != nil {
		return nil, err
	}
	if o.Body.EmbeddingProfile != nil {
		profile := o.Body.EmbeddingProfile
		requestID := (requestidempotency.Identity{ID: o.Body.RequestID}).ResourceID("knowledge-embedding-profile", r.LibraryID)
		if _, err = embeddingProfiles().SaveCreated(activityContext, r.LibraryID, requestID, profile.BaseURL, embeddingModelID, profile.APIKey); err != nil {
			return nil, err
		}
	}
	if o.Body.ExecutionDefaults != nil {
		defaults.LibraryID = r.LibraryID
		requestID := (requestidempotency.Identity{ID: o.Body.RequestID}).ResourceID("knowledge-execution-defaults", r.LibraryID)
		if _, err = global.KnowledgeControllerManager.UpdateLibraryExecutionDefaults(ctx, requestID, 0, defaults); err != nil {
			return nil, err
		}
		if err = global.KnowledgeEngine.RecordLibraryExecutionDefaultsActivity(activityContext, r.LibraryID, requestID); err != nil {
			return nil, bridge.MapError(err)
		}
	}
	return receiptDTO(r), nil
}
