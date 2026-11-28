package workticket

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func delegationReplyID(parent Binding, key string) string {
	return (requestidempotency.Identity{ID: strings.TrimSpace(key)}).ResourceID(constants.WorkTicketReplyOperation, parent.Ticket.ID, parent.ExecutionID)
}

// ReplyToDelegation 将文本答复按原等待报告提交给直接子单。原生用户提问及审批仍由用户处理。
func (s *Service) ReplyToDelegation(ctx context.Context, parent Binding, key, questionID, text string) (row models.WorkTicketReport, err error) {
	key, questionID, text = strings.TrimSpace(key), strings.TrimSpace(questionID), strings.TrimSpace(text)
	if key == "" || len(key) > 64 || questionID == "" || text == "" || len(text) > maximumDelegationText || !utf8.ValidString(text) {
		return row, status_error.BadRequest
	}
	defer s.lockSettlement(parent.Ticket.ID)()
	id := delegationReplyID(parent, key)
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, parent.ExecutionID)
		if err != nil {
			return err
		}
		if !matchesBinding(input, parent) || input.ConfirmedAt == nil {
			return status_error.WorkTicketAttemptUnavailable
		}
		row, err = s.store.Report(tx, id)
		if err != nil {
			return err
		}
		if row.ID != "" {
			if row.Content != text || row.ReplyToReportID != questionID {
				return status_error.CreateRequestConflict
			}
			return nil
		}
		owner, err := s.reportTicket(tx, parent, input)
		if err != nil {
			return err
		}
		question, err := s.store.Report(tx, questionID)
		if err != nil {
			return err
		}
		child, err := s.store.Ticket(tx, question.TicketID)
		if err != nil {
			return err
		}
		if child.ParentTicketID != owner.ID || child.ParentGeneration != owner.Generation || !isDelegatedQuestion(question, child) {
			return status_error.WorkTicketAttemptUnavailable
		}
		if err := s.authorizeDelegationChain(tx, child); err != nil {
			return err
		}
		row = models.WorkTicketReport{ID: id, TicketID: child.ID, ExecutionID: question.ExecutionID,
			Status: constants.WorkTicketStatusWaiting, Content: text, ReplyToReportID: question.ID}
		return s.store.CreateReport(tx, row)
	})
	return row, err
}

// DelegationReplyReceipt 核对原父执行提交的同一答复。
func (s *Service) DelegationReplyReceipt(ctx context.Context, parent Binding, key string) (models.WorkTicketReport, error) {
	row, err := s.store.Report(ctx, delegationReplyID(parent, key))
	if err == nil && (row.ID == "" || row.ReplyToReportID == "") {
		err = status_error.AgentReceiptNotFound
	}
	return row, err
}

func isDelegatedQuestion(question models.WorkTicketReport, child models.WorkTicket) bool {
	return child.ID != "" && question.ID != "" && question.ID == question.MessageID && question.ReplyToReportID == "" &&
		question.ID != reportIdentity(question.ExecutionID, "\x00native-final") && question.Status == constants.WorkTicketStatusWaiting &&
		child.Status == constants.WorkTicketStatusWaiting && child.CurrentExecutionID == question.ExecutionID && child.ID == question.TicketID
}

func (s *Service) delegationQuestion(ctx context.Context, parent models.WorkTicket, children []models.WorkTicket, pendingID string) (models.WorkTicketReport, error) {
	var empty models.WorkTicketReport
	waiting := make(map[string]models.WorkTicket, len(children))
	var executions []string
	for _, child := range children {
		if child.Status != constants.WorkTicketStatusWaiting || child.CurrentExecutionID == "" {
			continue
		}
		input, err := s.store.Input(ctx, child.CurrentExecutionID)
		if err != nil {
			return empty, err
		}
		if input.FinishedAt == nil || input.TicketID != child.ID {
			continue
		}
		waiting[child.CurrentExecutionID] = child
		executions = append(executions, child.CurrentExecutionID)
	}
	if len(executions) == 0 {
		return empty, nil
	}
	states := []constants.WorkTicketStatus{constants.WorkTicketStatusWaiting}
	for after := ""; ; {
		rows, err := s.store.DelegationTurnReports(ctx, executions, states, after, recoveryPageSize)
		if err != nil {
			return empty, err
		}
		for _, question := range rows {
			after = question.ID
			child := waiting[question.ExecutionID]
			if !isDelegatedQuestion(question, child) || (pendingID != "" && pendingID != question.ID) {
				continue
			}
			if err := s.authorizeDelegationChain(ctx, child); err != nil {
				if errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
					continue
				}
				return empty, err
			}
			if pendingID == question.ID {
				return question, nil
			}
			input, err := s.store.Input(ctx, DelegationQuestionExecutionID(parent, question.ID))
			if err != nil {
				return empty, err
			}
			if input.ExecutionID != "" {
				continue
			}
			reply, err := s.store.DelegationReplyForQuestion(ctx, question.ID)
			if err != nil {
				return empty, err
			}
			if reply.ID == "" {
				return question, nil
			}
		}
		if len(rows) < recoveryPageSize {
			return empty, nil
		}
	}
}

// DelegationReplyLaunch 持有已确认原问题、原来源及待接纳答复。
type DelegationReplyLaunch struct {
	// Ticket 是等待答复的直接子单。
	Ticket models.WorkTicket
	// Reply 是待接纳的文本答复。
	Reply models.WorkTicketReport
}

// ReadyDelegationReplies 返回用户意图之外的私有原问题，直到该答复被原工作接纳。
func (s *Service) ReadyDelegationReplies(ctx context.Context, visit func(DelegationReplyLaunch) error) error {
	for after := ""; ; {
		rows, err := s.store.PendingDelegationReplies(ctx, after, recoveryPageSize)
		if err != nil {
			return err
		}
		for _, row := range rows {
			after = row.ID
			var child models.WorkTicket
			err := s.tx.Transaction(ctx, func(tx context.Context) error {
				child, err = s.store.Ticket(tx, row.TicketID)
				if err != nil {
					return err
				}
				_, err = s.delegationReplyForLaunch(tx, child, row.ID)
				return err
			})
			if errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
				continue
			}
			if err != nil {
				return err
			}
			if err := visit(DelegationReplyLaunch{Ticket: child, Reply: row}); err != nil {
				return err
			}
		}
		if len(rows) < recoveryPageSize {
			return nil
		}
	}
}

func (s *Service) delegationReplyForLaunch(ctx context.Context, child models.WorkTicket, replyID string) (models.WorkTicketReport, error) {
	reply, err := s.store.Report(ctx, replyID)
	if err != nil {
		return reply, err
	}
	if child.ParentTicketID == "" || reply.ID == "" || reply.MessageID != "" || reply.TicketID != child.ID ||
		reply.ExecutionID != child.CurrentExecutionID || (child.PendingExecutionID != "" && child.PendingExecutionID != reply.ID) {
		return reply, status_error.WorkTicketAttemptUnavailable
	}
	question, err := s.store.Report(ctx, reply.ReplyToReportID)
	if err != nil {
		return reply, err
	}
	if !isDelegatedQuestion(question, child) {
		return reply, status_error.WorkTicketAttemptUnavailable
	}
	input, err := s.store.Input(ctx, reply.ExecutionID)
	if err != nil {
		return reply, err
	}
	if input.TicketID != child.ID || input.FinishedAt == nil {
		return reply, status_error.WorkTicketAttemptUnavailable
	}
	return reply, s.authorizeDelegationChain(ctx, child)
}
