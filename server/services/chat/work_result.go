package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// PrepareParticipantResult 只产生引用，正式交付资格由团队在原事务中提交。
func (m *service) PrepareParticipantResult(ctx context.Context, chatID, messageID string) (*models.WorkResultReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, chatContextStatus(err)
	}
	records, err := m.logStore.ReadMessage(chatID, messageID)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Type != constants.ChatEventTypeUserMessageCreated || r.MessageID != messageID {
			continue
		}
		var data eventlog.MessageCreatedData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			logx.Errorf("decode participant result: chat_id=%s message_id=%s error=%v", chatID, messageID, err)
			return nil, status_error.ChatLogError
		}
		if data.Actor != string(constants.ChannelMessageActorMemberReport) || data.Truncated || strings.TrimSpace(data.Content) == "" {
			return nil, nil
		}
		return &models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: chatID, LatestVersion: r.Sequence, AvailableAt: r.RecordedAt, Reference: models.WorkResultReference{ChatID: chatID, MessageID: messageID}}, nil
	}
	return nil, nil
}

// prepareResult 在数据库事务外读取精确消息，不扫描或复制全部历史正文。
func (p *queryIndexStore) prepareResult(chatID, messageID string) (*models.WorkResultReceipt, error) {
	if p.results == nil || p.logStore == nil || messageID == "" {
		return nil, nil
	}
	records, err := p.logStore.ReadMessage(chatID, messageID)
	if err != nil {
		return nil, err
	}
	return resultFromRecords(records, messageID)
}

// resultFromRecords 仅认真实结束回合的最终文本、错误或文件交付。
func resultFromRecords(records []eventlog.Record, messageID string) (*models.WorkResultReceipt, error) {
	var terminal *eventlog.Record
	for i := range records {
		r := &records[i]
		if r.ExecutionID == messageID && (r.Type == constants.ChatEventTypeExecutionCompleted || r.Type == constants.ChatEventTypeExecutionFailed || r.Type == constants.ChatEventTypeExecutionCanceled) {
			terminal = r
		}
	}
	if terminal == nil {
		return nil, nil
	}
	messages, err := messageview.ReplayPublicMessages(records)
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		if message.ID != messageID || message.Inherited || message.Role != constants.MessageRoleAssistant {
			continue
		}
		readable := message.ErrorCode != "" || message.ErrorMessage != "" || len(message.FileChanges) > 0
		for _, part := range message.DisplayParts {
			if part.Type == constants.MessageDisplayPartText && strings.TrimSpace(part.Text) != "" {
				readable = true
			}
		}
		if !readable {
			return nil, nil
		}
		return &models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: terminal.ChatID, LatestVersion: terminal.Sequence, AvailableAt: terminal.RecordedAt,
			Reference: models.WorkResultReference{ChatID: terminal.ChatID, ExecutionID: messageID, MessageID: messageID}}, nil
	}
	return nil, nil
}

// publishResult 与查询投影使用同一短事务，阅读事实在重建中保持不变。
func (p *queryIndexStore) publishResult(ctx context.Context, receipt *models.WorkResultReceipt) error {
	if receipt == nil || p.results == nil {
		return nil
	}
	return p.results.PublishChat(ctx, receipt.Reference.ChatID, receipt.LatestVersion, receipt.AvailableAt, receipt.Reference)
}
