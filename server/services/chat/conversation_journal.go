package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// NativeJournal is the Chat business boundary used by Thread admission and observation.
type NativeJournal interface {
	Journal
	// PrepareTurnAdmission supplies the business portion of the atomic BeforeSubmit batch.
	PrepareTurnAdmission(context.Context, StartedTurn, string) (agent.ConversationBatch, error)
	// ProjectTurnAdmission exposes the committed admission before native submission.
	ProjectTurnAdmission(context.Context, StartedTurn, agent.CommitReceipt) (AcceptedTurn, error)
	// ObserveCommitted wakes public subscriptions after Thread commits a native link.
	ObserveCommitted(string, agent.CommitReceipt)
	// BeginNativeLive 创建本轮原生助手消息的实时交付窗口。
	BeginNativeLive(string, string, int64) *NativeLiveMessage
}

var _ NativeJournal = (*service)(nil)

// chatJournalStorage keeps business projection DTOs separate from the one conversation authority.
type chatJournalStorage interface {
	Append(string, []eventlog.AppendInput) ([]eventlog.Record, error)
	Create(string, []eventlog.AppendInput) ([]eventlog.Record, error)
	ReadAfter(string, int64, int) ([]eventlog.Record, error)
	ReadAll(string) ([]eventlog.Record, error)
	ReadProjectionAfter(string, int64, int) (eventlog.ProjectionWindow, error)
	ReadMessageWindow(string, int64, int) (eventlog.MessageWindow, error)
	ReadMessage(string, string) ([]eventlog.Record, error)
	ReadMessageAt(string, int64) ([]eventlog.Record, error)
	ReadFileChangesReference(string, string) ([]eventlog.Record, error)
	ReadCompletedHistoryCandidates(string, int64, int) (eventlog.HistoryCandidateWindow, error)
	ReadExecutionIndexTail(string, int64) (eventlog.ExecutionIndexTail, error)
	ReadExecutionFacts(string, eventlog.ExecutionFactPositions) (eventlog.ExecutionWindow, error)
	ReadLatestSequence(string) (int64, error)
	MessageRecordedTimes(string, int64, int64) ([]int64, error)
	CountUserMessages(string) (int64, error)
	ListChatIDs() ([]string, error)
	ListThreadChatIDs(string) ([]string, error)
	RefreshCatalog() error
	Forget([]string)
	Delete(string) error
	FileChangesDirectory(string) (string, error)
	LogPath(string) (string, error)
	IndexPath(string) (string, error)
	Root() string
}

// conversationJournal translates Chat facts without owning any files or message bodies.
type conversationJournal struct {
	// Store is the authority shared with the native Thread.
	*conversation.Store
}

func factsToChanges(facts []eventlog.AppendInput) []agent.ConversationChange {
	changes := make([]agent.ConversationChange, 0, len(facts))
	for _, fact := range facts {
		changes = append(changes, agent.ConversationChange{Fact: &agent.ChatFact{
			Type: fact.Type, MessageID: fact.MessageID, ExecutionID: fact.ExecutionID, InvocationID: fact.InvocationID, Data: fact.Data,
		}})
	}
	return changes
}

func factRecord(chatID string, row agent.ConversationRecord) eventlog.Record {
	record := eventlog.Record{ChatID: chatID, SchemaVersion: constants.ChatLogSchemaVersion, Sequence: row.Cursor, RecordedAt: row.RecordedAt}
	if fact := row.Change.Fact; fact != nil {
		record.Type, record.MessageID, record.ExecutionID, record.InvocationID, record.Data =
			fact.Type, fact.MessageID, fact.ExecutionID, fact.InvocationID, fact.Data
	} else if message := row.Change.Message; message != nil {
		record.MessageID, record.ExecutionID = message.MessageID, message.ExecutionID
	}
	return record
}

func isNativeCheckpoint(record eventlog.Record) bool {
	return record.Type == constants.ChatEventTypeUnspecified && record.MessageID != ""
}

