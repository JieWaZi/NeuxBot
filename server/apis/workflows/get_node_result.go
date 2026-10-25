package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取节点产出
type GetWorkflowNodeResult struct {
	// MethodGet 声明产出只读查询使用 GET 请求。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 实例标识
	InstanceID string `in:"path" name:"instanceId" validate:"required,max=32"`
	// 尝试标识
	AttemptID string `in:"path" name:"attemptId" validate:"required,max=32"`
}

// 节点产出引用
type WorkflowNodeResultReference struct {
	// 工作流标识
	WorkflowID string `json:"workflowId"`
	// 运行标识
	RunID string `json:"runId"`
	// 实例标识
	InstanceID string `json:"instanceId"`
	// 尝试标识
	AttemptID string `json:"attemptId"`
	// 最终消息
	MessageID string `json:"messageId,omitempty"`
	// 产出标题
	Title string `json:"title"`
}

// 节点产出详情
type WorkflowNodeResultResponse struct {
	// 产出引用
	Reference WorkflowNodeResultReference `json:"reference"`
	// 冻结版本
	RevisionID string `json:"revisionId"`
	// 源码节点
	NodeID string `json:"nodeId"`
	// 尝试代际
	AttemptGeneration int64 `json:"attemptGeneration"`
	// 对话标识
	ChatID string `json:"chatId"`
	// 执行标识
	ExecutionID string `json:"executionId"`
	// 尝试状态
	Status constants.WorkflowStepStatus `json:"status"`
	// 结果存在
	Present bool `json:"present"`
	// 成功结果
	Result any `json:"result"`
	// 页面定义
	Page *workflowdsl.PageDefinition `json:"page,omitempty"`
	// 文件展示
	FileView *workflowdsl.FileResultDefinition `json:"fileView,omitempty"`
	// 文件目录
	WorkingDirectory string `json:"workingDirectory"`
	// 可用文件
	Files []workflow.OutputFile `json:"files"`
}

func (*GetWorkflowNodeResult) Path() string {
	return "/:id/runs/:runId/instances/:instanceId/results/:attemptId"
}
func (o *GetWorkflowNodeResult) Output(ctx *gin.Context) (interface{}, error) {
	run, effect, presentation, schema, err := nodeResultFacts(ctx, o.ID, o.RunID, o.InstanceID, o.AttemptID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	present := effect.Status == constants.WorkflowStepStatusSucceeded && effect.Result != nil
	result := WorkflowNodeResultResponse{
		Reference: WorkflowNodeResultReference{
			WorkflowID: o.ID, RunID: run.ID, InstanceID: effect.InstanceID,
			AttemptID: effect.ID, MessageID: effect.FinalMessageID, Title: presentation.title(),
		},
		RevisionID: run.RevisionID, NodeID: effect.Boundary.NodeID,
		AttemptGeneration: effect.AttemptGeneration, ChatID: effect.ChatID,
		ExecutionID: effect.ExecutionID, Status: effect.Status, Present: present,
		Page: presentation.page, FileView: presentation.files,
		WorkingDirectory: run.ScopeBinding.WorkingDirectory, Files: []workflow.OutputFile{},
	}
	if present {
		result.Result = effect.Result.Data
		result.Files = workflow.InspectOutputFiles(run, schema, result.Result)
	}
	return result, nil
}

// nodeResultFacts 为产出读取与文件下载共同校验完整尝试身份，不跟随当前 Instance.EffectID。
func nodeResultFacts(ctx context.Context, workflowID, runID, instanceID, attemptID string) (models.WorkflowRun, models.WorkflowEffect, nodeResultPresentation, map[string]any, error) {
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, runID)
	if err != nil {
		return run, models.WorkflowEffect{}, nodeResultPresentation{}, nil, err
	}
	effect, err := global.WorkflowRunControllerManager.GetEffect(ctx, attemptID)
	if err != nil {
		return run, effect, nodeResultPresentation{}, nil, err
	}
	instance, err := global.WorkflowRunControllerManager.GetInstance(ctx, runID, instanceID)
	if err != nil {
		return run, effect, nodeResultPresentation{}, nil, err
	}
	if run.DefinitionID != workflowID || effect.RunID != runID || effect.InstanceID != instanceID || effect.Boundary.NodeID != instance.NodeID {
		return run, effect, nodeResultPresentation{}, nil, status_error.WorkflowNotFound
	}
	revision, err := global.WorkflowControllerManager.GetRevision(ctx, workflowID, run.RevisionID)
	if err != nil {
		return run, effect, nodeResultPresentation{}, nil, err
	}
	presentation, exists := nodePresentation(revision, instance.NodeID)
	if !exists {
		return run, effect, presentation, nil, status_error.WorkflowNotFound
	}
	for _, node := range revision.Manifest.Nodes {
		if node.ID == instance.NodeID && node.Kind == "worker" {
			schema, _ := node.Options["schema"].(map[string]any)
			return run, effect, presentation, schema, nil
		}
	}
	return run, effect, presentation, nil, status_error.WorkflowNotFound
}

// nodeResultPresentation 在读取入口选择 Revision 冻结的页面或文件展示契约。
type nodeResultPresentation struct {
	// page 是结构化内容的只读页面，文件交付时为空。
	page *workflowdsl.PageDefinition
	// files 是交付文件的入口说明，页面展示时为空。
	files *workflowdsl.FileResultDefinition
}

func (p nodeResultPresentation) title() string {
	if p.files != nil {
		return p.files.Title
	}
	if p.page != nil {
		return p.page.Title
	}
	return ""
}

func nodePresentation(revision models.WorkflowRevision, nodeID string) (nodeResultPresentation, bool) {
	if files, exists := revision.Manifest.Meta.NodeResultFiles[nodeID]; exists {
		return nodeResultPresentation{files: &files}, true
	}
	if page, exists := revision.Manifest.Meta.NodeResultPages[nodeID]; exists {
		return nodeResultPresentation{page: &page}, true
	}
	return nodeResultPresentation{}, false
}
