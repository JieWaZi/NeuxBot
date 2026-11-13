package workspaceattention

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAttentionMultipleChats(t *testing.T) {
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "attention.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	reader := controllers.NewChatReadingController(db.DB)
	q := Query{Board: controllers.NewWorkBoardController(db.DB), Transactions: tx, Epoch: func() string { return "test-core" }}
	first, second, running := "a-first", "b-second", "c-running"
	chats := []models.Chat{
		{ID: first, WakerID: "waker", Title: "First", LatestReadableSequence: 3},
		{ID: second, WakerID: "waker", Title: "Second", LatestReadableSequence: 4, ActiveExecutionID: "waiting", WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: "first-request", RequestedAt: 5}, {ApprovalID: "second-request", RequestedAt: 6}}},
		{ID: running, WakerID: "waker", Title: "Running", ActiveExecutionID: "running"},
	}
	if err := db.DB.Create(&chats).Error; err != nil {
		t.Fatal(err)
	}
	for _, chat := range chats {
		if err := reader.EnsureCursor(ctx, constants.LocalReaderID, chat.ID, 0); err != nil {
			t.Fatal(err)
		}
		values := []models.ChatReadableMessage{}
		for i := int64(2); i <= chat.LatestReadableSequence; i++ {
			values = append(values, models.ChatReadableMessage{ChatID: chat.ID, MessageID: fmt.Sprint(i), Kind: "answer", OrderSequence: i, CreatedAt: i})
		}
		if _, err := reader.Project(ctx, chat.ID, nil, values, false); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	summaries, complete, err := q.ExecutorSummaries(ctx, constants.LocalReaderID, []string{"waker"})
	if err != nil {
		t.Fatal(err)
	}
	want := models.AttentionMetrics{UnreadMessageCount: 5, UnreadChatCount: 2, WaitingWorkCount: 1, PendingRequestCount: 2, AwaitingViewWorkCount: 2, RunningChatCount: 1}
	if !complete.Complete || len(summaries) != 1 || summaries[0].AttentionMetrics != want {
		t.Fatalf("summaries=%+v complete=%+v want=%+v", summaries, complete, want)
	}
	t.Logf("SQLite scope with two pending requests: %s", time.Since(started))
	if err := reader.MarkRead(ctx, constants.LocalReaderID, first, "3", 0, constants.ReadingIntentExplicit); err != nil {
		t.Fatal(err)
	}
	summaries, _, err = q.ExecutorSummaries(ctx, constants.LocalReaderID, []string{"waker"})
	if err != nil || len(summaries) != 1 || summaries[0].UnreadMessageCount != 3 || summaries[0].PendingRequestCount != 2 || summaries[0].WaitingWorkCount != 1 {
		t.Fatalf("local read affected another chat or request: %+v %v", summaries, err)
	}
}

// attentionQueryTrace 记录工作区权威查询使用的实际 SQL 数量。
type attentionQueryTrace struct {
	// Interface 保留 GORM 其余日志方法。
	logger.Interface
	// Statements 是本次查询执行的全部语句。
	Statements int
	// PendingChats 统计摘要从待办候选查询读取的原 Chat 行数。
	PendingChats int64
	// PendingSQL 保存待办候选查询，以检查实际访问路径。
	PendingSQL string
}

func (l *attentionQueryTrace) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *attentionQueryTrace) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	l.Statements++
	if statement, rows := sql(); strings.Contains(statement, "pending_approvals,pending_elicitations") {
		l.PendingChats += rows
		l.PendingSQL = statement
	}
}

