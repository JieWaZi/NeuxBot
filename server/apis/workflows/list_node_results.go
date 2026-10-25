package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 列出节点产出
type ListWorkflowNodeResults struct {
	// MethodGet 声明产出只读查询使用 GET 请求。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 节点标识
	NodeID string `in:"query" name:"nodeId" validate:"required,max=64"`
	// 分页偏移
	Offset int `in:"query" name:"offset" validate:"min=0"`
}

// 节点产出条目
type WorkflowNodeResultItem struct {
	// 产出引用
	Reference WorkflowNodeResultReference `json:"reference"`
	// 实例标题
	Label string `json:"label"`
	// 实例路径
	Path string `json:"path"`
	// 执行状态
	Status constants.WorkflowStepStatus `json:"status"`
}

// 节点产出列表
type WorkflowNodeResultsResponse struct {
	// 实例条目
	Items []WorkflowNodeResultItem `json:"items"`
	// 实例总数
	Total int64 `json:"total"`
	// 后续偏移
	NextOffset int `json:"nextOffset"`
}

func (*ListWorkflowNodeResults) Path() string { return "/:id/runs/:runId/node-results" }
func (o *ListWorkflowNodeResults) Output(ctx *gin.Context) (interface{}, error) {
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if run.DefinitionID != o.ID {
		return nil, ginx.WithStack(status_error.WorkflowNotFound)
	}
	revision, err := global.WorkflowControllerManager.GetRevision(ctx, o.ID, run.RevisionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	presentation, exists := nodePresentation(revision, o.NodeID)
	if !exists {
		return nil, ginx.WithStack(status_error.WorkflowNotFound)
	}
	instances, total, err := global.WorkflowRunControllerManager.ListNodeInstances(ctx, o.RunID, o.NodeID, 20, o.Offset)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	result := WorkflowNodeResultsResponse{Items: []WorkflowNodeResultItem{}, Total: total}
	for _, instance := range instances {
		result.Items = append(result.Items, WorkflowNodeResultItem{Reference: WorkflowNodeResultReference{WorkflowID: o.ID, RunID: o.RunID, InstanceID: instance.ID, AttemptID: instance.EffectID, Title: presentation.title()}, Label: instance.Label, Path: instance.Path, Status: instance.Status})
	}
	if o.Offset+len(instances) < int(total) {
		result.NextOffset = o.Offset + len(instances)
	}
	return result, nil
}
