package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNotificationFactsUnifyRequestsResultsAndUncoveredFailures(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	wakerID, chatID, approvalID := util.New(), util.New(), util.New()
	if err := db.Create(&models.Waker{ID: wakerID, Name: "通知伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	chat := models.Chat{ID: chatID, WakerID: wakerID, Title: "真实工作", ActiveExecutionID: util.New(), WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: approvalID, Title: "允许读取", RequestedAt: 5}}}
	if err := db.Create(&chat).Error; err != nil {
		t.Fatal(err)
	}
	results := NewWorkResultController(db)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := results.Publish(ctx, models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: chatID, LatestVersion: 1, AvailableAt: 10, Reference: models.WorkResultReference{ChatID: chatID, MessageID: util.New(), ExecutionID: util.New()}}); err != nil {
		t.Fatal(err)
	}
	task := automaticTaskFixture(wakerID)
	run := models.AutomaticRun{ID: util.New(), TaskID: task.ID, WakerID: wakerID, OccurrenceKey: "failed-before-chat", Admission: constants.AutomaticRunAdmissionFailed, Configuration: task.AutomaticTaskConfiguration, CreatedAt: 20, UpdatedAt: 20}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	notices := NewNotificationController(db)
	facts, err := notices.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if facts.ActionCount != 1 || facts.UnreadResultCount != 1 || facts.UnreadFailureCount != 1 {
		t.Fatalf("facts=%+v", facts)
	}
	if facts.ActionPreview[0].ID != "approval:"+approvalID || facts.ActionPreview[0].Target.RequestID != approvalID {
		t.Fatalf("request identity=%+v", facts.ActionPreview)
	}
	items, total, err := notices.List(ctx, NotificationQuery{Bucket: "unread", Limit: 1})
	if err != nil || total != 2 || len(items) != 2 || items[0].Kind != "automatic_failure" {
		t.Fatalf("page=%+v count=%d err=%v", items, total, err)
	}
	if err := NewAttentionNoticeController(db).Dismiss(ctx, "approval:"+approvalID, 30); err != nil {
		t.Fatal(err)
	}
	item, err := notices.Get(ctx, "approval:"+approvalID)
	if err != nil || !item.Dismissed {
		t.Fatalf("dismissed=%+v err=%v", item, err)
	}
	tx := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	if err := tx.Transaction(ctx, func(ctx context.Context) error {
		if err := results.ClearUnread(ctx, 40); err != nil {
			return err
		}
		return notices.ClearFailures(ctx, 40)
	}); err != nil {
		t.Fatal(err)
	}
	facts, err = notices.Snapshot(ctx)
	if err != nil || facts.ActionCount != 1 || facts.UnreadResultCount+facts.UnreadFailureCount != 0 {
		t.Fatalf("clear=%+v err=%v", facts, err)
	}
	if err := results.Publish(ctx, models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: chatID, LatestVersion: 2, AvailableAt: 50, Reference: models.WorkResultReference{ChatID: chatID}}); err != nil {
		t.Fatal(err)
	}
	facts, err = notices.Snapshot(ctx)
	if err != nil || facts.UnreadResultCount != 1 {
		t.Fatalf("new delivery cleared=%+v %v", facts, err)
	}
}

func TestCommittedRepositoryPublisherDoesNotPublishRolledBackWrites(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	db := WithEventPublisher(newTestDB(t), hub)
	repository := NewWakerController(db)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	subscription := hub.Subscribe("entities", events.Resource{Kind: "global"})
	defer subscription.Close()
	failed := errors.New("rollback")
	if err := transactions.Transaction(context.Background(), func(ctx context.Context) error {
		if err := repository.Create(ctx, models.Waker{ID: util.New(), Name: "回滚伙伴"}); err != nil {
			return err
		}
		if len(subscription.Events) != 0 {
			t.Fatal("event visible before commit")
		}
		return failed
	}); err == nil {
		t.Fatal("rollback unexpectedly succeeded")
	}
	if len(subscription.Events) != 0 {
		t.Fatal("rollback emitted successful change")
	}
	if err := repository.Create(context.Background(), models.Waker{ID: util.New(), Name: "提交伙伴"}); err != nil {
		t.Fatal(err)
	}
	if len(subscription.Events) != 1 {
		t.Fatal("committed independent write did not publish")
	}
}

