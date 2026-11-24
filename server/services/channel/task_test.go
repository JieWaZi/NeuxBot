package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// testTimestamp 返回团队服务测试模型使用的可选毫秒时间。
func testTimestamp(value int64) *int64 { return &value }

// notificationAttemptExists 判断结果通知批次是否包含指定任务尝试。
func notificationAttemptExists(
	notifications []models.ChannelLeadNotification,
	attemptID string,
) bool {
	for _, notification := range notifications {
		if notification.AttemptID == attemptID {
			return true
		}
	}
	return false
}

// createChannelTestWakers 为团队服务测试写入最小伙伴资料。
func createChannelTestWakers(
	t *testing.T,
	ctx context.Context,
	wakers controllers.WakerController,
	wakerIDs ...string,
) {
	t.Helper()
	for index, wakerID := range wakerIDs {
		if err := wakers.Create(ctx, models.Waker{
			ID:   wakerID,
			Name: fmt.Sprintf("团队测试伙伴-%d-%s", index, wakerID[:4]),
		}); err != nil {
			t.Fatalf("create test waker %s: %v", wakerID, err)
		}
	}
}

// postTurnChatFake 只实现负责人终态监听和成员空闲查询，其余 Chat 能力不参与本测试。
type postTurnChatFake struct {
	// Service 补齐本测试不关心的聊天服务接口方法。
	chatservice.Service
	// mutex 保护后台监听和测试线程共享的订阅状态。
	mutex sync.Mutex
	// summaries 保存测试中各 Chat 的当前有界摘要。
	summaries map[string]chatservice.ChatSummary
	// executions 提供任务工具读取的已接纳执行事实。
	executions map[string]models.Execution
	// leadChatID 是负责人主协调 Chat 标识。
	leadChatID string
	// leadTerminal 是负责人规划回合的持久终态事件。
	leadTerminal chatservice.PublicEvent
	// subscribeFailures 按 Chat 记录订阅成功前需要模拟的失败次数。
	subscribeFailures map[string]int
	// closeSubscriptions 标记需要在建立后立即断流的 Chat 订阅。
	closeSubscriptions map[string]bool
	// terminalEvents 按 Chat 提供可从 durable backlog 重放的执行终态。
	terminalEvents map[string]chatservice.PublicEvent
	// participantMessages 保存投递给负责人 Chat 的成员结果报告。
	participantMessages []chatservice.ParticipantMessageInput
}

// AppendParticipantMessages 记录即将进入负责人上下文的成员结果报告。
func (f *postTurnChatFake) AppendParticipantMessages(
	_ context.Context,
	_ string,
	messages []chatservice.ParticipantMessageInput,
) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	existing := make(map[string]struct{}, len(f.participantMessages))
	for _, message := range f.participantMessages {
		existing[message.ID] = struct{}{}
	}
	for _, message := range messages {
		if _, exists := existing[message.ID]; exists {
			continue
		}
		f.participantMessages = append(f.participantMessages, message)
		existing[message.ID] = struct{}{}
	}
	return nil
}

// GetChatSummary 返回测试配置的 Chat 当前摘要。
func (f *postTurnChatFake) GetChatSummary(
	_ context.Context,
	chatID string,
) (chatservice.ChatSummary, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.summaries[chatID], nil
}

// ReadExecution 返回指定测试执行的服务端接纳事实。
func (f *postTurnChatFake) ReadExecution(_ context.Context, chatID, executionID string) (models.Execution, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	execution := f.executions[executionID]
	execution.ID, execution.ChatID = executionID, chatID
	return execution, nil
}

// ReadSubmission 寻找测试已提交的原输入收据。
func (f *postTurnChatFake) ReadSubmission(_ context.Context, chatID, submissionID string) (*models.Execution, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	for _, execution := range f.executions {
		if execution.ChatID == chatID && execution.SubmissionID == submissionID {
			return &execution, nil
		}
	}
	return nil, nil
}

// ReadExecutionMessages 返回本轮完整终态消息，其他回合不参与补偿。
func (f *postTurnChatFake) ReadExecutionMessages(_ context.Context, chatID, executionID string) ([]messageview.Message, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	event := f.terminalEvents[chatID]
	if chatID == f.leadChatID {
		event = f.leadTerminal
	}
	if event.Message == nil || event.Message.ExecutionID != executionID {
		return nil, nil
	}
	return []messageview.Message{*event.Message}, nil
}

// SubscribeEvents 为负责人返回终态 backlog，为成员监听返回等待关闭的空事件流。
func (f *postTurnChatFake) SubscribeEvents(
	ctx context.Context,
	chatID string,
	_ int64,
) (chatservice.Subscription, error) {
	f.mutex.Lock()
	if f.subscribeFailures[chatID] > 0 {
		f.subscribeFailures[chatID]--
		f.mutex.Unlock()
		return chatservice.Subscription{}, status_error.DatabaseError
	}
	events := make(chan chatservice.PublicEvent)
	if f.closeSubscriptions[chatID] {
		close(events)
		f.mutex.Unlock()
		return chatservice.Subscription{Events: events, Close: func() {}}, nil
	}
	if chatID == f.leadChatID {
		summary := f.summaries[chatID]
		summary.Chat.ActiveExecutionID = ""
		f.summaries[chatID] = summary
		f.mutex.Unlock()
		return chatservice.Subscription{
			Backlog: []chatservice.PublicEvent{f.leadTerminal},
			Events:  events,
			Close:   func() {},
		}, nil
	}
	if terminal, exists := f.terminalEvents[chatID]; exists {
		f.mutex.Unlock()
		return chatservice.Subscription{
			Backlog: []chatservice.PublicEvent{terminal},
			Events:  events,
			Close:   func() {},
		}, nil
	}
	f.mutex.Unlock()
	go func() {
		<-ctx.Done()
		close(events)
	}()
	return chatservice.Subscription{Events: events, Close: func() {}}, nil
}

// SubscribeExecutionEvents 以原有替身的订阅故障和终态事实提供窄生命周期事件。
func (f *postTurnChatFake) SubscribeExecutionEvents(ctx context.Context, chatID, executionID string) (chatservice.Subscription, error) {
	subscription, err := f.SubscribeEvents(ctx, chatID, 9005)
	if err != nil {
		return subscription, err
	}
	for index, event := range subscription.Backlog {
		if event.Message != nil {
			subscription.Backlog[index] = chatservice.PublicEvent{ExecutionID: event.Message.ExecutionID, ExecutionStatus: event.Message.Status, ErrorCode: event.Message.ErrorCode, ErrorMessage: event.Message.ErrorMessage}
		}
	}
	return subscription, nil
}

// postTurnExecutionFake 记录负责人回合结束后实际派发到成员 Chat 的工作。
type postTurnExecutionFake struct {
	// Service 补齐本测试不关心的执行服务接口方法。
	execution.Service
	// mutex 保护跨 watcher goroutine 写入的工作意图。
	mutex sync.Mutex
	// starts 保存测试期间接纳的成员工作意图。
	starts []execution.WorkIntent
	// startError 是后续执行接纳需要返回的可选错误。
	startError error
	// canceledChats 保存 watcher 耗尽补偿请求停止的 Chat。
	// canceledExecutions 记录精确取消捕获的执行标识。
	canceledExecutions []string
	canceledChats      []string
	// cancelError 是停止并等待执行退出时返回的可选错误。
	cancelError error
}

func (f *postTurnExecutionFake) IsSubmissionStopped(string, string) bool { return false }

// transientTransactionController 在指定次数内拒绝事务，然后交给真实事务边界。
type transientTransactionController struct {
	// TransactionController 执行重试成功后的真实数据库事务。
	controllers.TransactionController
	// mutex 保护后台终态监听更新的调用计数。
	mutex sync.Mutex
	// remainingFailures 是还需要模拟的事务失败次数。
	remainingFailures int
	// calls 是测试期间收到的事务调用总数。
	calls int
}

