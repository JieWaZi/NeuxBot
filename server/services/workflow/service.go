package workflow

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// Service 是 HTTP、执行能力、触发入口及 Runner 共同复用的应用边界。
type Service struct {
	// commands 保存与领域变更同事务提交的 CLI 回执。
	commands agentcommand.Store
	// events 是已有应用事件通道。
	events *events.Hub
	// store 拥有不可变源码和持久运行事实。
	store controllers.WorkflowController
	// transactions 保证跨记录的不变量一起提交。
	transactions controllers.TransactionController
	// runs 拥有带租约和 fence 的运行事实。
	runs controllers.WorkflowRunController
	// state 协调 API、Runner 与 CLI 对同一运行的写入。
	state *workflowstate.Coordinator
	// runner 是显式 Start 创建的受管后台生命周期。
	runner *runnerLifecycle
	// worker 是既有 Execution Core 的窄适配端口。
	worker WorkerPort
	// creationSessions 保存创作会话与批注。
	creationSessions controllers.WorkflowCreationController
	// consumers 保存由用户选择且只在一个真实回合内有效的使用者能力。
	consumers controllers.WorkflowConsumerController
	// executions 为真实创作派发中立工作。
	executions execution.Service
	// chats 校验当前创作执行身份。
	chats chat.Module
	// wakers 校验真实创作与节点伙伴。
	wakers controllers.WakerController
	// resources 返回真实可用智能体等公开资源目录。
	resources func(context.Context) (map[string]any, error)
	// pythonExecutable 是用户配置的解释器路径。
	pythonExecutable string
	// connectors 解析创作获准的现有凭据引用。
	connectors controllers.MCPConnectorController
	// dataDirectory 拥有未选择目录时的运行工作区。
	dataDirectory string
	// imCall 在 Runner 冻结范围内执行可对账的内置 IM 动作。
	imCall func(context.Context, models.WorkflowRun, models.WorkflowActionCall) (any, error)
	// imValidate 在接纳及发送前核验真实聊天依赖。
	imValidate func(context.Context, string) error
	// projects 解析创作批准的项目范围。
	projects controllers.ProjectController
}

// Config 是显式装配 Workflow 服务需要的业务端口。
type Config struct {
	// Commands 保存主库命令回执。
	Commands agentcommand.Store
	// Events 复用应用内状态变化通道。
	Events *events.Hub
	// State 是 API 和后台运行共用的命令锁、租约与监听器身份。
	State *workflowstate.Coordinator
	// Store 是 Workflow Repository。
	Store controllers.WorkflowController
	// Runs 是运行 Repository。
	Runs controllers.WorkflowRunController
	// Worker 是真实执行与结果对账端口。
	Worker WorkerPort
	// CreationSessions 是创作仓储。
	CreationSessions controllers.WorkflowCreationController
	// Consumers 是单回合使用者能力仓储。
	Consumers controllers.WorkflowConsumerController
	// Executions 是中立执行入口。
	Executions execution.Service
	// Chats 是权威创作会话查询。
	Chats chat.Module
	// Wakers 是伙伴目录。
	Wakers controllers.WakerController
	// Resources 是智能体等公开资源目录。
	Resources func(context.Context) (map[string]any, error)
	// PythonExecutable 是用户配置的 CPython 3.11+ 路径。
	PythonExecutable string
	// Connectors 是已有连接器仓储。
	Connectors controllers.MCPConnectorController
	// DataDirectory 是应用受管数据根目录。
	DataDirectory string
	// IMCall 为内置 IM 动作提供服务端范围及耐久对账。
	IMCall func(context.Context, models.WorkflowRun, models.WorkflowActionCall) (any, error)
	// IMValidate 为手动、自动任务及子运行共用目标校验。
	IMValidate func(context.Context, string) error
	// Projects 是创作项目及目录关系仓储。
	Projects controllers.ProjectController
	// Transactions 是现有统一事务入口。
	Transactions controllers.TransactionController
}

// New 只构造服务，不启动任何后台流程。
func New(config Config) *Service {
	return &Service{commands: config.Commands, events: config.Events, store: config.Store, transactions: config.Transactions, runs: config.Runs, state: config.State, worker: config.Worker, creationSessions: config.CreationSessions, consumers: config.Consumers, executions: config.Executions, chats: config.Chats, wakers: config.Wakers, resources: config.Resources, pythonExecutable: config.PythonExecutable, connectors: config.Connectors, dataDirectory: config.DataDirectory, projects: config.Projects, imCall: config.IMCall, imValidate: config.IMValidate}
}

