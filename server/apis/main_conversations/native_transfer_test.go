package main_conversations

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/work_tickets"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
	"github.com/gin-gonic/gin"
)

func TestWorkTicketTransferThroughNativeWorker(t *testing.T) {
	for _, scenario := range []struct {
		// name 标识不同回投位置的子用例。
		name string
		// originKind 选择最初接纳交办的聊天类型。
		originKind constants.WorkTicketOrigin
		// deleteBeforeStart 模拟接手派发之前原普通聊天被删除。
		deleteBeforeStart bool
	}{
		{name: "chat", originKind: constants.WorkTicketOriginChat},
		{name: "main", originKind: constants.WorkTicketOriginMain},
		{name: "deleted_chat", originKind: constants.WorkTicketOriginChat, deleteBeforeStart: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			originKind := scenario.originKind
			root := t.TempDir()
			configureNativeMainConversationAt(t, root, "", true)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			const originalOwner = "native-main-partner"
			const targetOwner = "native-transfer-receiver"
			if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: targetOwner, Name: "Receiver"}); err != nil {
				t.Fatal(err)
			}
			request, _ := gin.CreateTestContext(httptest.NewRecorder())
			request.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
			var originID, ticketID, oldWorkChatID, oldExecutionID string
			if originKind == constants.WorkTicketOriginChat {
				accepted, err := global.ExecutionService.Start(ctx, execution.WorkIntent{
					ClientRequestID: "native-before-transfer", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: originalOwner}},
					Input: execution.WorkInput{Actor: "user", Content: "Inspect the harmless fixture."},
					Options: execution.WorkOptions{EntryPoint: execution.WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex,
						PermissionLevel: constants.PermissionLevelAuto}, Ticket: execution.WorkTicketInput{Start: true},
				})
				if err != nil {
					t.Fatal(err)
				}
				originID, ticketID, oldWorkChatID, oldExecutionID = accepted.Chat.ID, accepted.Execution.UserMessageID, accepted.Chat.ID, accepted.Execution.ID
			} else {
				raw, err := (&SendMainConversationMessage{SubjectType: "waker", SubjectID: originalOwner,
					Body: MainConversationInput{SubmissionID: "native-before-transfer", Content: "Inspect the harmless fixture.", StartWork: true}}).Output(request)
				if err != nil {
					t.Fatal(err)
				}
				accepted := raw.(MainConversationAcceptance)
				originID, ticketID, oldWorkChatID, oldExecutionID = accepted.Message.ConversationID, accepted.Message.ID, accepted.Message.SourceChatID, accepted.ExecutionID
			}
			address := workticket.Address{Kind: originKind, ID: originID}
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			wait := func(check func() bool) {
				t.Helper()
				for !check() {
					select {
					case <-ctx.Done():
						t.Fatal("native transfer failed to settle")
					case <-tick.C:
					}
				}
			}
			wait(func() bool { return !global.ExecutionService.IsExecutionActive(oldWorkChatID, oldExecutionID) })
			before, found, err := global.WorkTicketService.ForMessage(ctx, address, ticketID)
			if err != nil || !found || before.ID != ticketID || before.Generation != 1 {
				t.Fatalf("original ticket was not admitted: %+v, %t, %v", before, found, err)
			}
			command := &work_tickets.TransferWorkTicket{TicketID: ticketID, Body: work_tickets.TransferWorkTicketRequest{
				OriginKind: string(originKind), OriginID: originID, TargetKind: "waker", TargetID: targetOwner, ExpectedGeneration: before.Generation,
				RequestID: "real-native-handoff", Content: "fixture explicit completed",
			}}
			first, err := command.Output(request)
			if err != nil {
				t.Fatal(err)
			}
			receipt := first.(work_tickets.TransferWorkTicketResponse)
			second, err := command.Output(request)
			if err != nil || second != first || receipt.TicketID != ticketID || receipt.Generation != before.Generation+1 {
				t.Fatalf("transfer changed ticket identity or bounced on replay: %+v, %+v, %v", first, second, err)
			}
			if scenario.deleteBeforeStart {
				if err := global.ChatService.DeleteChat(ctx, originID); err != nil {
					t.Fatal(err)
				}
			}
			// The collaboration worker starts after the transfer committed, as it would after a Core restart.
			workerCtx, stopWorker := context.WithCancel(ctx)
			workerDone := make(chan struct{})
			go func() {
				defer close(workerDone)
				global.RunWorkTicketCollaboration(workerCtx, nil, global.ExecutionService)
			}()
			defer func() { stopWorker(); <-workerDone }()
			var result models.WorkTicket
			wait(func() bool {
				result, found, err = global.WorkTicketService.ForMessage(ctx, address, ticketID)
				if err != nil {
					t.Fatal(err)
				}
				return found && result.Status == constants.WorkTicketStatusCompleted && !global.ExecutionService.IsExecutionActive(result.WorkChatID, receipt.ExecutionID)
			})
			if result.OriginID != originID || result.WorkChatID == oldWorkChatID || result.OwnerWakerID != targetOwner || result.Generation != receipt.Generation || result.Summary != "Explicit completed report." {
				t.Fatalf("unexpected settled transfer: %+v", result)
			}
			newRun, err := global.ChatService.ReadExecution(ctx, result.WorkChatID, receipt.ExecutionID)
			if err != nil || newRun.UserMessageID == newRun.ID || newRun.Status != constants.ExecutionStatusCompleted {
				t.Fatalf("receiver did not use its own normal execution: %+v, %v", newRun, err)
			}
			if scenario.deleteBeforeStart {
				fallback, err := global.MainConversationService.Ensure(ctx, "waker", originalOwner)
				if err != nil {
					t.Fatal(err)
				}
				wait(func() bool {
					messages, _, err := global.MainConversationService.Messages(ctx, fallback.ID, 0, 10)
					if err != nil {
						t.Fatal(err)
					}
					if len(messages) == 0 {
						return false
					}
					if len(messages) != 1 || messages[0].Content != result.Summary || messages[0].SourceWakerID != targetOwner || messages[0].SourceChatID != result.WorkChatID || messages[0].SourceExecutionID != receipt.ExecutionID {
						t.Fatalf("deleted origin did not receive one report with B's authentic source: %+v", messages)
					}
					return true
				})
			} else if originKind == constants.WorkTicketOriginChat {
				records, err := global.ChatService.ReadExecutionRecords(originID, oldExecutionID)
				if err != nil {
					t.Fatal(err)
				}
				reports, err := workticket.OrdinaryWorkReportReceipts(records, oldExecutionID)
				if err != nil || len(reports) != 1 || reports[0].Content != result.Summary || reports[0].SourceExecutionID != receipt.ExecutionID {
					t.Fatalf("expected exactly one receiver result in the original Chat: %+v, %v", reports, err)
				}
				history, err := global.ChatService.ReadMessageTimeline(ctx, originID, 0, 0, 20)
				if err != nil {
					t.Fatalf("transferred report was not a separate timeline message: %+v, %v", history.Resources, err)
				}
				var matches int
				for _, message := range history.Resources {
					if message.ID == reports[0].ID && message.SourceWakerID == targetOwner && message.SourceChatID == result.WorkChatID && message.Content == result.Summary {
						matches++
					}
					if message.ID == oldExecutionID && strings.Contains(message.Content, result.Summary) {
						t.Fatalf("transferred report leaked into original assistant bubble: %+v", message)
					}
				}
				if matches != 1 {
					t.Fatalf("transferred report has incorrect author, source or count: %+v", history.Resources)
				}
			} else {
				messages, _, err := global.MainConversationService.Messages(ctx, originID, 0, 10)
				var report models.MainConversationMessage
				var matches int
				for _, message := range messages {
					if message.Content == result.Summary && message.ReplyToMessageID == ticketID && message.SourceReadOnly {
						report = message
						matches++
					}
				}
				if err != nil || matches != 1 || report.SourceWakerID != targetOwner || report.SourceChatID != result.WorkChatID || report.SourceExecutionID != receipt.ExecutionID {
					t.Fatalf("expected one readable receiver result with the original reply address: %+v, %v", messages, err)
				}
				data, err := eventlog.MarshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "Private content from another turn."})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = global.ChatService.AppendExecutionFacts(ctx, result.WorkChatID, []eventlog.AppendInput{
					{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "unrelated-turn-input", Data: data},
				}); err != nil {
					t.Fatal(err)
				}
				selector := GetMainConversationSource{SubjectType: "waker", SubjectID: originalOwner, WorkChatID: result.WorkChatID, PublicationID: report.ID}
				rawSource, err := selector.Output(request)
				if err != nil {
					t.Fatal(err)
				}
				source := rawSource.(MainConversationSourceResponse)
				if source.Chat.WakerID != targetOwner || source.Chat.ID != result.WorkChatID || source.Chat.WorkspaceDirectory != "" ||
					source.HasMore || source.PrivateContinuation || len(source.InputTriggers) != 0 ||
					len(source.Resources) != 2 || source.Resources[0].ID != newRun.UserMessageID ||
					source.Resources[1].ID != receipt.ExecutionID || source.Resources[1].Fork != nil ||
					len(source.PublishedMessages) != 1 || source.PublishedMessages[0].ID != report.ID {
					t.Fatalf("report did not open only its actual execution in read-only mode: %+v", source)
				}
				unbound := selector
				unbound.PublicationID = ""
				wrongPublication := selector
				wrongPublication.PublicationID = messages[1].ID
				wrongSource := selector
				wrongSource.WorkChatID = oldWorkChatID
				wrongSubject := selector
				wrongSubject.SubjectID = targetOwner
				for _, unauthorized := range []GetMainConversationSource{unbound, wrongPublication, wrongSource, wrongSubject} {
					if _, err := unauthorized.Output(request); !util.HasStatusCode(err, status_error.ChatNotFound) {
						t.Fatalf("unverified source read %+v: %v", unauthorized, err)
					}
				}
				if _, _, err := sourceChat(ctx, "waker", originalOwner, result.WorkChatID); !util.HasStatusCode(err, status_error.ChatNotFound) {
					t.Fatalf("report broadened the original subject's normal source access: %v", err)
				}
				if err := global.MainConversationService.RemoveMessage(ctx, "waker", originalOwner, report.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := selector.Output(request); !util.HasStatusCode(err, status_error.ChatNotFound) {
					t.Fatalf("removed public report retained access to the receiver's execution: %v", err)
				}
			}
			target, err := global.MainConversationService.Ensure(ctx, "waker", targetOwner)
			if err != nil {
				t.Fatal(err)
			}
			messages, _, err := global.MainConversationService.Messages(ctx, target.ID, 0, 10)
			if err != nil || len(messages) != 0 {
				t.Fatalf("transfer leaked into the receiver's public main conversation: %+v, %v", messages, err)
			}
			starts, err := os.ReadFile(filepath.Join(root, "worker-starts"))
			if err != nil || string(starts) != "worker\nworker\n" {
				t.Fatalf("request replay relaunched a worker: %q, %v", starts, err)
			}
		})
	}
}
