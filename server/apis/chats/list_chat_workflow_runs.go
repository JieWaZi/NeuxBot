package chats

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 查询对话回合的工作流运行
type ListChatWorkflowRuns struct {
	// 查询方法
	ginx.MethodGet
	// 对话标识
	ChatID string `in:"path" name:"chatId" validate:"required,max=32"`
	// 逗号分隔的回合标识
	ExecutionIDs string `in:"query" name:"executionIds" validate:"required,max=3299"`
}

// ChatWorkflowRun 是原消息链接使用的运行摘要。
type ChatWorkflowRun struct {
	// 运行标识
	ID string `json:"id"`
	// 工作流标识
	DefinitionID string `json:"definitionId"`
	// 执行版本
	RevisionID string `json:"revisionId"`
	// 调用时名称
	Name string `json:"name"`
	// 发起回合
	ExecutionID string `json:"executionId"`
	// 发起消息
	InputMessageID string `json:"inputMessageId"`
	// 重试来源
	SourceRunID string `json:"sourceRunId,omitempty"`
	// 工作台发起
	SourceWorkbench bool `json:"sourceWorkbench,omitempty"`
	// 执行状态
	Status constants.WorkflowRunStatus `json:"status"`
	// 历史已移除
	Removed bool `json:"removed"`
	// 接纳时间
	CreatedAt int64 `json:"createdAt"`
	// 运行结束时间
	CompletedAt *int64 `json:"completedAt,omitempty"`
	// 成果交付错误
	DeliveryError string `json:"deliveryError,omitempty"`
	// 原始快照错误
	SnapshotError string `json:"snapshotError,omitempty"`
	// 总结补写时间
	SummaryFinishedAt *int64 `json:"summaryFinishedAt,omitempty"`
	// 待处理请求
	ActiveRequests []controllers.WorkflowChatRequest `json:"activeRequests"`
	// 历史请求
	AnsweredRequests []controllers.WorkflowChatRequest `json:"answeredRequests"`
}

// ChatWorkflowRunsResponse 保存可见回合的运行记录。
type ChatWorkflowRunsResponse struct {
	// 运行列表
	Resources []ChatWorkflowRun `json:"resources"`
}

func (*ListChatWorkflowRuns) Path() string { return "/:chatId/workflow-runs" }

func (o *ListChatWorkflowRuns) Validate(*gin.Context) error {
	ids := strings.Split(o.ExecutionIDs, ",")
	if len(ids) > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	for _, id := range ids {
		if id == "" || len(id) > 32 || strings.TrimSpace(id) != id {
			return ginx.WithStack(status_error.BadRequest)
		}
	}
	return nil
}

func (o *ListChatWorkflowRuns) Output(ctx *gin.Context) (interface{}, error) {
	var response ChatWorkflowRunsResponse
	err := global.TransactionControllerManager.Transaction(ctx, func(tx context.Context) error {
		var err error
		response, err = o.read(tx)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return response, nil
}

func (o *ListChatWorkflowRuns) read(ctx context.Context) (ChatWorkflowRunsResponse, error) {
	if _, err := global.ChatControllerManager.Get(ctx, o.ChatID); err != nil {
		return ChatWorkflowRunsResponse{}, err
	}
	if global.WorkflowService != nil && !global.WorkflowService.MayHaveChatRunSources(o.ChatID) {
		return ChatWorkflowRunsResponse{Resources: []ChatWorkflowRun{}}, nil
	}
	runs, err := global.WorkflowRunControllerManager.ListChatRunSources(ctx, o.ChatID, strings.Split(o.ExecutionIDs, ","))
	if err != nil {
		return ChatWorkflowRunsResponse{}, err
	}
	requests, err := global.WorkflowRunControllerManager.ListChatRunRequests(ctx, runs, time.Now().UnixMilli())
	if err != nil {
		return ChatWorkflowRunsResponse{}, err
	}
	resources := make([]ChatWorkflowRun, 0, len(runs))
	for _, r := range runs {
		item := ChatWorkflowRun{
			ID: r.ID, DefinitionID: r.DefinitionID, RevisionID: r.RevisionID, Name: r.SourceName,
			ExecutionID: r.SourceExecutionID, InputMessageID: r.SourceMessageID, SourceRunID: r.SourceRunID,
			SourceWorkbench: r.SourceWorkbench,
			Status:          r.Status, Removed: r.HistoryDeletedAt != nil, CreatedAt: r.CreatedAt, CompletedAt: r.CompletedAt,
			DeliveryError: r.ChatDeliveryError, SnapshotError: workflow.ChatSnapshotError(r),
			SummaryFinishedAt: r.ChatSummaryFinishedAt,
			ActiveRequests:    []controllers.WorkflowChatRequest{}, AnsweredRequests: []controllers.WorkflowChatRequest{},
		}
		for _, q := range requests[r.ID] {
			if q.Active {
				item.ActiveRequests = append(item.ActiveRequests, q)
			} else {
				item.AnsweredRequests = append(item.AnsweredRequests, q)
			}
		}
		resources = append(resources, item)
	}
	return ChatWorkflowRunsResponse{Resources: resources}, nil
}
