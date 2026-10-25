package notification

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// notificationFixture 使用真实临时 SQLite、领域仓储和事件端口验证用户行为。
type notificationFixture struct {
	// database 保存实际领域投影与用户回执，不替换通知查询实现。
	database *gorm.DB
	// hub 路由提交后的实际领域事件与通知更新。
	hub *events.Hub
	// service 拥有待验证的全局聚合生命周期。
	service *Service
	// chats 用真实 CAS 投影接纳或解决请求。
	chats controllers.ChatController
	// results 发布原结果引用和确认版本。
	results controllers.WorkResultController
	// transactions 提供与生产相同的提交后发布边界。
	transactions controllers.TransactionController
	// wakerID 是这些独立测试工作的真实执行伙伴。
	wakerID string
	// reads 统计数据库查询，以观察空闲和流式消息是否触发摘要重算。
	reads atomic.Int64
}

func newNotificationFixture(t *testing.T) *notificationFixture {
	t.Helper()
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "notifications.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	hub := events.New()
	t.Cleanup(hub.Close)
	db := controllers.WithEventPublisher(database.DB, hub)
	fixture := &notificationFixture{database: db, hub: hub, wakerID: util.New(), chats: controllers.NewChatController(db), results: controllers.NewWorkResultController(db), transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db))}
	if err := db.Create(&models.Waker{ID: fixture.wakerID, Name: "通知测试伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:notification-reads", func(_ *gorm.DB) { fixture.reads.Add(1) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register("test:notification-row-reads", func(_ *gorm.DB) { fixture.reads.Add(1) }); err != nil {
		t.Fatal(err)
	}
	fixture.service = New(controllers.NewNotificationController(db), fixture.results, fixture.transactions, hub)
	t.Cleanup(fixture.service.Close)
	return fixture
}

func (f *notificationFixture) addChat(t *testing.T, at int64, withApproval bool) (models.Chat, string) {
	t.Helper()
	chat := models.Chat{ID: util.New(), WakerID: f.wakerID, Title: "真实用户工作", ActiveExecutionID: util.New()}
	approvalID := util.New()
	if withApproval {
		chat.WaitingApproval = true
		chat.PendingApprovals = []models.PendingApproval{{ApprovalID: approvalID, Title: "允许读取文件", RequestedAt: at}}
	}
	if err := f.chats.Create(context.Background(), chat); err != nil {
		t.Fatal(err)
	}
	return chat, approvalID
}

func (f *notificationFixture) start(t *testing.T) events.Subscription {
	t.Helper()
	if err := f.service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	subscription := f.hub.Subscribe("notifications", events.Resource{Kind: "global"})
	t.Cleanup(subscription.Close)
	return subscription
}

func (f *notificationFixture) approval(t *testing.T, ctx context.Context, chat models.Chat, sequence int64, approvalID string, at int64) error {
	t.Helper()
	pending := []models.PendingApproval{}
	if approvalID != "" {
		pending = append(pending, models.PendingApproval{ApprovalID: approvalID, Title: "允许读取文件", RequestedAt: at})
	}
	encoded, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	return f.chats.AdvanceProjection(ctx, chat.ID, sequence, map[string]any{"waiting_approval": approvalID != "", "pending_approvals": string(encoded), "latest_sequence": sequence + 1})
}

func nextNotificationUpdate(t *testing.T, subscription events.Subscription, matches func(Update) bool) Update {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, open := <-subscription.Events:
			if !open {
				t.Fatal("notification subscription closed before required update")
			}
			if event.Type != "notifications.updated" {
				t.Fatalf("unexpected notification frame: %s", event.Type)
			}
			var update Update
			if err := json.Unmarshal(event.Payload, &update); err != nil {
				t.Fatal(err)
			}
			if matches(update) {
				return update
			}
		case <-deadline.C:
			t.Fatal("committed user-visible state did not produce notification update")
		}
	}
}

