package controllers

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"strings"
	"testing"
)

func TestLibraryEmbeddingProfilesAreIsolatedEncryptedAndRecoverable(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeController(db)
	ctx := context.Background()
	in := PrepareLibraryEmbeddingInput{RequestID: "save-a", LibraryID: "a", BaseURL: "https://example.test/v1", Model: "embed-a", APIKey: "TOP-SECRET-A", CoreInput: json.RawMessage(`{"RequestID":"core-a"}`)}
	prepared, err := c.PrepareLibraryEmbedding(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prepared.CoreInput), "TOP-SECRET") || strings.Contains(prepared.Ciphertext, "TOP-SECRET") {
		t.Fatal("plaintext credential persisted")
	}
	if _, _, err := c.LibraryEmbeddingSnapshot(ctx, "a"); err == nil {
		t.Fatal("prepared mutation became active before core acceptance")
	}
	if _, err = c.CommitLibraryEmbedding(ctx, "save-a", 4); err != nil {
		t.Fatal(err)
	}
	profile, key, err := c.LibraryEmbeddingSnapshot(ctx, "a")
	if err != nil || key != "TOP-SECRET-A" || profile.Version != 1 {
		t.Fatal(profile, err)
	}
	raw, _ := json.Marshal(profile)
	if strings.Contains(string(raw), "TOP-SECRET") || strings.Contains(string(raw), "ciphertext") {
		t.Fatal("key echoed")
	}
	if _, _, err = c.LibraryEmbeddingSnapshot(ctx, "b"); err == nil {
		t.Fatal("other library inherited key")
	}
	in.APIKey = "changed"
	if _, err = c.PrepareLibraryEmbedding(ctx, in); err == nil {
		t.Fatal("same request changed credential")
	}
	in.APIKey = "TOP-SECRET-A"
	in.CoreInput = json.RawMessage(`{"RequestID":"derived-later"}`)
	prepared, err = NewKnowledgeController(db).PrepareLibraryEmbedding(ctx, in)
	if err != nil || !strings.Contains(string(prepared.CoreInput), "core-a") {
		t.Fatal("restart lost frozen core intent", err)
	}
	if _, err = c.CommitLibraryEmbedding(ctx, "save-a", 4); err != nil {
		t.Fatal("replay", err)
	}
	old := models.LibraryEmbeddingProfile{}
	if err = db.Where("library_id = ?", "a").Take(&old).Error; err != nil || old.Version != 1 {
		t.Fatal(old, err)
	}
	pending, err := c.PrepareLibraryEmbedding(ctx, PrepareLibraryEmbeddingInput{RequestID: "pending-delete", LibraryID: "a", ExpectedVersion: 1, BaseURL: "https://example.test/v1", Model: "embed-a", APIKey: "new-pending-key"})
	if err != nil || pending.Ciphertext == "" {
		t.Fatal("pending fixture missing", err)
	}
	if err = c.DeleteLibraryReferences(ctx, "local", "local", "a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.LibraryEmbeddingSnapshot(ctx, "a"); err == nil {
		t.Fatal("deleted library retained active secret")
	}
	var receipt models.LibraryEmbeddingMutation
	if err = db.Where("request_id = ?", "pending-delete").Take(&receipt).Error; err != nil || receipt.Ciphertext != "" || receipt.Digest == "" {
		t.Fatal("deleted secret retained or reconciliation lost", err)
	}
}
