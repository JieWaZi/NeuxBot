package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestProjectAPICRUDPreservesDirectoryOrderAndLocalFiles(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "project-api.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	previousWakers := global.WakerControllerManager
	previousProjects := global.ProjectControllerManager
	previousSources := global.ProjectSourceControllerManager
	previousTransactions := global.TransactionControllerManager
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ProjectControllerManager = previousProjects
		global.ProjectSourceControllerManager = previousSources
		global.TransactionControllerManager = previousTransactions
	})

	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ProjectControllerManager = controllers.NewProjectController(database.DB)
	global.ProjectSourceControllerManager = controllers.NewProjectSourceController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)

	ctx, _ := gin.CreateTestContext(nil)
	waker := models.Waker{ID: "waker-project-test", Name: "项目测试伙伴"}
	if err := global.WakerControllerManager.Create(context.Background(), waker); err != nil {
		t.Fatalf("create waker: %v", err)
	}
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	resolvedFirstDirectory, err := filepath.EvalSymlinks(firstDirectory)
	if err != nil {
		t.Fatalf("resolve first directory: %v", err)
	}
	resolvedSecondDirectory, err := filepath.EvalSymlinks(secondDirectory)
	if err != nil {
		t.Fatalf("resolve second directory: %v", err)
	}

	create := CreateProject{
		WakerID: waker.ID,
		Body: ProjectInput{
			Name:  " NeuxBot ",
			Brief: " 多目录项目 ",
			Directories: []ProjectDirectoryInput{
				{Path: secondDirectory},
				{Path: firstDirectory},
			},
		},
	}
	if err := create.Validate(ctx); err != nil {
		t.Fatalf("validate create project: %v", err)
	}
	createdValue, err := create.Output(ctx)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	createdResponse, ok := createdValue.(CreateProjectResponse)
	if !ok {
		t.Fatalf("create response type = %T", createdValue)
	}
	createdProject, err := global.ProjectControllerManager.Get(ctx, createdResponse.ProjectID)
	if err != nil {
		t.Fatalf("get created project: %v", err)
	}
	createdDirectories, err := global.ProjectControllerManager.ListDirectories(ctx, createdResponse.ProjectID)
	if err != nil {
		t.Fatalf("list created project sources: %v", err)
	}
	created := projectResource(createdProject, createdDirectories)
	if created.Name != "NeuxBot" || created.Brief != "多目录项目" {
		t.Fatalf("created project = %+v", created)
	}
	if len(created.Sources) != 2 || created.Sources[0].Path != resolvedSecondDirectory ||
		created.Sources[1].Path != resolvedFirstDirectory {
		t.Fatalf("created sources = %+v", created.Sources)
	}
	if !created.Sources[0].IsDefault || !created.Sources[0].LoadInstructions ||
		created.Sources[1].IsDefault || !created.Sources[1].LoadInstructions {
		t.Fatalf("created directory defaults = %+v", created.Sources)
	}

	duplicate := CreateProject{
		WakerID: waker.ID,
		Body: ProjectInput{
			Name: created.Name,
			Directories: []ProjectDirectoryInput{{
				Path:      firstDirectory,
				IsDefault: true,
			}},
		},
	}
	if err := duplicate.Validate(ctx); err != nil {
		t.Fatalf("validate duplicate project: %v", err)
	}
	if _, err := duplicate.Output(ctx); !errors.Is(err, status_error.ProjectNameConflict) {
		t.Fatalf("duplicate project error = %v", err)
	}

	loadInstructions := false
	update := UpdateProject{
		WakerID:   waker.ID,
		ProjectID: created.ID,
		Body: ProjectInput{
			Name:  "NeuxBot 2",
			Brief: "更新后",
			Directories: []ProjectDirectoryInput{
				{Path: firstDirectory, LoadInstructions: &loadInstructions},
				{Path: secondDirectory, IsDefault: true},
			},
		},
	}
	if err := update.Validate(ctx); err != nil {
		t.Fatalf("validate update project: %v", err)
	}
	if _, err := update.Output(ctx); err != nil {
		t.Fatalf("update project: %v", err)
	}
	updatedProject, err := global.ProjectControllerManager.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get updated project: %v", err)
	}
	updatedDirectories, err := global.ProjectControllerManager.ListDirectories(ctx, created.ID)
	if err != nil {
		t.Fatalf("list updated project sources: %v", err)
	}
	updated := projectResource(updatedProject, updatedDirectories)
	if len(updated.Sources) != 2 || updated.Sources[0].Path != resolvedFirstDirectory ||
		updated.Sources[1].Path != resolvedSecondDirectory {
		t.Fatalf("updated project = %+v", updated)
	}
	if updated.Sources[0].LoadInstructions || !updated.Sources[1].LoadInstructions ||
		updated.Sources[0].IsDefault || !updated.Sources[1].IsDefault {
		t.Fatalf("updated directory settings = %+v", updated.Sources)
	}

	secondCreate := create
	secondCreate.Body.Name = "Second project"
	secondValue, err := secondCreate.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondID := secondValue.(CreateProjectResponse).ProjectID
	if _, err := (&DeleteProject{WakerID: "another-waker", ProjectID: secondID}).Output(ctx); err == nil {
		t.Fatal("project deletion accepted a different owner")
	}
	if _, err := global.ProjectControllerManager.Get(ctx, secondID); err != nil {
		t.Fatal("rejected deletion changed the project", err)
	}
	if _, err := (&DeleteProject{WakerID: waker.ID, ProjectID: secondID}).Output(ctx); err != nil {
		t.Fatal(err)
	}

	remove := DeleteProject{WakerID: waker.ID, ProjectID: created.ID}
	if _, err := remove.Output(ctx); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := os.Stat(firstDirectory); err != nil {
		t.Fatalf("first local directory changed after delete: %v", err)
	}
	if _, err := os.Stat(secondDirectory); err != nil {
		t.Fatalf("second local directory changed after delete: %v", err)
	}
	if _, err := global.ProjectControllerManager.Get(ctx, created.ID); err != nil {
		var commonErr statuserror.CommonError
		if !errors.As(err, &commonErr) ||
			commonErr.Code() != status_error.ProjectNotFound.Code() {
			t.Fatalf("get deleted project error = %v", err)
		}
	} else {
		t.Fatal("deleted project lookup succeeded")
	}
	var bindingCount int64
	if err := database.DB.Model(&models.ProjectSourceBinding{}).
		Where("project_id IN ?", []string{created.ID, secondID}).
		Count(&bindingCount).Error; err != nil {
		t.Fatalf("count deleted bindings: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("binding count after delete = %d, want 0", bindingCount)
	}
}

func TestProjectInputRejectsDuplicateDirectories(t *testing.T) {
	directory := t.TempDir()

	enabled := true
	_, err := resolveProjectDirectories(ProjectInput{
		Directories: []ProjectDirectoryInput{
			{Path: directory, LoadInstructions: &enabled, IsDefault: true},
			{Path: directory, LoadInstructions: &enabled},
		},
	})
	if !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("duplicate directory error = %v", err)
	}
}
