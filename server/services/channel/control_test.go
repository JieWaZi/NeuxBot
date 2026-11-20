package channel

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// stopExecutionFake 模拟 Stop All 在指定成员 Chat 上暂时无法完成取消。
type stopExecutionFake struct {
	// Service 补齐本测试不使用的执行接口方法。
	execution.Service
	// failChatID 是本轮返回取消失败的成员 Chat。
	failChatID string
	// calls 保存本轮实际尝试取消的全部 Chat。
	calls []string
	// beforeWait 检查等待原生执行前已经登记的停止意图。
	beforeWait func(string)
	// associatedError 模拟重复进入逐回合工作流取消时的数据库故障。
	associatedError error
	// stoppedChat 和 stoppedSubmission 保存本次停止捕获的准确归属。
	stoppedChat       string
	stoppedSubmission string
}

func (f *stopExecutionFake) IsSubmissionStopped(chatID, submissionID string) bool {
	return chatID == f.stoppedChat && submissionID == f.stoppedSubmission
}

func (f *stopExecutionFake) CancelSubmissionAndWait(_ context.Context, chatID, submissionID string) error {
	f.stoppedChat, f.stoppedSubmission = chatID, submissionID
	return nil
}

// CancelAndWait 模拟撤销关联工作流并等待原生收尾。
func (f *stopExecutionFake) CancelAndWait(ctx context.Context, chatID string) error {
	if f.associatedError != nil {
		return f.associatedError
	}
	return f.StopNativeExecutions([]string{chatID})(ctx)
}

// StopNativeExecutions 记录被取消的全部执行，并可暂停原生收尾。
func (f *stopExecutionFake) StopNativeExecutions(chatIDs []string) func(context.Context) error {
	f.calls = append(f.calls, chatIDs...)
	return func(context.Context) error {
		var failure error
		for _, chatID := range chatIDs {
			if f.beforeWait != nil {
				f.beforeWait(chatID)
			}
			if chatID == f.failChatID {
				failure = status_error.ChatHasActiveExecution
			}
		}
		return failure
	}
}

// stopChatFake 补齐启动恢复不需要实际读取的 Chat 接口。
type stopChatFake struct {
	// Service 补齐本测试不使用的 Chat 接口方法。
	chatservice.Service
	// provisionalThreadID 保存启动恢复要求删除的未就绪团队对话。
	provisionalThreadID string
	// provisionalID 保存启动恢复使用的首发补偿标识。
	provisionalID string
	// provisionalChatIDs 保存启动恢复覆盖的全部计划成员 Chat。
	provisionalChatIDs []string
}

// ReadInitialExecution 模拟首发计划尚未建立负责人 Chat。
func (f *stopChatFake) ReadInitialExecution(context.Context, string) (chatservice.InitialExecutionState, error) {
	return chatservice.InitialExecutionState{}, status_error.ChatNotFound
}

// DeleteProvisionalTeam 记录启动恢复的 Team 聚合补偿范围。
func (f *stopChatFake) DeleteProvisionalTeam(
	_ context.Context,
	threadID string,
	provisioningID string,
	chatIDs []string,
) error {
	f.provisionalThreadID = threadID
	f.provisionalID = provisioningID
	f.provisionalChatIDs = append([]string(nil), chatIDs...)
	return nil
}

