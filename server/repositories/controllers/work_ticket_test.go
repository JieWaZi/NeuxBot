package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

func TestWorkTicketFactsFollowOriginOrSourceDeletion(t *testing.T) {
	db := newTestDB(t)
	tx := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	ctx := t.Context()
	for _, row := range []any{
		&models.Chat{ID: "chat", WakerID: "owner"},
		&models.MainConversation{ID: "main", SubjectType: "waker", SubjectID: "owner"},
		&models.WorkTicket{ID: "chat-ticket", OriginKind: constants.WorkTicketOriginChat, OriginID: "chat", WorkChatID: "chat"},
		&models.WorkTicket{ID: "main-ticket", OriginKind: constants.WorkTicketOriginMain, OriginID: "main", WorkChatID: "unmaterialized"},
		&models.WorkTicket{ID: "other-ticket", OriginKind: constants.WorkTicketOriginChat, OriginID: "another", WorkChatID: "another"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"chat-ticket", "main-ticket", "other-ticket"} {
		for _, row := range []any{
			&models.WorkTicketAttempt{TicketID: id, Generation: 1},
			&models.WorkTicketInput{ExecutionID: id, TicketID: id, OriginID: id},
			&models.WorkTicketReport{ID: id, TicketID: id},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Transaction(ctx, func(ctx context.Context) error {
		if err := NewChatController(db).Delete(ctx, "chat"); err != nil {
			return err
		}
		return deleteMainConversation(dbhelper.GetCtxDB(ctx, db), "waker", "owner")
	}); err != nil {
		t.Fatal(err)
	}

	var tickets []models.WorkTicket
	if err := db.Find(&tickets).Error; err != nil || len(tickets) != 1 || tickets[0].ID != "other-ticket" {
		t.Fatalf("tickets after deleting two origins: %+v, %v", tickets, err)
	}
	for _, model := range []any{&models.WorkTicketAttempt{}, &models.WorkTicketInput{}, &models.WorkTicketReport{}} {
		var kept []string
		if err := db.Model(model).Pluck("ticket_id", &kept).Error; err != nil || len(kept) != 1 || kept[0] != "other-ticket" {
			t.Fatalf("orphaned %T records after deletion: %v, %v", model, kept, err)
		}
	}
}

func TestWorkTicketLookupExcludesRemovedAndUnconfirmedMainMessages(t *testing.T) {
	db := newTestDB(t)
	kind := constants.WorkTicketOriginMain
	origin := "main"
	ticket := models.WorkTicket{ID: "ticket", OriginKind: kind, OriginID: origin, WorkChatID: "work"}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []models.MainConversationMessage{
		{ID: "removed", ConversationID: origin, Sequence: 1, RemovedAt: 1},
		{ID: "pending", ConversationID: origin, Sequence: 2, AdmissionPending: true},
		{ID: "visible", ConversationID: origin, Sequence: 3},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := int64(1)
	for _, id := range []string{"removed", "pending", "visible", "source-record"} {
		input := models.WorkTicketInput{
			ExecutionID: id, TicketID: ticket.ID, OriginKind: kind, OriginID: origin, SubmissionID: id, MessageID: id, ConfirmedAt: &now,
		}
		if err := db.Create(&input).Error; err != nil {
			t.Fatal(err)
		}
	}
	store := NewWorkTicketController(db)
	views, err := store.TicketsForMessages(t.Context(), kind, origin, []string{"removed", "pending", "source-record", "visible", "visible"})
	if err != nil || len(views) != 2 || views[0].MessageID != "source-record" || views[1].MessageID != "visible" {
		t.Fatalf("unexpected message associations: %+v, %v", views, err)
	}
}

func TestWorkTicketLookupUsesScopedMessageAndTicketIndexes(t *testing.T) {
	db := newTestDB(t)
	type planRow struct {
		// Detail 记录 SQLite 为每个查询阶段选择的索引。
		Detail string
	}
	statements := []func(*gorm.DB) *gorm.DB{
		func(db *gorm.DB) *gorm.DB {
			return db.Model(&models.WorkTicketInput{}).Where("origin_kind = ? AND origin_id = ? AND message_id IN ? AND confirmed_at IS NOT NULL", "chat", "source", []string{"a", "b"}).Find(&[]models.WorkTicketInput{})
		},
		func(db *gorm.DB) *gorm.DB {
			return db.Model(&models.WorkTicket{}).Where("origin_kind = ? AND origin_id = ? AND id IN ?", "chat", "source", []string{"a", "b"}).Find(&[]models.WorkTicket{})
		},
	}
	for _, query := range statements {
		statement := query(db.Session(&gorm.Session{DryRun: true})).Statement
		var rows []planRow
		if err := db.Raw("EXPLAIN QUERY PLAN "+statement.SQL.String(), statement.Vars...).Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || !strings.Contains(rows[0].Detail, "USING INDEX") {
			t.Fatalf("lookup did not use a scoped index: %+v", rows)
		}
	}
}

func TestWorkTicketRecoveryQueriesHaveNarrowIndexes(t *testing.T) {
	db := newTestDB(t)
	for _, query := range []struct {
		// sql reproduces the corresponding keyset lookup.
		sql string
		// index is the partial index for only unsettled rows.
		index string
	}{
		{"SELECT * FROM work_ticket_reports INDEXED BY idx_work_ticket_undelivered WHERE message_id = '' AND id > ? ORDER BY id LIMIT 100", "idx_work_ticket_undelivered"},
		{"SELECT * FROM work_ticket_inputs INDEXED BY idx_work_ticket_unfinished WHERE confirmed_at IS NOT NULL AND finished_at IS NULL AND execution_id > ? ORDER BY execution_id LIMIT 100", "idx_work_ticket_unfinished"},
	} {
		var rows []struct {
			// Detail names the index used to read the next recovery page.
			Detail string
		}
		if err := db.Raw("EXPLAIN QUERY PLAN "+query.sql, "after-id").Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || !strings.Contains(rows[0].Detail, query.index) {
			t.Fatalf("unbounded recovery lookup: %+v", rows)
		}
	}
}

func TestWorkTicketActivityCandidatesSeparateOriginAndReceiver(t *testing.T) {
	db := newTestDB(t)
	const main = constants.WorkTicketOriginMain
	ctx := t.Context()
	store := NewWorkTicketController(db)
	for _, row := range []models.WorkTicket{
		{ID: "A-root", OriginKind: main, OriginID: "A-main", OwnerWakerID: "A"},
		{ID: "B-child", OriginKind: main, OriginID: "B-main", OwnerWakerID: "B", ParentTicketID: "A-root", Status: constants.WorkTicketStatusCompleted},
		{ID: "C-child", OriginKind: main, OriginID: "C-main", OwnerWakerID: "C", ParentTicketID: "B-child"},
		{ID: "team-root", OriginKind: main, OriginID: "team-main", OwnerWakerID: "D"},
		{ID: "team-child", OriginKind: main, OriginID: "B-main", OwnerWakerID: "B", ParentTicketID: "team-root"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		conversation, kind, subject string
		want                        map[string]bool
	}{
		{"A-main", "waker", "A", map[string]bool{"A-root": true, "B-child": true, "C-child": true}},
		{"B-main", "waker", "B", map[string]bool{"B-child": true, "team-child": true}},
		{"team-main", "team", "team", map[string]bool{"team-root": true, "team-child": true}},
		{"", "waker", "absent", map[string]bool{}},
	} {
		rows, err := store.ActivityCandidates(ctx, tc.conversation, tc.kind, tc.subject)
		if err != nil || len(rows) != len(tc.want) {
			t.Fatalf("%s: unexpected candidates %+v, %v", tc.subject, rows, err)
		}
		for _, row := range rows {
			if !tc.want[row.ID] {
				t.Fatalf("%s: unexpected candidate %s", tc.subject, row.ID)
			}
		}
	}
}

func TestWorkTicketActivityInvalidatesOriginAndRecipientAfterCommit(t *testing.T) {
	ctx := t.Context()
	hub := events.New()
	t.Cleanup(hub.Close)
	db := WithEventPublisher(newTestDB(t), hub)
	origin := eventsResource("team", "team")
	receiver := eventsResource("waker", "B")
	source := hub.Subscribe(constants.MainConversationEventTopic, origin)
	target := hub.Subscribe(constants.MainConversationEventTopic, receiver)
	t.Cleanup(source.Close)
	t.Cleanup(target.Close)
	for _, row := range []any{
		&models.MainConversation{ID: "team-main", SubjectType: "team", SubjectID: "team"},
		&models.WorkTicket{ID: "root", OriginKind: constants.WorkTicketOriginMain, OriginID: "team-main"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	tx := NewTransactionController(dbhelper.NewGormTransactionManager(db))
	store := NewWorkTicketController(db)
	err := tx.Transaction(ctx, func(ctx context.Context) error {
		if err := store.CreateTicket(ctx, &models.WorkTicket{ID: "child", ParentTicketID: "root", OwnerWakerID: "B"}); err != nil {
			return err
		}
		select {
		case <-source.Events:
			t.Fatal("event before commit")
		default:
		}
		select {
		case <-target.Events:
			t.Fatal("event before commit")
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range []<-chan events.Event{source.Events, target.Events} {
		select {
		case e := <-ch:
			if e.Type != constants.MainConversationEventTopic+".changed" {
				t.Fatalf("wrong change %+v", e)
			}
		case <-time.After(time.Second):
			t.Fatal("missing work activity invalidation")
		}
	}
}
