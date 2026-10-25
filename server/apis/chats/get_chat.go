package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取对话
type GetChat struct {
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
}

// Path 返回对话详情接口路径。
func (g *GetChat) Path() string {
	return "/:chatId"
}

// Output 返回不重放完整消息历史的查询摘要。
func (g *GetChat) Output(ctx *gin.Context) (interface{}, error) {
	detail, err := global.ChatService.GetChatSummary(ctx, g.ChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return detail, nil
}
