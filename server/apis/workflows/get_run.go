package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
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

// WorkflowRunResponse 只公开运行的业务事实，执行上下文与恢复帧留在仓储。
type WorkflowRunResponse struct {
	// 调用树根运行
	RootRunID string `json:"rootRunId"`
	// 运行标识
	ID string `json:"id"`
	// 工作流标识
	DefinitionID string `json:"definitionId"`
	// 版本标识
	RevisionID string `json:"revisionId"`
	// 版本序号
	RevisionOrdinal int64 `json:"revisionOrdinal"`
	// 运行状态
	Status constants.WorkflowRunStatus `json:"status"`
	// 运行参数
	Args any `json:"args"`
	// 执行配置
	Defaults models.WorkflowExecutionDefaults `json:"defaults"`
	// 运行预算
	Limits workflowdsl.Limits `json:"limits"`
	// 批准工作范围
	ScopeBinding models.WorkflowScopeBinding `json:"scopeBinding"`
	// 父运行标识
	ParentRunID string `json:"parentRunId"`
	// 跟随父运行
	Joined bool `json:"joined"`
	// 调用层级
	Depth int `json:"depth"`
	// 运行结果
	Result any `json:"result"`
	// 失败信息
	Failure *workflowdsl.Value `json:"failure,omitempty"`
	// 运行版本
	Version int64 `json:"version"`
	// 开始时间
	StartedAt int64 `json:"startedAt"`
	// 截止时间
	DeadlineAt int64 `json:"deadlineAt"`
	// 完成时间
	CompletedAt *int64 `json:"completedAt,omitempty"`
	// 接纳时间
	CreatedAt int64 `json:"createdAt"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
	// 触发来源
	TriggerSource string `json:"triggerSource"`
}

func workflowRunResponse(run models.WorkflowRun) WorkflowRunResponse {
	triggerSource, _ := run.Context["triggerSource"].(string)
	return WorkflowRunResponse{
		RootRunID:       run.RootRunID,
		ID:              run.ID,
		DefinitionID:    run.DefinitionID,
		RevisionID:      run.RevisionID,
		RevisionOrdinal: run.RevisionOrdinal,
		Status:          run.Status,
		Args:            run.Args,
		Defaults:        run.Defaults,
		Limits:          run.Limits,
		ScopeBinding:    run.ScopeBinding,
		ParentRunID:     run.ParentRunID,
		Joined:          run.Joined,
		Depth:           run.Depth,
		Result:          run.Result,
		Failure:         run.Failure,
		Version:         run.Version,
		StartedAt:       run.StartedAt,
		DeadlineAt:      run.DeadlineAt,
		CompletedAt:     run.CompletedAt,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
		TriggerSource:   triggerSource,
	}
}

// 运行详情
type WorkflowRunDetailResponse struct {
	// 成果文件
	Files []workflow.OutputFile `json:"files"`
	// 节点状态汇总
	NodeCounts []controllers.WorkflowNodeCount `json:"nodeCounts"`
	// 源码摘要
	SourceDigest string `json:"sourceDigest"`
	// 画布摘要
	ManifestDigest string `json:"manifestDigest"`
	// 编译清单
	Manifest workflowdsl.CanvasManifest `json:"manifest"`
	// 子运行
	Children []WorkflowRunResponse `json:"children"`
	// 运行信息
	Run WorkflowRunResponse `json:"run"`
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
	response := WorkflowRunDetailResponse{SourceDigest: revision.SourceDigest, ManifestDigest: workflowdsl.CanvasDigest(workflowdsl.ExportCanvas(revision.Manifest)), Run: workflowRunResponse(run), Manifest: workflowdsl.ExportCanvas(revision.Manifest), Children: []WorkflowRunResponse{}, HumanRequests: humans, OperationRequests: operations, InstanceCount: int(total), Instances: instances}
	for _, child := range children {
		response.Children = append(response.Children, workflowRunResponse(child))
	}
	response.Files = []workflow.OutputFile{}
	if run.Status == constants.WorkflowRunStatusSucceeded {
		response.Files = workflow.InspectOutputFiles(run, revision.Manifest.Meta.OutputSchema, run.Result)
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
