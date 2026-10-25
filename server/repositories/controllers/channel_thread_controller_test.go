package controllers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestChannelThreadControllerUpdatesOnlyFutureExecutionConfig(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-thread-controller.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})

	controller := NewChannelThreadController(database.DB)
	now := time.Now().UnixMilli()
	threadID := util.New()
	wakerID := util.New()
	chatID := util.New()
	thread := models.ChannelThread{
		ID:                           threadID,
		ChannelID:                    util.New(),
		Title:                        "团队会话",
		LeadWakerID:                  wakerID,
		LeadChatID:                   chatID,
		WorkspaceDirectory:           "/tmp/ally",
		AdditionalWorkingDirectories: []string{},
		MaxParallel:                  3,
		MaxTasks:                     12,
		MaxFailureAttempts:           2,
		ReadyAt:                      &now,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	members := []models.ChannelThreadMember{{
		ThreadID:        threadID,
		WakerID:         wakerID,
		DisplayName:     "历史负责人",
		Avatar:          "data:image/png;base64,history",
		ChatID:          chatID,
		IsLead:          true,
		RuntimeID:       "codex",
		Model:           "gpt-5.6",
		Reasoning:       "high",
		PermissionLevel: constants.PermissionLevelAuto,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}
	if err := controller.Create(context.Background(), thread, members); err != nil {
		t.Fatalf("create thread: %v", err)
	}

	updatedAt := now + 1
	member := members[0]
	member.RuntimeID = "claude"
	member.AccountID = "anthropic-account"
	member.Model = "sonnet"
	member.Reasoning = "medium"
	member.PermissionLevel = constants.PermissionLevelFullAccess
	member.UpdatedAt = updatedAt
	if err := controller.UpdateMemberExecutionConfigs(
		context.Background(),
		[]models.ChannelThreadMember{member},
	); err != nil {
		t.Fatalf("update member config: %v", err)
	}

	stored, err := controller.GetMember(context.Background(), threadID, wakerID)
	if err != nil {
		t.Fatalf("get member: %v", err)
	}
	if stored.ChatID != chatID || !stored.IsLead ||
		stored.DisplayName != "历史负责人" ||
		stored.Avatar != "data:image/png;base64,history" {
		t.Fatalf("stable member mapping changed: %+v", stored)
	}
	if stored.RuntimeID != "claude" || stored.AccountID != "anthropic-account" || stored.Model != "sonnet" ||
		stored.Reasoning != "medium" ||
		stored.PermissionLevel != constants.PermissionLevelFullAccess ||
		stored.UpdatedAt != updatedAt {
		t.Fatalf("execution config was not updated: %+v", stored)
	}

	missing := member
	missing.WakerID = util.New()
	if err := controller.UpdateMemberExecutionConfigs(
		context.Background(),
		[]models.ChannelThreadMember{missing},
	); err != nil {
		t.Fatalf("update missing member config returned ordinary zero-row error: %v", err)
	}
}

func TestChannelThreadControllerScopesClientRequestUniquenessToChannel(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelThreadController(database)
	ctx := context.Background()
	clientRequestID := "request-1"
	readyAt := time.Now().UnixMilli()
	create := func(threadID string, channelID string, chatID string) error {
		return controller.Create(ctx, models.ChannelThread{
			ID:                           threadID,
			ChannelID:                    channelID,
			ClientRequestID:              &clientRequestID,
			ClientRequestHash:            "hash-1",
			Title:                        "团队会话",
			LeadWakerID:                  "lead",
			LeadChatID:                   chatID,
			WorkspaceDirectory:           "/tmp/project",
			AdditionalWorkingDirectories: []string{},
			MaxParallel:                  3,
			MaxTasks:                     12,
			MaxFailureAttempts:           2,
			ReadyAt:                      &readyAt,
		}, []models.ChannelThreadMember{{
			ThreadID:        threadID,
			WakerID:         "lead",
			ChatID:          chatID,
			IsLead:          true,
			RuntimeID:       constants.RuntimeIDCodex,
			PermissionLevel: constants.PermissionLevelAuto,
		}})
	}
	if err := create("thread-1", "channel-1", "chat-1"); err != nil {
		t.Fatalf("create first reservation: %v", err)
	}
	stored, exists, err := controller.GetByClientRequestID(ctx, "channel-1", clientRequestID)
	if err != nil || !exists || stored.ID != "thread-1" {
		t.Fatalf("lookup reservation = %+v, exists=%v, error=%v", stored, exists, err)
	}
	if err := create("thread-2", "channel-1", "chat-2"); err == nil {
		t.Fatal("same channel accepted a duplicate client request identifier")
	}
	if err := create("thread-3", "channel-2", "chat-3"); err != nil {
		t.Fatalf("different channel rejected the same client request identifier: %v", err)
	}
	_, exists, err = controller.GetByClientRequestID(ctx, "channel-1", "missing")
	if err != nil || exists {
		t.Fatalf("missing reservation exists=%v, error=%v", exists, err)
	}
}

func TestChannelThreadEventsUseUUIDCursorAndStableOrdering(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-event-controller.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})
	controller := NewChannelThreadController(database.DB)
	ctx := context.Background()
	threadID := util.New()
	createdAt := time.Now().UnixMilli()
	first := models.ChannelEvent{
		ID:        "11111111111111111111111111111111",
		ChannelID: util.New(),
		ThreadID:  threadID,
		Type:      constants.ChannelEventTaskCreated,
		Payload:   []byte(`{"taskId":"a"}`),
		CreatedAt: createdAt,
	}
	second := first
	second.ID = "22222222222222222222222222222222"
	second.Payload = []byte(`{"taskId":"b"}`)
	if _, err := controller.AppendEvent(ctx, first); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if _, err := controller.AppendEvent(ctx, second); err != nil {
		t.Fatalf("append second event: %v", err)
	}
	events, err := controller.ListEvents(ctx, threadID, first.ID, 10)
	if err != nil || len(events) != 1 || events[0].ID != second.ID {
		t.Fatalf("events after UUID cursor = %+v, error = %v", events, err)
	}
	latest, err := controller.LatestEventCursor(ctx, threadID)
	if err != nil || latest != second.ID {
		t.Fatalf("latest event cursor = %q, error = %v", latest, err)
	}
	empty, err := controller.LatestEventCursor(ctx, util.New())
	if err != nil || empty != "" {
		t.Fatalf("empty latest event cursor = %q, error = %v", empty, err)
	}
}

