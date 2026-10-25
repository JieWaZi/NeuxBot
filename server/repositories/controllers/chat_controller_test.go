package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestChatNotificationProjectionRejectsMalformedFieldsWithStableError(t *testing.T) {
	for _, field := range []string{"pending_approvals", "pending_elicitations"} {
		t.Run(field, func(t *testing.T) {
			for name, value := range map[string]any{"malformed_json": "{", "wrong_shape": map[string]string{"unexpected": "object"}, "unsupported_type": make(chan int)} {
				t.Run(name, func(t *testing.T) {
					hub := events.New()
					defer hub.Close()
					database := WithEventPublisher(newTestDB(t), hub)
					if err := database.Create(&models.Chat{ID: "chat", WakerID: "waker"}).Error; err != nil {
						t.Fatal(err)
					}
					changes := hub.Subscribe("", events.Resource{Kind: "global"})
					defer changes.Close()
					controller := NewChatController(database)
					err := controller.AdvanceProjection(context.Background(), "chat", 0, map[string]any{field: value, "latest_sequence": 1})
					if !util.HasStatusCode(err, status_error.ChatLogIndexCorrupted) {
						t.Fatalf("invalid notification projection must return a stable index error: %v", err)
					}
					chat, err := controller.Get(context.Background(), "chat")
					if err != nil || chat.LatestSequence != 0 || len(changes.Events) != 0 {
						t.Fatalf("invalid projection changed committed facts: chat=%+v events=%d err=%v", chat, len(changes.Events), err)
					}
				})
			}
		})
	}
}

