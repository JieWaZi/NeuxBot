package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalmemory "github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestMemoryAPICRUDAndTimeline(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "memory-api.db"), Migrate: true})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	service, err := internalmemory.NewService(internalmemory.ServiceConfig{DBPath: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatalf("create memory service: %v", err)
	}
	t.Cleanup(service.Close)

	previousWakers := global.WakerControllerManager
	previousMemory := global.MemoryService
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.MemoryService = previousMemory
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.MemoryService = service

	ctx, _ := gin.CreateTestContext(nil)
	waker := models.Waker{ID: "waker-memory-api", Name: "记忆测试伙伴"}
	if err := global.WakerControllerManager.Create(context.Background(), waker); err != nil {
		t.Fatalf("create waker: %v", err)
	}

	_, err = (&CreateMemory{WakerID: waker.ID, Body: MemoryMutationRequest{Content: "用户偏好使用中文"}}).Output(ctx)
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}
	listedValue, err := (&ListMemories{WakerID: waker.ID, PageNum: 1, PageSize: 20}).Output(ctx)
	if err != nil {
		t.Fatalf("list created memory: %v", err)
	}
	createdPage := listedValue.(ListMemoriesResponse)
	if createdPage.TotalSize != 1 || len(createdPage.Resources) != 1 || createdPage.Resources[0].Content != "用户偏好使用中文" {
		t.Fatalf("unexpected created memory page: %+v", createdPage)
	}
	created := createdPage.Resources[0]

	if _, err := (&UpdateMemory{WakerID: waker.ID, MemoryID: created.ID, Body: MemoryMutationRequest{Content: "用户偏好使用简体中文"}}).Output(ctx); err != nil {
		t.Fatalf("update memory: %v", err)
	}
	listedValue, err = (&ListMemories{WakerID: waker.ID, PageNum: 1, PageSize: 20}).Output(ctx)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	listed := listedValue.(ListMemoriesResponse)
	if listed.TotalSize != 1 || len(listed.Resources) != 1 || listed.Resources[0].Content != "用户偏好使用简体中文" {
		t.Fatalf("unexpected memory page: %+v", listed)
	}

	if _, err := (&DeleteMemory{WakerID: waker.ID, MemoryID: created.ID}).Output(ctx); err != nil {
		t.Fatalf("delete memory: %v", err)
	}
	timelineValue, err := (&ListTimeline{WakerID: waker.ID, PageNum: 1, PageSize: 20}).Output(ctx)
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	timeline := timelineValue.(ListTimelineResponse)
	if timeline.TotalSize != 3 || len(timeline.Resources) != 3 {
		t.Fatalf("timeline must include add, update and delete: %+v", timeline)
	}
	if timeline.Resources[0].Event != constants.MemoryEventDelete || timeline.Resources[0].OldContent != "用户偏好使用简体中文" {
		t.Fatalf("unexpected latest event: %+v", timeline.Resources[0])
	}
}

func TestMemoryAPIListsCoreBeforePagination(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	service, err := internalmemory.NewService(internalmemory.ServiceConfig{DBPath: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	previousWakers, previousMemory := global.WakerControllerManager, global.MemoryService
	t.Cleanup(func() { global.WakerControllerManager, global.MemoryService = previousWakers, previousMemory })
	global.WakerControllerManager, global.MemoryService = controllers.NewWakerController(database.DB), service
	for _, owner := range []string{"first", "second"} {
		if err := global.WakerControllerManager.Create(t.Context(), models.Waker{ID: owner, Name: owner}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, _ := gin.CreateTestContext(nil)
	first, err := service.Write(t.Context(), internalmemory.Context{PartnerID: "first"}, "workspace query one")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ owner, text string }{{"first", "workspace query regular"}, {"second", "workspace query other partner"}} {
		if _, err := service.Write(t.Context(), internalmemory.Context{PartnerID: item.owner}, item.text); err != nil {
			t.Fatal(err)
		}
	}
	core := true
	classify := SetMemoryCore{WakerID: "second", MemoryID: first.Memory.ID, Body: SetMemoryCoreRequest{Core: &core}}
	if _, err := classify.Output(ctx); err == nil {
		t.Fatal("accepted classifying another partner's memory")
	}
	classify.WakerID = "first"
	if _, err := classify.Output(ctx); err != nil {
		t.Fatal(err)
	}
	filtered := ListMemories{WakerID: "first", Core: &core, Query: "workspace query", PageNum: 1, PageSize: 1}
	out, err := filtered.Output(ctx)
	page, ok := out.(ListMemoriesResponse)
	if err != nil || !ok || page.TotalSize != 1 || len(page.Resources) != 1 || page.Resources[0].ID != first.Memory.ID || !page.Resources[0].Core {
		t.Fatalf("filtered core page: %+v, %v", out, err)
	}
	core = false
	if _, err := classify.Output(ctx); err != nil {
		t.Fatalf("explicit false must be accepted: %v", err)
	}
	out, err = filtered.Output(ctx)
	page, ok = out.(ListMemoriesResponse)
	if err != nil || !ok || page.TotalSize != 2 || len(page.Resources) != 1 || page.Resources[0].Core {
		t.Fatalf("filtered regular page: %+v, %v", out, err)
	}
	classify.Body.Core = nil
	if _, err := classify.Output(ctx); err == nil {
		t.Fatal("missing core field accepted")
	}
}
