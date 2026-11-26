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
		bindings[i].Call = func(ctx context.Context, request capability.Request) (capability.Result, error) {
			return binding.Call(capability.WithReceiptOrigin(ctx, origin, func(receipt capability.Receipt) {
				m.projectManagedResourceChange(context.WithoutCancel(ctx), receipt)
			}), request)
		}
	}
	return bindings
}

func (m *service) projectManagedResourceChange(ctx context.Context, receipt capability.Receipt) {
	ctx, cancel := context.WithTimeout(ctx, constants.ChatInteractionProjectionTimeoutMillis*time.Millisecond)
	defer cancel()
	kind, data, err := managedResourceChangeOf(receipt)
	if err == nil {
		err = m.recordManagedResourceChange(ctx, receipt.Origin.ChatID, receipt.Origin.ExecutionID, kind, "managed-resource-command:"+receipt.ID, data)
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

func (m *service) recoverManagedResourceChanges(ctx context.Context, chatID string) {
	if m.managedCommands == nil {
		return
	}
	const pageSize = 64
	var after capability.Receipt
	for ctx.Err() == nil {
		receipts, err := m.managedCommands.ListPendingManagedChanges(ctx, chatID, after, pageSize)
		if err != nil {
			logx.Warnf("read pending managed resource commands: chat_id=%s error=%v", chatID, err)
			return
		}
		for _, receipt := range receipts {
			m.projectManagedResourceChange(ctx, receipt)
		}
		if len(receipts) < pageSize {
			return
		}
		after = receipts[len(receipts)-1]
	}
}

func (m *service) recordManagedResourceChange(ctx context.Context, chatID, executionID string, kind constants.ChatEventType, key string, data eventlog.ManagedResourceChangeData) error {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return status_error.ChatLogRecordInvalid
	}
	facts := factsToChanges([]eventlog.AppendInput{{
		Type: kind, ExecutionID: executionID, Data: marshalData(data),
	}})
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
			m.queryIndexes.indexChatCursor(chatID, []eventlog.Record{record})
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
