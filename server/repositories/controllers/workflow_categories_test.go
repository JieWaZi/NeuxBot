package controllers

import (
	"context"
	"slices"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkflowCategoriesFilterBeforeCursorAndPreserveRevision(t *testing.T) {
	db := newTestDB(t)
	store := NewWorkflowController(db)
	ctx := context.Background()
	for index, categories := range [][]string{{"设计", "开发"}, {"开发工具"}, {}} {
		id := []string{"matching", "different", "untagged"}[index]
		if err := store.CreateDefinition(ctx, models.WorkflowDefinition{ID: id, Title: "流程", CurrentRevisionID: "revision", Ordinal: 4, Categories: categories, UpdatedAt: int64(100 - index)}); err != nil {
			t.Fatal(err)
		}
	}
	category := "开发"
	items, err := store.ListDefinitions(ctx, "流程", 0, "", 1, &category)
	if err != nil || len(items) != 1 || items[0].ID != "matching" {
		t.Fatalf("category did not filter before pagination: %+v %v", items, err)
	}
	if err := store.UpdateCategories(ctx, "matching", []string{"研究"}); err != nil {
		t.Fatal(err)
	}
	definition, err := store.GetDefinition(ctx, "matching")
	if err != nil || !slices.Equal(definition.Categories, []string{"研究"}) || definition.CurrentRevisionID != "revision" || definition.Ordinal != 4 {
		t.Fatalf("category update changed version: %+v %v", definition, err)
	}
	if err := store.UpdateCategories(ctx, "matching", []string{}); err != nil {
		t.Fatal(err)
	}
	category = ""
	items, err = store.ListDefinitions(ctx, "", 0, "", 10, &category)
	if err != nil || len(items) != 2 {
		t.Fatalf("cleared and untagged workflows missing: %+v %v", items, err)
	}
	items, err = store.ListDefinitions(ctx, "", 99, "different", 10, nil)
	if err != nil || len(items) != 1 || items[0].ID != "untagged" {
		t.Fatalf("cursor order changed: %+v %v", items, err)
	}
}