func TestChannelThreadListUsesServerFiltersAndCursor(t *testing.T) {
	database := newTestDB(t)
	controller := NewChannelThreadController(database)
	ctx := context.Background()
	channelID := "11111111111111111111111111111111"
	wakerID := "22222222222222222222222222222222"
	readyAt := int64(1)
	threads := []models.ChannelThread{
		{
			ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ChannelID: channelID, Title: "发布检查", LeadWakerID: wakerID,
			LeadChatID: "cccccccccccccccccccccccccccccccc", WorkspaceDirectory: "/tmp/release", AdditionalWorkingDirectories: []string{},
			MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt, CreatedAt: 100, UpdatedAt: 200,
		},
		{
			ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ChannelID: channelID, Title: "历史整理", LeadWakerID: wakerID,
			LeadChatID: "dddddddddddddddddddddddddddddddd", WorkspaceDirectory: "/tmp/archive", AdditionalWorkingDirectories: []string{},
			MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt, CreatedAt: 100, UpdatedAt: 100,
		},
	}
	for _, thread := range threads {
		if err := controller.Create(ctx, thread, []models.ChannelThreadMember{{
			ThreadID: thread.ID, WakerID: wakerID, ChatID: thread.LeadChatID, IsLead: true,
			RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto,
		}}); err != nil {
			t.Fatalf("create thread %s: %v", thread.ID, err)
		}
		if err := database.Create(&models.Chat{
			ID: thread.LeadChatID, WakerID: wakerID, Title: thread.Title,
			WorkspaceDirectory: thread.WorkspaceDirectory, AdditionalWorkingDirectories: []string{},
		}).Error; err != nil {
			t.Fatalf("create chat %s: %v", thread.LeadChatID, err)
		}
	}
	if err := database.Create(&models.ChannelTask{
		ID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", ThreadID: threads[0].ID, AssigneeWakerID: wakerID,
		Subject: "等待验收", Description: "验证状态筛选", Status: constants.ChannelTaskStatusInReview, ChangedPaths: []string{},
	}).Error; err != nil {
		t.Fatalf("create review task: %v", err)
	}

	first, err := controller.ListByChannel(ctx, ChannelThreadListQuery{ChannelID: channelID, Limit: 1})
	if err != nil || len(first.Threads) != 1 || first.Threads[0].ID != threads[0].ID || first.NextCursor == nil {
		t.Fatalf("first cursor page = %+v, error = %v", first, err)
	}
	second, err := controller.ListByChannel(ctx, ChannelThreadListQuery{
		ChannelID: channelID, Cursor: first.NextCursor, Limit: 1,
	})
	if err != nil || len(second.Threads) != 1 || second.Threads[0].ID != threads[1].ID || second.NextCursor != nil {
		t.Fatalf("second cursor page = %+v, error = %v", second, err)
	}
	filtered, err := controller.ListByChannel(ctx, ChannelThreadListQuery{
		ChannelID: channelID, Search: "发布", Status: constants.ChannelActivityStateWaiting, Limit: 10,
	})
	if err != nil || len(filtered.Threads) != 1 || filtered.Threads[0].ActivityState != constants.ChannelActivityStateWaiting {
		t.Fatalf("filtered page = %+v, error = %v", filtered, err)
	}
	rangePage, err := controller.ListByChannelUpdatedRange(ctx, channelID, "", 150, 250, 1, 0)
	if err != nil || len(rangePage) != 1 || rangePage[0].ID != threads[0].ID {
		t.Fatalf("updated range page = %+v, error = %v", rangePage, err)
	}
	rangeTotal, err := controller.CountByChannelUpdatedRange(ctx, channelID, "", 150, 250)
	if err != nil || rangeTotal != 1 {
		t.Fatalf("updated range total = %d, error = %v", rangeTotal, err)
	}
}

