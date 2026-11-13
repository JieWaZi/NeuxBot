package conversation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"gorm.io/gorm"
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

func (r chatReader) record(ctx context.Context, row rowIndex) (agent.ConversationRecord, error) {
	var out agent.ConversationRecord
	if err := contextError(ctx); err != nil {
		return out, err
	}
	if row.Cursor < 1 || row.Cursor > r.index.version() {
		return out, status_error.ChatEventCursorInvalid
	}
	entry, err := readDiskRow(r.file, row.Where)
	if err != nil || entry.Chat == nil {
		return out, status_error.ChatLogCorrupted
	}
	return agent.ConversationRecord{Cursor: row.Cursor, RecordedAt: row.At, Change: *entry.Chat}, nil
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
	for _, pos := range rows {
		row, err := r.record(ctx, pos)
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
	for _, pos := range locs {
		record, err := r.record(ctx, pos)
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
	for _, pos := range rows {
		row, err := r.record(ctx, pos)
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
	page, err := s.readTimeline(ctx, q, 0, false)
	return page.Messages, err
}

// TimelineQuestion 定位当前页的一条原请求及其已经持久化的终态。
type TimelineQuestion struct {
	// Request 是问题在 Chat 日志中的首次提交。
	Request agent.ConversationRecord
	// Outcome 是原处理结果，缺失时以同一执行的终态收口。
	Outcome agent.ConversationRecord
}

// TimelinePage 保存共用创建游标的普通消息与独立问答项。
type TimelinePage struct {
	// Messages 保存普通消息及整个时间线窗口的覆盖游标。
	Messages agent.MessagePage
	// Questions 保存当前页的独立问答项。
	Questions []TimelineQuestion
	// MessageBefore 是普通消息窗口的上界；问答分页不能截断一条回复的后续事件。
	MessageBefore int64
}

// ReadTimeline 分页读取公开时间线。团队分页可提供冻结的包含式日志上界。
func (s *Store) ReadTimeline(ctx context.Context, q agent.MessageQuery, through int64) (TimelinePage, error) {
	return s.readTimeline(ctx, q, through, true)
}

func (s *Store) readTimeline(ctx context.Context, q agent.MessageQuery, through int64, includeQuestions bool) (TimelinePage, error) {
	var result TimelinePage
	if q.Limit < 1 || q.Limit > maxPageSize || q.Before < 0 || through < 0 || (q.Before != 0 && through != 0) {
		return result, status_error.ChatMessageCursorInvalid
	}
	r, err := s.openChat(ctx, q.ChatID)
	if err != nil {
		return result, err
	}
	defer r.close()
	page := &result.Messages
	page.Version = r.index.version()
	if through > page.Version {
		return result, status_error.ChatMessageCursorInvalid
	}
	before := q.Before
	result.MessageBefore = before
	if through > 0 {
		before = through + 1
		result.MessageBefore = before
	} else if before == 0 {
		before = page.Version + 1
	} else {
		var boundary messageAnchor
		err := r.index.query(func(db *gorm.DB) error {
			return db.Where("stream = ? AND cursor = ?", r.index.coverage.Stream, before).Limit(1).Find(&boundary).Error
		})
		if err != nil {
			return result, err
		}
		if boundary.Cursor == 0 || (!includeQuestions && boundary.Kind == indexQuestionRequested) {
			return result, status_error.ChatMessageCursorInvalid
		}
		if boundary.Kind == indexQuestionRequested {
			var next messageAnchor
			err := r.index.query(func(db *gorm.DB) error {
				return db.Where("stream = ? AND cursor > ? AND kind <> ?", r.index.coverage.Stream, before, indexQuestionRequested).
					Order("cursor ASC").Limit(1).Find(&next).Error
			})
			if err != nil {
				return result, err
			}
			result.MessageBefore = next.Cursor
		}
	}
	anchors, err := r.index.anchors(before, q.Limit+1, false, includeQuestions)
	if err != nil {
		return result, err
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
		return result, err
	}
	if includeQuestions {
		var questions []messageAnchor
		anchors = slices.DeleteFunc(anchors, func(anchor messageAnchor) bool {
			if anchor.Kind != indexQuestionRequested {
				return false
			}
			questions = append(questions, anchor)
			return true
		})
		result.Questions, err = readTimelineQuestions(ctx, r, questions, groups)
		if err != nil {
			return TimelinePage{}, err
		}
	}
	page.Messages, err = s.projectPage(ctx, r, anchors, groups, true)
	if err != nil {
		return TimelinePage{}, err
	}
	return result, nil
}

func readTimelineQuestions(ctx context.Context, reader chatReader, anchors []messageAnchor, groups map[int64][]rowIndex) ([]TimelineQuestion, error) {
	questions := make([]TimelineQuestion, 0, len(anchors))
	byExecution := make(map[int64][]int)
	for _, anchor := range anchors {
		var question TimelineQuestion
		for _, pos := range groups[anchor.Ref] {
			var err error
			switch {
			case pos.Cursor == anchor.Cursor:
				question.Request, err = reader.record(ctx, pos)
			case pos.Kind == indexQuestionResolved:
				question.Outcome, err = reader.record(ctx, pos)
			}
			if err != nil {
				return nil, err
			}
		}
		if question.Request.Cursor == 0 {
			return nil, status_error.ChatLogIndexCorrupted
		}
		if question.Outcome.Cursor == 0 && anchor.ExecutionRef != 0 {
			byExecution[anchor.ExecutionRef] = append(byExecution[anchor.ExecutionRef], len(questions))
		}
		questions = append(questions, question)
	}
	if len(byExecution) == 0 {
		return questions, nil
	}
	executions := make([]int64, 0, len(byExecution))
	for ref := range byExecution {
		executions = append(executions, ref)
	}
	var terminals []rowIndex
	err := reader.index.query(func(db *gorm.DB) error {
		return db.Table("conversation_positions INDEXED BY "+executionTerminalIndex).
			Where("stream = ? AND execution_ref IN ?", reader.index.coverage.Stream, executions).
			Where(executionTerminalPredicate).Order("cursor").Find(&terminals).Error
	})
	if err != nil {
		return nil, err
	}
	for _, terminal := range terminals {
		outcome, err := reader.record(ctx, terminal)
		if err != nil {
			return nil, err
		}
		for _, index := range byExecution[terminal.ExecutionRef] {
			if outcome.Cursor > questions[index].Request.Cursor {
				questions[index].Outcome = outcome
			}
		}
	}
	return questions, nil
}

func readChatMessage(ctx context.Context, reader chatReader, anchor messageAnchor, positions []rowIndex) (agent.StoredMessage, error) {
	m := agent.StoredMessage{ID: string(anchor.Message), Cursor: anchor.Cursor, Role: constants.MessageRoleAssistant, Revision: anchor.Revision}
	if anchor.Kind == indexUserMessage {
		m.Role = constants.MessageRoleUser
	}
	for _, pos := range positions {
		r, err := reader.record(ctx, pos)
		if err != nil {
			return m, err
		}
		if f := r.Change.Fact; f != nil {
			if m.ExecutionID == "" {
				m.ExecutionID = f.ExecutionID
			}
			if err := applyChatMessageFact(&m, *f); err != nil {
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
	return m, nil
}

func applyChatMessageFact(m *agent.StoredMessage, fact agent.ChatFact) error {
	switch fact.Type {
	case constants.ChatEventTypeUserMessageCreated, constants.ChatEventTypeMessageInherited:
		var data struct {
			// Content is the user or inherited message text.
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
		m.Text = data.Content
		m.Complete = true
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

func (s *Store) projectPage(ctx context.Context, reader chatReader, anchors []messageAnchor, groups map[int64][]rowIndex, includeToolOutput bool) ([]agent.StoredMessage, error) {
	refs := make([]int64, 0, len(anchors))
	for _, anchor := range anchors {
		refs = append(refs, anchor.Ref)
	}
	display, err := reader.index.displayForMessages(refs)
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
	anchorPositions, err := reader.index.positions(anchorCursors)
	if err != nil {
		return nil, err
	}
	for offset, anchor := range anchors {
		if messages[offset].ExecutionID != "" || anchor.ExecutionRef == 0 {
			continue
		}
		original, err := reader.record(ctx, anchorPositions[anchor.Cursor])
		if err != nil {
			return nil, err
		}
		if original.Change.Message != nil {
			messages[offset].ExecutionID = original.Change.Message.ExecutionID
		}
	}
	for bucket, selected := range byBucket {
		if err := s.readDisplayBucket(ctx, bucket, selected, parts, includeToolOutput); err != nil {
			return nil, err
		}
	}
	for offset, anchor := range anchors {
		foldDisplay(&messages[offset], display[anchor.Ref], parts[offset])
	}
	return messages, nil
}

func (s *Store) readDisplayBucket(ctx context.Context, bucket int, selected []projectedPart, out [][]agent.MessagePart, includeToolOutput bool) error {
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
		if includeToolOutput {
			if err := hydrateToolOutput(ctx, file, idx, f); err != nil {
				return err
			}
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
	anchors, err := r.index.anchors(before, q.Limit+1, true, false)
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
	out := make([]agent.HistoryCandidate, 0, len(anchors))
	for _, a := range anchors {
		c := agent.HistoryCandidate{Cursor: a.Cursor, MessageID: string(a.Message), Inherited: a.Kind == indexInherited}
		if !c.Inherited {
			record, err := r.record(ctx, positions[a.Cursor])
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

// ReadMessageCheckpoint 返回安全实时投影所需的最新工具元数据和正文，不展开独立终端输出。
// 完整工具输出仍由 ReadMessage/ReadMessages 提供；旧游标只返回获胜检查点身份。
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
	if anchor.Cursor == 0 || anchor.Kind == indexQuestionRequested {
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
	messages, err := s.projectPage(ctx, r, []messageAnchor{anchor}, groups, !native)
	if err != nil {
		return agent.StoredMessage{}, 0, err
	}
	return messages[0], checkpoint.Cursor, nil
}
