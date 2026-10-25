package schedule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// AdmissionCommand 提供所有入口共用的冻结机会接纳事实。
type AdmissionCommand struct {
	// IntegrationInvocation 固定最初集成调用；不能通过任务正文自报。
	IntegrationInvocation *models.IntegrationInvocation
	// AcquireAuthorization 仅在原子领取机会时取得短授权锁，网络与 Runtime 调用前释放。
	AcquireAuthorization func(context.Context) (func(), error)
	// Task 是在协调保护内读取和校验的当前任务。
	Task models.AutomaticTask
	// Source 区分定时、手动与本机 API 机会。
	Source constants.AutomaticRunSource
	// OccurrenceKey 标识本任务内唯一机会，重试必须保持不变。
	OccurrenceKey string
	// RequestID 是调用方提供或计划派生的稳定请求身份。
	RequestID string
	// Input 是伙伴或团队目标的本次附加要求。
	Input string
	// Args 是工作流本次 API 或手动提供的顶层参数。
	Args map[string]any
	// ScheduledAt 是定时机会原定执行的 Unix 毫秒时间。
	ScheduledAt *int64
	// NextRunAt 是本次接纳完成后应提交的计划游标。
	NextRunAt *int64
}

// Admit 为定时、手动和本机 API 复用同一幂等、冻结和派发流程。
func (s *Service) Admit(ctx context.Context, command AdmissionCommand) (RunView, error) {
	ctx, finish, err := s.begin(ctx)
	if err != nil {
		return RunView{}, err
	}
	defer finish()
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	task, source := command.Task, command.Source
	occurrence, requestID, input := command.OccurrenceKey, command.RequestID, command.Input
	callArgs, scheduledAt, next := command.Args, command.ScheduledAt, command.NextRunAt
	input = strings.TrimSpace(input)
	if strings.TrimSpace(requestID) == "" || requestID != strings.TrimSpace(requestID) || len(requestID) > constants.CreateRequestIDMaximumLength || !utf8.ValidString(input) || (task.WorkflowID == "" && utf8.RuneCountInString(input) > constants.AutomaticTaskInputMaximumRunes) || len(input) > constants.AutomaticTaskRequestMaximumBytes {
		return RunView{}, status_error.BadRequest
	}
	canonicalInput := input
	if task.WorkflowID != "" {
		if input != "" {
			return RunView{}, status_error.BadRequest
		}
		if callArgs == nil {
			callArgs = map[string]any{}
		}
		canonical, err := json.Marshal(callArgs)
		if err != nil || len(canonical) > constants.AutomaticTaskRequestMaximumBytes {
			return RunView{}, status_error.AutomaticTaskInputInvalid
		}
		// 规范化调用输入并独立持有值；仅幂等摘要使用 JSON 编码。
		if json.Unmarshal(canonical, &callArgs) != nil {
			return RunView{}, status_error.AutomaticTaskInputInvalid
		}
		canonicalInput = string(canonical)
	} else if callArgs != nil {
		return RunView{}, status_error.BadRequest
	}
	digest := sha256.Sum256([]byte(canonicalInput))
	hash := hex.EncodeToString(digest[:])
	run, exists, err := s.config.Tasks.FindRun(ctx, task.ID, occurrence)
	if err != nil {
		return RunView{}, err
	}
	if exists {
		if run.RequestHash != hash {
			return RunView{}, status_error.AutomaticTaskRequestConflict
		}
		if err := s.ReconcileRun(ctx, &run); err != nil {
			return RunView{}, err
		}
		if run.Admission == constants.AutomaticRunAdmissionAccepted {
			return s.ReadRun(ctx, run)
		}
		if run.Admission != constants.AutomaticRunAdmissionPending {
			return RunView{}, status_error.AutomaticTaskAdmissionRevoked
		}
		if run.ConfigurationRevision != task.ConfigurationVersion || !task.Enabled {
			if err := s.interrupt(ctx, &run); err != nil {
				return RunView{}, err
			}
			return RunView{}, status_error.AutomaticTaskAdmissionRevoked
		}
	}
	if task.Deleting || s.sealedWakers[task.WakerID] {
		return RunView{}, status_error.AutomaticTaskAdmissionRevoked
	}
	if !task.Enabled {
		return RunView{}, status_error.AutomaticTaskDisabled
	}
	if err := s.refreshSlot(ctx, task); err != nil {
		return RunView{}, err
	}
	task, err = s.config.Tasks.Get(ctx, task.ID)
	if err != nil {
		return RunView{}, err
	}
	if task.ActiveRunID != "" && task.ActiveRunID != run.ID {
		return RunView{}, status_error.AutomaticTaskBusy
	}
	now := s.config.Now()
	if source == constants.AutomaticRunSourceAPI && !exists && !s.rateAllowed(task.ID, now) {
		return RunView{}, status_error.AutomaticTaskRateLimited
	}
	if !exists {
		run = models.AutomaticRun{ID: util.New(), TaskID: task.ID, WakerID: task.WakerID, OccurrenceKey: occurrence, RequestHash: hash, Input: input,
			IntegrationInvocation: command.IntegrationInvocation, Source: source, Configuration: task.AutomaticTaskConfiguration, ConfigurationRevision: task.ConfigurationVersion,
			ExecutionID: util.New(), Admission: constants.AutomaticRunAdmissionPending, ScheduledAt: scheduledAt, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
		if run.Configuration.WorkflowID != "" {
			if s.config.Workflows == nil {
				return RunView{}, status_error.WorkflowCapabilityUnsupported
			}
			config := run.Configuration
			// 解析和校验同一个不可变版本，避免 current 在两次读取之间变化。
			revision, err := s.resolveWorkflowConfiguration(ctx, run.WakerID, &config)
			if err != nil {
				return RunView{}, err
			}
			args, err := workflowinput.Resolve(revision.Manifest.Meta.InputSchema, config.WorkflowInputBindings, callArgs)
			if err != nil {
				return RunView{}, err
			}
			if source == constants.AutomaticRunSourceAPI {
				if err := workflowfiles.ValidateAPIInputFiles(s.config.ManagedFilesDirectory, config.WorkflowID, task.ID, revision.Manifest.Meta.InputSchema, callArgs, invocationOwner(run.IntegrationInvocation)); err != nil {
					return RunView{}, err
				}
			}
			run.WorkflowCallArgs, run.WorkflowResolvedArgs = callArgs, args
			run.Input = ""
			// current 只在领取机会前解析一次，后续重试不能重新绑定版本。
			run.Configuration.WorkflowRevisionID = revision.ID
			run.Configuration.WorkflowRevisionPolicy = constants.WorkflowRevisionPolicyPinned
			run.ExecutionID = ""
		}
	}
	if task.ActiveRunID == "" {
		authorizationRelease := func() {}
		if command.IntegrationInvocation != nil {
			if command.AcquireAuthorization == nil {
				return RunView{}, status_error.IntegrationForbidden
			}
			authorizationRelease, err = command.AcquireAuthorization(ctx)
			if err != nil {
				return RunView{}, err
			}
			if authorizationRelease == nil {
				return RunView{}, status_error.IntegrationForbidden
			}
		}
		err = s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
			claimed, err := s.config.Tasks.ClaimRun(tx, task.ID, task.Revision, run.ID, next)
			if err != nil {
				return err
			}
			if !claimed {
				return status_error.AutomaticTaskBusy
			}
			if !exists {
				return s.config.Tasks.CreateRun(tx, run)
			}
			return nil
		})
		authorizationRelease()
		if err != nil {
			return RunView{}, err
		}
	}
	// 在持久领取后执行可用性检查，依赖后来失效会留下一条可修复的失败记录。
	if err := s.validateConfiguration(ctx, run.WakerID, &run.Configuration); err != nil {
		return RunView{}, s.failAdmission(ctx, &run, err)
	}
	if run.Configuration.WorkflowID != "" {
		if err := s.startWorkflow(ctx, &run); err != nil {
			if reconcileErr := s.ReconcileRun(context.WithoutCancel(ctx), &run); reconcileErr != nil {
				return RunView{}, reconcileErr
			}
			if run.Admission != constants.AutomaticRunAdmissionAccepted {
				if ctx.Err() != nil {
					_ = s.config.Tasks.ReleaseRun(context.WithoutCancel(ctx), task.ID, run.ID)
					return RunView{}, status_error.OperationCanceled
				}
				return RunView{}, s.failAdmission(ctx, &run, err)
			}
		}
		if source == constants.AutomaticRunSourceAPI && !exists {
			s.rates[task.ID] = append(s.rates[task.ID], now)
		}
		return s.ReadRun(ctx, run)
	}
	content := run.Configuration.Instruction
	if input != "" {
		content += "\n\n本次调用附加要求：\n" + input
	}
	var accepted execution.AcceptedExecution
	var startErr error
	if run.Configuration.ChannelID != "" {
		locked, release, lockErr := teamwork.Acquire(ctx, run.Configuration.ChannelID)
		if lockErr != nil {
			return RunView{}, lockErr
		}
		ctx = locked
		defer release()
		accepted, run.ThreadID, startErr = s.startTeam(ctx, run, content)
	} else {
		intent := execution.WorkIntent{
			AutomaticRunID: run.ID, ClientRequestID: run.ID, ExecutionID: run.ExecutionID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
			Chat:    execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: run.WakerID, ProjectID: run.Configuration.ProjectID, WorkingDirectory: run.Configuration.WorkingDirectory, HiddenFromWakerHistory: true}},
			Input:   execution.WorkInput{Actor: constants.AutomaticTaskActor, Content: content},
			Options: execution.WorkOptions{RuntimeID: run.Configuration.RuntimeID, AccountID: run.Configuration.AccountID, Model: run.Configuration.Model, Reasoning: run.Configuration.Reasoning, PermissionLevel: run.Configuration.PermissionLevel},
		}
		if run.Configuration.Knowledge != nil {
			if s.config.BindKnowledge == nil {
				startErr = status_error.KnowledgeUnsupported
			} else {
				intent, startErr = s.config.BindKnowledge(ctx, intent, *run.Configuration.Knowledge)
			}
		}
		if startErr == nil {
			accepted, startErr = s.config.Executions.Start(ctx, intent)
		}
	}
	if startErr != nil {
		// 首批事实可能已提交但响应失败。先以稳定身份对账，绝不创建第二个 Chat。
		if err := s.ReconcileRun(context.WithoutCancel(ctx), &run); err != nil {
			return RunView{}, err
		}
		if run.Admission != constants.AutomaticRunAdmissionAccepted {
			if ctx.Err() != nil {
				// 未知取消保留 pending，只有显式同键请求可以接续。
				_ = s.config.Tasks.ReleaseRun(context.WithoutCancel(ctx), task.ID, run.ID)
				return RunView{}, status_error.OperationCanceled
			}
			return RunView{}, s.failAdmission(ctx, &run, startErr)
		}
	} else {
		run.ChatID = accepted.Chat.ID
		run.WakerID = accepted.Chat.WakerID
		run.ExecutionID = accepted.Execution.ID
		run.Admission = constants.AutomaticRunAdmissionAccepted
		run.UpdatedAt = now.UnixMilli()
		if err := s.config.Tasks.UpdateRunAdmission(context.WithoutCancel(ctx), run); err != nil {
			return RunView{}, err
		}
	}
	if source == constants.AutomaticRunSourceAPI && !exists {
		s.rates[task.ID] = append(s.rates[task.ID], now)
	}
	return s.ReadRun(ctx, run)
}

