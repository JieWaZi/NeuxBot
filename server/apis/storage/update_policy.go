package storage

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// UpdateStoragePolicy 保存存储策略。
type UpdateStoragePolicy struct {
	ginx.MethodPut
	// Data 请求体
	Data StoragePolicyInput `in:"body"`
}

// StoragePolicyInput 保存存储策略请求。
type StoragePolicyInput struct {
	// Enabled 自动清理
	Enabled bool `json:"enabled"`
	// RetentionDays 保留天数
	RetentionDays int `json:"retentionDays" validate:"required,min=1,max=3650"`
	// ExpectedRevision 期望版本
	ExpectedRevision uint64 `json:"expectedRevision"`
	// PreviewID 确认标识
	PreviewID string `json:"previewId,omitempty" validate:"max=64"`
}

// Path 返回保存存储策略地址。
func (*UpdateStoragePolicy) Path() string { return "/policy" }

// Output 保存存储策略。
// [StoragePolicyConflict][40900009703][存储策略已变化]
// [StoragePreviewExpired][40900009702][清理预览已失效]
// [StoragePolicyInvalid][50000009701][存储策略不可用]
func (d *UpdateStoragePolicy) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.StorageService.UpdatePolicy(d.Data.Enabled, d.Data.RetentionDays, d.Data.ExpectedRevision, d.Data.PreviewID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
