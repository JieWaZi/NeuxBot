package storage

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// PreviewStorageCleanup 预览资源清理。
type PreviewStorageCleanup struct {
	ginx.MethodPost
	// Data 请求体
	Data PreviewStorageCleanupInput `in:"body"`
}

// PreviewStorageCleanupInput 预览资源清理请求。
type PreviewStorageCleanupInput struct {
	// Categories 清理类别
	Categories []constants.StorageCleanupKind `json:"categories" validate:"required,min=1,max=3"`
}

// Path 返回预览资源清理地址。
func (*PreviewStorageCleanup) Path() string { return "/cleanup-preview" }

// Output 预览资源清理。
func (d *PreviewStorageCleanup) Output(ctx *gin.Context) (interface{}, error) {
	preview, err := global.StorageService.PreviewCleanup(ctx.Request.Context(), d.Data.Categories)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return preview, nil
}
