package controllers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestFailureDeepPageAndClearUseBoundedReads(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	tasks, runs := []models.AutomaticTask{}, []models.AutomaticRun{}
	for i := 0; i < 600; i++ {
		task := automaticTaskFixture("waker")
		task.ID = fmt.Sprintf("task-%04d", i)
		task.Name = "100%_名称"
		run := models.AutomaticRun{ID: fmt.Sprintf("run-%04d", i), TaskID: task.ID, WakerID: "waker", Admission: constants.AutomaticRunAdmissionFailed, Configuration: task.AutomaticTaskConfiguration, CreatedAt: 1, UpdatedAt: 10}
		tasks = append(tasks, task)
		runs = append(runs, run)
	}
	for _, value := range []any{&tasks, &runs} {
		if err := db.CreateInBatches(value, 50).Error; err != nil {
			t.Fatal(err)
		}
	}
	log := &notificationQueryLog{Interface: logger.Discard}
	controller := NewNotificationController(db.Session(&gorm.Session{Logger: log}))
	page, total, err := controller.List(ctx, NotificationQuery{Bucket: "unread", Offset: 570, Limit: 5, ExecutorID: "waker", Query: "100%_名称"})
	if err != nil || total != 600 || len(page) != 6 || page[0].ID != "automatic-run-failed:run-0029" {
		t.Fatalf("page=%+v total=%d err=%v", page, total, err)
	}
	for i, count := range log.Rows {
		if count > 6 {
			t.Fatalf("deep page loaded unselected failure payload: %d %s", count, log.Statements[i])
		}
	}
	// 工作流范围优先于团队，团队优先于伙伴；冻结标题搜索按字面量处理。
	if err := db.Model(&models.AutomaticTask{}).Where("id=?", tasks[0].ID).Updates(map[string]any{"workflow_id": "workflow", "channel_id": "team"}).Error; err != nil {
		t.Fatal(err)
	}
	for executor, want := range map[string]int64{"workflow": 1, "team": 0, "waker": 599} {
		_, total, err := controller.List(ctx, NotificationQuery{Bucket: "unread", ExecutorID: executor, Limit: 5})
		if err != nil || total != want {
			t.Fatalf("executor=%s count=%d want=%d err=%v", executor, total, want, err)
		}
	}
	_, total, err = controller.List(ctx, NotificationQuery{Bucket: "unread", Query: "100%X名称", Limit: 5})
	if err != nil || total != 0 {
		t.Fatalf("wildcard escaped count=%d err=%v", total, err)
	}
	log.Rows, log.Statements = nil, nil
	if err := controller.ClearFailures(ctx, 100); err != nil {
		t.Fatal(err)
	}
	for i, count := range log.Rows {
		if count > repositoryReadBatch {
			t.Fatalf("clear loaded entire failure set: %d %s", count, log.Statements[i])
		}
	}
	var ackCount int64
	if err := db.Model(&models.AttentionNoticeAck{}).Where("viewed_at=100").Count(&ackCount).Error; err != nil {
		t.Fatal(err)
	}
	_, total, err = controller.List(ctx, NotificationQuery{Bucket: "unread", Limit: 5})
	if err != nil || total != 0 || ackCount != 600 {
		t.Fatalf("clear skipped a moving batch: unread=%d ack=%d err=%v", total, ackCount, err)
	}
}