// Transaction 在短暂失败用尽后执行真实事务。
func (c *transientTransactionController) Transaction(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	c.mutex.Lock()
	c.calls++
	if c.remainingFailures > 0 {
		c.remainingFailures--
		c.mutex.Unlock()
		return status_error.DatabaseError
	}
	c.mutex.Unlock()
	return c.TransactionController.Transaction(ctx, fn)
}

// callCount 返回终态提交为恢复短暂失败发起的事务数。
func (c *transientTransactionController) callCount() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.calls
}

// currentAttemptFailureTasks 只覆盖当前尝试查询，验证启动恢复不会吞掉数据库故障。
type currentAttemptFailureTasks struct {
	// ChannelTaskController 提供启动恢复的其他真实任务操作。
	controllers.ChannelTaskController
}

// GetCurrentAttempt 模拟不是“尝试不存在”的持久化失败。
func (c currentAttemptFailureTasks) GetCurrentAttempt(
	context.Context,
	string,
	int,
) (models.ChannelTaskAttempt, error) {
	return models.ChannelTaskAttempt{}, status_error.DatabaseError
}

func TestAttemptWatcherRetriesSubscriptionAndStartupPreservesAttemptReadError(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "attempt-watcher-retry.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	wakers := controllers.NewWakerController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx := context.Background()
	channelID := "10101010101010101010101010101010"
	threadID := "20202020202020202020202020202020"
	leadID := "30303030303030303030303030303030"
	memberID := "40404040404040404040404040404040"
	leadChatID := "50505050505050505050505050505050"
	memberChatID := "60606060606060606060606060606060"
	createChannelTestWakers(t, ctx, wakers, leadID, memberID)
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "监听恢复团队", LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	readyAt := int64(1)
	if err := threads.Create(ctx, models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "恢复成员终态", LeadWakerID: leadID,
		LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/workspace", MaxParallel: 3,
		MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: "70707070707070707070707070707070", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	taskID := "80808080808080808080808080808080"
	attemptID := "90909090909090909090909090909090"
	executionID := "abababababababababababababababab"
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: taskID, ThreadID: threadID, AssigneeWakerID: memberID, Subject: "终态恢复",
		Description: "订阅短暂失败后继续提交终态", Status: constants.ChannelTaskStatusInProgress,
		CurrentFence: 1, ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{
		ID: attemptID, TaskID: taskID, ChatID: memberChatID, ExecutionID: executionID,
		AssigneeWakerID: memberID, Fence: 1, Reason: constants.ChannelAttemptReasonInitial,
	}); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	chatFake := &postTurnChatFake{
		summaries: map[string]chatservice.ChatSummary{
			leadChatID:   {Chat: models.Chat{ID: leadChatID}},
			memberChatID: {Chat: models.Chat{ID: memberChatID}},
		},
		subscribeFailures: map[string]int{memberChatID: 1},
		terminalEvents: map[string]chatservice.PublicEvent{memberChatID: {
			Message: &messageview.Message{ExecutionID: executionID, Status: constants.ExecutionStatusCompleted, Content: "已恢复"},
		}},
	}
	service := &service{
		channels: channels, threads: threads, tasks: currentAttemptFailureTasks{ChannelTaskController: tasks},
		transactions: transactions, wakers: wakers, chats: chatFake, executions: &postTurnExecutionFake{},
		watchers: make(map[string]struct{}), capabilities: channeltool.NewRegistry(),
	}
	if err := service.Start(ctx); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("Start() attempt read error = %v, want DatabaseError", err)
	}
	service.tasks = tasks
	if err := service.Start(ctx); err != nil {
		t.Fatalf("Start() retry watcher error = %v", err)
	}
	t.Cleanup(service.Close)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := tasks.Get(ctx, taskID)
		if getErr == nil && current.Status == constants.ChannelTaskStatusCompleted {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	current, getErr := tasks.Get(ctx, taskID)
	t.Fatalf("task after subscription retry = %+v, error = %v", current, getErr)
}

func TestAttemptWatcherExhaustionCancelsExecutionAndFailsCurrentFence(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "attempt-watcher-exhaustion.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx, cancel := context.WithCancel(context.Background())
	channelID := "11111111111111111111111111111111"
	threadID := "22222222222222222222222222222222"
	leadID := "33333333333333333333333333333333"
	leadChatID := "44444444444444444444444444444444"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "监听耗尽团队", LeadWakerID: leadID,
	}, []models.ChannelMember{{
		ChannelID: channelID, WakerID: leadID, RuntimeID: "codex",
		PermissionLevel: constants.PermissionLevelAuto,
	}}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	readyAt := int64(1)
	thread := models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "监听耗尽", LeadWakerID: leadID,
		LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/watcher-exhaustion",
		MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}
	if err := threads.Create(ctx, thread, []models.ChannelThreadMember{{
		ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true,
		RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto,
	}}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	attempt := models.ChannelTaskAttempt{
		ID: "55555555555555555555555555555555", TaskID: "66666666666666666666666666666666",
		ChatID: leadChatID, ExecutionID: "77777777777777777777777777777777",
		AssigneeWakerID: leadID, Fence: 1, Reason: constants.ChannelAttemptReasonInitial,
		Status: constants.ExecutionStatusRunning,
	}
	if err := tasks.Create(ctx, models.ChannelTask{
		ID: attempt.TaskID, ThreadID: threadID, AssigneeWakerID: leadID,
		Subject: "需要补偿", Description: "订阅持续失败后进入可见终态。",
		Status: constants.ChannelTaskStatusInProgress, CurrentFence: attempt.Fence,
		ChangedPaths: []string{},
	}, nil); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := tasks.CreateAttempt(ctx, attempt); err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	chatFake := &postTurnChatFake{
		summaries:          map[string]chatservice.ChatSummary{leadChatID: {Chat: models.Chat{ID: leadChatID}}},
		closeSubscriptions: map[string]bool{leadChatID: true},
	}
	executionFake := &postTurnExecutionFake{}
	service := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		chats: chatFake, executions: executionFake, lifecycleContext: ctx, lifecycleCancel: cancel,
		watchers:     make(map[string]struct{}),
		capabilities: channeltool.NewRegistry(),
		watcherPolicy: deliveryWatcherPolicy{
			maximumFailures: 2,
			initialDelay:    time.Millisecond,
			maximumDelay:    time.Millisecond,
		},
	}
	t.Cleanup(service.Close)
	service.watchAttempt(attempt, threadID, "")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := tasks.Get(ctx, attempt.TaskID)
		if getErr == nil && current.Status == constants.ChannelTaskStatusFailed {
			if executionFake.cancelCount() != 1 {
				t.Fatalf("watcher cancellation count = %d", executionFake.cancelCount())
			}
			events, listErr := threads.ListEvents(ctx, threadID, "", 20)
			if listErr != nil {
				t.Fatalf("list recovery events: %v", listErr)
			}
			for _, event := range events {
				if event.Type == constants.ChannelEventTaskRecoveryFailed {
					return
				}
			}
			t.Fatalf("recovery event missing: %+v", events)
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, getErr := tasks.Get(ctx, attempt.TaskID)
	t.Fatalf("task after watcher exhaustion = %+v, error = %v", current, getErr)
}

