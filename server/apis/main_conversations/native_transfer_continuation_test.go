package main_conversations

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/apis/work_tickets"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
)

func TestOrdinaryChatContinuesTransferredTicketFromOriginalAndReportQuotes(t *testing.T) {
	configureNativeMainConversationAt(t, t.TempDir(), "", true)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const originalOwner = "native-main-partner"
	const targetOwner = "native-transfer-recipient"
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: targetOwner, Name: "Recipient"}); err != nil {
		t.Fatal(err)
	}
	original, err := global.ExecutionService.Start(ctx, execution.WorkIntent{
		ClientRequestID: "ordinary-transfer-first", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: originalOwner}},
		Input:   execution.WorkInput{Actor: "user", Content: "Inspect the harmless fixture."},
		Options: execution.WorkOptions{EntryPoint: execution.WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
		Ticket:  execution.WorkTicketInput{Start: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(check func() bool) {
		t.Helper()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatal("work did not settle")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	originID, ticketID := original.Chat.ID, original.Execution.UserMessageID
	address := workticket.Address{Kind: constants.WorkTicketOriginChat, ID: originID}
	readTicket := func() models.WorkTicket {
		t.Helper()
		ticket, found, err := global.WorkTicketService.ForMessage(ctx, address, ticketID)
		if err != nil || !found {
			t.Fatalf("original work ticket missing: %+v, %v", ticket, err)
		}
		return ticket
	}
	wait(func() bool { return !global.ExecutionService.IsExecutionActive(originID, original.Execution.ID) })
	request, _ := gin.CreateTestContext(httptest.NewRecorder())
	request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	rawReceipt, err := (&work_tickets.TransferWorkTicket{TicketID: ticketID, Body: work_tickets.TransferWorkTicketRequest{
		OriginKind: string(address.Kind), OriginID: address.ID, TargetKind: "waker", TargetID: targetOwner,
		ExpectedGeneration: readTicket().Generation, RequestID: "ordinary-transfer-handoff", Content: "fixture explicit completed",
	}}).Output(request)
	if err != nil {
		t.Fatal(err)
	}
	receipt := rawReceipt.(work_tickets.TransferWorkTicketResponse)
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		global.RunWorkTicketCollaboration(workerCtx, nil, global.ExecutionService)
	}()
	defer func() { stop(); <-done }()
	wait(func() bool {
		ticket := readTicket()
		return ticket.CurrentExecutionID == receipt.ExecutionID && ticket.Status == constants.WorkTicketStatusCompleted &&
			!global.ExecutionService.IsExecutionActive(ticket.WorkChatID, receipt.ExecutionID)
	})
	transferred := readTicket()
	failed := &failingOriginReply{Module: global.ChatService}
	global.ChatService = failed
	t.Cleanup(func() { global.ChatService = failed.Module })
	send := func(submission, reply string) string {
		t.Helper()
		op := &chats.SendMessage{ChatID: originID, Body: chats.SendMessageRequest{
			SubmissionID: submission, ReplyToMessageID: reply, MessageRequest: chats.MessageRequest{
				Text: "fixture explicit completed", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto,
			},
		}}
		value, err := op.Output(request)
		if err != nil {
			t.Fatalf("follow-up in original ordinary chat was rejected: %v", err)
		}
		accepted := value.(execution.AcceptedExecution)
		if !accepted.Published || accepted.Chat.ID != originID || accepted.Execution.ID != "" {
			t.Fatalf("follow-up exposed a private execution or lost the original chat: %+v", accepted)
		}
		var current models.WorkTicket
		wait(func() bool {
			current = readTicket()
			return current.Status == constants.WorkTicketStatusCompleted && !global.ExecutionService.IsExecutionActive(current.WorkChatID, current.CurrentExecutionID)
		})
		workerInput, err := global.ChatService.ReadSubmission(ctx, transferred.WorkChatID, submission)
		if err != nil || workerInput == nil || workerInput.ID != current.CurrentExecutionID || current.OwnerWakerID != targetOwner {
			t.Fatalf("follow-up left the current owner's work: %+v, %+v, %v", current, workerInput, err)
		}
		replayed, err := op.Output(request)
		if err != nil || !replayed.(execution.AcceptedExecution).Replayed || readTicket().CurrentExecutionID != current.CurrentExecutionID {
			t.Fatalf("follow-up replay changed execution: %+v, %v", replayed, err)
		}
		op.Body.Text = "same submission with different content"
		if _, err := op.Output(request); err == nil {
			t.Fatal("a different input reused the accepted submission")
		}
		op.Body.Text, op.Body.ReplyToMessageID = "fixture explicit completed", ""
		if _, err := op.Output(request); !util.HasStatusCode(err, status_error.CreateRequestConflict) {
			t.Fatalf("changing reply omitted the original same-key check: %v", err)
		}
		return current.CurrentExecutionID
	}
	first := send("ordinary-follow-original", ticketID)
	var reportID string
	wait(func() bool {
		history, err := global.ChatService.ReadMessageTimeline(ctx, originID, 0, 0, 40)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range history.Resources {
			if row.Actor == constants.WorkTicketReportActor && row.SourceWakerID == targetOwner {
				reportID = row.ID
				return true
			}
		}
		return false
	})
	second := send("ordinary-follow-report", reportID)
	if second == first || readTicket().CurrentExecutionID != second {
		t.Fatalf("a distinct reply did not advance the same work ticket: %+v", readTicket())
	}
	if failed.calls.Load() == 0 {
		t.Fatal("did not exercise origin publication failure")
	}
	unsupported := &chats.SendMessage{ChatID: originID, Body: chats.SendMessageRequest{
		SubmissionID: "ordinary-follow-file", ReplyToMessageID: reportID, MessageRequest: chats.MessageRequest{
			Text: "Attached a file", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto,
			AttachmentOnly: true, Attachments: []chats.MessageAttachment{{SourcePath: "upload:unclaimed"}},
		},
	}}
	if _, err := unsupported.Output(request); !util.HasStatusCode(err, status_error.MessageAttachmentInvalid) {
		t.Fatalf("unsupported forwarded file was accepted: %v", err)
	}
	if receipt, err := global.ChatService.ReadSubmission(ctx, transferred.WorkChatID, "ordinary-follow-file"); err != nil || receipt != nil {
		t.Fatalf("forwarded file reserved private execution: %+v %v", receipt, err)
	}
	ordinary := &chats.SendMessage{ChatID: originID, Body: chats.SendMessageRequest{
		SubmissionID: "normal-before-quote", MessageRequest: chats.MessageRequest{
			Text: "Inspect the harmless fixture.", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto,
		},
	}}
	if value, err := ordinary.Output(request); err != nil || value.(execution.AcceptedExecution).Execution.ID == "" {
		t.Fatalf("ordinary non-ticket execution failed to reserve same namespace: %+v %v", value, err)
	}
	ordinary.Body.ReplyToMessageID = reportID
	if _, err := ordinary.Output(request); !util.HasStatusCode(err, status_error.CreateRequestConflict) {
		t.Fatalf("switching normal accepted key to forwarded quote created another action: %v", err)
	}
	if err := global.WakerControllerManager.UpdateDefaultModel(ctx, targetOwner, &models.WakerDefaultModel{RuntimeID: "missing-runtime"}, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	op := &chats.SendMessage{ChatID: originID, Body: chats.SendMessageRequest{
		SubmissionID: "ordinary-follow-report", ReplyToMessageID: reportID, MessageRequest: chats.MessageRequest{
			Text: "fixture explicit completed", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto,
		},
	}}
	value, err := op.Output(request)
	if err != nil || !value.(execution.AcceptedExecution).Replayed {
		t.Fatalf("accepted original submission could not replay after runtime removal: %+v %v", value, err)
	}
	history, err := global.ChatService.ReadMessageTimeline(ctx, originID, 0, 0, 40)
	if err != nil {
		t.Fatal(err)
	}
	var quotes []string
	notices := 0
	for _, row := range history.Resources {
		if row.Role == constants.MessageRoleUser && row.ReplyTo != nil {
			quotes = append(quotes, row.ReplyTo.MessageID)
		}
		if row.Actor == constants.WorkTicketHandoffActor && row.Content == "Transferred to Recipient." {
			notices++
		}
	}
	if len(quotes) != 2 || quotes[0] != ticketID || quotes[1] != reportID {
		t.Fatalf("expected two independently quoted user messages in the original chat: %v", quotes)
	}
	if notices != 1 {
		t.Fatalf("expected one handoff notice in the original chat, got %d", notices)
	}
	if err := global.WakerControllerManager.UpdateDefaultModel(ctx, targetOwner, nil, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	keepOpen := *op
	keepOpen.Body.SubmissionID, keepOpen.Body.Text = "reopen-for-second-transfer", "fixture explicit blocked"
	if _, err := keepOpen.Output(request); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		ticket := readTicket()
		return ticket.Status == constants.WorkTicketStatusBlocked && !global.ExecutionService.IsExecutionActive(ticket.WorkChatID, ticket.CurrentExecutionID)
	})
	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "third-recipient", Name: "Third recipient"}); err != nil {
		t.Fatal(err)
	}
	if _, err := global.TransferWorkTicket(ctx, workticket.TransferInput{Address: address, TicketID: ticketID,
		ExpectedGeneration: readTicket().Generation, RequestID: "handoff-to-third", OwnerKind: "waker", OwnerID: "third-recipient", Content: "Continue."}); err != nil {
		t.Fatal(err)
	}
	value, err = op.Output(request)
	if err != nil || !value.(execution.AcceptedExecution).Replayed {
		t.Fatalf("second transfer invalidated the prior accepted reply: %+v %v", value, err)
	}
}

type failingOriginReply struct {
	// Module 委托所有工作来源和聊天读取。
	chatservice.Module
	// calls 仅统计独立的原会话用户消息投递。
	calls atomic.Int32
}

func (s *failingOriginReply) AppendExecutionFacts(ctx context.Context, chatID string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
	if len(facts) == 1 && facts[0].Type == constants.ChatEventTypeUserMessageCreated && facts[0].ExecutionID == "" && s.calls.Add(1) == 1 {
		return nil, status_error.ChatLogError
	}
	return s.Module.AppendExecutionFacts(ctx, chatID, facts)
}
