package channel

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
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
}

// CancelAndWait 记录取消，并为指定 Chat 返回可重试错误。
func (f *stopExecutionFake) CancelAndWait(_ context.Context, chatID string) error {
	f.calls = append(f.calls, chatID)
	if chatID == f.failChatID {
		return status_error.ChatHasActiveExecution
	}
	return nil
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
		watchers: make(map[string]struct{}), scheduleLocks: make(map[string]*threadScheduleLock),
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
	failingExecution := &stopExecutionFake{failChatID: memberChatID}
	first := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		executions: failingExecution,
	}
	if err := first.StopWork(ctx, channelID, threadID); !errors.Is(err, status_error.ChatHasActiveExecution) {
		t.Fatalf("StopWork() error = %v, want ChatHasActiveExecution", err)
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
	recovered := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		chats: &stopChatFake{}, executions: recoveryExecution,
		watchers: make(map[string]struct{}), scheduleLocks: make(map[string]*threadScheduleLock),
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
	if len(recoveryExecution.calls) != 2 || recoveryExecution.calls[0] != memberChatID ||
		recoveryExecution.calls[1] != leadChatID {
		t.Fatalf("startup cancellation order = %v", recoveryExecution.calls)
	}
}
