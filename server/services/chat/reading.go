package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// readingChanges holds the narrow projection changes prepared against a frozen log tail.
type readingChanges struct {
	// remove 是不再符合公开阅读资格的原消息身份。
	remove []string
	// rows 保存当前合格的公开消息索引行。
	rows []models.ChatReadableMessage
	// inheritedThrough 是新分叉继承的公开前缀。
	inheritedThrough int64
}

// prepareReading reads only message identities affected by the frozen tail.
func (p *queryIndexStore) prepareReading(ctx context.Context, chatID string, tail []eventlog.Record, through int64) (readingChanges, error) {
	var changes readingChanges
	if p.reading == nil || len(tail) == 0 {
		return changes, nil
	}
	affected := map[string]bool{}
	for _, row := range tail {
		switch row.Type {
		case constants.ChatEventTypeMessageInherited, constants.ChatEventTypeUserMessageCreated:
			affected[row.MessageID] = true
		case constants.ChatEventTypeAssistantMessage, constants.ChatEventTypeRuntimeEvent, constants.ChatEventTypeExecutionFailed,
			constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionCanceled:
			id := row.MessageID
			if id == "" {
				id = row.ExecutionID
			}
			affected[id] = true
		default:
			if isNativeCheckpoint(row) {
				affected[row.MessageID] = true
			}
		}
	}
	delete(affected, "")
	for id := range affected {
		if err := ctx.Err(); err != nil {
			return changes, chatContextStatus(err)
		}
		message, err := p.readPublicMessageThrough(ctx, chatID, id, through)
		if err != nil {
			return changes, err
		}
		if message == nil || !message.Readable {
			changes.remove = append(changes.remove, id)
			continue
		}
		if message.Inherited {
			changes.inheritedThrough = max(changes.inheritedThrough, message.CreatedSequence)
		}
		changes.rows = append(changes.rows, models.ChatReadableMessage{ChatID: chatID, MessageID: id, OrderSequence: message.CreatedSequence, ExecutionID: message.ExecutionID, Kind: messageview.ReadingKind(*message), CreatedAt: message.CreatedAt})
	}
	slices.SortFunc(changes.rows, func(a, b models.ChatReadableMessage) int {
		if a.OrderSequence < b.OrderSequence {
			return -1
		}
		if a.OrderSequence > b.OrderSequence {
			return 1
		}
		return 0
	})
	return changes, nil
}

// readPublicMessageThrough never applies a body newer than the requested projection boundary.
func (p *queryIndexStore) readPublicMessageThrough(ctx context.Context, chatID, messageID string, through int64) (*messageview.Message, error) {
	records, err := p.logStore.ReadMessage(chatID, messageID)
	if err != nil || len(records) < 2 {
		return nil, err
	}
	builder := messageview.NewPublicMessageViewBuilder()
	var stamp int64
	for _, r := range records {
		if r.Sequence > through {
			break
		}
		if _, err := builder.Apply(r); err != nil {
			return nil, err
		}
		stamp = r.RecordedAt
	}
	if journal, ok := p.logStore.(*conversationJournal); ok {
		stored, cursor, err := journal.ReadMessageCheckpoint(ctx, chatID, messageID, 0)
		if err != nil && !errors.Is(err, status_error.ChatMessageNotFound) {
			return nil, err
		}
		if cursor > through {
			return nil, status_error.ReadingProjectionIncomplete
		}
		if cursor > 0 {
			if err := builder.ReplaceStoredContent(stored, cursor, stamp); err != nil {
				return nil, err
			}
		}
	}
	return builder.PublicMessage(messageID), nil
}

// projectReadableMessages stores derived rows and a missing local cursor inside the Chat transaction.
func (p *queryIndexStore) projectReadableMessages(ctx context.Context, chatID string, changes readingChanges, replace bool, initial *int64) (int64, error) {
	if p.reading == nil {
		return 0, nil
	}
	latest, err := p.reading.Project(ctx, chatID, changes.remove, changes.rows, replace)
	if err != nil {
		return 0, err
	}
	if initial != nil {
		if err := p.reading.EnsureCursor(ctx, constants.LocalReaderID, chatID, *initial); err != nil {
			return 0, err
		}
	}
	return latest, nil
}

// CatchUpReading 由原会话投影负责定向追平。
func (m *service) CatchUpReading(ctx context.Context, chatID string) error {
	var generation uint64
	if m.readingRepair != nil {
		m.readingRepair.mu.Lock()
		generation = m.readingRepair.pending[chatID].generation
		m.readingRepair.mu.Unlock()
	}
	err := m.catchUpReadingProjection(ctx, chatID)
	if m.readingRepair != nil {
		if !m.readingRepair.recordOutcome(m, ctx, chatID, generation, err) && ctx.Err() == nil {
			m.registerReadingFailure(chatID, err)
		}
	}
	return err
}

// PublicReadingMessage 为精确系统提醒定位已提交公开消息。
func (m *service) PublicReadingMessage(ctx context.Context, chatID, messageID string) (*messageview.Message, error) {
	through, err := m.logStore.ReadLatestSequence(chatID)
	if err != nil {
		return nil, err
	}
	return m.queryIndexes.readPublicMessageThrough(ctx, chatID, messageID, through)
}

// appendInterruptedRecovery 先关闭未答问题，再记录 Core 重启中断的执行结果。
func (c *recoveryCoordinator) appendInterruptedRecovery(chatID, executionID string, pending []models.PendingElicitation) ([]eventlog.Record, error) {
	status := constants.ExecutionStatusCanceled
	kind := constants.ChatEventTypeExecutionCanceled
	data := eventlog.ExecutionFinishedData{Status: status, ExitCode: -1}
	if c.queryIndexes.reading != nil {
		kind = constants.ChatEventTypeExecutionFailed
		data.Status = constants.ExecutionStatusFailed
		data.ErrorCode = status_error.ReadingExecutionInterrupted.Key()
	}
	facts := make([]eventlog.AppendInput, 0, len(pending)+1)
	for _, question := range pending {
		facts = append(facts, eventlog.AppendInput{
			Type: constants.ChatEventTypeElicitationResolved, ExecutionID: executionID,
			Data: marshalData(eventlog.ElicitationResolvedData{
				ElicitationID: question.ElicitationID, ToolCallID: question.ToolCallID,
				Result: constants.ElicitationResultInvalidated,
			}),
		})
	}
	facts = append(facts, eventlog.AppendInput{Type: kind, ExecutionID: executionID, Data: marshalData(data)})
	// 大量并发 Runtime 请求可能超过单个日志批次的容量。中途失败时，重放会去除已关闭的请求。
	const recoveryBatchSize = 64
	var records []eventlog.Record
	for batch := range slices.Chunk(facts, recoveryBatchSize) {
		committed, err := c.logStore.Append(chatID, batch)
		if err != nil {
			return nil, err
		}
		records = append(records, committed...)
	}
	return records, nil
}

// readProjectionSequence validates the original Chat checkpoint before reading its fixed tail.
func readProjectionSequence(fields map[string]any) (int64, error) {
	switch value := fields["latest_sequence"].(type) {
	case int64:
		return value, nil
	case json.Number:
		return value.Int64()
	default:
		return 0, status_error.ChatLogIndexCorrupted
	}
}
