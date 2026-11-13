package work_board

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/workspace_attention"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workspaceattention"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type boardReadTrace struct {
	// Interface 提供其余 GORM 日志行为。
	logger.Interface
	// statements 保存当前 HTTP 入口执行的 SQLite 语句。
	statements []string
	// slowest 保存本轮最长单条语句的耗时。
	slowest time.Duration
	// slowSQL 保存本轮最慢语句，便于诊断规模回归。
	slowSQL string
}

func (l *boardReadTrace) LogMode(logger.LogLevel) logger.Interface { return l }

func (l *boardReadTrace) Trace(_ context.Context, started time.Time, fields func() (string, int64), _ error) {
	statement, _ := fields()
	l.statements = append(l.statements, statement)
	if duration := time.Since(started); duration > l.slowest {
		l.slowest, l.slowSQL = duration, statement
	}
}

func TestAttentionOperatorsKeepHistoryOutOfPageEnrichment(t *testing.T) {
	trace := &boardReadTrace{Interface: logger.Discard}
	db := boardDatabase(t, trace)
	const conversations = 10000
	chats := make([]models.Chat, conversations)
	cursors := make([]models.ChatReadCursor, conversations)
	messages := make([]models.ChatReadableMessage, conversations)
	for i := range chats {
		id := fmt.Sprintf("%032x", i+1)
		chats[i] = models.Chat{ID: id, WakerID: "waker", Title: id, LatestReadableSequence: 3}
		cursors[i] = models.ChatReadCursor{ReaderID: constants.LocalReaderID, ChatID: id}
		messages[i] = models.ChatReadableMessage{ChatID: id, MessageID: "answer", OrderSequence: 3, Kind: "answer"}
	}
	for _, rows := range []any{chats, cursors, messages} {
		if err := db.CreateInBatches(rows, 128).Error; err != nil {
			t.Fatal(err)
		}
	}
	trace.statements = nil
	trace.slowest = 0
	started := time.Now()
	out, err := callBoard(t, &workspace_attention.GetWorkspaceAttention{}, "", nil, nil)
	if err != nil || !out.(models.WorkspaceAttentionSnapshot).HasUnread {
		t.Fatalf("workspace attention: %+v %v", out, err)
	}
	if len(trace.statements) == 0 {
		t.Fatal("workspace attention did not reach SQLite")
	}
	t.Logf("bell over %d Chats: %d SQL in %s", conversations, len(trace.statements), time.Since(started))
	for _, statement := range trace.statements {
		if strings.Contains(statement, "chat_readable_messages") {
			t.Fatalf("bell enumerated unread message ranges: %s", statement)
		}
	}
	trace.statements = nil
	trace.slowest = 0
	started = time.Now()
	out, err = callBoard(t, &ListWorkBoard{}, "?mode=awaiting_view&page=11&limit=5", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := out.(WorkBoardPage)
	if page.TotalSize != conversations || page.UnreadMessageCount != conversations || len(page.AwaitingView) != 5 || page.AwaitingView[0].FirstUnreadMessageID != "answer" {
		t.Fatalf("awaiting page: %+v", page)
	}
	var rangeQueries int
	var metricSQL string
	for _, statement := range trace.statements {
		if strings.Contains(statement, "AS first,") && strings.Contains(statement, "chat_readable_messages") {
			rangeQueries++
		}
		if metricSQL == "" && strings.Contains(statement, "AS unread_message_count") {
			metricSQL = statement
		}
	}
	if rangeQueries != 1 {
		t.Fatalf("awaiting page enriched more than its own reading window: %d range queries", rangeQueries)
	}
	t.Logf("awaiting page over %d unread Chats: %d SQL in %s", conversations, len(trace.statements), time.Since(started))
	t.Logf("slowest SQL took %s: %.120s", trace.slowest, trace.slowSQL)
	if metricSQL == "" {
		t.Fatal("awaiting page did not execute unread range count")
	}
	var plan []struct {
		// Detail 是 SQLite 选择的单步访问路径。
		Detail string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN " + metricSQL).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range plan {
		if strings.Contains(row.Detail, "SEARCH chat_readable_messages USING COVERING INDEX idx_chat_readable_order (chat_id=? AND order_sequence>?)") {
			return
		}
	}
	t.Fatalf("awaiting metrics did not seek the message range index: %+v", plan)
}

func TestWorkspaceAttentionStopsAfterFindingAction(t *testing.T) {
	trace := &boardReadTrace{Interface: logger.Discard}
	db := boardDatabase(t, trace)
	chats := make([]models.Chat, 1025)
	for i := range chats {
		id := fmt.Sprintf("%032x", i+1)
		chats[i] = models.Chat{ID: id, WakerID: "waker", Title: id, ActiveExecutionID: id, WaitingApproval: true,
			PendingApprovals: []models.PendingApproval{{ApprovalID: "approval", RequestedAt: 1}}}
	}
	if err := db.CreateInBatches(chats, 128).Error; err != nil {
		t.Fatal(err)
	}
	trace.statements = nil
	out, err := callBoard(t, &workspace_attention.GetWorkspaceAttention{}, "", nil, nil)
	if err != nil || !out.(models.WorkspaceAttentionSnapshot).HasAction {
		t.Fatalf("workspace attention: %+v %v", out, err)
	}
	batches := 0
	for _, statement := range trace.statements {
		if strings.Contains(statement, "chats INDEXED BY idx_chat_pending_id") {
			batches++
		}
	}
	if batches != 1 {
		t.Fatalf("bell continued scanning after finding an action: %d candidate batches", batches)
	}
}

func boardDatabase(t *testing.T, log ...logger.Interface) *gorm.DB {
	t.Helper()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "board.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(log) > 0 {
		db.DB = db.DB.Session(&gorm.Session{Logger: log[0]})
	}
	hub := events.New()
	db.DB = controllers.WithEventPublisher(db.DB, hub)
	sql, _ := db.DB.DB()
	t.Cleanup(func() { _ = sql.Close() })
	oldTransactions := global.TransactionControllerManager
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	t.Cleanup(func() { global.TransactionControllerManager = oldTransactions })
	oldBoard, oldAttention := global.WorkBoardControllerManager, global.WorkspaceAttention
	oldReading, oldViewing := global.ChatReadingControllerManager, global.RunViewingControllerManager
	oldHub := global.EventHub
	board := controllers.NewWorkBoardController(db.DB)
	global.EventHub = hub
	global.WorkBoardControllerManager = board
	global.ChatReadingControllerManager = controllers.NewChatReadingController(db.DB)
	global.RunViewingControllerManager = controllers.NewRunViewingController(db.DB)
	global.WorkspaceAttention = &workspaceattention.Query{Board: board, Transactions: global.TransactionControllerManager, Epoch: hub.Epoch}
	t.Cleanup(func() {
		hub.Close()
		global.WorkBoardControllerManager, global.WorkspaceAttention = oldBoard, oldAttention
		global.ChatReadingControllerManager, global.RunViewingControllerManager = oldReading, oldViewing
		global.EventHub = oldHub
	})
	return db.DB
}

