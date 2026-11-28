package workticket

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// PublishedReport 是宿主从原执行权威日志中读到的已提交工单报告。
type PublishedReport struct {
	// ID 是原报告的提交身份。
	ID string
	// MessageID 是普通聊天实际保存报告的助手消息。
	MessageID string
	// Content 核对权威日志与原报告预留正文一致。
	Content string
	// SourceExecutionID 是在转交后的独立 Chat 中实际发起报告的执行。
	SourceExecutionID string
	// SourceChatID 是接手执行的实际工作记录。
	SourceChatID string
	// SourceWakerID 是接手执行的实际报告人。
	SourceWakerID string
}

// Matches 核对原址真实收件凭据，兼容曾追加在原执行答复中的转交回件。
func (p PublishedReport) Matches(targetExecutionID, sourceExecutionID, sourceChatID, sourceWakerID, content string) bool {
	if p.Content != content || p.SourceExecutionID != sourceExecutionID {
		return false
	}
	if p.MessageID == targetExecutionID {
		return p.SourceChatID == "" && p.SourceWakerID == ""
	}
	return sourceExecutionID != "" && p.MessageID == p.ID &&
		p.SourceChatID == sourceChatID && p.SourceWakerID == sourceWakerID
}

// AuthorizesReportSource 核对公开回件对应的已提交报告、实际执行和原工单关系。
func (s *Service) AuthorizesReportSource(ctx context.Context, publication models.MainConversationMessage) (bool, error) {
	report, err := s.store.Report(ctx, publication.ID)
	if err != nil || report.ID == "" || report.MessageID != publication.ID ||
		report.Content != publication.Content || report.ExecutionID != publication.SourceExecutionID {
		return false, err
	}
	input, err := s.store.Input(ctx, report.ExecutionID)
	if err != nil || input.ConfirmedAt == nil || input.TicketID != report.TicketID || input.WorkChatID != publication.SourceChatID {
		return false, err
	}
	if origin := publication.OriginalWork; origin != nil {
		return input.OriginKind == constants.WorkTicketOriginChat && input.OriginID == origin.ChatID &&
			input.TicketID == origin.TicketID && report.DeliveryConversationID == publication.ConversationID, nil
	}
	return input.OriginKind == constants.WorkTicketOriginMain && input.OriginID == publication.ConversationID &&
		input.TicketID == publication.ReplyToMessageID, nil
}

// OrdinaryWorkReportReceipts 从原执行日志提取工单发布凭据，不接受其它领域的执行交付。
func OrdinaryWorkReportReceipts(records []eventlog.Record, executionID string) ([]PublishedReport, error) {
	var reports []PublishedReport
	for _, record := range records {
		if record.Type != constants.ChatEventTypeExecutionDelivery || record.ExecutionID != executionID {
			continue
		}
		var delivery eventlog.ExecutionDeliveryData
		if json.Unmarshal(record.Data, &delivery) != nil {
			return nil, status_error.ChatLogCorrupted
		}
		if delivery.WorkTicketReport {
			if delivery.SourceID == "" || (record.MessageID != executionID && (record.MessageID != delivery.SourceID || !delivery.IsTransferredWorkReport())) {
				return nil, status_error.ChatLogCorrupted
			}
			reports = append(reports, PublishedReport{
				ID: delivery.SourceID, MessageID: record.MessageID, Content: delivery.Summary,
				SourceExecutionID: delivery.WorkTicketExecutionID, SourceChatID: delivery.SourceChatID, SourceWakerID: delivery.SourceWakerID,
			})
		}
	}
	return reports, nil
}

// ReconcilePublishedReports 用同一普通聊天执行中按日志顺序保存的报告补齐 SQLite 结算。
func (s *Service) ReconcilePublishedReports(ctx context.Context, executionID string, publications []PublishedReport) error {
	if len(publications) == 0 {
		return nil
	}
	input, err := s.store.Input(ctx, executionID)
	if err != nil || input.TicketID == "" {
		return err
	}
	defer s.lockSettlement(input.TicketID)()
	return s.reconcilePublishedReports(ctx, executionID, publications)
}

func (s *Service) reconcilePublishedReports(ctx context.Context, executionID string, publications []PublishedReport) error {
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		input, err := s.store.Input(tx, executionID)
		if err != nil || input.TicketID == "" {
			return err
		}
		ticket, err := s.store.Ticket(tx, input.TicketID)
		if err != nil {
			return err
		}
		if input.ConfirmedAt == nil || ticket.OriginKind != constants.WorkTicketOriginChat {
			return status_error.WorkTicketAttemptUnavailable
		}
		latest, err := s.recoverReportReceipts(tx, input, publications)
		if err != nil || latest.ID == "" {
			return err
		}
		if ticket.Generation != input.Generation || ticket.CurrentExecutionID != executionID ||
			ticket.Status == constants.WorkTicketStatusStopped || ticket.Status == constants.WorkTicketStatusCompleted {
			// 已经写入原日志的旧报告可恢复自己的收据，只有当前回合可以改变工单。
			return nil
		}
		status, err := s.recoveredReportStatus(tx, ticket, input, latest.Status)
		if err != nil {
			return err
		}
		return s.applyReportState(tx, ticket, input.Generation, status, latest.Content)
	})
}

func (s *Service) recoverReportReceipts(ctx context.Context, input models.WorkTicketInput, publications []PublishedReport) (models.WorkTicketReport, error) {
	var latest models.WorkTicketReport
	seen := make(map[string]PublishedReport, len(publications))
	for _, evidence := range publications {
		if evidence.SourceExecutionID != "" && evidence.SourceExecutionID != input.ExecutionID {
			continue // 转交回件与原执行共享同一收件日志，原回合不能认领转交收据。
		}
		if previous, exists := seen[evidence.ID]; exists {
			if previous != evidence {
				return latest, status_error.ChatLogCorrupted
			}
			continue
		}
		seen[evidence.ID] = evidence
		report, err := s.store.Report(ctx, evidence.ID)
		if err != nil {
			return latest, err
		}
		if report.ID == "" || report.ExecutionID != input.ExecutionID || report.TicketID != input.TicketID ||
			report.Content != evidence.Content || evidence.MessageID != input.ExecutionID {
			return latest, status_error.ChatLogCorrupted
		}
		if report.MessageID != "" {
			if report.MessageID != evidence.MessageID {
				return latest, status_error.ChatLogCorrupted
			}
			// 更晚的报告已结算时，只补早期收据，保留最新结论。
			latest = models.WorkTicketReport{}
			continue
		}
		report.MessageID = evidence.MessageID
		if err := s.store.ConfirmReport(ctx, report); err != nil {
			return latest, err
		}
		latest = report
	}
	return latest, nil
}

func (s *Service) recoveredReportStatus(ctx context.Context, ticket models.WorkTicket, input models.WorkTicketInput, published constants.WorkTicketStatus) (constants.WorkTicketStatus, error) {
	if input.FinishedAt == nil || published == constants.WorkTicketStatusCompleted {
		return published, nil
	}
	if published == constants.WorkTicketStatusActive {
		return ticket.Status, nil
	}
	if ticket.Status == constants.WorkTicketStatusBlocked && published == constants.WorkTicketStatusWaiting {
		attempt, err := s.store.Attempt(ctx, ticket.ID, input.Generation)
		if err != nil {
			return published, err
		}
		if attempt.Status == constants.WorkTicketAttemptStatusEnded {
			// 原生失败晚于报告里的等待请求，保留失败后的受阻状态。
			return ticket.Status, nil
		}
	}
	return published, nil
}
