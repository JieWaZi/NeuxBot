package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取运行
type GetWorkflowRun struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 实例游标
	Offset int `in:"query" name:"offset" validate:"min=0"`
}

// 运行详情
type WorkflowRunDetailResponse struct {
	// 文件产物
	Files []workflow.OutputFile `json:"files"`
	// 节点状态汇总
	NodeCounts []controllers.WorkflowNodeCount `json:"nodeCounts"`
	// 编译清单
	Manifest workflowdsl.Manifest `json:"manifest"`
	// 子运行
	Children []models.WorkflowRun `json:"children"`
	// 运行信息
	Run models.WorkflowRun `json:"run"`
	// 节点实例
	Instances []models.WorkflowNodeInstance `json:"instances"`
	// 人工请求
	HumanRequests []models.WorkflowHumanRequest `json:"humanRequests"`
	// 操作许可
	OperationRequests []models.WorkflowOperationRequest `json:"operationRequests"`
	// 实例总数
	InstanceCount int `json:"instanceCount"`
	// 后续游标
	NextOffset int `json:"nextOffset"`
}

func (*GetWorkflowRun) Path() string { return "/:id/runs/:runId" }
func (o *GetWorkflowRun) Output(ctx *gin.Context) (interface{}, error) {
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
	instances, total, err := global.WorkflowRunControllerManager.ListInstances(ctx, o.RunID, 200, o.Offset)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	humans, err := global.WorkflowRunControllerManager.ListHumanRequests(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	operations, err := global.WorkflowRunControllerManager.ListOperations(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	children, err := global.WorkflowRunControllerManager.ListChildren(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	response := WorkflowRunDetailResponse{Run: run, Manifest: revision.Manifest, Children: children, HumanRequests: humans, OperationRequests: operations, InstanceCount: int(total), Instances: instances}
	response.Files, err = global.WorkflowService.OutputFiles(ctx, run)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	response.NodeCounts, err = global.WorkflowRunControllerManager.NodeCounts(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if o.Offset+len(instances) < int(total) {
		response.NextOffset = o.Offset + len(instances)
	}
	return response, nil
}