func TestStartRollsBackPersistedInitializingThread(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "initializing-recovery.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create initializing recovery database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("open initializing recovery database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx := context.Background()
	threadID := "10101010101010101010101010101010"
	provisioningID := "20202020202020202020202020202020"
	leadID := "30303030303030303030303030303030"
	memberID := "40404040404040404040404040404040"
	leadChatID := "50505050505050505050505050505050"
	memberChatID := "60606060606060606060606060606060"
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: "70707070707070707070707070707070", Title: "未完成首发",
		LeadWakerID: leadID, LeadChatID: leadChatID, ProvisioningID: provisioningID,
		MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "claude", PermissionLevel: constants.PermissionLevelDefault, Position: 1},
	}); err != nil {
		t.Fatalf("create initializing thread: %v", err)
	}
	taskID := "80808080808080808080808080808080"
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: taskID, ThreadID: threadID, AssigneeWakerID: memberID,
		Subject: "未完成任务", Description: "随首发锚点一起补偿",
		Status: constants.ChannelTaskStatusPending, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create initializing task: %v", err)
	}
	chatFake := &stopChatFake{}
	executionFake := &stopExecutionFake{}
	recovered := &service{
		threads: threads, tasks: tasks, transactions: transactions,
		chats: chatFake, executions: executionFake,
		watchers:           make(map[string]struct{}),
		interactionCursors: make(map[string]int64), capabilities: channeltool.NewRegistry(),
	}
	if err := recovered.Start(ctx); err != nil {
		t.Fatalf("recover initializing thread: %v", err)
	}
	recovered.Close()
	if _, err := threads.Get(ctx, threadID); !hasStatusErrorCode(
		err,
		status_error.ChannelThreadNotFound.Code(),
	) {
		t.Fatalf("initializing thread still exists: %v", err)
	}
	if _, err := tasks.Get(ctx, taskID); !hasStatusErrorCode(
		err,
		status_error.ChannelTaskNotFound.Code(),
	) {
		t.Fatalf("initializing task still exists: %v", err)
	}
	if chatFake.provisionalThreadID != threadID || chatFake.provisionalID != provisioningID ||
		len(chatFake.provisionalChatIDs) != 2 || chatFake.provisionalChatIDs[0] != leadChatID ||
		chatFake.provisionalChatIDs[1] != memberChatID {
		t.Fatalf("provisional cleanup = thread:%s provisioning:%s chats:%v",
			chatFake.provisionalThreadID, chatFake.provisionalID, chatFake.provisionalChatIDs)
	}
	if len(executionFake.calls) != 2 || executionFake.calls[0] != leadChatID ||
		executionFake.calls[1] != memberChatID {
		t.Fatalf("initializing execution cancellation order = %v", executionFake.calls)
	}
}

