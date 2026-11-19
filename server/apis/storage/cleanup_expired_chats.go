package storage

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// CleanupExpiredChats 清理过期会话。
type CleanupExpiredChats struct {
	ginx.MethodPost
	// Data 请求体
	Data StorageConfirmation `in:"body"`
}

// Path 返回清理过期会话地址。
func (*CleanupExpiredChats) Path() string { return "/expired-chats/cleanup" }

// Output 清理过期会话。
// [StoragePreviewExpired][40900009702][清理预览已失效]
// [StorageMaintenanceBusy][40900009705][另一项存储维护正在运行]
// [StoragePolicyInvalid][50000009701][存储策略不可用]
func (d *CleanupExpiredChats) Output(ctx *gin.Context) (interface{}, error) {
	id, err := global.StorageService.StartExpiredCleanup(d.Data.PreviewID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return StorageRunReceipt{RunID: id}, nil
}
