package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ManagedResourceChange 是已提交的聊天资源变更及其权威日志位置。
type ManagedResourceChange struct {
	// ManagedResourceChangeData 是变更发生时的资源公开标识。
	eventlog.ManagedResourceChangeData
	// Sequence 是变化在原聊天的游标。
	Sequence int64
	// OccurredAt 是原日志的提交时间。
	OccurredAt int64
}

// BindManagedResourceChanges 在资源变更事务中冻结来源，提交后将原回执投影到聊天。
func (m *service) BindManagedResourceChanges(chat models.Chat, executionID string, bindings []capability.Binding) []capability.Binding {
	if m.managedCommands == nil {
		return bindings
	}
	for i, binding := range bindings {
		if binding.Receipt == nil {
			continue
		}
		origin := capability.ReceiptOrigin{ChatID: chat.ID, ExecutionID: executionID, WakerID: chat.WakerID, Operation: binding.Operation}
		project := func(ctx context.Context, receipt capability.Receipt) {
			m.projectManagedResourceChange(context.WithoutCancel(ctx), receipt)
		}
		retryProjection := func(ctx context.Context, result capability.Result, err error) {
			if err != nil || origin.Operation != constants.AutomaticTaskCreateOperation || !result.Replayed {
				return
			}
			if data, ok := result.Data.(map[string]any); ok {
				project(ctx, capability.Receipt{ID: result.CommandID, Origin: origin, Data: data})
			}
		}
		bindings[i].Call = func(ctx context.Context, request capability.Request) (capability.Result, error) {
			result, err := binding.Call(capability.WithReceiptOrigin(ctx, origin, func(receipt capability.Receipt) {
				project(ctx, receipt)
			}), request)
			retryProjection(ctx, result, err)
			return result, err
		}
		if origin.Operation == constants.AutomaticTaskCreateOperation {
			bindings[i].Receipt = func(ctx context.Context, requestID string) (capability.Result, error) {
				result, err := binding.Receipt(ctx, requestID)
				retryProjection(ctx, result, err)
				return result, err
			}
		}
	}
	return bindings
}

func (m *service) projectManagedResourceChange(ctx context.Context, receipt capability.Receipt) {
	ctx, cancel := context.WithTimeout(ctx, constants.ChatInteractionProjectionTimeoutMillis*time.Millisecond)
	defer cancel()
	var err error
	if receipt.Origin.Operation == constants.AutomaticTaskCreateOperation {
		err = m.recordAutomaticTaskCreation(ctx, receipt)
	} else {
		kind, data, parseErr := managedResourceChangeOf(receipt)
		err = parseErr
		if err == nil {
			err = m.recordManagedResourceChange(ctx, receipt.Origin.ChatID, receipt.Origin.ExecutionID, kind, "managed-resource-command:"+receipt.ID, data)
		}
	}
	if err == nil || hasStatusErrorCode(err, status_error.ChatNotFound.Code()) {
		err = m.managedCommands.MarkManagedChangeProjected(ctx, receipt.ID)
	}
	if err != nil && ctx.Err() == nil {
		logx.Warnf("project managed resource command: chat_id=%s command_id=%s error=%v", receipt.Origin.ChatID, receipt.ID, err)
	}
}

func managedResourceChangeOf(receipt capability.Receipt) (constants.ChatEventType, eventlog.ManagedResourceChangeData, error) {
	origin := receipt.Origin
	name, _ := receipt.Data["name"].(string)
	data := eventlog.ManagedResourceChangeData{Name: name}
	if receipt.ID == "" || origin.ChatID == "" || origin.ExecutionID == "" || strings.TrimSpace(name) == "" {
		return "", data, status_error.ChatLogRecordInvalid
	}
	if action, ok := strings.CutPrefix(origin.Operation, "skills.packages."); ok {
		data.Action = action
		data.SkillID, _ = receipt.Data["skillId"].(string)
		data.VersionID, _ = receipt.Data["versionId"].(string)
		if data.SkillID != "" {
			return constants.ChatEventTypeManagedSkillChanged, data, nil
		}
	} else if action, ok := strings.CutPrefix(origin.Operation, "mcp.connectors."); ok {
		data.Action = action
		data.ConnectorID, _ = receipt.Data["connectorId"].(string)
		if receipt.Data["scope"] == string(constants.MCPConnectorScopePrivate) {
			data.OwnerWakerID = origin.WakerID
		}
		if data.ConnectorID != "" {
			return constants.ChatEventTypeManagedConnectorChanged, data, nil
		}
	}
	return "", data, status_error.ChatLogRecordInvalid
}