func TestExecutorAttentionSkipsUnrelatedPendingChats(t *testing.T) {
	ctx := t.Context()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "attention.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db.DB = db.DB.Session(&gorm.Session{Logger: logger.Discard})
	chats := make([]models.Chat, 1025)
	for i := range chats {
		id := fmt.Sprintf("%032x", i+1)
		chats[i] = models.Chat{ID: id, WakerID: "unrelated", ActiveExecutionID: id, WaitingApproval: true,
			PendingApprovals: []models.PendingApproval{{ApprovalID: "approval", RequestedAt: 1}}}
	}
	selected := &chats[len(chats)-1]
	selected.WakerID, selected.HiddenFromWakerHistory = "selected", true
	teamChat := &chats[0]
	teamChat.WakerID = selected.WakerID
	if err := db.DB.CreateInBatches(chats, 128).Error; err != nil {
		t.Fatal(err)
	}
	ready := time.Now().UnixMilli()
	thread := models.ChannelThread{ID: "thread", ChannelID: "team", LeadWakerID: teamChat.WakerID, LeadChatID: teamChat.ID, ReadyAt: &ready}
	if err := db.DB.Create(&thread).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.ChannelThreadMember{ThreadID: thread.ID, WakerID: teamChat.WakerID, ChatID: teamChat.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.ChannelTask{ID: "team-review", ThreadID: thread.ID, AssigneeWakerID: teamChat.WakerID, Status: constants.ChannelTaskStatusInReview, CurrentFence: 2}).Error; err != nil {
		t.Fatal(err)
	}
	automatic := []models.AutomaticRun{
		{ID: "automatic", TaskID: "personal-task", ChatID: selected.ID, WakerID: selected.WakerID, ExecutionID: selected.ActiveExecutionID, Admission: constants.AutomaticRunAdmissionAccepted},
		{ID: "team-automatic", TaskID: "team-task", ThreadID: thread.ID, Admission: constants.AutomaticRunAdmissionAccepted},
	}
	if err := db.DB.Create(&automatic).Error; err != nil {
		t.Fatal(err)
	}
	cursors := []models.ChatReadCursor{{ReaderID: constants.LocalReaderID, ChatID: selected.ID}, {ReaderID: constants.LocalReaderID, ChatID: teamChat.ID}}
	if err := db.DB.Create(&cursors).Error; err != nil {
		t.Fatal(err)
	}
	trace := &attentionQueryTrace{Interface: logger.Discard}
	observed := db.DB.Session(&gorm.Session{Logger: trace})
	q := Query{Board: controllers.NewWorkBoardController(observed), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(observed)), Epoch: func() string { return "core" }}
	executors := make([]string, 128)
	executors[0] = selected.WakerID
	for i := 1; i < len(executors); i++ {
		executors[i] = fmt.Sprintf("unknown-%d", i)
	}
	values, complete, err := q.ExecutorSummaries(ctx, constants.LocalReaderID, executors)
	if err != nil || !complete.Complete || len(values) != len(executors) || values[0].PendingRequestCount != 1 || len(values[len(values)-1].Requests) != 0 {
		t.Fatalf("executor summaries = %+v, complete = %+v, err = %v", values, complete, err)
	}
	if request := values[0].Requests[0]; request.OwnerID != selected.ID || request.ExecutorID != selected.WakerID || request.Source != "automatic" {
		t.Fatalf("automatic Chat lost its original executor or source: %+v", request)
	}
	if trace.PendingChats != 1 || trace.Statements > 18 {
		t.Fatalf("selected executor read %d pending Chats in %d SQL statements", trace.PendingChats, trace.Statements)
	}
	t.Logf("selected executor with 1024 unrelated pending Chats: %d candidate Chat, %d SQL statements", trace.PendingChats, trace.Statements)
	var plan []struct {
		// Detail 是 SQLite 的单步访问路径。
		Detail string
	}
	if err := db.DB.Raw("EXPLAIN QUERY PLAN " + trace.PendingSQL).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	indexedWaker, indexedPending := false, false
	for _, row := range plan {
		if !strings.Contains(row.Detail, "SEARCH") {
			continue
		}
		indexedWaker = indexedWaker || strings.Contains(row.Detail, "idx_chat_pending_waker (waker_id=?)")
		indexedPending = indexedPending || strings.Contains(row.Detail, "idx_chat_pending_id (id=?)")
	}
	if !indexedWaker || !indexedPending {
		t.Fatalf("executor candidates did not seek waker and pending Chat indices: %+v", plan)
	}
	trace.PendingChats = 0
	values, complete, err = q.ExecutorSummaries(ctx, constants.LocalReaderID, []string{thread.ChannelID})
	if err != nil || !complete.Complete || len(values) != 1 || values[0].PendingRequestCount != 2 || trace.PendingChats != 1 {
		t.Fatalf("team summary = %+v, complete = %+v, candidates = %d, err = %v", values, complete, trace.PendingChats, err)
	}
	for _, request := range values[0].Requests {
		if request.OwnerKind != constants.WorkOwnerKindTeamThread || request.OwnerID != thread.ID || request.ExecutorID != thread.ChannelID || request.ChatID != teamChat.ID || request.Source != "automatic" {
			t.Fatalf("team member or review lost original owner: %+v", request)
		}
	}
}

func TestExecutorAttentionQueryScale(t *testing.T) {
	for _, chatCount := range []int{100, 1000, 10000} {
		t.Run(fmt.Sprint(chatCount), func(t *testing.T) {
			ctx := context.Background()
			db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "attention.db"), Migrate: true})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			chats := make([]models.Chat, chatCount)
			cursors := make([]models.ChatReadCursor, chatCount)
			unread := []models.ChatReadableMessage{}
			waiting := min(chatCount/2, 1000)
			for i := range chats {
				id := fmt.Sprintf("%032x", i+1)
				chats[i] = models.Chat{ID: id, WakerID: "waker", Title: "Conversation"}
				cursors[i] = models.ChatReadCursor{ReaderID: constants.LocalReaderID, ChatID: id}
				if i%17 == 0 {
					chats[i].LatestReadableSequence = 4
					unread = append(unread, models.ChatReadableMessage{ChatID: id, MessageID: "answer", Kind: "answer", OrderSequence: 4, CreatedAt: 2})
				}
				if i < waiting {
					chats[i].ActiveExecutionID = "execution"
					chats[i].WaitingApproval = true
					chats[i].PendingApprovals = []models.PendingApproval{{ApprovalID: "first", RequestedAt: 5}, {ApprovalID: "second", RequestedAt: 6}}
				}
			}
			for _, rows := range []any{chats, cursors, unread} {
				if err := db.DB.CreateInBatches(rows, 100).Error; err != nil {
					t.Fatal(err)
				}
			}
			trace := &attentionQueryTrace{Interface: logger.Discard}
			observed := db.DB.Session(&gorm.Session{Logger: trace})
			q := Query{Board: controllers.NewWorkBoardController(observed), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(observed)), Epoch: func() string { return "core" }}
			started := time.Now()
			values, complete, err := q.ExecutorSummaries(ctx, constants.LocalReaderID, []string{"waker"})
			elapsed := time.Since(started)
			if err != nil {
				t.Fatal(err)
			}
			if !complete.Complete || len(values) != 1 || values[0].UnreadMessageCount != int64(len(unread)) || values[0].PendingRequestCount != int64(2*waiting) || values[0].WaitingWorkCount != int64(waiting) {
				t.Fatalf("metrics at %d chats: %+v", chatCount, values)
			}
			if trace.Statements > max(100, chatCount/20) {
				t.Fatalf("workspace scope lost bounded batching: %d SQL for %d chats", trace.Statements, chatCount)
			}
			t.Logf("%d conversations, %d unread, %d requests: %d SQLite statements in %s", chatCount, len(unread), 2*waiting, trace.Statements, elapsed)
		})
	}
}