func TestResultCountUsesSameOwnerVisibilityAsPages(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	ready := int64(1)
	values := []any{
		&models.WorkflowDefinition{ID: "definition", Title: "shared"},
		&models.Chat{ID: "visible", Title: "shared", WakerID: "waker"},
		&models.Chat{ID: "worker", Title: "shared", WakerID: "waker"},
		&models.Chat{ID: "creation", Title: "shared", WakerID: "waker"},
		&models.ChannelThread{ID: "thread", LeadChatID: "worker", ChannelID: "team", ReadyAt: &ready, Title: "shared"},
		&models.ChannelThread{ID: "unready", LeadChatID: "other", ChannelID: "team", Title: "shared"},
		&models.ChannelThreadMember{ThreadID: "thread", ChatID: "worker", WakerID: "waker"},
		&models.WorkflowCreationSession{ID: "session", ChatID: "creation", DefinitionID: "definition"},
		&models.WorkflowRun{ID: "run", DefinitionID: "definition", Status: constants.WorkflowRunStatusSucceeded},
		&models.WorkflowRun{ID: "child", DefinitionID: "definition", ParentRunID: "run", RootRunID: "run", Status: constants.WorkflowRunStatusSucceeded},
		&models.WorkflowRun{ID: "deleted", DefinitionID: "definition", HistoryDeletedAt: &ready, Status: constants.WorkflowRunStatusSucceeded},
		&models.WorkflowRun{ID: "orphan", DefinitionID: "missing", Status: constants.WorkflowRunStatusSucceeded},
	}
	for _, value := range values {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for kind, ids := range map[constants.WorkResultOwnerKind][]string{
		constants.WorkResultOwnerKindChat:        {"visible", "worker", "creation", "missing"},
		constants.WorkResultOwnerKindTeamThread:  {"thread", "unready", "missing"},
		constants.WorkResultOwnerKindWorkflowRun: {"run", "child", "deleted", "orphan", "missing"},
	} {
		for _, id := range ids {
			if err := db.Create(&models.WorkResultReceipt{OwnerKind: kind, OwnerID: id, LatestVersion: 1, AvailableAt: 10}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	controller := NewWorkBoardController(db)
	for _, query := range []WorkBoardQuery{{Limit: 50}, {Source: "chat", Limit: 50}, {Source: "team", Limit: 50}, {Source: "workflow", Limit: 50}, {ExecutorID: "definition", Limit: 50}, {Query: "shared", Limit: 50}, {Query: "missing", Limit: 50}} {
		count, err := controller.ResultCount(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		page, err := controller.Results(ctx, query)
		if err != nil || int64(len(page)) != count {
			t.Fatalf("query=%+v count=%d page=%+v err=%v", query, count, page, err)
		}
		if query.Source == "" && query.ExecutorID == "" && query.Query == "" && count != 3 {
			t.Fatalf("invalid owner receipts counted: %d", count)
		}
	}
	_, count, err := NewNotificationController(db).List(ctx, NotificationQuery{Bucket: "unread", Limit: 50})
	if err != nil || count != 3 {
		t.Fatalf("notification count=%d err=%v", count, err)
	}
}

func TestAttentionDeepPagesAndPageScopedEnrichment(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	chats := []models.Chat{}
	for i := 0; i < 600; i++ {
		id := fmt.Sprintf("pending-%04d", i)
		chats = append(chats, models.Chat{ID: id, WakerID: "waker", Title: "工作", ActiveExecutionID: "execution", WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: "request-" + id, Title: "允许", RequestedAt: 10}}})
	}
	if err := db.CreateInBatches(&chats, 50).Error; err != nil {
		t.Fatal(err)
	}
	log := &notificationQueryLog{Interface: logger.Discard}
	board := &workBoardController{db: db.Session(&gorm.Session{Logger: log})}
	err := NewTransactionController(dbhelper.NewGormTransactionManager(board.db)).Transaction(ctx, func(ctx context.Context) error {
		page, total, err := board.Attention(ctx, WorkBoardQuery{Offset: 570, Limit: 5})
		if err != nil || total != 600 || len(page) != 6 || page[0].OwnerID != "pending-0570" {
			t.Fatalf("deep attention=%+v count=%d err=%v", page, total, err)
		}
		for i, count := range log.Rows {
			if count > repositoryReadBatch {
				t.Fatalf("unbounded attention batch: %d %s", count, log.Statements[i])
			}
		}
		log.Rows, log.Statements = nil, nil
		entries := []models.WorkBoardEntry{{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: "pending-0570"}}
		if err := board.attachAttentionCounts(ctx, entries); err != nil {
			t.Fatal(err)
		}
		if entries[0].ApprovalCount != 1 {
			t.Fatalf("page counts=%+v", entries)
		}
		for i, count := range log.Rows {
			if count > 1 {
				t.Fatalf("page enrichment read other owners: %d %s", count, log.Statements[i])
			}
		}
		page, total, err = board.Attention(ctx, WorkBoardQuery{Offset: 999999, Limit: 5})
		if err != nil || total != 600 || len(page) != 0 {
			t.Fatalf("out of range page=%+v total=%d err=%v", page, total, err)
		}
		// 同一容器的多个请求只计一份工作；概览不能为工作数再次读取全部候选。
		if err := controllerDB(ctx, board.db).Model(&models.Chat{}).Where("id=?", "pending-0000").Update("pending_approvals", `[{"approvalId":"first","title":"允许","requestedAt":10},{"approvalId":"second","title":"允许","requestedAt":10}]`).Error; err != nil {
			t.Fatal(err)
		}
		log.Rows, log.Statements = nil, nil
		preview, requests, works, err := board.AttentionOverview(ctx, WorkBoardQuery{Limit: 5})
		if err != nil || len(preview) != 6 || requests != 601 || works != 600 {
			t.Fatalf("overview rows=%d requests=%d works=%d err=%v", len(preview), requests, works, err)
		}
		var candidateRows int64
		for i, sql := range log.Statements {
			if strings.Contains(sql, "pending_approvals") {
				candidateRows += max(log.Rows[i], 0)
			}
		}
		if candidateRows < 600 || candidateRows > 606 {
			t.Fatalf("overview must read one candidate pass and the selected owners: %d", candidateRows)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIMDependencyGraphBatchesChildrenAndIgnoresHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	compile := func(id, definition, source string, ordinal int64) models.WorkflowRevision {
		t.Helper()
		compiled, diagnostics, err := workflowdsl.Compile(source)
		if err != nil {
			t.Fatalf("compile: %v %+v", err, diagnostics)
		}
		return models.WorkflowRevision{ID: id, DefinitionID: definition, Ordinal: ordinal, Manifest: compiled.Manifest, Bundle: compiled.Bundle}
	}
	meta := `export const meta={sdk:'ally.workflow/1',title:'父流程',description:'调用子流程',phases:[{id:'phase',title:'调用',detail:'调用子流程',canExit:true}]};`
	leaf := compile("leaf-version", "leaf", `export const meta={sdk:'ally.workflow/1',title:'微信依赖',description:'发送',phases:[{id:'phase',title:'发送',detail:'交付',canExit:true}],actions:[{id:'send',label:'发送微信',description:'交付结果',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]};export const actions={send:async(ctx,input)=>await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'binding',content:'正文'}})};return await action('send',{id:'deliver',label:'发送结果',phase:'phase'});`, 1)
	definitions := []models.WorkflowDefinition{{ID: "parent", CurrentRevisionID: "parent-version"}, {ID: "leaf", CurrentRevisionID: leaf.ID}}
	revisions := []models.WorkflowRevision{leaf}
	var body strings.Builder
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("child-%03d", i)
		definitions = append(definitions, models.WorkflowDefinition{ID: id, CurrentRevisionID: id + "-version"})
		revisions = append(revisions, compile(id+"-version", id, meta+`return await workflow('leaf',{id:'leaf-call',label:'叶子',phase:'phase'});`, 1))
		fmt.Fprintf(&body, "await workflow('%s',{id:'call-%03d',label:'父调用',phase:'phase'});", id, i)
	}
	parent := compile("parent-version", "parent", meta+body.String()+"return null;", 1)
	revisions = append(revisions, parent)
	// 历史版本故意携带不可解析的依赖声明；若查询越界读取历史会报错。
	invalid := leaf
	invalid.Manifest.Meta.Actions[0].IMTargetIDs = []string{strings.Repeat("x", 33)}
	for i := 0; i < 600; i++ {
		revision := invalid
		revision.ID = fmt.Sprintf("history-%04d", i)
		revision.Ordinal = int64(i + 2)
		revisions = append(revisions, revision)
	}
	// 上述切片共享测试数据，重新编译持久化的有效叶版本。
	revisions[0] = compile(leaf.ID, "leaf", `export const meta={sdk:'ally.workflow/1',title:'发送',description:'发送',phases:[{id:'phase',title:'发送',detail:'交付',canExit:true}],actions:[{id:'send',label:'发送',description:'交付',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012'],imTargetIds:['binding']}]};export const actions={send:async(ctx,input)=>null};return await action('send',{id:'deliver',label:'发送',phase:'phase'});`, 1)
	for _, value := range []any{&definitions, &revisions} {
		if err := db.CreateInBatches(value, 25).Error; err != nil {
			t.Fatal(err)
		}
	}
	store := NewIMController(db)
	if err := store.SaveConnection(ctx, models.IMConnection{ID: "connection"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.IMBinding{ID: "binding", ConnectionID: "connection"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowRun{ID: "active", DefinitionID: "parent", RevisionID: parent.ID, Status: constants.WorkflowRunStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	log := &notificationQueryLog{Interface: logger.Discard}
	controller := &imController{db: db.Session(&gorm.Session{Logger: log})}
	var deps models.IMConnectionDependencies
	err := NewTransactionController(dbhelper.NewGormTransactionManager(controller.db)).Transaction(ctx, func(ctx context.Context) error {
		var err error
		deps, err = controller.ConnectionDependencies(ctx, "connection")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	parentCount := 0
	for _, dep := range deps.Workflows {
		if dep.DefinitionID == "parent" {
			parentCount++
			if dep.RunID != "active" || dep.Label != "父调用" || !strings.HasPrefix(dep.NodeID, "call-") {
				t.Fatalf("lost parent location: %+v", dep)
			}
		}
	}
	if parentCount != 100 {
		t.Fatalf("parent references=%d", parentCount)
	}
	if len(log.Statements) == 0 || len(log.Statements) > 20 {
		t.Fatalf("child N+1 reads: %d", len(log.Statements))
	}
	for i, count := range log.Rows {
		if count > repositoryReadBatch {
			t.Fatalf("loaded revision history: %d %s", count, log.Statements[i])
		}
	}
}