func (j *conversationJournal) Create(id string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
	return j.commitFacts(id, facts, true)
}

func (j *conversationJournal) Append(id string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
	return j.commitFacts(id, facts, false)
}

func (j *conversationJournal) commitFacts(id string, facts []eventlog.AppendInput, create bool) ([]eventlog.Record, error) {
	if len(facts) == 0 || create != (facts[0].Type == constants.ChatEventTypeCreated) {
		return nil, status_error.ChatLogRecordInvalid
	}
	ctx := context.Background()
	batch := agent.ConversationBatch{ChatID: id, Key: util.New(), Changes: factsToChanges(facts)}
	for {
		version, err := j.ReadVersion(ctx, id)
		if err != nil {
			return nil, err
		}
		if create && version > 0 {
			return nil, status_error.Conflict
		}
		if !create && version == 0 {
			return nil, status_error.ChatNotFound
		}
		batch.ExpectedVersion = version
		receipt, err := j.Commit(ctx, batch)
		if errors.Is(err, status_error.ChatLogVersionConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		records := make([]eventlog.Record, len(batch.Changes))
		for index, change := range batch.Changes {
			records[index] = factRecord(id, agent.ConversationRecord{Cursor: receipt.First + int64(index), RecordedAt: receipt.RecordedAt, Change: change})
		}
		return records, nil
	}
}

func (j *conversationJournal) ReadAll(id string) ([]eventlog.Record, error) {
	return j.ReadAfter(id, 0, 0)
}

func (j *conversationJournal) ReadAfter(id string, after int64, limit int) ([]eventlog.Record, error) {
	return j.readAfter(context.Background(), id, after, limit)
}

func (j *conversationJournal) readAfter(ctx context.Context, id string, after int64, limit int) ([]eventlog.Record, error) {
	if after < 0 || limit < 0 {
		return nil, status_error.ChatEventCursorInvalid
	}
	version, err := j.ReadVersion(ctx, id)
	if err != nil {
		return nil, err
	}
	if version == 0 {
		return nil, status_error.ChatNotFound.WithParams(map[string]any{"ChatID": id})
	}
	if limit > 0 {
		version = min(version, after+int64(limit))
	}
	var records []eventlog.Record
	for after < version {
		page, err := j.ReadChanges(ctx, id, after, int(min(256, version-after)))
		if err != nil {
			return nil, err
		}
		for _, row := range page.Records {
			records = append(records, factRecord(id, row))
		}
		after = page.Next
	}
	return records, nil
}

func (j *conversationJournal) ReadLatestSequence(id string) (int64, error) {
	version, err := j.ReadVersion(context.Background(), id)
	if err == nil && version == 0 {
		err = status_error.ChatNotFound.WithParams(map[string]any{"ChatID": id})
	}
	return version, err
}

func (j *conversationJournal) ReadMessage(id, messageID string) ([]eventlog.Record, error) {
	rows, err := j.ReadMessagePositions(context.Background(), id, messageID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	created, err := j.ReadAfter(id, 0, 1)
	if err != nil {
		return nil, err
	}
	return append(created, factRecords(id, rows)...), nil
}

func (j *conversationJournal) ReadMessageAt(id string, cursor int64) ([]eventlog.Record, error) {
	ctx := context.Background()
	rows, err := j.ReadPositions(ctx, id, []int64{1, cursor})
	if err != nil {
		return nil, err
	}
	anchor := rows[1].Change.Fact
	if anchor == nil {
		return nil, status_error.ChatLogIndexCorrupted
	}
	if anchor.Type == constants.ChatEventTypeExecutionCreated {
		turn, err := j.ReadTurn(ctx, id, anchor.ExecutionID)
		if err != nil {
			return nil, err
		}
		rows = append(rows[:1], turn...)
	} else {
		messageRows, err := j.ReadMessagePositions(ctx, id, anchor.MessageID)
		if err != nil {
			return nil, err
		}
		rows = append(rows[:1], messageRows...)
	}
	return factRecords(id, rows), nil
}

func factRecords(id string, rows []agent.ConversationRecord) []eventlog.Record {
	records := make([]eventlog.Record, 0, len(rows))
	for _, row := range rows {
		if row.Change.Fact != nil {
			records = append(records, factRecord(id, row))
		}
	}
	return records
}

func (j *conversationJournal) ReadFileChangesReference(id, messageID string) ([]eventlog.Record, error) {
	rows, err := j.ReadMessagePositions(context.Background(), id, messageID)
	if err != nil {
		return nil, err
	}
	created, err := j.ReadAfter(id, 0, 1)
	if err != nil {
		return nil, err
	}
	for index := len(rows) - 1; index >= 0; index-- {
		row := rows[index]
		if fact := row.Change.Fact; fact != nil &&
			(fact.Type == constants.ChatEventTypeFileChanges || fact.Type == constants.ChatEventTypeMessageInherited) {
			return append(created, factRecord(id, row)), nil
		}
	}
	return created, nil
}

func (j *conversationJournal) ReadCompletedHistoryCandidates(id string, before int64, limit int) (eventlog.HistoryCandidateWindow, error) {
	var window eventlog.HistoryCandidateWindow
	rows, next, err := j.ReadHistoryCandidates(context.Background(), agent.MessageQuery{ChatID: id, Before: before, Limit: limit})
	if err != nil {
		return window, err
	}
	window.NextBeforeSequence, window.HasMore = next, next > 0
	for _, row := range rows {
		window.Resources = append(window.Resources, eventlog.HistoryCandidate{MessageID: row.MessageID, UserMessageID: row.UserMessageID, Sequence: row.Cursor, Inherited: row.Inherited})
	}
	return window, nil
}

func (j *conversationJournal) MessageRecordedTimes(id string, start, end int64) ([]int64, error) {
	return j.ReadMessageActivity(context.Background(), id, start, end)
}

func (j *conversationJournal) CountUserMessages(id string) (int64, error) {
	rows, err := j.ReadUserInputs(context.Background(), id)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]struct{})
	for _, row := range rows {
		fact := row.Change.Fact
		var data eventlog.MessageCreatedData
		if fact == nil || json.Unmarshal(fact.Data, &data) != nil {
			return 0, status_error.ChatLogCorrupted
		}
		if data.Role == constants.MessageRoleUser && data.SourceWakerID == "" &&
			(data.Actor == "" || data.Actor == string(constants.ChannelMessageActorUser)) {
			seen[fact.MessageID] = struct{}{}
		}
	}
	return int64(len(seen)), nil
}

