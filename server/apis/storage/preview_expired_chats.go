package storage

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// PreviewExpiredChats 预览过期会话。
type PreviewExpiredChats struct {
	ginx.MethodPost
	// Data 请求体
	Data ExpirationPreviewInput `in:"body"`
}

// ExpirationPreviewInput 预览过期会话请求。
type ExpirationPreviewInput struct {
	// RetentionDays 保留天数
	RetentionDays int `json:"retentionDays" validate:"required,min=1,max=3650"`
	// ChatIDs 会话标识
	ChatIDs []string `json:"chatIds,omitempty" validate:"max=256,dive,min=1,max=64"`
}

// Path 返回预览过期会话地址。
func (*PreviewExpiredChats) Path() string { return "/expired-chats/preview" }

// Output 预览过期会话。
// [StoragePreviewExpired][40900009702][候选已变化]
// [StoragePolicyConflict][40900009703][存储策略已变化]
// [StoragePolicyInvalid][50000009701][存储策略不可用]
func (d *PreviewExpiredChats) Output(ctx *gin.Context) (interface{}, error) {
	preview, err := global.StorageService.PreviewExpiration(ctx.Request.Context(), d.Data.RetentionDays, d.Data.ChatIDs)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return preview, nil
}