func TestLeadWatcherExhaustionConsumesDurableInputWithoutDuplicatingMessage(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "lead-watcher-exhaustion.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx, cancel := context.WithCancel(context.Background())
	channelID := "88888888888888888888888888888888"
	threadID := "99999999999999999999999999999999"
	leadID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	leadChatID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	executionID := "cccccccccccccccccccccccccccccccc"
	inputID := "dddddddddddddddddddddddddddddddd"
	if err := channels.Create(ctx, models.Channel{
		ID: channelID, Name: "负责人监听耗尽团队", LeadWakerID: leadID,
	}, []models.ChannelMember{{
		ChannelID: channelID, WakerID: leadID, RuntimeID: "codex",
		PermissionLevel: constants.PermissionLevelAuto,
	}}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	readyAt := int64(1)
	thread := models.ChannelThread{
		ID: threadID, ChannelID: channelID, Title: "负责人监听耗尽", LeadWakerID: leadID,
		LeadChatID: leadChatID, WorkspaceDirectory: "/tmp/lead-watcher-exhaustion",
		MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt,
	}
	if err := threads.Create(ctx, thread, []models.ChannelThreadMember{{
		ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true,
		RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto,
	}}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if err := threads.CreateLeadInput(ctx, models.ChannelLeadInput{
		ID: inputID, ThreadID: threadID,
		Payload: workinput.Input{Actor: string(constants.ChannelMessageActorUser), Content: "继续处理"},
		Status:  constants.ChannelInputStatusDelivering, LeadExecutionID: executionID,
	}); err != nil {
		t.Fatalf("create delivering lead input: %v", err)
	}
	chatFake := &postTurnChatFake{
		summaries:         map[string]chatservice.ChatSummary{leadChatID: {Chat: models.Chat{ID: leadChatID}}},
		subscribeFailures: map[string]int{leadChatID: 100},
	}
	executionFake := &postTurnExecutionFake{}
	service := &service{
		channels: channels, threads: threads, tasks: tasks, transactions: transactions,
		chats: chatFake, executions: executionFake, lifecycleContext: ctx, lifecycleCancel: cancel,
		watchers:     make(map[string]struct{}),
		capabilities: channeltool.NewRegistry(),
		watcherPolicy: deliveryWatcherPolicy{
			maximumFailures: 2,
			initialDelay:    time.Millisecond,
			maximumDelay:    time.Millisecond,
		},
	}
	t.Cleanup(service.Close)
	service.watchLeadExecution(thread, executionID, nil, []string{inputID}, "")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inputs, listErr := threads.ListLeadInputs(
			ctx,
			threadID,
			[]constants.ChannelInputStatus{constants.ChannelInputStatusConsumed},
			10,
		)
		if listErr == nil && len(inputs) == 1 {
			if inputs[0].LeadExecutionID != executionID || executionFake.cancelCount() != 1 {
				t.Fatalf("compensated lead input = %+v cancellations=%d", inputs[0], executionFake.cancelCount())
			}
			events, eventErr := threads.ListEvents(ctx, threadID, "", 20)
			if eventErr != nil {
				t.Fatalf("list leader processing events: %v", eventErr)
			}
			for _, event := range events {
				if event.Type != constants.ChannelEventTeamLeaderProcessing {
					continue
				}
				var processing leaderProcessingEvent
				if unmarshalErr := json.Unmarshal(event.Payload, &processing); unmarshalErr != nil {
					t.Fatalf("decode leader processing event: %v", unmarshalErr)
				}
				if processing.RetryExhausted && !processing.Active {
					return
				}
			}
			t.Fatalf("retry exhaustion event missing: %+v", events)
		}
		time.Sleep(10 * time.Millisecond)
	}
	inputs, listErr := threads.ListLeadInputs(
		ctx,
		threadID,
		[]constants.ChannelInputStatus{constants.ChannelInputStatusConsumed},
		10,
	)
	t.Fatalf("lead input after watcher exhaustion = %+v, error = %v", inputs, listErr)
}

// Start 记录成员派发并返回已经接纳的测试 Execution。
func (f *postTurnExecutionFake) Start(
	_ context.Context,
	intent execution.WorkIntent,
) (execution.AcceptedExecution, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.startError != nil {
		return execution.AcceptedExecution{}, f.startError
	}
	f.starts = append(f.starts, intent)
	executionID := fmt.Sprintf("execution-%022d", len(f.starts))
	if intent.ExecutionID != "" {
		executionID = intent.ExecutionID
	}
	return execution.AcceptedExecution{
		Execution: models.Execution{
			ID:     executionID,
			ChatID: intent.Chat.ChatID,
			Status: constants.ExecutionStatusRunning,
		},
	}, nil
}

// Cancel 记录通知接纳失败后发起的非等待停止，避免调用未装配的接口。
func (f *postTurnExecutionFake) Cancel(chatID string) error {
	return f.CancelAndWait(context.Background(), chatID)
}

// CancelAndWait 记录 watcher 补偿需要精确停止的 Chat。
func (f *postTurnExecutionFake) CancelAndWait(_ context.Context, chatID string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.canceledChats = append(f.canceledChats, chatID)
	return f.cancelError
}

// CancelExecutionAndWait 记录精确停止所针对的执行。
func (f *postTurnExecutionFake) CancelExecutionAndWait(ctx context.Context, chatID, executionID string) error {
	f.mutex.Lock()
	f.canceledExecutions = append(f.canceledExecutions, executionID)
	f.mutex.Unlock()
	return f.CancelAndWait(ctx, chatID)
}

// cancelCount 返回 watcher 补偿已经请求的停止次数。
func (f *postTurnExecutionFake) cancelCount() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return len(f.canceledChats)
}

// setStartError 设置后续执行接纳返回的错误。
func (f *postTurnExecutionFake) setStartError(err error) {
	f.mutex.Lock()
	f.startError = err
	f.mutex.Unlock()
}

// startCount 返回测试期间已经接纳的成员执行数量。
func (f *postTurnExecutionFake) startCount() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return len(f.starts)
}

// firstStart 返回第一条已经接纳的成员工作意图。
func (f *postTurnExecutionFake) firstStart() execution.WorkIntent {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.starts[0]
}

// startAt 返回指定顺序已经接纳的测试工作意图。
func (f *postTurnExecutionFake) startAt(index int) execution.WorkIntent {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.starts[index]
}