func TestWorkflowResultLinkPublishesAfterCommitWithoutWorkerReceipt(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	db := WithEventPublisher(newTestDB(t), hub)
	ctx := context.Background()
	run := models.WorkflowRun{ID: util.New(), DefinitionID: util.New(), Status: constants.WorkflowRunStatusRunning, Version: 1, LeaseOwner: "runner", Fence: 1, LeaseUntil: 10000}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	effect := models.WorkflowEffect{ID: util.New(), RunID: run.ID, ChatID: util.New(), Status: constants.WorkflowStepStatusRunning}
	if err := db.Create(&effect).Error; err != nil {
		t.Fatal(err)
	}
	stream := hub.Subscribe("work-board", events.Resource{Kind: "chat", ID: effect.ChatID})
	defer stream.Close()
	tx := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	runs := NewWorkflowRunController(db)
	effect.Status, effect.FinalMessageID = constants.WorkflowStepStatusSucceeded, util.New()
	if err := tx.Transaction(ctx, func(ctx context.Context) error {
		if err := runs.CommitRunState(ctx, WorkflowRunState{Run: run, Effects: []models.WorkflowEffect{effect}}, 1, 100); err != nil {
			return err
		}
		if len(stream.Events) != 0 {
			t.Fatal("uncommitted result link signaled")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(stream.Events) != 1 {
		t.Fatalf("missing exact worker result signal: %d", len(stream.Events))
	}
	links, err := runs.ListChatAttempts(ctx, effect.ChatID)
	if err != nil || len(links) != 1 || links[0].FinalMessageID != effect.FinalMessageID {
		t.Fatalf("linked exact result not persisted: %+v %v", links, err)
	}
	if _, found, err := NewWorkResultController(db).Get(ctx, constants.WorkResultOwnerKindChat, effect.ChatID); err != nil || found {
		t.Fatalf("worker linkage fabricated user result receipt: %v %v", found, err)
	}
	// 同样事实再次提交不能成为新成果变化。
	<-stream.Events
	run.Version = 2
	if err := tx.Transaction(ctx, func(ctx context.Context) error {
		return runs.CommitRunState(ctx, WorkflowRunState{Run: run, Effects: []models.WorkflowEffect{effect}}, 2, 101)
	}); err != nil {
		t.Fatal(err)
	}
	if len(stream.Events) != 0 {
		t.Fatal("same result link replayed new change")
	}
}

// notificationQueryLog 记录实际查询数和返回行数，验证候选规模增长不会产生逐行反查。
type notificationQueryLog struct {
	// Interface 复用其余日志方法。
	logger.Interface
	// Statements 保存本次读取的实际 SQL。
	Statements []string
	// Rows 保存对应 SQL 返回到应用的行数，不代表数据库内部扫描次数。
	Rows []int64
}

// LogMode 保留观测器，防止仓储切换 Silent 时丢失查询次数与返回行数。
func (l *notificationQueryLog) LogMode(logger.LogLevel) logger.Interface { return l }

func (l *notificationQueryLog) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, rows := fc()
	l.Statements = append(l.Statements, sql)
	l.Rows = append(l.Rows, rows)
}

func TestNotificationQueriesBatchRelationsAndIgnoreHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	wakerID := util.New()
	if err := db.Create(&models.Waker{ID: wakerID, Name: "批量伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	pending := []models.Chat{}
	for i := 0; i < 300; i++ {
		id := fmt.Sprintf("pending-%04d", i)
		pending = append(pending, models.Chat{ID: id, WakerID: wakerID, Title: "100%_example", ActiveExecutionID: "execution-" + id, WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: "request-" + id, Title: "允许", RequestedAt: 10}}})
	}
	if err := db.CreateInBatches(&pending, 50).Error; err != nil {
		t.Fatal(err)
	}
	history := []models.Chat{}
	for i := 0; i < 2000; i++ {
		history = append(history, models.Chat{ID: fmt.Sprintf("history-%04d", i), WakerID: wakerID, Title: "历史"})
	}
	if err := db.CreateInBatches(&history, 50).Error; err != nil {
		t.Fatal(err)
	}
	log := &notificationQueryLog{Interface: logger.Discard}
	controller := NewNotificationController(db.Session(&gorm.Session{Logger: log}))
	page, count, err := controller.List(ctx, NotificationQuery{Bucket: "action", Limit: 5, Query: "100%_EXAMPLE"})
	if err != nil || count != 300 || len(page) != 6 {
		t.Fatalf("page=%d count=%d err=%v", len(page), count, err)
	}
	if page[0].Executor.Name != "批量伙伴" || page[0].Source != "chat" {
		t.Fatalf("metadata=%+v", page[0])
	}
	if len(log.Statements) > 40 {
		t.Fatalf("N+1 query growth: %d", len(log.Statements))
	}
	for i, sql := range log.Statements {
		if strings.Contains(strings.ToUpper(sql), " JOIN ") {
			t.Fatalf("unexpected join: %s", sql)
		}
		if log.Rows[i] > repositoryReadBatch {
			t.Fatalf("loaded unrelated history: rows=%d SQL=%s", log.Rows[i], sql)
		}
	}
	cursor := page[4]
	next, nextCount, err := controller.List(ctx, NotificationQuery{Bucket: "action", Limit: 5, CursorAt: cursor.CreatedAt, CursorID: cursor.ID})
	if err != nil || nextCount != 300 || len(next) != 6 || next[0].ID != page[5].ID {
		t.Fatalf("cursor page=%+v count=%d err=%v", next, nextCount, err)
	}
	none, count, err := controller.List(ctx, NotificationQuery{Bucket: "action", Query: "100%Xexample"})
	if err != nil || count != 0 || len(none) != 0 {
		t.Fatalf("literal wildcard filter=%+v %d %v", none, count, err)
	}
	// 第二批已读记录必须实际返回，不能因复用 GORM 条件而叠加前一批的 IN。
	ids := []string{}
	for _, chat := range pending {
		ids = append(ids, chat.ID)
	}
	rows, err := readRelatedRows[models.Chat](db.Where("waker_id = ?", wakerID), "id", "id", ids)
	if err != nil || len(rows) != 300 {
		t.Fatalf("batched conditions leaked: %d %v", len(rows), err)
	}
}

func TestNotificationUnreadPaginationAndLatestFailure(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	wakerID := util.New()
	if err := db.Create(&models.Waker{ID: wakerID, Name: "结果伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("result-chat-%02d", i)
		if err := db.Create(&models.Chat{ID: id, WakerID: wakerID, Title: "结果"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: id, LatestVersion: 2, AvailableAt: 100, Reference: models.WorkResultReference{ChatID: id, MessageID: "message-" + id}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	task := automaticTaskFixture(wakerID)
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	older := models.AutomaticRun{ID: "older-failure", TaskID: task.ID, WakerID: wakerID, OccurrenceKey: "old", Admission: constants.AutomaticRunAdmissionFailed, CreatedAt: 1, UpdatedAt: 100, Configuration: task.AutomaticTaskConfiguration}
	latest := older
	latest.ID = "latest-failure"
	latest.OccurrenceKey = "new"
	latest.CreatedAt = 2
	if err := db.Create(&[]models.AutomaticRun{older, latest}).Error; err != nil {
		t.Fatal(err)
	}
	controller := NewNotificationController(db)
	first, total, err := controller.List(ctx, NotificationQuery{Bucket: "unread", Limit: 4})
	if err != nil || total != 10 || len(first) != 5 {
		t.Fatalf("first=%+v total=%d err=%v", first, total, err)
	}
	cursor := first[3]
	next, total, err := controller.List(ctx, NotificationQuery{Bucket: "unread", Limit: 4, CursorAt: cursor.CreatedAt, CursorID: cursor.ID})
	if err != nil || total != 10 || len(next) != 5 || next[0].ID != first[4].ID {
		t.Fatalf("next=%+v total=%d err=%v", next, total, err)
	}
	offset, _, err := controller.List(ctx, NotificationQuery{Bucket: "unread", Limit: 4, Offset: 4})
	if err != nil || offset[0].ID != next[0].ID {
		t.Fatalf("offset=%+v err=%v", offset, err)
	}
	item, err := controller.Get(ctx, first[0].ID)
	if err != nil || item.Target.Reference == nil || item.Target.Reference.MessageID == "" {
		t.Fatalf("exact reference=%+v err=%v", item, err)
	}
	if _, err := controller.Get(ctx, "automatic-run-failed:"+older.ID); err == nil {
		t.Fatal("historical failure leaked")
	}
	// 失败分别位于成果之前和之后时，所有跳页位置都必须等价于完整稳定序列。
	for _, failureAt := range []int64{50, 150} {
		if err := db.Model(&models.AutomaticRun{}).Where("id = ?", latest.ID).Update("updated_at", failureAt).Error; err != nil {
			t.Fatal(err)
		}
		all, _, err := controller.List(ctx, NotificationQuery{Bucket: "unread", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for offset := 0; offset <= 12; offset++ {
			page, count, err := controller.List(ctx, NotificationQuery{Bucket: "unread", Offset: offset, Limit: 2})
			start := min(offset, len(all))
			expected := all[start:min(start+3, len(all))]
			if err != nil || count != 10 || len(page) != len(expected) {
				t.Fatalf("offset=%d rows=%d want=%d count=%d err=%v", offset, len(page), len(expected), count, err)
			}
			for i := range page {
				if page[i].ID != expected[i].ID {
					t.Fatalf("offset=%d row=%d got=%s want=%s", offset, i, page[i].ID, expected[i].ID)
				}
			}
		}
	}
	// 当前运行优先于按时间选择的最新运行。
	if err := db.Model(&models.AutomaticTask{}).Where("id = ?", task.ID).Update("active_run_id", older.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Get(ctx, "automatic-run-failed:"+older.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.AutomaticRun{}).Where("id = ?", older.ID).Update("configuration", "{}").Error; err != nil {
		t.Fatal(err)
	}
	fallback, err := controller.Get(ctx, "automatic-run-failed:"+older.ID)
	if err != nil || fallback.Title != task.Name {
		t.Fatalf("missing frozen name fallback=%+v %v", fallback, err)
	}
	if err := controller.ClearFailures(ctx, 123); err != nil {
		t.Fatal(err)
	}
	facts, err := controller.Snapshot(ctx)
	if err != nil || facts.UnreadFailureCount != 0 || facts.UnreadResultCount != 9 {
		t.Fatalf("clear facts=%+v %v", facts, err)
	}
}

func TestNotificationQueryPlansUseCandidateIndexes(t *testing.T) {
	db := newTestDB(t)
	plan := func(sql string) string {
		t.Helper()
		var rows []struct {
			// Detail 是 SQLite 查询计划的一步说明。
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN " + sql).Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		details := []string{}
		for _, row := range rows {
			details = append(details, row.Detail)
		}
		return strings.Join(details, "\n")
	}
	pending := plan("SELECT id FROM chats WHERE active_execution_id<>'' AND (waiting_approval=1 OR waiting_elicitation=1)")
	if !strings.Contains(pending, "idx_chats_waiting_") && !strings.Contains(pending, "idx_chat_pending_id") {
		t.Fatalf("pending requests must use waiting indexes: %s", pending)
	}
	latest := plan("SELECT id FROM automatic_runs WHERE task_id='task' ORDER BY created_at DESC,id DESC LIMIT 1")
	if !strings.Contains(latest, "idx_automatic_run_latest") || strings.Contains(latest, "TEMP B-TREE") {
		t.Fatalf("latest run requires indexed seek: %s", latest)
	}
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		board := &workBoardController{db: tx}
		return board.resultBranch(context.Background(), WorkBoardQuery{}, workBoardChatSelect, constants.WorkResultOwnerKindChat).Select("board.owner_id,board.result_available_at").Order("board.result_available_at DESC,board.owner_id DESC").Limit(6).Find(&[]models.WorkBoardEntry{})
	})
	results := plan(sql)
	if strings.Contains(results, "SCAN chat") || !strings.Contains(results, "SEARCH chat USING INDEX") || !strings.Contains(results, "idx_work_result_unread") || strings.Contains(results, "TEMP B-TREE") {
		t.Fatalf("unread candidates must not scan chat history: %s", results)
	}
	failureSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return (&notificationController{db: tx}).failureBase(context.Background(), NotificationQuery{ExecutorID: "actor"}, nil).Select("id,updated_at").Limit(6).Find(&[]models.AutomaticRun{})
	})
	failurePlan := plan(failureSQL)
	if strings.Contains(failurePlan, "SCAN automatic_runs") || !strings.Contains(failurePlan, "idx_automatic_run_latest") || !strings.Contains(failurePlan, "SEARCH automatic_tasks USING INDEX sqlite_autoindex_automatic_tasks_1 (id=?)") {
		t.Fatalf("failure scope must seek task/run identities: %s", failurePlan)
	}
	activeRuns := plan("SELECT revision_id FROM workflow_runs WHERE status IN ('queued','running','waiting','recovering','cancelling')")
	if strings.Contains(activeRuns, "SCAN workflow_runs") || !strings.Contains(activeRuns, "SEARCH workflow_runs USING INDEX idx_workflow_runs_status") {
		t.Fatalf("IM dependencies must not scan terminal run history: %s", activeRuns)
	}
	t.Logf("latest failure plan:\n%s\nunread candidates plan:\n%s", latest, results)
}

// 历史整理收据不再进入铃铛、未读计数或通知分页。
func TestLegacyKnowledgeDocumentResultsAreExcludedFromNotifications(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	for _, state := range []string{"processed", "failed"} {
		row := models.WorkResultReceipt{OwnerKind: "knowledge_source", OwnerID: "source-" + state, LatestVersion: 3, AvailableAt: 10, Reference: models.WorkResultReference{}}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		// 模拟旧版本写入的 JSON；新模型不再声明这些交付字段。
		if err := db.Exec("UPDATE work_result_receipts SET reference = ? WHERE owner_kind = ? AND owner_id = ?", `{"knowledgeSourceState":"`+state+`","knowledgeSourceName":"资料.md"}`, "knowledge_source", row.OwnerID).Error; err != nil {
			t.Fatal(err)
		}
	}
	notices := NewNotificationController(db)
	facts, err := notices.Snapshot(ctx)
	if err != nil || facts.UnreadResultCount != 0 || facts.UnreadFailureCount != 0 || len(facts.UnreadPreview) != 0 {
		t.Fatalf("整理收据仍产生通知: %+v %v", facts, err)
	}
	for _, source := range []string{"", "knowledge"} {
		page, total, err := notices.List(ctx, NotificationQuery{Bucket: "unread", Source: source, Limit: 10})
		if err != nil || total != 0 || len(page) != 0 {
			t.Fatalf("page=%+v total=%d err=%v", page, total, err)
		}
	}
}
