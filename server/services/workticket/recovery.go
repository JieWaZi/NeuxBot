package workticket

import (
	"context"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const recoveryPageSize = 100

// RecoverOrdinaryReports 核对普通 Chat 原结论的待投行和权威收件记录，只补交已接纳的结案报告。
func (s *Service) RecoverOrdinaryReports(ctx context.Context,
	read func(string, string) ([]eventlog.Record, error), deliver func(context.Context, models.WorkTicket, models.WorkTicketReport) (string, error),
) error {
	var result error
	for after := ""; ; {
		rows, err := s.store.PendingReports(ctx, after, recoveryPageSize)
		if err != nil {
			return errors.Join(result, err)
		}
		for _, row := range rows {
			after = row.ID
			if err := ctx.Err(); err != nil {
				return errors.Join(result, err)
			}
			err := s.recoverOrdinaryReport(ctx, row, read, deliver)
			if reportRecoverySourceUnavailable(err) || util.HasStatusCode(err, status_error.WorkTicketAttemptUnavailable) {
				// 没有积极的原址删除或转交凭据，不能因为原始记录暂不可读而改址。
				logx.Warnf("read ordinary work report recovery evidence: report_id=%s error=%v", row.ID, err)
				continue
			}
			result = errors.Join(result, err)
		}
		if len(rows) < recoveryPageSize {
			return result
		}
	}
}

func reportRecoverySourceUnavailable(err error) bool {
	return util.HasStatusCode(err, status_error.ChatNotFound) || util.HasStatusCode(err, status_error.ExecutionNotFound) ||
		util.HasStatusCode(err, status_error.ChatLogError) || util.HasStatusCode(err, status_error.ChatLogCorrupted) ||
		util.HasStatusCode(err, status_error.ChatLogIndexCorrupted) || util.HasStatusCode(err, status_error.ChatLogNativeOutcomePending)
}

func (s *Service) recoverOrdinaryReport(ctx context.Context, row models.WorkTicketReport,
	read func(string, string) ([]eventlog.Record, error), deliver func(context.Context, models.WorkTicket, models.WorkTicketReport) (string, error),
) error {
	defer s.lockSettlement(row.TicketID)()
	report, err := s.store.Report(ctx, row.ID)
	if err != nil || report.ID == "" || report.MessageID != "" {
		return err
	}
	input, err := s.store.Input(ctx, report.ExecutionID)
	if err != nil || input.TicketID != report.TicketID || input.ConfirmedAt == nil || input.OriginKind != constants.WorkTicketOriginChat {
		return err
	}
	target, err := s.ordinaryReportTarget(ctx, input)
	if err != nil {
		return err
	}
	records, err := read(target.WorkChatID, target.ExecutionID)
	if err != nil {
		return err
	}
	publications, err := OrdinaryWorkReportReceipts(records, target.ExecutionID)
	if err != nil {
		return err
	}
	for _, publication := range publications {
		if publication.ID == report.ID {
			if target.ExecutionID != input.ExecutionID {
				ticket, err := s.store.Ticket(ctx, input.TicketID)
				if err != nil {
					return err
				}
				if !publication.Matches(target.ExecutionID, input.ExecutionID, input.WorkChatID, ticket.OwnerWakerID, report.Content) {
					return status_error.ChatLogCorrupted
				}
				report.MessageID = publication.MessageID
				_, err = s.confirmOrdinaryReport(ctx, inputBinding(ticket, input, false), report)
				return err
			}
			return s.reconcilePublishedReports(ctx, input.ExecutionID, publications)
		}
	}
	if report.Status != constants.WorkTicketStatusCompleted {
		return nil
	}
	ticket, err := s.ordinaryConclusionTarget(ctx, input)
	if err != nil {
		return err
	}
	report.MessageID, err = deliver(ctx, ticket, report)
	if err != nil {
		return err
	}
	_, err = s.confirmOrdinaryReport(context.WithoutCancel(ctx), inputBinding(ticket, input, false), report)
	return err
}

// RecoverUnfinishedInputs 仅遍历原接纳缺少终态确认的输入，不重放已结束的历史回合。
func (s *Service) RecoverUnfinishedInputs(ctx context.Context, settle func(context.Context, Binding) error) error {
	var result error
	for after := ""; ; {
		rows, err := s.store.UnfinishedInputs(ctx, after, recoveryPageSize)
		if err != nil {
			return errors.Join(result, err)
		}
		for _, input := range rows {
			after = input.ExecutionID
			if err := ctx.Err(); err != nil {
				return errors.Join(result, err)
			}
			ticket, err := s.store.Ticket(ctx, input.TicketID)
			if err == nil && ticket.ID != "" {
				err = settle(ctx, inputBinding(ticket, input, false))
			}
			result = errors.Join(result, err)
		}
		if len(rows) < recoveryPageSize {
			return result
		}
	}
}