// runnerLifecycle 拥有调度循环、在途派发和取消生命周期。
type runnerLifecycle struct {
	// ctx 是服务生命周期。
	ctx context.Context
	// cancel 停止接纳和后台推进。
	cancel context.CancelFunc
	// done 表示调度循环已退出。
	done chan struct{}
	// workers 等待所有外部派发退出。
	workers sync.WaitGroup
}

// Start 注册执行能力并启动有界公平调度，不在构造时执行背景工作。
func (s *Service) Start(ctx context.Context) error {
	if s.state == nil {
		return status_error.WorkflowCapabilityUnsupported.WithField("capability", "workflow_state")
	}
	if s.runs == nil {
		return status_error.WorkflowCapabilityUnsupported
	}
	if s.runner != nil {
		return status_error.WorkflowStateConflict
	}
	child, cancel := context.WithCancel(ctx)
	s.state.AgentBindings = s.AgentBindings
	s.runner = &runnerLifecycle{ctx: child, cancel: cancel, done: make(chan struct{})}
	go s.runLoop(s.runner)
	return nil
}

// Close 停止接纳、等待派发退出并释放能力装配，未终态帧保留供启动恢复。
func (s *Service) Close() {
	if s == nil || s.runner == nil {
		return
	}
	lifecycle := s.runner
	lifecycle.cancel()
	<-lifecycle.done
	lifecycle.workers.Wait()
	if err := s.runs.ReleaseLeases(context.Background(), s.state.Owner, time.Now().UnixMilli()); err != nil {
		logx.Errorf("release workflow leases failed: error=%v", err)
	}
	s.state.AgentBindings = nil
	s.runner = nil
}

func (s *Service) runLoop(lifecycle *runnerLifecycle) {
	defer close(lifecycle.done)
	ticker := time.NewTicker(time.Duration(constants.WorkflowRunnerTickMillis) * time.Millisecond)
	defer ticker.Stop()
	for {
		runs, err := s.runs.ListRunnable(lifecycle.ctx, 50)
		if err != nil && lifecycle.ctx.Err() == nil {
			logx.Errorf("scan workflows failed: error=%v", err)
		}
		for _, run := range runs {
			if lifecycle.ctx.Err() != nil {
				return
			}
			state, err := s.tick(lifecycle.ctx, run.ID)
			if err != nil {
				if !errors.Is(err, status_error.WorkflowFenceConflict) {
					logx.Errorf("advance workflow failed: run_id=%s error=%v", run.ID, err)
				}
				continue
			}
			// CommitRun 可将未知效果收敛为 cancelling；只确认运行行，复用 Tick 已核验的效果与许可。
			state.Run, err = s.runs.GetRun(lifecycle.ctx, run.ID)
			if err != nil {
				continue
			}
			if workflowstate.TerminalRun(state.Run.Status) {
				continue
			}
			if state.Run.Status == constants.WorkflowRunStatusCancelling {
				children, err := s.runs.ListJoinedChildren(lifecycle.ctx, state.Run.ID)
				if err == nil {
					for _, child := range children {
						if !workflowstate.TerminalRun(child.Status) {
							_ = s.CancelRun(lifecycle.ctx, child.ID)
						}
					}
				}
			}
			for _, effect := range state.Effects {
				if effect.Status != constants.WorkflowStepStatusPrepared && effect.Status != constants.WorkflowStepStatusRunning {
					continue
				}
				if effect.Boundary.Primitive == "askUser" {
					continue
				}
				blocked := false
				for _, operation := range state.Operations {
					if operation.EffectID == effect.ID && operation.Status == constants.WorkflowStepStatusWaiting {
						blocked = true
					}
				}
				if blocked && state.Run.Status != constants.WorkflowRunStatusCancelling && effect.Failure == nil {
					continue
				}
				if (effect.Boundary.Primitive == "worker" || effect.Boundary.Primitive == "action" || effect.Boundary.Primitive == "functionTask") && state.Run.Status != constants.WorkflowRunStatusCancelling && effect.Failure == nil {
					if err := s.reserveExternalSlot(lifecycle.ctx, state.Run.ID, effect.ID); err != nil {
						continue
					}
				}
				jobCtx, jobCancel, admitted := s.state.BeginEffect(lifecycle.ctx, effect.ID, state.Run.Status == constants.WorkflowRunStatusCancelling || effect.Failure != nil, constants.WorkflowGlobalConcurrency)
				if !admitted {
					continue
				}
				lifecycle.workers.Add(1)
				go func(jobCtx context.Context, jobCancel context.CancelFunc, run models.WorkflowRun, effect models.WorkflowEffect) {
					defer jobCancel()
					defer lifecycle.workers.Done()
					defer s.state.EndEffect(effect.ID)
					s.dispatchEffect(jobCtx, run, effect)
				}(jobCtx, jobCancel, state.Run, effect)
			}
		}
		select {
		case <-lifecycle.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