// AuthenticateTask 为接纳、上传、状态和下载入口复用任务级密钥校验。
func (s *Service) AuthenticateTask(ctx context.Context, id, token string) (models.AutomaticTask, error) {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	task, err := s.config.Tasks.Get(ctx, id)
	// 即使任务不存在也完成固定长度比较，不通过查询错误短路泄漏存在性。
	valid := util.AutomaticTaskTokenMatches(token, task.TokenHash)
	if err != nil || !valid || task.Trigger != constants.AutomaticTaskTriggerAPI || task.Deleting || s.sealedWakers[task.WakerID] {
		return models.AutomaticTask{}, status_error.AutomaticTaskTokenInvalid
	}
	return task, nil
}

func candidateChatID(run models.AutomaticRun) string {
	return (requestidempotency.Identity{ID: run.ID}).ResourceID("chat", run.WakerID)
}

// ReconcileRun 按稳定派发身份补齐已提交收据，调用方不会重复创建执行。
func (s *Service) ReconcileRun(ctx context.Context, run *models.AutomaticRun) error {
	if run.Admission != constants.AutomaticRunAdmissionPending {
		return nil
	}
	if run.Configuration.WorkflowID != "" {
		return s.reconcileWorkflow(ctx, run)
	}
	if run.Configuration.ChannelID != "" {
		return s.reconcileTeam(ctx, run)
	}
	state, err := s.config.Chats.ReadInitialExecution(ctx, candidateChatID(*run))
	if util.HasStatusCode(err, status_error.ChatNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.CreateRequestID != run.ID || state.InitialExecution == nil || state.InitialExecution.ID != run.ExecutionID {
		return status_error.AutomaticTaskServiceInvalid
	}
	run.ChatID = state.Chat.ID
	run.Admission = constants.AutomaticRunAdmissionAccepted
	run.UpdatedAt = s.config.Now().UnixMilli()
	return s.config.Tasks.UpdateRunAdmission(ctx, *run)
}

func (s *Service) failAdmission(ctx context.Context, run *models.AutomaticRun, cause error) error {
	run.Admission = constants.AutomaticRunAdmissionFailed
	run.ErrorCode = util.ErrorCode(cause, status_error.AutomaticTaskServiceInvalid.Key())
	run.ChatID = ""
	run.ExecutionID = ""
	run.UpdatedAt = s.config.Now().UnixMilli()
	if err := s.config.Tasks.UpdateRunAdmission(ctx, *run); err != nil {
		return err
	}
	if err := s.config.Tasks.ReleaseRun(ctx, run.TaskID, run.ID); err != nil {
		return err
	}
	return cause
}

func (s *Service) interrupt(ctx context.Context, run *models.AutomaticRun) error {
	run.Admission = constants.AutomaticRunAdmissionInterrupted
	run.ErrorCode = status_error.AutomaticTaskAdmissionRevoked.Key()
	run.ChatID = ""
	run.ExecutionID = ""
	run.UpdatedAt = s.config.Now().UnixMilli()
	if err := s.config.Tasks.UpdateRunAdmission(ctx, *run); err != nil {
		return err
	}
	return s.config.Tasks.ReleaseRun(ctx, run.TaskID, run.ID)
}

func (s *Service) rateAllowed(taskID string, now time.Time) bool {
	values := s.rates[taskID]
	first := 0
	for first < len(values) && !values[first].After(now.Add(-time.Minute)) {
		first++
	}
	s.rates[taskID] = values[first:]
	return len(s.rates[taskID]) < constants.AutomaticTaskAPIRequestsPerMinute
}

// startTeam 复用团队首发的幂等、成员快照和负责人编排，不额外建立独立伙伴会话。
func (s *Service) startTeam(ctx context.Context, run models.AutomaticRun, content string) (execution.AcceptedExecution, string, error) {
	result, err := s.config.Teams.SubmitMessage(ctx, channelservice.SubmitMessageInput{Create: &channelservice.CreateThreadInput{AutomaticRunID: run.ID, ChannelID: run.Configuration.ChannelID, ClientRequestID: run.ID, ExecutionID: run.ExecutionID, ProjectID: run.Configuration.ProjectID, WorkingDirectory: run.Configuration.WorkingDirectory, MemberConfigs: teamMemberOverrides(run.Configuration), Input: execution.WorkInput{Actor: constants.AutomaticTaskActor, Content: content}}})
	if err != nil {
		return execution.AcceptedExecution{}, "", err
	}
	if result.Thread == nil || result.Thread.Thread.ReadyAt == nil {
		return execution.AcceptedExecution{}, "", status_error.AutomaticTaskServiceInvalid
	}
	thread := result.Thread.Thread
	state, err := s.config.Chats.ReadInitialExecution(ctx, thread.LeadChatID)
	if err != nil {
		return execution.AcceptedExecution{}, "", err
	}
	if state.InitialExecution == nil || state.InitialExecution.ID != run.ExecutionID {
		return execution.AcceptedExecution{}, "", status_error.AutomaticTaskServiceInvalid
	}
	return execution.AcceptedExecution{Chat: state.Chat, Execution: *state.InitialExecution}, thread.ID, nil
}

// reconcileTeam 在响应丢失或重启后通过团队首发身份寻找已就绪的权威会话。
func (s *Service) reconcileTeam(ctx context.Context, run *models.AutomaticRun) error {
	thread, exists, err := s.config.Threads.GetByClientRequestID(ctx, run.Configuration.ChannelID, run.ID)
	if err != nil || !exists || thread.ReadyAt == nil {
		return err
	}
	state, err := s.config.Chats.ReadInitialExecution(ctx, thread.LeadChatID)
	if err != nil {
		return err
	}
	if state.InitialExecution == nil || state.InitialExecution.ID != run.ExecutionID {
		return status_error.AutomaticTaskServiceInvalid
	}
	run.ThreadID = thread.ID
	run.ChatID = thread.LeadChatID
	run.WakerID = state.Chat.WakerID
	run.Admission = constants.AutomaticRunAdmissionAccepted
	run.UpdatedAt = s.config.Now().UnixMilli()
	return s.config.Tasks.UpdateRunAdmission(ctx, *run)
}

// teamRunView 保留团队全部成员的忙碌、审批和验收状态，负责人结束规划不代表运行完成。
func (s *Service) teamRunView(ctx context.Context, view RunView, states map[string]chat.InitialExecutionState) (RunView, error) {
	locked, release, lockErr := teamwork.Acquire(ctx, view.Configuration.ChannelID)
	if lockErr != nil {
		return view, lockErr
	}
	ctx = locked
	defer release()
	// 等待锁期间另一条查询可能已经封闭该运行，必须优先使用已保存结果。
	stored, err := s.config.Tasks.GetRun(ctx, view.ID)
	if err != nil {
		return view, err
	}
	if stored.FinalState != constants.AutomaticRunStateUnspecified {
		view.State, view.CompletedAt = stored.FinalState, stored.FinalCompletedAt
		return view, nil
	}
	members, err := s.config.Threads.ListMembers(ctx, view.ThreadID)
	if err != nil {
		return view, err
	}
	waiting := constants.AutomaticRunStateUnspecified
	for _, member := range members {
		state, err := s.ReadExecutionSnapshot(ctx, member.ChatID, states)
		if err != nil {
			return view, err
		}
		if state.Chat.ActiveExecutionID != "" {
			if state.Chat.WaitingApproval {
				waiting = constants.AutomaticRunStateWaitingApproval
			} else if state.Chat.WaitingElicitation && waiting == constants.AutomaticRunStateUnspecified {
				waiting = constants.AutomaticRunStateWaitingInput
			}
		}
		if state.InitialExecution != nil && state.InitialExecution.CompletedAt != nil && (view.CompletedAt == nil || *state.InitialExecution.CompletedAt > *view.CompletedAt) {
			view.CompletedAt = state.InitialExecution.CompletedAt
		}
		if state.Chat.ActiveExecutionID == "" && view.CompletedAt != nil && state.Chat.UpdatedAt > *view.CompletedAt {
			completedAt := state.Chat.UpdatedAt
			view.CompletedAt = &completedAt
		}
	}
	tasks, err := s.config.TeamTasks.ListByThread(ctx, view.ThreadID)
	if err != nil {
		return view, err
	}
	failed := false
	cancelled := false
	for _, task := range tasks {
		if task.FinishedAt != nil && (view.CompletedAt == nil || *task.FinishedAt > *view.CompletedAt) {
			view.CompletedAt = task.FinishedAt
		}
		if task.Status == constants.ChannelTaskStatusInReview && waiting == constants.AutomaticRunStateUnspecified {
			waiting = constants.AutomaticRunStateWaitingInput
		}
		if task.Status == constants.ChannelTaskStatusFailed {
			failed = true
		}
		if task.Status == constants.ChannelTaskStatusCancelled {
			cancelled = true
		}
	}
	active, err := s.config.Threads.CountActiveWork(ctx, view.ThreadID)
	if err != nil {
		return view, err
	}
	// 成员结束与负责人汇总之间仍有待交付通知，不能提前释放该次运行槽。
	summarizing, err := s.config.TeamTasks.HasLeadProcessingNotifications(ctx, view.ThreadID, "")
	if err != nil {
		return view, err
	}
	if waiting != constants.AutomaticRunStateUnspecified {
		view.State = waiting
		view.CompletedAt = nil
	} else if active > 0 || summarizing {
		view.State = constants.AutomaticRunStateRunning
		view.CompletedAt = nil
	} else if failed {
		view.State = constants.AutomaticRunStateFailed
	} else if cancelled && view.State != constants.AutomaticRunStateFailed {
		view.State = constants.AutomaticRunStateCanceled
	}
	if !Unfinished(view.State) {
		if err := s.config.Tasks.CloseTeamRun(ctx, view.ID, view.State, view.CompletedAt); err != nil {
			return view, err
		}
	}
	return view, nil
}

// FreezeTeamRun 在内核接纳新的手动工作之前封闭已经整体结束的团队自动运行。
// 调用方持有团队保护；本方法不获取调度全局 gate，避免与自动首发形成锁环。
func (s *Service) FreezeTeamRun(ctx context.Context, threadID string) error {
	run, exists, err := s.config.Tasks.FindOpenTeamRun(ctx, threadID)
	if err != nil || !exists {
		return err
	}
	view, err := s.ReadRun(ctx, run)
	if err != nil {
		return err
	}
	if !Unfinished(view.State) {
		return s.config.Tasks.ReleaseRun(ctx, run.TaskID, run.ID)
	}
	return nil
}

func candidateWorkflowRunID(run models.AutomaticRun) string {
	return requestidempotency.Identity{ID: run.ID}.ResourceID("workflow-run", run.Configuration.WorkflowID)
}

func (s *Service) startWorkflow(ctx context.Context, run *models.AutomaticRun) error {
	accepted, err := s.config.Workflows.StartRun(ctx, workflow.StartRunCommand{AutomaticRunID: run.ID, DefinitionID: run.Configuration.WorkflowID, RequestID: run.ID, RevisionID: run.Configuration.WorkflowRevisionID, Args: run.WorkflowResolvedArgs, Defaults: workflowDefaults(run.Configuration), TriggerSource: string(run.Source), InputFileOwnerKeyID: invocationOwner(run.IntegrationInvocation), InputFileTaskID: run.TaskID})
	if err != nil {
		return err
	}
	run.WorkflowRunID = accepted.ID
	run.ExecutionID = ""
	run.Admission = constants.AutomaticRunAdmissionAccepted
	run.UpdatedAt = s.config.Now().UnixMilli()
	return s.config.Tasks.UpdateRunAdmission(context.WithoutCancel(ctx), *run)
}

func (s *Service) reconcileWorkflow(ctx context.Context, run *models.AutomaticRun) error {
	if s.config.Workflows == nil {
		return status_error.WorkflowCapabilityUnsupported
	}
	value, err := s.config.WorkflowRuns.GetRun(ctx, candidateWorkflowRunID(*run))
	if util.HasStatusCode(err, status_error.WorkflowNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if value.DefinitionID != run.Configuration.WorkflowID || value.RevisionID != run.Configuration.WorkflowRevisionID {
		return status_error.AutomaticTaskServiceInvalid
	}
	run.WorkflowRunID = value.ID
	run.ExecutionID = ""
	run.Admission = constants.AutomaticRunAdmissionAccepted
	run.UpdatedAt = s.config.Now().UnixMilli()
	return s.config.Tasks.UpdateRunAdmission(ctx, *run)
}

func (s *Service) workflowRunView(ctx context.Context, run models.AutomaticRun) (RunView, error) {
	view := RunView{AutomaticRun: run}
	value, err := s.config.WorkflowRuns.GetRun(ctx, run.WorkflowRunID)
	if util.HasStatusCode(err, status_error.WorkflowNotFound) {
		view.State = constants.AutomaticRunStateInterrupted
		view.ErrorCode = status_error.WorkflowNotFound.Key()
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.CompletedAt = value.CompletedAt
	if value.Failure != nil {
		if data, ok := value.Failure.Data.(map[string]any); ok {
			view.ErrorCode, _ = data["code"].(string)
		}
	}
	switch value.Status {
	case constants.WorkflowRunStatusQueued:
		view.State = constants.AutomaticRunStatePending
	case constants.WorkflowRunStatusRunning, constants.WorkflowRunStatusCancelling, constants.WorkflowRunStatusRecovering:
		view.State = constants.AutomaticRunStateRunning
	case constants.WorkflowRunStatusWaiting:
		operation, err := s.waitingForOperation(ctx, value.ID)
		if err != nil {
			return view, err
		}
		view.State = constants.AutomaticRunStateWaitingInput
		if operation {
			view.State = constants.AutomaticRunStateWaitingApproval
		}
	case constants.WorkflowRunStatusSucceeded:
		view.State = constants.AutomaticRunStateCompleted
	case constants.WorkflowRunStatusFailed:
		view.State = constants.AutomaticRunStateFailed
	case constants.WorkflowRunStatusCancelled:
		view.State = constants.AutomaticRunStateCanceled
	default:
		return view, status_error.AutomaticTaskServiceInvalid
	}
	return view, nil
}

// RunView 组合不可变机会记录与独立执行或团队整体运行的展示状态。
type RunView struct {
	// AutomaticRun 是该机会的持久身份、来源与冻结配置。
	models.AutomaticRun
	// State 是首次独立执行或团队整体运行的状态，结束后的手动续聊不改变它。
	State constants.AutomaticRunState `json:"state"`
	// CompletedAt 是首次独立执行或团队整体运行结束的 Unix 毫秒时间。
	CompletedAt *int64 `json:"completedAt"`
}

// ReadExecutionSnapshot 在一次查询内共享权威日志读取，避免负责人和成员提醒重复重放同一 Chat。
func (s *Service) ReadExecutionSnapshot(ctx context.Context, chatID string, states map[string]chat.InitialExecutionState) (chat.InitialExecutionState, error) {
	if state, exists := states[chatID]; exists {
		return state, nil
	}
	state, err := s.config.Chats.ReadInitialExecution(ctx, chatID)
	if err == nil {
		states[chatID] = state
	}
	return state, err
}

// ReadRun 从持久机会与首次执行事实派生运行状态，后续手动续聊不改写结果。
func (s *Service) ReadRun(ctx context.Context, run models.AutomaticRun) (RunView, error) {
	return s.ReadRunWithSnapshots(ctx, run, map[string]chat.InitialExecutionState{})
}

// ReadRunWithSnapshots 在一次查询内复用 Chat 快照并投影独立执行、团队和工作流状态。
func (s *Service) ReadRunWithSnapshots(ctx context.Context, run models.AutomaticRun, states map[string]chat.InitialExecutionState) (RunView, error) {
	view := RunView{AutomaticRun: run}
	switch run.Admission {
	case constants.AutomaticRunAdmissionPending:
		view.State = constants.AutomaticRunStatePending
	case constants.AutomaticRunAdmissionInterrupted:
		view.State = constants.AutomaticRunStateInterrupted
	case constants.AutomaticRunAdmissionSkipped:
		view.State = constants.AutomaticRunStateSkipped
	case constants.AutomaticRunAdmissionFailed:
		view.State = constants.AutomaticRunStateFailed
	case constants.AutomaticRunAdmissionAccepted:
		if run.WorkflowRunID != "" {
			return s.workflowRunView(ctx, run)
		}
		state, err := s.ReadExecutionSnapshot(ctx, run.ChatID, states)
		if err != nil {
			// 用户从 Chat 页面独立删除历史时保留运行归属和明确不可用状态。
			if util.HasStatusCode(err, status_error.ChatNotFound) {
				view.State = constants.AutomaticRunStateInterrupted
				view.ErrorCode = status_error.ChatNotFound.Key()
				return view, nil
			}
			return view, err
		}
		initial := state.InitialExecution
		if initial == nil || initial.ID != run.ExecutionID {
			return view, status_error.AutomaticTaskServiceInvalid
		}
		view.State = constants.AutomaticRunState(initial.Status)
		view.CompletedAt = initial.CompletedAt
		view.ErrorCode = initial.ErrorCode
		if state.Chat.ActiveExecutionID == run.ExecutionID {
			if state.Chat.WaitingApproval {
				view.State = constants.AutomaticRunStateWaitingApproval
			} else if state.Chat.WaitingElicitation {
				view.State = constants.AutomaticRunStateWaitingInput
			}
		}
	default:
		return view, status_error.AutomaticTaskServiceInvalid
	}
	if run.ThreadID != "" && run.FinalState != constants.AutomaticRunStateUnspecified {
		view.State = run.FinalState
		view.CompletedAt = run.FinalCompletedAt
		return view, nil
	}
	if run.ThreadID != "" && view.State != constants.AutomaticRunStateInterrupted {
		return s.teamRunView(ctx, view, states)
	}
	return view, nil
}

// Unfinished 判断机会是否仍占用运行槽，包括业务输入、工具审批和待对账状态。
func Unfinished(state constants.AutomaticRunState) bool {
	return state == constants.AutomaticRunStatePending || state == constants.AutomaticRunStateRunning || state == constants.AutomaticRunStateWaitingApproval || state == constants.AutomaticRunStateWaitingInput
}

func (s *Service) refreshSlot(ctx context.Context, task models.AutomaticTask) error {
	if task.ActiveRunID == "" {
		return nil
	}
	run, err := s.config.Tasks.GetRun(ctx, task.ActiveRunID)
	if err != nil {
		return err
	}
	if err := s.ReconcileRun(ctx, &run); err != nil {
		return err
	}
	view, err := s.ReadRun(ctx, run)
	if err != nil {
		return err
	}
	if !Unfinished(view.State) {
		return s.config.Tasks.ReleaseRun(ctx, task.ID, run.ID)
	}
	return nil
}

// waitingForOperation 综合宿主操作许可与 Worker 的真实授权状态。
func (s *Service) waitingForOperation(ctx context.Context, runID string) (bool, error) {
	operations, err := s.config.WorkflowRuns.ListOperations(ctx, runID)
	if err != nil {
		return false, err
	}
	for _, operation := range operations {
		if operation.Status == constants.WorkflowStepStatusWaiting {
			return true, nil
		}
	}
	workers, err := s.config.WorkflowRuns.ListActiveWorkers(ctx, runID)
	if err != nil {
		return false, err
	}
	for _, worker := range workers {
		summary, err := s.config.Chats.GetChatSummary(ctx, worker.ChatID)
		if err != nil {
			return false, err
		}
		if summary.Chat.ActiveExecutionID == worker.ExecutionID && len(summary.Chat.PendingApprovals) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// IntegrationTask 只为完成集成 Key 认证的入口读取可发布任务；不代替任何凭据验证。
func (s *Service) IntegrationTask(ctx context.Context, id string, invoke bool) (models.AutomaticTask, error) {
	ctx, release := s.AcquireMutation(ctx)
	defer release()
	task, err := s.config.Tasks.Get(ctx, id)
	if err != nil || task.Trigger != constants.AutomaticTaskTriggerAPI || task.Deleting || s.sealedWakers[task.WakerID] || (invoke && !task.Enabled) {
		return models.AutomaticTask{}, status_error.AutomaticTaskNotFound
	}
	return task, nil
}

func invocationOwner(invocation *models.IntegrationInvocation) string {
	if invocation == nil {
		return ""
	}
	return invocation.Identity.KeyID
}
