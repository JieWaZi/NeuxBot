package memory

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/embedding"
)

// Embedder 是记忆嵌入端口，对齐 记忆嵌入调用契约。
type Embedder interface {
	// Embed 返回单条文本的嵌入向量。
	Embed(ctx context.Context, text string, action constants.MemoryEmbeddingAction) ([]float32, error)
	// EmbedBatch 返回多条文本的嵌入向量，与输入一一对应。
	EmbedBatch(ctx context.Context, texts []string, action constants.MemoryEmbeddingAction) ([][]float32, error)
}

// FrozenEmbedder 复用已保存的嵌入配置，为独立索引提供固定的模型与向量空间身份。
type FrozenEmbedder struct {
	// ModelID 是接纳时核对的嵌入模型名称。
	ModelID string
	// Fingerprint 标识基础地址与模型组合，不包含访问凭据。
	Fingerprint string
	// Embed 使用冻结配置批量嵌入文本，不读取或修改记忆内容及其索引。
	Embed func(context.Context, []string) ([][]float32, error)
}

// EmbeddingConfig 沿用通用客户端配置；记忆持久设置仍由记忆模块独占。
type EmbeddingConfig = embedding.Config

// openAIEmbedder 仅适配记忆调用与错误码，不向其他模块暴露记忆设置。
type openAIEmbedder struct {
	// client 是独立实例的通用嵌入客户端。
	client *embedding.Client
}

// NewOpenAIEmbedder 创建直接返回记忆领域稳定错误的嵌入适配器。
func NewOpenAIEmbedder(config EmbeddingConfig) Embedder {
	return &openAIEmbedder{client: embedding.NewOpenAIClient(config, embedding.ErrorSet{
		RequestFailed:   status_error.MemoryEmbeddingRequestFailed,
		Unavailable:     status_error.MemoryEmbeddingUnavailable,
		ResponseInvalid: status_error.MemoryEmbeddingResponseInvalid,
	})}
}

// Embed 返回单条文本的向量，保留批量客户端的原始稳定错误。
func (e *openAIEmbedder) Embed(ctx context.Context, text string, action constants.MemoryEmbeddingAction) ([]float32, error) {
	vectors, err := e.EmbedBatch(ctx, []string{text}, action)
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, status_error.MemoryEmbeddingResponseInvalid
	}
	return vectors[0], nil
}

// EmbedBatch 复用通用HTTP调用，不在记忆适配层重复转换错误。
func (e *openAIEmbedder) EmbedBatch(ctx context.Context, texts []string, _ constants.MemoryEmbeddingAction) ([][]float32, error) {
	return e.client.EmbedBatch(ctx, texts)
}