func TestStopWorkKeepsOtherThreadsInTheTeamWritableWhileCanceling(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, executions := newDelegationDispatchService(t)
	sibling := thread
	sibling.ID, sibling.LeadChatID = "other-thread", "other-lead-chat"
	if err := sut.threads.Create(ctx, sibling, []models.ChannelThreadMember{
		{ThreadID: sibling.ID, WakerID: "lead", ChatID: sibling.LeadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: sibling.ID, WakerID: "member", ChatID: "other-member-chat", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	chats.summaries[sibling.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: sibling.LeadChatID}}

	paused, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	sut.executions = &stopExecutionFake{Service: executions, beforeWait: func(chatID string) {
		if chatID == thread.LeadChatID {
			close(paused)
			<-resume
		}
	}}
	var stopErr error
	go func() {
		stopErr = sut.StopWork(ctx, thread.ChannelID, thread.ID)
		close(done)
	}()
	defer func() {
		close(resume)
		<-done
		if stopErr != nil {
			t.Error(stopErr)
		}
	}()
	select {
	case <-paused:
	case <-done:
		t.Fatalf("stop finished before native cancellation was paused: %v", stopErr)
	case <-time.After(2 * time.Second):
		t.Fatal("native cancellation was not reached")
	}

	inputCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	accepted, err := sut.sendMessage(inputCtx, SendMessageInput{
		ThreadID: sibling.ID,
		Input:    execution.WorkInput{Content: "同团队其他对话继续工作"},
	})
	if err != nil || accepted.Execution == nil || accepted.Execution.ChatID != sibling.LeadChatID {
		t.Fatalf("another thread could not start while stop was waiting: %+v %v", accepted, err)
	}
}

func TestStopWorkDrainsMemberExecutionsWhenWorkflowStopFails(t *testing.T) {
	ctx := t.Context()
	sut, thread, _, _ := newDelegationDispatchService(t)
	members, err := sut.threads.ListMembers(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	stops := 0
	sut.stopChatWorkflows = func(_ context.Context, _ string, _ []string, _ int64) error {
		stops++
		return status_error.DatabaseBusy
	}
	executions := &stopExecutionFake{associatedError: status_error.DatabaseError}
	sut.executions = executions
	if err := sut.StopWork(ctx, thread.ChannelID, thread.ID); !errors.Is(err, status_error.DatabaseBusy) {
		t.Fatalf("stop did not return the bulk workflow error: %v", err)
	}
	if stops != 1 || len(executions.calls) != len(members) {
		t.Fatalf("stop did not drain all member executions after one bulk revoke: stops=%d drained=%v", stops, executions.calls)
	}
	for _, member := range members {
		if !slices.Contains(executions.calls, member.ChatID) {
			t.Errorf("member execution was not drained: %s", member.ChatID)
		}
	}
	current, err := sut.threads.Get(ctx, thread.ID)
	if err != nil || current.CancelingAt == nil {
		t.Fatalf("failed workflow stop did not preserve recovery intent: %+v %v", current, err)
	}
}

func TestStopWorkKeepsCancelingIntentUntilStartupRecoveryCompletes(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "stop-work.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create stop work database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("open stop work database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	threadID := "22222222222222222222222222222222"
	leadID := "33333333333333333333333333333333"
	memberID := "44444444444444444444444444444444"
	leadChatID := "55555555555555555555555555555555"
	memberChatID := "66666666666666666666666666666666"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "停止恢复团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "claude", PermissionLevel: constants.PermissionLevelDefault, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	readyAt := int64(1)
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "停止恢复", LeadWakerID: leadID,
		LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/team", MaxParallel: 3,
		MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "claude", PermissionLevel: constants.PermissionLevelDefault, Position: 1},
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	taskID := "77777777777777777777777777777777"
	attemptID := "88888888888888888888888888888888"
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: taskID, ThreadID: threadID, AssigneeWakerID: memberID,
		Subject: "正在执行", Description: "验证取消恢复", Status: constants.ChannelTaskStatusInProgress,
		CurrentFence: 1, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{
		ID: attemptID, TaskID: taskID, ChatID: memberChatID,
		ExecutionID: "99999999999999999999999999999999", AssigneeWakerID: memberID,
		Fence: 1, Reason: constants.ChannelAttemptReasonInitial, Status: constants.ExecutionStatusRunning,
	}); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	runs := controllers.NewWorkflowRunController(database.DB)
	store := controllers.NewWorkflowController(database.DB)
	consumers := controllers.NewWorkflowConsumerController(database.DB)
	// 原生注册表已无这些回合，持久 Run 仍然活跃，并可能保留崩溃前的租约。
	leaseUntil := time.Now().Add(time.Hour).UnixMilli()
	workflowRuns := []models.WorkflowRun{
		{ID: "lead-workflow", SourceThreadID: threadID, SourceChatID: leadChatID, SourceExecutionID: "stale-lead-exec", Status: constants.WorkflowRunStatusWaiting, Version: 2, LeaseOwner: "old-core", LeaseUntil: leaseUntil},
		{ID: "member-workflow", SourceThreadID: threadID, SourceChatID: memberChatID, SourceExecutionID: "stale-member-exec", Status: constants.WorkflowRunStatusRunning, Version: 4},
		{ID: "independent-workbench", SourceThreadID: threadID, SourceChatID: leadChatID, SourceExecutionID: "stale-lead-exec", SourceWorkbench: true, Status: constants.WorkflowRunStatusWaiting},
		{ID: "foreign-thread", SourceThreadID: "other-thread", SourceChatID: leadChatID, SourceExecutionID: "other-exec", Status: constants.WorkflowRunStatusWaiting},
		{ID: "foreign-chat", SourceThreadID: threadID, SourceChatID: "other-chat", SourceExecutionID: "foreign-exec", Status: constants.WorkflowRunStatusWaiting},
		{ID: "terminal-workflow", SourceThreadID: threadID, SourceChatID: leadChatID, SourceExecutionID: "ended-exec", Status: constants.WorkflowRunStatusSucceeded},
	}
	if err := database.DB.Create(&workflowRuns).Error; err != nil {
		t.Fatal(err)
	}
	human := models.WorkflowHumanRequest{ID: "awaiting-answer", RunID: "lead-workflow", Status: constants.WorkflowStepStatusWaiting, DeadlineAt: leaseUntil}
	if err := database.DB.Create(&human).Error; err != nil {
		t.Fatal(err)
	}
	selectedThreadID, selectedChatID := "selection-without-run", "selection-lead-chat"
	if err := threads.Create(ctx, models.ChannelThread{
		ID: selectedThreadID, ChannelID: channelID, Title: "已选工作流", LeadWakerID: leadID,
		LeadChatID: selectedChatID, WorkspaceDirectory: "/tmp/team", MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{{ThreadID: selectedThreadID, WakerID: leadID, ChatID: selectedChatID, IsLead: true}}); err != nil {
		t.Fatal(err)
	}
	for _, lease := range []models.WorkflowConsumerLease{
		{ID: "stale-lead-exec", ChatID: leadChatID, ThreadID: threadID, DeadlineAt: leaseUntil},
		{ID: "stale-member-exec", ChatID: memberChatID, ThreadID: threadID, DeadlineAt: leaseUntil},
		{ID: "pending-member-exec", ChatID: memberChatID, ThreadID: threadID, DeadlineAt: leaseUntil},
		{ID: "other-exec", ChatID: leadChatID, ThreadID: "other-thread", DeadlineAt: leaseUntil},
		{ID: "selected-without-run", ChatID: selectedChatID, ThreadID: selectedThreadID, DeadlineAt: leaseUntil,
			Selection: &workinput.WorkflowReference{ID: "definition", RevisionID: "revision", Name: "准备启动"}},
	} {
		if err := consumers.CreateConsumer(ctx, lease); err != nil {
			t.Fatal(err)
		}
	}
	workflows := workflow.New(workflow.Config{Runs: runs, Store: store, State: workflowstate.New(store, runs, transactions), Consumers: consumers, Transactions: transactions, ChannelThreads: threads})
	failingExecution := &stopExecutionFake{failChatID: memberChatID, beforeWait: func(chatID string) {
		if chatID != memberChatID {
			return
		}
		for _, id := range []string{"lead-workflow", "member-workflow"} {
			run, err := runs.GetRun(ctx, id)
			if err != nil || run.Status != constants.WorkflowRunStatusCancelling {
				t.Fatalf("native wait started before linked workflow cancellation: %+v %v", run, err)
			}
		}
	}}
	first := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		executions: failingExecution, stopChatWorkflows: workflows.StopTeamWorkflowTurns,
	}
	if err := first.StopWork(ctx, channelID, threadID); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("StopWork() error = %v, want ChatHasActiveExecution", err)
	}
	first.executions = &stopExecutionFake{failChatID: selectedChatID}
	if err := first.StopWork(ctx, channelID, selectedThreadID); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("StopWork() with pending selection error = %v", err)
	}
	interrupted, err := threads.Get(ctx, threadID)
	if err != nil || interrupted.CancelingAt == nil {
		t.Fatalf("interrupted stop intent = %+v, error = %v", interrupted, err)
	}
	stillRunning, err := tasks.Get(ctx, taskID)
	if err != nil || stillRunning.Status != constants.ChannelTaskStatusInProgress {
		t.Fatalf("task was declared cancelled before runtime exit: %+v, error = %v", stillRunning, err)
	}
	reserved, err := threads.ReserveTaskSlot(ctx, threadID)
	if err != nil || reserved {
		t.Fatalf("canceling thread accepted a new task slot: reserved=%t error=%v", reserved, err)
	}
	recoveryExecution := &stopExecutionFake{}
	workflows.RestoreChatRunIndex(ctx)
	recovered := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		chats: &stopChatFake{}, executions: recoveryExecution,
		stopChatWorkflows:  workflows.StopTeamWorkflowTurns,
		watchers:           make(map[string]struct{}),
		interactionCursors: make(map[string]int64), capabilities: channeltool.NewRegistry(),
	}
	if err := recovered.Start(ctx); err != nil {
		t.Fatalf("recover canceling thread: %v", err)
	}
	recovered.Close()
	finished, err := threads.Get(ctx, threadID)
	if err != nil || finished.CancelingAt != nil {
		t.Fatalf("recovered stop intent = %+v, error = %v", finished, err)
	}
	cancelledTask, err := tasks.Get(ctx, taskID)
	if err != nil || cancelledTask.Status != constants.ChannelTaskStatusCancelled {
		t.Fatalf("recovered task = %+v, error = %v", cancelledTask, err)
	}
	attempt, err := tasks.GetCurrentAttempt(ctx, taskID, 1)
	if err != nil || attempt.Status != constants.ExecutionStatusCanceled {
		t.Fatalf("recovered attempt = %+v, error = %v", attempt, err)
	}
	memberIndex, leadIndex := slices.Index(recoveryExecution.calls, memberChatID), slices.Index(recoveryExecution.calls, leadChatID)
	if len(recoveryExecution.calls) != 3 || memberIndex < 0 || leadIndex <= memberIndex || !slices.Contains(recoveryExecution.calls, selectedChatID) {
		t.Fatalf("startup cancellation order = %v", recoveryExecution.calls)
	}
	recoveryExecution.calls = nil
	if err := recovered.finishStopWork(ctx, interrupted); err != nil || len(recoveryExecution.calls) != 0 {
		t.Fatalf("stale stop attempted to cancel current chat executions: calls=%v error=%v", recoveryExecution.calls, err)
	}
	for _, before := range workflowRuns {
		run, err := runs.GetRun(ctx, before.ID)
		wantStatus, wantVersion := before.Status, before.Version
		if before.ID == "lead-workflow" || before.ID == "member-workflow" {
			wantStatus, wantVersion = constants.WorkflowRunStatusCancelling, before.Version+1
		}
		if err != nil || run.Status != wantStatus || run.Version != wantVersion || run.LeaseOwner != before.LeaseOwner {
			t.Fatalf("unexpected stop target or changed lease: %+v %v", run, err)
		}
	}
	for _, id := range []string{"stale-lead-exec", "stale-member-exec", "pending-member-exec", "other-exec", "selected-without-run"} {
		lease, err := consumers.GetConsumer(ctx, id)
		if err != nil || (lease.StoppedAt == nil) != (id == "other-exec") {
			t.Fatalf("unexpected revoked authorization for %s: %+v %v", id, lease, err)
		}
	}
	if err := runs.ReleaseLeases(ctx, "old-core", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lead-workflow", "member-workflow"} {
		if err := workflows.Tick(ctx, id); err != nil {
			t.Fatalf("finalize recovered workflow cancellation: %v", err)
		}
		state, err := runs.LoadRunState(ctx, id)
		if err != nil || state.Run.Status != constants.WorkflowRunStatusCancelled || state.Run.CompletedAt == nil {
			t.Fatalf("runner did not finalize cancellation: %+v %v", state.Run, err)
		}
		if id == human.RunID && (len(state.Humans) != 1 || state.Humans[0].Status != constants.WorkflowStepStatusCancelled) {
			t.Fatalf("stopped workflow still requests a business answer: %+v", state.Humans)
		}
	}
	newRun := models.WorkflowRun{ID: "later-workflow", SourceThreadID: threadID, SourceChatID: memberChatID, SourceExecutionID: "new-exec", Status: constants.WorkflowRunStatusQueued}
	if err := database.DB.Create(&newRun).Error; err != nil {
		t.Fatal(err)
	}
	if err := workflows.StopTeamWorkflowTurns(ctx, threadID, []string{memberChatID}, *interrupted.CancelingAt); err != nil {
		t.Fatal(err)
	}
	actual, err := runs.GetRun(ctx, newRun.ID)
	if err != nil || actual.Status != constants.WorkflowRunStatusQueued {
		t.Fatalf("stale stop changed later workflow: %+v %v", actual, err)
	}
}

