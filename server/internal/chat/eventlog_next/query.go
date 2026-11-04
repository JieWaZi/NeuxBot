package eventlog_next

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
)

const maxPageSize = 256

// chatReader holds the serialization boundary while reading indexed data.
type chatReader struct {
	// file is the authoritative stream for this read.
	file *os.File
	// index is the stable set of committed positions for this read.
	index *streamIndex
	// release unlocks the stream after file reads complete.
	release func()
}

func (r chatReader) close() {
	_ = r.file.Close()
	r.release()
}

func (s *Store) openChat(ctx context.Context, id string) (chatReader, error) {
	var empty chatReader
	slot := s.chatSlot(id)
	slot.mu.Lock()
	path, err := s.LogPath(id)
	if err != nil {
		slot.mu.Unlock()
		return empty, err
	}
	idx, err := slot.load(ctx, path, true)
	if err != nil {
		slot.mu.Unlock()
		return empty, err
	}
	if idx.version() == 0 {
		slot.mu.Unlock()
		return empty, status_error.ChatNotFound.WithParams(map[string]any{"ChatID": id})
	}
	file, err := os.Open(path)
	if err != nil {
		slot.mu.Unlock()
		return empty, status_error.ChatLogError
	}
	return chatReader{file: file, index: idx, release: slot.mu.Unlock}, nil
}

// recordReader reuses only the relevant legacy decode scope across selected cursors.
type recordReader struct {
	// stream is the durable read snapshot.
	stream chatReader
	// legacy is the current legacy execution's patch context.
	legacy legacyScope
	// cursor is the last decoded legacy row.
	cursor int64
	// known caches only locations selected by this query or a legacy execution dependency.
	known map[int64]rowIndex
}

func newRecordReader(stream chatReader, rows []rowIndex) *recordReader {
	known := make(map[int64]rowIndex, len(rows))
	for _, row := range rows {
		known[row.Cursor] = row
	}
	return &recordReader{stream: stream, known: known}
}

func (r *recordReader) prepareLegacy() error {
	windows := make(map[int64]int64)
	for _, row := range r.known {
		if row.LegacyStart > 0 && row.LegacyStart < row.Cursor {
			windows[row.LegacyStart] = max(windows[row.LegacyStart], row.Cursor)
		}
	}
	var needed []cursorSpan
	for first, last := range windows {
		for cursor := first; cursor <= last; cursor++ {
			if _, ok := r.known[cursor]; !ok {
				needed = append(needed, cursorSpan{first: cursor, last: last})
				break
			}
		}
	}
	rows, err := r.stream.index.legacyPositions(needed)
	if err != nil {
		return err
	}
	for _, row := range rows {
		r.known[row.Cursor] = row
	}
	return nil
}

func (r *recordReader) get(ctx context.Context, row rowIndex) (agent.ConversationRecord, error) {
	var out agent.ConversationRecord
	if err := contextError(ctx); err != nil {
		return out, err
	}
	cursor := row.Cursor
	if cursor < 1 || cursor > r.stream.index.version() {
		return out, status_error.ChatEventCursorInvalid
	}
	out.Cursor, out.RecordedAt = cursor, row.At
	if row.LegacyStart == 0 {
		entry, err := readDiskRow(r.stream.file, row.Where)
		if err != nil || entry.Chat == nil {
			return out, status_error.ChatLogCorrupted
		}
		out.Change = *entry.Chat
		return out, nil
	}
	start := row.LegacyStart
	if r.cursor >= start && r.cursor < cursor && r.legacy.start == start {
		start = r.cursor + 1
	} else {
		r.legacy = legacyScope{}
	}
	for current := start; current <= cursor; current++ {
		previous, ok := r.known[current]
		if !ok {
			window, err := r.stream.index.between(current, cursor)
			if err != nil {
				return out, err
			}
			for _, p := range window {
				r.known[p.Cursor] = p
			}
			previous = r.known[current]
		}
		raw, err := readRaw(r.stream.file, previous.Where)
		if err != nil {
			return out, err
		}
		fact, at, _, err := decodeLegacy(raw, current, &r.legacy)
		if err != nil {
			return out, err
		}
		out.Change.Fact = &fact
		out.RecordedAt = at
	}
	r.cursor = cursor
	return out, nil
}

