package channel

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// eventStreamChatFake 提供共享成员交互投影所需的可控 Chat 事件订阅。
type eventStreamChatFake struct {
	// Service 补齐本测试不关心的 Chat 用例。
	chatservice.Service
	// mutex 保护订阅调用计数和事件通道映射。
	mutex sync.Mutex
	// events 按成员 Chat 标识保存测试主动写入的公开事件流。
	events map[string]chan chatservice.PublicEvent
	// subscribeCalls 按成员 Chat 标识累计实际建立的订阅数。
	subscribeCalls map[string]int
}

// ListEvents 返回空的初始持久窗口，实时事实由测试事件通道提供。
func (f *eventStreamChatFake) ListEvents(
	context.Context,
	string,
	int64,
	int,
) ([]chatservice.PublicEvent, error) {
	return nil, nil
}

// SubscribeEvents 返回一个由测试控制且随 watcher Context 退出的事件流。
func (f *eventStreamChatFake) SubscribeEvents(
	_ context.Context,
	chatID string,
	_ int64,
) (chatservice.Subscription, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.subscribeCalls[chatID]++
	return chatservice.Subscription{
		Events: f.events[chatID],
		Close:  func() {},
	}, nil
}

// callCount 返回一个成员 Chat 已建立的实时订阅数。
func (f *eventStreamChatFake) callCount(chatID string) int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.subscribeCalls[chatID]
}

func TestChannelEventTransactionPublishesOnlyAfterCommit(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "channel-event-transaction.db"), Migrate: true,
	})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	threads := controllers.NewChannelThreadController(database.DB)
	transactions := controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	thread := models.ChannelThread{
		ID: "11111111111111111111111111111111", ChannelID: "22222222222222222222222222222222",
		Title: "事务事件", LeadWakerID: "lead", LeadChatID: "lead-chat",
		WorkspaceDirectory: t.TempDir(), MaxParallel: 1, MaxTasks: 2, MaxFailureAttempts: 1,
	}
	if err := threads.Create(context.Background(), thread, []models.ChannelThreadMember{{
		ThreadID: thread.ID, WakerID: "lead", ChatID: "lead-chat", IsLead: true,
		RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault,
	}}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	service := &service{
		threads: threads, transactions: transactions, events: newChannelEventBroker(),
	}
	subscriberID, wakeups, _, _ := service.events.add(thread.ID)
	defer service.events.remove(thread.ID, subscriberID)
	if err := service.transaction(context.Background(), func(transactionContext context.Context) error {
		_, appendErr := service.appendEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTaskCreated,
			map[string]string{"taskId": "committed"},
		)
		return appendErr
	}); err != nil {
		t.Fatalf("commit event transaction: %v", err)
	}
	select {
	case <-wakeups:
	case <-time.After(time.Second):
		t.Fatal("committed event did not wake broker subscriber")
	}
	rollbackCause := errors.New("rollback channel event")
	if err := service.transaction(context.Background(), func(transactionContext context.Context) error {
		if _, appendErr := service.appendEvent(
			transactionContext,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTaskCreated,
			map[string]string{"taskId": "rolled-back"},
		); appendErr != nil {
			return appendErr
		}
		return rollbackCause
	}); err == nil {
		t.Fatal("rollback event transaction unexpectedly succeeded")
	}
	select {
	case <-wakeups:
		t.Fatal("rolled-back event must not wake broker subscriber")
	case <-time.After(30 * time.Millisecond):
	}
	events, err := threads.ListEvents(context.Background(), thread.ID, "", 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("persisted events = %+v, error = %v, want only committed event", events, err)
	}
	// Team 子事务完成不等于外层提交，外层回滚不能留下任何实时唤醒。
	if err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
		if err := service.transaction(ctx, func(nested context.Context) error {
			_, err := service.appendEvent(nested, thread.ChannelID, thread.ID, constants.ChannelEventTaskCreated, map[string]string{"taskId": "nested-rollback"})
			return err
		}); err != nil {
			return err
		}
		select {
		case <-wakeups:
			t.Fatal("nested transaction woke subscriber before outer commit")
		default:
		}
		return status_error.BadRequest
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("outer rollback result = %v", err)
	}
	select {
	case <-wakeups:
		t.Fatal("outer rollback left a nested committed wakeup")
	default:
	}
	// 第一轮已暂存的通知随 busy 回滚；重试只唤醒实际提交的另一条线程。
	other := thread
	other.ID = "55555555555555555555555555555555"
	other.LeadChatID = "other-lead-chat"
	if err := threads.Create(context.Background(), other, []models.ChannelThreadMember{{
		ThreadID: other.ID, WakerID: "lead", ChatID: other.LeadChatID, IsLead: true,
		RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault,
	}}); err != nil {
		t.Fatal(err)
	}
	otherSubscriber, otherWakeups, _, _ := service.events.add(other.ID)
	defer service.events.remove(other.ID, otherSubscriber)
	attempts := 0
	if err := service.transaction(context.Background(), func(ctx context.Context) error {
		attempts++
		target := other.ID
		if attempts == 1 {
			target = thread.ID
		}
		if _, err := service.appendEvent(ctx, thread.ChannelID, target, constants.ChannelEventTaskCreated, map[string]string{"taskId": "retry"}); err != nil {
			return err
		}
		if attempts == 1 {
			return status_error.DatabaseBusy
		}
		return nil
	}); err != nil {
		t.Fatalf("retry event transaction: %v", err)
	}
	select {
	case <-wakeups:
		t.Fatal("rolled-back retry attempt must not wake its thread")
	default:
	}
	select {
	case <-otherWakeups:
	case <-time.After(time.Second):
		t.Fatal("committed retry did not wake its thread")
	}
	events, err = threads.ListEvents(context.Background(), thread.ID, "", 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("retry rollback left events: count=%d error=%v", len(events), err)
	}
}