func TestLeadCreatedTasksDispatchAfterLeadTerminalAndStartupRecovery(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "post-turn-dispatch.db"),
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

	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	wakers := controllers.NewWakerController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx, cancel := context.WithCancel(context.Background())
	channelID := "11111111111111111111111111111111"
	threadID := "22222222222222222222222222222222"
	leadID := "33333333333333333333333333333333"
	memberID := "44444444444444444444444444444444"
	leadChatID := "55555555555555555555555555555555"
	memberChatID := "66666666666666666666666666666666"
	createChannelTestWakers(t, ctx, wakers, leadID, memberID)
	if err := channels.Create(ctx, models.Channel{
		ID:          channelID,
		Name:        "交付团队",
		LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	thread := models.ChannelThread{
		ID:                 threadID,
		ChannelID:          channelID,
		Title:              "完成团队模式",
		LeadWakerID:        leadID,
		LeadChatID:         leadChatID,
		WorkspaceDirectory: "/tmp/workspace",
		MaxParallel:        3,
		MaxTasks:           12,
		MaxFailureAttempts: 2,
		ReadyAt:            testTimestamp(1),
	}
	if err := threads.Create(ctx, thread, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel thread: %v", err)
	}

	leadExecutionID := "88888888888888888888888888888888"
	chatFake := &postTurnChatFake{
		summaries: map[string]chatservice.ChatSummary{
			leadChatID:   {Chat: models.Chat{ID: leadChatID, ActiveExecutionID: leadExecutionID}},
			memberChatID: {Chat: models.Chat{ID: memberChatID}},
		},
		leadChatID: leadChatID,
		leadTerminal: chatservice.PublicEvent{Message: &messageview.Message{
			ExecutionID: leadExecutionID,
			Status:      constants.ExecutionStatusCompleted,
		}},
	}
	executionFake := &postTurnExecutionFake{}
	service := &service{
		channels:         channels,
		threads:          threads,
		tasks:            tasks,
		transactions:     transactions,
		wakers:           wakers,
		chats:            chatFake,
		executions:       executionFake,
		lifecycleContext: ctx,
		lifecycleCancel:  cancel,
		watchers:         make(map[string]struct{}),
		capabilities:     channeltool.NewRegistry(),
	}

	t.Cleanup(service.Close)

	created, err := service.CreateTask(ctx, CreateTaskInput{
		ThreadID:        threadID,
		AssigneeWakerID: memberID,
		Subject:         "实现任务面板",
		Description:     "实现并验证任务面板。",
		DeferDispatch:   true,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if created.Task.Status != constants.ChannelTaskStatusPending || executionFake.startCount() != 0 {
		t.Fatalf("task before lead terminal = %+v, starts = %d", created.Task, executionFake.startCount())
	}
	if _, err := service.schedule(ctx, threadID); err != nil {
		t.Fatalf("schedule while lead is active: %v", err)
	}
	if executionFake.startCount() != 0 {
		t.Fatalf("deferred task dispatched while lead was active: %d", executionFake.startCount())
	}

	service.watchLeadExecution(thread, leadExecutionID, nil, nil, "")
	deadline := time.Now().Add(2 * time.Second)
	for executionFake.startCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if executionFake.startCount() != 1 {
		t.Fatalf("member executions after lead terminal = %d, want 1", executionFake.startCount())
	}
	memberIntent := executionFake.firstStart()
	if memberIntent.Input.Actor != "team_lead" ||
		!strings.HasPrefix(memberIntent.Input.Content, "实现任务面板\n\n") ||
		strings.Contains(memberIntent.Input.Content, "你正在作为团队执行伙伴完成一个正式任务。") ||
		strings.Contains(memberIntent.Input.Content, "完成后请在最终答复中简洁总结") {
		t.Fatalf("member task prompt = %q", memberIntent.Input.Content)
	}
	if len(memberIntent.Options.SupplementalInstructions) != 1 ||
		memberIntent.Options.SupplementalInstructions[0].Name != constants.TeamInstructionFileName ||
		!strings.Contains(memberIntent.Options.SupplementalInstructions[0].Content, "Role: Member") {
		t.Fatalf("member supplemental instructions = %+v", memberIntent.Options.SupplementalInstructions)
	}
	if len(memberIntent.Options.ToolCapabilities) != 1 ||
		len(memberIntent.Options.ToolCapabilities[0].Bindings) != 1 || memberIntent.Options.ToolCapabilities[0].Bindings[0].Operation != "team.reports.submit" {
		t.Fatalf("member report capability = %+v", memberIntent.Options.ToolCapabilities)
	}

	current, err := tasks.Get(ctx, created.Task.ID)
	if err != nil {
		t.Fatalf("get dispatched task: %v", err)
	}
	if current.Status != constants.ChannelTaskStatusInProgress || current.CurrentFence != 1 {
		t.Fatalf("dispatched task = %+v", current)
	}

	// 服务重启时必须继续推进负责人上一回合已经持久化、但尚未派发的任务。
	service.Close()
	// 停止旧生命周期后，后续模拟请求使用新的调用上下文。
	ctx = context.Background()
	if err := tasks.UpdateFields(ctx, current.ID, map[string]interface{}{
		"status":      constants.ChannelTaskStatusCompleted,
		"finished_at": time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("complete first task before restart: %v", err)
	}
	recovered, err := service.CreateTask(ctx, CreateTaskInput{
		ThreadID:        threadID,
		AssigneeWakerID: memberID,
		Subject:         "补齐恢复派发",
		Description:     "服务启动后继续派发已持久化任务。",
		DeferDispatch:   true,
	})
	if err != nil {
		t.Fatalf("create deferred recovery task: %v", err)
	}
	if executionFake.startCount() != 1 {
		t.Fatalf("member executions before restart = %d, want 1", executionFake.startCount())
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if executionFake.startCount() != 2 {
		t.Fatalf("member executions after restart = %d, want 2", executionFake.startCount())
	}
	recoveredCurrent, err := tasks.Get(ctx, recovered.Task.ID)
	if err != nil {
		t.Fatalf("get startup-dispatched task: %v", err)
	}
	if recoveredCurrent.Status != constants.ChannelTaskStatusInProgress || recoveredCurrent.CurrentFence != 1 {
		t.Fatalf("startup-dispatched task = %+v", recoveredCurrent)
	}

	// 显式提及负责人直接进入可写的负责人 Chat，不创建会与自动汇总重叠的负责人任务。
	directInput := execution.WorkInput{
		Actor:       "user",
		Content:     "验证负责人在主 Chat 中直接执行正式工作。",
		Attachments: []execution.AttachmentInput{{SourcePath: "/tmp/team-spec.md"}},
		CodeReferences: []execution.CodeReferenceInput{{
			Path:      "server/services/channel/task.go",
			StartLine: 1,
			EndLine:   20,
			Content:   "package channel",
		}},
		SkillIDs:     []string{"skill-a"},
		ConnectorIDs: []string{"connector-a"},
	}
	leadMessage, err := service.sendMessage(ctx, SendMessageInput{
		ThreadID:       threadID,
		MentionWakerID: leadID,
		Input:          directInput,
	})
	if err != nil {
		t.Fatalf("send direct lead message: %v", err)
	}
	if leadMessage.Task != nil {
		t.Fatalf("direct lead message unexpectedly created task: %+v", leadMessage.Task)
	}
	if leadMessage.Execution == nil || leadMessage.QueuedInput == nil {
		t.Fatalf("direct lead message result = %+v", leadMessage)
	}
	deliveringInputs, err := threads.ListLeadInputs(
		ctx,
		threadID,
		[]constants.ChannelInputStatus{constants.ChannelInputStatusDelivering},
		10,
	)
	if err != nil || len(deliveringInputs) != 1 || !deliveringInputs[0].DirectExecution {
		t.Fatalf("direct lead queued input = %+v, error = %v", deliveringInputs, err)
	}
	if executionFake.startCount() != 3 {
		t.Fatalf("executions after direct lead message = %d, want 3", executionFake.startCount())
	}
	leadDirectIntent := executionFake.startAt(2)
	if leadDirectIntent.Chat.ChatID != leadChatID || leadDirectIntent.Input.Actor != "user" {
		t.Fatalf("direct lead intent = %+v", leadDirectIntent)
	}
	if leadDirectIntent.Options.WorkspaceReadOnly {
		t.Fatal("direct lead execution unexpectedly inherited coordinator read-only policy")
	}
	if leadDirectIntent.Input.Content != directInput.Content ||
		len(leadDirectIntent.Input.Attachments) != 1 ||
		len(leadDirectIntent.Input.CodeReferences) != 1 ||
		len(leadDirectIntent.Input.SkillIDs) != 1 ||
		len(leadDirectIntent.Input.ConnectorIDs) != 1 {
		t.Fatalf("direct lead input lost capability references: %+v", leadDirectIntent.Input)
	}
	if len(leadDirectIntent.Options.SupplementalInstructions) != 1 ||
		leadDirectIntent.Options.SupplementalInstructions[0].Name != constants.TeamInstructionFileName {
		t.Fatalf("direct lead supplemental instructions = %+v", leadDirectIntent.Options.SupplementalInstructions)
	}
	if _, err := service.CreateTask(ctx, CreateTaskInput{
		ThreadID:        threadID,
		AssigneeWakerID: leadID,
		Subject:         "汇总结果",
		Description:     "把最终汇总错误地建成负责人任务。",
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("CreateTask(lead) error = %v, want BadRequest", err)
	}

	// 任务板稳定后 announce 启动失败或负责人繁忙时必须配对写入 active=false，SSE 不能停留在瞬时 bridge。
	executionFake.setStartError(status_error.ChatHasActiveExecution)
	if err := tasks.UpdateFields(ctx, recoveredCurrent.ID, map[string]interface{}{
		"status":      constants.ChannelTaskStatusCompleted,
		"finished_at": time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("complete recovered task before stable-board drain: %v", err)
	}
	reviewTask := models.ChannelTask{
		ID:                  "99999999999999999999999999999999",
		ThreadID:            threadID,
		AssigneeWakerID:     memberID,
		Subject:             "等待用户验收",
		Description:         "验收后触发负责人继续汇总。",
		Status:              constants.ChannelTaskStatusInReview,
		RequiresHumanReview: true,
		CurrentFence:        1,
		Result:              "已完成等待验收",
		ChangedPaths:        []string{},
		CreatedAt:           time.Now().UnixMilli(),
		UpdatedAt:           time.Now().UnixMilli(),
	}
	if err := tasks.Create(ctx, reviewTask, nil); err != nil {
		t.Fatalf("create review task: %v", err)
	}
	now := time.Now().UnixMilli()
	if err := tasks.UpdateFields(ctx, reviewTask.ID, map[string]interface{}{
		"status":      constants.ChannelTaskStatusCompleted,
		"finished_at": now,
		"updated_at":  now,
	}); err != nil {
		t.Fatalf("complete approved review task: %v", err)
	}
	if err := tasks.CreateNotification(ctx, models.ChannelLeadNotification{
		ID:        "abababababababababababababababab",
		ThreadID:  threadID,
		TaskID:    reviewTask.ID,
		AttemptID: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
		Status:    constants.ChannelNotificationStatusPending,
		Payload:   `{"taskId":"99999999999999999999999999999999","status":"completed"}`,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create approved review notification: %v", err)
	}
	cursor, err := threads.LatestEventCursor(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReconcileThread(ctx, threadID); err != nil {
		t.Fatalf("reconcile approved review task: %v", err)
	}
	approved, err := tasks.Get(ctx, reviewTask.ID)
	if err != nil || approved.Status != constants.ChannelTaskStatusCompleted {
		t.Fatalf("approved review task = %+v, error = %v", approved, err)
	}
	pendingNotifications, err := tasks.ListPendingNotifications(ctx, threadID, 20)
	if err != nil || len(pendingNotifications) != 1 ||
		!notificationAttemptExists(pendingNotifications, "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd") {
		t.Fatalf("review completion notifications = %+v, error = %v", pendingNotifications, err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		events, listErr := threads.ListEvents(ctx, threadID, cursor, 100)
		if listErr != nil {
			t.Fatalf("list processing events: %v", listErr)
		}
		opened := make(map[string]bool)
		closed := false
		for _, event := range events {
			if event.Type != constants.ChannelEventTeamLeaderProcessing {
				continue
			}
			var processing leaderProcessingEvent
			if err := json.Unmarshal(event.Payload, &processing); err != nil {
				t.Fatalf("decode processing event: %v", err)
			}
			if processing.ExecutionID == "" {
				continue
			}
			if processing.Active {
				opened[processing.ExecutionID] = true
			} else if opened[processing.ExecutionID] {
				closed = true
			}
		}
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("busy announce did not close its processing bridge for any of %v", opened)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPendingFollowUpReassignmentKeepsDelegationReason(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "reassigned-follow-up.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()
	tasks := controllers.NewChannelTaskController(database.DB)
	if err := tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{
		ID: "original-attempt", TaskID: "task", AssigneeWakerID: "original", Fence: 1,
		Reason: constants.ChannelAttemptReasonInitial, StartedAt: testTimestamp(100),
	}); err != nil {
		t.Fatal(err)
	}
	sut := &service{tasks: tasks}
	for _, actor := range []constants.ChannelMessageActor{constants.ChannelMessageActorLead, constants.ChannelMessageActorUser} {
		followUps := []models.ChannelTaskFollowUp{{Actor: actor}}
		reason, err := sut.taskAttemptReason(ctx, models.ChannelTask{ID: "task", CurrentFence: 1, AssigneeWakerID: "replacement"}, followUps)
		if err != nil || reason != constants.ChannelAttemptReasonReassigned {
			t.Fatalf("reassigned %s continuation reason = %s, error = %v", actor, reason, err)
		}
		reason, err = sut.taskAttemptReason(ctx, models.ChannelTask{ID: "task", CurrentFence: 1, AssigneeWakerID: "original"}, followUps)
		if err != nil || reason == constants.ChannelAttemptReasonReassigned {
			t.Fatalf("same-member %s continuation reason = %s, error = %v", actor, reason, err)
		}
	}
}

func TestUnstartedAttemptKeepsDelegationReasonAfterRelease(t *testing.T) {
	for _, reason := range []constants.ChannelAttemptReason{
		constants.ChannelAttemptReasonInitial,
		constants.ChannelAttemptReasonRetry,
		constants.ChannelAttemptReasonReassigned,
		constants.ChannelAttemptReasonLeadFollowUp,
		constants.ChannelAttemptReasonUserFollowUp,
	} {
		t.Run(string(reason), func(t *testing.T) {
			database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "unstarted.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := database.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			ctx := context.Background()
			tasks := controllers.NewChannelTaskController(database.DB)
			sut := &service{tasks: tasks, transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))}
			task := models.ChannelTask{ID: "task", ThreadID: "thread", AssigneeWakerID: "replacement", CurrentFence: 2,
				Status: constants.ChannelTaskStatusInProgress, ChangedPaths: []string{}, CancelRequestedAt: testTimestamp(50), CancelRequestedFence: 1}
			if err := tasks.Create(ctx, task, nil); err != nil {
				t.Fatal(err)
			}
			attempt := models.ChannelTaskAttempt{ID: "reservation", TaskID: task.ID, AssigneeWakerID: task.AssigneeWakerID,
				Fence: task.CurrentFence, Reason: reason, Status: constants.ExecutionStatusPending}
			if err := tasks.CreateAttempt(ctx, attempt); err != nil {
				t.Fatal(err)
			}
			if err := tasks.CreateFollowUp(ctx, models.ChannelTaskFollowUp{
				ID: "supplement", TaskID: task.ID, Actor: constants.ChannelMessageActorLead,
				DeliveredAt: testTimestamp(100), DeliveredAttemptID: attempt.ID,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := sut.releaseUnstartedAttempt(ctx, attempt); err != nil {
				t.Fatal(err)
			}
			task, err = tasks.Get(ctx, task.ID)
			if err != nil || task.Status != constants.ChannelTaskStatusPending {
				t.Fatalf("released task = %+v, error = %v", task, err)
			}
			followUps, err := tasks.ListPendingFollowUps(ctx, task.ID)
			if err != nil || len(followUps) != 1 {
				t.Fatalf("released follow-ups = %+v, error = %v", followUps, err)
			}
			// 新实例只读取持久事实，覆盖释放后重启再派发。
			recovered := &service{tasks: controllers.NewChannelTaskController(database.DB)}
			got, err := recovered.taskAttemptReason(ctx, task, followUps)
			if err != nil || got != reason {
				t.Fatalf("next accepted reason = %s, want %s, error = %v", got, reason, err)
			}
			got, err = recovered.taskAttemptReason(ctx, task, nil)
			if err != nil || got != reason {
				t.Fatalf("next accepted reason without supplement = %s, want %s, error = %v", got, reason, err)
			}
		})
	}
}

type pausedTaskAdmission struct {
	// postTurnExecutionFake 保留真实服务接口及实际派发计数。
	*postTurnExecutionFake
	// pauseFirst 只暂停第一次派发，使后续排队任务可正常接纳。
	pauseFirst sync.Once
	// ready 通知测试任务已领取，Execution 尚未接纳输入。
	ready chan struct{}
	// resume 模拟准备完成后继续耐久接纳。
	resume <-chan struct{}
	// stopped 记录尚无 Chat 投影时发出的精确取消。
	stopped chan string
}

func (p *pausedTaskAdmission) CancelExecutionAndWait(ctx context.Context, chatID, executionID string) error {
	err := p.postTurnExecutionFake.CancelExecutionAndWait(ctx, chatID, executionID)
	p.stopped <- executionID
	return err
}

func (p *pausedTaskAdmission) Start(ctx context.Context, intent execution.WorkIntent) (accepted execution.AcceptedExecution, err error) {
	paused := false
	p.pauseFirst.Do(func() {
		paused = true
		close(p.ready)
	})
	if !paused {
		return p.postTurnExecutionFake.Start(ctx, intent)
	}
	select {
	case <-p.resume:
	case <-ctx.Done():
		return execution.AcceptedExecution{}, ctx.Err()
	}
	err = intent.AdmissionBoundary.Commit(ctx, func(ctx context.Context) error {
		accepted, err = p.postTurnExecutionFake.Start(ctx, intent)
		return err
	})
	return accepted, err
}

func TestStopTaskDuringClaimPreventsExecutionAdmission(t *testing.T) {
	for _, source := range []string{"user", "capability"} {
		t.Run(source, func(t *testing.T) {
			sut, thread, _, executions := newDelegationDispatchService(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			task, err := sut.CreateTask(ctx, CreateTaskInput{ThreadID: thread.ID, SenderWakerID: "lead", AssigneeWakerID: "member",
				Subject: "停止尚未接纳的任务", Description: "检查当前尝试的停止围栏。", DeferDispatch: true})
			if err != nil {
				t.Fatal(err)
			}
			resume := make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			defer release()
			paused := &pausedTaskAdmission{postTurnExecutionFake: executions, ready: make(chan struct{}), resume: resume, stopped: make(chan string, 1)}
			sut.executions = paused
			dispatched := make(chan error, 1)
			go func() {
				_, err := sut.schedule(ctx, thread.ID)
				dispatched <- err
			}()
			select {
			case <-paused.ready:
			case <-ctx.Done():
				t.Fatal("task never reached claimed admission")
			}
			next, err := sut.CreateTask(ctx, CreateTaskInput{ThreadID: thread.ID, SenderWakerID: "lead", AssigneeWakerID: "member",
				Subject: "执行后续任务", Description: "前一任务停止后继续派发。"})
			if err != nil || next.Task.Status != constants.ChannelTaskStatusPending {
				t.Fatalf("next task = %+v, error = %v", next.Task, err)
			}
			if source == "user" {
				err = sut.StopTask(ctx, task.Task.ID)
			} else {
				_, err = capability.Execute(ctx, sut.commands, "lead:"+thread.ID, capability.Request{Operation: "team.tasks.cancel", RequestID: "stop"}, func(ctx context.Context) error {
					return sut.StopTask(ctx, task.Task.ID)
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := sut.tasks.GetCurrentAttempt(ctx, task.Task.ID, task.Task.CurrentFence+1)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case executionID := <-paused.stopped:
				if executionID != claimed.ExecutionID {
					t.Fatalf("stop targeted execution %s, want %s", executionID, claimed.ExecutionID)
				}
			case <-ctx.Done():
				t.Fatal("stop waited for the Chat projection before targeting the exact execution")
			}
			release()
			select {
			case err := <-dispatched:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("claimed task did not settle")
			}
			current, err := sut.tasks.Get(ctx, task.Task.ID)
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := sut.tasks.GetCurrentAttempt(ctx, current.ID, current.CurrentFence)
			if err != nil || current.Status != constants.ChannelTaskStatusCancelled || attempt.Status != constants.ExecutionStatusCanceled || attempt.StartedAt != nil {
				t.Fatalf("stopped attempt reached execution for %s: task=%s attempt=%s err=%v", source, current.Status, attempt.Status, err)
			}
			if started := waitForDispatchedTask(t, ctx, sut, next.Task.ID); started.ExecutionID == attempt.ExecutionID || executions.startCount() != 1 {
				t.Fatalf("unexpected task dispatch: next=%s stopped=%s starts=%d", started.ExecutionID, attempt.ExecutionID, executions.startCount())
			}
		})
	}
}

// newDelegationDispatchService 使用真实仓储与执行边界替身构造委派调度测试。
func newDelegationDispatchService(t *testing.T) (*service, models.ChannelThread, *postTurnChatFake, *postTurnExecutionFake) {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "delegation-dispatch.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	wakers := controllers.NewWakerController(database.DB)
	createChannelTestWakers(t, ctx, wakers, "lead", "member")
	channels := controllers.NewChannelController(database.DB)
	if err := channels.Create(ctx, models.Channel{ID: "channel", Name: "委派调度", LeadWakerID: "lead"}, []models.ChannelMember{
		{ChannelID: "channel", WakerID: "lead", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: "channel", WakerID: "member", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	thread := models.ChannelThread{ID: "thread", ChannelID: "channel", LeadWakerID: "lead", LeadChatID: "lead-chat",
		WorkspaceDirectory: t.TempDir(), MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: testTimestamp(1)}
	threads := controllers.NewChannelThreadController(database.DB)
	if err := threads.Create(ctx, thread, []models.ChannelThreadMember{
		{ThreadID: thread.ID, WakerID: "lead", ChatID: "lead-chat", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: thread.ID, WakerID: "member", ChatID: "member-chat", RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatal(err)
	}
	chats := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{
		"lead-chat": {Chat: models.Chat{ID: "lead-chat"}}, "member-chat": {Chat: models.Chat{ID: "member-chat"}},
	}}
	executions := &postTurnExecutionFake{}
	sut := &service{channels: channels, threads: threads, tasks: controllers.NewChannelTaskController(database.DB),
		commands:     controllers.NewAgentCommandController(database.DB),
		transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)),
		wakers:       wakers, chats: chats, executions: executions, lifecycleContext: ctx, lifecycleCancel: cancel,
		watchers: make(map[string]struct{}), capabilities: channeltool.NewRegistry()}
	t.Cleanup(sut.Close)
	return sut, thread, chats, executions
}

func TestRetryDelegationKeepsRequestTimeAcrossBusyAndFailedAdmission(t *testing.T) {
	sut, thread, chats, executions := newDelegationDispatchService(t)
	ctx := context.Background()
	created, err := sut.CreateTask(ctx, CreateTaskInput{ThreadID: thread.ID, SenderWakerID: "lead", AssigneeWakerID: "member",
		Subject: "验证委派时间", Description: "忙碌和接纳失败均不能改变发起身份。", DeferDispatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if created.Task.DelegationSentAt != created.Task.CreatedAt {
		t.Fatalf("initial request time = %d, want %d", created.Task.DelegationSentAt, created.Task.CreatedAt)
	}
	if err := sut.tasks.UpdateFields(ctx, created.Task.ID, map[string]interface{}{
		"status": constants.ChannelTaskStatusFailed, "current_fence": 1, "delegation_sent_at": int64(50),
	}); err != nil {
		t.Fatal(err)
	}
	if err := sut.tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{ID: "original", TaskID: created.Task.ID, Fence: 1,
		AssigneeWakerID: "member", Reason: constants.ChannelAttemptReasonInitial, Status: constants.ExecutionStatusFailed,
		StartedAt: testTimestamp(100)}); err != nil {
		t.Fatal(err)
	}
	chats.summaries["member-chat"] = chatservice.ChatSummary{Chat: models.Chat{ID: "member-chat", ActiveExecutionID: "busy"}}
	before := time.Now().UnixMilli()
	if err := sut.retryTask(ctx, created.Task.ID, "member", ""); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UnixMilli()
	queued, err := sut.tasks.Get(ctx, created.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.DelegationSentAt < before || queued.DelegationSentAt > after || executions.startCount() != 0 {
		t.Fatalf("busy retry request = %+v, starts = %d", queued, executions.startCount())
	}
	if _, err := sut.updatePendingTask(ctx, UpdateTaskInput{ThreadID: thread.ID, TaskID: queued.ID, Subject: "只修订说明"}); err != nil {
		t.Fatal(err)
	}
	// 模拟排队后的其他变化，不允许 UpdatedAt 充当委派发起时间。
	if err := sut.tasks.UpdateFields(ctx, queued.ID, map[string]interface{}{"updated_at": after + 1000}); err != nil {
		t.Fatal(err)
	}
	chats.summaries["member-chat"] = chatservice.ChatSummary{Chat: models.Chat{ID: "member-chat"}}
	executions.setStartError(status_error.ChatHasActiveExecution)
	if _, err := sut.schedule(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	executions.setStartError(nil)
	if _, err := sut.schedule(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	delegations, err := sut.tasks.ListDelegationAttempts(ctx, []string{queued.ID})
	if err != nil || len(delegations) != 2 {
		t.Fatalf("accepted delegation attempts = %+v, error = %v", delegations, err)
	}
	if got := delegations[1]; got.Reason != constants.ChannelAttemptReasonRetry || got.DelegationSentAt != queued.DelegationSentAt {
		t.Fatalf("accepted retry lost queued identity: %+v, want sentAt %d", got, queued.DelegationSentAt)
	}
}

func TestDirectMentionResumesWhenMemberChatBecomesAvailable(t *testing.T) {
	for _, busy := range []struct {
		name string
		err  error
	}{
		{name: "chat occupied", err: status_error.ChatHasActiveExecution},
		{name: "native preparation busy", err: agent.ErrTurnAdmissionBusy},
	} {
		t.Run(busy.name, func(t *testing.T) {
			sut, thread, _, executions := newDelegationDispatchService(t)
			ctx, cancel := context.WithTimeout(t.Context(), 2500*time.Millisecond)
			defer cancel()
			executions.setStartError(busy.err)
			sent, err := sut.sendMessage(ctx, SendMessageInput{
				ThreadID: thread.ID, MentionWakerID: "member", Input: execution.WorkInput{Content: "待成员空闲后执行"},
			})
			if err != nil || sent.Task == nil || sent.Task.Status != constants.ChannelTaskStatusPending {
				t.Fatalf("busy member task = %+v, error = %v", sent.Task, err)
			}
			executions.setStartError(nil)
			waitForDispatchedTask(t, ctx, sut, sent.Task.ID)
		})
	}
}

func waitForDispatchedTask(t *testing.T, ctx context.Context, sut *service, taskID string) models.ChannelTaskAttempt {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := sut.tasks.Get(ctx, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == constants.ChannelTaskStatusInProgress {
			attempt, err := sut.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
			if err != nil {
				t.Fatal(err)
			}
			if attempt.StartedAt != nil {
				return attempt
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("queued task %s was not accepted: status=%s", taskID, task.Status)
		case <-ticker.C:
		}
	}
}

func TestPendingReassignmentCapturesNewRequestTime(t *testing.T) {
	sut, thread, _, executions := newDelegationDispatchService(t)
	ctx := context.Background()
	task := models.ChannelTask{ID: "task", ThreadID: thread.ID, SenderWakerID: "lead", AssigneeWakerID: "original",
		Status: constants.ChannelTaskStatusPending, CurrentFence: 1, ChangedPaths: []string{}, DelegationSentAt: 50}
	if err := sut.tasks.Create(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	if err := sut.tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{ID: "original", TaskID: task.ID, Fence: 1,
		AssigneeWakerID: "original", Reason: constants.ChannelAttemptReasonInitial, StartedAt: testTimestamp(100), DelegationSentAt: 50}); err != nil {
		t.Fatal(err)
	}
	if err := sut.tasks.CreateFollowUp(ctx, models.ChannelTaskFollowUp{ID: "follow-up", TaskID: task.ID,
		Actor: constants.ChannelMessageActorLead, Text: "继续执行", Payload: workinput.Input{Content: "继续执行"}}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UnixMilli()
	updated, err := sut.updatePendingTask(ctx, UpdateTaskInput{ThreadID: thread.ID, TaskID: task.ID, AssigneeWakerID: "member"})
	if err != nil || updated.DelegationSentAt < before {
		t.Fatalf("reassignment request = %+v, error = %v", updated, err)
	}
	executions.setStartError(status_error.ChatHasActiveExecution)
	if _, err := sut.schedule(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	executions.setStartError(nil)
	if _, err := sut.schedule(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	delegations, err := sut.tasks.ListDelegationAttempts(ctx, []string{task.ID})
	if err != nil || len(delegations) != 2 || delegations[0].DelegationSentAt != 50 ||
		delegations[1].Reason != constants.ChannelAttemptReasonReassigned || delegations[1].DelegationSentAt != updated.DelegationSentAt {
		t.Fatalf("reassignment rewrote request history: %+v, error = %v", delegations, err)
	}
}

func TestDirectMentionOnlySupplementsInProgressAndReviewRequiresExplicitDecision(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "direct-mention-state.db"),
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

	channels := controllers.NewChannelController(database.DB)
	threads := controllers.NewChannelThreadController(database.DB)
	tasks := controllers.NewChannelTaskController(database.DB)
	wakers := controllers.NewWakerController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	ctx := context.Background()
	channelID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	threadID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	leadID := "cccccccccccccccccccccccccccccccc"
	memberID := "dddddddddddddddddddddddddddddddd"
	memberChatID := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	createChannelTestWakers(t, ctx, wakers, leadID, memberID)
	if err := channels.Create(ctx, models.Channel{
		ID:          channelID,
		Name:        "状态边界团队",
		LeadWakerID: leadID,
	}, []models.ChannelMember{
		{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := threads.Create(ctx, models.ChannelThread{
		ID:                 threadID,
		ChannelID:          channelID,
		Title:              "验证定向补充状态",
		LeadWakerID:        leadID,
		LeadChatID:         "ffffffffffffffffffffffffffffffff",
		WorkspaceDirectory: "/tmp/workspace",
		MaxParallel:        3,
		MaxTasks:           12,
		MaxFailureAttempts: 2,
		ReadyAt:            testTimestamp(1),
	}, []models.ChannelThreadMember{
		{ThreadID: threadID, WakerID: leadID, ChatID: "99999999999999999999999999999999", IsLead: true, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto},
		{ThreadID: threadID, WakerID: memberID, ChatID: memberChatID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
	}); err != nil {
		t.Fatalf("create channel thread: %v", err)
	}
	now := time.Now().UnixMilli()
	pendingTask := models.ChannelTask{
		ID:              "11111111111111111111111111111111",
		ThreadID:        threadID,
		AssigneeWakerID: memberID,
		Subject:         "已经排队",
		Description:     "保持排队，不吸收新输入。",
		Status:          constants.ChannelTaskStatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	reviewTask := models.ChannelTask{
		ID:                  "22222222222222222222222222222222",
		ThreadID:            threadID,
		AssigneeWakerID:     memberID,
		Subject:             "等待人工验收",
		Description:         "必须由用户明确通过、驳回或取消。",
		Status:              constants.ChannelTaskStatusInReview,
		RequiresHumanReview: true,
		CurrentFence:        1,
		CreatedAt:           now + 1,
		UpdatedAt:           now + 1,
	}
	for _, task := range []models.ChannelTask{pendingTask, reviewTask} {
		if err := tasks.Create(ctx, task, nil); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}
	chatFake := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{
		memberChatID: {Chat: models.Chat{ID: memberChatID, ActiveExecutionID: "external-member-work"}},
	}}
	executions := &postTurnExecutionFake{}
	service := &service{
		channels:     channels,
		threads:      threads,
		tasks:        tasks,
		transactions: transactions,
		wakers:       wakers,
		chats:        chatFake,
		executions:   executions,
		watchers:     make(map[string]struct{}),
		capabilities: channeltool.NewRegistry(),
	}

	sent, err := service.sendMessage(ctx, SendMessageInput{
		ThreadID:       threadID,
		MentionWakerID: memberID,
		Input: execution.WorkInput{
			Actor:   string(constants.ChannelMessageActorUser),
			Content: "这是另一项明确任务",
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if sent.Task == nil || sent.Task.ID == pendingTask.ID || sent.Task.ID == reviewTask.ID {
		t.Fatalf("direct mention task = %+v, want a new formal task", sent.Task)
	}
	if sent.Task.SenderWakerID != "" {
		t.Fatalf("direct mention sender = %q, want user", sent.Task.SenderWakerID)
	}
	unchangedPending, err := tasks.Get(ctx, pendingTask.ID)
	if err != nil {
		t.Fatalf("get unchanged pending task: %v", err)
	}
	unchangedReview, err := tasks.Get(ctx, reviewTask.ID)
	if err != nil {
		t.Fatalf("get unchanged review task: %v", err)
	}
	if unchangedPending.Status != constants.ChannelTaskStatusPending ||
		unchangedReview.Status != constants.ChannelTaskStatusInReview {
		t.Fatalf("existing task states changed: pending=%s review=%s", unchangedPending.Status, unchangedReview.Status)
	}
	if err := service.FollowUpTask(ctx, pendingTask.ID, execution.WorkInput{Actor: string(constants.ChannelMessageActorUser), Content: "待执行任务应由负责人 update"}); err != status_error.ChannelTaskStateConflict {
		t.Fatalf("FollowUpTask(pending) error = %v, want state conflict", err)
	}
	failedTask := models.ChannelTask{
		ID:              "44444444444444444444444444444444",
		ThreadID:        threadID,
		AssigneeWakerID: memberID,
		Subject:         "等待负责人重试",
		Description:     "失败任务不能通过用户 follow-up 绕过 retry。",
		Status:          constants.ChannelTaskStatusFailed,
		CurrentFence:    1,
		CreatedAt:       now + 2,
		UpdatedAt:       now + 2,
	}
	if err := tasks.Create(ctx, failedTask, nil); err != nil {
		t.Fatalf("create failed task: %v", err)
	}
	if err := service.FollowUpTask(ctx, failedTask.ID, execution.WorkInput{Actor: string(constants.ChannelMessageActorUser), Content: "失败任务应由负责人 retry"}); err != status_error.ChannelTaskStateConflict {
		t.Fatalf("FollowUpTask(failed) error = %v, want state conflict", err)
	}
	failedAfterFollowUp, err := tasks.Get(ctx, failedTask.ID)
	if err != nil {
		t.Fatalf("get failed task after follow-up: %v", err)
	}
	failedFollowUps, err := tasks.ListPendingFollowUps(ctx, failedTask.ID)
	if err != nil {
		t.Fatalf("list failed task follow-ups: %v", err)
	}
	if failedAfterFollowUp.Status != constants.ChannelTaskStatusFailed || len(failedFollowUps) != 0 {
		t.Fatalf("failed task changed by follow-up: task=%+v follow-ups=%+v", failedAfterFollowUp, failedFollowUps)
	}
	if err := service.FollowUpTask(ctx, reviewTask.ID, execution.WorkInput{Actor: string(constants.ChannelMessageActorUser), Content: "普通补充不得驳回验收"}); err != status_error.ChannelTaskStateConflict {
		t.Fatalf("FollowUpTask(in_review) error = %v, want state conflict", err)
	}
	stillReview, err := tasks.Get(ctx, reviewTask.ID)
	if err != nil {
		t.Fatalf("get task still awaiting review: %v", err)
	}
	if stillReview.Status != constants.ChannelTaskStatusInReview {
		t.Fatalf("ordinary follow-up moved review task to %s", stillReview.Status)
	}
	cancellableReview := models.ChannelTask{
		ID:                  "33333333333333333333333333333333",
		ThreadID:            threadID,
		AssigneeWakerID:     memberID,
		Subject:             "取消单项验收",
		Description:         "验证 in_review 可以单独取消。",
		Status:              constants.ChannelTaskStatusInReview,
		RequiresHumanReview: true,
		CurrentFence:        1,
		CreatedAt:           now + 3,
		UpdatedAt:           now + 3,
	}
	if err := tasks.Create(ctx, cancellableReview, nil); err != nil {
		t.Fatalf("create cancellable review task: %v", err)
	}
	reviewAttempt := models.ChannelTaskAttempt{
		ID: "55555555555555555555555555555555", TaskID: cancellableReview.ID,
		ChatID: memberChatID, ExecutionID: "66666666666666666666666666666666", AssigneeWakerID: memberID,
		Fence: cancellableReview.CurrentFence, Reason: constants.ChannelAttemptReasonInitial,
		Status: constants.ExecutionStatusCompleted, StartedAt: testTimestamp(now + 2), FinishedAt: testTimestamp(now + 3),
	}
	if err := tasks.CreateAttempt(ctx, reviewAttempt); err != nil {
		t.Fatalf("create completed review attempt: %v", err)
	}
	if err := service.StopTask(ctx, cancellableReview.ID); err != nil {
		t.Fatalf("StopTask(in_review) error = %v", err)
	}
	cancelled, err := tasks.Get(ctx, cancellableReview.ID)
	if err != nil {
		t.Fatalf("get cancelled review task: %v", err)
	}
	if cancelled.Status != constants.ChannelTaskStatusCancelled || cancelled.FinishedAt == nil {
		t.Fatalf("cancelled review task = %+v", cancelled)
	}
	if len(executions.canceledExecutions) != 1 || executions.canceledExecutions[0] != reviewAttempt.ExecutionID {
		t.Fatalf("review cancellation stopped another member execution: %v", executions.canceledExecutions)
	}
}

// TestPostTurnExecutionFakeCancel 验证通知补偿使用的非等待取消也由替身记录。
func TestPostTurnExecutionFakeCancel(t *testing.T) {
	fake := &postTurnExecutionFake{}
	if err := fake.Cancel("lead-chat"); err != nil {
		t.Fatal(err)
	}
	if fake.cancelCount() != 1 || fake.canceledChats[0] != "lead-chat" {
		t.Fatalf("取消请求未记录: %v", fake.canceledChats)
	}
}

func TestExecutionObservationHotReadBudgetIgnoresLongPublicHistory(t *testing.T) {
	measure := func(prefix int) float64 {
		fixture := newDeletionFixture(t)
		root := t.TempDir()
		store := conversationfixture.New(t, root)
		marshal := func(value interface{}) json.RawMessage {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		chatID := "91919191919191919191919191919191"
		records := conversationfixture.Facts([]eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshal(eventlog.ChatCreatedData{WorkspaceDirectory: t.TempDir()})}})
		for index := 0; index <= prefix; index++ {
			id := fmt.Sprintf("round-%d", index)
			records = append(records, conversationfixture.Facts([]eventlog.AppendInput{
				{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-" + id, Data: marshal(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Actor: string(constants.ChannelMessageActorUser), Content: strings.Repeat("historical input ", 128)})},
				{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Data: marshal(eventlog.ExecutionCreatedData{UserMessageID: "user-" + id})},
			})...)
			records = append(records, conversationfixture.NativeMessage(t, store, chatID, id, agent.MessageChange{
				Kind: agent.MessageAppend, ContentType: constants.RuntimeEventTypeTextDelta, Text: strings.Repeat("historical result ", 128),
			}))
			records = append(records, agent.ConversationChange{Fact: &agent.ChatFact{
				Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: id, Data: marshal(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted}),
			}})
		}
		if _, err := conversationfixture.Commit(t.Context(), store, chatID, records); err != nil {
			t.Fatal(err)
		}
		module, err := chatservice.New(chatservice.Config{StorageDirectory: root, Conversations: store, Projects: controllers.NewProjectController(fixture.db), QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(fixture.db), DeletionIntents: controllers.NewChatDeletionIntentController(fixture.db), Transactions: fixture.service.transactions}})
		if err != nil {
			t.Fatal(err)
		}
		fixture.service.chats = module
		target := fmt.Sprintf("round-%d", prefix)
		// 首次恢复可重建索引；计量只覆盖索引健康后的真实订阅与准确终态补偿。
		warm, err := fixture.service.chats.SubscribeExecutionEvents(context.Background(), chatID, target)
		if err != nil {
			t.Fatal(err)
		}
		warm.Close()
		return testing.AllocsPerRun(2, func() {
			subscription, err := fixture.service.chats.SubscribeExecutionEvents(context.Background(), chatID, target)
			if err != nil {
				t.Fatal(err)
			}
			defer subscription.Close()
			if len(subscription.Backlog) != 1 || subscription.Backlog[0].ExecutionID != target || subscription.Backlog[0].Message != nil {
				t.Fatalf("historical public snapshots materialized: backlog=%d", len(subscription.Backlog))
			}
		})
	}
	small, large := measure(8), measure(256)
	t.Logf("hot observation allocations: 8 prior rounds=%.0f, 256 prior rounds=%.0f", small, large)
	if large > small*3 {
		t.Fatalf("public history replay grows with prior rounds: %.0f -> %.0f", small, large)
	}
}
