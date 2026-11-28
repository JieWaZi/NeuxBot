package execution

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// RecoverDefaultMainWork settles the existing Chat turn using its native result and own publication receipt.
func RecoverDefaultMainWork(ctx context.Context, journal chatservice.Journal, threads *agent.ThreadLifecycle,
	store *controllers.MainConversationController, main MainConversationMessages, scope string,
) error {
	const batch = 100
	for after := ""; ; {
		sources, err := store.ActiveScopedSources(ctx, scope, after, batch)
		if err != nil {
			return err
		}
		for _, source := range sources {
			after = source.ID
			err := recoverDefaultMainTurn(ctx, journal, threads, store, main, source)
			if unavailableRecoveryExecution(err) {
				logx.Warnf("read main conversation recovery evidence: chat_id=%s execution_id=%s error=%v", source.ID, source.ActiveExecutionID, err)
				continue
			}
			if err != nil {
				return err
			}
		}
		if len(sources) < batch {
			return nil
		}
	}
}

func recoverDefaultMainTurn(ctx context.Context, journal chatservice.Journal, threads *agent.ThreadLifecycle,
	store *controllers.MainConversationController, main MainConversationMessages, source models.Chat,
) error {
	row, err := journal.ReadExecution(ctx, source.ID, source.ActiveExecutionID)
	if err != nil || workTicketTerminal(row.Status) {
		return err
	}
	complete, answer, err := threads.RecoverCompletedAnswer(ctx, source.ID, row.ID)
	if err != nil || !complete {
		return err
	}
	work, err := store.Work(ctx, source.ID)
	if err != nil {
		return err
	}
	origin, found, err := store.FindMessage(ctx, work.ConversationID, row.ID)
	if err != nil {
		return err
	}
	if found && (origin.Role != string(constants.MessageRoleUser) || origin.SourceChatID != source.ID || origin.SourceExecutionID != row.ID) {
		return status_error.ChatLogCorrupted
	}
	ctx, release := events.HoldCommitEvents(ctx)
	defer release()
	eventType := constants.ChatEventTypeExecutionCompleted
	finished := eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted}
	if found {
		posts, err := store.WorkPublications(ctx, work, []string{row.ID})
		if err != nil {
			return err
		}
		if len(posts) == 0 {
			if answer == "" {
				eventType = constants.ChatEventTypeExecutionFailed
				finished = eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed, ErrorCode: status_error.RuntimeExecutionFailed.Key(),
					FailureCode: constants.RuntimeFailureCodeEventDelivery, ErrorMessageSource: constants.ErrorMessageSourceLocalized}
			} else if _, _, err := main.PublishWithReceipt(ctx, source.ID, row.ID, "\x00final-answer", answer, origin.ID); err != nil {
				return err
			}
		}
	}
	terminal, err := journal.CommitTerminal(ctx, chatservice.TerminalFact{Execution: row,
		Type: eventType, Status: finished.Status, ErrorCode: finished.ErrorCode, Data: marshalData(finished)})
	if err == nil {
		journal.PublishTerminal(terminal)
	}
	return err
}
