package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// MainConversationSourceModelChange 是来源时间线中已保存的模型设置。
type MainConversationSourceModelChange struct {
	// 变更游标
	Sequence int64 `json:"sequence"`
	// 提交时间
	CreatedAt int64 `json:"createdAt"`
	// 操作人
	Actor string `json:"actor"`
	// 原模型
	FromModel string `json:"fromModel,omitempty"`
	// 新模型
	ToModel string `json:"toModel"`
}

// sourceModelSelection 冻结一次显式选择及当时有权复用它的本地运行配置。
type sourceModelSelection struct {
	// Actor 说明该选择来自本机用户。
	Actor string `json:"actor"`
	// RuntimeID 是模型所属的 Runtime。
	RuntimeID string `json:"runtimeId"`
	// AccountID 是模型所属的归一化本机账号。
	AccountID string `json:"accountId"`
	// FromModel 是选择前在当前配置下有效的模型。
	FromModel string `json:"fromModel,omitempty"`
	// ToModel 是用户明确选择的下轮模型。
	ToModel string `json:"toModel"`
}

func sourceModelAccount(id string) string {
	if id == "" {
		return constants.AiAccountSystemID
	}
	return id
}

func (s sourceModelSelection) appliesTo(source models.Chat) bool {
	return s.RuntimeID == source.LastRuntimeID && s.AccountID == sourceModelAccount(source.LastAccountID)
}

func (s sourceModelSelection) project(seq, at int64) MainConversationSourceModelChange {
	return MainConversationSourceModelChange{
		Sequence: seq, CreatedAt: at, Actor: s.Actor, FromModel: s.FromModel, ToModel: s.ToModel,
	}
}

func readSourceModelSelection(row agent.ConversationRecord) (sourceModelSelection, error) {
	var state sourceModelSelection
	fact := row.Change.Fact
	if fact == nil || fact.Type != constants.ChatEventTypeSourceModelSelected ||
		json.Unmarshal(fact.Data, &state) != nil || state.Actor != "user" ||
		state.RuntimeID == "" || state.AccountID == "" || state.ToModel == "" {
		return state, status_error.ChatLogCorrupted
	}
	return state, nil
}

// SetMainConversationSourceModel 将配置和审计作为同一权威事实提交。
func (m *service) SetMainConversationSourceModel(ctx context.Context, chatID, model string) (string, *MainConversationSourceModelChange, error) {
	model = strings.TrimSpace(model)
	if model == "" || len(model) > 500 || strings.ContainsFunc(model, unicode.IsControl) {
		return "", nil, status_error.BadRequest
	}
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return "", nil, status_error.ChatLogError
	}
	release, err := chatstorage.AcquireOperation(m.storage.Root(), chatID)
	if err != nil {
		return "", nil, err
	}
	defer release()
	key := util.New()
	for {
		version, err := journal.ReadVersion(ctx, chatID)
		if err != nil {
			return "", nil, err
		}
		summary, err := m.GetChatSummary(ctx, chatID)
		if err != nil {
			return "", nil, err
		}
		source := summary.Chat
		if version == 0 || !source.HiddenFromWakerHistory {
			return "", nil, status_error.ChatNotFound
		}
		if source.LastRuntimeID == "" {
			return "", nil, status_error.BadRequest
		}
		selected, err := m.ReadMainConversationSourceModel(ctx, source)
		if err != nil {
			return "", nil, err
		}
		before := selected
		if before == "" {
			before = source.LastModel
		}
		if before == model {
			return model, nil, nil
		}
		data := sourceModelSelection{
			Actor: "user", RuntimeID: source.LastRuntimeID, AccountID: sourceModelAccount(source.LastAccountID),
			FromModel: before, ToModel: model,
		}
		fact := eventlog.AppendInput{Type: constants.ChatEventTypeSourceModelSelected, Data: marshalData(data)}
		changes := factsToChanges([]eventlog.AppendInput{fact})
		receipt, err := journal.Commit(ctx, agent.ConversationBatch{
			ChatID: chatID, Key: key, ExpectedVersion: version, Changes: changes,
		})
		if errors.Is(err, status_error.ChatLogVersionConflict) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		record := factRecord(chatID, agent.ConversationRecord{Cursor: receipt.First, RecordedAt: receipt.RecordedAt, Change: changes[0]})
		m.queryIndexes.indexChatCursor(chatID, []eventlog.Record{record})
		m.ObserveCommitted(chatID, receipt)
		change := data.project(receipt.First, receipt.RecordedAt)
		return model, &change, nil
	}
}

// ReadMainConversationSourceModel 只读最后一次显式选择，在配置一致时返回它。
func (m *service) ReadMainConversationSourceModel(ctx context.Context, source models.Chat) (string, error) {
	if source.LastRuntimeID == "" {
		return "", nil
	}
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return "", status_error.ChatLogError
	}
	rows, err := journal.ReadSourceModelChanges(ctx, source.ID, 0, 0, 1)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	selection, err := readSourceModelSelection(rows[0])
	if err != nil || !selection.appliesTo(source) {
		return "", err
	}
	return selection.ToModel, nil
}

// ReadMainConversationSourceModelChanges 返回当前来源页内最多一百条专用设置事实。
func (m *service) ReadMainConversationSourceModelChanges(ctx context.Context, chatID string, from, before int64) ([]MainConversationSourceModelChange, error) {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return nil, status_error.ChatLogError
	}
	rows, err := journal.ReadSourceModelChanges(ctx, chatID, from, before, 100)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	slices.Reverse(rows)
	changes := make([]MainConversationSourceModelChange, 0, len(rows))
	for _, row := range rows {
		selection, err := readSourceModelSelection(row)
		if err != nil {
			return nil, err
		}
		changes = append(changes, selection.project(row.Cursor, row.RecordedAt))
	}
	return changes, nil
}
