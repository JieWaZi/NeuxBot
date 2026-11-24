package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalmemory "github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/gin-gonic/gin"
)

// 记忆资源
type MemoryResource struct {
	// 标识
	ID string `json:"id"`
	// 记忆正文
	Content string `json:"content"`
	// 核心记忆
	Core bool `json:"core"`
	// 创建来源
	Source constants.MemoryTimelineSource `json:"source"`
	// 执行标识
	SourceRunID string `json:"sourceRunId,omitempty"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
}

// 时间线事件
type TimelineResource struct {
	// 标识
	ID string `json:"id"`
	// 记忆标识
	MemoryID string `json:"memoryId"`
	// 事件类型
	Event constants.MemoryEvent `json:"event"`
	// 变更前正文
	OldContent string `json:"oldContent,omitempty"`
	// 变更后正文
	NewContent string `json:"newContent,omitempty"`
	// 来源
	Source constants.MemoryTimelineSource `json:"source"`
	// 执行标识
	SourceRunID string `json:"sourceRunId,omitempty"`
	// 发生时间
	OccurredAt int64 `json:"occurredAt"`
}

// 记忆状态
type MemoryStatusResource struct {
	// 召回方式
	Mode constants.MemoryRetrievalMode `json:"mode"`
	// 已配置
	Configured bool `json:"configured"`
	// 配置冲突
	Conflict bool `json:"conflict"`
	// 待重建
	NeedsReindex bool `json:"needsReindex"`
	// 模型
	Model string `json:"model,omitempty"`
	// 向量维度
	Dimensions int `json:"dimensions,omitempty"`
	// 记忆总数
	MemoryCount int64 `json:"memoryCount"`
	// 向量总数
	VectorCount int64 `json:"vectorCount"`
}

// 嵌入配置
type EmbeddingSettingsResource struct {
	// 服务地址
	BaseURL string `json:"baseUrl"`
	// 模型
	Model string `json:"model"`
	// 密钥已配置
	APIKeyConfigured bool `json:"apiKeyConfigured"`
}

func memoryResource(item internalmemory.Memory) MemoryResource {
	return MemoryResource{ID: item.ID, Content: item.Data, Core: item.Core, Source: item.Origin, SourceRunID: item.SourceRunID, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func timelineResource(item internalmemory.HistoryEntry) TimelineResource {
	return TimelineResource{ID: item.ID, MemoryID: item.MemoryID, Event: item.Event, OldContent: item.OldMemory, NewContent: item.NewMemory, Source: item.Source, SourceRunID: item.SourceRunID, OccurredAt: item.UpdatedAt}
}

func memoryStatusResource(item internalmemory.Status) MemoryStatusResource {
	return MemoryStatusResource{Mode: item.Mode, Configured: item.Configured, Conflict: item.Conflict, NeedsReindex: item.NeedsReindex, Model: item.Model, Dimensions: item.Dimensions, MemoryCount: item.MemoryCount, VectorCount: item.VectorCount}
}

func embeddingSettingsResource(item internalmemory.EmbeddingSettings) EmbeddingSettingsResource {
	return EmbeddingSettingsResource{BaseURL: item.BaseURL, Model: item.Model, APIKeyConfigured: item.APIKeyConfigured}
}

func partnerMemoryContext(ctx *gin.Context, wakerID string) (internalmemory.Context, error) {
	if global.MemoryService == nil {
		return internalmemory.Context{}, status_error.MemoryServiceUnavailable
	}
	if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
		return internalmemory.Context{}, err
	}
	return internalmemory.Context{PartnerID: wakerID}, nil
}
