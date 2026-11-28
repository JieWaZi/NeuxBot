package workticket

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ReportInput 是 Worker 对当前工单提交的一次有效报告。
type ReportInput struct {
	// RequestID 用本轮工具调用的稳定身份去重。
	RequestID string
	// Kind 表示报告要求的业务状态。
	Kind string
	// Content 是应返回原会话的报告正文。
	Content string
}

// ReportReceipt 允许 Worker 重放原调用并得到同一会话消息。
type ReportReceipt struct {
	// ID 是报告本身的稳定身份。
	ID string
	// MessageID 是报告在原会话中采用的消息身份。
	MessageID string
	// Replayed 表示原报告已经交付。
	Replayed bool
}

func reportIdentity(executionID, key string) string {
	return (requestidempotency.Identity{ID: key}).ResourceID(constants.WorkTicketReportOperation, executionID)
}

// ReportReceipt 只返回已经持久交付的精确工具调用收据。
func (s *Service) ReportReceipt(ctx context.Context, binding Binding, requestID string) (receipt ReportReceipt, found bool, err error) {
	report, err := s.store.Report(ctx, reportIdentity(binding.ExecutionID, strings.TrimSpace(requestID)))
	if err != nil || report.MessageID == "" || report.TicketID != binding.Ticket.ID || report.ExecutionID != binding.ExecutionID {
		return receipt, false, err
	}
	input, err := s.store.Input(ctx, binding.ExecutionID)
	if err != nil || !matchesBinding(input, binding) {
		return receipt, false, err
	}
	return ReportReceipt{ID: report.ID, MessageID: report.MessageID, Replayed: true}, true, nil
}

// HasPendingOrdinaryReport 判断同一普通会话对原执行是否还有未结算的报告预留。
func (s *Service) HasPendingOrdinaryReport(ctx context.Context, address Address, executionID string) (bool, error) {
	if address.Kind != constants.WorkTicketOriginChat || address.ID == "" || executionID == "" {
		return false, nil
	}
	var pending bool
	err := s.tx.Transaction(ctx, func(tx context.Context) error {
		pending = false
		input, err := s.store.Input(tx, executionID)
		if err != nil || input.ConfirmedAt == nil || input.OriginKind != address.Kind || input.OriginID != address.ID || input.WorkChatID != address.ID {
			return err
		}
		pending, err = s.store.HasPendingReport(tx, input.TicketID, executionID)
		return err
	})
	return pending, err
}

// Report 使用原执行和原会话结算一次工单报告。主会话的消息写入加入事务，普通聊天的宿主日志复用稳定报告标识。
func (s *Service) Report(ctx context.Context, binding Binding, in ReportInput, deliver func(context.Context, models.WorkTicket, string) (string, error)) (receipt ReportReceipt, err error) {
	var publish func(context.Context, models.WorkTicket, models.WorkTicketReport) (string, error)
	if deliver != nil {
		publish = func(ctx context.Context, ticket models.WorkTicket, report models.WorkTicketReport) (string, error) {
			return deliver(ctx, ticket, report.ID)
		}
	}
	return s.report(ctx, binding, in, nil, publish)
}

// ReportNativeFinal 向原主会话补交当前回合尚未发布的可验证原生正文。
// 开放工单转为等待，已提交的结案结果继续保留。
func (s *Service) ReportNativeFinal(ctx context.Context, binding Binding, answer func(context.Context) (string, error),
	deliver func(context.Context, models.WorkTicket, string, string) (string, error),
) error {
	if answer == nil || deliver == nil || (binding.Ticket.OriginKind != constants.WorkTicketOriginMain && !binding.Ticket.Transferred) {
		return status_error.BadRequest
	}
	// 能力调用的请求标识不允许 NUL，宿主回件可以安全地共用同一报告索引。
	input := ReportInput{RequestID: "\x00native-final", Kind: string(constants.WorkTicketStatusWaiting)}
	_, err := s.report(ctx, binding, input, answer, func(ctx context.Context, ticket models.WorkTicket, report models.WorkTicketReport) (string, error) {
		return deliver(ctx, ticket, report.ID, report.Content)
	})
	return err
}

