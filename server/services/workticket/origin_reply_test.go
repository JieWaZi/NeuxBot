package workticket_test

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestOriginReplyRecoveryUsesAcceptedInputAndKeepsReplayDigest(t *testing.T) {
	db, service, origin := newTicketChat(t, nil)
	ctx := t.Context()
	in := ticketInput(origin, "accepted-reply")
	in.OriginReplyDigest = "exact-public-request"
	in.OriginReply = &eventlog.MessageCreatedData{SubmissionID: in.SubmissionID, Role: constants.MessageRoleUser, Actor: "user",
		Content: "Continue this work.", ReplyTo: &eventlog.MessageReply{MessageID: "original-work"}}
	binding, err := service.PrepareAdmission(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate interruption between the authoritative private log and SQLite acknowledgment.
	service = ticketService(db, nil)
	settle := func(ctx context.Context, row models.WorkTicketInput) error {
		row, err := service.ReconcileOriginReply(ctx, row, func(_ context.Context, chatID, submissionID string) (*models.Execution, error) {
			if chatID != in.WorkChatID || submissionID != in.SubmissionID {
				t.Fatalf("recovery read a different source: %s / %s", chatID, submissionID)
			}
			return &models.Execution{ID: binding.ExecutionID, SubmissionHash: in.SubmissionHash, Status: constants.ExecutionStatusCompleted}, nil
		})
		if err != nil {
			return err
		}
		return service.PublishOriginReply(ctx, row, func(_ context.Context, address string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
			if address != origin.ID || len(facts) != 1 || facts[0].MessageID != in.MessageID || facts[0].ExecutionID != "" {
				t.Fatalf("recovery changed the original publication: %s %+v", address, facts)
			}
			return nil, status_error.ChatLogError
		})
	}
	if err := service.RecoverOriginReplies(ctx, settle); !errors.Is(err, status_error.ChatLogError) {
		t.Fatalf("expected transient public journal failure: %v", err)
	}
	service = ticketService(db, nil)
	published := 0
	if err := service.RecoverOriginReplies(ctx, func(ctx context.Context, row models.WorkTicketInput) error {
		if row.ConfirmedAt == nil || row.FinishedAt == nil || row.OriginReplyDigest != in.OriginReplyDigest {
			t.Fatalf("source acceptance is incomplete after restart: %+v", row)
		}
		return service.PublishOriginReply(ctx, row, func(_ context.Context, address string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
			published++
			return nil, nil
		})
	}); err != nil || published != 1 {
		t.Fatalf("recovery published %d times: %v", published, err)
	}
	if err := service.RecoverOriginReplies(ctx, func(context.Context, models.WorkTicketInput) error {
		t.Fatal("accepted public receipt remained in the recovery index")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	row, err := service.OriginReply(ctx, origin.ID, in.SubmissionID)
	if err != nil || row.OriginReply != nil || row.OriginReplyDigest != in.OriginReplyDigest {
		t.Fatalf("cleanup lost stable origin replay evidence: %+v %v", row, err)
	}
}
