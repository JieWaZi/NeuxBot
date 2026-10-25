package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 重试失败回复
type RetryChatExecution struct {
	// MethodPost 声明恢复执行需要显式 POST 请求。
	ginx.MethodPost
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 执行标识
	ExecutionID string `in:"path" name:"executionId" validate:"required,max=32"`
}

// Path 返回按失败执行幂等重试的接口路径。
func (r *RetryChatExecution) Path() string { return "/:chatId/executions/:executionId/retry" }

// Output 由后端恢复原输入并重新校验重试资格。
func (r *RetryChatExecution) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.ExecutionService.Start(ctx, execution.WorkIntent{
		Chat: execution.ChatTarget{ChatID: r.ChatID}, RetryExecutionID: r.ExecutionID,
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
