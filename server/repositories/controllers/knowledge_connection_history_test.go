package controllers

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"strings"
	"testing"
)

func TestRemoteRecoveryRetainsOriginalEncryptedConnectionRevision(t *testing.T) {
	db := newTestDB(t)
	c := NewKnowledgeConnectionController(db)
	ctx := context.Background()
	input := SaveKnowledgeConnectionInput{RequestID: "first", Credential: "old-private", Connection: models.KnowledgeConnection{ID: "c", Backend: "custom", Name: "原目标", BaseURL: "https://first.test/proxy", AuthMode: "bearer", Enabled: true}}
	if _, err := c.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.RequestID = "second"
	input.ExpectedRevision = 1
	input.Connection.BaseURL = "https://second.test"
	input.Credential = "new-private"
	if _, err := c.Save(ctx, input); err != nil {
		t.Fatal(err)
	}
	history, ok := c.(KnowledgeConnectionHistoryController)
	if !ok {
		t.Fatal("no immutable connection history port")
	}
	first, key, err := history.SnapshotRevision(ctx, "c", 1)
	if err != nil || first.BaseURL != "https://first.test/proxy" || key != "old-private" || first.Revision != 1 {
		t.Fatal(first, err)
	}
	second, key, err := history.SnapshotRevision(ctx, "c", 2)
	if err != nil || second.BaseURL != "https://second.test" || key != "new-private" {
		t.Fatal(second, err)
	}
	var rows []models.KnowledgeConnectionRevision
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.Contains(row.CredentialCiphertext, "private") {
			t.Fatal("history contains plain secret")
		}
	}
	if _, _, err = history.SnapshotRevision(ctx, "other", 1); err == nil {
		t.Fatal("history identity crossed connections")
	}
}
