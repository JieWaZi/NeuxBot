package work_board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/workspace_attention"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"gorm.io/gorm/logger"
)

func TestMainConversationPublishedReportsUsePublicReadingBoundary(t *testing.T) {
	db := boardDatabase(t)
	ctx := t.Context()
	actor := models.Waker{ID: util.New(), Name: "报告伙伴"}
	other := models.Waker{ID: util.New(), Name: "其他伙伴"}
	if err := db.Create(&[]models.Waker{actor, other}).Error; err != nil {
		t.Fatal(err)
	}
	store := controllers.NewMainConversationController(db)
	service := mainconversation.New(store, global.TransactionControllerManager, controllers.NewWakerController(db), controllers.NewChannelController(db))
	previous := global.MainConversationService
	previousController := global.MainConversationControllerManager
	global.MainConversationService = service
	global.MainConversationControllerManager = store
	t.Cleanup(func() {
		global.MainConversationService = previous
		global.MainConversationControllerManager = previousController
	})
	public, err := service.Ensure(ctx, "waker", actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.ReserveWork(ctx, public.ID, actor.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	source := models.Chat{
		ID: work.WorkChatID, WakerID: actor.ID, HiddenFromWakerHistory: true, LatestReadableSequence: 4,
		ActiveExecutionID: "active", WaitingApproval: true,
		PendingApprovals: []models.PendingApproval{{ApprovalID: "ask"}},
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ChatReadableMessage{ChatID: source.ID, MessageID: "source-only", OrderSequence: 4, Kind: "answer"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.AdmitUser(ctx, "waker", actor.ID, "submission", "用户交办", "", source.ID, "", "", "", "", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	snapshot, err := callBoard(t, &workspace_attention.GetWorkspaceAttention{}, "", nil, nil)
	if err != nil || snapshot.(models.WorkspaceAttentionSnapshot).HasUnread {
		t.Fatalf("own message or hidden source became unread: %+v %v", snapshot, err)
	}
	publish := func(key string, wantReplay bool) models.MainConversationMessage {
		t.Helper()
		row, replay, err := service.PublishWithReceipt(ctx, source.ID, "active", key, key, "")
		if err != nil || replay != wantReplay {
			t.Fatalf("publish %q replay=%t: %v", key, replay, err)
		}
		return row
	}
	first := publish("first", false)
	if replay := publish("first", true); replay.ID != first.ID {
		t.Fatalf("replay changed identity: %+v", replay)
	}
	readAttention := func(want int64, message string, generation int64) {
		t.Helper()
		value, err := callBoard(t, &GetWorkBoardSummary{}, "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		summary := value.(WorkBoardSummary)
		if summary.UnreadMessageCount != want || summary.UnreadChatCount != 1 || summary.AwaitingViewWorkCount != 1 || len(summary.AwaitingViewPreview) != 1 {
			t.Fatalf("unexpected summary: %+v", summary)
		}
		item := summary.AwaitingViewPreview[0]
		if item.OwnerKind != "main_conversation" || item.OwnerID != public.ID || item.WakerID != actor.ID ||
			item.Title != actor.Name || item.ChatID != "" || item.FirstUnreadMessageID != message || item.UnreadMessageCount != want {
			t.Fatalf("report lost public identity: %+v", item)
		}
		summaries, health, err := global.WorkspaceAttention.ExecutorSummaries(ctx, constants.LocalReaderID, []string{actor.ID, other.ID})
		if err != nil || !health.Complete || len(summaries) != 2 || summaries[0].UnreadMessageCount != want || summaries[1].UnreadMessageCount != 0 ||
			summaries[0].PendingRequestCount != 1 || summaries[0].WaitingWorkCount != 1 {
			t.Fatalf("reading replaced pending approval or reached another subject: %+v %+v %v", summaries, health, err)
		}
		gotGeneration := summaries[0].MainConversationUnreadGeneration
		if gotGeneration == nil || *gotGeneration != generation || summaries[1].MainConversationUnreadGeneration != nil {
			t.Fatalf("the main unread generation was absent, stale, or on another subject: %+v", summaries)
		}
		payload, err := json.Marshal(summaries[0])
		if err != nil || !strings.Contains(string(payload), fmt.Sprintf(`"mainConversationUnreadGeneration":%d`, generation)) {
			t.Fatalf("unread generation is missing in JSON even though it was present in the snapshot: %s %v", payload, err)
		}
	}
	readAttention(1, first.ID, 0)
	snapshot, err = callBoard(t, &workspace_attention.GetWorkspaceAttention{}, "", nil, nil)
	if err != nil || !snapshot.(models.WorkspaceAttentionSnapshot).HasUnread {
		t.Fatalf("public report did not set the attention indicator: %+v %v", snapshot, err)
	}
	frozenValue, err := callBoard(t, &GetWorkBoardViewTargets{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	frozen := frozenValue.(WorkBoardViewTargets)
	if len(frozen.Targets) != 1 || frozen.Targets[0].Work.Kind != "main_conversation" || frozen.Targets[0].ThroughMessageID != first.ID || frozen.Targets[0].ChatID != "" {
		t.Fatalf("unexpected frozen target: %+v", frozen)
	}
	second := publish("second", false)
	readAttention(2, first.ID, 0)
	result, err := callBoard(t, &MarkWorkBoardViewed{}, "", nil, MarkWorkBoardViewedRequest{Targets: frozen.Targets})
	if err != nil || result.(models.MarkViewedResult).Applied != 1 {
		t.Fatalf("mark frozen target: %+v %v", result, err)
	}
	readAttention(1, second.ID, 0)
	frozenValue, err = callBoard(t, &GetWorkBoardViewTargets{}, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	frozen = frozenValue.(WorkBoardViewTargets)
	if len(frozen.Targets) != 1 || frozen.Targets[0].ThroughMessageID != second.ID {
		t.Fatalf("fresh target did not reflect the second report: %+v", frozen)
	}
	if err := store.MarkUnread(ctx, constants.LocalReaderID, public.ID, second.ID, frozen.Targets[0].ObservedUnreadGeneration); err != nil {
		t.Fatal(err)
	}
	result, err = callBoard(t, &MarkWorkBoardViewed{}, "", nil, MarkWorkBoardViewedRequest{Targets: frozen.Targets})
	if err != nil || result.(models.MarkViewedResult) != (models.MarkViewedResult{SkippedConflict: 1}) {
		t.Fatalf("a stale work-board batch cleared another window's unread intent: %+v %v", result, err)
	}
	readAttention(1, second.ID, 1)

	ordinary := models.Chat{ID: util.New(), WakerID: other.ID, LatestReadableSequence: 3}
	for _, row := range []any{
		&ordinary,
		&models.ChatReadCursor{ReaderID: constants.LocalReaderID, ChatID: ordinary.ID, UnreadGeneration: 4},
		&models.ChatReadableMessage{ChatID: ordinary.ID, MessageID: "ordinary-report", OrderSequence: 3, Kind: "answer"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	value, err := callBoard(t, &workspace_attention.GetExecutorAttention{}, "?executorIds="+other.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := value.(workspace_attention.ExecutorAttentionResponse).Resources[0]
	payload, err := json.Marshal(summary)
	if err != nil || summary.UnreadMessageCount != 1 || strings.Contains(string(payload), "mainConversationUnreadGeneration") {
		t.Fatalf("ordinary unread affected the main conversation generation: %s %v", payload, err)
	}
}

func TestMainConversationBoardFindsTeamReportsThroughReadableIndex(t *testing.T) {
	trace := &boardReadTrace{Interface: logger.Discard}
	db := boardDatabase(t, trace)
	ctx := t.Context()
	team := models.Channel{ID: util.New(), Name: "有回报的团队"}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	public, err := controllers.NewMainConversationController(db).Ensure(ctx, "team", team.ID)
	if err != nil {
		t.Fatal(err)
	}
	const userMessages = 500
	messages := make([]models.MainConversationMessage, 0, userMessages+2)
	for sequence := 1; sequence <= userMessages; sequence++ {
		messages = append(messages, models.MainConversationMessage{ID: fmt.Sprintf("%032x", sequence), ConversationID: public.ID, Role: "user", Sequence: int64(sequence)})
	}
	messages = append(messages,
		models.MainConversationMessage{ID: "first-report", ConversationID: public.ID, Role: "assistant", Sequence: userMessages + 1},
		models.MainConversationMessage{ID: "second-report", ConversationID: public.ID, Role: "assistant", Sequence: userMessages + 2},
	)
	if err := db.CreateInBatches(messages, 100).Error; err != nil {
		t.Fatal(err)
	}
	trace.statements = nil
	value, err := callBoard(t, &ListWorkBoard{}, "?mode=awaiting_view&source=team&limit=1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page := value.(WorkBoardPage)
	if page.TotalSize != 1 || len(page.AwaitingView) != 1 || page.UnreadMessageCount != 2 {
		t.Fatalf("unexpected team work page: %+v", page)
	}
	entry := page.AwaitingView[0]
	if entry.ChannelID != team.ID || entry.WakerID != "" || entry.Title != team.Name || entry.FirstUnreadMessageID != "first-report" || entry.LatestReadableMessageID != "second-report" {
		t.Fatalf("team target lost its public identities: %+v", entry)
	}
	const mainMessages = "main_conversation_messages"
	rangeQueries := 0
	statements := append([]string(nil), trace.statements...)
	for _, sql := range statements {
		if !strings.Contains(sql, mainMessages) {
			continue
		}
		if strings.Contains(sql, "AS first,") {
			rangeQueries++
		}
		var plan []struct {
			// Detail 是当前 SQLite 查询计划步骤。
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN " + sql).Scan(&plan).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "sequence>reading.through_sequence") {
			continue
		}
		hasBoundedRange := false
		for _, step := range plan {
			if strings.Contains(step.Detail, "SCAN "+mainMessages) {
				t.Fatalf("board read scanned the public timeline: %+v", plan)
			}
			if strings.Contains(step.Detail, "SEARCH "+mainMessages+" USING INDEX idx_main_conversation_readable (conversation_id=? AND sequence>?)") ||
				strings.Contains(step.Detail, "SEARCH "+mainMessages+" USING COVERING INDEX idx_main_conversation_readable (conversation_id=? AND sequence>?)") {
				hasBoundedRange = true
			}
		}
		if !hasBoundedRange {
			t.Fatalf("board read did not seek the eligible publication index: %+v", plan)
		}
	}
	if rangeQueries != 1 {
		t.Fatalf("read %d boundary queries for one displayed row", rangeQueries)
	}
}
