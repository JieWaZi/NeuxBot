package chats

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
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
		if err := stopChatExecution(ctx.Request.Context(), c.ChatID, c.ExecutionID); err != nil {
			return nil, ginx.WithStack(err)
		}
		return ginx.CommonSuccessResponse(), nil
	}
	if err := stopChatSubmission(ctx.Request.Context(), c.ChatID, c.SubmissionID); err != nil {
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
	if err := stopChatSubmission(ctx.Request.Context(), identity.ResourceID("chat", wakerID), identity.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}

// stopChatExecution 在公开停止用例中分别结束原生执行与该回合拥有的工作流。
func stopChatExecution(ctx context.Context, chatID, executionID string) error {
	global.ExecutionService.CancelExecution(chatID, executionID)
	var workflowErr error
	if global.WorkflowService != nil {
		workflowErr = global.WorkflowService.StopChatWorkflowTurn(ctx, chatID, executionID)
	}
	if err := global.ExecutionService.CancelExecutionAndWait(ctx, chatID, executionID); err != nil {
		return err
	}
	return workflowErr
}

// stopChatSubmission 用原提交事实定位关联工作，不在执行注册表内编排工作流。
func stopChatSubmission(ctx context.Context, chatID, submissionID string) error {
	nativeErr := global.ExecutionService.CancelSubmissionAndWait(ctx, chatID, submissionID)
	if global.WorkflowService == nil {
		return nativeErr
	}
	receipt, err := global.ChatService.ReadSubmission(ctx, chatID, submissionID)
	if util.HasStatusCode(err, status_error.ChatNotFound) || err == nil && receipt == nil {
		return nativeErr
	}
	if err != nil {
		return err
	}
	if err := global.WorkflowService.StopChatWorkflowTurn(ctx, chatID, receipt.ID); err != nil {
		return err
	}
	return nativeErr
}
