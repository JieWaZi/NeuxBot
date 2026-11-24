package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
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

// RecordManagedSkillChange 保留已提交技能变更的原聊天位置，重复回执共用同一条记录。
func (m *service) RecordManagedSkillChange(ctx context.Context, chatID, executionID, action, skillID, versionID, name string) error {
	if chatID == "" || executionID == "" || skillID == "" || strings.TrimSpace(name) == "" {
		return status_error.BadRequest
	}
	switch action {
	case "create", "revise":
		if versionID == "" {
			return status_error.BadRequest
		}
	case "uninstall", "delete":
	default:
		return status_error.BadRequest
	}
	data := eventlog.ManagedResourceChangeData{Action: action, SkillID: skillID, VersionID: versionID, Name: name}
	digest := sha256.Sum256([]byte(executionID + "\x00" + action + "\x00" + skillID + "\x00" + versionID))
	key := "skill-change:" + hex.EncodeToString(digest[:])
	return m.recordManagedResourceChange(ctx, chatID, executionID, constants.ChatEventTypeManagedSkillChanged, key, data)
}

// RecordManagedConnectorChange 按原命令回执幂等记录已提交的连接器变更。
func (m *service) RecordManagedConnectorChange(ctx context.Context, chatID, executionID, commandID, action, connectorID, ownerWakerID, name string) error {
	if chatID == "" || executionID == "" || commandID == "" || connectorID == "" || strings.TrimSpace(name) == "" {
		return status_error.BadRequest
	}
	switch action {
	case "create", "update", "detach", "delete":
	default:
		return status_error.BadRequest
	}
	data := eventlog.ManagedResourceChangeData{Action: action, ConnectorID: connectorID, OwnerWakerID: ownerWakerID, Name: name}
	return m.recordManagedResourceChange(ctx, chatID, executionID, constants.ChatEventTypeManagedConnectorChanged, "connector-change:"+commandID, data)
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
	// 资源已提交。这条权威记录在原工具超时或取消后仍可完成。
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.ChatInteractionProjectionTimeoutMillis*time.Millisecond)
	defer cancel()
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
