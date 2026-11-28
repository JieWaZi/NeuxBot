package chat

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

func TestDeleteOrdinaryWorkOriginSettlesItsExactOutstandingReport(t *testing.T) {
	for _, scenario := range []struct {
		// name 标识原报告已知的收件状态或删除范围。
		name string
		// originalReport 模拟原会话的真实收件正文，空值表示未收件。
		originalReport string
		// deleteOwner 将本次删除扩展到原收件伙伴。
		deleteOwner bool
		// missingLog 模拟删除前已丢失原会话的权威日志。
		missingLog bool
		// transferred 表示 Worker 在独立来源生成原会话回件。
		transferred bool
		// deleteBeforeReport 表示原址在接手人首次登记报告前已完成删除。
		deleteBeforeReport bool
		// authoredReceipt 表示先按独立回件格式写入原址。
		authoredReceipt bool
		// conflictingAuthor 表示原址的收据作者与工单不符。
		conflictingAuthor bool
	}{
		{name: "undelivered report falls back after a canceled native turn"},
		{name: "committed receipt resolves lost acknowledgment", originalReport: "The requested result was verified."},
		{name: "conflicting receipt prevents deletion", originalReport: "Mismatched report receipt."},
		{name: "deleting the owner never redirects its reports", deleteOwner: true},
		{name: "missing original history does not authorize fallback", missingLog: true},
		{name: "transferred conclusion falls back to the original partner", transferred: true},
		{name: "transferred receipt in the original chat survives lost acknowledgment", transferred: true, originalReport: "The requested result was verified."},
		{name: "authored transferred receipt survives lost acknowledgment", transferred: true, originalReport: "The requested result was verified.", authoredReceipt: true},
		{name: "transferred receipt with the wrong author prevents deletion", transferred: true, originalReport: "The requested result was verified.", authoredReceipt: true, conflictingAuthor: true},
		{name: "transferred conclusion after original deletion still reaches the original partner", transferred: true, deleteBeforeReport: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := t.Context()
			db, chats, log := newDeleteTestService(t)
			const owner = "report-owner"
			const receiver = "another-partner"
			const chatID = "aaa00000000000000000000000000001"
			const inputID = "aaa00000000000000000000000000002"
			const executionID = "aaa00000000000000000000000000003"
			const reportID = "aaa00000000000000000000000000004"
			const content = "The requested result was verified."
			wakers := controllers.NewWakerController(db)
			for _, waker := range []string{owner, receiver} {
				if err := wakers.Create(ctx, models.Waker{ID: waker, Name: waker}); err != nil {
					t.Fatal(err)
				}
			}
			main := mainconversation.New(controllers.NewMainConversationController(db), chats.queryIndexes.transactionController, wakers, controllers.NewChannelController(db))
			other, err := main.Ensure(ctx, "waker", receiver)
			if err != nil {
				t.Fatal(err)
			}
			workdir, err := chats.storage.AgentWorkspace(owner)
			if err != nil {
				t.Fatal(err)
			}
			if err := workspace.EnsureAgentWorkspace(workdir, owner); err != nil {
				t.Fatal(err)
			}
			facts := []eventlog.AppendInput{
				{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: owner, WorkspaceDirectory: workdir, Title: "Original ordinary chat"})},
				{Type: constants.ChatEventTypeUserMessageCreated, MessageID: inputID, Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: "Inspect the report"})},
				{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, MessageID: executionID, Data: marshalData(eventlog.ExecutionCreatedData{UserMessageID: inputID})},
			}
			if scenario.originalReport != "" && !scenario.transferred {
				facts = append(facts, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionDelivery, ExecutionID: executionID, MessageID: executionID,
					Data: marshalData(eventlog.ExecutionDeliveryData{SourceID: reportID, Summary: scenario.originalReport, WorkTicketReport: true})})
			}
			facts = append(facts, eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCanceled, ExecutionID: executionID, MessageID: executionID,
				Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCanceled})})
			records, err := log.Create(chatID, facts)
			if err != nil {
				t.Fatal(err)
			}
			if err := chats.queryIndexes.chatController.Create(ctx, models.Chat{ID: chatID, WakerID: owner, WorkspaceDirectory: workdir, LatestSequence: records[len(records)-1].Sequence}); err != nil {
				t.Fatal(err)
			}
			store := controllers.NewWorkTicketController(db)
			tickets := workticket.New(store, chats.queryIndexes.transactionController, nil, main.PublishDeletedOriginReport)
			chats.preserveOrdinaryOriginReports = func(ctx context.Context, origins []models.Chat, read func(string, string) ([]eventlog.Record, error)) (func(context.Context) error, error) {
				return tickets.PrepareOrdinaryOriginDeletion(ctx, origins, read)
			}
			binding, err := tickets.PrepareAdmission(ctx, workticket.Admission{
				Address: workticket.Address{Kind: constants.WorkTicketOriginChat, ID: chatID}, WorkChatID: chatID,
				ExecutionID: executionID, MessageID: inputID, SourceMessageID: inputID, SubmissionID: "submit", SubmissionHash: "scope-digest",
				OwnerKind: "waker", OwnerID: owner, OwnerWakerID: owner})
			if err != nil {
				t.Fatal(err)
			}
			if err = tickets.ConfirmAdmission(ctx, binding); err != nil {
				t.Fatal(err)
			}
			author := owner
			if scenario.transferred {
				author = receiver
				source, err := main.ReserveWork(ctx, "waker", receiver, receiver, "handoff")
				if err != nil {
					t.Fatal(err)
				}
				change, err := tickets.Transfer(ctx, workticket.TransferInput{
					Address:  workticket.Address{Kind: constants.WorkTicketOriginChat, ID: chatID},
					TicketID: inputID, ExpectedGeneration: binding.Generation, RequestID: "handoff",
					OwnerKind: "waker", OwnerID: receiver, OwnerWakerID: receiver, WorkChatID: source.WorkChatID, Content: "Finish the report",
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := chats.queryIndexes.chatController.Create(ctx, models.Chat{ID: source.WorkChatID, WakerID: receiver, HiddenFromWakerHistory: true}); err != nil {
					t.Fatal(err)
				}
				binding, err = tickets.PrepareAdmission(ctx, workticket.Admission{
					Address: workticket.Address{Kind: constants.WorkTicketOriginChat, ID: chatID}, TicketID: inputID,
					MessageID: change.ExecutionID, SourceMessageID: "receiver-input", WorkChatID: source.WorkChatID, ExecutionID: change.ExecutionID,
					SubmissionID: change.ExecutionID, SubmissionHash: "handoff-digest", OwnerKind: "waker", OwnerID: receiver,
					OwnerWakerID: receiver, PermissionLevel: constants.PermissionLevelDefault,
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := tickets.ConfirmAdmission(ctx, binding); err != nil {
					t.Fatal(err)
				}
			}
			activeReportID := reportID
			originalMessageID := executionID
			if scenario.deleteBeforeReport {
				if err = chats.DeleteChatsAtomically(ctx, []string{chatID}, func(context.Context) error {
					return status_error.BadRequest
				}); !errors.Is(err, status_error.BadRequest) {
					t.Fatalf("expected deletion transaction to roll back: %v", err)
				}
				if err := chats.DeleteChat(ctx, chatID); err != nil {
					t.Fatal(err)
				}
				tickets = workticket.New(store, chats.queryIndexes.transactionController, nil, main.PublishDeletedOriginReport)
			}
			if scenario.transferred {
				cut := errors.New("publication interrupted after reservation")
				receipt, err := tickets.Report(ctx, binding, workticket.ReportInput{RequestID: "done", Kind: "completed", Content: content},
					func(_ context.Context, _ models.WorkTicket, id string) (string, error) {
						if scenario.deleteBeforeReport {
							t.Fatal("delivery attempted to write a definitively deleted conversation")
						}
						activeReportID = id
						if scenario.originalReport != "" {
							data := eventlog.ExecutionDeliveryData{SourceID: id, Summary: scenario.originalReport, WorkTicketReport: true, WorkTicketExecutionID: binding.ExecutionID}
							if scenario.authoredReceipt {
								originalMessageID = id
								data.SourceChatID, data.SourceWakerID = binding.WorkChatID, receiver
								if scenario.conflictingAuthor {
									data.SourceWakerID = owner
								}
							}
							if _, err := log.Append(chatID, []eventlog.AppendInput{
								{Type: constants.ChatEventTypeExecutionDelivery, ExecutionID: executionID, MessageID: originalMessageID, Data: marshalData(data)},
							}); err != nil {
								t.Fatal(err)
							}
						}
						return "", cut
					})
				if scenario.deleteBeforeReport {
					if err != nil || receipt.ID == "" || receipt.MessageID != receipt.ID {
						t.Fatalf("report did not reach the original partner after deletion: %+v %v", receipt, err)
					}
					activeReportID = receipt.ID
				} else if !errors.Is(err, cut) {
					t.Fatalf("expected a persisted conclusion with missing ACK: %v", err)
				}
			} else {
				err = store.CreateReport(ctx, models.WorkTicketReport{ID: activeReportID, TicketID: inputID, ExecutionID: executionID,
					Status: constants.WorkTicketStatusCompleted, Content: content})
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario.deleteOwner {
				if err := chats.DeleteWakerChats(ctx, owner, nil, func([]string) (func() error, error) { return nil, nil }, func(tx context.Context) error {
					return wakers.Delete(tx, owner)
				}); err != nil {
					t.Fatal(err)
				}
				if got, err := store.Report(ctx, reportID); err != nil || got.ID != "" {
					t.Fatalf("owner cascade retained an outstanding report: %+v %v", got, err)
				}
				if _, err := main.Ensure(ctx, "waker", owner); !hasStatusErrorCode(err, status_error.WakerNotFound.Code()) {
					t.Fatalf("owner cascade retained or recreated the original owner: %v", err)
				}
				return
			}
			if scenario.conflictingAuthor || (scenario.originalReport != "" && scenario.originalReport != content) {
				if err := chats.DeleteChat(ctx, chatID); !hasStatusErrorCode(err, status_error.ChatLogCorrupted.Code()) {
					t.Fatalf("unverified original evidence did not stop deletion: %v", err)
				}
				if _, err := log.ReadAll(chatID); err != nil {
					t.Fatalf("deletion discarded the conflicting evidence: %v", err)
				}
				if got, err := store.Report(ctx, activeReportID); err != nil || got.ID != activeReportID || got.MessageID != "" {
					t.Fatalf("unproven report was redirected or deleted: %+v %v", got, err)
				}
				return
			}
			if scenario.missingLog {
				path, err := log.LogPath(chatID)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := chats.DeleteChat(ctx, chatID); err == nil {
					t.Fatal("missing evidence allowed clearing a still-pending completion")
				}
				if got, err := store.Report(ctx, reportID); err != nil || got.ID != reportID || got.MessageID != "" {
					t.Fatalf("missing evidence advanced or removed the intended report: %+v %v", got, err)
				}
				return
			}
			if !scenario.deleteBeforeReport {
				if err = chats.DeleteChatsAtomically(ctx, []string{chatID}, func(context.Context) error {
					return status_error.BadRequest
				}); !errors.Is(err, status_error.BadRequest) {
					t.Fatalf("expected deletion transaction to roll back: %v", err)
				}
				if result, err := store.Report(ctx, activeReportID); err != nil || result.MessageID != "" {
					t.Fatalf("deletion rollback settled the report: %+v %v", result, err)
				}
			}
			for range 2 {
				if err := chats.DeleteChat(ctx, chatID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := log.ReadAll(chatID); !hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
				t.Fatalf("the hard-deleted original still exists: %v", err)
			}
			got, err := store.Report(ctx, activeReportID)
			if err != nil || got.ID != activeReportID || got.MessageID == "" {
				t.Fatalf("hard deletion lost the report receipt: %+v %v", got, err)
			}
			conv, err := main.Ensure(ctx, "waker", owner)
			if err != nil {
				t.Fatal(err)
			}
			rows, _, err := main.Messages(ctx, conv.ID, 0, 10)
			if scenario.originalReport != "" {
				if err != nil || len(rows) != 0 || got.MessageID != originalMessageID || got.DeliveryConversationID != chatID {
					t.Fatalf("an already-received result was redirected: %+v %+v %v", rows, got, err)
				}
			} else if err != nil || len(rows) != 1 || rows[0].ID != got.MessageID || rows[0].Content != content || rows[0].ReplyToMessageID != "" || rows[0].SourceWakerID != author {
				t.Fatalf("completed report did not fall back once without a cross-chat reply: %+v %+v %v", rows, got, err)
			} else if got.DeliveryConversationID != conv.ID || rows[0].OriginalWork == nil || *rows[0].OriginalWork != (models.MainConversationOriginalWork{ChatID: chatID, TicketID: inputID, Title: "Original ordinary chat"}) {
				t.Fatalf("falling back lost the original conversation reference: %+v %+v", rows[0], got)
			} else if scenario.transferred && (!rows[0].SourceReadOnly || rows[0].SourceChatID != binding.WorkChatID || rows[0].SourceExecutionID != binding.ExecutionID) {
				t.Fatalf("falling back lost the receiver's read-only work link: %+v", rows[0])
			}
			if rows, _, err = main.Messages(ctx, other.ID, 0, 10); err != nil || len(rows) != 0 {
				t.Fatalf("fallback escaped the original partner: %+v %v", rows, err)
			}
			if scenario.transferred {
				return
			}
			if err := wakers.Delete(ctx, owner); err != nil {
				t.Fatal(err)
			}
			if got, err := store.Report(ctx, reportID); err != nil || got.ID != "" {
				t.Fatalf("deleting the original partner retained the detached report: %+v %v", got, err)
			}
		})
	}
}
