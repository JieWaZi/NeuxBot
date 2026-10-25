package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestProjectControllerContract(t *testing.T) {
	db := newTestDB(t)
	controller := NewProjectController(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	project := models.Project{
		ID:        "11111111111111111111111111111111",
		WakerID:   "waker-1",
		Name:      "NeuxBot",
		Brief:     "多目录项目",
		CreatedAt: now,
		UpdatedAt: now,
	}
	sources := []models.ProjectSource{
		{ID: "source-1", Name: "desktop", Path: "/tmp/ally-desktop", CreatedAt: now, UpdatedAt: now},
		{ID: "source-2", Name: "server", Path: "/tmp/ally-server", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&sources).Error; err != nil {
		t.Fatalf("create project sources: %v", err)
	}
	if err := controller.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	directories := []models.ProjectDirectory{
		{Source: sources[1], LoadInstructions: false, IsDefault: true},
		{Source: sources[0], LoadInstructions: true},
	}
	if err := controller.ReplaceDirectories(ctx, project.ID, directories); err != nil {
		t.Fatalf("replace project directories: %v", err)
	}

	listed, err := controller.ListByWaker(ctx, project.WakerID, "", 50, 0)
	if err != nil || len(listed) != 1 || listed[0].ID != project.ID {
		t.Fatalf("list projects = %+v, error = %v", listed, err)
	}
	count, err := controller.CountByWaker(ctx, project.WakerID, "")
	if err != nil || count != 1 {
		t.Fatalf("count projects = %d, error = %v", count, err)
	}
	listedDirectories, err := controller.ListDirectories(ctx, project.ID)
	if err != nil || len(listedDirectories) != 2 ||
		listedDirectories[0].Source.ID != "source-2" ||
		listedDirectories[1].Source.ID != "source-1" ||
		!listedDirectories[0].IsDefault || listedDirectories[0].LoadInstructions ||
		listedDirectories[1].IsDefault || !listedDirectories[1].LoadInstructions {
		t.Fatalf("list project directories = %+v, error = %v", listedDirectories, err)
	}
	if err := controller.ReplaceDirectories(ctx, project.ID, nil); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("replace project directories with empty input error = %v", err)
	}
	listedDirectories, err = controller.ListDirectories(ctx, project.ID)
	if err != nil || len(listedDirectories) != 2 ||
		listedDirectories[0].Source.ID != "source-2" || listedDirectories[1].Source.ID != "source-1" {
		t.Fatalf("project directories after rejected replacement = %+v, error = %v", listedDirectories, err)
	}
	if err := controller.ReplaceDirectories(ctx, project.ID, []models.ProjectDirectory{
		{Source: sources[0], IsDefault: true},
		{Source: sources[1], IsDefault: true},
	}); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("replace project directories with two defaults error = %v", err)
	}

	project.Name = "NeuxBot 更新"
	project.Brief = "更新后的简介"
	project.UpdatedAt = now + 1
	if err := controller.Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}
	updated, err := controller.Get(ctx, project.ID)
	if err != nil || updated.Name != project.Name || updated.Brief != project.Brief {
		t.Fatalf("updated project = %+v, error = %v", updated, err)
	}

	duplicate := project
	duplicate.ID = "22222222222222222222222222222222"
	if err := controller.Create(ctx, duplicate); !errors.Is(err, status_error.ProjectNameConflict) {
		t.Fatalf("duplicate project error = %v", err)
	}

	if err := controller.Delete(ctx, project.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := controller.Get(ctx, project.ID); err != nil {
		var commonErr statuserror.CommonError
		if !errors.As(err, &commonErr) ||
			commonErr.Code() != status_error.ProjectNotFound.Code() {
			t.Fatalf("deleted project error = %v", err)
		}
	} else {
		t.Fatal("deleted project lookup succeeded")
	}
	var bindingCount int64
	if err := db.Model(&models.ProjectSourceBinding{}).
		Where("project_id = ?", project.ID).
		Count(&bindingCount).Error; err != nil {
		t.Fatalf("count project bindings: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("project binding count = %d, want 0", bindingCount)
	}
}

func TestProjectSearchMatchesBeforePaginationAndStaysWithinWaker(t *testing.T) {
	controller := NewProjectController(newTestDB(t))
	ctx := context.Background()
	for _, project := range []models.Project{
		{ID: "one", WakerID: "a", Name: "NeuxBot", Brief: "目录工具", UpdatedAt: 3},
		{ID: "two", WakerID: "a", Name: "Other", Brief: "NeuxBot integration", UpdatedAt: 2},
		{ID: "three", WakerID: "b", Name: "NeuxBot", UpdatedAt: 1},
	} {
		if err := controller.Create(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	items, err := controller.ListByWaker(ctx, "a", " ALLY ", 1, 1)
	if err != nil || len(items) != 1 || items[0].ID != "two" {
		t.Fatalf("search page: %v, %v", items, err)
	}
	count, err := controller.CountByWaker(ctx, "a", " ALLY ")
	if err != nil || count != 2 {
		t.Fatalf("search count: %d, %v", count, err)
	}
	for search, expected := range map[string]int64{"目录": 1, "%": 0, "missing": 0} {
		count, err := controller.CountByWaker(ctx, "a", search)
		if err != nil || count != expected {
			t.Fatalf("search %q: %d, %v", search, count, err)
		}
	}
}
