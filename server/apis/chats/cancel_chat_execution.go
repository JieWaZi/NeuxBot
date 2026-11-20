package chats

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 取消执行
type CancelChatExecution struct {
	ginx.MethodPost
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required"`
	// 执行标识
	ExecutionID string `in:"query" name:"executionId" validate:"max=32"`
	// 提交标识
	SubmissionID string `in:"query" name:"submissionId" validate:"max=128"`
}

// Path 返回取消当前执行接口路径。
func (c *CancelChatExecution) Path() string {
	return "/:chatId/cancel"
}

// Validate 校验一次取消只定位一个执行或尚在接纳的提交。
func (c *CancelChatExecution) Validate(*gin.Context) error {
	c.ExecutionID = strings.TrimSpace(c.ExecutionID)
	c.SubmissionID = strings.TrimSpace(c.SubmissionID)
	if (c.ExecutionID == "") == (c.SubmissionID == "") {
		return status_error.BadRequest
	}
	return nil
}

// Output 取消使用原回执或提交标识定位的执行。
func (c *CancelChatExecution) Output(ctx *gin.Context) (interface{}, error) {
	if c.ExecutionID != "" {
		if err := global.ExecutionService.CancelExecutionAndWait(ctx.Request.Context(), c.ChatID, c.ExecutionID); err != nil {
			return nil, ginx.WithStack(err)
		}
		return ginx.CommonSuccessResponse(), nil
	}
	if err := global.ExecutionService.CancelSubmissionAndWait(ctx.Request.Context(), c.ChatID, c.SubmissionID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// 取消聊天首发
type CancelFirstChatExecution struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"query" name:"wakerId" validate:"required,max=32"`
	// 首次发送标识
	ClientRequestID string `in:"query" name:"clientRequestId" validate:"required,max=64"`
}

func (c *CancelFirstChatExecution) Path() string { return "/first-send/cancel" }

func (c *CancelFirstChatExecution) Output(ctx *gin.Context) (interface{}, error) {
	wakerID := strings.TrimSpace(c.WakerID)
	identity, err := requestidempotency.New(c.ClientRequestID, nil)
	if err != nil || wakerID == "" {
		return nil, status_error.BadRequest
	}
	if err := global.ExecutionService.CancelSubmissionAndWait(ctx.Request.Context(), identity.ResourceID("chat", wakerID), identity.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
