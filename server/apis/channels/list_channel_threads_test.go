package channels

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestChannelThreadCursorRoundTrip(t *testing.T) {
	want := &controllers.ChannelThreadListCursor{
		UpdatedAt: 1_725_000_000_000,
		ID:        "11111111111111111111111111111111",
	}
	encoded := encodeChannelThreadCursor(want)
	got, err := decodeChannelThreadCursor(encoded)
	if err != nil || got == nil || got.UpdatedAt != want.UpdatedAt || got.ID != want.ID {
		t.Fatalf("cursor round trip = %+v, error = %v", got, err)
	}
	if _, err := decodeChannelThreadCursor("not-base64!"); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}

func TestListChannelThreadsValidatesCursorAndNumberedPagination(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	validCursor := ListChannelThreads{
		PageSize: 30,
		Status:   constants.ChannelActivityStateWaiting,
	}
	if err := validCursor.Validate(ctx); err != nil {
		t.Fatalf("valid cursor filters rejected: %v", err)
	}
	validPage := ListChannelThreads{
		PageNum:        2,
		PageSize:       10,
		UpdatedAtStart: 100,
		UpdatedAtEnd:   200,
	}
	if err := validPage.Validate(ctx); err != nil {
		t.Fatalf("valid numbered page rejected: %v", err)
	}
	invalidStatus := ListChannelThreads{Status: constants.ChannelActivityState("unknown")}
	if err := invalidStatus.Validate(ctx); err == nil {
		t.Fatal("invalid status was accepted")
	}
	invalidCursor := ListChannelThreads{Cursor: "not-base64!"}
	if err := invalidCursor.Validate(ctx); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
	invalidRange := ListChannelThreads{PageNum: 1, UpdatedAtStart: 200, UpdatedAtEnd: 100}
	if err := invalidRange.Validate(ctx); err == nil {
		t.Fatal("reversed activity range was accepted")
	}
	mixedPagination := ListChannelThreads{PageNum: 1, Cursor: encodeChannelThreadCursor(&controllers.ChannelThreadListCursor{
		UpdatedAt: 100,
		ID:        "11111111111111111111111111111111",
	})}
	if err := mixedPagination.Validate(ctx); err == nil {
		t.Fatal("mixed cursor and numbered pagination was accepted")
	}
	cursorRange := ListChannelThreads{UpdatedAtStart: 100}
	if err := cursorRange.Validate(ctx); err == nil {
		t.Fatal("cursor pagination accepted a numbered-page time range")
	}
}

func TestListChannelThreadsOutputPagesUpdatedRange(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "list-channel-threads.db"),
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
		_ = sqlDB.Close()
	})

	previousChannels := global.ChannelControllerManager
	previousThreads := global.ChannelThreadControllerManager
	t.Cleanup(func() {
		global.ChannelControllerManager = previousChannels
		global.ChannelThreadControllerManager = previousThreads
	})
	global.ChannelControllerManager = controllers.NewChannelController(database.DB)
	global.ChannelThreadControllerManager = controllers.NewChannelThreadController(database.DB)

	ctx, _ := gin.CreateTestContext(nil)
	channelID := "11111111111111111111111111111111"
	wakerID := "22222222222222222222222222222222"
	readyAt := int64(1)
	if err := database.DB.Create(&models.Channel{
		ID:          channelID,
		Name:        "分页测试团队",
		LeadWakerID: wakerID,
	}).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	for _, thread := range []models.ChannelThread{
		{
			ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ChannelID: channelID, Title: "较新任务", LeadWakerID: wakerID,
			LeadChatID: "cccccccccccccccccccccccccccccccc", AdditionalWorkingDirectories: []string{},
			MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt, CreatedAt: 100, UpdatedAt: 300,
		},
		{
			ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ChannelID: channelID, Title: "较早任务", LeadWakerID: wakerID,
			LeadChatID: "dddddddddddddddddddddddddddddddd", AdditionalWorkingDirectories: []string{},
			MaxParallel: 3, MaxTasks: 12, MaxFailureAttempts: 2, ReadyAt: &readyAt, CreatedAt: 100, UpdatedAt: 200,
		},
	} {
		if err := global.ChannelThreadControllerManager.Create(context.Background(), thread, []models.ChannelThreadMember{{
			ThreadID: thread.ID, WakerID: wakerID, ChatID: thread.LeadChatID, IsLead: true,
			RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto,
		}}); err != nil {
			t.Fatalf("create thread %s: %v", thread.ID, err)
		}
	}

	output, err := (&ListChannelThreads{
		ChannelID:      channelID,
		Query:          "任务",
		UpdatedAtStart: 150,
		UpdatedAtEnd:   350,
		PageNum:        2,
		PageSize:       1,
	}).Output(ctx)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	response, ok := output.(ListChannelThreadsResponse)
	if !ok {
		t.Fatalf("Output() type = %T, want ListChannelThreadsResponse", output)
	}
	if response.TotalSize != 2 || response.PageNum != 2 || response.PageSize != 1 {
		t.Fatalf("pagination response = %+v, want total 2 on page 2", response)
	}
	if len(response.Resources) != 1 || response.Resources[0].ID != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("resources = %+v, want older channel thread", response.Resources)
	}
}
