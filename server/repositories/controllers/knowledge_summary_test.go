package controllers

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"
)

func TestKnowledgeSummaryPersistsAndIsolatesConnectionRevision(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeController(db)
	ctx := context.Background()
	value := models.KnowledgeLibrarySummary{Backend: "custom", ConnectionID: "remote", ConnectionRevision: 1, KnowledgeBaseID: "same", PageTotal: 7, ObservedAt: 20}
	if err := c.RecordLibrarySummary(ctx, value); err != nil {
		t.Fatal(err)
	}
	value.PageTotal = 2
	value.ObservedAt = 10
	if err := c.RecordLibrarySummary(ctx, value); err != nil {
		t.Fatal(err)
	}
	// 重新构造Repository仍可读取，较晚到达的旧响应不能覆盖新值。
	c = NewKnowledgeController(db)
	rows, err := c.LibrarySummaries(ctx, "custom", "remote", 1, []string{"same"})
	if err != nil || len(rows) != 1 || rows[0].PageTotal != 7 {
		t.Fatal(rows, err)
	}
	for _, key := range []struct {
		// backend 为隔离测试使用的协议。
		backend string
		// connection 为隔离测试使用的连接。
		connection string
		// revision 为隔离测试使用的连接版本。
		revision int64
	}{{"weknora", "remote", 1}, {"custom", "other", 1}, {"custom", "remote", 2}} {
		rows, err = c.LibrarySummaries(ctx, key.backend, key.connection, key.revision, []string{"same"})
		if err != nil || len(rows) != 0 {
			t.Fatal(rows, err)
		}
	}
	value.PageTotal = 0
	value.ObservedAt = 30
	if err = c.RecordLibrarySummary(ctx, value); err != nil {
		t.Fatal(err)
	}
	rows, err = c.LibrarySummaries(ctx, "custom", "remote", 1, []string{"same"})
	if err != nil || len(rows) != 1 || rows[0].PageTotal != 0 {
		t.Fatal(rows, err)
	}
}

func TestDeleteLibraryReferencesPreservesOtherConnections(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeController(db)
	ctx := context.Background()
	for _, connection := range []string{"first", "second"} {
		if err := db.Create(&models.KnowledgeTaskConfiguration{ID: connection, Backend: "custom", ConnectionID: connection, KnowledgeBaseID: "same"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := c.RecordLibrarySummary(ctx, models.KnowledgeLibrarySummary{Backend: "custom", ConnectionID: connection, ConnectionRevision: 1, KnowledgeBaseID: "same", PageTotal: 2, ObservedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.DeleteLibraryReferences(ctx, "custom", "first", "same"); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&models.KnowledgeTaskConfiguration{}, &models.KnowledgeLibrarySummary{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
			t.Fatal(count, err)
		}
	}
}
