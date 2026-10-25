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
	previousProjects := global.ProjectControllerManager
	previousMemory := global.MemoryService
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ProjectControllerManager = previousProjects
		global.MemoryService = previousMemory
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ProjectControllerManager = controllers.NewProjectController(database.DB)
	global.MemoryService = service

	ctx, _ := gin.CreateTestContext(nil)
	waker := models.Waker{ID: "waker-memory-api", Name: "记忆测试伙伴"}
	if err := global.WakerControllerManager.Create(context.Background(), waker); err != nil {
		t.Fatalf("create waker: %v", err)
	}

	_, err = (&CreateMemory{WakerID: waker.ID, Body: MemoryMutationRequest{Scope: constants.MemoryScopePartner, Content: "用户偏好使用中文"}}).Output(ctx)
	if err != nil {
		t.Fatalf("create memory: %v", err)
	}
	listedValue, err := (&ListMemories{WakerID: waker.ID, Scope: constants.MemoryScopePartner, PageNum: 1, PageSize: 20}).Output(ctx)
	if err != nil {
		t.Fatalf("list created memory: %v", err)
	}
	createdPage := listedValue.(ListMemoriesResponse)
	if createdPage.TotalSize != 1 || len(createdPage.Resources) != 1 || createdPage.Resources[0].Content != "用户偏好使用中文" {
		t.Fatalf("unexpected created memory page: %+v", createdPage)
	}
	created := createdPage.Resources[0]

	if _, err := (&UpdateMemory{WakerID: waker.ID, MemoryID: created.ID, Body: MemoryMutationRequest{Scope: constants.MemoryScopePartner, Content: "用户偏好使用简体中文"}}).Output(ctx); err != nil {
		t.Fatalf("update memory: %v", err)
	}
	listedValue, err = (&ListMemories{WakerID: waker.ID, Scope: constants.MemoryScopePartner, PageNum: 1, PageSize: 20}).Output(ctx)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	listed := listedValue.(ListMemoriesResponse)
	if listed.TotalSize != 1 || len(listed.Resources) != 1 || listed.Resources[0].Content != "用户偏好使用简体中文" {
		t.Fatalf("unexpected memory page: %+v", listed)
	}

	if _, err := (&DeleteMemory{WakerID: waker.ID, MemoryID: created.ID, Scope: constants.MemoryScopePartner}).Output(ctx); err != nil {
		t.Fatalf("delete memory: %v", err)
	}
	timelineValue, err := (&ListTimeline{WakerID: waker.ID, Scope: constants.MemoryScopePartner, PageNum: 1, PageSize: 20}).Output(ctx)
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

func TestMemoryAPIListsPartnerAndProjectMemoriesTogether(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "memory-all-api.db"), Migrate: true})
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
	previousProjects := global.ProjectControllerManager
	previousMemory := global.MemoryService
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ProjectControllerManager = previousProjects
		global.MemoryService = previousMemory
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ProjectControllerManager = controllers.NewProjectController(database.DB)
	global.MemoryService = service

	ctx, _ := gin.CreateTestContext(nil)
	waker := models.Waker{ID: "waker-all-scope", Name: "合并范围伙伴"}
	if err := global.WakerControllerManager.Create(context.Background(), waker); err != nil {
		t.Fatalf("create waker: %v", err)
	}
	otherWaker := models.Waker{ID: "waker-all-scope-other", Name: "其他伙伴"}
	if err := global.WakerControllerManager.Create(context.Background(), otherWaker); err != nil {
		t.Fatalf("create other waker: %v", err)
	}
	project := models.Project{ID: "project-all-scope", WakerID: waker.ID, Name: "合并范围项目"}
	if err := global.ProjectControllerManager.Create(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	otherProject := models.Project{ID: "project-all-scope-other", WakerID: otherWaker.ID, Name: "其他伙伴项目"}
	if err := global.ProjectControllerManager.Create(context.Background(), otherProject); err != nil {
		t.Fatalf("create other project: %v", err)
	}

	mutations := []MemoryMutationRequest{
		{Scope: constants.MemoryScopePartner, Content: "个人记忆内容"},
		{Scope: constants.MemoryScopeProject, ProjectID: project.ID, Content: "项目记忆内容"},
	}
	for _, mutation := range mutations {
		if _, err := (&CreateMemory{WakerID: waker.ID, Body: mutation}).Output(ctx); err != nil {
			t.Fatalf("create memory: %v", err)
		}
	}
	if _, err := (&CreateMemory{WakerID: otherWaker.ID, Body: MemoryMutationRequest{Scope: constants.MemoryScopeProject, ProjectID: otherProject.ID, Content: "其他伙伴的项目记忆"}}).Output(ctx); err != nil {
		t.Fatalf("create other waker memory: %v", err)
	}

	listedValue, err := (&ListMemories{WakerID: waker.ID, Scope: constants.MemoryScopeAll, PageNum: 1, PageSize: 20}).Output(ctx)
	if err != nil {
		t.Fatalf("list merged memories: %v", err)
	}
	merged := listedValue.(ListMemoriesResponse)
	if merged.TotalSize != 2 || len(merged.Resources) != 2 {
		t.Fatalf("merged page must only contain memories owned by the waker: %+v", merged)
	}
	scopes := map[constants.MemoryScope]string{}
	for _, resource := range merged.Resources {
		scopes[resource.Scope] = resource.Content
	}
	if scopes[constants.MemoryScopePartner] != "个人记忆内容" || scopes[constants.MemoryScopeProject] != "项目记忆内容" {
		t.Fatalf("merged page must expose both scopes: %+v", merged.Resources)
	}

	if _, err := (&ListMemories{WakerID: waker.ID, Scope: constants.MemoryScopeAll, ProjectID: project.ID, PageNum: 1, PageSize: 20}).Output(ctx); err == nil {
		t.Fatalf("merged scope must reject an explicit project filter")
	}
}

func TestMemoryAPIRejectsProjectOwnedByAnotherWaker(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "memory-scope-api.db"), Migrate: true})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, _ := database.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	previousWakers := global.WakerControllerManager
	previousProjects := global.ProjectControllerManager
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ProjectControllerManager = previousProjects
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ProjectControllerManager = controllers.NewProjectController(database.DB)
	ctx := context.Background()
	for _, waker := range []models.Waker{{ID: "waker-a", Name: "伙伴甲"}, {ID: "waker-b", Name: "伙伴乙"}} {
		if err := global.WakerControllerManager.Create(ctx, waker); err != nil {
			t.Fatalf("create waker: %v", err)
		}
	}
	project := models.Project{ID: "project-a", WakerID: "waker-a", Name: "甲的项目"}
	if err := global.ProjectControllerManager.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	ginContext, _ := gin.CreateTestContext(nil)
	if _, err := visibleScope(ginContext, "waker-b", constants.MemoryScopeProject, project.ID); err == nil {
		t.Fatalf("project scope owned by another waker must be rejected")
	}
}
