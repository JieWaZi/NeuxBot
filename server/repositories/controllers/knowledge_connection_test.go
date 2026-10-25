package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestKnowledgeConnectionCredentialAndCAS(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeConnectionController(db)
	ctx := context.Background()
	input := SaveKnowledgeConnectionInput{RequestID: "create", Connection: models.KnowledgeConnection{ID: "remote", Name: "共享", Backend: "weknora", BaseURL: "https://example.test/base", AuthMode: "api_key", Enabled: true}, Credential: "private-token"}
	first, err := c.Save(ctx, input)
	if err != nil || first.Revision != 1 {
		t.Fatal(first, err)
	}
	replay, err := c.Save(ctx, input)
	if err != nil || replay.Revision != 1 {
		t.Fatal(replay, err)
	}
	got, err := c.Get(ctx, "remote")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "private-token") || strings.Contains(string(encoded), "credentialRef") {
		t.Fatal("credential exposed")
	}
	secret, err := c.Credential(ctx, "remote")
	if err != nil || secret != "private-token" {
		t.Fatal(err)
	}
	var stored models.KnowledgeConnectionSecret
	if err := db.First(&stored).Error; err != nil || strings.Contains(stored.Ciphertext, "private-token") {
		t.Fatal("plain credential stored", err)
	}
	changed := input
	changed.RequestID = "update"
	changed.ExpectedRevision = 1
	changed.Connection.Name = "改名"
	changed.Credential = ""
	if _, err = c.Save(ctx, changed); err != nil {
		t.Fatal(err)
	}
	changed.RequestID = "stale"
	if _, err = c.Save(ctx, changed); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("stale save", err)
	}
	input.Credential = "different"
	if _, err = c.Save(ctx, input); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("request reuse", err)
	}
	secret, err = c.Credential(ctx, "remote")
	if err != nil || secret != "private-token" {
		t.Fatal("empty edit removed credential", err)
	}
}

func TestKnowledgeConnectionSnapshotKeepsCredentialOnSameRevision(t *testing.T) {
	db := newTestDB(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(2)
	if err = db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		t.Fatal(err)
	}
	c := NewKnowledgeConnectionController(db)
	ctx := context.Background()
	in := SaveKnowledgeConnectionInput{RequestID: "old", Connection: models.KnowledgeConnection{ID: "remote", Name: "共享", Backend: "custom", BaseURL: "https://old.example.test", AuthMode: "bearer", Enabled: true}, Credential: "old-only"}
	if _, err = c.Save(ctx, in); err != nil {
		t.Fatal(err)
	}
	updated := false
	var updateErr error
	// 在端点已读、凭据尚未读的精确间隙提交新地址和凭据，验证读取事务仍使用旧快照。
	if err = db.Callback().Query().After("gorm:query").Register("test:retarget-connection", func(tx *gorm.DB) {
		if updated || tx.Statement.Schema == nil || tx.Statement.Schema.Name != "KnowledgeConnection" {
			return
		}
		updated = true
		in.RequestID = "new"
		in.ExpectedRevision = 1
		in.Connection.BaseURL = "https://new.example.test"
		in.Credential = "new-only"
		_, updateErr = c.Save(ctx, in)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:retarget-connection") })
	v, token, err := c.Snapshot(ctx, "remote")
	if err != nil || updateErr != nil || !updated {
		t.Fatal(err, updateErr, updated)
	}
	if v.Revision != 1 || v.BaseURL != "https://old.example.test" || token != "old-only" {
		t.Fatal("connection and credential revisions diverged")
	}
	v, token, err = c.Snapshot(ctx, "remote")
	if err != nil || v.Revision != 2 || v.BaseURL != "https://new.example.test" || token != "new-only" {
		t.Fatal("new snapshot did not observe committed revision", err)
	}
}

func TestKnowledgeConnectionRejectsCredentialURLAndCrossIdentity(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeConnectionController(db)
	ctx := context.Background()
	for _, address := range []string{"file:///private", "https://user:pass@example.test", "https://example.test/?token=secret", "https://example.test/#secret"} {
		_, err := c.Save(ctx, SaveKnowledgeConnectionInput{RequestID: address, Connection: models.KnowledgeConnection{ID: "c", Name: "x", Backend: "custom", BaseURL: address, AuthMode: "bearer", Enabled: true}, Credential: "token"})
		if !errors.Is(err, status_error.KnowledgeInvalid) {
			t.Fatalf("accepted %s: %v", address, err)
		}
	}
}

func TestKnowledgeCustomConnectionRequiresSupportedBearerAuth(t *testing.T) {
	c := NewKnowledgeConnectionController(newTestDB(t))
	_, err := c.Save(context.Background(), SaveKnowledgeConnectionInput{RequestID: "unsupported", Connection: models.KnowledgeConnection{ID: "custom", Name: "共享", Backend: "custom", BaseURL: "http://127.0.0.1:8787", AuthMode: "api_key", Enabled: true}, Credential: "token"})
	if !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatalf("unsupported auth persisted: %v", err)
	}
}
