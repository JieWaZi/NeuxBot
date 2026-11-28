package execution

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/workticket"
)

// RecoverWorkTicketDeliveries 在 Chat 将无证据的活跃执行标为中断前，补交已持久完成的工单结果。
func RecoverWorkTicketDeliveries(ctx context.Context, storageRoot string, journal chatservice.Journal,
	threads *agent.ThreadLifecycle, tickets *workticket.Service, main MainConversationMessages,
) error {
	if tickets == nil {
		return nil
	}
	if err := tickets.RecoverOrdinaryReports(ctx, journal.ReadExecutionRecords,
		func(ctx context.Context, ticket models.WorkTicket, report models.WorkTicketReport) (string, error) {
			target, err := tickets.OrdinaryReportTarget(ctx, report.ExecutionID)
			if err != nil {
				return "", err
			}
			release, err := chatstorage.AcquireReferences(storageRoot, target.WorkChatID)
			if err != nil {
				return "", err
			}
			defer release()
			return deliverOrdinaryWorkReport(ctx, journal, target, ticket, report.ExecutionID, report.ID, report.Content)
		}); err != nil {
		return err
	}
	return tickets.RecoverUnfinishedInputs(ctx, func(ctx context.Context, binding workticket.Binding) error {
		err := recoverTicketExecution(ctx, storageRoot, journal, threads, tickets, main, binding)
		if unavailableRecoveryExecution(err) {
			logx.Warnf("read work ticket recovery evidence: execution_id=%s error=%v", binding.ExecutionID, err)
			return nil
		}
		return err
	})
}

// FinishRecoveredWorkTickets 对账 Chat 已标为中断的原执行，仅完成已确认接纳的工单回合。
func FinishRecoveredWorkTickets(ctx context.Context, journal chatservice.Journal, tickets *workticket.Service) error {
	if tickets == nil {
		return nil
	}
	return tickets.RecoverUnfinishedInputs(ctx, func(ctx context.Context, binding workticket.Binding) error {
		execution, err := journal.ReadExecution(ctx, binding.WorkChatID, binding.ExecutionID)
		if unavailableRecoveryExecution(err) {
			return nil
		}
		if err != nil || !workTicketTerminal(execution.Status) {
			return err
		}
		return tickets.FinishTurn(ctx, binding, execution.Status)
	})
}

func recoverTicketExecution(ctx context.Context, storageRoot string, journal chatservice.Journal, threads *agent.ThreadLifecycle, tickets *workticket.Service,
	main MainConversationMessages, binding workticket.Binding,
) error {
	ticket := binding.Ticket
	execution, err := journal.ReadExecution(ctx, binding.WorkChatID, binding.ExecutionID)
	if err != nil {
		return err
	}
	if execution.ID != binding.ExecutionID || execution.ChatID != binding.WorkChatID {
		return status_error.ChatLogCorrupted
	}
	if workTicketTerminal(execution.Status) {
		return tickets.FinishTurn(ctx, binding, execution.Status)
	}
	if ticket.CurrentExecutionID != binding.ExecutionID ||
		ticket.Generation != binding.Generation || ticket.Status == constants.WorkTicketStatusStopped {
		return nil
	}
	completed, answer, err := threads.RecoverCompletedAnswer(ctx, binding.WorkChatID, binding.ExecutionID)
	if err != nil || !completed {
		return err
	}
	event, err := tickets.EventDeliveryForExecution(ctx, binding.ExecutionID)
	if err != nil {
		return err
	}
	if event != nil {
		if err := tickets.MarkEventInputRead(ctx, binding.ExecutionID); err != nil {
			return err
		}
	}
	ctx, publish := events.HoldCommitEvents(ctx)
	defer publish()
	eventType := constants.ChatEventTypeExecutionCompleted
	finished := eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted}
	if execution.AutomaticRunID == "" && (ticket.OriginKind == constants.WorkTicketOriginMain || ticket.Transferred) {
		turn := workTicketTurn{tickets: tickets, main: main, binding: binding, journal: journal, storageRoot: storageRoot, event: event}
		deliver := turn.publishMainReport
		if ticket.OriginKind == constants.WorkTicketOriginChat {
			deliver = turn.deliverOrdinary
		}
		err := tickets.ReportNativeFinal(ctx, binding, func(context.Context) (string, error) {
			return answer, nil
		}, deliver)
		if err != nil {
			if !errors.Is(err, status_error.RuntimeExecutionFailed) {
				return err
			}
			eventType = constants.ChatEventTypeExecutionFailed
			finished = eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusFailed, ErrorCode: status_error.RuntimeExecutionFailed.Key(),
				FailureCode: constants.RuntimeFailureCodeEventDelivery, ErrorMessageSource: constants.ErrorMessageSourceLocalized}
		}
	}
	terminal, err := journal.CommitTerminal(ctx, chatservice.TerminalFact{Execution: execution,
		Type: eventType, Status: finished.Status, ErrorCode: finished.ErrorCode, Data: marshalData(finished)})
	if err != nil {
		return err
	}
	err = tickets.FinishTurn(ctx, binding, finished.Status)
	journal.PublishTerminal(terminal)
	return err
}

func workTicketTerminal(status constants.ExecutionStatus) bool {
	return status == constants.ExecutionStatusCompleted || status == constants.ExecutionStatusFailed || status == constants.ExecutionStatusCanceled
}

func unavailableRecoveryExecution(err error) bool {
	return util.HasStatusCode(err, status_error.ChatNotFound) || util.HasStatusCode(err, status_error.ExecutionNotFound) ||
		util.HasStatusCode(err, status_error.ChatLogError) || util.HasStatusCode(err, status_error.ChatLogCorrupted) ||
		util.HasStatusCode(err, status_error.ChatLogIndexCorrupted) || util.HasStatusCode(err, status_error.ChatLogNativeOutcomePending)
}