type pausedLeadStart struct {
	// postTurnExecutionFake 补齐与准备阶段无关的执行接口。
	*postTurnExecutionFake
	// started 标记负责人接纳已经进入可取消的准备。
	started chan struct{}
	// canceled 记录停止向负责人发出的原生取消。
	canceled chan struct{}
}

func (f *pausedLeadStart) Start(ctx context.Context, _ execution.WorkIntent) (execution.AcceptedExecution, error) {
	close(f.started)
	select {
	case <-f.canceled:
		return execution.AcceptedExecution{}, status_error.OperationCanceled
	case <-ctx.Done():
		return execution.AcceptedExecution{}, ctx.Err()
	}
}

// StopNativeExecutions 在任何工作流停止或原生收尾前取消负责人。
func (f *pausedLeadStart) StopNativeExecutions(chatIDs []string) func(context.Context) error {
	if slices.Contains(chatIDs, "lead-chat") {
		close(f.canceled)
	}
	return func(context.Context) error { return nil }
}

func TestStopWorkSignalsAlreadyPreparingLeadInput(t *testing.T) {
	svc, thread, _, original := newDelegationDispatchService(t)
	f := &pausedLeadStart{original, make(chan struct{}), make(chan struct{})}
	svc.executions = f
	sendCtx, cancelSend := context.WithCancel(t.Context())
	defer cancelSend()
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		_, _ = svc.sendMessage(sendCtx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "准备中的负责人输入"}})
	}()
	defer func() { cancelSend(); <-sent }()
	select {
	case <-f.started:
	case <-time.After(2 * time.Second):
		t.Fatal("lead input did not enter preparation")
	}
	stopCtx, cancelStop := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancelStop()
	err := svc.StopWork(stopCtx, thread.ChannelID, thread.ID)
	current, readErr := svc.threads.Get(t.Context(), thread.ID)
	select {
	case <-f.canceled:
		if err != nil {
			t.Fatalf("stop after cancellation: %v", err)
		}
	default:
		t.Fatalf("stop returned %v without sending cancellation; persisted stop=%v, read err=%v", err, current.CancelingAt != nil, readErr)
	}
}