func TestDeleteChatPublishesIMChangesOnlyForAffectedConnections(t *testing.T) {
	for _, target := range []string{"partner_chat", "team_member_chat"} {
		t.Run(target, func(t *testing.T) {
			hub := events.New()
			defer hub.Close()
			database := WithEventPublisher(newTestDB(t), hub)
			controller := NewChatController(database)
			ctx := context.Background()
			if err := database.Create(&models.Chat{ID: "deleted-chat", WakerID: "waker"}).Error; err != nil {
				t.Fatal(err)
			}
			for _, binding := range []models.IMBinding{
				{ID: "related-binding", ConnectionID: "related-connection", ExternalChatID: "external-one"},
				{ID: "also-related-binding", ConnectionID: "related-connection", ExternalChatID: "external-two"},
				{ID: "unrelated-binding", ConnectionID: "unrelated-connection", ExternalChatID: "external-three"},
			} {
				if err := database.Create(&binding).Error; err != nil {
					t.Fatal(err)
				}
			}
			chatID, threadID := "deleted-chat", ""
			if target == "team_member_chat" {
				chatID, threadID = "lead-chat", "related-thread"
				if err := database.Create(&models.ChannelThread{ID: threadID, ChannelID: "team", LeadChatID: chatID, AdditionalWorkingDirectories: []string{}}).Error; err != nil {
					t.Fatal(err)
				}
				if err := database.Create(&models.ChannelThreadMember{ThreadID: threadID, WakerID: "member", ChatID: "deleted-chat"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, link := range []models.IMConversationLink{
				{ID: "related-link", BindingID: "related-binding", ChatID: chatID, ThreadID: threadID},
				{ID: "duplicate-reference", BindingID: "related-binding", ChatID: chatID, ThreadID: threadID},
				{ID: "also-related-link", BindingID: "also-related-binding", ChatID: chatID, ThreadID: threadID},
				{ID: "unrelated-link", BindingID: "unrelated-binding", ChatID: "other-chat"},
			} {
				if err := database.Create(&link).Error; err != nil {
					t.Fatal(err)
				}
			}
			related := hub.Subscribe("im", events.Resource{Kind: "connection", ID: "related-connection"})
			defer related.Close()
			unrelated := hub.Subscribe("im", events.Resource{Kind: "connection", ID: "unrelated-connection"})
			defer unrelated.Close()
			binding := hub.Subscribe("im", events.Resource{Kind: "binding", ID: "related-binding"})
			defer binding.Close()
			if err := controller.Delete(ctx, "deleted-chat"); err != nil {
				t.Fatal(err)
			}
			if len(related.Events) != 1 {
				t.Fatalf("删除 %s 后受影响连接未收到唯一刷新事件: %d", target, len(related.Events))
			}
			event := <-related.Events
			if event.Type != "im.changed" || event.Resource != (events.Resource{Kind: "connection", ID: "related-connection"}) {
				t.Fatalf("删除会话发布了错误主题或范围: %+v", event)
			}
			if len(binding.Events) != 1 || len(unrelated.Events) != 0 {
				t.Fatalf("绑定未刷新或无关连接受到影响: binding=%d unrelated=%d", len(binding.Events), len(unrelated.Events))
			}
			var retained int64
			if err := database.Model(&models.IMConversationLink{}).Count(&retained).Error; err != nil || retained != 4 {
				t.Fatalf("状态刷新删除了原 IM 关联历史: count=%d error=%v", retained, err)
			}
		})
	}
}

func TestDeleteChatIMChangesArePublishedOnlyAfterCommit(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	controller := NewChatController(database)
	transactions := NewTransactionController(dbhelper.NewGormTransactionManager(database))
	ctx := context.Background()
	for _, value := range []any{
		&models.Chat{ID: "deleted-chat", WakerID: "waker"},
		&models.IMBinding{ID: "binding", ConnectionID: "connection", ExternalChatID: "external"},
		&models.IMConversationLink{ID: "link", BindingID: "binding", ChatID: "deleted-chat"},
	} {
		if err := database.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	subscription := hub.Subscribe("im", events.Resource{Kind: "connection", ID: "connection"})
	defer subscription.Close()
	if err := transactions.Transaction(ctx, func(transactionContext context.Context) error {
		if err := controller.Delete(transactionContext, "deleted-chat"); err != nil {
			return err
		}
		if len(subscription.Events) != 0 {
			t.Fatal("IM 会话删除事件提前暴露了尚未提交的写入")
		}
		return status_error.BadRequest
	}); err == nil {
		t.Fatal("测试回滚未生效")
	}
	if len(subscription.Events) != 0 {
		t.Fatal("回滚的会话删除发布了成功 IM 刷新事件")
	}
	if _, err := controller.Get(ctx, "deleted-chat"); err != nil {
		t.Fatalf("回滚未保留原会话: %v", err)
	}
	if err := transactions.Transaction(ctx, func(transactionContext context.Context) error {
		if err := controller.Delete(transactionContext, "deleted-chat"); err != nil {
			return err
		}
		if len(subscription.Events) != 0 {
			t.Fatal("成功事务提交前出现 IM 刷新事件")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(subscription.Events) != 1 {
		t.Fatalf("成功提交会话删除后没有唯一 IM 刷新事件: %d", len(subscription.Events))
	}
}

func TestDeleteChatWithoutPublisherDoesNotReadIMTables(t *testing.T) {
	database := newTestDB(t)
	controller := NewChatController(database)
	ctx := context.Background()
	if err := database.Create(&models.Chat{ID: "deleted-chat", WakerID: "waker"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrator().DropTable(&models.IMConversationLink{}, &models.IMBinding{}, &models.ChannelThreadMember{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Delete(ctx, "deleted-chat"); err != nil {
		t.Fatalf("无事件端口的 Chat 删除引入了无关 IM 表依赖: %v", err)
	}
}

func TestUnrelatedChatChangesDoNotPublishIMRefresh(t *testing.T) {
	hub := events.New()
	defer hub.Close()
	database := WithEventPublisher(newTestDB(t), hub)
	controller := NewChatController(database)
	ctx := context.Background()
	subscription := hub.Subscribe("im", events.Resource{Kind: "global"})
	defer subscription.Close()
	if err := controller.Create(ctx, models.Chat{ID: "unlinked-chat", WakerID: "waker"}); err != nil {
		t.Fatal(err)
	}
	if err := controller.AdvanceProjection(ctx, "unlinked-chat", 0, map[string]any{"title": "修改工作标题", "latest_sequence": 1}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Delete(ctx, "unlinked-chat"); err != nil {
		t.Fatal(err)
	}
	if len(subscription.Events) != 0 {
		t.Fatalf("无关联的工作变化触发了 IM 全局刷新: %d", len(subscription.Events))
	}
}

func TestChatControllerListsActivityStatesByWaker(t *testing.T) {
	database := newTestDB(t)
	controller := NewChatController(database)
	ctx := context.Background()
	runningWakerID := "11111111111111111111111111111111"
	waitingInputWakerID := "22222222222222222222222222222222"
	waitingApprovalWakerID := "33333333333333333333333333333333"
	idleWakerID := "44444444444444444444444444444444"

	for _, chat := range []models.Chat{
		{
			ID:                "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			WakerID:           runningWakerID,
			ActiveExecutionID: "execution-running",
		},
		{
			ID:                 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			WakerID:            waitingInputWakerID,
			ActiveExecutionID:  "execution-waiting",
			WaitingElicitation: true,
		},
		{
			ID:                "cccccccccccccccccccccccccccccccc",
			WakerID:           waitingApprovalWakerID,
			ActiveExecutionID: "execution-approval",
			WaitingApproval:   true,
		},
	} {
		if err := controller.Create(ctx, chat); err != nil {
			t.Fatalf("Create(%s) error = %v", chat.ID, err)
		}
	}

	summaries, err := controller.ListActivitySummariesByWaker(
		ctx,
		[]string{
			runningWakerID,
			waitingInputWakerID,
			waitingApprovalWakerID,
			idleWakerID,
		},
	)
	if err != nil {
		t.Fatalf("ListActivitySummariesByWaker() error = %v", err)
	}
	if summaries[runningWakerID].WakerActivityState() != constants.WakerActivityStateRunning {
		t.Fatalf("running state = %+v, want running", summaries[runningWakerID])
	}
	if summaries[runningWakerID].RunningCount != 1 {
		t.Fatalf("running count = %d, want 1", summaries[runningWakerID].RunningCount)
	}
	if summaries[waitingInputWakerID].WakerActivityState() != constants.WakerActivityStateWaiting {
		t.Fatalf("waiting input state = %+v, want waiting", summaries[waitingInputWakerID])
	}
	if summaries[waitingInputWakerID].WaitingCount != 1 ||
		summaries[waitingInputWakerID].RunningCount != 0 {
		t.Fatalf("waiting input counts = %+v, want waiting=1 running=0", summaries[waitingInputWakerID])
	}
	if summaries[waitingApprovalWakerID].WakerActivityState() != constants.WakerActivityStateWaiting {
		t.Fatalf(
			"waiting approval state = %+v, want waiting",
			summaries[waitingApprovalWakerID],
		)
	}
	if summaries[waitingApprovalWakerID].WaitingCount != 1 ||
		summaries[waitingApprovalWakerID].RunningCount != 0 {
		t.Fatalf("waiting approval counts = %+v, want waiting=1 running=0", summaries[waitingApprovalWakerID])
	}
	if summaries[idleWakerID].WakerActivityState() != constants.WakerActivityStateIdle {
		t.Fatalf("idle state = %+v, want idle", summaries[idleWakerID])
	}

	empty, err := controller.ListActivitySummariesByWaker(ctx, nil)
	if err != nil {
		t.Fatalf("ListActivitySummariesByWaker(nil) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty summaries = %+v, want empty map", empty)
	}
}

// TestChatControllerActivityCountsRemainExclusivePerChat 验证会话等待期间计数互斥且恢复后重新计入运行。
func TestChatControllerActivityCountsRemainExclusivePerChat(t *testing.T) {
	for _, waitingField := range []string{"waiting_approval", "waiting_elicitation"} {
		t.Run(waitingField, func(t *testing.T) {
			database := newTestDB(t)
			controller := NewChatController(database)
			ctx := context.Background()
			wakerID := "11111111111111111111111111111111"
			waitingChatID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

			for _, chat := range []models.Chat{
				{
					ID:                "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					WakerID:           wakerID,
					ActiveExecutionID: "execution-running",
				},
				{
					ID:                waitingChatID,
					WakerID:           wakerID,
					ActiveExecutionID: "execution-waiting",
				},
			} {
				if err := controller.Create(ctx, chat); err != nil {
					t.Fatalf("Create(%s) error = %v", chat.ID, err)
				}
			}

			assertCounts := func(running, waiting int) {
				t.Helper()
				summaries, err := controller.ListActivitySummariesByWaker(ctx, []string{wakerID})
				if err != nil {
					t.Fatalf("ListActivitySummariesByWaker() error = %v", err)
				}
				got := summaries[wakerID]
				if got.RunningCount != running || got.WaitingCount != waiting {
					t.Fatalf("activity counts = %+v, want running=%d waiting=%d", got, running, waiting)
				}
			}

			if err := controller.AdvanceProjection(
				ctx,
				waitingChatID,
				0,
				map[string]any{waitingField: true, "latest_sequence": 1},
			); err != nil {
				t.Fatalf("mark waiting: %v", err)
			}
			assertCounts(1, 1)

			if err := controller.AdvanceProjection(
				ctx,
				waitingChatID,
				1,
				map[string]any{waitingField: false, "latest_sequence": 2},
			); err != nil {
				t.Fatalf("resume execution: %v", err)
			}
			assertCounts(2, 0)

			if err := controller.AdvanceProjection(
				ctx,
				waitingChatID,
				2,
				map[string]any{"active_execution_id": "", "latest_sequence": 3},
			); err != nil {
				t.Fatalf("finish execution: %v", err)
			}
			assertCounts(1, 0)
		})
	}
}

func TestChatControllerWorkRecordCounts(t *testing.T) {
	database := newTestDB(t)
	controller := NewChatController(database)
	ctx := context.Background()
	wakerID := "11111111111111111111111111111111"
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	dayOne := time.Date(2026, 8, 1, 10, 0, 0, 0, loc).UnixMilli()
	dayOneLater := time.Date(2026, 8, 1, 18, 0, 0, 0, loc).UnixMilli()
	dayTwo := time.Date(2026, 8, 2, 9, 0, 0, 0, loc).UnixMilli()

	for _, chat := range []models.Chat{
		{
			ID:                 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			WakerID:            wakerID,
			WorkspaceDirectory: "/tmp/project-a",
			Title:              "对话 A",
			ActiveExecutionID:  "exec-active",
			CreatedAt:          dayOne,
			UpdatedAt:          dayOne,
		},
		{
			ID:                 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			WakerID:            wakerID,
			WorkspaceDirectory: "/tmp/project-b",
			Title:              "对话 B",
			CreatedAt:          dayOneLater,
			UpdatedAt:          dayOneLater,
		},
		{
			ID:                 "cccccccccccccccccccccccccccccccc",
			WakerID:            wakerID,
			WorkspaceDirectory: "/tmp/project-c",
			Title:              "对话 C",
			CreatedAt:          dayTwo,
			UpdatedAt:          dayTwo,
		},
	} {
		if err := controller.Create(ctx, chat); err != nil {
			t.Fatalf("Create(%s) error = %v", chat.ID, err)
		}
	}

	total, err := controller.CountByWaker(ctx, wakerID)
	if err != nil {
		t.Fatalf("CountByWaker() error = %v", err)
	}
	if total != 3 {
		t.Fatalf("CountByWaker() = %d, want 3", total)
	}

	active, err := controller.CountActiveByWaker(ctx, wakerID)
	if err != nil {
		t.Fatalf("CountActiveByWaker() error = %v", err)
	}
	if active != 1 {
		t.Fatalf("CountActiveByWaker() = %d, want 1", active)
	}

	startMs := time.Date(2026, 8, 1, 0, 0, 0, 0, loc).UnixMilli()
	endMs := time.Date(2026, 8, 2, 23, 59, 59, 999999999, loc).UnixMilli()
	counts, err := controller.CountCreatedByWakerGroupedByDay(
		ctx,
		wakerID,
		startMs,
		endMs,
		loc,
	)
	if err != nil {
		t.Fatalf("CountCreatedByWakerGroupedByDay() error = %v", err)
	}
	if counts["2026-08-01"] != 2 || counts["2026-08-02"] != 1 {
		t.Fatalf("daily counts = %+v, want 2 on 2026-08-01 and 1 on 2026-08-02", counts)
	}

	dayOneStart := time.Date(2026, 8, 1, 0, 0, 0, 0, loc).UnixMilli()
	dayOneEnd := time.Date(2026, 8, 1, 23, 59, 59, 999999999, loc).UnixMilli()
	updatedDayOne, err := controller.CountUpdatedByWakerBetween(ctx, wakerID, dayOneStart, dayOneEnd)
	if err != nil {
		t.Fatalf("CountUpdatedByWakerBetween() error = %v", err)
	}
	if updatedDayOne != 2 {
		t.Fatalf("CountUpdatedByWakerBetween(day one) = %d, want 2", updatedDayOne)
	}

	dayTwoStart := time.Date(2026, 8, 2, 0, 0, 0, 0, loc).UnixMilli()
	dayTwoEnd := time.Date(2026, 8, 2, 23, 59, 59, 999999999, loc).UnixMilli()
	updatedDayTwo, err := controller.CountUpdatedByWakerBetween(ctx, wakerID, dayTwoStart, dayTwoEnd)
	if err != nil {
		t.Fatalf("CountUpdatedByWakerBetween() error = %v", err)
	}
	if updatedDayTwo != 1 {
		t.Fatalf("CountUpdatedByWakerBetween(day two) = %d, want 1", updatedDayTwo)
	}
}

func TestChatControllerListsStableActivityCursor(t *testing.T) {
	database := newTestDB(t)
	controller := NewChatController(database)
	ctx := context.Background()
	wakerID := "11111111111111111111111111111111"

	for _, chat := range []models.Chat{
		{ID: "cccccccccccccccccccccccccccccccc", WakerID: wakerID, UpdatedAt: 200},
		{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", WakerID: wakerID, UpdatedAt: 200},
		{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WakerID: wakerID, UpdatedAt: 100},
	} {
		if err := controller.Create(ctx, chat); err != nil {
			t.Fatalf("Create(%s) error = %v", chat.ID, err)
		}
	}

	first, err := controller.ListByWakerBefore(ctx, wakerID, 0, "", 2)
	if err != nil {
		t.Fatalf("first ListByWakerBefore() error = %v", err)
	}
	if len(first) != 2 ||
		first[0].ID != "cccccccccccccccccccccccccccccccc" ||
		first[1].ID != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("first page = %+v, want c then b", first)
	}

	second, err := controller.ListByWakerBefore(
		ctx,
		wakerID,
		first[1].UpdatedAt,
		first[1].ID,
		2,
	)
	if err != nil {
		t.Fatalf("second ListByWakerBefore() error = %v", err)
	}
	if len(second) != 1 || second[0].ID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("second page = %+v, want a", second)
	}
}

func TestChatControllerPagesByUpdatedRange(t *testing.T) {
	database := newTestDB(t)
	controller := NewChatController(database)
	ctx := context.Background()
	wakerID := "11111111111111111111111111111111"
	otherWakerID := "22222222222222222222222222222222"

	for _, chat := range []models.Chat{
		{ID: "dddddddddddddddddddddddddddddddd", WakerID: wakerID, Title: "范围外任务", UpdatedAt: 400},
		{ID: "cccccccccccccccccccccccccccccccc", WakerID: wakerID, Title: "页面设计", UpdatedAt: 300},
		{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", WakerID: wakerID, Title: "页面开发", UpdatedAt: 200},
		{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WakerID: wakerID, Title: "范围外页面", UpdatedAt: 100},
		{ID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", WakerID: otherWakerID, Title: "其他伙伴页面", UpdatedAt: 300},
	} {
		if err := controller.Create(ctx, chat); err != nil {
			t.Fatalf("Create(%s) error = %v", chat.ID, err)
		}
	}

	resources, err := controller.ListByWakerUpdatedRange(ctx, wakerID, " 页面 ", 150, 350, 1, 1)
	if err != nil {
		t.Fatalf("ListByWakerUpdatedRange() error = %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("resources = %+v, want second matching Chat b", resources)
	}

	total, err := controller.CountByWakerUpdatedRange(ctx, wakerID, "页面", 150, 350)
	if err != nil {
		t.Fatalf("CountByWakerUpdatedRange() error = %v", err)
	}
	if total != 2 {
		t.Fatalf("CountByWakerUpdatedRange() = %d, want 2", total)
	}
}

func TestChatControllerAdvancesOnlyMatchingProjectionSequence(t *testing.T) {
	database := newTestDB(t)
	controller := NewChatController(database)
	ctx := context.Background()
	chatID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := controller.Create(ctx, models.Chat{
		ID:             chatID,
		WakerID:        "11111111111111111111111111111111",
		LatestSequence: 4,
		Title:          "before",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err := controller.AdvanceProjection(ctx, chatID, 3, map[string]any{
		"latest_sequence": 5,
		"title":           "must-not-skip",
	})
	var commonErr statuserror.CommonError
	if !errors.As(err, &commonErr) || commonErr.Code() != status_error.Conflict.Code() {
		t.Fatalf("AdvanceProjection(stale cursor) error = %v, want conflict", err)
	}
	unchanged, err := controller.Get(ctx, chatID)
	if err != nil {
		t.Fatalf("Get(unchanged) error = %v", err)
	}
	if unchanged.LatestSequence != 4 || unchanged.Title != "before" {
		t.Fatalf("stale advance changed projection = %+v", unchanged)
	}

	if err := controller.AdvanceProjection(ctx, chatID, 4, map[string]any{
		"latest_sequence": 5,
		"title":           "after",
	}); err != nil {
		t.Fatalf("AdvanceProjection(matching cursor) error = %v", err)
	}
	advanced, err := controller.Get(ctx, chatID)
	if err != nil {
		t.Fatalf("Get(advanced) error = %v", err)
	}
	if advanced.LatestSequence != 5 || advanced.Title != "after" {
		t.Fatalf("advanced projection = %+v", advanced)
	}
}

func TestWakerPaginationExcludesHiddenChats(t *testing.T) {
	db := newTestDB(t)
	c := NewChatController(db)
	ctx := context.Background()
	if err := c.Create(ctx, models.Chat{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WakerID: "waker-review", Title: "普通对话", UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, models.Chat{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", WakerID: "waker-review", Title: "自动任务运行", HiddenFromWakerHistory: true, UpdatedAt: 2}); err != nil {
		t.Fatal(err)
	}
	chats, err := c.ListByWakerUpdatedRange(ctx, "waker-review", "", 0, 0, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	count, err := c.CountByWakerUpdatedRange(ctx, "waker-review", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || count != 1 {
		t.Fatalf("隐藏 Chat 进入普通伙伴分页: len=%d count=%d chats=%+v", len(chats), count, chats)
	}
}

// TestChatControllerWakerActivityExcludesOwnedBackgroundChats 验证其他领域拥有的后台聊天不改变伙伴普通聊天活动。
func TestChatControllerWakerActivityExcludesOwnedBackgroundChats(t *testing.T) {
	database := newTestDB(t)
	controller := NewChatController(database)
	ctx := context.Background()
	wakerID := "11111111111111111111111111111111"
	for _, chat := range []models.Chat{
		{ID: "ordinary-running", WakerID: wakerID, ActiveExecutionID: "running"},
		{ID: "ordinary-waiting", WakerID: wakerID, ActiveExecutionID: "waiting", WaitingApproval: true},
		{ID: "workflow-worker", WakerID: wakerID, ActiveExecutionID: "workflow-execution", HiddenFromWakerHistory: true},
		{ID: "automatic-task", WakerID: wakerID, ActiveExecutionID: "automatic-execution", HiddenFromWakerHistory: true, WaitingApproval: true},
		{ID: "workflow-other-worker", WakerID: wakerID, ActiveExecutionID: "workflow-other-execution", HiddenFromWakerHistory: true, WaitingElicitation: true},
	} {
		if err := controller.Create(ctx, chat); err != nil {
			t.Fatal(err)
		}
	}
	summaries, err := controller.ListActivitySummariesByWaker(ctx, []string{wakerID})
	if err != nil {
		t.Fatal(err)
	}
	if got := summaries[wakerID]; got.RunningCount != 1 || got.WaitingCount != 1 {
		t.Fatalf("ordinary activity = %+v, want running=1 waiting=1", got)
	}
	active, err := controller.CountActiveByWaker(ctx, wakerID)
	if err != nil || active != 2 {
		t.Fatalf("ordinary active count = %d, error=%v, want 2", active, err)
	}
	for _, id := range []string{"workflow-worker", "automatic-task", "workflow-other-worker"} {
		chat, err := controller.Get(ctx, id)
		if err != nil || chat.ActiveExecutionID == "" || !chat.HiddenFromWakerHistory {
			t.Fatalf("background execution fact changed: %+v error=%v", chat, err)
		}
	}
}
