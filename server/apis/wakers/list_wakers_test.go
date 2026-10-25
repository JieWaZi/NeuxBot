package wakers

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestListWakersReturnsAggregatedActivityState(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "list-wakers.db"),
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
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ChatControllerManager = previousChats
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ChatControllerManager = controllers.NewChatController(database.DB)

	ctx, _ := gin.CreateTestContext(nil)
	runningWakerID := "11111111111111111111111111111111"
	idleWakerID := builtinrolepreset.PersonalAssistantWakerID
	for _, waker := range []models.Waker{
		{ID: runningWakerID, Name: "运行伙伴"},
		{ID: idleWakerID, Name: "空闲伙伴"},
	} {
		if err := global.WakerControllerManager.Create(context.Background(), waker); err != nil {
			t.Fatalf("create Waker %s: %v", waker.ID, err)
		}
	}
	if err := global.ChatControllerManager.Create(ctx, models.Chat{
		ID:                "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WakerID:           runningWakerID,
		ActiveExecutionID: "execution-running",
	}); err != nil {
		t.Fatalf("create Chat: %v", err)
	}

	if err := global.ChatControllerManager.Create(ctx, models.Chat{
		ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", WakerID: idleWakerID,
		ActiveExecutionID: "background-workflow-execution", HiddenFromWakerHistory: true,
		WaitingApproval: true,
	}); err != nil {
		t.Fatalf("create background Chat: %v", err)
	}

	output, err := (&ListWakers{PageNum: 1, PageSize: 100}).Output(ctx)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	response, ok := output.(ListWakersResponse)
	if !ok {
		t.Fatalf("Output() type = %T, want ListWakersResponse", output)
	}
	byID := make(map[string]models.Waker, len(response.Resources))
	for _, waker := range response.Resources {
		byID[waker.ID] = waker
	}
	if byID[runningWakerID].ActivityState != constants.WakerActivityStateRunning {
		t.Fatalf("running state = %q, want running", byID[runningWakerID].ActivityState)
	}
	if byID[runningWakerID].RunningCount != 1 {
		t.Fatalf("running count = %d, want 1", byID[runningWakerID].RunningCount)
	}
	if byID[idleWakerID].ActivityState != constants.WakerActivityStateIdle {
		t.Fatalf("idle state = %q, want idle", byID[idleWakerID].ActivityState)
	}
	if !byID[idleWakerID].IsDefault {
		t.Fatal("built-in default waker was not marked as default")
	}
}
