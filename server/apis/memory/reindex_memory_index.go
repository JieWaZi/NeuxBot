// Package memory 提供内置记忆能力的数据管理接口。
package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 重建索引
type ReindexMemoryIndex struct {
	ginx.MethodPost
}

// Path 返回重建语义索引接口路径。
func (r *ReindexMemoryIndex) Path() string { return "/reindex" }

// 重建索引响应
type ReindexMemoryIndexResponse struct {
	// 向量总数
	VectorCount int64 `json:"vectorCount"`
	// 模型
	Model string `json:"model"`
	// 向量维度
	Dimensions int `json:"dimensions"`
}

func (r *ReindexMemoryIndex) Output(ctx *gin.Context) (interface{}, error) {
	if global.MemoryService == nil {
		return nil, ginx.WithStack(status_error.MemoryServiceUnavailable)
	}
	settings, err := global.MemoryService.GetEmbeddingSettings()
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if settings.BaseURL == "" || settings.Model == "" || !settings.APIKeyConfigured {
		return nil, ginx.WithStack(status_error.MemoryEmbeddingNotConfigured)
	}
	result, err := global.MemoryService.Reindex(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ReindexMemoryIndexResponse{
		VectorCount: result.VectorCount,
		Model:       result.Model,
		Dimensions:  result.Dimensions,
	}, nil
}
