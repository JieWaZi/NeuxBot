package conversation

import (
	"cmp"
	"context"
	"path/filepath"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"gorm.io/gorm"
)

// Root returns the managed layout root used by this store.
func (s *Store) Root() string { return s.root }

// RefreshCatalog discovers aggregates restored from staging.
func (s *Store) RefreshCatalog() error { return s.catalog.Refresh() }

// ListChatIDs discovers managed public conversation logs.
func (s *Store) ListChatIDs() ([]string, error) {
	if err := s.RefreshCatalog(); err != nil {
		return nil, err
	}
	return s.catalog.ListChatIDs(), nil
}

// ListThreadChatIDs discovers the member chats owned by one team aggregate.
func (s *Store) ListThreadChatIDs(threadID string) ([]string, error) {
	return s.catalog.ListThreadChatIDs(threadID)
}

// Forget invalidates derived locations after their aggregates were staged for deletion.
func (s *Store) Forget(chatIDs []string) {
	for _, id := range chatIDs {
		slot := s.chatSlot(id)
		slot.mu.Lock()
		if path, err := s.LogPath(id); err == nil {
			if relative, err := filepath.Rel(s.root, path); err == nil {
				s.indexes.delete(context.Background(), relative)
			}
		}
		s.catalog.Remove(id)
		slot.mu.Unlock()
	}
}

// ReadPositions reads selected business and message-reference records without loading bodies.
func (s *Store) ReadPositions(ctx context.Context, id string, cursors []int64) ([]agent.ConversationRecord, error) {
	r, err := s.openChat(ctx, id)
	if err != nil {
		return nil, err
	}
	defer r.close()
	selected, err := r.index.positions(cursors)
	if err != nil {
		return nil, err
	}
	rows := make([]rowIndex, 0, len(cursors))
	for _, cursor := range cursors {
		pos, ok := selected[cursor]
		if !ok {
			return nil, status_error.ChatLogIndexCorrupted
		}
		rows = append(rows, pos)
	}
	return readSelected(ctx, r, rows)
}

// ReadMessagePositions reads the start and metadata for one message using its anchor.
func (s *Store) ReadMessagePositions(ctx context.Context, id, messageID string) ([]agent.ConversationRecord, error) {
	r, err := s.openChat(ctx, id)
	if err != nil {
		return nil, err
	}
	defer r.close()
	anchor, err := r.index.anchor(messageID)
	if err != nil || anchor.Cursor == 0 {
		return nil, err
	}
	groups, err := r.index.forMessages([]int64{anchor.Ref})
	if err != nil {
		return nil, err
	}
	return readSelected(ctx, r, groups[anchor.Ref])
}

// ReadMessageActivity reads only recorded times for message creations in the requested interval.
func (s *Store) ReadMessageActivity(ctx context.Context, id string, start, end int64) ([]int64, error) {
	r, err := s.openChat(ctx, id)
	if err != nil {
		return nil, err
	}
	defer r.close()
	var times []int64
	err = r.index.query(func(db *gorm.DB) error {
		return db.Model(&rowIndex{}).Where("stream = ? AND kind IN ? AND at BETWEEN ? AND ?", r.index.coverage.Stream,
			[]indexKind{indexExecutionCreated, indexUserMessage}, start, end).Order("cursor").Pluck("at", &times).Error
	})
	return times, err
}