func notificationIDPresent(items []models.NotificationItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestNotificationStartupCountsAllRequestsAndNewRequestOutsidePreview(t *testing.T) {
	fixture := newNotificationFixture(t)
	chats, approvals := []models.Chat{}, []string{}
	for index := 0; index < 6; index++ {
		chat, approvalID := fixture.addChat(t, int64(index+1), true)
		chats, approvals = append(chats, chat), append(approvals, approvalID)
	}
	seventh, seventhApproval := fixture.addChat(t, 7, false)
	subscription := fixture.start(t)
	initial, err := fixture.service.Snapshot()
	if err != nil || initial.ActionCount != 6 || initial.TotalCount != 6 || len(initial.ActionPreview) != 5 {
		t.Fatalf("startup count/preview=%+v err=%v", initial, err)
	}
	if notificationIDPresent(initial.ActionPreview, "approval:"+approvals[5]) {
		t.Fatal("sixth existing request unexpectedly fit five-item preview")
	}
	if len(subscription.Events) != 0 {
		t.Fatal("startup replayed existing requests as new pushes")
	}
	if err := fixture.approval(t, context.Background(), seventh, 0, seventhApproval, 7); err != nil {
		t.Fatal(err)
	}
	created := nextNotificationUpdate(t, subscription, func(update Update) bool { return update.Snapshot.ActionCount == 7 })
	newID := "approval:" + seventhApproval
	if len(created.Snapshot.ActionPreview) != 5 || notificationIDPresent(created.Snapshot.ActionPreview, newID) {
		t.Fatal("newest request must remain outside earliest-five preview")
	}
	if len(created.Upserts) != 1 || created.Upserts[0].ID != newID || created.Upserts[0].Target.RequestID != seventhApproval {
		t.Fatalf("out-of-preview real identity was lost: %+v", created.Upserts)
	}
	if created.Snapshot.Revision <= initial.Revision || created.Snapshot.Epoch != initial.Epoch {
		t.Fatal("snapshot version did not monotonically advance in Core epoch")
	}
	// 解决第一项只改变预览位置；原第六项进入预览不会成为新通知。
	if err := fixture.approval(t, context.Background(), chats[0], 0, "", 0); err != nil {
		t.Fatal(err)
	}
	moved := nextNotificationUpdate(t, subscription, func(update Update) bool { return update.Snapshot.ActionCount == 6 })
	if !notificationIDPresent(moved.Snapshot.ActionPreview, "approval:"+approvals[5]) || len(moved.Upserts) != 0 {
		t.Fatalf("preview movement created a false new notification: %+v", moved)
	}
	if len(moved.Removals) != 1 || moved.Removals[0] != "approval:"+approvals[0] {
		t.Fatalf("request resolution lost its exact removal: %v", moved.Removals)
	}
}

func TestNotificationDismissalAndObservedVersionKeepNewDeliveryUnread(t *testing.T) {
	fixture := newNotificationFixture(t)
	_, approvalID := fixture.addChat(t, 1, true)
	resultChat, _ := fixture.addChat(t, 2, false)
	if err := fixture.results.EnableTracking(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	publish := func(version int64) models.WorkResultReceipt {
		t.Helper()
		receipt := models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: resultChat.ID, LatestVersion: version, AvailableAt: 10 + version, Reference: models.WorkResultReference{ChatID: resultChat.ID, MessageID: util.New(), ExecutionID: util.New()}}
		if err := fixture.results.Publish(context.Background(), receipt); err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	old := publish(1)
	subscription := fixture.start(t)
	if err := controllers.NewAttentionNoticeController(fixture.database).Dismiss(context.Background(), "approval:"+approvalID, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	dismissed := nextNotificationUpdate(t, subscription, func(update Update) bool {
		return len(update.Snapshot.ActionPreview) > 0 && update.Snapshot.ActionPreview[0].Dismissed
	})
	if dismissed.Snapshot.ActionCount != 1 || dismissed.Snapshot.UnreadResultCount != 1 || dismissed.Snapshot.TotalCount != 2 {
		t.Fatalf("dismissing hint changed attention count: %+v", dismissed.Snapshot)
	}
	latest := publish(2)
	delivered := nextNotificationUpdate(t, subscription, func(update Update) bool {
		return notificationIDPresent(update.Upserts, "result:chat:"+resultChat.ID+":2")
	})
	if delivered.Snapshot.UnreadResultCount != 1 {
		t.Fatal("one result owner became duplicate unread results")
	}
	if err := fixture.service.ViewResult(context.Background(), old.OwnerKind, old.OwnerID, 1, old.Reference); err != nil {
		t.Fatal(err)
	}
	receipt, found, err := fixture.results.Get(context.Background(), latest.OwnerKind, latest.OwnerID)
	if err != nil || !found || receipt.LatestVersion != 2 || receipt.ViewedVersion >= 2 {
		t.Fatalf("old observed result confirmed new delivery: %+v %v", receipt, err)
	}
	if err := fixture.service.ViewResult(context.Background(), latest.OwnerKind, latest.OwnerID, 2, latest.Reference); err != nil {
		t.Fatal(err)
	}
	read := nextNotificationUpdate(t, subscription, func(update Update) bool { return update.Snapshot.UnreadResultCount == 0 })
	if read.Snapshot.ActionCount != 1 || read.Snapshot.TotalCount != 1 {
		t.Fatal("reading result processed pending approval")
	}
}

func TestNotificationIdleSnapshotsAndTransientMessagesDoNotQueryFacts(t *testing.T) {
	fixture := newNotificationFixture(t)
	fixture.addChat(t, 1, true)
	subscription := fixture.start(t)
	initial, _ := fixture.service.Snapshot()
	reads := fixture.reads.Load()
	if reads == 0 {
		t.Fatal("read instrumentation missed actual initial repository snapshot")
	}
	for index := 0; index < 50; index++ {
		if _, err := fixture.service.Snapshot(); err != nil {
			t.Fatal(err)
		}
		fixture.hub.Publish(events.Event{Topic: "chat", Type: "chat.event", Resource: events.Resource{Kind: "chat", ID: "active-chat"}, Transient: true, Payload: json.RawMessage(`{"type":"message_chunk","text":"temporary"}`)})
	}
	// 覆盖多个 250ms 聚合窗口，空闲与 token 变化都不能变成数据库定时扫描。
	timer := time.NewTimer(1100 * time.Millisecond)
	defer timer.Stop()
	select {
	case update := <-subscription.Events:
		t.Fatalf("idle/transient emitted notification: %s", update.Type)
	case <-timer.C:
	}
	current, _ := fixture.service.Snapshot()
	if fixture.reads.Load() != reads || current.Revision != initial.Revision {
		t.Fatalf("idle/transient rebuilt database summary: reads=%d->%d revision=%d->%d", reads, fixture.reads.Load(), initial.Revision, current.Revision)
	}
}

func TestNotificationRollbackAndFailedProjectionDoNotPublish(t *testing.T) {
	fixture := newNotificationFixture(t)
	chat, approvalID := fixture.addChat(t, 1, false)
	subscription := fixture.start(t)
	sources := fixture.hub.Subscribe("", events.Resource{Kind: "global"})
	defer sources.Close()
	err := fixture.transactions.Transaction(context.Background(), func(ctx context.Context) error {
		if err := fixture.approval(t, ctx, chat, 0, approvalID, 1); err != nil {
			return err
		}
		if len(sources.Events) != 0 {
			t.Fatal("uncommitted projection notified consumers")
		}
		return status_error.BadRequest
	})
	if !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("expected explicit rollback: %v", err)
	}
	if len(sources.Events) != 0 {
		t.Fatal("rolled-back projection emitted source event")
	}
	if err := fixture.approval(t, context.Background(), chat, 99, approvalID, 1); !errors.Is(err, status_error.Conflict) {
		t.Fatalf("stale projection CAS unexpectedly accepted: %v", err)
	}
	if len(sources.Events) != 0 {
		t.Fatal("failed CAS emitted new request identity")
	}
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case update := <-subscription.Events:
		t.Fatalf("uncommitted change produced notification: %s", update.Type)
	case <-timer.C:
	}
	current, err := fixture.service.Snapshot()
	saved, readErr := fixture.chats.Get(context.Background(), chat.ID)
	if err != nil || readErr != nil || current.TotalCount != 0 || saved.WaitingApproval || saved.LatestSequence != 0 {
		t.Fatalf("rollback changed public/authoritative state: snapshot=%+v chat=%+v errors=%v/%v", current, saved, err, readErr)
	}
}

func TestNotificationCloseStopsAggregationWithoutDeletingFacts(t *testing.T) {
	fixture := newNotificationFixture(t)
	chat, _ := fixture.addChat(t, 1, false)
	subscription := fixture.start(t)
	initial, _ := fixture.service.Snapshot()
	fixture.service.Close()
	if err := fixture.approval(t, context.Background(), chat, 0, util.New(), 1); err != nil {
		t.Fatal(err)
	}
	reads := fixture.reads.Load()
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case update := <-subscription.Events:
		t.Fatalf("closed Core aggregation published update: %s", update.Type)
	case <-timer.C:
	}
	current, _ := fixture.service.Snapshot()
	if fixture.reads.Load() != reads || current.Revision != initial.Revision {
		t.Fatal("closed notification service continued querying/publishing")
	}
	saved, err := fixture.chats.Get(context.Background(), chat.ID)
	if err != nil || !saved.WaitingApproval || len(saved.PendingApprovals) != 1 {
		t.Fatal("closing aggregation changed original domain request")
	}
	// 通知关闭不删除原事实；恢复新 Core 后仍可从权威来源完整重建。
	nextHub := events.New()
	defer nextHub.Close()
	recoveredDB := controllers.WithEventPublisher(fixture.database, nextHub)
	fresh := New(controllers.NewNotificationController(recoveredDB), controllers.NewWorkResultController(recoveredDB), controllers.NewTransactionController(dbhelper.NewGormTransactionManager(recoveredDB)), nextHub)
	defer fresh.Close()
	if err := fresh.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := fresh.Snapshot()
	if err != nil || recovered.Epoch == initial.Epoch || recovered.TotalCount != 1 || recovered.ActionPreview[0].ID != "approval:"+saved.PendingApprovals[0].ApprovalID {
		t.Fatalf("recovered snapshot lost stable request: %+v %v", recovered, err)
	}
}