func TestChannelThreadControllerFencesProvisioningAndListsInitializing(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-thread-provisioning.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})
	controller := NewChannelThreadController(database.DB)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	thread := models.ChannelThread{
		ID:                           util.New(),
		ChannelID:                    util.New(),
		Title:                        "准备中",
		LeadWakerID:                  util.New(),
		LeadChatID:                   util.New(),
		AdditionalWorkingDirectories: []string{},
		MaxParallel:                  3,
		MaxTasks:                     12,
		MaxFailureAttempts:           2,
		ProvisioningID:               util.New(),
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}
	members := []models.ChannelThreadMember{{
		ThreadID:        thread.ID,
		WakerID:         thread.LeadWakerID,
		DisplayName:     "负责人",
		ChatID:          thread.LeadChatID,
		IsLead:          true,
		RuntimeID:       constants.RuntimeIDCodex,
		PermissionLevel: constants.PermissionLevelAuto,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}
	if err := controller.Create(ctx, thread, members); err != nil {
		t.Fatalf("create initializing thread: %v", err)
	}
	initializing, err := controller.ListInitializing(ctx)
	if err != nil || len(initializing) != 1 || initializing[0].ID != thread.ID {
		t.Fatalf("ListInitializing() = %+v, error = %v", initializing, err)
	}
	thread.Title = "已经物化"
	thread.ProjectName = "项目"
	thread.WorkspaceDirectory = "/managed/team/leader"
	thread.AdditionalWorkingDirectories = []string{"/project"}
	thread.UpdatedAt = now + 1
	if err := controller.UpdateProvisioned(ctx, thread); err != nil {
		t.Fatalf("UpdateProvisioned() error = %v", err)
	}
	stored, err := controller.Get(ctx, thread.ID)
	if err != nil {
		t.Fatalf("Get() after UpdateProvisioned() error = %v", err)
	}
	if len(stored.AdditionalWorkingDirectories) != 1 ||
		stored.AdditionalWorkingDirectories[0] != "/project" {
		t.Fatalf(
			"Get() additional working directories = %v",
			stored.AdditionalWorkingDirectories,
		)
	}
	if err := controller.MarkReady(ctx, thread.ID, "wrong-provisioning", now+2); err == nil {
		t.Fatal("MarkReady() accepted a stale provisioning fence")
	}
	if err := controller.MarkReady(ctx, thread.ID, thread.ProvisioningID, now+2); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	initializing, err = controller.ListInitializing(ctx)
	if err != nil || len(initializing) != 0 {
		t.Fatalf("ready thread remained initializing: %+v, error = %v", initializing, err)
	}
}

func TestChannelThreadLookupByMemberChat(t *testing.T) {
	db := newTestDB(t)
	controller := NewChannelThreadController(db)
	ctx := context.Background()
	threadID, channelID, leadID, memberID := util.New(), util.New(), util.New(), util.New()
	leadChatID, memberChatID := util.New(), util.New()
	thread := models.ChannelThread{ID: threadID, ChannelID: channelID, LeadWakerID: leadID, LeadChatID: leadChatID}
	if err := controller.Create(ctx, thread, []models.ChannelThreadMember{{ThreadID: threadID, WakerID: leadID, ChatID: leadChatID, IsLead: true}, {ThreadID: threadID, WakerID: memberID, ChatID: memberChatID}}); err != nil {
		t.Fatal(err)
	}
	for _, chatID := range []string{leadChatID, memberChatID} {
		got, exists, err := controller.FindByChatID(ctx, chatID)
		if err != nil || !exists || got.ID != threadID || got.ChannelID != channelID {
			t.Fatalf("member lookup: %+v %t %v", got, exists, err)
		}
	}
	if _, exists, err := controller.FindByChatID(ctx, util.New()); err != nil || exists {
		t.Fatalf("ordinary Chat lookup: %t %v", exists, err)
	}
	if err := controller.Delete(ctx, threadID); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := controller.FindByChatID(ctx, memberChatID); err != nil || exists {
		t.Fatalf("deleted member lookup: %t %v", exists, err)
	}
}
