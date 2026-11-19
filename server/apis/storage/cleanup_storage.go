package storage

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// CleanupStorage 清理存储资源。
type CleanupStorage struct {
	ginx.MethodPost
	// Data 请求体
	Data StorageConfirmation `in:"body"`
}

// StorageConfirmation 清理存储资源请求。
type StorageConfirmation struct {
	// PreviewID 确认标识
	PreviewID string `json:"previewId" validate:"required,max=64"`
}

// StorageRunReceipt 返回供客户端对账的维护身份。
type StorageRunReceipt struct {
	// RunID 运行标识
	RunID string `json:"runId"`
}

// Path 返回清理存储资源地址。
func (*CleanupStorage) Path() string { return "/cleanup" }

// Output 清理存储资源。
// [StoragePreviewExpired][40900009702][清理预览已失效]
// [StorageMaintenanceBusy][40900009705][另一项存储维护正在运行]
func (d *CleanupStorage) Output(ctx *gin.Context) (interface{}, error) {
	id, err := global.StorageService.StartCleanup(d.Data.PreviewID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return StorageRunReceipt{RunID: id}, nil
}
