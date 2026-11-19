package controllers

import (
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestRetentionCandidatesStableCursorAndIndexedOwners(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	control := NewChatController(db)
	for _, id := range []string{"a", "b", "boundary", "protected", "active", "unknown", "owned", "hidden", "waiting", "team", "worker", "auto", "creation", "onboarding"} {
		row := models.Chat{ID: id, WakerID: "partner", LatestSequence: 5, LastActivityAt: 10}
		switch id {
		case "boundary":
			row.LastActivityAt = 11
		case "unknown":
			row.LastActivityAt = 0
		case "active":
			row.ActiveExecutionID = "active"
		case "hidden":
			row.HiddenFromWakerHistory = true
		case "waiting":
			row.WaitingApproval = true
		case "protected":
			row.RetentionProtected = true
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []any{&models.Waker{ID: "partner", Name: "partner", OnboardingChatID: "onboarding", OnboardingStatus: "pending"}, &models.IMConversationLink{ID: "im", ChatID: "owned"}, &models.ChannelThread{ID: "t", LeadChatID: "team"}, &models.WorkflowCreationSession{ID: "wc", ChatID: "creation"}, &models.WorkflowNodeInstance{ID: "node", ChatID: "worker"}, &models.AutomaticRun{ID: "r", ChatID: "auto"}} {
		if err := db.Create(owner).Error; err != nil {
			t.Fatal(err)
		}
	}
	n, err := control.CountRetentionCandidates(ctx, 11)
	if err != nil || n != 2 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	for i, after := range []string{"", "a", "b"} {
		rows, err := control.ListRetentionCandidates(ctx, 11, 10, after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			if len(rows) != 0 {
				t.Fatal(rows)
			}
			break
		}
		if len(rows) != 1 || rows[0].ChatID != []string{"a", "b"}[i] || rows[0].Sequence != 5 {
			t.Fatal(rows)
		}
	}
	var plan []struct{ Detail string }
	query := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return retentionCandidateQuery(tx, 11).Order("last_activity_at, id").Find(&[]models.Chat{})
	})
	if err := db.Raw("EXPLAIN QUERY PLAN " + query).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	var index bool
	for _, row := range plan {
		index = index || strings.Contains(row.Detail, "idx_chat_retention")
		if strings.Contains(row.Detail, "SCAN ") || strings.Contains(row.Detail, "TEMP B-TREE") {
			t.Fatalf("unindexed plan: %+v", plan)
		}
	}
	if !index {
		t.Fatalf("candidate index missing: %+v", plan)
	}
}
