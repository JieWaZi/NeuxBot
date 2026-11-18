package storage

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// UpdateChatProtection 更新会话保留。
type UpdateChatProtection struct {
	ginx.MethodPut
	// ChatID 会话标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=64"`
	// Data 请求体
	Data ChatProtectionInput `in:"body"`
}

// ChatProtectionInput 更新会话保留请求。
type ChatProtectionInput struct {
	// Protected 保留会话
	Protected *bool `json:"protected" validate:"required"`
}

// Path 返回更新会话保留地址。
func (*UpdateChatProtection) Path() string { return "/chats/:chatId/protection" }

// Output 更新会话保留。
func (d *UpdateChatProtection) Output(ctx *gin.Context) (interface{}, error) {
	if d.Data.Protected == nil {
		return nil, ginx.WithStack(status_error.BadRequest)
	}
	if err := global.ChatService.SetRetentionProtection(ctx.Request.Context(), d.ChatID, *d.Data.Protected); err != nil {
		return nil, ginx.WithStack(err)
	}
	global.StorageService.Invalidate()
	return ginx.CommonSuccessResponse(), nil
}
