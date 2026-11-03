package workflows

import (
	"slices"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取原版本重试计划
type GetWorkflowRetryPlan struct {
	// 查询方法
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 原运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
}

// 重试计划条目
type WorkflowRetryPlanItem struct {
	// 实例标识
	InstanceID string `json:"instanceId"`
	// 节点标识
	NodeID string `json:"nodeId"`
	// 处理方式
	Action string `json:"action"`
	// 处理原因
	Reason string `json:"reason"`
}

// 原版本重试计划
type WorkflowRetryPlan struct {
	// 原运行标识
	RunID string `json:"runId"`
	// 原版本标识
	RevisionID string `json:"revisionId"`
	// 并发版本
	ExpectedVersion int64 `json:"expectedVersion"`
	// 可否重试
	Allowed bool `json:"allowed"`
	// 节点计划
	Items []WorkflowRetryPlanItem `json:"items"`
}

func (*GetWorkflowRetryPlan) Path() string { return "/:id/runs/:runId/retry-plan" }
func (o *GetWorkflowRetryPlan) Output(ctx *gin.Context) (interface{}, error) {
	state, err := global.WorkflowRunControllerManager.LoadRunState(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if state.Run.DefinitionID != o.ID {
		return nil, ginx.WithStack(status_error.WorkflowNotFound)
	}
	return buildWorkflowRetryPlan(state), nil
}

// buildWorkflowRetryPlan 使用真实效果未知状态，与重试接纳入口保持一致。
func buildWorkflowRetryPlan(state controllers.WorkflowRunState) WorkflowRetryPlan {
	unknown := map[string]bool{}
	for _, effect := range state.Effects {
		if effect.Status == constants.WorkflowStepStatusUnknown {
			unknown[effect.InstanceID] = true
		}
	}
	plan := WorkflowRetryPlan{RunID: state.Run.ID, RevisionID: state.Run.RevisionID, ExpectedVersion: state.Run.Version, Allowed: state.Run.Status == constants.WorkflowRunStatusFailed || state.Run.Status == constants.WorkflowRunStatusCancelled, Items: []WorkflowRetryPlanItem{}}
	if len(unknown) > 0 {
		plan.Allowed = false
	}
	// 运行快照按实例身份装载；计划沿用运行详情的创建顺序，避免随机身份打乱步骤。
	instances := slices.Clone(state.Instances)
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].CreatedAt != instances[j].CreatedAt {
			return instances[i].CreatedAt < instances[j].CreatedAt
		}
		return instances[i].ID < instances[j].ID
	})
	for _, instance := range instances {
		item := WorkflowRetryPlanItem{InstanceID: instance.ID, NodeID: instance.NodeID, Action: "rerun", Reason: "重新执行并按实际操作重新授权"}
		if instance.Status == constants.WorkflowStepStatusSucceeded && (instance.Kind == "worker" || instance.Kind == "askUser") {
			item.Action = "evaluate_reuse"
			item.Reason = "仅在版本、输入、资源与配置仍一致时复用，确认问题及文件结果重新执行"
		}
		if instance.Status == constants.WorkflowStepStatusUnknown || unknown[instance.ID] {
			item.Action = "reconcile"
			item.Reason = "原效果未知，禁止自动重试"
			plan.Allowed = false
		}
		plan.Items = append(plan.Items, item)
	}
	return plan
}