func (s *Store) ReadChanges(ctx context.Context, chatID string, after int64, limit int) (agent.ChangePage, error) {
	page := agent.ChangePage{Next: after}
	if after < 0 || limit < 1 || limit > maxPageSize {
		return page, status_error.ChatEventCursorInvalid
	}
	r, err := s.openChat(ctx, chatID)
	if err != nil {
		return page, err
	}
	defer r.close()
	page.Version = r.index.version()
	if after > page.Version {
		return page, status_error.ChatEventCursorInvalid
	}
	end := after + min(int64(limit), page.Version-after)
	rows, err := r.index.between(after+1, end)
	if err != nil {
		return page, err
	}
	if int64(len(rows)) != end-after {
		return page, status_error.ChatLogIndexCorrupted
	}
	reader := newRecordReader(r, rows)
	for _, pos := range rows {
		row, err := reader.get(ctx, pos)
		if err != nil {
			return agent.ChangePage{}, err
		}
		page.Records = append(page.Records, row)
	}
	page.Next, page.More = end, end < page.Version
	return page, nil
}

// ReadBinding returns nil for a valid Chat without a durable binding, including a new Chat.
func (s *Store) ReadBinding(ctx context.Context, chatID string) (*agent.NativeBinding, error) {
	slot := s.chatSlot(chatID)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	path, err := s.LogPath(chatID)
	if err != nil {
		return nil, err
	}
	idx, err := slot.load(ctx, path, true)
	if err != nil {
		return nil, err
	}
	pos, err := idx.bindingPosition()
	if err != nil || pos.Cursor == 0 {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, status_error.ChatLogError
	}
	defer file.Close()
	row, err := readDiskRow(file, pos.Where)
	if err != nil || row.Chat == nil || row.Chat.Binding == nil {
		return nil, status_error.ChatLogCorrupted
	}
	if row.Chat.Binding.Cleared {
		return nil, nil
	}
	return row.Chat.Binding, nil
}

