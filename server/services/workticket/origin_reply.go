package workticket

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// OriginReply 读取按原普通会话与请求键保存的转发回执。
func (s *Service) OriginReply(ctx context.Context, originID, submissionID string) (models.WorkTicketInput, error) {
	row, err := s.store.Submission(ctx, constants.WorkTicketOriginChat, originID, submissionID)
	if row.OriginReplyDigest == "" {
		row = models.WorkTicketInput{}
	}
	return row, err
}

// ReconcileOriginReply 用私有执行的权威接纳事实确认转发或释放未接纳的预留。
func (s *Service) ReconcileOriginReply(ctx context.Context, row models.WorkTicketInput,
	read func(context.Context, string, string) (*models.Execution, error),
) (models.WorkTicketInput, error) {
	if row.ExecutionID == "" || row.ConfirmedAt != nil {
		return row, nil
	}
	var execution *models.Execution
	var binding Binding
	current, err := func() (models.WorkTicketInput, error) {
		defer s.lockSettlement(row.TicketID)()
		current, err := s.store.Input(ctx, row.ExecutionID)
		if err != nil || current.ExecutionID == "" || current.ConfirmedAt != nil {
			return current, err
		}
		ticket, err := s.store.Ticket(ctx, current.TicketID)
		if err != nil {
			return current, err
		}
		binding = inputBinding(ticket, current, false)
		execution, err = read(ctx, current.WorkChatID, current.SubmissionID)
		if err != nil && !util.HasStatusCode(err, status_error.ChatNotFound) {
			return current, err
		}
		if execution == nil {
			return models.WorkTicketInput{}, s.AbortAdmission(ctx, binding)
		}
		if execution.ID != current.ExecutionID || execution.SubmissionHash != current.SubmissionHash {
			return current, status_error.ChatLogCorrupted
		}
		if err := s.ConfirmAdmission(ctx, binding); err != nil {
			return current, err
		}
		return s.store.Input(ctx, current.ExecutionID)
	}()
	if err == nil && execution != nil && terminalExecution(execution.Status) {
		err = s.FinishTurn(ctx, binding, execution.Status)
	}
	return current, err
}

// PublishOriginReply 投递已确认的原会话输入，权威日志用同一消息键去重。
func (s *Service) PublishOriginReply(ctx context.Context, row models.WorkTicketInput,
	appendFacts func(context.Context, string, []eventlog.AppendInput) ([]eventlog.Record, error),
) error {
	if row.OriginReply == nil || row.ConfirmedAt == nil {
		return nil
	}
	ticket, err := s.store.Ticket(ctx, row.TicketID)
	if err != nil {
		return err
	}
	if ticket.DeletedOrigin != nil {
		return s.store.ConfirmOriginReply(ctx, row.ExecutionID)
	}
	payload, err := json.Marshal(row.OriginReply)
	if err != nil {
		return status_error.ChatLogRecordInvalid
	}
	_, err = appendFacts(ctx, row.OriginID, []eventlog.AppendInput{{
		Type: constants.ChatEventTypeUserMessageCreated, MessageID: row.MessageID, Data: payload,
	}})
	if err != nil && !util.HasStatusCode(err, status_error.ChatNotFound) {
		return err
	}
	return s.store.ConfirmOriginReply(context.WithoutCancel(ctx), row.ExecutionID)
}

// PublishOriginReplyForExecution 在追加该执行的报告之前补交同一轮的原会话输入。
func (s *Service) PublishOriginReplyForExecution(ctx context.Context, executionID string,
	appendFacts func(context.Context, string, []eventlog.AppendInput) ([]eventlog.Record, error),
) error {
	row, err := s.store.Input(ctx, executionID)
	if err != nil {
		return err
	}
	return s.PublishOriginReply(ctx, row, appendFacts)
}

// RecoverOriginReplies 分页核对已经提交的转发输入并补齐未完成的独立公开消息。
func (s *Service) RecoverOriginReplies(ctx context.Context, visit func(context.Context, models.WorkTicketInput) error) error {
	var result error
	for after := ""; ctx.Err() == nil; {
		rows, err := s.store.PendingOriginReplies(ctx, after, recoveryPageSize)
		if err != nil {
			return errors.Join(result, err)
		}
		for _, row := range rows {
			after = row.ExecutionID
			result = errors.Join(result, visit(ctx, row))
		}
		if len(rows) < recoveryPageSize {
			return result
		}
	}
	return errors.Join(result, ctx.Err())
}
