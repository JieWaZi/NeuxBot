package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrorSet 由调用领域提供稳定错误，通用客户端在首次失败边界直接返回。
type ErrorSet struct {
	// RequestFailed 表示请求无法构造，不含服务凭据或原始响应。
	RequestFailed status_error.StatusError
	// Unavailable 表示端点连接或服务状态失败。
	Unavailable status_error.StatusError
	// ResponseInvalid 表示返回数量或向量结构不合法。
	ResponseInvalid status_error.StatusError
}

// Config 是 OpenAI 兼容嵌入端点的配置。
type Config struct {
	// BaseURL 是 OpenAI 兼容服务的基础地址，例如 https://api.openai.com/v1。
	BaseURL string
	// APIKey 是访问嵌入服务的凭据。
	APIKey string
	// Model 是嵌入模型名。
	Model string
}

// Complete 返回三项配置是否齐全；齐全才启用语义检索。
func (c Config) Complete() bool {
	return strings.TrimSpace(c.BaseURL) != "" &&
		c.APIKey != "" &&
		strings.TrimSpace(c.Model) != ""
}

// embeddingRequest 是 OpenAI 兼容 /embeddings 的请求体。
type embeddingRequest struct {
	// Model 是嵌入模型名。
	Model string `json:"model"`
	// Input 是待嵌入文本列表。
	Input []string `json:"input"`
	// EncodingFormat 要求以浮点数组返回向量。
	EncodingFormat string `json:"encoding_format"`
}

// embeddingResponse 是 OpenAI 兼容 /embeddings 的响应体。
type embeddingResponse struct {
	// Data 是按输入顺序返回的嵌入结果。
	Data []struct {
		// Index 是服务声明的原始输入位置；全部省略时沿用响应顺序。
		Index *int `json:"index"`
		// Embedding 是浮点向量。
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	// Error 是服务返回的错误对象。
	Error *struct {
		// Message 是服务错误描述。
		Message string `json:"message"`
	} `json:"error"`
}

// Client 是 OpenAI 兼容端点的 Embedder 实现。
type Client struct {
	// config 是当前嵌入端点配置。
	config Config
	// client 是带超时的 HTTP 客户端。
	client *http.Client
	// failures 是调用领域明确选择的稳定错误，不在上层重复映射。
	failures ErrorSet
}

// embeddingHTTPTimeout 是单次嵌入调用的最大耗时。
const embeddingHTTPTimeout = 30 * time.Second

// NewOpenAIClient 创建 OpenAI 兼容端点的 Embedder。
func NewOpenAIClient(config Config, failures ErrorSet) *Client {
	return &Client{
		config:   config,
		client:   &http.Client{Timeout: embeddingHTTPTimeout},
		failures: failures,
	}
}

// EmbedBatch 一次请求嵌入多条文本，失败不做逐条降级重试，由上层决定整体失败语义。
func (e *Client) EmbedBatch(
	ctx context.Context,
	texts []string,
) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	body, err := json.Marshal(embeddingRequest{
		Model:          e.config.Model,
		Input:          texts,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, e.failures.RequestFailed
	}
	endpoint := strings.TrimRight(strings.TrimSpace(e.config.BaseURL), "/") + "/embeddings"
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, e.failures.RequestFailed
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+e.config.APIKey)

	response, err := e.client.Do(request)
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return nil, status_error.OperationCanceled
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return nil, status_error.OperationTimeout
		default:
			return nil, e.failures.Unavailable
		}
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, e.failures.ResponseInvalid
	}
	var decoded embeddingResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, e.failures.ResponseInvalid
	}
	if response.StatusCode != http.StatusOK {
		return nil, e.failures.Unavailable
	}
	if len(decoded.Data) != len(texts) {
		return nil, e.failures.ResponseInvalid
	}
	vectors := make([][]float32, len(decoded.Data))
	indexed := decoded.Data[0].Index != nil
	for position, item := range decoded.Data {
		if len(item.Embedding) == 0 {
			return nil, e.failures.ResponseInvalid
		}
		if (item.Index != nil) != indexed {
			return nil, e.failures.ResponseInvalid
		}
		if indexed {
			position = *item.Index
		}
		// 原件分块与向量必须一一对应，不能按服务返回的数组顺序误配证据。
		if position < 0 || position >= len(vectors) || vectors[position] != nil {
			return nil, e.failures.ResponseInvalid
		}
		vectors[position] = item.Embedding
	}
	return vectors, nil
}