func (s *Store) ReadTurn(ctx context.Context, chatID, executionID string) ([]agent.ConversationRecord, error) {
	if !validID(executionID) {
		return nil, status_error.ChatLogRecordInvalid
	}
	r, err := s.openChat(ctx, chatID)
	if err != nil {
		return nil, err
	}
	defer r.close()
	locs, err := r.index.forExecution(executionID)
	if err != nil {
		return nil, err
	}
	if len(locs) == 0 {
		return nil, status_error.ExecutionNotFound
	}
	result := make([]agent.ConversationRecord, 0, len(locs))
	reader := newRecordReader(r, locs)
	for _, pos := range locs {
		record, err := reader.get(ctx, pos)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, nil
}

// ReadHostInputConsumption reads the admission even if its native consumption is unresolved.
func (s *Store) ReadHostInputConsumption(ctx context.Context, ref agent.HostInputRef) (agent.HostInput, *agent.InputConsumption, error) {
	var input agent.HostInput
	var consumption *agent.InputConsumption
	r, err := s.openChat(ctx, ref.ChatID)
	if err != nil {
		return input, nil, err
	}
	defer r.close()
	rows, err := r.index.hostRows(ref.HostInvocationID)
	if err != nil {
		return input, nil, err
	}
	reader := newRecordReader(r, rows)
	for _, pos := range rows {
		row, err := reader.get(ctx, pos)
		if err != nil {
			return input, nil, err
		}
		switch {
		case row.Change.Input != nil:
			input = *row.Change.Input
		case row.Change.Consumption != nil:
			consumption = row.Change.Consumption
		}
	}
	if input.HostInvocationID == "" && consumption == nil {
		return input, nil, status_error.ChatLogNativeOutcomePending
	}
	if input.HostInvocationID != ref.HostInvocationID || (consumption != nil && consumption.HostInvocationID != ref.HostInvocationID) {
		return input, nil, status_error.ChatLogCorrupted
	}
	return input, consumption, nil
}

func (s *Store) ReadMessages(ctx context.Context, q agent.MessageQuery) (agent.MessagePage, error) {
	var page agent.MessagePage
	if q.Limit < 1 || q.Limit > maxPageSize || q.Before < 0 {
		return page, status_error.ChatMessageCursorInvalid
	}
	r, err := s.openChat(ctx, q.ChatID)
	if err != nil {
		return page, err
	}
	defer r.close()
	page.Version = r.index.version()
	before := q.Before
	if before == 0 {
		before = page.Version + 1
	} else {
		var boundary messageAnchor
		err := r.index.query(func(db *gorm.DB) error {
			return db.Where("stream = ? AND cursor = ?", r.index.coverage.Stream, before).Limit(1).Find(&boundary).Error
		})
		if err != nil {
			return page, err
		}
		if boundary.Cursor == 0 {
			return page, status_error.ChatMessageCursorInvalid
		}
	}
	anchors, err := r.index.anchors(before, q.Limit+1, false)
	if err != nil {
		return page, err
	}
	if len(anchors) > q.Limit {
		anchors = anchors[:q.Limit]
		page.Before = anchors[len(anchors)-1].Cursor
	}
	slices.Reverse(anchors)
	ids := make([]int64, len(anchors))
	for j, a := range anchors {
		ids[j] = a.Ref
	}
	groups, err := r.index.forMessages(ids)
	if err != nil {
		return page, err
	}
	var all []rowIndex
	for _, rows := range groups {
		all = append(all, rows...)
	}
	reader := newRecordReader(r, all)
	if err := reader.prepareLegacy(); err != nil {
		return page, err
	}
	page.Messages, err = s.projectPage(ctx, reader, anchors, groups)
	if err != nil {
		return agent.MessagePage{}, err
	}
	return page, nil
}

func readChatMessage(ctx context.Context, reader *recordReader, anchor messageAnchor, positions []rowIndex) (agent.StoredMessage, error) {
	m := agent.StoredMessage{ID: string(anchor.Message), Cursor: anchor.Cursor, Role: constants.MessageRoleAssistant, Revision: anchor.Revision}
	if anchor.Kind == indexUserMessage {
		m.Role = constants.MessageRoleUser
	}
	var text strings.Builder
	for _, pos := range positions {
		r, err := reader.get(ctx, pos)
		if err != nil {
			return m, err
		}
		if f := r.Change.Fact; f != nil {
			if m.ExecutionID == "" {
				m.ExecutionID = f.ExecutionID
			}
			if err := applyChatMessageFact(&m, &text, *f); err != nil {
				return m, err
			}
		}
		if c := r.Change.Message; c != nil {
			if m.ExecutionID == "" {
				m.ExecutionID = c.ExecutionID
			}
			m.Revision = max(m.Revision, c.Revision)
			m.Complete = m.Complete || c.Kind == agent.MessageComplete
		}
	}
	m.Text = text.String()
	return m, nil
}

func applyChatMessageFact(m *agent.StoredMessage, text *strings.Builder, fact agent.ChatFact) error {
	m.Legacy = append(m.Legacy, fact)
	switch fact.Type {
	case constants.ChatEventTypeUserMessageCreated, constants.ChatEventTypeMessageInherited:
		var data struct {
			// Content is the old user or inherited message text.
			Content string `json:"content"`
			// Role preserves the original inherited message role.
			Role constants.MessageRole `json:"role"`
		}
		if len(fact.Data) > 0 && json.Unmarshal(fact.Data, &data) != nil {
			return status_error.ChatLogCorrupted
		}
		if data.Role != "" {
			m.Role = data.Role
		}
		text.WriteString(data.Content)
		m.Complete = true
	case constants.ChatEventTypeAssistantMessage, constants.ChatEventTypeToolMessage:
		var data legacyRuntimeMessage
		if json.Unmarshal(fact.Data, &data) != nil {
			return status_error.ChatLogCorrupted
		}
		m.Revision = max(m.Revision, data.Revision)
		for _, content := range data.Content {
			m.Parts = append(m.Parts, agent.MessagePart{Type: content.Type, ToolCallID: toolID(content.Data), Data: content.Data})
			if content.Type == constants.RuntimeEventTypeTextDelta {
				var part struct {
					// Text is the legacy segment body.
					Text string `json:"text"`
				}
				if json.Unmarshal(content.Data, &part) != nil {
					return status_error.ChatLogCorrupted
				}
				text.WriteString(part.Text)
			}
		}
	case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
		m.Complete = true
	}
	return nil
}

// projectedNativeRow refers to a selected native operation without opening its body.
// projectedPart ties a selected locator to its ordered public display slot.
type projectedPart struct {
	// locator comes from the current-only SQLite projection.
	locator displayPosition
	// message is the public page offset.
	message int
	// part is the offset inside that message's native parts.
	part int
}

func (s *Store) projectPage(ctx context.Context, reader *recordReader, anchors []messageAnchor, groups map[int64][]rowIndex) ([]agent.StoredMessage, error) {
	refs := make([]int64, 0, len(anchors))
	for _, anchor := range anchors {
		refs = append(refs, anchor.Ref)
	}
	display, err := reader.stream.index.displayForMessages(refs)
	if err != nil {
		return nil, err
	}
	messages := make([]agent.StoredMessage, 0, len(anchors))
	parts := make([][]agent.MessagePart, len(anchors))
	byBucket := make(map[int][]projectedPart)
	var anchorCursors []int64
	for offset, anchor := range anchors {
		m, err := readChatMessage(ctx, reader, anchor, groups[anchor.Ref])
		if err != nil {
			return nil, err
		}
		messages = append(messages, m)
		if m.ExecutionID == "" && anchor.ExecutionRef != 0 {
			anchorCursors = append(anchorCursors, anchor.Cursor)
		}
		parts[offset] = make([]agent.MessagePart, len(display[anchor.Ref]))
		for j, pos := range display[anchor.Ref] {
			if pos.Kind != indexNativeClassification {
				byBucket[pos.Bucket] = append(byBucket[pos.Bucket], projectedPart{pos, offset, j})
			}
		}
	}
	// An independently linked message may have no ordinary Chat fact on the page.
	// Read its original anchor once to preserve the public execution identity.
	anchorPositions, err := reader.stream.index.positions(anchorCursors)
	if err != nil {
		return nil, err
	}
	for offset, anchor := range anchors {
		if messages[offset].ExecutionID != "" || anchor.ExecutionRef == 0 {
			continue
		}
		original, err := reader.get(ctx, anchorPositions[anchor.Cursor])
		if err != nil {
			return nil, err
		}
		if original.Change.Message != nil {
			messages[offset].ExecutionID = original.Change.Message.ExecutionID
		}
	}
	for bucket, selected := range byBucket {
		if err := s.readDisplayBucket(ctx, bucket, selected, parts); err != nil {
			return nil, err
		}
	}
	for offset, anchor := range anchors {
		foldDisplay(&messages[offset], display[anchor.Ref], parts[offset])
	}
	return messages, nil
}

func (s *Store) readDisplayBucket(ctx context.Context, bucket int, selected []projectedPart, out [][]agent.MessagePart) error {
	if bucket < 0 || bucket >= streamShards {
		return status_error.ChatLogIndexCorrupted
	}
	slot := &s.natives[bucket]
	slot.mu.Lock()
	defer slot.mu.Unlock()
	path := filepath.Join(s.root, nativeBucketPath(bucket))
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return err
	}
	cursors := make([]int64, 0, len(selected))
	for _, entry := range selected {
		cursors = append(cursors, entry.locator.NativeCursor)
	}
	positions, err := idx.positions(cursors)
	if err != nil {
		return err
	}
	rows := make([]rowIndex, 0, len(positions))
	for _, pos := range positions {
		rows = append(rows, pos)
	}
	positions, err = idx.withPatchBases(rows)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return status_error.ChatLogError
	}
	defer file.Close()
	terminals := make(map[int64]nativeTerminal)
	var wanted []nativeBodyLookup
	for _, entry := range selected {
		if err := contextError(ctx); err != nil {
			return err
		}
		d := entry.locator
		pos := positions[d.NativeCursor]
		if pos.Cursor == 0 || pos.LaneRef != d.Lane || pos.Kind != d.Kind {
			return status_error.ChatLogIndexCorrupted
		}
		if d.Kind == indexNativeTerminal {
			if _, known := terminals[pos.Cursor]; known {
				continue
			}
			row, err := readDiskRow(file, pos.Where)
			if err != nil || row.Terminal == nil {
				return status_error.ChatLogCorrupted
			}
			t := *row.Terminal
			terminals[pos.Cursor] = t
			if t.Body != nil {
				if t.Body.Last >= pos.Cursor {
					return status_error.ChatLogCorrupted
				}
				wanted = append(wanted, nativeBodyLookup{pos.NativeRef, pos.LaneRef, *t.Body})
			}
			continue
		}
		f, err := readFragment(file, positions, pos.Cursor, 0)
		if err != nil {
			return err
		}
		kind := f.Change.ContentType
		if kind == "" {
			if f.Change.Kind == agent.MessageToolSnapshot {
				kind = constants.RuntimeEventTypeToolUpdate
			} else {
				kind = constants.RuntimeEventTypeTextDelta
			}
		}
		out[entry.message][entry.part] = agent.MessagePart{Type: kind, Text: f.Change.Text, ToolCallID: f.Change.ToolCallID, Data: f.Change.Data}
	}
	bodyRows, err := idx.bodyPositions(wanted)
	if err != nil {
		return err
	}
	for cursor, t := range terminals {
		if t.Body != nil {
			t.Result.FinalMessage, err = readNativeBody(ctx, file, t.Result.Key, *t.Body, bodyRows[positions[cursor].LaneRef])
			if err != nil {
				return err
			}
			terminals[cursor] = t
		}
		if t.Result.FinalMessage == nil {
			return status_error.ChatLogCorrupted
		}
	}
	for _, entry := range selected {
		if entry.locator.Kind == indexNativeTerminal {
			final := terminals[entry.locator.NativeCursor].Result.FinalMessage
			out[entry.message][entry.part] = agent.MessagePart{Type: constants.RuntimeEventTypeTextDelta, Text: final.Text}
		}
	}
	return nil
}

