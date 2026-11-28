package workticket_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"gorm.io/gorm"
)

func TestTransferPreservesOriginFencesOldOwnerAndRestartsPendingHandoff(t *testing.T) {
	db, tickets, origin := newTicketChat(t, nil)
	ctx := t.Context()
	first := admit(t, tickets, ticketInput(origin, "original"))
	if err := controllers.NewWakerController(db).Create(ctx, models.Waker{ID: "receiver", Name: "Receiver"}); err != nil {
		t.Fatal(err)
	}
	main := controllers.NewMainConversationController(db)
	receiver, err := main.Ensure(ctx, "waker", "receiver")
	if err != nil {
		t.Fatal(err)
	}
	source, err := main.ReserveWork(ctx, receiver.ID, "receiver", "handoff")
	if err != nil {
		t.Fatal(err)
	}
	input := workticket.TransferInput{Address: origin, TicketID: first.Ticket.ID, ExpectedGeneration: first.Generation, RequestID: "handoff",
		OwnerKind: "waker", OwnerID: "receiver", OwnerWakerID: "receiver", WorkChatID: source.WorkChatID, Content: "Finish the verification."}
	wrong := input
	wrong.WorkChatID = "unauthorized"
	if _, err := tickets.Transfer(ctx, wrong); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("unauthorized target acquired the ticket: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_transfer BEFORE UPDATE OF owner_id ON work_tickets BEGIN SELECT RAISE(ABORT, 'test transfer rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tickets.Transfer(ctx, input); err == nil {
		t.Fatal("expected transfer transaction to roll back")
	}
	if err := db.Exec(`DROP TRIGGER reject_transfer`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tickets.Report(ctx, first, workticket.ReportInput{RequestID: "before-transfer", Kind: "progress", Content: "original actor is authorized"}, noDelivery); err != nil {
		t.Fatalf("failed transfer revoked original ownership: %v", err)
	}
	change, err := tickets.Transfer(ctx, input)
	if err != nil || change.TicketID != first.Ticket.ID || change.Generation != first.Generation+1 || change.PreviousExecutionID != first.ExecutionID || change.ExecutionID == "" {
		t.Fatalf("wrong transfer receipt: %+v %v", change, err)
	}
	if _, err := tickets.Report(ctx, first, workticket.ReportInput{RequestID: "stale", Kind: "completed", Content: "Late conclusion"}, noDelivery); !errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		t.Fatalf("old owner could complete a transferred ticket: %v", err)
	}
	restarted := ticketService(db, nil)
	replayInput := input
	replayInput.OwnerWakerID, replayInput.WorkChatID = "", ""
	replayInput.ResolveTarget = func(context.Context, string) (string, string, error) {
		t.Fatal("replay read mutable target information")
		return "", "", nil
	}
	if replay, err := restarted.Transfer(ctx, replayInput); err != nil || replay != change {
		t.Fatalf("same transfer was not replayed after restart: %+v %v", replay, err)
	}
	wrong = input
	wrong.Content = "Changed instruction"
	if _, err := restarted.Transfer(ctx, wrong); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("same request ID accepted a changed handoff: %v", err)
	}
	launches, err := restarted.PendingTransfers(ctx, "", 10)
	if err != nil || len(launches) != 1 || launches[0].Ticket.OriginID != origin.ID || launches[0].Ticket.OwnerWakerID != "receiver" ||
		launches[0].Ticket.ProjectID != "" || len(launches[0].Ticket.AllowedDirectories) != 0 || launches[0].Receipt != change {
		t.Fatalf("restart lost or broadened the transferred launch: %+v %v", launches, err)
	}
	if err := controllers.NewChatController(db).Create(ctx, models.Chat{ID: source.WorkChatID, WakerID: "receiver", HiddenFromWakerHistory: true,
		WorkspaceDirectory: "/receiver", AdditionalWorkingDirectories: []string{"/receiver"}}); err != nil {
		t.Fatal(err)
	}
	in := workticket.Admission{Address: origin, TicketID: first.Ticket.ID, MessageID: change.ExecutionID, SourceMessageID: "private-user-message",
		WorkChatID: source.WorkChatID, ExecutionID: change.ExecutionID, SubmissionID: change.ExecutionID, SubmissionHash: "admission-digest",
		OwnerKind: "waker", OwnerID: "receiver", OwnerWakerID: "receiver", PermissionLevel: constants.PermissionLevelDefault,
		WorkingDirectory: "/receiver", AllowedDirectories: []string{"/receiver"}}
	inherited := in
	inherited.AllowedDirectories = []string{"/receiver", "/work"}
	if _, err := restarted.PrepareAdmission(ctx, inherited); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("handoff inherited prior workspace authorization: %v", err)
	}
	binding := admit(t, restarted, in)
	if pending, err := restarted.PendingTransfers(ctx, "", 10); err != nil || len(pending) != 0 {
		t.Fatalf("admitted handoff still queued: %+v %v", pending, err)
	}
	conclusion := workticket.ReportInput{RequestID: "receiver-done", Kind: "completed", Content: "Verified by the receiver."}
	deliveries := 0
	var journal []eventlog.Record
	cut := errors.New("stopped before confirming the original journal receipt")
	publish := func(_ context.Context, ticket models.WorkTicket, reportID string) (string, error) {
		if ticket.ID != first.Ticket.ID || ticket.OriginID != origin.ID || ticket.OwnerWakerID != "receiver" {
			t.Fatalf("report escaped the original private origin: %+v", ticket)
		}
		deliveries++
		data, err := json.Marshal(eventlog.ExecutionDeliveryData{SourceID: reportID, Summary: conclusion.Content, WorkTicketReport: true, WorkTicketExecutionID: binding.ExecutionID})
		if err != nil {
			t.Fatal(err)
		}
		journal = append(journal, eventlog.Record{Type: constants.ChatEventTypeExecutionDelivery, ExecutionID: first.ExecutionID, MessageID: first.ExecutionID, Data: data})
		return "", cut
	}
	if _, err := restarted.Report(ctx, binding, conclusion, publish); !errors.Is(err, cut) {
		t.Fatalf("expected cut between journal and report confirmation: %v", err)
	}
	restarted = ticketService(db, nil)
	if err := restarted.RecoverOrdinaryReports(ctx, func(chatID, executionID string) ([]eventlog.Record, error) {
		if chatID != origin.ID || executionID != first.ExecutionID {
			t.Fatalf("recovery read a different destination: %s / %s", chatID, executionID)
		}
		return journal, nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.Report(ctx, binding, conclusion, publish); err != nil || got.MessageID != first.ExecutionID || !got.Replayed {
		t.Fatalf("transferred result did not settle the same original turn: %+v %v", got, err)
	}
	if deliveries != 1 {
		t.Fatalf("delivery count = %d, want 1", deliveries)
	}
}

func TestTransferKeepsDirectReturnsAndDoesNotUnblockEarly(t *testing.T) {
	db, service, origin := newTicketChat(t, nil)
	ctx := t.Context()
	original := admit(t, service, ticketInput(origin, "transfer-parent"))
	for _, row := range []any{
		&models.Waker{ID: "receiver", Name: "Receiver"}, &models.Waker{ID: "helper-one", Name: "Helper one"}, &models.Waker{ID: "helper-two", Name: "Helper two"},
		&models.MainConversation{ID: "receiver-main", SubjectType: "waker", SubjectID: "receiver"},
		&models.MainConversation{ID: "helper-one-main", SubjectType: "waker", SubjectID: "helper-one"},
		&models.MainConversation{ID: "helper-two-main", SubjectType: "waker", SubjectID: "helper-two"},
		&models.MainConversationWork{ConversationID: "receiver-main", WakerID: "receiver", WorkChatID: "receiver-work", ScopeKey: "transfer"},
		&models.MainConversationWork{ConversationID: "helper-one-main", WakerID: "helper-one", WorkChatID: "helper-one-work", ScopeKey: "delegation"},
		&models.MainConversationWork{ConversationID: "helper-two-main", WakerID: "helper-two", WorkChatID: "helper-two-work", ScopeKey: "delegation"},
		&models.MainConversationWork{ConversationID: "helper-one-main", WakerID: "helper-one", WorkChatID: "helper-one-handoff", ScopeKey: "transfer"},
		&models.Chat{ID: "helper-one-work", WakerID: "helper-one", HiddenFromWakerHistory: true},
		&models.Chat{ID: "helper-two-work", WakerID: "helper-two", HiddenFromWakerHistory: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	delegate := func(owner string) (workticket.DelegationInput, workticket.Binding) {
		t.Helper()
		in := workticket.DelegationInput{RequestID: owner, Target: workticket.Address{Kind: constants.WorkTicketOriginMain, ID: owner + "-main"},
			OwnerKind: "waker", OwnerID: owner, OwnerWakerID: owner, WorkChatID: owner + "-work", Content: "Check the fixture."}
		child, err := service.Delegate(ctx, original, in)
		if err != nil {
			t.Fatal(err)
		}
		return in, admit(t, service, workticket.Admission{Address: in.Target, TicketID: child.ID, WorkChatID: child.WorkChatID,
			MessageID: child.ID, SourceMessageID: child.ID, ExecutionID: child.ID, SubmissionID: child.ID, SubmissionHash: "first-child-input",
			OwnerKind: child.OwnerKind, OwnerID: owner, OwnerWakerID: owner, PermissionLevel: child.PermissionLevel})
	}
	firstInput, firstChild := delegate("helper-one")
	secondInput, secondChild := delegate("helper-two")
	question, err := service.Report(ctx, secondChild, workticket.ReportInput{RequestID: "question", Kind: "waiting", Content: "Which fixture?"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []workticket.Binding{original, secondChild} {
		if err := service.FinishTurn(ctx, binding, constants.ExecutionStatusCompleted); err != nil {
			t.Fatal(err)
		}
	}
	oldQuestionRun := workticket.DelegationQuestionExecutionID(original.Ticket, question.ID)
	oldQuestion := ticketInput(origin, oldQuestionRun)
	oldQuestion.TicketID, oldQuestion.ExecutionID = original.Ticket.ID, oldQuestionRun
	oldQuestion.ResumeAfterExecutionID, oldQuestion.DelegationQuestionID = original.ExecutionID, question.ID
	oldResponder := admit(t, service, oldQuestion)
	transfer := workticket.TransferInput{Address: origin, TicketID: original.Ticket.ID, ExpectedGeneration: original.Generation,
		RequestID: "transfer-to-valid-owner", OwnerKind: "waker", OwnerID: "receiver", OwnerWakerID: "receiver", WorkChatID: "receiver-work", Content: "Continue the original work."}
	cycle := transfer
	cycle.RequestID, cycle.OwnerID, cycle.OwnerWakerID, cycle.WorkChatID = "transfer-to-dependent", "helper-one", "helper-one", "helper-one-handoff"
	if _, err := service.Transfer(ctx, cycle); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("a live dependency became its own ancestor: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_child_transfer BEFORE UPDATE OF parent_generation ON work_tickets BEGIN SELECT RAISE(ABORT, 'test child transfer rollback'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transfer(ctx, transfer); err == nil {
		t.Fatal("parent transfer committed after failing to transfer a child relation")
	}
	if reports, err := service.DelegationReports(ctx, oldResponder, question.ID); err != nil || len(reports) != 1 {
		t.Fatalf("failed transfer displaced the original owner: %+v %v", reports, err)
	}
	if err := db.Exec(`DROP TRIGGER reject_child_transfer`).Error; err != nil {
		t.Fatal(err)
	}
	transferred, err := service.Transfer(ctx, transfer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplyToDelegation(ctx, oldResponder, "stale-reply", question.ID, "Stale answer"); !errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
		t.Fatalf("old owner could still answer a child: %v", err)
	}
	if replay, err := service.DelegationReceipt(ctx, original, firstInput.RequestID); err != nil || replay.ID != firstChild.Ticket.ID {
		t.Fatalf("transfer invalidated the original delegation receipt: %+v %v", replay, err)
	}
	if replay, err := service.Delegate(ctx, original, firstInput); err != nil || replay.ID != firstChild.Ticket.ID {
		t.Fatalf("transfer changed the original delegation replay: %+v %v", replay, err)
	}
	result := workticket.ReportInput{RequestID: "done", Kind: "completed", Content: "Verified result."}
	firstReport, err := service.Report(ctx, firstChild, result, nil)
	if err != nil {
		t.Fatalf("child finishing during handoff lost its direct parent: %v", err)
	}
	if err := db.Create(&models.Chat{ID: "receiver-work", WakerID: "receiver", HiddenFromWakerHistory: true}).Error; err != nil {
		t.Fatal(err)
	}
	in := workticket.Admission{Address: origin, TicketID: original.Ticket.ID, WorkChatID: "receiver-work",
		MessageID: transferred.ExecutionID, SourceMessageID: "handoff-input", ExecutionID: transferred.ExecutionID,
		SubmissionID: transferred.ExecutionID, SubmissionHash: "handoff", OwnerKind: "waker", OwnerID: "receiver", OwnerWakerID: "receiver",
		PermissionLevel: constants.PermissionLevelDefault}
	incoming := admit(t, service, in)
	if reports, err := service.DelegationReports(ctx, incoming, firstReport.ID); err != nil || len(reports) != 1 || reports[0].ID != firstReport.ID {
		t.Fatalf("new owner could not read original child's result: %+v %v", reports, err)
	}
	if _, err := service.Report(ctx, incoming, result, noDelivery); !errors.Is(err, status_error.WorkTicketCannotContinue) {
		t.Fatalf("new owner could close the parent with a live dependency: %v", err)
	}
	if err := service.FinishTurn(ctx, incoming, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	ready, err := service.ReadyDelegationParents(ctx, "", 10)
	if err != nil || len(ready) != 1 || ready[0].QuestionID != question.ID {
		t.Fatalf("unanswered direct question did not wake the incoming owner: %+v %v", ready, err)
	}
	incomingQuestionRun := workticket.DelegationQuestionExecutionID(ready[0].Ticket, question.ID)
	if incomingQuestionRun == oldQuestionRun {
		t.Fatal("new owner would reuse the old owner's question execution")
	}
	answerIn := in
	answerIn.ExecutionID, answerIn.MessageID, answerIn.SubmissionID = incomingQuestionRun, incomingQuestionRun, incomingQuestionRun
	answerIn.SourceMessageID, answerIn.ResumeAfterExecutionID, answerIn.DelegationQuestionID = "incoming-question", incoming.ExecutionID, question.ID
	responder := admit(t, service, answerIn)
	answer, err := service.ReplyToDelegation(ctx, responder, "answer", question.ID, "Use fixture Alpha.")
	if err != nil {
		t.Fatalf("incoming owner could not respond to direct question: %v", err)
	}
	if err := service.FinishTurn(ctx, responder, constants.ExecutionStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if ready, err := service.ReadyDelegationParents(ctx, "", 10); err != nil || len(ready) != 0 {
		t.Fatalf("answered question or incomplete dependency woke the parent: %+v %v", ready, err)
	}
	secondChild = admit(t, service, workticket.Admission{Address: secondInput.Target, TicketID: secondChild.Ticket.ID,
		WorkChatID: secondChild.WorkChatID, MessageID: answer.ID, SourceMessageID: "child-answer", ExecutionID: answer.ID,
		SubmissionID: answer.ID, SubmissionHash: "child-answer", OwnerKind: secondInput.OwnerKind, OwnerID: secondInput.OwnerID,
		OwnerWakerID: secondInput.OwnerWakerID, PermissionLevel: constants.PermissionLevelAuto,
		ResumeAfterExecutionID: secondChild.ExecutionID, DelegationReplyID: answer.ID})
	if _, err := service.Report(ctx, secondChild, result, nil); err != nil {
		t.Fatalf("second child lost its original return: %v", err)
	}
	ready, err = service.ReadyDelegationParents(ctx, "", 10)
	if err != nil || len(ready) != 1 || ready[0].Ticket.CurrentExecutionID != responder.ExecutionID || len(ready[0].Reports) != 2 {
		t.Fatalf("original child results did not wake the new owner: %+v %v", ready, err)
	}
	in.MessageID, in.SourceMessageID, in.ExecutionID, in.SubmissionID, in.ResumeAfterExecutionID =
		"resume-handoff", "resume-handoff", "resume-handoff", "resume-handoff", responder.ExecutionID
	resume := admit(t, service, in)
	publish := func(context.Context, models.WorkTicket, string) (string, error) { return original.ExecutionID, nil }
	if _, err := service.Report(ctx, resume, result, publish); err != nil {
		t.Fatalf("could not settle original ticket after both direct returns: %v", err)
	}
	if ready, err := service.ReadyDelegationParents(ctx, "", 10); err != nil || len(ready) != 0 {
		t.Fatalf("already consumed results woke the new owner again: %+v %v", ready, err)
	}
}

func TestTransferPendingPageBatchesCurrentAttempts(t *testing.T) {
	db, service, _ := newTicketChat(t, nil)
	for i := range 32 {
		ticketID := fmt.Sprintf("pending-%02d", i)
		receipt := models.WorkTicketTransfer{TicketID: ticketID, Generation: 3, ExecutionID: ticketID + "-current"}
		if err := db.Create(&models.WorkTicket{ID: ticketID, Transferred: true, Status: constants.WorkTicketStatusActive,
			Generation: 3, PendingExecutionID: receipt.ExecutionID}).Error; err != nil {
			t.Fatal(err)
		}
		for _, generation := range []int64{2, 3} {
			candidate := receipt
			candidate.Generation = generation
			attempt := models.WorkTicketAttempt{TicketID: ticketID, Generation: generation,
				TransferRequestID: fmt.Sprintf("handoff-%d", generation), Status: constants.WorkTicketAttemptStatusPending, Transfer: &candidate}
			if err := db.Create(&attempt).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var queries int
	var attemptCount int64
	var attemptSQL string
	var attemptArgs []any
	if err := db.Callback().Query().After("gorm:query").Register("test:transfer-query-count", func(result *gorm.DB) {
		queries++
		if result.Statement.Table == "work_ticket_attempts" {
			attemptSQL, attemptArgs = result.Statement.SQL.String(), result.Statement.Vars
			attemptCount = result.RowsAffected
		}
	}); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 32} {
		queries = 0
		launches, err := service.PendingTransfers(t.Context(), "", limit)
		if err != nil || len(launches) != limit || queries != 2 || attemptCount != int64(limit) {
			t.Fatalf("pending page size=%d launches=%d attempts=%d queries=%d error=%v", limit, len(launches), attemptCount, queries, err)
		}
		for i, launch := range launches {
			if launch.Ticket.ID != fmt.Sprintf("pending-%02d", i) || launch.Receipt.Generation != launch.Ticket.Generation ||
				launch.Receipt.ExecutionID != launch.Ticket.PendingExecutionID {
				t.Fatalf("page returned the wrong transfer generation in row %d: %+v", i, launch)
			}
		}
	}
	var plan []struct {
		// Detail names SQLite's plan step and chosen index.
		Detail string
	}
	if err := db.Raw("EXPLAIN QUERY PLAN "+attemptSQL, attemptArgs...).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	if len(plan) == 0 || !strings.Contains(plan[0].Detail, "SEARCH work_ticket_attempts USING INDEX") {
		t.Fatalf("batch attempts query does not seek by available index: %+v", plan)
	}
}

func TestTransferSurvivesDeletionOfUnadmittedRecipient(t *testing.T) {
	db, service, origin := newTicketChat(t, nil)
	ctx := t.Context()
	original := admit(t, service, ticketInput(origin, "before-deleted-recipient"))
	wakers := controllers.NewWakerController(db)
	if err := wakers.Create(ctx, models.Waker{ID: "deleted-recipient", Name: "Temporary recipient"}); err != nil {
		t.Fatal(err)
	}
	main := controllers.NewMainConversationController(db)
	receiver, err := main.Ensure(ctx, "waker", "deleted-recipient")
	if err != nil {
		t.Fatal(err)
	}
	source, err := main.ReserveWork(ctx, receiver.ID, "deleted-recipient", "handoff")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Transfer(ctx, workticket.TransferInput{Address: origin, TicketID: original.Ticket.ID, ExpectedGeneration: original.Generation,
		RequestID: "transfer-before-delete", OwnerKind: "waker", OwnerID: "deleted-recipient", OwnerWakerID: "deleted-recipient",
		WorkChatID: source.WorkChatID, Content: "Continue the ticket."})
	if err != nil {
		t.Fatal(err)
	}
	if err := controllers.NewChatController(db).Delete(ctx, source.WorkChatID); err != nil {
		t.Fatal(err)
	}
	if err := wakers.Delete(ctx, "deleted-recipient"); err != nil {
		t.Fatal(err)
	}
	pending, err := service.PendingTransfers(ctx, "", 10)
	if err != nil || len(pending) != 1 || pending[0].Receipt != receipt {
		t.Fatalf("deleting the recipient erased the original pending ticket or receipt: %+v %v", pending, err)
	}
	if blocked, err := service.BlockTransfer(ctx, pending[0], status_error.WakerNotFound); err != nil || !blocked {
		t.Fatalf("removed recipient was not marked blocked: %t %v", blocked, err)
	}
	if stopped, err := service.Stop(ctx, origin, original.Ticket.ID); err != nil || stopped.Status != constants.WorkTicketStatusStopped {
		t.Fatalf("original user could not stop the blocked transfer: %+v %v", stopped, err)
	}
}