func TestStopWorkRejectsTheOldDispatchSnapshotAfterCompletion(t *testing.T) {
	ctx := t.Context()
	svc, thread, _, executions := newDelegationDispatchService(t)
	svc.executions = &stopExecutionFake{Service: executions}
	oldDispatch := context.WithValue(ctx, threadWorkSnapshotKey{}, threadWorkSnapshot{thread.ID, thread.StopGeneration})
	if err := svc.StopWork(ctx, thread.ChannelID, thread.ID); err != nil {
		t.Fatal(err)
	}
	current, err := svc.threads.Get(ctx, thread.ID)
	if err != nil || current.CancelingAt != nil || current.StopGeneration != thread.StopGeneration+1 {
		t.Fatalf("stop did not complete its original generation: %+v, %v", current, err)
	}
	if _, release, err := svc.AcquireChatInput(oldDispatch, thread.LeadChatID, false); !errors.Is(err, status_error.ChannelTaskStateConflict) {
		if release != nil {
			release()
		}
		t.Fatalf("old dispatch passed the completed stop: %v", err)
	}
	_, release, err := svc.AcquireChatInput(ctx, thread.LeadChatID, true)
	if err != nil {
		t.Fatalf("new input after stop was blocked: %v", err)
	}
	release()
}

func TestStopFirstTeamBeforeSendLeavesNoProvisionalThread(t *testing.T) {
	ctx := t.Context()
	svc, thread, _, executions := newDelegationDispatchService(t)
	svc.executions = &stopExecutionFake{Service: executions}
	const submissionID = "stopped-first-input"
	if err := svc.StopFirstWork(ctx, thread.ChannelID, submissionID); err != nil {
		t.Fatal(err)
	}
	input := CreateThreadInput{ChannelID: thread.ChannelID, ClientRequestID: submissionID,
		Input: execution.WorkInput{Content: "原首发输入", SubmissionID: submissionID}}
	if _, err := svc.createThread(ctx, input); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("late team first send: %v", err)
	}
	if _, exists, err := svc.threads.GetByClientRequestID(ctx, thread.ChannelID, submissionID); err != nil || exists {
		t.Fatalf("late team first send provisioned a thread: %t, %v", exists, err)
	}
}