func TestMarkViewedBatchPublishesOneAggregateChange(t *testing.T) {
	trace := &boardReadTrace{Interface: logger.Discard}
	db := boardDatabase(t, trace)
	const size = 128
	chats := make([]models.Chat, size)
	cursors := make([]models.ChatReadCursor, size)
	messages := make([]models.ChatReadableMessage, size)
	targets := make([]models.WorkViewTarget, size)
	for i := range chats {
		id := fmt.Sprintf("%032x", i+1)
		chats[i] = models.Chat{ID: id, WakerID: "waker", Title: id, LatestReadableSequence: 3}
		cursors[i] = models.ChatReadCursor{ReaderID: constants.LocalReaderID, ChatID: id}
		messages[i] = models.ChatReadableMessage{ChatID: id, MessageID: "answer", OrderSequence: 3, Kind: "answer"}
		targets[i] = models.WorkViewTarget{Work: models.WorkRef{Kind: constants.WorkOwnerKindChat, ID: id}, ChatID: id, ThroughMessageID: "answer"}
	}
	for _, rows := range []any{chats, cursors, messages} {
		if err := db.Create(rows).Error; err != nil {
			t.Fatal(err)
		}
	}
	board := global.EventHub.Subscribe("work-board", events.Resource{Kind: "global"})
	defer board.Close()
	reading := global.EventHub.Subscribe("reading", events.Resource{Kind: "chat", ID: chats[0].ID})
	defer reading.Close()
	trace.statements = nil
	out, err := callBoard(t, &MarkWorkBoardViewed{}, "", nil, MarkWorkBoardViewedRequest{Targets: targets})
	if err != nil || out.(models.MarkViewedResult).Applied != size {
		t.Fatalf("batch view: %+v %v", out, err)
	}
	if count := len(trace.statements); count > 1+3*size {
		t.Fatalf("batch repeated scope or invalidation reads: %d SQL", count)
	}
	t.Logf("mark-viewed for %d Chats with publisher: %d SQL", size, len(trace.statements))
	if len(board.Events) != 1 || len(reading.Events) != 1 {
		t.Fatalf("expected one aggregate and one precise invalidation, got %d and %d", len(board.Events), len(reading.Events))
	}
	<-board.Events
	if _, err := callBoard(t, &MarkWorkBoardViewed{}, "", nil, MarkWorkBoardViewedRequest{Targets: targets}); err != nil || len(board.Events) != 0 {
		t.Fatalf("unchanged retry emitted aggregate invalidation: %v %d", err, len(board.Events))
	}
}