func canPublishNativeFinal(status constants.WorkTicketStatus) bool {
	return status == constants.WorkTicketStatusActive || status == constants.WorkTicketStatusCompleted
}

func (s *Service) report(ctx context.Context, binding Binding, in ReportInput, answer func(context.Context) (string, error),
	deliver func(context.Context, models.WorkTicket, models.WorkTicketReport) (string, error),
) (receipt ReportReceipt, err error) {
	in.RequestID = strings.TrimSpace(in.RequestID)
	in.Content = strings.TrimSpace(in.Content)
	status := constants.WorkTicketStatus(in.Kind)
	switch status {
	case constants.WorkTicketStatus(constants.WorkTicketReportKindProgress):
		status = constants.WorkTicketStatusActive
	case constants.WorkTicketStatusWaiting, constants.WorkTicketStatusBlocked, constants.WorkTicketStatusCompleted:
	default:
		return receipt, status_error.BadRequest
	}
	if binding.Ticket.ID == "" || binding.ExecutionID == "" || in.RequestID == "" || (in.Content == "" && answer == nil) {
		return receipt, status_error.BadRequest
	}
	defer s.lockSettlement(binding.Ticket.ID)()
	if answer != nil {
		current, err := s.store.Ticket(ctx, binding.Ticket.ID)
		if err != nil || current.CurrentExecutionID != binding.ExecutionID || current.Generation != binding.Generation ||
			(current.Transferred && current.Status == constants.WorkTicketStatusCompleted) ||
			!canPublishNativeFinal(current.Status) {
			return receipt, err
		}
		// 原生回合日志可能很长，只在 SQLite 事务外读取，提交时重新核对授权。
		text, err := answer(ctx)
		if err != nil {
			return receipt, err
		}
		in.Content = text
	}
	report := models.WorkTicketReport{ID: reportIdentity(binding.ExecutionID, in.RequestID), TicketID: binding.Ticket.ID,
		ExecutionID: binding.ExecutionID, Status: status, Content: in.Content}
	var ticket models.WorkTicket
	var publishOrdinary bool
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		ticket, receipt = models.WorkTicket{}, ReportReceipt{}
		publishOrdinary = false
		report.Status = status
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil {
			return err
		}
		if !matchesBinding(input, binding) {
			return status_error.WorkTicketAttemptUnavailable
		}
		if answer != nil {
			ticket, err = s.reportTarget(tx, binding, input, true)
			if errors.Is(err, status_error.WorkTicketAttemptUnavailable) {
				return nil
			}
			if err != nil || !canPublishNativeFinal(ticket.Status) {
				// 声明等待或受阻的显式回件保留为本轮结论。
				return err
			}
			hasChildren, blocked, err := s.dependencyState(tx, ticket)
			if err != nil || blocked || (ticket.Status == constants.WorkTicketStatusCompleted && (hasChildren || ticket.ParentTicketID != "")) {
				return err
			}
			plain := strings.TrimSpace(report.Content)
			if ticket.Status == constants.WorkTicketStatusCompleted {
				if plain == "" {
					return nil
				}
				report.Status = ticket.Status
			}
			published, err := s.store.HasPublishedReport(tx, ticket.ID, binding.ExecutionID, plain)
			if err != nil || published {
				return err
			}
			if plain == "" {
				if ticket.OriginKind == constants.WorkTicketOriginMain && ticket.ParentTicketID == "" {
					published, err := s.store.HasPublishedMainMessage(tx, ticket.OriginID, ticket.WorkChatID, binding.ExecutionID)
					if err != nil || published {
						return err
					}
				}
				return status_error.RuntimeExecutionFailed
			}
		}
		saved, err := s.store.Report(tx, report.ID)
		if err != nil {
			return err
		}
		if saved.ID != "" {
			if saved.TicketID != report.TicketID || saved.ExecutionID != report.ExecutionID ||
				saved.Status != report.Status || saved.Content != report.Content {
				return status_error.CreateRequestConflict
			}
			if saved.MessageID != "" {
				receipt = ReportReceipt{ID: saved.ID, MessageID: saved.MessageID, Replayed: true}
				return nil
			}
		}
		if saved.ID == "" && input.OriginKind == constants.WorkTicketOriginChat {
			pending, err := s.store.HasPendingReport(tx, input.TicketID, binding.ExecutionID)
			if err != nil {
				return err
			}
			if pending {
				return status_error.CreateRequestInProgress
			}
		}
		if answer == nil {
			if saved.ID != "" && input.OriginKind == constants.WorkTicketOriginChat && report.Status == constants.WorkTicketStatusCompleted {
				ticket, err = s.ordinaryConclusionTarget(tx, input)
			} else {
				ticket, err = s.reportTicket(tx, binding, input)
			}
			if err != nil {
				return err
			}
		}
		if status == constants.WorkTicketStatusCompleted {
			_, blocked, err := s.dependencyState(tx, ticket)
			if err != nil {
				return err
			}
			if blocked {
				return status_error.WorkTicketCannotContinue
			}
		}
		if deliver == nil && ticket.ParentTicketID == "" && ticket.DeletedOrigin == nil {
			return status_error.BadRequest
		}
		if saved.ID == "" {
			if err := s.store.CreateReport(tx, report); err != nil {
				return err
			}
			if ticket.OriginKind == constants.WorkTicketOriginChat && ticket.DeletedOrigin == nil && report.Status == constants.WorkTicketStatusCompleted {
				// 已接受的结论和发往普通会话的待投意图同时提交，日志写入只确认交付。
				publishOrdinary = true
				return s.applyReportState(tx, ticket, binding.Generation, report.Status, report.Content)
			}
		}
		if ticket.ParentTicketID != "" {
			// 报告本身就是直接父单的持久收件凭据，不调用公开会话发布端口。
			publication := report
			publication.MessageID = report.ID
			receipt, err = s.applyReport(tx, ticket, binding, publication)
			return err
		}
		if ticket.DeletedOrigin != nil {
			// 新登记的结果还没有出现在旧地址。旧的待投报告只由删除事务核定收件与回退。
			if saved.ID != "" {
				return status_error.WorkTicketAttemptUnavailable
			}
			publication, err := s.fallbackPublication(tx, ticket, report, *ticket.DeletedOrigin)
			if err != nil {
				return err
			}
			receipt, err = s.applyReport(tx, ticket, binding, publication)
			return err
		}
		if ticket.OriginKind != constants.WorkTicketOriginMain {
			publishOrdinary = true
			return nil
		}
		messageID, err := deliver(tx, ticket, report)
		if err != nil {
			return err
		}
		publication := report
		publication.MessageID = messageID
		receipt, err = s.applyReport(tx, ticket, binding, publication)
		return err
	})
	if err != nil || !publishOrdinary {
		return receipt, err
	}

	// 普通 Chat 的回件来自权威日志，事务重试不能重复执行日志副作用。
	report.MessageID, err = deliver(ctx, ticket, report)
	if err != nil {
		if replay, found, readErr := s.ReportReceipt(context.WithoutCancel(ctx), binding, in.RequestID); readErr == nil && found {
			return replay, nil
		}
		return receipt, err
	}
	receipt, err = s.confirmOrdinaryReport(context.WithoutCancel(ctx), binding, report)
	return receipt, err
}

