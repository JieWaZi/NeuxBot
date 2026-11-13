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
	log := &repositoryQueryLog{Interface: logger.Discard}
	board := &workBoardController{db: db.Session(&gorm.Session{Logger: log})}
	err := NewTransactionController(dbhelper.NewGormTransactionManager(board.db)).Transaction(ctx, func(ctx context.Context) error {
		page, total, _, err := board.Attention(ctx, WorkBoardQuery{Offset: 570, Limit: 5})
		if err != nil || total != 600 || len(page) != 6 || page[0].OwnerID != "pending-0570" {
			t.Fatalf("deep attention=%+v count=%d err=%v", page, total, err)
		}
		for i, count := range log.Rows {
			if count > repositoryReadBatch {
				t.Fatalf("unbounded attention batch: %d %s", count, log.Statements[i])
			}
		}
		log.Rows, log.Statements = nil, nil
		entries := []models.WorkBoardEntry{{OwnerKind: constants.WorkOwnerKindChat, OwnerID: "pending-0570"}}
		if err := board.attachAttentionCounts(ctx, entries, nil); err != nil {
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
		page, total, _, err = board.Attention(ctx, WorkBoardQuery{Offset: 999999, Limit: 5})
		if err != nil || total != 600 || len(page) != 0 {
			t.Fatalf("out of range page=%+v total=%d err=%v", page, total, err)
		}
		// 同一容器的多个请求只计一份工作；概览不能为工作数再次读取全部候选。
		if err := controllerDB(ctx, board.db).Model(&models.Chat{}).Where("id=?", "pending-0000").Update("pending_approvals", `[{"approvalId":"first","title":"允许","requestedAt":10},{"approvalId":"second","title":"允许","requestedAt":10}]`).Error; err != nil {
			t.Fatal(err)
		}
		log.Rows, log.Statements = nil, nil
		preview, requests, works, err := board.Attention(ctx, WorkBoardQuery{Limit: 5})
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
	meta := `export const meta={sdk:'ally.workflow',title:'父流程',description:'调用子流程',phases:[{id:'phase',title:'调用',detail:'调用子流程',canExit:true}]};`
	leaf := compile("leaf-version", "leaf", `export const meta={sdk:'ally.workflow',title:'微信依赖',description:'发送',phases:[{id:'phase',title:'发送',detail:'交付',canExit:true}],actions:[{id:'send',label:'发送微信',description:'交付结果',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012']}]};const actions={send:async(ctx,input)=>await ctx.callTool({callId:'send',connectorId:'b17d96aa55314c33ab46c172e8cab012',toolName:'im_delivery',input:{action:'deliveries.create',targetId:'binding',content:'正文'}})};return await action('send',{id:'deliver',label:'发送结果',phase:'phase'});`, 1)
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
	revisions[0] = compile(leaf.ID, "leaf", `export const meta={sdk:'ally.workflow',title:'发送',description:'发送',phases:[{id:'phase',title:'发送',detail:'交付',canExit:true}],actions:[{id:'send',label:'发送',description:'交付',risk:'external',type:'builtin',handler:'send',connectorIds:['b17d96aa55314c33ab46c172e8cab012'],imTargetIds:['binding']}]};const actions={send:async(ctx,input)=>null};return await action('send',{id:'deliver',label:'发送',phase:'phase'});`, 1)
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
	log := &repositoryQueryLog{Interface: logger.Discard}
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
