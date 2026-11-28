package main_conversations

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
)

func TestMainConversationQuotedContinuationUsesTransferredWork(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	gate := filepath.Join(root, "release-transferred-worker")
	t.Setenv(queueFixtureGate, gate)
	t.Cleanup(func() { _ = os.WriteFile(gate, nil, 0600) })
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const origin = "native-main-partner"
	const receiver = "native-transfer-receiver"
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: receiver, Name: "Receiver"}); err != nil {
		t.Fatal(err)
	}
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	send := func(id, body, quote string) (MainConversationAcceptance, error) {
		t.Helper()
		raw, err := (&SendMainConversationMessage{SubjectType: "waker", SubjectID: origin, Body: MainConversationInput{
			SubmissionID: id, Content: body, ReplyToMessageID: quote, StartWork: quote == "",
		}}).Output(request)
		if err != nil {
			return MainConversationAcceptance{}, err
		}
		return raw.(MainConversationAcceptance), nil
	}
	first, err := send("initial", "Inspect the harmless fixture.", "")
	if err != nil {
		t.Fatal(err)
	}
	address := workticket.Address{Kind: constants.WorkTicketOriginMain, ID: first.Message.ConversationID}
	ticketID := first.Message.ID
	waitTicket := func(generation int64, status constants.WorkTicketStatus) models.WorkTicket {
		t.Helper()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			ticket, found, err := global.WorkTicketService.ForMessage(ctx, address, ticketID)
			if err != nil {
				t.Fatal(err)
			}
			if found && ticket.Generation == generation && ticket.Status == status && !global.ExecutionService.IsExecutionActive(ticket.WorkChatID, ticket.CurrentExecutionID) {
				return ticket
			}
			select {
			case <-ctx.Done():
				t.Fatalf("ticket generation %d did not settle: %+v", generation, ticket)
			case <-tick.C:
			}
		}
	}
	before := waitTicket(1, constants.WorkTicketStatusWaiting)
	handoff, err := global.TransferWorkTicket(ctx, workticket.TransferInput{
		Address: address, TicketID: ticketID, RequestID: "handoff", ExpectedGeneration: before.Generation,
		OwnerKind: "waker", OwnerID: receiver, Content: "fixture explicit completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		global.RunWorkTicketCollaboration(worker, nil, global.ExecutionService)
	}()
	defer func() { stop(); <-done }()
	transferred := waitTicket(handoff.Generation, constants.WorkTicketStatusCompleted)
	rows, _, err := global.MainConversationService.Messages(ctx, address.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var reportID string
	var notices int
	for _, row := range rows {
		if row.Content == "Transferred to Receiver." && row.ReplyToMessageID == ticketID {
			notices++
		}
		if row.Content == "Explicit completed report." && row.ReplyToMessageID == ticketID && row.SourceWakerID == receiver {
			reportID = row.ID
		}
	}
	if notices != 1 {
		t.Fatalf("expected one original-conversation handoff: %+v", rows)
	}
	if reportID == "" {
		t.Fatalf("receiver's result missing in origin: %+v", rows)
	}
	for i, quote := range []string{ticketID, reportID} {
		const content = "Continue with the harmless fixture. fixture explicit completed"
		submission := "continue-" + quote
		accepted, err := send(submission, content, quote)
		if err != nil {
			t.Fatalf("follow-up quoting %s: %v", quote, err)
		}
		settled := waitTicket(transferred.Generation+int64(i)+1, constants.WorkTicketStatusCompleted)
		if settled.WorkChatID != transferred.WorkChatID || settled.ID != ticketID || settled.OriginID != address.ID || settled.OwnerWakerID != receiver {
			t.Fatalf("follow-up changed original binding: %+v", settled)
		}
		row, err := global.MainConversationService.Message(ctx, address.ID, accepted.Message.ID)
		if err != nil || row.ReplyToMessageID != quote || row.Content != content {
			t.Fatalf("follow-up was not published in the original conversation: %+v, %v", row, err)
		}
		replay, err := send(submission, content, quote)
		if err != nil || replay.Message.ID != accepted.Message.ID || replay.ExecutionID != accepted.ExecutionID {
			t.Fatalf("follow-up resubmission changed receipt: %+v, %v", replay, err)
		}
	}
	active, err := send("gated", "gate the initial work; fixture explicit completed", ticketID)
	if err != nil || active.ExecutionID == "" {
		t.Fatalf("could not start gated continuation: %+v, %v", active, err)
	}
	if !global.ExecutionService.IsExecutionActive(transferred.WorkChatID, active.ExecutionID) {
		t.Fatalf("receiver did not remain active: %+v", active)
	}
	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	const busySubmission = "receiver-still-working"
	const busyContent = "Add one more fixture check. fixture explicit completed"
	queued, err := send(busySubmission, busyContent, reportID)
	if err != nil || queued.QueuedInput == nil || queued.QueuedInput.WorkChatID != "" {
		t.Fatalf("follow-up while receiver active should queue with an origin-only receipt: %+v, %v", queued, err)
	}
	if run, err := global.ChatService.ReadSubmission(ctx, transferred.WorkChatID, busySubmission); err != nil || run != nil {
		t.Fatalf("queued reply bypassed the receiver's single native input lane: %+v, %v", run, err)
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	settled := waitTicket(transferred.Generation+4, constants.WorkTicketStatusCompleted)
	row, err := global.MainConversationService.Message(ctx, address.ID, queued.QueuedInput.ID)
	if err != nil || row.AdmissionPending || row.Content != busyContent || row.ReplyToMessageID != reportID ||
		settled.WorkChatID != transferred.WorkChatID || settled.ID != ticketID || settled.OriginID != address.ID {
		t.Fatalf("queued follow-up did not reach the original address with the same ticket: %+v, %+v, %v", row, settled, err)
	}
	target, err := global.MainConversationService.Ensure(ctx, "waker", receiver)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err = global.MainConversationService.Messages(ctx, target.ID, 0, 20)
	if err != nil || len(rows) != 0 {
		t.Fatalf("origin follow-up escaped into target main conversation: %+v, %v", rows, err)
	}
}
