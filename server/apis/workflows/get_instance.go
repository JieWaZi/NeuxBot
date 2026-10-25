package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取执行步骤
type GetWorkflowInstance struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 实例标识
	InstanceID string `in:"path" name:"instanceId" validate:"required,max=32"`
}

// 实际工具步骤
type WorkflowActionCallView struct {
	// 调用身份
	models.WorkflowActionCall
	// 实际返回
	Result *workflowdsl.Value `json:"result,omitempty"`
}

// 效果收据
type WorkflowEffectView struct {
	// 跳过意图
	SkipRequested bool `json:"skipRequested"`
	// 效果身份
	ID string `json:"id"`
	// 尝试代际
	AttemptGeneration int64 `json:"attemptGeneration"`
	// 生命周期
	Status constants.WorkflowStepStatus `json:"status"`
	// 结果候选
	CandidateSubmitted bool `json:"candidateSubmitted"`
	// 等待期限
	DeadlineAt int64 `json:"deadlineAt"`
	// 子运行身份
	ChildRunID string `json:"childRunId"`
}

// 执行步骤详情
type WorkflowInstanceResponse struct {
	// 运行版本
	RunVersion int64 `json:"runVersion"`
	// 实际节点
	Instance models.WorkflowNodeInstance `json:"instance"`
	// 效果收据
	Effect WorkflowEffectView `json:"effect"`
	// 尝试历史
	Attempts []models.WorkflowEffect `json:"attempts"`
	// 独立伙伴对话
	Chat *chat.ChatSummary `json:"chat,omitempty"`
	// 权威执行
	Execution *models.Execution `json:"execution,omitempty"`
	// 内部工具调用
	ActionCalls []WorkflowActionCallView `json:"actionCalls"`
}

func (*GetWorkflowInstance) Path() string { return "/:id/runs/:runId/instances/:instanceId" }
func (o *GetWorkflowInstance) Output(ctx *gin.Context) (interface{}, error) {
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if run.DefinitionID != o.ID {
		return nil, ginx.WithStack(status_error.WorkflowNotFound)
	}
	instance, err := global.WorkflowRunControllerManager.GetInstance(ctx, o.RunID, o.InstanceID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	effect, err := global.WorkflowRunControllerManager.GetEffect(ctx, instance.EffectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	calls, err := global.WorkflowRunControllerManager.ListActionCalls(ctx, o.RunID, instance.EffectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	view := WorkflowInstanceResponse{RunVersion: run.Version, Instance: instance, Effect: WorkflowEffectView{ID: effect.ID, Status: effect.Status, AttemptGeneration: effect.AttemptGeneration, CandidateSubmitted: effect.Candidate != nil, DeadlineAt: effect.DeadlineAt, ChildRunID: effect.ChildRunID, SkipRequested: effect.SkipRequested}, ActionCalls: []WorkflowActionCallView{}}
	view.Attempts, err = global.WorkflowRunControllerManager.ListInstanceAttempts(ctx, o.RunID, o.InstanceID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	for _, call := range calls {
		view.ActionCalls = append(view.ActionCalls, WorkflowActionCallView{WorkflowActionCall: call, Result: call.Result})
	}
	if instance.ChatID != "" && global.ChatService != nil {
		summary, err := global.ChatService.GetChatSummary(ctx, instance.ChatID)
		if err == nil {
			view.Chat = &summary
		} else if errorKeyForQuery(err) != status_error.ChatNotFound.Key() {
			return nil, ginx.WithStack(err)
		}
		if global.ChatService != nil {
			if execution, err := global.ChatService.ReadExecution(ctx, instance.ChatID, instance.ExecutionID); err == nil {
				view.Execution = &execution
			} else if errorKeyForQuery(err) != status_error.ChatNotFound.Key() && errorKeyForQuery(err) != status_error.ExecutionNotFound.Key() {
				return nil, ginx.WithStack(err)
			}
		}
	}
	return view, nil
}

func errorKeyForQuery(err error) string {
	if value, ok := err.(interface{ Key() string }); ok {
		return value.Key()
	}
	return ""
}
