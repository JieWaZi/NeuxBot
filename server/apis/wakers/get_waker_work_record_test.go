package wakers

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestGetWakerWorkRecordReturnsAggregatedStats(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "waker-work-record-api.db"),
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

	previousWakers := global.WakerControllerManager
	previousChats := global.ChatControllerManager
	previousChatService := global.ChatService
	previousAutomaticTasks := global.AutomaticTaskControllerManager
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ChatControllerManager = previousChats
		global.ChatService = previousChatService
		global.AutomaticTaskControllerManager = previousAutomaticTasks
	})

	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ChatControllerManager = controllers.NewChatController(database.DB)
	global.AutomaticTaskControllerManager = controllers.NewAutomaticTaskController(database.DB)
	logDirectory := filepath.Join(t.TempDir(), constants.ChatLogDirectoryName)
	manager, err := chatservice.New(chatservice.Config{
		StorageDirectory: logDirectory,
		Projects:         controllers.NewProjectController(database.DB),
		QueryIndexes: chatservice.QueryIndexDependencies{
			Chats:           global.ChatControllerManager,
			RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions: controllers.NewTransactionController(
				dbhelper.NewGormTransactionManager(database.DB),
			),
		},
	})
	if err != nil {
		t.Fatalf("New Chat service error = %v", err)
	}
	global.ChatService = manager
	t.Cleanup(manager.Close)
	logStore, err := eventlog.NewStore(logDirectory)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx, _ := gin.CreateTestContext(nil)
	now := time.Now().UnixMilli()
	waker := models.Waker{
		ID:        util.New(),
		Name:      "统计伙伴",
		RoleName:  "工程师",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
		t.Fatalf("create waker: %v", err)
	}

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	today := time.Now().In(loc)
	_, _, daysInYear := currentYearDailyRange(loc, today)
	createdToday := time.Date(
		today.Year(),
		today.Month(),
		today.Day(),
		12,
		0,
		0,
		0,
		loc,
	).UnixMilli()

	for index, activeExecutionID := range []string{"exec-active", ""} {
		chatID := util.New()
		inputs := []eventlog.AppendInput{{
			Type: constants.ChatEventTypeCreated,
			Data: mustMarshalWakerWorkRecordEvent(t, eventlog.ChatCreatedData{
				WakerID:            waker.ID,
				WorkspaceDirectory: "/tmp/project",
				Title:              "对话",
			}),
		}}
		inputs = append(inputs, eventlog.AppendInput{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: util.New(),
			Data: mustMarshalWakerWorkRecordEvent(t, eventlog.MessageCreatedData{
				Role: constants.MessageRoleUser,
			}),
		})
		if index == 1 {
			inputs = append(inputs, eventlog.AppendInput{
				Type:        constants.ChatEventTypeExecutionCreated,
				ExecutionID: util.New(),
				Data:        mustMarshalWakerWorkRecordEvent(t, eventlog.ExecutionCreatedData{}),
			})
		}
		records, createErr := logStore.Create(chatID, inputs)
		if createErr != nil {
			t.Fatalf("Create Chat log error = %v", createErr)
		}
		chat := models.Chat{
			ID:                 chatID,
			WakerID:            waker.ID,
			WorkspaceDirectory: "/tmp/project",
			Title:              "对话",
			ActiveExecutionID:  activeExecutionID,
			CreatedAt:          createdToday + int64(index),
			LatestSequence:     records[len(records)-1].Sequence,
			UpdatedAt:          createdToday + int64(index),
		}
		if err := global.ChatControllerManager.Create(ctx, chat); err != nil {
			t.Fatalf("create chat: %v", err)
		}
	}

	output, err := (&GetWakerWorkRecord{
		WakerID:  waker.ID,
		Timezone: "Asia/Shanghai",
	}).Output(ctx)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	response, ok := output.(WakerWorkRecordResponse)
	if !ok {
		t.Fatalf("Output() = %T, want WakerWorkRecordResponse", output)
	}
	if response.TotalChats != 2 {
		t.Fatalf("TotalChats = %d, want 2", response.TotalChats)
	}
	if response.ActiveChats != 1 {
		t.Fatalf("ActiveChats = %d, want 1", response.ActiveChats)
	}
	if response.TotalAutomaticTasks != 0 {
		t.Fatalf("TotalAutomaticTasks = %d, want 0", response.TotalAutomaticTasks)
	}
	if len(response.DailyChats) != daysInYear {
		t.Fatalf(
			"DailyChats length = %d, want %d",
			len(response.DailyChats),
			daysInYear,
		)
	}

	firstDay := response.DailyChats[0]
	if firstDay.Date != time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, loc).Format("2006-01-02") {
		t.Fatalf("first day = %q, want Jan 1", firstDay.Date)
	}

	lastDay := response.DailyChats[len(response.DailyChats)-1]
	if lastDay.Date != time.Date(today.Year(), time.December, 31, 0, 0, 0, 0, loc).Format("2006-01-02") {
		t.Fatalf("last day = %q, want Dec 31", lastDay.Date)
	}

	todayKey := time.Date(
		today.Year(),
		today.Month(),
		today.Day(),
		0,
		0,
		0,
		0,
		loc,
	).Format("2006-01-02")
	var todayCount int64
	for _, entry := range response.DailyChats {
		if entry.Date == todayKey {
			todayCount = entry.Count
			break
		}
	}
	if todayCount != 3 {
		t.Fatalf("today count = %d, want 3", todayCount)
	}
	if !response.ActiveToday {
		t.Fatal("ActiveToday = false, want true")
	}
}

func mustMarshalWakerWorkRecordEvent(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := eventlog.MarshalData(value)
	if err != nil {
		t.Fatalf("MarshalData() error = %v", err)
	}
	return data
}
