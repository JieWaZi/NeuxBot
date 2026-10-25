package channels

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// reviewChannelChatFake 让成员和负责人 Chat 在验收测试中保持忙碌。
type reviewChannelChatFake struct {
	// Service 补齐本测试不使用的 Chat 接口方法。
	chatservice.Service
	// summaries 保存按 Chat 标识返回的当前摘要。
	summaries map[string]chatservice.ChatSummary
}

// AppendParticipantMessages 让不触发汇总的验收用例接受成员报告投递接口。
func (f *reviewChannelChatFake) AppendParticipantMessages(
	context.Context,
	string,
	[]chatservice.ParticipantMessageInput,
) error {
	return nil
}

// GetChatSummary 返回测试指定的 Chat 活跃执行状态。
func (f *reviewChannelChatFake) GetChatSummary(
	_ context.Context,
	chatID string,
) (chatservice.ChatSummary, error) {
	return f.summaries[chatID], nil
}

// reviewChannelExecutionFake 补齐验收测试不应触发的执行服务接口。
type reviewChannelExecutionFake struct {
	// Service 补齐本测试不使用的执行接口方法。
	execution.Service
	// mutex 保护取消顺序和终态回调。
	mutex sync.Mutex
	// cancelled 记录停止全部工作时各 Chat 的取消顺序。
	cancelled []string
	// onCancel 在测试中模拟成员 Chat durable 终态后的任务监听回调。
	onCancel func(chatID string)
}

// PrepareChatGroup 补齐本验收测试不会调用的原子 Chat 组准备端口。
func (f *reviewChannelExecutionFake) PrepareChatGroup(
	context.Context,
	execution.PrepareChatGroupIntent,
) (execution.PreparedChatGroup, error) {
	return execution.PreparedChatGroup{}, status_error.BadRequest
}

// CancelAndWait 记录取消顺序并同步执行测试终态回调。
func (f *reviewChannelExecutionFake) CancelAndWait(
	_ context.Context,
	chatID string,
) error {
	f.mutex.Lock()
	f.cancelled = append(f.cancelled, chatID)
	callback := f.onCancel
	f.mutex.Unlock()
	if callback != nil {
		callback(chatID)
	}
	return nil
}

// cancelOrder 返回成员与主协调 Chat 的实际取消顺序。
func (f *reviewChannelExecutionFake) cancelOrder() []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return append([]string(nil), f.cancelled...)
}