// ordinaryConclusionTarget 只用于已经接纳并持久保存的普通会话结论。
func (s *Service) ordinaryConclusionTarget(ctx context.Context, input models.WorkTicketInput) (models.WorkTicket, error) {
	ticket, err := s.store.Ticket(ctx, input.TicketID)
	if err != nil {
		return ticket, err
	}
	if input.ConfirmedAt == nil || input.OriginKind != constants.WorkTicketOriginChat ||
		ticket.ID == "" || ticket.OriginKind != input.OriginKind || ticket.OriginID != input.OriginID || ticket.WorkChatID != input.WorkChatID {
		return ticket, status_error.WorkTicketAttemptUnavailable
	}
	allowed, err := s.store.Authorizes(ctx, ticket, false)
	if err == nil && !allowed {
		err = status_error.WorkTicketAttemptUnavailable
	}
	return ticket, err
}

func (s *Service) confirmOrdinaryReport(ctx context.Context, binding Binding, report models.WorkTicketReport) (receipt ReportReceipt, err error) {
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil {
			return err
		}
		if !matchesBinding(input, binding) || input.ConfirmedAt == nil {
			return status_error.WorkTicketAttemptUnavailable
		}
		target, err := s.ordinaryReportTarget(tx, input)
		if err != nil {
			return err
		}
		if report.MessageID != target.ExecutionID && (target.ExecutionID == input.ExecutionID || report.MessageID != report.ID) {
			return status_error.WorkTicketAttemptUnavailable
		}
		stored, err := s.store.Report(tx, report.ID)
		if err != nil {
			return err
		}
		if stored.ID == "" || stored.ExecutionID != report.ExecutionID || stored.TicketID != report.TicketID ||
			stored.Status != report.Status || stored.Content != report.Content {
			return status_error.CreateRequestConflict
		}
		receipt = ReportReceipt{ID: report.ID, MessageID: report.MessageID}
		if stored.MessageID != "" {
			if stored.MessageID != report.MessageID {
				return status_error.CreateRequestConflict
			}
			receipt.Replayed = true
			return nil
		}
		ticket, err := s.store.Ticket(tx, input.TicketID)
		if err != nil {
			return err
		}
		if err := s.store.ConfirmReport(tx, report); err != nil {
			return err
		}
		if ticket.ID == "" || ticket.Generation != input.Generation || ticket.CurrentExecutionID != input.ExecutionID ||
			ticket.Status == constants.WorkTicketStatusCompleted || ticket.Status == constants.WorkTicketStatusStopped {
			return nil
		}
		status, err := s.recoveredReportStatus(tx, ticket, input, report.Status)
		if err != nil {
			return err
		}
		return s.applyReportState(tx, ticket, binding.Generation, status, report.Content)
	})
	return receipt, err
}