type displayLane struct {
	// bucket scopes the durable native lane cursor.
	bucket int
	// lane is the first durable cursor of the native message.
	lane int64
}

func foldDisplay(message *agent.StoredMessage, positions []displayPosition, parts []agent.MessagePart) {
	classification := make(map[displayLane]displayPosition)
	for _, pos := range positions {
		if (pos.Kind == indexNativeClassification || pos.Kind == indexNativeTextReplace) &&
			(pos.Content == constants.RuntimeEventTypeTextDelta || pos.Content == constants.RuntimeEventTypeCommentaryDelta) {
			lane := displayLane{pos.Bucket, pos.Lane}
			if pos.NativeCursor > classification[lane].NativeCursor {
				classification[lane] = pos
			}
		}
	}
	var text strings.Builder
	text.WriteString(message.Text)
	for j, pos := range positions {
		if pos.Kind == indexNativeClassification {
			continue
		}
		part := parts[j]
		if pos.LaneText && pos.Kind != indexNativeTerminal {
			if kind := classification[displayLane{pos.Bucket, pos.Lane}].Content; kind != "" {
				part.Type = kind
			}
		}
		message.Parts = append(message.Parts, part)
		if part.Type == constants.RuntimeEventTypeTextDelta {
			text.WriteString(part.Text)
		}
	}
	message.Text = text.String()
}