func TestChannelEventSubscribersShareMemberInteractionProjection(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "channel-event-subscription.db"), Migrate: true,
	})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	threads := controllers.NewChannelThreadController(database.DB)
	thread := models.ChannelThread{
		ID: "33333333333333333333333333333333", ChannelID: "44444444444444444444444444444444",
		Title: "共享事件投影", LeadWakerID: "lead", LeadChatID: "lead-chat",
		WorkspaceDirectory: t.TempDir(), MaxParallel: 2, MaxTasks: 4, MaxFailureAttempts: 2,
	}
	members := []models.ChannelThreadMember{
		{
			ThreadID: thread.ID, WakerID: "lead", ChatID: "lead-chat", IsLead: true,
			RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault,
		},
		{
			ThreadID: thread.ID, WakerID: "member", ChatID: "member-chat", Position: 1,
			RuntimeID: "claude", PermissionLevel: constants.PermissionLevelDefault,
		},
	}
	if err := threads.Create(context.Background(), thread, members); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	chatFake := &eventStreamChatFake{
		events: map[string]chan chatservice.PublicEvent{
			"lead-chat":   make(chan chatservice.PublicEvent, 4),
			"member-chat": make(chan chatservice.PublicEvent, 4),
		},
		subscribeCalls: make(map[string]int),
	}
	lifecycleContext, cancelLifecycle := context.WithCancel(context.Background())
	service := &service{
		threads:            threads,
		chats:              chatFake,
		watchers:           make(map[string]struct{}),
		interactionCursors: make(map[string]int64),
		events:             newChannelEventBroker(),
		lifecycleContext:   lifecycleContext,
		lifecycleCancel:    cancelLifecycle,
		watcherPolicy: deliveryWatcherPolicy{
			maximumFailures: 2,
			initialDelay:    time.Millisecond,
			maximumDelay:    2 * time.Millisecond,
		},
	}
	t.Cleanup(func() {
		service.events.closeAll()
		cancelLifecycle()
		service.waitGroup.Wait()
	})
	first, err := service.SubscribeEvents(context.Background(), thread.ID, "")
	if err != nil {
		t.Fatalf("subscribe first Team stream: %v", err)
	}
	defer first.Close()
	second, err := service.SubscribeEvents(context.Background(), thread.ID, "")
	if err != nil {
		t.Fatalf("subscribe second Team stream: %v", err)
	}
	defer second.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) &&
		(chatFake.callCount("lead-chat") != 1 || chatFake.callCount("member-chat") != 1) {
		time.Sleep(time.Millisecond)
	}
	if chatFake.callCount("lead-chat") != 1 || chatFake.callCount("member-chat") != 1 {
		t.Fatalf(
			"member subscriptions = lead:%d member:%d, want one shared subscription each",
			chatFake.callCount("lead-chat"),
			chatFake.callCount("member-chat"),
		)
	}
	chatFake.events["member-chat"] <- chatservice.PublicEvent{
		Sequence: 1, RecordedAt: 100, Type: constants.ChatEventTypeApprovalRequested,
		ExecutionID: "execution", ApprovalRequest: &chatservice.PublicApprovalRequest{
			ApprovalID: "approval", ToolCallID: "tool", Title: "确认操作",
		},
	}
	assertChannelEventType(t, first.Events, constants.ChannelEventApprovalRequested)
	assertChannelEventType(t, second.Events, constants.ChannelEventApprovalRequested)
	first.Close()
	chatFake.events["member-chat"] <- chatservice.PublicEvent{
		Sequence: 2, RecordedAt: 110, Type: constants.ChatEventTypeApprovalResolved,
		ExecutionID: "execution", ApprovalResolution: &chatservice.PublicApprovalResolution{
			ApprovalID: "approval", ToolCallID: "tool", Result: constants.ApprovalResultSelected,
		},
	}
	assertChannelEventType(t, second.Events, constants.ChannelEventApprovalResolved)
	if chatFake.callCount("member-chat") != 1 {
		t.Fatalf("member subscription count after first client close = %d, want 1", chatFake.callCount("member-chat"))
	}
}

// assertChannelEventType 等待一个 Team 事件并校验其稳定类型。
func assertChannelEventType(
	t *testing.T,
	events <-chan models.ChannelEvent,
	want constants.ChannelEventType,
) {
	t.Helper()
	select {
	case event, open := <-events:
		if !open {
			t.Fatalf("Team event stream closed before %s", want)
		}
		if event.Type != want {
			t.Fatalf("Team event type = %s, want %s", event.Type, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for Team event %s", want)
	}
}
