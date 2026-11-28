package workticket

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// DeletedOriginPublisher 将已验证原址删除的报告写入同一伙伴的主会话。
type DeletedOriginPublisher func(context.Context, string, models.WorkTicket, models.WorkTicketReport, models.MainConversationOriginalWork) (models.MainConversationMessage, error)

type deletedOriginEvidence struct {
	// reportID 标识暂存前读到的待确认结案报告。
	reportID string
	// chat 保存权威日志核对的会话归属和标题。
	chat models.Chat
	// received 为原址已经提交的同键消息，空值表示原址未收件。
	received string
	// finishedAt 是原执行终态日志的存储时间。
	finishedAt int64
	// targetExecutionID 是原会话中接收报告的执行。
	targetExecutionID string
}

// PrepareOrdinaryOriginDeletion 在排他删除门内读完原执行日志，返回应与正式删除一起提交的回执操作。
func (s *Service) PrepareOrdinaryOriginDeletion(ctx context.Context, chats []models.Chat, read func(string, string) ([]eventlog.Record, error)) (func(context.Context) error, error) {
	if len(chats) == 0 {
		return nil, nil
	}
	byID := make(map[string]models.Chat, len(chats))
	ids := make([]string, 0, len(chats))
	for _, chat := range chats {
		byID[chat.ID] = chat
		ids = append(ids, chat.ID)
	}
	reports, err := s.store.PendingConclusionsForChats(ctx, ids)
	if err != nil {
		return nil, err
	}
	evidence := make([]deletedOriginEvidence, 0, len(reports))
	for _, report := range reports {
		input, err := s.store.Input(ctx, report.ExecutionID)
		if err != nil {
			return nil, err
		}
		if input.TicketID != report.TicketID {
			return nil, status_error.WorkTicketAttemptUnavailable
		}
		target, err := s.ordinaryReportTarget(ctx, input)
		if err != nil {
			return nil, err
		}
		chat := byID[target.WorkChatID]
		if chat.ID == "" || target.OriginID != chat.ID {
			return nil, status_error.WorkTicketAttemptUnavailable
		}
		ticket, err := s.store.Ticket(ctx, input.TicketID)
		if err != nil {
			return nil, err
		}
		records, err := read(chat.ID, target.ExecutionID)
		if err != nil {
			return nil, err
		}
		if chat.WakerID == "" || ticket.ID == "" {
			return nil, status_error.WorkTicketAttemptUnavailable
		}
		publications, err := OrdinaryWorkReportReceipts(records, target.ExecutionID)
		if err != nil {
			return nil, err
		}
		proof := deletedOriginEvidence{
			reportID: report.ID, chat: chat, targetExecutionID: target.ExecutionID,
			finishedAt: ordinaryReportOriginFinishedAt(records, target.ExecutionID),
		}
		sourceExecutionID := ""
		if target.ExecutionID != input.ExecutionID {
			sourceExecutionID = input.ExecutionID
		}
		for _, receipt := range publications {
			if receipt.ID != report.ID {
				continue
			}
			if !receipt.Matches(target.ExecutionID, sourceExecutionID, input.WorkChatID, ticket.OwnerWakerID, report.Content) ||
				(proof.received != "" && proof.received != receipt.MessageID) {
				return nil, status_error.ChatLogCorrupted
			}
			proof.received = receipt.MessageID
		}
		if proof.received == "" && proof.finishedAt <= 0 {
			return nil, status_error.ChatHasActiveExecution
		}
		evidence = append(evidence, proof)
	}
	return func(tx context.Context) error {
		// 删除门排除了 JSONL 写入。事务用当前报告行复核收件决定和工单代次。
		pending, err := s.store.PendingConclusionsForChats(tx, ids)
		if err != nil {
			return err
		}
		checked := make(map[string]bool, len(evidence))
		for _, proof := range evidence {
			checked[proof.reportID] = true
			if err := s.preserveDeletedOriginReport(tx, proof); err != nil {
				return err
			}
		}
		for _, report := range pending {
			if !checked[report.ID] {
				// 接手人可以同时登记结论，重试删除后才能核验这份报告的收件事实。
				return status_error.ChatHasActiveExecution
			}
		}
		for _, chat := range chats {
			if chat.WakerID != "" {
				if err := s.store.MarkOrdinaryTicketOriginDeleted(tx, chat); err != nil {
					return err
				}
			}
		}
		return nil
	}, nil
}

