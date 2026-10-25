package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 分叉对话请求
type ForkChatRequest struct {
	// 结束回合
	ExecutionID string `json:"executionId" validate:"required,max=64"`
	// 幂等标识
	ClientRequestID string `json:"clientRequestId" validate:"required,max=128"`
}

// 分叉对话结果
type ForkChatResponse struct {
	// 新对话标识
	ChatID string `json:"chatId"`
}

// 分叉对话
type ForkChat struct {
	ginx.MethodPost
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 请求体
	Body ForkChatRequest `in:"body"`
}

// Path 返回普通伙伴会话的分叉接口。
func (f *ForkChat) Path() string { return "/:chatId/fork" }

// Output 在原生分叉和历史提交完成后返回导航所需的新标识。
func (f *ForkChat) Output(ctx *gin.Context) (interface{}, error) {
	id, err := global.ExecutionService.ForkChat(ctx, chatservice.ForkInput{ChatID: f.ChatID, ExecutionID: f.Body.ExecutionID, ClientRequestID: f.Body.ClientRequestID})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ForkChatResponse{ChatID: id}, nil
}