func (j *conversationJournal) ReadExecutionFacts(id string, positions eventlog.ExecutionFactPositions) (eventlog.ExecutionWindow, error) {
	window := eventlog.ExecutionWindow{LatestSequence: positions.LatestSequence, StartSequence: positions.StartSequence}
	cursors := append([]int64{1}, positions.LifecycleSequences...)
	for _, cursor := range []int64{positions.StartSequence} {
		if cursor > 0 {
			cursors = append(cursors, cursor)
		}
	}
	slices.Sort(cursors)
	rows, err := j.ReadPositions(context.Background(), id, slices.Compact(cursors))
	window.Records = factRecords(id, rows)
	return window, err
}

func (j *conversationJournal) ReadExecutionIndexTail(id string, after int64) (eventlog.ExecutionIndexTail, error) {
	var tail eventlog.ExecutionIndexTail
	page, err := j.ReadExecutionProjection(context.Background(), id, after)
	if err != nil {
		return tail, err
	}
	tail.LatestSequence = page.Version
	starts := make(map[string]int64)
	for _, record := range factRecords(id, page.Records) {
		switch record.Type {
		case constants.ChatEventTypeExecutionCreated:
			starts[record.ExecutionID] = record.Sequence
			tail.Records = append(tail.Records, record)
		case constants.ChatEventTypeUserMessageCreated, constants.ChatEventTypeMessageInherited:
			tail.Records = append(tail.Records, record)
		}
		start := starts[record.ExecutionID]
		if start == 0 || record.Sequence <= after {
			continue
		}
		position := eventlog.ExecutionIndexPosition{
			Sequence: record.Sequence, ExecutionStartSequence: start,
			Lifecycle: record.Type == constants.ChatEventTypeExecutionStarted || record.Type == constants.ChatEventTypeExecutionCompleted ||
				record.Type == constants.ChatEventTypeExecutionCanceled || record.Type == constants.ChatEventTypeExecutionFailed,
		}

		if position.Lifecycle {
			tail.Positions = append(tail.Positions, position)
		}
	}
	return tail, nil
}