// ReadHistoryCandidates locates completed turns without reading process or tool bodies.
func (s *Store) ReadHistoryCandidates(ctx context.Context, q agent.MessageQuery) ([]agent.HistoryCandidate, int64, error) {
	if q.Limit < 1 || q.Limit > maxPageSize || q.Before < 0 {
		return nil, 0, status_error.ChatMessageCursorInvalid
	}
	r, err := s.openChat(ctx, q.ChatID)
	if err != nil {
		return nil, 0, err
	}
	defer r.close()
	before := q.Before
	if before == 0 {
		before = r.index.version() + 1
	}
	anchors, err := r.index.anchors(before, q.Limit+1, true)
	if err != nil {
		return nil, 0, err
	}
	next := int64(0)
	if len(anchors) > q.Limit {
		anchors = anchors[:q.Limit]
		next = anchors[len(anchors)-1].Cursor
	}
	cursors := make([]int64, 0, len(anchors))
	for _, anchor := range anchors {
		if anchor.Kind != indexInherited {
			cursors = append(cursors, anchor.Cursor)
		}
	}
	positions, err := r.index.positions(cursors)
	if err != nil {
		return nil, 0, err
	}
	known := make([]rowIndex, 0, len(positions))
	for _, p := range positions {
		known = append(known, p)
	}
	reader := newRecordReader(r, known)
	out := make([]agent.HistoryCandidate, 0, len(anchors))
	for _, a := range anchors {
		c := agent.HistoryCandidate{Cursor: a.Cursor, MessageID: string(a.Message), Inherited: a.Kind == indexInherited}
		if !c.Inherited {
			record, err := reader.get(ctx, positions[a.Cursor])
			if err != nil || record.Change.Fact == nil {
				return nil, 0, status_error.ChatLogCorrupted
			}
			var data struct {
				// UserMessageID is the input frozen in the execution creation fact.
				UserMessageID string `json:"userMessageId"`
			}
			if len(record.Change.Fact.Data) > 0 && json.Unmarshal(record.Change.Fact.Data, &data) != nil {
				return nil, 0, status_error.ChatLogCorrupted
			}
			c.UserMessageID = data.UserMessageID
		}
		out = append(out, c)
	}
	return out, next, nil
}

