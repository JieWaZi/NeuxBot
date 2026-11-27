package mainconversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func publicMessageID(parts ...string) string {
	// 字符串数组的 JSON 边界可区分任意调用键和用户输入中的分隔符。
	data, _ := json.Marshal(parts)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:16])
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
	if strings.TrimSpace(requestID) == "" || strings.TrimSpace(content) == "" || commit == nil {
		return msg, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		w, e := s.Source(tx, kind, id, chatID)
		if e != nil {
			return e
		}
		msg = models.MainConversationMessage{
			ID:                    UserMessageID(w.ConversationID, requestID),
			ConversationID:        w.ConversationID,
			Role:                  string(constants.MessageRoleUser),
			AdmissionPending:      true,
			AdmissionSubmissionID: requestID,
			Content:               content,
			ReplyToMessageID:      replyID,
			SourceChatID:          chatID,
			SourceMessageID:       sourceMessageID,
			SourceExecutionID:     sourceExecutionID,
			RequestedDirectory:    requestedDirectory,
			InputDigest:           inputDigest,
		}
		msg, _, e = s.append(tx, msg, kind, id)
		return e
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

// PublishWithReceipt 在同一次持久提交中区分首次发送和同键重投。
func (s *Service) PublishWithReceipt(ctx context.Context, chatID, executionID, callKey, content, replyID string) (msg models.MainConversationMessage, replay bool, err error) {
	if strings.TrimSpace(executionID) == "" || strings.TrimSpace(callKey) == "" || strings.TrimSpace(content) == "" {
		return msg, false, status_error.BadRequest
	}
	err = s.tx.Transaction(ctx, func(tx context.Context) error {
		w, conv, e := s.authorizedWork(tx, chatID)
		if e != nil {
			return e
		}
		msg = models.MainConversationMessage{
			ID:                publicMessageID("assistant", conv.ID, chatID, executionID, callKey),
			ConversationID:    conv.ID,
			Role:              "assistant",
			Content:           content,
			ReplyToMessageID:  replyID,
			SourceChatID:      chatID,
			SourceExecutionID: executionID,
			SourceWakerID:     w.WakerID,
		}
		msg, replay, e = s.append(tx, msg, conv.SubjectType, conv.SubjectID)
		return e
	})
	return
}

func (s *Service) append(ctx context.Context, msg models.MainConversationMessage, kind, id string) (models.MainConversationMessage, bool, error) {
	if msg.IsReadablePublication() && msg.ReplyToMessageID != "" {
		// 原执行发言前先补齐对应交办，同一事务内保持提问先于答复。
		if err := s.validateReply(ctx, msg); err != nil {
			return models.MainConversationMessage{}, false, err
		}
	}
	saved, replay, err := s.store.Append(ctx, msg, kind, id)
	if err != nil || replay || saved.IsReadablePublication() || saved.ReplyToMessageID == "" {
		return saved, replay, err
	}
	if err := s.validateReply(ctx, saved); err != nil {
		return models.MainConversationMessage{}, false, err
	}
	return saved, false, nil
}

func (s *Service) validateReply(ctx context.Context, message models.MainConversationMessage) error {
	if message.ID == message.ReplyToMessageID {
		return status_error.ChatReplyTargetUnavailable
	}
	origin, err := s.store.Message(ctx, message.ConversationID, message.ReplyToMessageID)
	if err != nil {
		return err
	}
	// 回复沿用原工作来源，不能借引用跨项目派发或公开未接纳的交办。
	if origin.SourceChatID != message.SourceChatID || (message.Role == string(constants.MessageRoleUser) && origin.RemovedAt != 0) {
		return status_error.ChatReplyTargetUnavailable
	}
	if !origin.AdmissionPending {
		return nil
	}
	if !message.IsReadablePublication() || origin.SourceExecutionID != message.SourceExecutionID {
		return status_error.ChatReplyTargetUnavailable
	}
	// 到达领域发布端口的同一执行已经过宿主回合授权。
	_, err = s.store.ConfirmUserAdmission(ctx, origin.ConversationID, origin.ID)
	return err
}
