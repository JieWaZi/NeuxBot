package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// workflowDependencies 是 HTTP 用例直接编排的持久化及外部执行端口。
type workflowDependencies struct {
	// Store 保存定义、不可变版本、命令回执与事件。
	Store controllers.WorkflowController
	// Transactions 提交跨记录的原子用例。
	Transactions controllers.TransactionController
	// Runs 读取冻结运行与节点事实。
	Runs controllers.WorkflowRunController
	// CreationSessions 保存创作会话和修改批注。
	CreationSessions controllers.WorkflowCreationController
	// State 共享 API 与 Runner 的命令锁、租约及状态提交边界。
	State *workflowstate.Coordinator
	// WorkerCancel 等待对应 Core 执行真实停止。
	WorkerCancel func(context.Context, models.WorkflowEffect) error
	// Executions 接纳并派发中立创作工作。
	Executions execution.Service
	// Chats 读取创作的权威 Chat 与执行事实。
	Chats chat.Module
	// Wakers 校验创作伙伴并确保内置伙伴存在。
	Wakers controllers.WakerController
	// Projects 校验用户批准的项目范围。
	Projects controllers.ProjectController
	// DataDirectory 拥有工作流的受管默认目录。
	DataDirectory string
	// SaveSource 复用 HTTP 与 scoped MCP 共同使用的完整源码保存规则。
	SaveSource func(context.Context, string, string, string, string, string) (workflowdsl.SaveReceipt, []workflowdsl.Diagnostic, error)
	// RetryStart 复用多入口运行接纳并冻结来源运行的版本、参数与范围。
	RetryStart func(context.Context, models.WorkflowRun, string) (models.WorkflowRun, error)
}

// dependencies 在入口装配共享状态边界，不保存另一份命令锁或执行事实。
func dependencies() *workflowDependencies {
	chats := global.ChatService
	worker := workflow.CoreWorkerAdapter{Executions: global.ExecutionService, Chats: chats}
	return &workflowDependencies{
		Store:            global.WorkflowControllerManager,
		Transactions:     global.TransactionControllerManager,
		Runs:             global.WorkflowRunControllerManager,
		CreationSessions: global.WorkflowCreationControllerManager,
		State:            global.WorkflowState,
		WorkerCancel:     worker.Cancel,
		Executions:       global.ExecutionService,
		Chats:            chats,
		Wakers:           global.WakerControllerManager,
		Projects:         global.ProjectControllerManager,
		DataDirectory:    global.Config.ManagedFilesDirectory,
		SaveSource: func(ctx context.Context, definitionID, requestID, expectedID, source, restoredFrom string) (workflowdsl.SaveReceipt, []workflowdsl.Diagnostic, error) {
			return global.WorkflowService.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definitionID, RequestID: requestID, ExpectedRevisionID: expectedID, Source: source, SDKVersion: constants.WorkflowSDKVersion, RestoredFromRevisionID: restoredFrom})
		},
		RetryStart: func(ctx context.Context, source models.WorkflowRun, requestID string) (models.WorkflowRun, error) {
			return global.WorkflowService.StartRun(ctx, workflow.StartRunCommand{DefinitionID: source.DefinitionID, RevisionID: source.RevisionID, Args: source.Args, Defaults: source.Defaults, WorkerConfigurations: source.WorkerConfigurations, ApprovedScope: &source.ScopeBinding, RequestID: requestID, RetrySourceRunID: source.ID})
		},
	}
}

// requireOwnedRun 在命令入口校验运行归属，防止跨工作流提交恢复请求。
func requireOwnedRun(ctx context.Context, definitionID, runID string) error {
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.DefinitionID != definitionID {
		return status_error.WorkflowNotFound
	}
	return nil
}