func ordinaryReportOriginFinishedAt(records []eventlog.Record, executionID string) int64 {
	for _, record := range records {
		if record.ExecutionID != executionID {
			continue
		}
		switch record.Type {
		case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
			return record.RecordedAt
		}
	}
	return 0
}

func (s *Service) preserveDeletedOriginReport(ctx context.Context, proof deletedOriginEvidence) error {
	report, err := s.store.Report(ctx, proof.reportID)
	if err != nil || report.ID == "" || report.DeliveryConversationID != "" {
		return err
	}
	if report.Status != constants.WorkTicketStatusCompleted || (report.MessageID != "" && report.MessageID != proof.received) {
		return status_error.ChatLogCorrupted
	}
	input, err := s.store.Input(ctx, report.ExecutionID)
	if err != nil {
		return err
	}
	if input.TicketID != report.TicketID || input.OriginID != proof.chat.ID {
		return status_error.WorkTicketAttemptUnavailable
	}
	target, err := s.ordinaryReportTarget(ctx, input)
	if err != nil {
		return err
	}
	if target.ExecutionID != proof.targetExecutionID || target.WorkChatID != proof.chat.ID {
		return status_error.WorkTicketAttemptUnavailable
	}
	ticket, err := s.store.Ticket(ctx, input.TicketID)
	if err != nil || ticket.ID == "" {
		return err
	}
	if ticket.OriginKind != input.OriginKind || ticket.OriginID != input.OriginID || ticket.WorkChatID != input.WorkChatID ||
		(!ticket.Transferred && ticket.OwnerWakerID != proof.chat.WakerID) {
		return status_error.WorkTicketAttemptUnavailable
	}
	if proof.received != "" {
		report.MessageID, report.DeliveryConversationID = proof.received, input.OriginID
	} else {
		if ticket.Status == constants.WorkTicketStatusStopped ||
			(ticket.Transferred && (ticket.Generation != input.Generation || ticket.CurrentExecutionID != input.ExecutionID)) {
			return nil
		}
		if _, err = s.ordinaryConclusionTarget(ctx, input); util.HasStatusCode(err, status_error.WorkTicketAttemptUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		report, err = s.fallbackPublication(ctx, ticket, report, models.WorkTicketDeletedOrigin{WakerID: proof.chat.WakerID, Title: proof.chat.Title})
		if err != nil {
			return err
		}
	}
	if ticket.Generation == input.Generation && ticket.CurrentExecutionID == input.ExecutionID &&
		ticket.Status != constants.WorkTicketStatusCompleted && ticket.Status != constants.WorkTicketStatusStopped {
		if err := s.applyReportState(ctx, ticket, input.Generation, report.Status, report.Content); err != nil {
			return err
		}
	}
	if target.FinishedAt == nil && proof.finishedAt > 0 {
		target.FinishedAt = &proof.finishedAt
		if err := s.store.ChangeInput(ctx, target); err != nil {
			return err
		}
	}
	return s.store.PreserveDeletedOriginReceipt(ctx, report)
}

// fallbackPublication 只能在核对原址未收件的删除事务内，或读取该事务留下的原址标记后调用。
func (s *Service) fallbackPublication(ctx context.Context, ticket models.WorkTicket, report models.WorkTicketReport, deleted models.WorkTicketDeletedOrigin) (models.WorkTicketReport, error) {
	if s.publishDeletedOrigin == nil {
		return report, status_error.WorkTicketAttemptUnavailable
	}
	row, err := s.publishDeletedOrigin(ctx, deleted.WakerID, ticket, report, models.MainConversationOriginalWork{
		ChatID: ticket.OriginID, Title: deleted.Title, TicketID: ticket.ID,
	})
	if err == nil {
		report.MessageID, report.DeliveryConversationID = row.ID, row.ConversationID
	}
	return report, err
}
