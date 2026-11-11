package controllers

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"path/filepath"
	"testing"
)

func TestResourceCategoriesPersistAndRemainIsolatedByType(t *testing.T) {
	configuration := conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "categories.db"), Migrate: true}
	database, err := dbhelper.NewDB(configuration)
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := database.DB.DB()
	ctx := context.Background()
	store := NewResourceCategoryController(database.DB)
	for _, kind := range []constants.ResourceCategoryType{constants.ResourceCategoryTypeSkill, constants.ResourceCategoryTypePlugin, constants.ResourceCategoryTypeConnector, constants.ResourceCategoryTypeWorkflow} {
		if err := store.Ensure(ctx, kind, []string{"开发", "开发", "写作"}); err != nil {
			t.Fatal(err)
		}
		if err := store.Ensure(ctx, kind, []string{"开发"}); err != nil {
			t.Fatal(err)
		}
		rows, err := store.List(ctx, kind)
		if err != nil || len(rows) != 2 {
			t.Fatalf("type=%s rows=%+v error=%v", kind, rows, err)
		}
		for _, row := range rows {
			if row.Type != kind || len(row.ID) != 32 {
				t.Fatalf("invalid category: %+v", row)
			}
		}
	}
	if err := store.Ensure(ctx, constants.ResourceCategoryTypeSkill, []string{"技能专用"}); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	database, err = dbhelper.NewDB(configuration)
	if err != nil {
		t.Fatal(err)
	}
	connection, _ = database.DB.DB()
	defer connection.Close()
	for _, kind := range []constants.ResourceCategoryType{constants.ResourceCategoryTypeSkill, constants.ResourceCategoryTypePlugin} {
		rows, err := NewResourceCategoryController(database.DB).List(ctx, kind)
		expected := 2
		if kind == constants.ResourceCategoryTypeSkill {
			expected = 3
		}
		if err != nil || len(rows) != expected {
			t.Fatalf("reopened type=%s rows=%+v error=%v", kind, rows, err)
		}
	}
}
