package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalmemory "github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取嵌入配置
type GetMemorySettings struct {
	ginx.MethodGet
}

func (g *GetMemorySettings) Path() string { return "/settings" }

// Output 返回嵌入配置。
func (g *GetMemorySettings) Output(*gin.Context) (interface{}, error) {
	settings, err := global.MemoryService.GetEmbeddingSettings()
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return embeddingSettingsResource(settings), nil
}

// 更新嵌入配置
type UpdateMemorySettings struct {
	ginx.MethodPut
	// 请求体
	Body UpdateMemorySettingsRequest `in:"body"`
}

// 嵌入配置请求
type UpdateMemorySettingsRequest struct {
	// 服务地址
	BaseURL string `json:"baseUrl"`
	// 模型
	Model string `json:"model"`
	// API 密钥
	APIKey string `json:"apiKey,omitempty"`
	// 清除密钥
	ClearAPIKey bool `json:"clearApiKey,omitempty"`
}

func (u *UpdateMemorySettings) Path() string { return "/settings" }

// Validate 校验 API Key 更新语义。
func (u *UpdateMemorySettings) Validate(*gin.Context) error {
	if u.Body.ClearAPIKey && u.Body.APIKey != "" {
		return ginx.WithStack(status_error.BadRequest)
	}
	return nil
}

// Output 保存嵌入配置。
func (u *UpdateMemorySettings) Output(ctx *gin.Context) (interface{}, error) {
	_, err := global.MemoryService.UpdateEmbeddingSettings(ctx, internalmemory.EmbeddingSettingsUpdate{BaseURL: u.Body.BaseURL, Model: u.Body.Model, APIKey: u.Body.APIKey, ClearAPIKey: u.Body.ClearAPIKey})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 测试嵌入
type TestMemoryEmbedding struct {
	ginx.MethodPost
}

func (t *TestMemoryEmbedding) Path() string { return "/settings/test" }

// Output 探测嵌入服务。
func (t *TestMemoryEmbedding) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.MemoryService.ProbeEmbedding(ctx); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
