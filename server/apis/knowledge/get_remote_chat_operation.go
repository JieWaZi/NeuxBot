package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查询聊天远端操作；刷新只读取原回执与远端任务。
type GetRemoteChatOperation struct {
	ginx.MethodGet
	// 聊天标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 操作标识
	RequestID string `in:"query" name:"requestId" validate:"required,max=64"`
}

func (o *GetRemoteChatOperation) Path() string { return "/chat-operations/:chatId" }
func (o *GetRemoteChatOperation) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	result, err := global.KnowledgeService.ReadRemoteChatOperation(ctx, o.ChatID, o.RequestID)
	if err != nil {
		return nil, err
	}
	return remoteChatDTO(result), nil
}
func init() { Router.Register(&GetRemoteChatOperation{}) }