func (s *Service) reportTicket(ctx context.Context, binding Binding, input models.WorkTicketInput) (models.WorkTicket, error) {
	return s.reportTarget(ctx, binding, input, false)
}

// PublishMainMessage 在同一事务内核对主会话的当前工单授权并提交发言。
func (s *Service) PublishMainMessage(ctx context.Context, binding Binding, publish func(context.Context) error) error {
	if binding.Ticket.ID == "" || binding.Ticket.OriginKind != constants.WorkTicketOriginMain || binding.Ticket.ParentTicketID != "" {
		return status_error.WorkTicketAttemptUnavailable
	}
	defer s.lockSettlement(binding.Ticket.ID)()
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil {
			return err
		}
		ticket, err := s.reportTicket(tx, binding, input)
		if err != nil {
			return err
		}
		if ticket.OriginKind != constants.WorkTicketOriginMain || ticket.ParentTicketID != "" {
			return status_error.WorkTicketAttemptUnavailable
		}
		return publish(tx)
	})
}

func (s *Service) reportTarget(ctx context.Context, binding Binding, input models.WorkTicketInput, allowCompleted bool) (models.WorkTicket, error) {
	if !matchesBinding(input, binding) || input.ConfirmedAt == nil || input.FinishedAt != nil {
		return models.WorkTicket{}, status_error.WorkTicketAttemptUnavailable
	}
	ticket, err := s.store.Ticket(ctx, input.TicketID)
	if err != nil {
		return ticket, err
	}
	if ticket.CurrentExecutionID != input.ExecutionID || ticket.Generation != input.Generation ||
		ticket.Status == constants.WorkTicketStatusStopped || (ticket.Status == constants.WorkTicketStatusCompleted && !allowCompleted) {
		return ticket, status_error.WorkTicketAttemptUnavailable
	}
	if ticket.ParentTicketID != "" {
		return ticket, s.authorizeDelegationChain(ctx, ticket)
	}
	allowed, err := s.store.Authorizes(ctx, ticket, false)
	if err != nil {
		return ticket, err
	}
	if !allowed {
		return ticket, status_error.WorkTicketAttemptUnavailable
	}
	return ticket, nil
}