// ReadMessage resolves one stable message and its late facts without scanning a conversation.
func (s *Store) ReadMessage(ctx context.Context, chatID, messageID string) (agent.StoredMessage, error) {
	message, _, err := s.readMessage(ctx, chatID, messageID, 0, false)
	return message, err
}

// ReadMessageCheckpoint returns the effective native message at its latest checkpoint.
// An older requested cursor only returns the winning checkpoint identity.
func (s *Store) ReadMessageCheckpoint(ctx context.Context, chatID, messageID string, at int64) (agent.StoredMessage, int64, error) {
	return s.readMessage(ctx, chatID, messageID, at, true)
}

func (s *Store) readMessage(ctx context.Context, chatID, messageID string, at int64, native bool) (agent.StoredMessage, int64, error) {
	r, err := s.openChat(ctx, chatID)
	if err != nil {
		return agent.StoredMessage{}, 0, err
	}
	defer r.close()
	anchor, err := r.index.anchor(messageID)
	if err != nil {
		return agent.StoredMessage{}, 0, err
	}
	if anchor.Cursor == 0 {
		return agent.StoredMessage{}, 0, status_error.ChatMessageNotFound
	}
	var checkpoint rowIndex
	if native {
		err = r.index.query(func(db *gorm.DB) error {
			return db.Where("stream = ? AND message_ref <> 0 AND message_ref = ? AND (display_link OR kind = ?)",
				r.index.coverage.Stream, anchor.Ref, indexMessageComplete).Order("cursor DESC").Limit(1).Find(&checkpoint).Error
		})
		if err != nil || checkpoint.Cursor == 0 || (at > 0 && at != checkpoint.Cursor) {
			return agent.StoredMessage{}, checkpoint.Cursor, err
		}
	}
	groups, err := r.index.forMessages([]int64{anchor.Ref})
	if err != nil {
		return agent.StoredMessage{}, 0, err
	}
	rows := groups[anchor.Ref]
	reader := newRecordReader(r, rows)
	if err := reader.prepareLegacy(); err != nil {
		return agent.StoredMessage{}, 0, err
	}
	messages, err := s.projectPage(ctx, reader, []messageAnchor{anchor}, groups)
	if err != nil {
		return agent.StoredMessage{}, 0, err
	}
	return messages[0], checkpoint.Cursor, nil
}
