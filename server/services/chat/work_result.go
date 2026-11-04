package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
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
	if journal, ok := p.logStore.(*conversationJournal); ok && len(records) > 1 {
		message, cursor, err := journal.ReadMessageCheckpoint(context.Background(), chatID, messageID, 0)
		if err != nil {
			return nil, err
		}
		if cursor > 0 {
			return resultFromContent(records, messageID, &message)
		}
	}
	return resultFromRecords(records, messageID)
}

// resultFromRecords 仅认真实结束回合的最终文本、错误或文件交付。
func resultFromRecords(records []eventlog.Record, messageID string) (*models.WorkResultReceipt, error) {
	return resultFromContent(records, messageID, nil)
}

func resultFromContent(records []eventlog.Record, messageID string, native *agent.StoredMessage) (*models.WorkResultReceipt, error) {
	var terminal *eventlog.Record
	var fileResult *eventlog.Record
	var fileReference *eventlog.FileChangesData
	for i := range records {
		r := &records[i]
		if r.Type == constants.ChatEventTypeFileChanges && r.MessageID == messageID {
			var reference eventlog.FileChangesData
			if json.Unmarshal(r.Data, &reference) != nil {
				return nil, status_error.ChatLogCorrupted
			}
			// 没有确认的净变化时不产生可读文件交付，采集状态不前移正文版本。
			fileResult, fileReference = nil, nil
			if reference.State != "pending" && fileResultNeedsPresentation(reference) {
				fileResult, fileReference = r, &reference
			}
		}
		if r.ExecutionID == messageID && (r.Type == constants.ChatEventTypeExecutionCompleted || r.Type == constants.ChatEventTypeExecutionFailed || r.Type == constants.ChatEventTypeExecutionCanceled) {
			terminal = r
		}
	}
	if terminal == nil {
		return nil, nil
	}
	builder := messageview.NewPublicMessageViewBuilder()
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			return nil, err
		}
	}
	if native != nil {
		if err := builder.ReplaceStoredContent(*native, terminal.Sequence, terminal.RecordedAt); err != nil {
			return nil, err
		}
	}
	messages := builder.Messages()
	for _, message := range messages {
		if message.ID != messageID || message.Inherited || message.Role != constants.MessageRoleAssistant {
			continue
		}
		readable := message.ErrorCode != "" || message.ErrorMessage != "" || fileReference != nil
		for _, part := range message.DisplayParts {
			if part.Type == constants.MessageDisplayPartText && strings.TrimSpace(part.Text) != "" {
				readable = true
			}
		}
		if !readable {
			return nil, nil
		}
		version, at := terminal.Sequence, terminal.RecordedAt
		if fileResult != nil && fileResult.Sequence > version {
			version, at = fileResult.Sequence, fileResult.RecordedAt
		}
		reference := models.WorkResultReference{ChatID: terminal.ChatID, ExecutionID: messageID, MessageID: messageID}
		if fileReference != nil {
			reference.FileChangeRecordID, reference.FileChangeRecordVersion = fileReference.RecordID, fileReference.Version
		}
		return &models.WorkResultReceipt{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: terminal.ChatID, LatestVersion: version, AvailableAt: at, Reference: reference}, nil
	}
	return nil, nil
}

// fileResultNeedsPresentation 只为确认有净变更的文件交付绑定卡片阅读身份。
func fileResultNeedsPresentation(reference eventlog.FileChangesData) bool {
	return reference.TotalFiles > 0
}

// publishResult 与查询投影使用同一短事务，阅读事实在重建中保持不变。
func (p *queryIndexStore) publishResult(ctx context.Context, receipt *models.WorkResultReceipt) error {
	if receipt == nil || p.results == nil {
		return nil
	}
	return p.results.PublishChat(ctx, receipt.Reference.ChatID, receipt.LatestVersion, receipt.AvailableAt, receipt.Reference)
}