func TestBoardHTTPTitleFallbackPreservesRareOlderRows(t *testing.T) {
	db := boardDatabase(t)
	now := time.Now().UnixMilli()
	waker := util.New()
	if err := db.Create(&models.Waker{ID: waker, Name: "检索伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	chats := make([]models.Chat, 0, 760)

	for i := range 760 {
		id := fmt.Sprintf("%032x", i+1)
		title, at := "常规资料", now
		if i < 150 {
			title, at = "罕见%_资料", now-1000
		}
		chats = append(chats, models.Chat{ID: id, WakerID: waker, Title: title, UpdatedAt: at})
	}
	if err := db.CreateInBatches(chats, 100).Error; err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"all"} {
		seen := map[string]bool{}
		cursor := ""
		for {
			out, err := callBoard(t, &ListWorkBoard{}, "?mode="+mode+"&query=%E7%BD%95%E8%A7%81%25_&limit=7&cursor="+cursor, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			page := out.(WorkBoardPage)
			for _, v := range page.Resources {
				if v.Title != "罕见%_资料" || seen[v.OwnerID] {
					t.Fatalf("wrong or repeated old row: %+v", v)
				}
				seen[v.OwnerID] = true
			}
			if !page.HasMore {
				break
			}
			cursor = page.NextCursor
		}
		if len(seen) != 150 {
			t.Fatalf("%s fallback skipped old tied matches: %d", mode, len(seen))
		}
		out, err := callBoard(t, &ListWorkBoard{}, "?mode="+mode+"&query=missing&limit=7", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.(WorkBoardPage).Resources) != 0 {
			t.Fatal("no-match search fabricated row")
		}
	}
}

func TestBoardHTTPWorkflowRequestsRollUpCurrentWorkersAndChildren(t *testing.T) {
	db := boardDatabase(t)
	now := time.Now().UnixMilli()
	definitionID, childDefinitionID, rootID, childID, workerID, executionID := util.New(), util.New(), util.New(), util.New(), util.New(), util.New()
	workerEffectID, workerApprovalID := util.New(), util.New()
	values := []any{
		&models.WorkflowDefinition{ID: definitionID, Title: "资料流程"},
		&models.WorkflowDefinition{ID: childDefinitionID, Title: "独立校验流程"},
		&models.WorkflowRun{ID: rootID, DefinitionID: definitionID, RootRunID: rootID, Status: constants.WorkflowRunStatusRunning, UpdatedAt: now},
		&models.WorkflowRun{ID: childID, DefinitionID: childDefinitionID, RootRunID: rootID, ParentRunID: rootID, Joined: true, Status: constants.WorkflowRunStatusWaiting, UpdatedAt: now},
		&models.Chat{ID: workerID, WakerID: util.New(), HiddenFromWakerHistory: true, ActiveExecutionID: executionID, WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: workerApprovalID, Title: "工作器授权", RequestedAt: now}}},
		&models.WorkflowEffect{ID: workerEffectID, RunID: childID, ChatID: workerID, ExecutionID: executionID, Status: constants.WorkflowStepStatusWaiting, Boundary: workflowdsl.EffectBoundary{Primitive: "worker"}},
		&models.WorkflowEffect{ID: util.New(), RunID: childID, ChatID: workerID, ExecutionID: util.New(), Status: constants.WorkflowStepStatusFailed},
		&models.WorkflowHumanRequest{ID: util.New(), RunID: childID, Prompt: "补充条件", Status: constants.WorkflowStepStatusWaiting, DeadlineAt: now + 60000, CreatedAt: now},
		&models.WorkflowHumanRequest{ID: util.New(), RunID: childID, Prompt: "已过期", Status: constants.WorkflowStepStatusWaiting, DeadlineAt: now - 1, CreatedAt: now},
		&models.WorkflowOperationRequest{ID: util.New(), EffectID: util.New(), RunID: rootID, Description: "宿主授权", Status: constants.WorkflowStepStatusWaiting, CreatedAt: now},
		&models.AutomaticRun{ID: util.New(), WorkflowRunID: rootID, Admission: constants.AutomaticRunAdmissionAccepted},
	}
	for _, v := range values {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	out, err := callBoard(t, &ListWorkBoard{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := out.(WorkBoardPage)
	if len(page.Resources) != 1 || page.Resources[0].OwnerID != rootID || page.Resources[0].ApprovalCount != 2 || page.Resources[0].InputCount != 1 {
		t.Fatalf("root workflow rows/counts=%+v", page.Resources)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := out.(WorkBoardSummary)
	if summary.PendingRequestCount != 3 {
		t.Fatalf("workflow attention=%+v", summary)
	}
	for _, v := range summary.ActionPreview {
		if v.RunID == childID && (v.WorkflowID != childDefinitionID || v.ExecutorID != definitionID) {
			t.Fatalf("子流程精确目标和顶层范围不应混淆: %+v", v)
		}
		if v.OwnerID != rootID || (v.Title == "工作器授权" && (v.ChatID != workerID || v.RunID != childID)) {
			t.Fatalf("lost exact target=%+v", v)
		}
	}
	if err := db.Model(&models.WorkflowEffect{}).Where("id = ?", workerEffectID).UpdateColumn("chat_id", "").Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardLiveTarget{}, "?operation=request_opened&workKind=chat&workId="+workerID+"&chatId="+workerID+"&executionId="+executionID+"&requestKind=approval&requestId="+workerApprovalID, nil, nil)
	if err != nil || !out.(WorkBoardLiveTarget).Valid || out.(WorkBoardLiveTarget).RunID != childID || out.(WorkBoardLiveTarget).Work == nil || out.(WorkBoardLiveTarget).Work.ID != rootID {
		t.Fatalf("worker 审批先于 Chat 关联写入时无法定位: %+v %v", out, err)
	}
	out, err = callBoard(t, &ListWorkBoard{}, "", nil, nil)
	if err != nil || len(out.(WorkBoardPage).Resources) != 1 || out.(WorkBoardPage).Resources[0].ApprovalCount != 2 {
		t.Fatalf("worker 审批先于 Chat 关联写入时未计入原工作: %+v %v", out, err)
	}
	out, err = callBoard(t, &workspace_attention.GetExecutorAttention{}, "?executorIds="+definitionID+","+childDefinitionID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	executors := out.(workspace_attention.ExecutorAttentionResponse).Resources
	if len(executors) != 2 || executors[0].PendingRequestCount != 3 || executors[1].PendingRequestCount != 0 {
		t.Fatalf("joined 待办未归并到原自动工作流定义: %+v", executors)
	}
	for _, request := range executors[0].Requests {
		if request.OwnerID != rootID || request.ExecutorID != definitionID || request.Source != "automatic" {
			t.Fatalf("joined 待办丢失原运行归属或来源: %+v", request)
		}
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", rootID).Update("status", constants.WorkflowRunStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.(WorkBoardSummary).PendingRequestCount != 0 {
		t.Fatal("stopped root retained child/worker requests")
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", childID).Update("joined", false).Error; err != nil {
		t.Fatal(err)
	}
	grandchildID, grandchildRequest := util.New(), util.New()
	if err := db.Create(&models.WorkflowRun{ID: grandchildID, DefinitionID: childDefinitionID, RootRunID: rootID, ParentRunID: childID, Joined: true, Status: constants.WorkflowRunStatusWaiting}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowHumanRequest{ID: grandchildRequest, RunID: grandchildID, Prompt: "后台子树输入", Status: constants.WorkflowStepStatusWaiting, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil || out.(WorkBoardSummary).PendingRequestCount != 3 {
		t.Fatalf("detached 子树不随顶层停止丢失有效请求: %+v %v", out, err)
	}
	out, err = callBoard(t, &GetWorkBoardLiveTarget{}, "?operation=request_opened&workKind=workflow_run&workId="+childID+"&executionId="+grandchildID+"&requestKind=input&requestId="+grandchildRequest, nil, nil)
	if err != nil || !out.(WorkBoardLiveTarget).Valid || out.(WorkBoardLiveTarget).RunID != grandchildID || out.(WorkBoardLiveTarget).Work == nil || out.(WorkBoardLiveTarget).Work.ID != childID {
		t.Fatalf("detached 子树请求无法精确定位: %+v %v", out, err)
	}
	out, err = callBoard(t, &GetWorkBoardLiveTarget{}, "?operation=request_opened&workKind=workflow_run&workId="+grandchildID+"&executionId="+grandchildID+"&requestKind=input&requestId="+grandchildRequest, nil, nil)
	if err != nil || !out.(WorkBoardLiveTarget).Valid || out.(WorkBoardLiveTarget).RunID != grandchildID {
		t.Fatalf("joined 原运行请求无法定位到有效归属: %+v %v", out, err)
	}
	out, err = callBoard(t, &ListWorkBoard{}, "?executorId="+childDefinitionID, nil, nil)
	if err != nil || len(out.(WorkBoardPage).Resources) != 1 || out.(WorkBoardPage).Resources[0].InputCount != 2 {
		t.Fatalf("detached 子树请求未计入工作记录: %+v %v", out, err)
	}
	out, err = callBoard(t, &workspace_attention.GetExecutorAttention{}, "?executorIds="+definitionID+","+childDefinitionID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	executors = out.(workspace_attention.ExecutorAttentionResponse).Resources
	if len(executors) != 2 || executors[0].PendingRequestCount != 0 || executors[1].PendingRequestCount != 3 {
		t.Fatalf("detached 子树的待办范围不准确: %+v", executors)
	}
	for _, request := range executors[1].Requests {
		if request.OwnerID != childID || request.ExecutorID != childDefinitionID || request.Source != "workflow" {
			t.Fatalf("detached 子树丢失独立运行归属: %+v", request)
		}
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", childID).Update("status", constants.WorkflowRunStatusCancelling).Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil || out.(WorkBoardSummary).PendingRequestCount != 0 {
		t.Fatalf("取消中的 joined 子树不应继续提供操作: %+v %v", out, err)
	}
}

func callBoard(t *testing.T, operator ginx.HandleOperator, query string, params gin.Params, body any) (any, error) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Params = params
	ctx.Request = httptest.NewRequest(operator.Method(), "/work-board"+query, bytes.NewReader(data))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if err := ginx.ParameterBinding(ctx, operator, ginx.GetOperatorTypeInfo(reflect.TypeOf(operator))); err != nil {
		return nil, err
	}
	if err := operator.Validate(ctx); err != nil {
		return nil, err
	}
	return operator.Output(ctx)
}

type reminderExecutionState struct {
	// Service 提供测试无需调用的原执行接口。
	execution.Service
	// activeID 是尚未释放的执行。
	activeID string
}

func (s *reminderExecutionState) IsExecutionActive(_ string, executionID string) bool {
	return s.activeID == executionID
}

func TestLiveChatOutcomeResolvesOnlyVisibleWork(t *testing.T) {
	db := boardDatabase(t)
	autoChat, leadChat, memberChat, threadID, executionID := util.New(), util.New(), util.New(), util.New(), util.New()
	channelID, wakerID, runID := util.New(), util.New(), util.New()
	previousExecution := global.ExecutionService
	state := &reminderExecutionState{activeID: executionID}
	global.ExecutionService = state
	t.Cleanup(func() { global.ExecutionService = previousExecution })
	ready := time.Now().UnixMilli()
	for _, row := range []any{
		&models.Chat{ID: autoChat, WakerID: wakerID, HiddenFromWakerHistory: true},
		&models.Chat{ID: leadChat, WakerID: wakerID, HiddenFromWakerHistory: true},
		&models.Chat{ID: memberChat, WakerID: wakerID, HiddenFromWakerHistory: true},
		&models.ChannelThread{ID: threadID, ChannelID: channelID, LeadChatID: leadChat, LeadWakerID: wakerID, ReadyAt: &ready},
		&models.ChannelThreadMember{ThreadID: threadID, WakerID: wakerID, ChatID: memberChat},
		&models.AutomaticRun{ID: runID, TaskID: util.New(), OccurrenceKey: "audit", Admission: constants.AutomaticRunAdmissionPending},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	lookup := func(id string) WorkBoardLiveTarget {
		t.Helper()
		out, err := callBoard(t, &GetWorkBoardLiveTarget{}, "?operation=completed&workKind=chat&workId="+id+"&chatId="+id+"&executionId="+executionID+"&messageId="+executionID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out.(WorkBoardLiveTarget)
	}
	for _, id := range []string{autoChat, leadChat, memberChat} {
		if err := db.Create(&models.ChatReadableMessage{ChatID: id, MessageID: executionID, ExecutionID: executionID, OrderSequence: 4, Kind: "answer"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if lookup(autoChat).Valid || lookup(memberChat).Valid {
		t.Fatal("unbound automatic chat or private member result was visible")
	}
	if lookup(leadChat).Valid {
		t.Fatal("result was deliverable before the original execution slot was released")
	}
	state.activeID = util.New()
	team := lookup(leadChat)
	if !team.Valid || team.Work == nil || team.Work.Kind != constants.WorkOwnerKindTeamThread || team.ThreadID != threadID {
		t.Fatalf("raw result did not resolve to the team thread: %+v", team)
	}
	if err := db.Model(&models.AutomaticRun{}).Where("id=?", runID).
		Updates(map[string]any{"chat_id": autoChat, "admission": constants.AutomaticRunAdmissionAccepted}).Error; err != nil {
		t.Fatal(err)
	}
	automatic := lookup(autoChat)
	if !automatic.Valid || automatic.Work == nil || automatic.Work.Kind != constants.WorkOwnerKindChat || automatic.Work.ID != autoChat {
		t.Fatalf("bound automatic result did not resolve to the original chat: %+v", automatic)
	}
}

func TestBoardHTTPScopesDeduplicateAndKeepOldAttention(t *testing.T) {
	db := boardDatabase(t)
	now := time.Now().UnixMilli()
	ready := now
	wakerID, channelID, threadID, leadID, memberID, chatID := util.New(), util.New(), util.New(), util.New(), util.New(), util.New()
	values := []any{
		&models.Waker{ID: wakerID, Name: "伙伴"}, &models.Channel{ID: channelID, Name: "团队"},
		&models.Chat{ID: chatID, WakerID: wakerID, Title: "独立 100%", UpdatedAt: now, ActiveExecutionID: util.New(), WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: util.New(), Title: "授权", RequestedAt: 10}}},
		&models.Chat{ID: leadID, WakerID: wakerID, Title: "内部主对话", UpdatedAt: now, HiddenFromWakerHistory: true},
		&models.Chat{ID: memberID, WakerID: wakerID, Title: "内部成员", UpdatedAt: now, HiddenFromWakerHistory: true, ActiveExecutionID: util.New(), WaitingElicitation: true, PendingElicitations: []models.PendingElicitation{{ElicitationID: util.New(), Message: "补充资料", RequestedAt: 20}}},
		&models.ChannelThread{ID: threadID, ChannelID: channelID, Title: "团队工作", LeadChatID: leadID, LeadWakerID: wakerID, ReadyAt: &ready, UpdatedAt: now},
		&models.ChannelThreadMember{ThreadID: threadID, WakerID: wakerID, ChatID: memberID, DisplayName: "成员"},
		&models.AutomaticRun{ID: util.New(), ChatID: chatID},
		&models.IMConversationLink{ID: util.New(), ChatID: chatID},
		&models.ChannelTask{ID: util.New(), ThreadID: threadID, Subject: "验收", AssigneeWakerID: wakerID, Status: constants.ChannelTaskStatusInReview, UpdatedAt: 30},
	}
	for _, v := range values {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	out, err := callBoard(t, &ListWorkBoard{}, "?limit=10", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := out.(WorkBoardPage)
	if len(page.Resources) != 2 {
		t.Fatalf("canonical rows=%+v", page.Resources)
	}
	for _, v := range page.Resources {
		if v.OwnerID == chatID && (v.Source != "automatic" || v.ApprovalCount != 1) {
			t.Fatalf("automatic wrapper or wrong count: %+v", v)
		}
		if v.OwnerID == threadID && (v.InputCount != 1 || v.ReviewCount != 1) {
			t.Fatalf("member rollup=%+v", v)
		}
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := out.(WorkBoardSummary)
	if summary.PendingRequestCount != 3 || len(summary.ActionPreview) != 3 || summary.ActionPreview[0].CreatedAt != 10 {
		t.Fatalf("oldest request summary=%+v", summary)
	}
	for _, kind := range []string{"approval", "input", "review"} {
		filtered, filterErr := callBoard(t, &ListWorkBoard{}, "?mode=action&states="+kind+"&limit=1", nil, nil)
		if filterErr != nil {
			t.Fatal(filterErr)
		}
		filteredPage := filtered.(WorkBoardPage)
		if filteredPage.TotalSize != 1 || len(filteredPage.Attention) != 1 || filteredPage.Attention[0].Kind != kind || filteredPage.HasMore {
			t.Fatalf("wrong pending state %s: %+v", kind, filteredPage)
		}
	}
	filtered, err := callBoard(t, &ListWorkBoard{}, "?mode=action&states=approval,input&limit=1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	filteredPage := filtered.(WorkBoardPage)
	if filteredPage.TotalSize != 2 || !filteredPage.HasMore || filteredPage.NextCursor == "" {
		t.Fatalf("filtered pending pagination=%+v", filteredPage)
	}
	next, err := callBoard(t, &ListWorkBoard{}, "?mode=action&states=approval,input&limit=1&cursor="+filteredPage.NextCursor, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	nextPage := next.(WorkBoardPage)
	if nextPage.TotalSize != 2 || len(nextPage.Attention) != 1 || nextPage.Attention[0].Kind != "input" || nextPage.HasMore {
		t.Fatalf("filtered pending next page=%+v", nextPage)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "?source=automatic", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.(WorkBoardSummary).PendingRequestCount != 1 {
		t.Fatalf("scope=%+v", out)
	}
	out, err = callBoard(t, &ListWorkBoard{}, "?query=100%25", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.(WorkBoardPage).Resources) != 1 {
		t.Fatalf("literal title search=%+v", out)
	}
	if err := db.Model(&models.Chat{}).Where("id = ?", chatID).Updates(map[string]any{"active_execution_id": "", "waiting_approval": true}).Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.(WorkBoardSummary).PendingRequestCount != 2 {
		t.Fatalf("ended execution retained stale approval: %+v", out)
	}
}

func TestBoardHTTPReadingRacesAndStableTiedPagination(t *testing.T) {
	db := boardDatabase(t)
	now := time.Now().UnixMilli()
	waker := util.New()
	if err := db.Create(&models.Waker{ID: waker, Name: "伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{util.New(), util.New(), util.New(), util.New()}
	for _, id := range ids {
		if err := db.Create(&models.Chat{ID: id, WakerID: waker, Title: "工作", UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		out, err := callBoard(t, &ListWorkBoard{}, "?limit=1&cursor="+cursor, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		page := out.(WorkBoardPage)
		for _, v := range page.Resources {
			if seen[v.OwnerID] {
				t.Fatal("seek repeated tied row")
			}
			seen[v.OwnerID] = true
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 4 {
		t.Fatalf("pagination skipped rows: %v", seen)
	}

}

func TestBoardHTTPOverviewCountsCurrentWorkAndMultiSelectedTerminalStates(t *testing.T) {
	db := boardDatabase(t)
	now := time.Now().UnixMilli()
	old := time.Now().AddDate(0, 0, -60).UnixMilli()
	wakerID := util.New()
	rows := []models.Chat{
		{ID: util.New(), WakerID: wakerID, Title: "执行中", ActiveExecutionID: util.New(), LastEndedExecutionStatus: constants.ExecutionStatusFailed, UpdatedAt: now},
		{ID: util.New(), WakerID: wakerID, Title: "成功", LastEndedExecutionStatus: constants.ExecutionStatusCompleted, UpdatedAt: now},
		{ID: util.New(), WakerID: wakerID, Title: "失败", LastEndedExecutionStatus: constants.ExecutionStatusFailed, UpdatedAt: now},
		{ID: util.New(), WakerID: wakerID, Title: "取消", LastEndedExecutionStatus: constants.ExecutionStatusCanceled, UpdatedAt: now},
		{ID: util.New(), WakerID: wakerID, Title: "未开始", UpdatedAt: now},
		{ID: util.New(), WakerID: wakerID, Title: "旧待办", ActiveExecutionID: util.New(), WaitingApproval: true, UpdatedAt: old, PendingApprovals: []models.PendingApproval{{ApprovalID: util.New(), Title: "授权1", RequestedAt: old}, {ApprovalID: util.New(), Title: "授权2", RequestedAt: old}}},
	}
	if err := db.Create(&models.Waker{ID: wakerID, Name: "伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(rows).Error; err != nil {
		t.Fatal(err)
	}
	out, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := out.(WorkBoardSummary)
	if summary.WorkCount != 5 || summary.RunningCount != 1 || summary.EndedCount != 3 || summary.WaitingWorkCount != 1 || summary.PendingRequestCount != 2 {
		t.Fatalf("工作统计不能混用请求数、旧待办或活跃工作的旧终态: %+v", summary)
	}
	out, err = callBoard(t, &ListWorkBoard{}, "?states=completed,failed,canceled&limit=1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := out.(WorkBoardPage)
	seen := map[string]bool{}
	for {
		for _, row := range page.Resources {
			if row.Activity != constants.ChannelActivityStateIdle || seen[row.OwnerID] {
				t.Fatalf("多选与分页错误: %+v", row)
			}
			seen[row.OwnerID] = true
		}
		if !page.HasMore {
			break
		}
		out, err = callBoard(t, &ListWorkBoard{}, "?states=completed,failed,canceled&limit=1&cursor="+page.NextCursor, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		page = out.(WorkBoardPage)
	}
	if len(seen) != 3 {
		t.Fatalf("多选状态遗漏终态: %v", seen)
	}
	out, err = callBoard(t, &ListWorkBoard{}, "?states=running,failed", nil, nil)
	if err != nil || len(out.(WorkBoardPage).Resources) != 2 {
		t.Fatalf("状态多选应取并集: %+v %v", out, err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, fmt.Sprintf("?from=1&to=%d", old+1), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary = out.(WorkBoardSummary)
	if summary.WorkCount != 1 || summary.EndedCount != 0 || summary.WaitingWorkCount != 1 || summary.PendingRequestCount != 2 {
		t.Fatalf("历史日期不能隐藏待办: %+v", summary)
	}
	if _, err := callBoard(t, &ListWorkBoard{}, "?states=running,invalid", nil, nil); err == nil {
		t.Fatal("未知多选状态未拒绝")
	}
	if _, err := callBoard(t, &GetWorkBoardSummary{}, "?from=10&to=1", nil, nil); err == nil {
		t.Fatal("倒置日期未拒绝")
	}
}

func TestBoardHTTPNumberedPagesMatchStableCursorAcrossSources(t *testing.T) {
	db := boardDatabase(t)
	now := time.Now().UnixMilli()
	waker, team, workflow := util.New(), util.New(), util.New()
	for _, value := range []any{&models.Waker{ID: waker, Name: "伙伴"}, &models.Channel{ID: team, Name: "团队"}, &models.WorkflowDefinition{ID: workflow, Title: "流程"}} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 11 {
		if err := db.Create(&models.Chat{ID: util.New(), WakerID: waker, Title: "工作", UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 5 {
		if err := db.Create(&models.ChannelThread{ID: util.New(), ChannelID: team, LeadChatID: util.New(), Title: "工作", ReadyAt: &now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for range 7 {
		if err := db.Create(&models.WorkflowRun{ID: util.New(), DefinitionID: workflow, Status: constants.WorkflowRunStatusSucceeded, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	var second WorkBoardPage
	for pageNumber := 1; pageNumber <= 3; pageNumber++ {
		out, err := callBoard(t, &ListWorkBoard{}, fmt.Sprintf("?limit=10&page=%d", pageNumber), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		page := out.(WorkBoardPage)
		want := 10
		if pageNumber == 3 {
			want = 3
		}
		if page.TotalSize != 23 || len(page.Resources) != want || page.HasMore != (pageNumber < 3) {
			t.Fatalf("页码/数量错误 page=%d value=%+v", pageNumber, page)
		}
		for _, row := range page.Resources {
			key := string(row.OwnerKind) + ":" + row.OwnerID
			if seen[key] {
				t.Fatalf("数字跳页重复工作: page=%d owner=%s", pageNumber, key)
			}
			seen[key] = true
		}
		if pageNumber == 2 {
			second = page
		}
	}
	out, err := callBoard(t, &ListWorkBoard{}, "?limit=10&cursor="+second.NextCursor, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cursorPage := out.(WorkBoardPage)
	out, err = callBoard(t, &ListWorkBoard{}, "?limit=10&page=3", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	numberedPage := out.(WorkBoardPage)
	if !reflect.DeepEqual(cursorPage.Resources, numberedPage.Resources) {
		t.Fatal("数字跳页与相邻seek分页顺序不一致")
	}
}

func TestMarkViewedFrozenBatch(t *testing.T) {
	db := boardDatabase(t)
	now := time.Now().UnixMilli()
	a, b, c := util.New(), util.New(), util.New()
	oldA, oldB, oldC, newA := util.New(), util.New(), util.New(), util.New()
	ctx := context.Background()
	reading := global.ChatReadingControllerManager
	for _, id := range []string{a, b, c} {
		if err := db.Create(&models.Chat{ID: id, WakerID: "waker", Title: id, UpdatedAt: now, LatestSequence: 2, LatestReadableSequence: 2}).Error; err != nil {
			t.Fatal(err)
		}
		if err := reading.EnsureCursor(ctx, constants.LocalReaderID, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	add := func(chat, id string, order int64) {
		t.Helper()
		if _, err := reading.Project(ctx, chat, nil, []models.ChatReadableMessage{{ChatID: chat, MessageID: id, OrderSequence: order, Kind: "answer", CreatedAt: now}}, false); err != nil {
			t.Fatal(err)
		}
	}
	add(a, oldA, 2)
	add(b, oldB, 2)
	add(c, oldC, 2)
	output, err := callBoard(t, &GetWorkBoardViewTargets{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	frozen := output.(WorkBoardViewTargets)
	if frozen.TargetCount != 3 || len(frozen.Targets) != 3 || frozen.Epoch == "" {
		t.Fatalf("unexpected frozen boundaries: %+v", frozen)
	}
	add(a, newA, 3)
	if err := db.Model(&models.Chat{}).Where("id=?", a).UpdateColumn("latest_readable_sequence", 3).Error; err != nil {
		t.Fatal(err)
	}
	if err := reading.MarkUnread(ctx, constants.LocalReaderID, b, oldB, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&models.Chat{ID: c}).Error; err != nil {
		t.Fatal(err)
	}
	output, err = callBoard(t, &MarkWorkBoardViewed{}, "", nil, MarkWorkBoardViewedRequest{Targets: frozen.Targets})
	if err != nil {
		t.Fatal(err)
	}
	if got := output.(models.MarkViewedResult); got != (models.MarkViewedResult{Applied: 1, SkippedConflict: 1, SkippedDeleted: 1}) {
		t.Fatalf("batch: %+v", got)
	}
	for chat, first := range map[string]string{a: newA, b: oldB} {
		state, err := reading.Get(ctx, constants.LocalReaderID, chat)
		if err != nil || state.UnreadMessageCount != 1 || state.FirstUnreadMessageID != first {
			t.Fatalf("frozen batch consumed a newer message or manual unread: %+v %v", state, err)
		}
	}
	output, err = callBoard(t, &MarkWorkBoardViewed{}, "", nil, MarkWorkBoardViewedRequest{Targets: frozen.Targets})
	if err != nil || output.(models.MarkViewedResult).Applied != 1 {
		t.Fatalf("retry: %+v %v", output, err)
	}
}

func TestWorkBoardViewTargetsUseFilteredReadingHealth(t *testing.T) {
	db := boardDatabase(t)
	healthy := models.Chat{ID: util.New(), WakerID: util.New(), Title: "Healthy", LatestReadableSequence: 3}
	isolated := models.Chat{ID: util.New(), WakerID: util.New(), Title: "Isolated", LatestReadableSequence: 3}
	for _, chat := range []models.Chat{healthy, isolated} {
		if err := db.Create(&chat).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ChatReadCursor{ReaderID: constants.LocalReaderID, ChatID: chat.ID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ChatReadableMessage{ChatID: chat.ID, MessageID: "answer", OrderSequence: 3, Kind: "answer"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	global.WorkspaceAttention.Isolated = func() []string { return []string{isolated.ID} }
	global.WorkspaceAttention.Incomplete = func(chatIDs []string) string {
		if chatIDs == nil || slices.Contains(chatIDs, isolated.ID) {
			return "isolated"
		}
		return ""
	}
	if _, err := callBoard(t, &GetWorkBoardViewTargets{}, "", nil, nil); err == nil {
		t.Fatal("isolated workspace was marked complete")
	}
	out, err := callBoard(t, &GetWorkBoardViewTargets{}, "?executorId="+healthy.WakerID, nil, nil)
	if err != nil || out.(WorkBoardViewTargets).TargetCount != 1 || out.(WorkBoardViewTargets).Targets[0].ChatID != healthy.ID {
		t.Fatalf("unrelated isolated chat blocked healthy scope: %+v %v", out, err)
	}
}

func TestMarkViewedTargetBounds(t *testing.T) {
	db := boardDatabase(t)
	targets := make([]models.WorkViewTarget, 129)
	for i := range targets {
		targets[i] = models.WorkViewTarget{Work: models.WorkRef{Kind: constants.WorkOwnerKindChat, ID: util.New()}}
	}
	if _, err := callBoard(t, &MarkWorkBoardViewed{}, "", nil, MarkWorkBoardViewedRequest{Targets: targets}); err == nil {
		t.Fatal("129 targets accepted")
	}
	task := models.AutomaticTask{ID: util.New(), WakerID: "waker", AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{Name: "批量任务"}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	runs := make([]models.AutomaticRun, 10001)
	for i := range runs {
		id := fmt.Sprintf("%032x", i+1)
		runs[i] = models.AutomaticRun{ID: id, TaskID: task.ID, WakerID: task.WakerID, OccurrenceKey: id, Admission: constants.AutomaticRunAdmissionFailed, Source: constants.AutomaticRunSourceManual, ErrorCode: "RuntimeUnavailable"}
	}
	if err := db.CreateInBatches(runs, 128).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := callBoard(t, &GetWorkBoardViewTargets{}, "", nil, nil); err == nil {
		t.Fatal("10001 targets accepted")
	}
}
