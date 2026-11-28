package mainconversation

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func publicMessageID(parts ...string) string {
	return models.MainConversationMessageID(parts...)
}

// RemoveMessage 只改变当前主体主会话中的消息可见性。
func (s *Service) RemoveMessage(ctx context.Context, kind, id, messageID string) error {
	return s.tx.Transaction(ctx, func(tx context.Context) error {
		conversation, err := s.Ensure(tx, kind, id)
		if err != nil {
			return err
		}
		return s.store.RemoveMessage(tx, conversation, messageID)
	})
}

// UserMessageID 在执行接纳前定位公开交办，重投仍引用此前的同一条消息。
func UserMessageID(conversationID, submissionID string) string {
	return publicMessageID("user", conversationID, submissionID)
}

// Message 读取指定会话内的原消息以恢复回复引用和工作来源。
func (s *Service) Message(ctx context.Context, conversationID, messageID string) (models.MainConversationMessage, error) {
	return s.store.Message(ctx, conversationID, messageID)
}

// Messages 读取按公开顺序排列的一页消息及是否有更早记录。
func (s *Service) Messages(ctx context.Context, conversationID string, before int64, limit int) ([]models.MainConversationMessage, bool, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, false, status_error.BadRequest
	}
	return s.store.Messages(ctx, conversationID, before, limit)
}

// AdmitUser 预留提交键并在来源日志确认接纳后公开交办。
func (s *Service) AdmitUser(ctx context.Context, kind, id, requestID, content, replyID, chatID, sourceExecutionID, sourceMessageID, requestedDirectory, inputDigest string, commit func(context.Context) error) (msg models.MainConversationMessage, err error) {
	return s.admitUser(ctx, kind, id, models.MainConversationMessage{
		AdmissionSubmissionID: requestID, Content: content, ReplyToMessageID: replyID, SourceChatID: chatID,
		SourceExecutionID: sourceExecutionID, SourceMessageID: sourceMessageID, RequestedDirectory: requestedDirectory, InputDigest: inputDigest,
	}, func(tx context.Context) (string, error) {
		work, err := s.Source(tx, kind, id, chatID)
		return work.ConversationID, err
	}, commit)
}

func (s *Service) admitUser(ctx context.Context, kind, id string, msg models.MainConversationMessage, authorize func(context.Context) (string, error), commit func(context.Context) error) (models.MainConversationMessage, error) {
	if strings.TrimSpace(msg.AdmissionSubmissionID) == "" || strings.TrimSpace(msg.Content) == "" || commit == nil {
		return msg, status_error.BadRequest
	}
	err := s.tx.Transaction(ctx, func(tx context.Context) error {
		conversationID, err := authorize(tx)
		if err != nil {
			return err
		}
		msg.ID = UserMessageID(conversationID, msg.AdmissionSubmissionID)
		msg.ConversationID, msg.Role, msg.AdmissionPending = conversationID, string(constants.MessageRoleUser), true
		msg, _, err = s.append(tx, msg, kind, id)
		return err
	})
	if err != nil {
		return msg, err
	}
	if err := commit(ctx); err != nil {
		return msg, err
	}
	// 来源已持久接纳，公开投影失败仍应派发。原回执重试与启动恢复都可补齐。
	if accepted, err := s.store.ConfirmUserAdmission(context.WithoutCancel(ctx), msg.ConversationID, msg.ID); err == nil {
		msg = accepted
	}
	return msg, nil
}