func TestStopWorkBeforeSendOnlyRejectsOriginalSubmission(t *testing.T) {
	ctx := t.Context()
	svc, thread, _, executions := newDelegationDispatchService(t)
	stop := &stopExecutionFake{Service: executions}
	svc.executions = stop
	if err := svc.StopWorkForSubmission(ctx, thread.ChannelID, thread.ID, "stopped-input"); err != nil {
		t.Fatal(err)
	}
	input := SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "原输入", SubmissionID: "stopped-input"}}
	if _, err := svc.sendMessage(ctx, input); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("late stopped input: %v", err)
	}
	if _, exists, err := svc.threads.GetLeadInput(ctx, LeadInputID(thread.ID, input.Input.SubmissionID)); err != nil || exists {
		t.Fatalf("late stopped input entered queue: %t, %v", exists, err)
	}
	input.Input.SubmissionID = "other-input"
	if result, err := svc.sendMessage(ctx, input); err != nil || result.QueuedInput == nil {
		t.Fatalf("next input could not enter queue: %+v, %v", result, err)
	}
	if err := svc.StopWorkForSubmission(ctx, thread.ChannelID, thread.ID, "stopped-input"); err != nil {
		t.Fatal(err)
	}
	current, err := svc.threads.Get(ctx, thread.ID)
	if err != nil || current.StopGeneration != thread.StopGeneration || len(stop.calls) != 0 {
		t.Fatalf("submission stop stopped the whole team: %+v, chats=%v, err=%v", current, stop.calls, err)
	}
}

