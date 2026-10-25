package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// LibraryEmbeddingInput 是此库独立服务草稿，密钥只输入不回显。
type LibraryEmbeddingInput struct {
	// 服务地址
	BaseURL string `json:"baseUrl" validate:"required"`
	// 模型名称
	Model string `json:"model" validate:"required"`
	// 访问密钥
	APIKey string `json:"apiKey,omitempty"`
}

// GetLibraryEmbeddingProfile 获取知识库独立嵌入配置。
type GetLibraryEmbeddingProfile struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
}

func (o *GetLibraryEmbeddingProfile) Path() string { return "/libraries/:libraryId/embedding" }
func (o *GetLibraryEmbeddingProfile) Output(ctx *gin.Context) (interface{}, error) {
	return embeddingProfiles().Get(ctx, o.LibraryID)
}

// UpdateLibraryEmbeddingProfileInput 保存配置请求。
type UpdateLibraryEmbeddingProfileInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 配置版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=0"`
	// 知识库版本
	ExpectedLibraryVersion int64 `json:"expectedLibraryVersion" validate:"required,min=1"`
	// 独立服务
	LibraryEmbeddingInput
}

// UpdateLibraryEmbeddingProfile 保存知识库独立嵌入配置。
type UpdateLibraryEmbeddingProfile struct {
	ginx.MethodPut
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body UpdateLibraryEmbeddingProfileInput `in:"body"`
}

func (o *UpdateLibraryEmbeddingProfile) Path() string { return "/libraries/:libraryId/embedding" }
func (o *UpdateLibraryEmbeddingProfile) Output(ctx *gin.Context) (interface{}, error) {
	saved, err := embeddingProfiles().Save(bridge.LocalUserActivity(ctx), bridge.SaveLibraryEmbeddingInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion, ExpectedLibraryVersion: o.Body.ExpectedLibraryVersion, BaseURL: o.Body.BaseURL, Model: o.Body.Model, APIKey: o.Body.APIKey})
	if err != nil {
		return nil, err
	}
	if global.EventHub != nil {
		_ = events.Emit(global.EventHub, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: o.LibraryID, ParentID: "local"}, map[string]any{"ref": bridge.LocalRef(o.LibraryID), "requestId": o.Body.RequestID})
	}
	return saved, nil
}

// TestLibraryEmbeddingInput 仅探测草稿，库身份只用于读取本库已有密钥。
type TestLibraryEmbeddingInput struct {
	// 知识库标识
	LibraryID string `json:"libraryId,omitempty"`
	// 独立服务
	LibraryEmbeddingInput
}

// LibraryEmbeddingTestResult 是真实探测返回的维度，不代表配置已保存或资料已索引。
type LibraryEmbeddingTestResult struct {
	// 向量维度
	Dimensions int `json:"dimensions"`
}

// TestLibraryEmbedding 测试知识库独立嵌入服务。
type TestLibraryEmbedding struct {
	ginx.MethodPost
	// 请求体
	Body TestLibraryEmbeddingInput `in:"body"`
}

func (o *TestLibraryEmbedding) Path() string { return "/embedding/test" }

// Output 探测此库嵌入服务，保留调用取消与超时的稳定响应。
// @err[OperationCanceled][40800000001][操作已取消]
// @err[OperationTimeout][40800000002][操作超时]
func (o *TestLibraryEmbedding) Output(ctx *gin.Context) (interface{}, error) {
	dimensions, err := embeddingProfiles().Test(ctx, o.Body.LibraryID, o.Body.BaseURL, o.Body.Model, o.Body.APIKey)
	if err != nil {
		return nil, err
	}
	return LibraryEmbeddingTestResult{Dimensions: dimensions}, nil
}
func embeddingProfiles() bridge.LibraryEmbeddings {
	return bridge.LibraryEmbeddings{Engine: global.KnowledgeEngine, References: global.KnowledgeControllerManager}
}
