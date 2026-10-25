package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消执行
type CancelChatExecution struct {
	ginx.MethodPost
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
}

// Path 返回取消当前执行接口路径。
func (c *CancelChatExecution) Path() string {
	return "/:chatId/cancel"
}

// Output 触发当前 Runtime Invocation 取消。
func (c *CancelChatExecution) Output(*gin.Context) (interface{}, error) {
	if err := global.ExecutionService.Cancel(c.ChatID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
