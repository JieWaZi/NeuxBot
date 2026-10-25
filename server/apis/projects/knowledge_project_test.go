package projects

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestProjectSaveDoesNotCreateSoftwareKnowledge(t *testing.T) {
	ctx := context.Background()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "main.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	defer sqlDB.Close()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	oldWakers, oldProjects, oldSources, oldTransactions, oldKnowledge := global.WakerControllerManager, global.ProjectControllerManager, global.ProjectSourceControllerManager, global.TransactionControllerManager, global.KnowledgeService
	defer func() {
		global.WakerControllerManager = oldWakers
		global.ProjectControllerManager = oldProjects
		global.ProjectSourceControllerManager = oldSources
		global.TransactionControllerManager = oldTransactions
		global.KnowledgeService = oldKnowledge
	}()
	global.WakerControllerManager = controllers.NewWakerController(db.DB)
	global.ProjectControllerManager = controllers.NewProjectController(db.DB)
	global.ProjectSourceControllerManager = controllers.NewProjectSourceController(db.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	refs := controllers.NewKnowledgeController(db.DB)
	global.KnowledgeService = knowledgeservice.New(knowledgeservice.Config{Engine: engine, References: refs, Projects: global.ProjectControllerManager})
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "waker", Name: "Fixture"}); err != nil {
		t.Fatal(err)
	}
	mainDirectory, auxiliaryDirectory := t.TempDir(), t.TempDir()
	ginContext, _ := gin.CreateTestContext(nil)
	create := CreateProject{WakerID: "waker", Body: ProjectInput{Name: "Project", Directories: []ProjectDirectoryInput{{Path: mainDirectory, IsDefault: true}, {Path: auxiliaryDirectory}}}}
	if err := create.Validate(ginContext); err != nil {
		t.Fatal(err)
	}
	value, err := create.Output(ginContext)
	if err != nil {
		t.Fatalf("project creation unexpectedly invoked software knowledge: %v", err)
	}
	projectID := value.(CreateProjectResponse).ProjectID
	update := UpdateProject{WakerID: "waker", ProjectID: projectID, Body: create.Body}
	if err := update.Validate(ginContext); err != nil {
		t.Fatal(err)
	}
	if _, err := update.Output(ginContext); err != nil {
		t.Fatalf("project update unexpectedly invoked software knowledge: %v", err)
	}
	create.Body.Name = "Other project"
	if _, err := create.Output(ginContext); err != nil {
		t.Fatal(err)
	}
	if _, total, err := engine.ListLibraries(ctx, 0, 100); err != nil || total != 0 {
		t.Fatalf("project save created software knowledge total=%d err=%v", total, err)
	}
	if total, err := global.ProjectControllerManager.CountByWaker(ctx, "waker", ""); err != nil || total != 2 {
		t.Fatalf("project persistence changed total=%d err=%v", total, err)
	}
}