// PrepareTurnAdmission hands Thread only the already authorized business facts.
func (m *service) PrepareTurnAdmission(ctx context.Context, turn StartedTurn, key string) (agent.ConversationBatch, error) {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok || key == "" || len(turn.Facts) == 0 || turn.NewChat != (turn.Facts[0].Type == constants.ChatEventTypeCreated) {
		return agent.ConversationBatch{}, status_error.ChatLogRecordInvalid
	}
	version, err := journal.ReadVersion(ctx, turn.Chat.ID)
	if err != nil {
		return agent.ConversationBatch{}, err
	}
	if turn.NewChat && version != 0 {
		return agent.ConversationBatch{}, status_error.Conflict
	}
	if !turn.NewChat && version == 0 {
		return agent.ConversationBatch{}, status_error.ChatNotFound
	}
	return agent.ConversationBatch{ChatID: turn.Chat.ID, Key: key, ExpectedVersion: version, Changes: factsToChanges(turn.Facts)}, nil
}

// ProjectTurnAdmission exposes the durable business facts after Thread writes the admission.
func (m *service) ProjectTurnAdmission(ctx context.Context, turn StartedTurn, receipt agent.CommitReceipt) (AcceptedTurn, error) {
	if receipt.First < 1 || len(turn.Facts) == 0 || receipt.Version-receipt.First+1 != int64(len(turn.Facts))+2 {
		return AcceptedTurn{}, status_error.ChatLogRecordInvalid
	}
	chat, execution := turn.Chat, turn.Execution
	chat.LatestSequence, chat.UpdatedAt = receipt.Version, receipt.RecordedAt
	if turn.NewChat {
		chat.CreatedAt = receipt.RecordedAt
	}
	chat.ActiveExecutionID, chat.LastAccountID, chat.LastRuntimeID = execution.ID, execution.AccountID, execution.RuntimeID
	chat.LastModel, chat.LastReasoning, chat.LastPermissionLevel = execution.Model, execution.Reasoning, execution.PermissionLevel
	chat.LastFast = execution.Fast
	chat.LastContextWindow = execution.ContextWindow
	execution.Status, execution.UpdatedAt = constants.ExecutionStatusRunning, receipt.RecordedAt
	execution.Revision++
	m.queryIndexes.indexStartedMessage(context.WithoutCancel(ctx), turn.NewChat, chat, execution, receipt.First-1)
	records := make([]eventlog.Record, 0, len(turn.Facts))
	for index, change := range factsToChanges(turn.Facts) {
		records = append(records, factRecord(chat.ID, agent.ConversationRecord{Cursor: receipt.First + int64(index), RecordedAt: receipt.RecordedAt, Change: change}))
	}
	m.eventCommitter.publishCommitted(records)
	m.ObserveCommitted(chat.ID, receipt)
	return AcceptedTurn{Chat: chat, Execution: execution}, nil
}

// ObserveCommitted wakes active subscriptions without reading or copying native bodies.
func (m *service) ObserveCommitted(chatID string, receipt agent.CommitReceipt) {
	if receipt.Version > 0 {
		m.subscriptions.publishCheckpoint(chatID, receipt.Version)
	}
}