// ReadUserInputs reads user input facts without reading assistant output or tool state.
func (s *Store) ReadUserInputs(ctx context.Context, id string) ([]agent.ConversationRecord, error) {
	r, err := s.openChat(ctx, id)
	if err != nil {
		return nil, err
	}
	defer r.close()
	var rows []rowIndex
	err = r.index.query(func(db *gorm.DB) error {
		return db.Where("stream = ? AND kind = ?", r.index.coverage.Stream, indexUserMessage).Order("cursor").Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return readSelected(ctx, r, rows)
}

// ReadExecutionProjection reads only metadata in the uncovered tail and the starts needed to index it.
func (s *Store) ReadExecutionProjection(ctx context.Context, id string, after int64) (agent.ChangePage, error) {
	r, err := s.openChat(ctx, id)
	if err != nil {
		return agent.ChangePage{}, err
	}
	defer r.close()
	var rows []rowIndex
	err = r.index.query(func(db *gorm.DB) error {
		return db.Where("stream = ? AND cursor > ? AND (kind IN ? OR (kind = ? AND execution_ref <> 0))", r.index.coverage.Stream, after,
			[]indexKind{indexExecutionCreated, indexExecutionCompleted, indexExecutionEnded, indexInherited, indexUserMessage}, indexOther).
			Order("cursor").Find(&rows).Error
	})
	if err != nil {
		return agent.ChangePage{}, err
	}
	var before []int64
	for _, row := range rows {
		if row.ExecutionRef > 0 && row.ExecutionRef <= after {
			before = append(before, row.ExecutionRef)
		}
	}
	slices.Sort(before)
	starts, err := r.index.positions(slices.Compact(before))
	if err != nil {
		return agent.ChangePage{}, err
	}
	for _, row := range starts {
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b rowIndex) int { return cmp.Compare(a.Cursor, b.Cursor) })
	records, err := readSelected(ctx, r, rows)
	return agent.ChangePage{Records: records, Version: r.index.version()}, err
}

// ReadProjectionPrefix finds the relevant open execution start at a reconnect cursor.
func (s *Store) ReadProjectionPrefix(ctx context.Context, id string, after int64) (int64, error) {
	r, err := s.openChat(ctx, id)
	if err != nil {
		return 0, err
	}
	defer r.close()
	var row rowIndex
	err = r.index.query(func(db *gorm.DB) error {
		return db.Where("stream = ? AND cursor <= ? AND kind IN ?", r.index.coverage.Stream, after,
			[]indexKind{indexExecutionCreated, indexExecutionCompleted, indexExecutionEnded}).
			Order("cursor DESC").Limit(1).Find(&row).Error
	})
	if row.Kind == indexExecutionCreated {
		return row.Cursor, err
	}
	return 0, err
}

func readSelected(ctx context.Context, r chatReader, rows []rowIndex) ([]agent.ConversationRecord, error) {
	result := make([]agent.ConversationRecord, 0, len(rows))
	for _, row := range rows {
		record, err := r.record(ctx, row)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

// ReadNativeResultMetadata reads canonical terminal evidence without loading its body.
func (s *Store) ReadNativeResultMetadata(ctx context.Context, key agent.NativeResultKey) (agent.NativeResult, error) {
	terminal, err := s.readNativeTerminal(ctx, key)
	return terminal.Result, err
}

// ReadLastConsumedResult locates the last proven native outcome of one business execution.
func (s *Store) ReadLastConsumedResult(ctx context.Context, id, executionID string) (*agent.NativeResultKey, error) {
	r, err := s.openChat(ctx, id)
	if err != nil {
		return nil, err
	}
	defer r.close()
	var locator executionLocator
	var consumed rowIndex
	err = r.index.query(func(db *gorm.DB) error {
		if err := db.Where("stream = ? AND key = ?", r.index.coverage.Stream, indexIdentity(executionID)).
			Limit(1).Find(&locator).Error; err != nil || locator.Ref == 0 {
			return err
		}
		return db.Where("stream = ? AND execution_ref <> 0 AND execution_ref = ? AND kind = ?",
			r.index.coverage.Stream, locator.Ref, indexConsumption).Order("cursor DESC").Limit(1).Find(&consumed).Error
	})
	if err != nil || consumed.Cursor == 0 {
		return nil, err
	}
	record, err := r.record(ctx, consumed)
	if err != nil {
		return nil, err
	}
	if record.Change.Consumption == nil {
		return nil, status_error.ChatLogCorrupted
	}
	return &record.Change.Consumption.Result, nil
}

// ReadLateMessageFacts finds metadata for selected messages beyond their page's time window.
func (s *Store) ReadLateMessageFacts(ctx context.Context, id string, ids []string, after, through int64) ([]agent.ConversationRecord, error) {
	if len(ids) == 0 || through <= after {
		return nil, nil
	}
	if len(ids) > maxPageSize {
		return nil, status_error.ChatMessageCursorInvalid
	}
	r, err := s.openChat(ctx, id)
	if err != nil {
		return nil, err
	}
	defer r.close()
	keys := make([]indexIdentity, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, indexIdentity(id))
	}
	var rows []rowIndex
	err = r.index.query(func(db *gorm.DB) error {
		var refs []int64
		if err := db.Model(&messageAnchor{}).Where("stream = ? AND message IN ?", r.index.coverage.Stream, keys).Pluck("ref", &refs).Error; err != nil {
			return err
		}
		return db.Where("stream = ? AND message_ref <> 0 AND message_ref IN ? AND cursor > ? AND cursor <= ? AND kind = ?",
			r.index.coverage.Stream, refs, after, through, indexOther).Order("cursor").Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return readSelected(ctx, r, rows)
}
