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
	canceledChats []string
	// cancelError 是停止并等待执行退出时返回的可选错误。
	cancelError error
}

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
		watchers: make(map[string]struct{}), scheduleLocks: make(map[string]*threadScheduleLock),
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
		watchers: make(map[string]struct{}), scheduleLocks: make(map[string]*threadScheduleLock),
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
		!strings.Contains(memberIntent.Input.Content, "[Assigned task]: 实现任务面板") ||
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
		events, listErr := threads.ListEvents(ctx, threadID, "", 100)
		if listErr != nil {
			t.Fatalf("list processing events: %v", listErr)
		}
		opened := false
		closed := false
		for _, event := range events {
			if event.Type != constants.ChannelEventTeamLeaderProcessing {
				continue
			}
			var processing leaderProcessingEvent
			if err := json.Unmarshal(event.Payload, &processing); err != nil {
				t.Fatalf("decode processing event: %v", err)
			}
			if processing.Active && processing.ExecutionID == "" {
				opened = true
			}
			if opened && !processing.Active && processing.ExecutionID == "" {
				closed = true
			}
		}
		if opened && closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("busy announce did not close its processing bridge; opened=%t closed=%t", opened, closed)
		}
		time.Sleep(10 * time.Millisecond)
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
	service := &service{
		channels:     channels,
		threads:      threads,
		tasks:        tasks,
		transactions: transactions,
		wakers:       wakers,
		chats:        chatFake,
		executions:   &postTurnExecutionFake{},
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
