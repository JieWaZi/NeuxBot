package work_board

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/notification"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

func boardDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "board.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB.DB()
	t.Cleanup(func() { _ = sql.Close() })
	oldTransactions := global.TransactionControllerManager
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	t.Cleanup(func() { global.TransactionControllerManager = oldTransactions })
	oldBoard, oldResults := global.WorkBoardControllerManager, global.WorkResultControllerManager
	oldNotifications := global.NotificationService
	global.WorkBoardControllerManager = controllers.NewWorkBoardController(db.DB)
	global.WorkResultControllerManager = controllers.NewWorkResultController(db.DB)
	hub := events.New()
	global.NotificationService = notification.New(controllers.NewNotificationController(db.DB), global.WorkResultControllerManager, controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), hub)
	t.Cleanup(func() { global.NotificationService.Close(); hub.Close(); global.NotificationService = oldNotifications })
	if err := global.WorkResultControllerManager.EnableTracking(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { global.WorkBoardControllerManager, global.WorkResultControllerManager = oldBoard, oldResults })
	return db.DB
}

func TestBoardClearResultsKeepsAttentionAndOriginalResults(t *testing.T) {
	db := boardDatabase(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	wakerID := util.New()
	if err := db.Create(&models.Waker{ID: wakerID, Name: "资料伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 7)
	for index := range 7 {
		id := util.New()
		ids = append(ids, id)
		chat := models.Chat{ID: id, WakerID: wakerID, Title: "待查看成果", UpdatedAt: now}
		if index == 0 {
			chat.ActiveExecutionID = util.New()
			chat.WaitingApproval = true
			chat.PendingApprovals = []models.PendingApproval{{ApprovalID: util.New(), Title: "需要许可", RequestedAt: now}}
		}
		if err := db.Create(&chat).Error; err != nil {
			t.Fatal(err)
		}
		if err := global.WorkResultControllerManager.Publish(ctx, models.WorkResultReceipt{
			OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: id,
			LatestVersion: 1, AvailableAt: now,
			Reference: models.WorkResultReference{ChatID: id, MessageID: id},
		}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := before.(WorkBoardSummary)
	if summary.ResultCount != 7 || len(summary.Results) != 5 || summary.AttentionCount != 1 {
		t.Fatalf("清空前统计不正确: %+v", summary)
	}
	if _, err := callBoard(t, &ClearWorkResults{}, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	after, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary = after.(WorkBoardSummary)
	if summary.ResultCount != 0 || len(summary.Results) != 0 || summary.AttentionCount != 1 {
		t.Fatalf("清空未覆盖全部成果或影响了需要操作: %+v", summary)
	}
	for _, id := range ids {
		output, err := callBoard(t, &GetWorkResult{}, "", gin.Params{
			{Key: "ownerKind", Value: "chat"}, {Key: "ownerId", Value: id},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result := output.(WorkResultResponse).Result
		if result == nil || result.LatestVersion != 1 || result.ViewedVersion != 1 || result.Reference.MessageID != id {
			t.Fatalf("清空改写了原成果: %+v", result)
		}
	}
	if err := global.WorkResultControllerManager.Publish(ctx, models.WorkResultReceipt{
		OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: ids[0],
		LatestVersion: 2, AvailableAt: now + 1,
		Reference: models.WorkResultReference{ChatID: ids[0], MessageID: "new-result"},
	}); err != nil {
		t.Fatal(err)
	}
	next, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value := next.(WorkBoardSummary); value.ResultCount != 1 || value.AttentionCount != 1 {
		t.Fatalf("后续新结果未重新待查看: %+v", value)
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
	receipts := make([]models.WorkResultReceipt, 0, 760)
	for i := range 760 {
		id := fmt.Sprintf("%032x", i+1)
		title, at := "常规资料", now
		if i < 150 {
			title, at = "罕见%_资料", now-1000
		}
		chats = append(chats, models.Chat{ID: id, WakerID: waker, Title: title, UpdatedAt: at})
		receipts = append(receipts, models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: id, LatestVersion: 1, AvailableAt: at, Reference: models.WorkResultReference{ChatID: id, MessageID: id}})
	}
	if err := db.CreateInBatches(chats, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(receipts, 100).Error; err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"all", "results"} {
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
	values := []any{
		&models.WorkflowDefinition{ID: definitionID, Title: "资料流程"},
		&models.WorkflowDefinition{ID: childDefinitionID, Title: "独立校验流程"},
		&models.WorkflowRun{ID: rootID, DefinitionID: definitionID, RootRunID: rootID, Status: constants.WorkflowRunStatusRunning, UpdatedAt: now},
		&models.WorkflowRun{ID: childID, DefinitionID: childDefinitionID, RootRunID: rootID, ParentRunID: rootID, Joined: true, Status: constants.WorkflowRunStatusWaiting, UpdatedAt: now},
		&models.Chat{ID: workerID, WakerID: util.New(), HiddenFromWakerHistory: true, ActiveExecutionID: executionID, WaitingApproval: true, PendingApprovals: []models.PendingApproval{{ApprovalID: util.New(), Title: "工作器授权", RequestedAt: now}}},
		&models.WorkflowEffect{ID: util.New(), RunID: childID, ChatID: workerID, ExecutionID: executionID, Status: constants.WorkflowStepStatusWaiting},
		&models.WorkflowEffect{ID: util.New(), RunID: childID, ChatID: workerID, ExecutionID: util.New(), Status: constants.WorkflowStepStatusFailed},
		&models.WorkflowHumanRequest{ID: util.New(), RunID: childID, Prompt: "补充条件", Status: constants.WorkflowStepStatusWaiting, DeadlineAt: now + 60000, CreatedAt: now},
		&models.WorkflowHumanRequest{ID: util.New(), RunID: childID, Prompt: "已过期", Status: constants.WorkflowStepStatusWaiting, DeadlineAt: now - 1, CreatedAt: now},
		&models.WorkflowOperationRequest{ID: util.New(), EffectID: util.New(), RunID: rootID, Description: "宿主授权", Status: constants.WorkflowStepStatusWaiting, CreatedAt: now},
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
	if summary.AttentionCount != 3 {
		t.Fatalf("workflow attention=%+v", summary)
	}
	for _, v := range summary.Attention {
		if v.RunID == childID && (v.WorkflowID != childDefinitionID || v.ExecutorID != definitionID) {
			t.Fatalf("子流程精确目标和顶层范围不应混淆: %+v", v)
		}
		if v.OwnerID != rootID || (v.Title == "工作器授权" && (v.ChatID != workerID || v.RunID != childID)) {
			t.Fatalf("lost exact target=%+v", v)
		}
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", rootID).Update("status", constants.WorkflowRunStatusCancelled).Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.(WorkBoardSummary).AttentionCount != 0 {
		t.Fatal("stopped root retained child/worker requests")
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", childID).Update("joined", false).Error; err != nil {
		t.Fatal(err)
	}
	grandchildID := util.New()
	if err := db.Create(&models.WorkflowRun{ID: grandchildID, DefinitionID: childDefinitionID, RootRunID: rootID, ParentRunID: childID, Joined: true, Status: constants.WorkflowRunStatusWaiting}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowHumanRequest{ID: util.New(), RunID: grandchildID, Prompt: "后台子树输入", Status: constants.WorkflowStepStatusWaiting, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil || out.(WorkBoardSummary).AttentionCount != 3 {
		t.Fatalf("detached 子树不随顶层停止丢失有效请求: %+v %v", out, err)
	}
	if err := db.Model(&models.WorkflowRun{}).Where("id = ?", childID).Update("status", constants.WorkflowRunStatusCancelling).Error; err != nil {
		t.Fatal(err)
	}
	out, err = callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil || out.(WorkBoardSummary).AttentionCount != 0 {
		t.Fatalf("取消中的 joined 子树不应继续提供操作: %+v %v", out, err)
	}
}

func TestBoardHTTPRetainsReadableInternalChatAfterIMLinkRemoved(t *testing.T) {
	db := boardDatabase(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	chatID, wakerID, linkID := util.New(), util.New(), util.New()
	if err := db.Create(&models.Waker{ID: wakerID, Name: "IM 伙伴"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Chat{ID: chatID, WakerID: wakerID, Title: "保留工作", HiddenFromWakerHistory: true, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.IMConversationLink{ID: linkID, ChatID: chatID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.WorkResultControllerManager.PublishChat(ctx, chatID, 5, now, models.WorkResultReference{ChatID: chatID, MessageID: util.New()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", linkID).Delete(&models.IMConversationLink{}).Error; err != nil {
		t.Fatal(err)
	}
	out, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := out.(WorkBoardSummary)
	if summary.ResultCount != 1 || len(summary.Results) != 1 || summary.Results[0].OwnerID != chatID || summary.Results[0].Source != "chat" {
		t.Fatalf("retained internal result disappeared=%+v", summary)
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
	if summary.AttentionCount != 3 || len(summary.Attention) != 3 || summary.Attention[0].CreatedAt != 10 {
		t.Fatalf("oldest request summary=%+v", summary)
	}
	for _, kind := range []string{"approval", "input", "review"} {
		filtered, filterErr := callBoard(t, &ListWorkBoard{}, "?mode=attention&states="+kind+"&limit=1", nil, nil)
		if filterErr != nil {
			t.Fatal(filterErr)
		}
		filteredPage := filtered.(WorkBoardPage)
		if filteredPage.TotalSize != 1 || len(filteredPage.Attention) != 1 || filteredPage.Attention[0].Kind != kind || filteredPage.HasMore {
			t.Fatalf("wrong pending state %s: %+v", kind, filteredPage)
		}
	}
	filtered, err := callBoard(t, &ListWorkBoard{}, "?mode=attention&states=approval,input&limit=1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	filteredPage := filtered.(WorkBoardPage)
	if filteredPage.TotalSize != 2 || !filteredPage.HasMore || filteredPage.NextCursor == "" {
		t.Fatalf("filtered pending pagination=%+v", filteredPage)
	}
	next, err := callBoard(t, &ListWorkBoard{}, "?mode=attention&states=approval,input&limit=1&cursor="+filteredPage.NextCursor, nil, nil)
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
	if out.(WorkBoardSummary).AttentionCount != 1 {
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
	if out.(WorkBoardSummary).AttentionCount != 2 {
		t.Fatalf("ended execution retained stale approval: %+v", out)
	}
}

func TestBoardHTTPReadingRacesAndStableTiedPagination(t *testing.T) {
	db := boardDatabase(t)
	ctx := context.Background()
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
	a := models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: ids[0], LatestVersion: 10, AvailableAt: now, Reference: models.WorkResultReference{ChatID: ids[0], MessageID: util.New()}}
	if err := global.WorkResultControllerManager.Publish(ctx, a); err != nil {
		t.Fatal(err)
	}
	b := a
	b.LatestVersion = 20
	b.Reference.MessageID = util.New()
	if err := global.WorkResultControllerManager.Publish(ctx, b); err != nil {
		t.Fatal(err)
	}
	params := gin.Params{{Key: "ownerKind", Value: "chat"}, {Key: "ownerId", Value: ids[0]}}
	if _, err := callBoard(t, &ViewWorkResult{}, "", params, map[string]any{"version": a.LatestVersion, "reference": a.Reference}); err != nil {
		t.Fatal(err)
	}
	out, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.(WorkBoardSummary).ResultCount != 1 {
		t.Fatal("view A cleared B")
	}
	if _, err := callBoard(t, &ViewWorkResult{}, "", params, map[string]any{"version": b.LatestVersion, "reference": a.Reference}); err == nil {
		t.Fatal("wrong exact result accepted")
	}
	if _, err := callBoard(t, &ViewWorkResult{}, "", params, map[string]any{"version": b.LatestVersion, "reference": b.Reference}); err != nil {
		t.Fatal(err)
	}
	if _, err := callBoard(t, &ViewWorkResult{}, "", params, map[string]any{"version": a.LatestVersion, "reference": a.Reference}); err != nil {
		t.Fatal(err)
	}
	v, _, err := global.WorkResultControllerManager.Get(ctx, a.OwnerKind, a.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if v.ViewedVersion != 20 {
		t.Fatalf("late A regressed: %+v", v)
	}
	if _, err := callBoard(t, &ViewWorkResult{}, "", params, map[string]any{"version": 30, "reference": b.Reference}); err == nil {
		t.Fatal("future result accepted")
	}
	if _, err := callBoard(t, &ListWorkBoard{}, "?cursor=invalid!", nil, nil); err == nil {
		t.Fatal("invalid cursor accepted")
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
	if summary.WorkCount != 5 || summary.RunningCount != 1 || summary.EndedCount != 3 || summary.ActionWorkCount != 1 || summary.AttentionCount != 2 {
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
	if summary.WorkCount != 1 || summary.EndedCount != 0 || summary.ActionWorkCount != 1 || summary.AttentionCount != 2 {
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