// WhileExecutionCurrent 核对原工单尝试仍有效，并防止移交或重开与精确停止请求交错。
func (s *Service) WhileExecutionCurrent(ctx context.Context, binding Binding, stop func() error) error {
	defer s.lockSettlement(binding.Ticket.ID)()
	input, err := s.store.Input(ctx, binding.ExecutionID)
	if err != nil {
		return err
	}
	if _, err := s.reportTarget(ctx, binding, input, true); err != nil {
		return err
	}
	return stop()
}

func (s *Service) applyReport(ctx context.Context, ticket models.WorkTicket, binding Binding, report models.WorkTicketReport) (ReportReceipt, error) {
	var receipt ReportReceipt
	if report.MessageID == "" {
		return receipt, status_error.BadRequest
	}
	if err := s.store.ConfirmReport(ctx, report); err != nil {
		return receipt, err
	}
	// 已结案工单的原生正文只是追加消息，保留已确认的结案摘要和尝试状态。
	if ticket.Status != constants.WorkTicketStatusCompleted {
		if err := s.applyReportState(ctx, ticket, binding.Generation, report.Status, report.Content); err != nil {
			return receipt, err
		}
	}
	return ReportReceipt{ID: report.ID, MessageID: report.MessageID}, nil
}

func (s *Service) applyReportState(ctx context.Context, ticket models.WorkTicket, generation int64, status constants.WorkTicketStatus, content string) error {
	if status == constants.WorkTicketStatusCompleted {
		if err := s.endAttempt(ctx, ticket.ID, generation); err != nil {
			return err
		}
	}
	next := ticket
	next.Status, next.Summary, next.UpdatedAt = status, content, ticketTime(ticket)
	if status == constants.WorkTicketStatusCompleted {
		next.AwaitingDelegationReturns = false
	}
	if err := s.store.ChangeTicket(ctx, ticket, next); err != nil {
		return err
	}
	s.notify(ctx, ticketAddress(ticket))
	return nil
}

func terminalExecution(status constants.ExecutionStatus) bool {
	return status == constants.ExecutionStatusCompleted || status == constants.ExecutionStatusFailed || status == constants.ExecutionStatusCanceled
}

// FinishTurn 保存原生回合的结束，仅在该轮仍具有推进资格时更新开放工单。
func (s *Service) FinishTurn(ctx context.Context, binding Binding, status constants.ExecutionStatus) error {
	if !terminalExecution(status) {
		return status_error.BadRequest
	}
	defer s.lockSettlement(binding.Ticket.ID)()
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, binding.ExecutionID)
		if err != nil || input.ExecutionID == "" {
			return err
		}
		if !matchesBinding(input, binding) || input.ConfirmedAt == nil {
			return status_error.WorkTicketAttemptUnavailable
		}
		if input.FinishedAt != nil {
			return nil
		}
		ticket, err := s.store.Ticket(tx, binding.Ticket.ID)
		if err != nil {
			return err
		}
		now := ticketTime(ticket)
		input.FinishedAt = &now
		if err := s.store.ChangeInput(tx, input); err != nil {
			return err
		}
		if ticket.CurrentExecutionID != binding.ExecutionID || ticket.Generation != binding.Generation ||
			ticket.Status == constants.WorkTicketStatusCompleted || ticket.Status == constants.WorkTicketStatusStopped {
			return nil
		}
		next := ticket
		if status == constants.ExecutionStatusFailed && ticket.AwaitingDelegationReturns {
			// 原生失败已落盘。保持这批直接依赖的代次，待有可行动回件再续办。
			next.Status = constants.WorkTicketStatusWaiting
		} else if status != constants.ExecutionStatusCompleted {
			if err := s.endAttempt(tx, ticket.ID, binding.Generation); err != nil {
				return err
			}
			next.Status = constants.WorkTicketStatusBlocked
			if ticket.ParentTicketID != "" && status == constants.ExecutionStatusFailed {
				if err := s.recordDelegationFailure(tx, ticket); err != nil {
					return err
				}
			}
		} else if ticket.Status == constants.WorkTicketStatusActive {
			next.Status = constants.WorkTicketStatusWaiting
		}
		if next.Status == ticket.Status {
			return nil
		}
		next.UpdatedAt = now
		if err := s.store.ChangeTicket(tx, ticket, next); err != nil {
			return err
		}
		s.notify(tx, ticketAddress(ticket))
		return nil
	})
}