// RestoreAutomaticTaskCreationCards 只尝试恢复当前聊天少量待投影的任务卡片。
func (m *service) RestoreAutomaticTaskCreationCards(ctx context.Context, chatID string) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || m.managedCommands == nil {
		return
	}
	// 首屏是补偿入口，超出预算的回执仍由命令重试或后续恢复处理。
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	receipts, err := m.managedCommands.ListPendingManagedChanges(ctx, chatID, constants.AutomaticTaskCreateOperation, capability.Receipt{}, 4)
	if err != nil && ctx.Err() == nil {
		logx.Warnf("read pending automatic task creations: chat_id=%s error=%v", chatID, err)
	}
	for _, receipt := range receipts {
		if ctx.Err() != nil {
			return
		}
		m.projectManagedResourceChange(ctx, receipt)
	}
}

func (m *service) recoverManagedResourceChanges(ctx context.Context, chatID string) {
	if m.managedCommands == nil {
		return
	}
	const pageSize = 64
	var after capability.Receipt
	for ctx.Err() == nil {
		receipts, err := m.managedCommands.ListPendingManagedChanges(ctx, chatID, "", after, pageSize)
		if err != nil {
			if ctx.Err() == nil {
				logx.Warnf("read pending managed resource commands: chat_id=%s error=%v", chatID, err)
			}
			return
		}
		for _, receipt := range receipts {
			if ctx.Err() != nil {
				return
			}
			m.projectManagedResourceChange(ctx, receipt)
		}
		if len(receipts) < pageSize {
			return
		}
		after = receipts[len(receipts)-1]
	}
}

func (m *service) recordAutomaticTaskCreation(ctx context.Context, receipt capability.Receipt) error {
	var creation eventlog.AutomaticTaskCreation
	origin := receipt.Origin
	if err := json.Unmarshal(marshalData(receipt.Data), &creation); err != nil || receipt.ID == "" ||
		origin.ChatID == "" || origin.ExecutionID == "" || creation.TaskID == "" || creation.Name == "" ||
		creation.Schedule.Timezone == "" || creation.FirstRunAt <= 0 {
		return status_error.ChatLogRecordInvalid
	}
	return m.recordCommandProjection(ctx, origin.ChatID, "managed-resource-command:"+receipt.ID, eventlog.AppendInput{
		Type: constants.ChatEventTypeExecutionDelivery, ExecutionID: origin.ExecutionID, MessageID: origin.ExecutionID,
		Data: marshalData(eventlog.ExecutionDeliveryData{SourceID: creation.TaskID, AutomaticTaskCreation: &creation}),
	})
}

func (m *service) recordManagedResourceChange(ctx context.Context, chatID, executionID string, kind constants.ChatEventType, key string, data eventlog.ManagedResourceChangeData) error {
	return m.recordCommandProjection(ctx, chatID, key, eventlog.AppendInput{Type: kind, ExecutionID: executionID, Data: marshalData(data)})
}

func (m *service) recordCommandProjection(ctx context.Context, chatID, key string, fact eventlog.AppendInput) error {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return status_error.ChatLogRecordInvalid
	}
	facts := factsToChanges([]eventlog.AppendInput{fact})
	batch := agent.ConversationBatch{ChatID: chatID, Key: key, Changes: facts}
	for {
		version, err := journal.ReadVersion(ctx, chatID)
		if err != nil {
			return err
		}
		if version == 0 {
			return status_error.ChatNotFound
		}
		batch.ExpectedVersion = version
		receipt, err := journal.Commit(ctx, batch)
		if errors.Is(err, status_error.ChatLogVersionConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if receipt.First > version {
			record := factRecord(chatID, agent.ConversationRecord{Cursor: receipt.First, RecordedAt: receipt.RecordedAt, Change: facts[0]})
			m.queryIndexes.indexChatCursor(ctx, chatID, []eventlog.Record{record})
		}
		m.ObserveCommitted(chatID, receipt)
		return nil
	}
}

// ReadManagedResourceChanges 仅从派生定位读取原时间线上的资源变更。
func (m *service) ReadManagedResourceChanges(ctx context.Context, chatID string, fromSequence int64) ([]ManagedResourceChange, error) {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return nil, status_error.ChatLogRecordInvalid
	}
	m.recoverManagedResourceChanges(ctx, chatID)
	rows, err := journal.ReadManagedResourceChanges(ctx, chatID, fromSequence)
	if err != nil {
		return nil, err
	}
	changes := make([]ManagedResourceChange, 0, len(rows))
	for _, row := range rows {
		var data eventlog.ManagedResourceChangeData
		if row.Change.Fact == nil || json.Unmarshal(row.Change.Fact.Data, &data) != nil {
			return nil, status_error.ChatLogCorrupted
		}
		changes = append(changes, ManagedResourceChange{ManagedResourceChangeData: data, Sequence: row.Cursor, OccurredAt: row.RecordedAt})
	}
	return changes, nil
}
