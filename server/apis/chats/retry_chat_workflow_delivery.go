package chats

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 补交工作流结果
type RetryChatWorkflowDelivery struct {
	ginx.MethodPost
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 请求体
	Body RetryChatWorkflowDeliveryRequest `in:"body"`
}

// 工作流补交请求
type RetryChatWorkflowDeliveryRequest struct {
	// 补写总结
	Summary bool `json:"summary,omitempty"`
}

// Path 返回同一运行的独立补交入口。
func (*RetryChatWorkflowDelivery) Path() string { return "/:chatId/workflow-runs/:runId/delivery" }

// Output 重试原运行的附件或补写摘要。
func (o *RetryChatWorkflowDelivery) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.WorkflowService.RetryChatDelivery(ctx, o.ChatID, o.RunID, o.Body.Summary); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
