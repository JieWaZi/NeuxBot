package chats

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除对话
type DeleteChat struct {
	ginx.MethodDelete
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
}

// Path 返回对话删除接口路径。
func (d *DeleteChat) Path() string {
	return "/:chatId"
}

// Output 执行归属校验后删除对话全部受管数据。
// [ChatNotFound][40400000005][Chat 不存在]
// [ChatHasActiveExecution][40900000006][Chat 存在活跃执行]
// [ChatLogError][50000000004][Chat 权威日志读写失败]
// [ChatDirectoryOwnershipInvalid][50000000007][Chat 目录归属校验失败]
func (d *DeleteChat) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.ExecutionService.CancelAndWait(ctx, d.ChatID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ChatService.DeleteChatsAtomically(ctx, []string{d.ChatID}, func(ctx context.Context) error {
		return global.WorkflowCreationControllerManager.DeleteCreationSessionsByChatID(ctx, d.ChatID)
	}); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