func (j *conversationJournal) ReadProjectionAfter(id string, after int64, limit int) (eventlog.ProjectionWindow, error) {
	var window eventlog.ProjectionWindow
	prefix := after
	if after > 0 {
		start, err := j.ReadProjectionPrefix(context.Background(), id, after)
		if err != nil {
			return window, err
		}
		if start > 0 {
			prefix = start - 1
		}
	}
	records, err := j.ReadAfter(id, prefix, boundedTail(after-prefix, limit))
	if err != nil {
		return window, err
	}
	if prefix > 0 {
		created, err := j.ReadAfter(id, 0, 1)
		if err != nil {
			return window, err
		}
		records = append(created, records...)
	}
	window.Records = records
	window.LatestSequence = after
	if len(records) > 0 {
		window.LatestSequence = max(after, records[len(records)-1].Sequence)
	}
	return window, nil
}

func boundedTail(prefix int64, limit int) int {
	if limit == 0 {
		return 0
	}
	return int(prefix) + limit
}

func (j *conversationJournal) ReadMessageWindow(id string, before int64, limit int) (eventlog.MessageWindow, error) {
	page, err := j.Store.ReadMessages(context.Background(), agent.MessageQuery{ChatID: id, Before: before, Limit: limit})
	if err != nil || len(page.Messages) == 0 {
		return eventlog.MessageWindow{}, err
	}
	return j.messageWindow(id, before, page)
}

func (j *conversationJournal) messageWindow(id string, before int64, page agent.MessagePage) (eventlog.MessageWindow, error) {
	window := eventlog.MessageWindow{LatestSequence: page.Version, NextBeforeSequence: page.Before, HasMore: page.Before > 0}
	if len(page.Messages) == 0 {
		return window, nil
	}
	start := page.Messages[0].Cursor
	end := page.Version
	if before > 0 {
		end = min(end, before-1)
	}
	records, err := j.ReadAfter(id, start-1, int(end-start+1))
	if err != nil {
		return window, err
	}
	created, err := j.ReadAfter(id, 0, 1)
	if err != nil {
		return window, err
	}
	window.Records = append(created, records...)
	if before > 0 {
		ids := make([]string, 0, len(page.Messages))
		for _, message := range page.Messages {
			ids = append(ids, message.ID)
		}
		late, err := j.ReadLateMessageFacts(context.Background(), id, ids, end, page.Version)
		if err != nil {
			return window, err
		}
		for _, record := range factRecords(id, late) {
			if record.Type == constants.ChatEventTypeFileChanges {
				window.Records = append(window.Records, record)
			}
		}
	}
	return window, nil
}