// RecoverUserAdmissions 核对已接纳但来不及公开的交办。缺少来源回执的交办继续等待原键重试。
func (s *Service) RecoverUserAdmissions(ctx context.Context, conversationID string, read func(context.Context, string, string) (*models.Execution, error)) error {
	const batch = 100
	var report error
	for after := ""; ; {
		rows, err := s.store.PendingUserAdmissions(ctx, conversationID, after, batch)
		if err != nil {
			return err
		}
		for _, row := range rows {
			after = row.ID
			if row.AdmissionSubmissionID == "" {
				continue
			}
			execution, err := read(ctx, row.SourceChatID, row.AdmissionSubmissionID)
			if util.HasStatusCode(err, status_error.ExecutionNotFound) || util.HasStatusCode(err, status_error.ChatNotFound) ||
				util.HasStatusCode(err, status_error.ChatLogCorrupted) || util.HasStatusCode(err, status_error.ChatLogIndexCorrupted) {
				continue
			}
			if err != nil {
				report = errors.Join(report, err)
				continue
			}
			if execution == nil {
				continue
			}
			if _, err := s.ConfirmUserReceipt(ctx, row, *execution); err != nil && !util.HasStatusCode(err, status_error.CreateRequestConflict) {
				report = errors.Join(report, err)
			}
		}
		if len(rows) < batch {
			return report
		}
	}
}

// ConfirmUserReceipt 用权威回执恢复公开交办，不重复派发原执行。
func (s *Service) ConfirmUserReceipt(ctx context.Context, row models.MainConversationMessage, execution models.Execution) (models.MainConversationMessage, error) {
	if !row.AdmissionPending {
		return row, nil
	}
	if execution.ChatID != row.SourceChatID || execution.ID != row.SourceExecutionID ||
		execution.SubmissionID == "" || UserMessageID(row.ConversationID, execution.SubmissionID) != row.ID {
		return row, status_error.CreateRequestConflict
	}
	return s.store.ConfirmUserAdmission(ctx, row.ConversationID, row.ID)
}

// WorkMessages 在读取公开历史的同一事务内核验本次执行来源和成员当前资格。
func (s *Service) WorkMessages(ctx context.Context, chatID string, before int64, limit int) (rows []models.MainConversationMessage, more bool, err error) {
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		w, _, err := s.authorizedWork(tx, chatID)
		if err != nil {
			return err
		}
		rows, more, err = s.Messages(tx, w.ConversationID, before, limit)
		return err
	})
	return
}

// Published 按当前执行资格查找原工具调用持久接纳的发言收据。
func (s *Service) Published(ctx context.Context, chatID, executionID, callKey string) (msg models.MainConversationMessage, found bool, err error) {
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		w, _, err := s.authorizedWork(tx, chatID)
		if err != nil {
			return err
		}
		msg, found, err = s.store.FindMessage(tx, w.ConversationID, publicMessageID("assistant", w.ConversationID, chatID, executionID, callKey))
		return err
	})
	return
}

// AcknowledgeDispatch 发布已接纳交办的 Router 准备说明，不代替执行回报。
func (s *Service) AcknowledgeDispatch(ctx context.Context, chatID, executionID, publicInputID, text string) {
	if text == "" {
		return
	}
	// Reserve an internal receipt key so the model cannot collide with this post.
	if _, _, err := s.PublishWithReceipt(context.WithoutCancel(ctx), chatID, executionID, models.MainConversationDispatchAcknowledgmentKey, text, publicInputID); err != nil {
		logx.Warn("main_conversation_dispatch_ack: ", err)
	}
}

// PublishWithReceipt 在同一次持久提交中区分首次发送和同键重投。
func (s *Service) PublishWithReceipt(ctx context.Context, chatID, executionID, callKey, content, replyID string) (msg models.MainConversationMessage, replay bool, err error) {
	return s.publish(ctx, chatID, executionID, callKey, content, replyID, false, false)
}

// PublishQuotedWithReceipt 发布显式引用宿主绑定输入的发言。
func (s *Service) PublishQuotedWithReceipt(ctx context.Context, chatID, executionID, callKey, content, replyID string) (models.MainConversationMessage, bool, error) {
	if replyID == "" {
		return models.MainConversationMessage{}, false, status_error.ChatReplyTargetUnavailable
	}
	return s.publish(ctx, chatID, executionID, callKey, content, replyID, true, false)
}

