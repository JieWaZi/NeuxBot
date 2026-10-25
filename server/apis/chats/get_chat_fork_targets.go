package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取对话分叉位置
type GetChatForkTargets struct {
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
}

// Path 返回不包含原生句柄的分叉入口状态接口。
func (g *GetChatForkTargets) Path() string { return "/:chatId/fork-targets" }

// Output 返回按回合排序的可用性和不可用原因。
func (g *GetChatForkTargets) Output(ctx *gin.Context) (interface{}, error) {
	targets, err := global.ChatService.ForkTargets(ctx, g.ChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return targets, nil
}