func TestNotificationDatabaseFailureRetainsCountsAndPendingNewIdentity(t *testing.T) {
	fixture := newNotificationFixture(t)
	_, existingApproval := fixture.addChat(t, 1, true)
	chat, newApproval := fixture.addChat(t, 2, false)
	subscription := fixture.start(t)
	initial, err := fixture.service.Snapshot()
	if err != nil || !initial.Synced || initial.TotalCount != 1 {
		t.Fatalf("initial snapshot=%+v err=%v", initial, err)
	}
	var failQueries atomic.Bool
	if err := fixture.database.Callback().Query().Before("gorm:query").Register("test:controlled-notification-db-failure", func(query *gorm.DB) {
		if failQueries.Load() {
			query.AddError(status_error.DatabaseError)
		}
	}); err != nil {
		t.Fatal(err)
	}
	// 先提交真实新请求，再让聚合的实际数据库查询失败，不用替身通知仓储。
	if err := fixture.approval(t, context.Background(), chat, 0, newApproval, 2); err != nil {
		t.Fatal(err)
	}
	failQueries.Store(true)
	stale := nextNotificationUpdate(t, subscription, func(update Update) bool { return !update.Snapshot.Synced })
	if stale.Snapshot.TotalCount != 1 || stale.Snapshot.ActionCount != 1 || !notificationIDPresent(stale.Snapshot.ActionPreview, "approval:"+existingApproval) || len(stale.Upserts) != 0 || stale.Snapshot.Revision <= initial.Revision {
		t.Fatalf("failed read replaced established count or emitted unverified notice: %+v", stale)
	}
	cached, err := fixture.service.Snapshot()
	if err != nil || cached.Synced || cached.TotalCount != 1 {
		t.Fatalf("REST cache hid stale state: %+v %v", cached, err)
	}
	failQueries.Store(false)
	recovered := nextNotificationUpdate(t, subscription, func(update Update) bool { return update.Snapshot.Synced && update.Snapshot.TotalCount == 2 })
	if len(recovered.Upserts) != 1 || recovered.Upserts[0].ID != "approval:"+newApproval || recovered.Upserts[0].Target.RequestID != newApproval {
		t.Fatalf("failure/retry lost committed new request identity: %+v", recovered.Upserts)
	}
	if recovered.Snapshot.Revision <= stale.Snapshot.Revision || recovered.Snapshot.Epoch != initial.Epoch {
		t.Fatal("stale/recovered snapshots lost monotonic epoch/revision")
	}
	reads := fixture.reads.Load()
	timer := time.NewTimer(1100 * time.Millisecond)
	defer timer.Stop()
	select {
	case event := <-subscription.Events:
		t.Fatalf("recovered service continued failure retry: %s", event.Type)
	case <-timer.C:
	}
	if fixture.reads.Load() != reads {
		t.Fatal("recovery retained a database polling loop")
	}
}

