package wakers

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestListWakerChatsValidateDefaults(t *testing.T) {
	request := ListWakerChats{}
	if err := request.Validate(&gin.Context{}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if request.PageNum != 1 || request.PageSize != 50 {
		t.Fatalf(
			"pagination = (%d, %d), want (1, 50)",
			request.PageNum,
			request.PageSize,
		)
	}
}

func TestListWakerChatsValidateRejectsInvalidRangeAndPagination(t *testing.T) {
	tests := []struct {
		// name 是失败场景名称。
		name string
		// request 是需要校验的请求参数。
		request ListWakerChats
	}{
		{name: "reversed range", request: ListWakerChats{UpdatedAtStart: 200, UpdatedAtEnd: 100}},
		{name: "negative start", request: ListWakerChats{UpdatedAtStart: -1}},
		{name: "negative page", request: ListWakerChats{PageNum: -1}},
		{name: "oversized page", request: ListWakerChats{PageSize: 101}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.request.Validate(&gin.Context{}); err == nil {
				t.Fatal("Validate() error = nil, want bad request")
			}
		})
	}
}

func TestListWakerChatsOutputPagesFilteredResults(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "list-waker-chats.db"),
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
	wakerID := "11111111111111111111111111111111"
	if err := global.WakerControllerManager.Create(context.Background(), models.Waker{
		ID:   wakerID,
		Name: "分页测试伙伴",
	}); err != nil {
		t.Fatalf("create waker: %v", err)
	}
	for _, chat := range []models.Chat{
		{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WakerID: wakerID, Title: "较早任务", UpdatedAt: 100},
		{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", WakerID: wakerID, Title: "页面第二项", UpdatedAt: 200},
		{ID: "cccccccccccccccccccccccccccccccc", WakerID: wakerID, Title: "页面第一项", UpdatedAt: 300},
		{ID: "dddddddddddddddddddddddddddddddd", WakerID: wakerID, Title: "无关任务", UpdatedAt: 250},
	} {
		if err := global.ChatControllerManager.Create(ctx, chat); err != nil {
			t.Fatalf("create Chat %s: %v", chat.ID, err)
		}
	}

	output, err := (&ListWakerChats{
		WakerID:        wakerID,
		Query:          " 页面 ",
		UpdatedAtStart: 150,
		UpdatedAtEnd:   350,
		PageNum:        2,
		PageSize:       1,
	}).Output(ctx)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	response, ok := output.(ListWakerChatsResponse)
	if !ok {
		t.Fatalf("Output() type = %T, want ListWakerChatsResponse", output)
	}
	if response.TotalSize != 2 || response.PageNum != 2 || response.PageSize != 1 {
		t.Fatalf("pagination response = %+v, want total 2 on page 2", response)
	}
	if len(response.Resources) != 1 || response.Resources[0].ID != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("resources = %+v, want Chat b", response.Resources)
	}
}
