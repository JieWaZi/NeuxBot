package controllers

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/remote"
	"gorm.io/gorm"
	"testing"
)

func TestRemoteMountAtomicReplay(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeRemoteMountController(db)
	ctx := context.Background()
	input := AddKnowledgeRemoteMountInput{RequestID: "add", ConnectionID: "c", MountID: "m", Credential: "fixture-only", Detection: remote.Detection{BaseURL: "https://example.test/proxy/api/v1", Library: remote.Library{ID: "same", Name: "目标"}, ServiceKind: "unknown", UploadMode: "unknown"}}
	first, err := c.Add(ctx, input)
	if err != nil || first.KnowledgeBaseID != "same" || first.Revision != 1 {
		t.Fatal(first, err)
	}
	input.Detection.Library.Name = "服务器后来改名"
	replay, err := c.Add(ctx, input)
	if err != nil || replay.ID != first.ID || replay.Revision != first.Revision {
		t.Fatal("unstable replay", replay, err)
	}
	input.Credential = "different"
	if _, err = c.Add(ctx, input); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("request credential changed", err)
	}
	connection, token, err := NewKnowledgeConnectionController(db).Snapshot(ctx, "c")
	if err != nil || token != "fixture-only" || connection.Backend != "integration" {
		t.Fatal(connection, err)
	}
	mounts, err := c.List(ctx, "c")
	if err != nil || len(mounts) != 1 {
		t.Fatal(mounts, err)
	}
	if err = c.Remove(ctx, "m", 2); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("stale removal", err)
	}
	if err = c.Remove(ctx, "m", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = NewKnowledgeConnectionController(db).Get(ctx, "c"); err != nil {
		t.Fatal("mount deletion lost recovery target", err)
	}
}
func TestRemoteMountFailureRollsBackConnectionAndSecret(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeRemoteMountController(db)
	ctx := context.Background()
	if err := db.Callback().Create().Before("gorm:create").Register("test:mount-save-fails", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "KnowledgeRemoteMount" {
			tx.AddError(status_error.KnowledgeStorageError)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:mount-save-fails") })
	_, err := c.Add(ctx, AddKnowledgeRemoteMountInput{RequestID: "add", ConnectionID: "c", MountID: "m", Credential: "fixture-only", Detection: remote.Detection{BaseURL: "https://example.test/api/v1", Library: remote.Library{ID: "same", Name: "目标"}, ServiceKind: "unknown", UploadMode: "unknown"}})
	if err == nil {
		t.Fatal("injected failure ignored")
	}
	for _, model := range []any{&models.KnowledgeConnection{}, &models.KnowledgeConnectionSecret{}, &models.KnowledgeConnectionMutation{}, &models.KnowledgeRemoteMount{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("partial mount committed", count, err)
		}
	}
}