func TestStopTeamSubmissionCancelsOnlyItsLeadQueueEntry(t *testing.T) {
	ctx := t.Context()
	svc, thread, _, executions := newDelegationDispatchService(t)
	stop := &stopExecutionFake{Service: executions}
	svc.executions = stop
	for _, submissionID := range []string{"stopped", "another"} {
		err := svc.threads.CreateLeadInput(ctx, models.ChannelLeadInput{
			ID: LeadInputID(thread.ID, submissionID), ThreadID: thread.ID,
			Status:  constants.ChannelInputStatusPending,
			Payload: execution.WorkInput{Content: submissionID, SubmissionID: submissionID},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StopWorkForSubmission(ctx, thread.ChannelID, thread.ID, "stopped"); err != nil {
		t.Fatal(err)
	}
	stopped, _, err := svc.threads.GetLeadInput(ctx, LeadInputID(thread.ID, "stopped"))
	if err != nil || stopped.Status != constants.ChannelInputStatusCancelled {
		t.Fatalf("target input = %+v, %v", stopped, err)
	}
	another, _, err := svc.threads.GetLeadInput(ctx, LeadInputID(thread.ID, "another"))
	if err != nil || another.Status != constants.ChannelInputStatusPending || len(stop.calls) != 0 {
		t.Fatalf("other input = %+v, stopped chats = %v, err = %v", another, stop.calls, err)
	}
}

func TestStopTeamSubmissionWithdrawsOnlyItsPendingFollowUp(t *testing.T) {
	ctx := t.Context()
	svc, thread, _, executions := newDelegationDispatchService(t)
	svc.executions = &stopExecutionFake{Service: executions}
	task := models.ChannelTask{ID: "parent-task", ThreadID: thread.ID, AssigneeWakerID: "member",
		Status: constants.ChannelTaskStatusInProgress}
	if err := svc.tasks.Create(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	for _, submissionID := range []string{"withdrawn", "another"} {
		if err := svc.tasks.CreateFollowUp(ctx, models.ChannelTaskFollowUp{
			ID: submissionID, TaskID: task.ID, Text: submissionID, Actor: constants.ChannelMessageActorUser,
			Payload: execution.WorkInput{Content: submissionID, SubmissionID: submissionID},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StopWorkForSubmission(ctx, thread.ChannelID, thread.ID, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	followUps, err := svc.tasks.ListPendingFollowUps(ctx, task.ID)
	if err != nil || len(followUps) != 1 || followUps[0].ID != "another" {
		t.Fatalf("pending follow-ups = %+v, %v", followUps, err)
	}
	task, err = svc.tasks.Get(ctx, task.ID)
	if err != nil || task.Status != constants.ChannelTaskStatusInProgress {
		t.Fatalf("original task = %+v, %v", task, err)
	}
}

func TestStopFirstTeamCancelsMentionBeforePublication(t *testing.T) {
	ctx := t.Context()
	svc, thread, _, executions := newDelegationDispatchService(t)
	svc.executions = &stopExecutionFake{Service: executions}
	submissionID := "initial-mention"
	thread.ID, thread.ReadyAt, thread.ClientRequestID = "unpublished-thread", nil, &submissionID
	thread.LeadChatID = initialLeadChatID(thread.ChannelID, requestidempotency.Identity{ID: submissionID})
	if err := svc.threads.Create(ctx, thread, []models.ChannelThreadMember{
		{ThreadID: thread.ID, WakerID: "lead", ChatID: thread.LeadChatID, IsLead: true},
		{ThreadID: thread.ID, WakerID: "member", ChatID: "unpublished-member-chat"},
	}); err != nil {
		t.Fatal(err)
	}
	task := models.ChannelTask{ID: "unpublished-task", ThreadID: thread.ID, AssigneeWakerID: "member",
		Status: constants.ChannelTaskStatusPending, DirectInput: &execution.WorkInput{Content: "原提及", SubmissionID: submissionID},
	}
	if err := svc.tasks.Create(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.StopFirstWork(ctx, thread.ChannelID, submissionID); err != nil {
		t.Fatal(err)
	}
	current, err := svc.tasks.Get(ctx, task.ID)
	if err != nil || current.Status != constants.ChannelTaskStatusCancelled {
		t.Fatalf("unpublished task = %+v, %v", current, err)
	}
}

func TestStopWorkRecoversWithoutRestartAfterTransientWorkflowFailure(t *testing.T) {
	ctx := t.Context()
	svc, thread, _, executions := newDelegationDispatchService(t)
	svc.executions = &stopExecutionFake{Service: executions}
	var attempts atomic.Int32
	svc.stopChatWorkflows = func(context.Context, string, []string, int64) error {
		if attempts.Add(1) == 1 {
			return status_error.DatabaseBusy
		}
		return nil
	}
	if err := svc.StopWork(ctx, thread.ChannelID, thread.ID); !errors.Is(err, status_error.DatabaseBusy) {
		t.Fatalf("initial failure = %v", err)
	}
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("background owner did not settle stop, attempts=%d", attempts.Load())
		case <-ticker.C:
			current, err := svc.threads.Get(ctx, thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.CancelingAt == nil {
				return
			}
		}
	}
}