func TestWorkflowCreationRequestsPersistUntilResolved(t *testing.T) {
	fixture := newNotificationFixture(t)
	ctx := context.Background()
	chat, approvalID := fixture.addChat(t, 1, false)
	definitionID, inputID := util.New(), util.New()
	for _, value := range []any{
		&models.WorkflowDefinition{ID: definitionID, Title: "创作工作流"},
		&models.WorkflowCreationSession{ID: util.New(), DefinitionID: definitionID, ChatID: chat.ID},
	} {
		if err := fixture.database.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.database.Model(&models.Chat{}).Where("id = ?", chat.ID).Update("hidden_from_waker_history", true).Error; err != nil {
		t.Fatal(err)
	}
	subscription := fixture.start(t)
	inputs, err := json.Marshal([]models.PendingElicitation{{ElicitationID: inputID, Message: "请选择用途", RequestedAt: 2}})
	if err != nil {
		t.Fatal(err)
	}
	approvals, err := json.Marshal([]models.PendingApproval{{ApprovalID: approvalID, Title: "允许操作", RequestedAt: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.chats.AdvanceProjection(ctx, chat.ID, 0, map[string]any{
		"waiting_approval": true, "pending_approvals": string(approvals),
		"waiting_elicitation": true, "pending_elicitations": string(inputs), "latest_sequence": int64(1),
	}); err != nil {
		t.Fatal(err)
	}
	update := nextNotificationUpdate(t, subscription, func(update Update) bool { return update.Snapshot.ActionCount == 2 })
	if len(update.Upserts) != 2 {
		t.Fatalf("创作请求没有实时通知: %+v", update)
	}
	facts := controllers.NewNotificationController(fixture.database)
	board := controllers.NewWorkBoardController(fixture.database)
	assertPending := func(want int64) {
		t.Helper()
		snapshot, err := facts.Snapshot(ctx)
		if err != nil || snapshot.ActionCount != want {
			t.Fatalf("通知待办数=%+v err=%v", snapshot, err)
		}
		for _, item := range snapshot.ActionPreview {
			if item.Source != "workflow" || item.Target.DefinitionID != definitionID || item.Target.ChatID != chat.ID || item.Target.RunID != "" || item.Executor.ID != definitionID || item.Executor.Kind != "workflow" {
				t.Fatalf("创作定位或归属错误: %+v", item)
			}
		}
		_, count, err := board.Attention(ctx, controllers.WorkBoardQuery{Source: "workflow", ExecutorID: definitionID, Limit: 10})
		if err != nil || count != want {
			t.Fatalf("需要操作数=%d want=%d err=%v", count, want, err)
		}
		_, _, owners, err := board.AttentionOverview(ctx, controllers.WorkBoardQuery{Limit: 5})
		if err != nil || (want > 0 && owners != 1) || (want == 0 && owners != 0) {
			t.Fatalf("待办工作归属数=%d err=%v", owners, err)
		}
	}
	assertPending(2)
	for _, id := range []string{"approval:" + approvalID, "elicitation:" + inputID} {
		if err := controllers.NewAttentionNoticeController(fixture.database).Dismiss(ctx, id, 10); err != nil {
			t.Fatal(err)
		}
		item, err := facts.Get(ctx, id)
		if err != nil || !item.Dismissed {
			t.Fatalf("提示关闭记录=%+v err=%v", item, err)
		}
	}
	if err := fixture.service.ClearUnread(ctx); err != nil {
		t.Fatal(err)
	}
	assertPending(2)
	if err := fixture.approval(t, ctx, chat, 1, "", 0); err != nil {
		t.Fatal(err)
	}
	nextNotificationUpdate(t, subscription, func(update Update) bool {
		return update.Snapshot.ActionCount == 1 && len(update.Removals) == 1 && update.Removals[0] == "approval:"+approvalID
	})
	assertPending(1)
	if err := fixture.chats.AdvanceProjection(ctx, chat.ID, 2, map[string]any{"waiting_elicitation": false, "pending_elicitations": "[]", "latest_sequence": int64(3)}); err != nil {
		t.Fatal(err)
	}
	nextNotificationUpdate(t, subscription, func(update Update) bool {
		return update.Snapshot.ActionCount == 0 && len(update.Removals) == 1 && update.Removals[0] == "elicitation:"+inputID
	})
	assertPending(0)
}

func TestWorkflowCreationLinkReconcilesEarlierRequest(t *testing.T) {
	fixture := newNotificationFixture(t)
	ctx := context.Background()
	chat, approvalID := fixture.addChat(t, 1, true)
	if err := fixture.database.Model(&models.Chat{}).Where("id = ?", chat.ID).Update("hidden_from_waker_history", true).Error; err != nil {
		t.Fatal(err)
	}
	session := models.WorkflowCreationSession{ID: util.New(), DefinitionID: util.New(), ActiveExecutionID: chat.ActiveExecutionID}
	creations := controllers.NewWorkflowCreationController(fixture.database)
	if err := fixture.database.Create(&models.WorkflowDefinition{ID: session.DefinitionID, Title: "先提问后绑定"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := creations.CreateCreationSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	subscription := fixture.start(t)
	initial, err := fixture.service.Snapshot()
	if err != nil || initial.ActionCount != 0 {
		t.Fatalf("未绑定隐藏聊天不应作为普通待办: %+v err=%v", initial, err)
	}
	session.ChatID = chat.ID
	if err := fixture.transactions.Transaction(ctx, func(ctx context.Context) error {
		if err := creations.UpdateCreationSession(ctx, session); err != nil {
			return err
		}
		return status_error.BadRequest
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("模拟绑定回滚失败: %v", err)
	}
	if len(subscription.Events) != 0 {
		t.Fatal("绑定回滚仍发布了通知")
	}
	if err := fixture.transactions.Transaction(ctx, func(ctx context.Context) error {
		return creations.UpdateCreationSession(ctx, session)
	}); err != nil {
		t.Fatal(err)
	}
	update := nextNotificationUpdate(t, subscription, func(update Update) bool { return update.Snapshot.ActionCount == 1 })
	if len(update.Upserts) != 1 || update.Upserts[0].ID != "approval:"+approvalID || update.Upserts[0].Target.DefinitionID != session.DefinitionID {
		t.Fatalf("首次绑定未补发原待办: %+v", update)
	}
}