// PublishReportWithReceipt 向已有交付正文关联工单报告，或提交一条新的公开消息。
func (s *Service) PublishReportWithReceipt(ctx context.Context, chatID, executionID, callKey, content, replyID string) (models.MainConversationMessage, bool, error) {
	return s.publish(ctx, chatID, executionID, callKey, content, replyID, false, true)
}

func (s *Service) publish(ctx context.Context, chatID, executionID, callKey, content, replyID string, quoteReply, reuseBody bool) (msg models.MainConversationMessage, replay bool, err error) {
	if strings.TrimSpace(executionID) == "" || strings.TrimSpace(callKey) == "" || strings.TrimSpace(content) == "" {
		return msg, false, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		w, conv, e := s.authorizedWork(tx, chatID)
		if e != nil {
			return e
		}
		if reuseBody {
			msg, e = s.store.FindPublishedBody(tx, conv.ID, chatID, executionID, content)
			if e != nil || msg.ID != "" {
				replay = msg.ID != ""
				return e
			}
		}
		msg = models.MainConversationMessage{
			ID:                publicMessageID("assistant", conv.ID, chatID, executionID, callKey),
			ConversationID:    conv.ID,
			Role:              string(constants.MessageRoleAssistant),
			Content:           content,
			ReplyToMessageID:  replyID,
			QuoteRequested:    quoteReply,
			SourceChatID:      chatID,
			SourceExecutionID: executionID,
			SourceWakerID:     w.WakerID,
		}
		// 重投先返回原收据，避免重新确认交办或按变化后的时间线决定引用。
		if saved, found, err := s.store.FindAppendReceipt(tx, msg); err != nil || found {
			msg, replay = saved, found
			return err
		}
		msg, replay, e = s.append(tx, msg, conv.SubjectType, conv.SubjectID)
		return e
	})
	return
}

func (s *Service) append(ctx context.Context, msg models.MainConversationMessage, kind, id string) (models.MainConversationMessage, bool, error) {
	if msg.IsReadablePublication() && msg.ReplyToMessageID != "" {
		// 原执行发言前先补齐对应交办，同一事务内保持提问先于答复。
		origin, err := s.validateReply(ctx, msg)
		if err != nil {
			return models.MainConversationMessage{}, false, err
		}
		if msg.QuoteRequested && origin.RemovedAt == 0 {
			msg.QuoteReply, err = s.store.HasVisibleMessageAfter(ctx, msg.ConversationID, origin.Sequence)
			if err != nil {
				return models.MainConversationMessage{}, false, err
			}
		}
	}
	saved, replay, err := s.store.Append(ctx, msg, kind, id)
	if err != nil || replay || saved.IsReadablePublication() || saved.ReplyToMessageID == "" {
		return saved, replay, err
	}
	if _, err := s.validateReply(ctx, saved); err != nil {
		return models.MainConversationMessage{}, false, err
	}
	return saved, false, nil
}

func (s *Service) validateReply(ctx context.Context, message models.MainConversationMessage) (models.MainConversationMessage, error) {
	if message.ID == message.ReplyToMessageID {
		return models.MainConversationMessage{}, status_error.ChatReplyTargetUnavailable
	}
	origin, err := s.store.Message(ctx, message.ConversationID, message.ReplyToMessageID)
	if err != nil {
		return origin, err
	}
	// 有来源的回复沿用原范围，无来源的公开回复可作为新交办的上下文。
	userReply := message.Role == string(constants.MessageRoleUser)
	transferredReply := userReply && message.SourceReadOnly
	if (!transferredReply && origin.SourceChatID != message.SourceChatID && (!userReply || origin.SourceChatID != "")) || (userReply && origin.RemovedAt != 0) {
		return origin, status_error.ChatReplyTargetUnavailable
	}
	if !origin.AdmissionPending {
		return origin, nil
	}
	if !message.IsReadablePublication() || origin.SourceExecutionID != message.SourceExecutionID {
		return origin, status_error.ChatReplyTargetUnavailable
	}
	// 到达领域发布端口的同一执行已经过宿主回合授权。
	return s.store.ConfirmUserAdmission(ctx, origin.ConversationID, origin.ID)
}