func TestReviewChannelTaskKeepsHumanDecisionInAPIUseCase(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "review-channel-task.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})

	previousTransactions := global.TransactionControllerManager
	previousChannels := global.ChannelControllerManager
	previousThreads := global.ChannelThreadControllerManager
	previousTasks := global.ChannelTaskControllerManager
	previousWakers := global.WakerControllerManager
	previousChannelService := global.ChannelService
	previousExecutionService := global.ExecutionService
	t.Cleanup(func() {
		global.TransactionControllerManager = previousTransactions
		global.ChannelControllerManager = previousChannels
		global.ChannelThreadControllerManager = previousThreads
		global.ChannelTaskControllerManager = previousTasks
		global.WakerControllerManager = previousWakers
		global.ChannelService = previousChannelService
		global.ExecutionService = previousExecutionService
	})
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.ChannelControllerManager = controllers.NewChannelController(database.DB)
	global.ChannelThreadControllerManager = controllers.NewChannelThreadController(database.DB)
	global.ChannelTaskControllerManager = controllers.NewChannelTaskController(database.DB)
	global.WakerControllerManager = controllers.NewWakerController(database.DB)

	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	threadID := "22222222222222222222222222222222"
	leadID := "33333333333333333333333333333333"
	memberID := "44444444444444444444444444444444"
	leadChatID := "55555555555555555555555555555555"
	memberChatID := "66666666666666666666666666666666"
	for _, waker := range []models.Waker{
		{ID: leadID, Name: "验收负责人"},
		{ID: memberID, Name: "验收成员"},
	} {
		if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
			t.Fatalf("create waker %s: %v", waker.ID, err)
		}
	}
	if err := global.ChannelControllerManager.Create(ctx, models.Channel{
		ID:          channelID,
		Name:        "人工验收团队",
		LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := global.ChannelThreadControllerManager.Create(ctx, models.ChannelThread{
		ID:                 threadID,
		ChannelID:          channelID,
		Title:              "人工验收",
		LeadWakerID:        leadID,
		LeadChatID:         leadChatID,
		WorkspaceDirectory: "/tmp/workspace",
		MaxParallel:        3,
		MaxTasks:           12,
		MaxFailureAttempts: 2,
		ReadyAt:            apiTestTimestamp(1),
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: "77777777777777777777777777777777", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	chatFake := &reviewChannelChatFake{summaries: map[string]chatservice.ChatSummary{
		leadChatID:   {Chat: models.Chat{ID: leadChatID, ActiveExecutionID: "lead-busy"}},
		memberChatID: {Chat: models.Chat{ID: memberChatID, ActiveExecutionID: "member-busy"}},
	}}
	executionDependency := &reviewChannelExecutionFake{}
	service, err := channelservice.New(channelservice.Config{
		Channels:     global.ChannelControllerManager,
		Threads:      global.ChannelThreadControllerManager,
		Tasks:        global.ChannelTaskControllerManager,
		Transactions: global.TransactionControllerManager,
		Wakers:       global.WakerControllerManager,
		Chats:        chatFake,
		Executions:   executionDependency,
		ChatGroups:   executionDependency,
	})
	if err != nil {
		t.Fatalf("create channel service: %v", err)
	}
	global.ChannelService = service

	rejectedID := "88888888888888888888888888888888"
	approvedID := "99999999999999999999999999999999"
	for _, task := range []models.ChannelTask{
		{
			ID: rejectedID, ThreadID: threadID, AssigneeWakerID: memberID,
			Subject: "驳回任务", Description: "等待驳回", Status: constants.ChannelTaskStatusInReview,
			RequiresHumanReview: true, CurrentFence: 1,
		},
		{
			ID: approvedID, ThreadID: threadID, AssigneeWakerID: memberID,
			Subject: "通过任务", Description: "等待通过", Status: constants.ChannelTaskStatusInReview,
			RequiresHumanReview: true, CurrentFence: 1, Result: "已交付",
		},
	} {
		if err := global.ChannelTaskControllerManager.Create(ctx, task, nil); err != nil {
			t.Fatalf("create review task %s: %v", task.ID, err)
		}
	}

	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	reject := &ReviewChannelTask{
		ChannelID: channelID,
		ThreadID:  threadID,
		TaskID:    rejectedID,
		Body: ReviewChannelTaskRequest{
			Feedback: "请补充回归验证",
		},
	}
	if _, err := reject.Output(ginContext); err != nil {
		t.Fatalf("reject review Output() error = %v", err)
	}
	rejected, err := global.ChannelTaskControllerManager.Get(ctx, rejectedID)
	if err != nil {
		t.Fatalf("get rejected task: %v", err)
	}
	followUps, err := global.ChannelTaskControllerManager.ListPendingFollowUps(ctx, rejectedID)
	if err != nil {
		t.Fatalf("list rejected task follow-ups: %v", err)
	}
	if rejected.Status != constants.ChannelTaskStatusPending || len(followUps) != 1 ||
		followUps[0].Text != "请补充回归验证" {
		t.Fatalf("rejected task = %+v, follow-ups = %+v", rejected, followUps)
	}

	approve := &ReviewChannelTask{
		ChannelID: channelID,
		ThreadID:  threadID,
		TaskID:    approvedID,
		Body:      ReviewChannelTaskRequest{Approved: true},
	}
	if _, err := approve.Output(ginContext); err != nil {
		t.Fatalf("approve review Output() error = %v", err)
	}
	approved, err := global.ChannelTaskControllerManager.Get(ctx, approvedID)
	if err != nil {
		t.Fatalf("get approved task: %v", err)
	}
	notifications, err := global.ChannelTaskControllerManager.ListPendingNotifications(ctx, threadID, 10)
	if err != nil {
		t.Fatalf("list approved task notifications: %v", err)
	}
	if approved.Status != constants.ChannelTaskStatusCompleted || len(notifications) != 1 ||
		notifications[0].TaskID != approvedID {
		t.Fatalf("approved task = %+v, notifications = %+v", approved, notifications)
	}

	humanCancelledID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := global.ChannelTaskControllerManager.Create(ctx, models.ChannelTask{
		ID:                  humanCancelledID,
		ThreadID:            threadID,
		AssigneeWakerID:     memberID,
		Subject:             "用户取消验收",
		Description:         "验证人类可以取消 in_review",
		Status:              constants.ChannelTaskStatusInReview,
		RequiresHumanReview: true,
		ChangedPaths:        []string{},
	}, nil); err != nil {
		t.Fatalf("create cancellable review task: %v", err)
	}
	stopTask := &StopChannelTask{ChannelID: channelID, ThreadID: threadID, TaskID: humanCancelledID}
	if _, err := stopTask.Output(ginContext); err != nil {
		t.Fatalf("human stop in-review task: %v", err)
	}
	humanCancelled, err := global.ChannelTaskControllerManager.Get(ctx, humanCancelledID)
	if err != nil {
		t.Fatalf("get human-cancelled review task: %v", err)
	}
	if humanCancelled.Status != constants.ChannelTaskStatusCancelled {
		t.Fatalf("human cancelled review task status = %s", humanCancelled.Status)
	}

	activeTaskID := "acacacacacacacacacacacacacacacac"
	if err := global.ChannelTaskControllerManager.Create(ctx, models.ChannelTask{
		ID: activeTaskID, ThreadID: threadID, AssigneeWakerID: memberID,
		Subject: "停止运行任务", Description: "成员终态不得再唤醒负责人",
		Status: constants.ChannelTaskStatusInProgress, CurrentFence: 1, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create active task: %v", err)
	}
	executionDependency.onCancel = func(chatID string) {
		if chatID != memberChatID {
			return
		}
		current, getErr := global.ChannelTaskControllerManager.Get(ctx, activeTaskID)
		if getErr != nil || current.Status != constants.ChannelTaskStatusInProgress {
			return
		}
		_ = global.ChannelTaskControllerManager.CreateNotification(ctx, models.ChannelLeadNotification{
			ID: "adadadadadadadadadadadadadadadad", ThreadID: threadID, TaskID: activeTaskID,
			AttemptID: "aeaeaeaeaeaeaeaeaeaeaeaeaeaeaeae", Status: constants.ChannelNotificationStatusPending,
			Payload: `{}`,
		})
	}
	if _, err := (&StopChannelWork{ChannelID: channelID, ThreadID: threadID}).Output(ginContext); err != nil {
		t.Fatalf("stop all current team work: %v", err)
	}
	stoppedTask, err := global.ChannelTaskControllerManager.Get(ctx, activeTaskID)
	if err != nil || stoppedTask.Status != constants.ChannelTaskStatusCancelled {
		t.Fatalf("stopped active task = %+v, error = %v", stoppedTask, err)
	}
	remainingNotifications, err := global.ChannelTaskControllerManager.ListPendingNotifications(ctx, threadID, 10)
	if err != nil || len(remainingNotifications) != 0 {
		t.Fatalf("notifications after stop = %+v, error = %v", remainingNotifications, err)
	}
	order := executionDependency.cancelOrder()
	if len(order) != 3 || order[0] == leadChatID || order[len(order)-1] != leadChatID {
		t.Fatalf("stop cancel order = %v, want member chats before coordinator %s", order, leadChatID)
	}

}