// nativeOutcome resolves the canonical result consumed by this business execution.
func (j *conversationJournal) nativeOutcome(ctx context.Context, id, executionID string) (*agent.NativeResult, error) {
	if executionID == "" {
		return nil, nil
	}
	key, err := j.ReadLastConsumedResult(ctx, id, executionID)
	if err != nil || key == nil {
		return nil, err
	}
	result, err := j.ReadNativeResultMetadata(ctx, *key)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (j *conversationJournal) nativeHistory(ctx context.Context, id string, before int64, limit int) (MessageHistoryPage, error) {
	stored, err := j.Store.ReadMessages(ctx, agent.MessageQuery{ChatID: id, Before: before, Limit: limit})
	if err != nil {
		return MessageHistoryPage{}, err
	}
	window, err := j.messageWindow(id, before, stored)
	if err != nil {
		return MessageHistoryPage{}, err
	}
	builder := messageview.NewPublicMessageViewBuilder()
	if before == 0 {
		binding, err := j.ReadBinding(ctx, id)
		if err != nil {
			return MessageHistoryPage{}, err
		}
		if binding != nil && !binding.Cleared {
			builder.SetCurrentForkSession(binding.Session.SessionID)
		}
	}
	nativeCursors := make(map[string]eventlog.Record)
	for _, record := range window.Records {
		if _, err := builder.Apply(record); err != nil {
			return MessageHistoryPage{}, err
		}
		if isNativeCheckpoint(record) {
			nativeCursors[record.MessageID] = record
		}
	}
	for _, message := range stored.Messages {
		checkpoint, linked := nativeCursors[message.ID]
		if linked {
			if err := builder.ReplaceStoredContent(message, checkpoint.Sequence, checkpoint.RecordedAt); err != nil {
				return MessageHistoryPage{}, err
			}
		} else if !message.Complete || message.Role != constants.MessageRoleAssistant {
			continue
		}
		usage, err := j.nativeOutcome(ctx, id, message.ExecutionID)
		if err != nil {
			return MessageHistoryPage{}, err
		}
		builder.SetNativeResult(message.ID, usage)
	}
	result := MessageHistoryPage{
		Resources: builder.Messages(), LatestSequence: stored.Version, NextBeforeSequence: stored.Before,
		HasMore: stored.Before > 0, ForkLatestExecutionID: builder.LatestForkExecutionID(),
	}
	if before > 0 {
		result.LatestSequence = 0
		result.ForkLatestExecutionID = ""
	}
	return result, nil
}

// replayPublic reads durable checkpoints alongside the same business event projection.
func (m *service) replayPublic(ctx context.Context, id string, records []eventlog.Record, after int64) (*eventstream.Adapter, []PublicEvent, error) {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		adapter, err := eventstream.ReplayAfter(records, after)
		if err != nil {
			return nil, nil, err
		}
		return adapter, publicEventsFromStream(adapter.EventsAfter(after, 0)), nil
	}
	adapter := eventstream.NewAdapter()
	adapter.ReleaseHistory()
	events := make([]PublicEvent, 0)
	for _, row := range records {
		projected, visible, err := journal.applyPublic(ctx, adapter, row)
		if err != nil {
			return nil, nil, err
		}
		if visible && row.Sequence > after {
			events = append(events, publicEventFromStream(projected))
		}
	}
	return adapter, events, nil
}

func (j *conversationJournal) applyPublic(ctx context.Context, adapter *eventstream.Adapter, row eventlog.Record) (eventstream.PublicEvent, bool, error) {
	if isNativeCheckpoint(row) {
		stored, checkpoint, err := j.ReadMessageCheckpoint(ctx, row.ChatID, row.MessageID, row.Sequence)
		if err != nil {
			return eventstream.PublicEvent{}, false, err
		}
		if checkpoint != row.Sequence {
			// A newer checkpoint already subsumes this body. Advance only with the real winning cursor.
			return eventstream.PublicEvent{}, false, nil
		}
		projected, err := adapter.ApplyNativeCheckpoint(row, stored)
		projected.Type = constants.ChatEventTypeAssistantMessage
		return projected, projected.Message != nil, err
	}
	if row.Type == constants.ChatEventTypeUnspecified {
		return eventstream.PublicEvent{}, false, nil
	}
	switch row.Type {
	case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
		outcome, err := j.nativeOutcome(ctx, row.ChatID, row.ExecutionID)
		if err != nil {
			return eventstream.PublicEvent{}, false, err
		}
		// Canonical native outcomes provide the only persisted usage evidence.
		if outcome != nil {
			binding, err := j.ReadBinding(ctx, row.ChatID)
			if err != nil {
				return eventstream.PublicEvent{}, false, err
			}
			sessionID := ""
			if binding != nil && !binding.Cleared {
				sessionID = binding.Session.SessionID
			}
			adapter.SetCurrentForkSession(sessionID)
			projected, err := adapter.ApplyNativeTerminal(row, outcome)
			return projected, true, err
		}
	}
	projected, err := adapter.Apply(row)
	return projected, true, err
}
