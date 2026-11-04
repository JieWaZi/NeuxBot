package eventlog_next

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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

func (s *Store) readHostRelation(ctx context.Context, ref agent.HostInputRef) (agent.HostInput, agent.InputConsumption, error) {
	var input agent.HostInput
	var consumption agent.InputConsumption
	r, err := s.openChat(ctx, ref.ChatID)
	if err != nil {
		return input, consumption, err
	}
	defer r.close()
	rows, err := r.index.hostRows(ref.HostInvocationID)
	if err != nil {
		return input, consumption, err
	}
	if len(rows) != 2 {
		return input, consumption, status_error.ChatLogNativeOutcomePending
	}
	reader := newRecordReader(r, rows)
	for _, pos := range rows {
		row, err := reader.get(ctx, pos)
		if err != nil {
			return input, consumption, err
		}
		switch {
		case row.Change.Input != nil:
			input = *row.Change.Input
		case row.Change.Consumption != nil:
			consumption = *row.Change.Consumption
		}
	}
	if input.HostInvocationID != ref.HostInvocationID || consumption.HostInvocationID != ref.HostInvocationID {
		return input, consumption, status_error.ChatLogCorrupted
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
	projections := make([]messageProjection, 0, len(anchors))
	for _, anchor := range anchors {
		rows := groups[anchor.Ref]
		m, err := readChatMessage(ctx, reader, anchor, rows)
		if err != nil {
			return agent.MessagePage{}, err
		}
		projections = append(projections, m)
	}
	page.Messages, err = s.projectMessages(ctx, projections)
	if err != nil {
		return agent.MessagePage{}, err
	}
	return page, nil
}

// messageProjection joins only the Chat facts and native ranges selected for this page.
type messageProjection struct {
	// message carries Chat facts and later the effective native display parts.
	message agent.StoredMessage
	// refs is the ordered set of native range links in Chat.
	refs []agent.NativeFragmentRange
	// final points to the canonical calibration of this public reply.
	final *agent.NativeResultKey
	// firstText is the original display position of the native answer.
	firstText int
	// nativeParts holds effective selected operations by their display order.
	nativeParts map[int]agent.MessageChange
	// finalText is the reconstructed native final answer, if one exists.
	finalText *agent.NativeFinalMessage
	// count bounds the native display ordering for this message.
	count int
}

func readChatMessage(ctx context.Context, reader *recordReader, anchor messageAnchor, positions []rowIndex) (messageProjection, error) {
	var p messageProjection
	m := agent.StoredMessage{ID: string(anchor.Message), Cursor: anchor.Cursor, Role: constants.MessageRoleAssistant}
	if anchor.Kind == indexUserMessage {
		m.Role = constants.MessageRoleUser
	}
	var text strings.Builder
	for _, pos := range positions {
		r, err := reader.get(ctx, pos)
		if err != nil {
			return p, err
		}
		if fact := r.Change.Fact; fact != nil {
			if m.ExecutionID == "" {
				m.ExecutionID = fact.ExecutionID
			}
			if err := applyChatMessageFact(&m, &text, *fact); err != nil {
				return p, err
			}
			continue
		}
		if change := r.Change.Message; change != nil {
			if m.ExecutionID == "" {
				m.ExecutionID = change.ExecutionID
			}
			m.Revision = max(m.Revision, change.Revision)
			switch change.Kind {
			case agent.MessageNativeFragment:
				p.refs = append(p.refs, *change.Fragments)
			case agent.MessageNativeFinal:
				p.final = change.Result
			case agent.MessageComplete:
				m.Complete = true
			}
		}
	}
	m.Text = text.String()
	p.message = m
	return p, nil
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
type projectedNativeRow struct {
	// ref identifies the canonical invocation and message.
	ref agent.NativeFragmentRange
	// cursor locates the change in that native stream.
	cursor int64
	// meta holds its kind and tool identity for choosing the effective version.
	meta rowIndex
}

func (s *Store) nativePositions(ctx context.Context, refs []agent.NativeFragmentRange) ([]projectedNativeRow, error) {
	byStream := make(map[*streamSlot][]int)
	byRef := make([][]projectedNativeRow, len(refs))
	for i, ref := range refs {
		slot, _ := s.nativeStream(ref.Key)
		byStream[slot] = append(byStream[slot], i)
	}
	for slot, indexes := range byStream {
		if err := s.collectNativePositions(ctx, slot, refs, indexes, byRef); err != nil {
			return nil, err
		}
	}
	var rows []projectedNativeRow
	for _, refs := range byRef {
		rows = append(rows, refs...)
	}
	return rows, nil
}

func (s *Store) collectNativePositions(ctx context.Context, slot *streamSlot, refs []agent.NativeFragmentRange, indexes []int, out [][]projectedNativeRow) error {
	_, path := s.nativeStream(refs[indexes[0]].Key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return err
	}
	var all []int64
	for _, i := range indexes {
		ref := refs[i]
		if !validNativeRange(ref) || ref.Last > idx.version() {
			return status_error.ChatLogCorrupted
		}
		for cursor := ref.First; cursor <= ref.Last; cursor++ {
			all = append(all, cursor)
		}
	}
	positions, err := idx.positions(all)
	if err != nil {
		return err
	}
	for _, i := range indexes {
		ref := refs[i]
		id := nativeRowID(ref.Key)
		rows := make([]projectedNativeRow, 0, ref.Last-ref.First+1)
		for cursor := ref.First; cursor <= ref.Last; cursor++ {
			row := positions[cursor]
			if row.Kind < indexNativeTextAppend || row.Kind > indexNativeEvent || !bytes.Equal(row.Native, id) || row.Message != ref.MessageID {
				return status_error.ChatLogCorrupted
			}
			rows = append(rows, projectedNativeRow{ref: ref, cursor: cursor, meta: row})
		}
		out[i] = rows
	}
	return nil
}

type nativeSelection struct {
	// message is the position of the public message in the requested page.
	message int
	// order is the display position inside that message.
	order int
	// row locates the selected native operation.
	row projectedNativeRow
}

type nativeProjectionGroup struct {
	// path locates one shared native authority bucket.
	path string
	// rows are the effective fragment positions for the whole page in this bucket.
	rows []nativeSelection
	// finals maps one native final result to each selected public message that references it.
	finals map[agent.NativeResultKey][]int
}

func (s *Store) projectMessages(ctx context.Context, messages []messageProjection) ([]agent.StoredMessage, error) {
	var refs []agent.NativeFragmentRange
	for _, p := range messages {
		refs = append(refs, p.refs...)
	}
	positions, err := s.nativePositions(ctx, refs)
	if err != nil {
		return nil, err
	}
	groups := make(map[*streamSlot]*nativeProjectionGroup)
	groupFor := func(key agent.NativeResultKey) *nativeProjectionGroup {
		slot, path := s.nativeStream(key)
		if groups[slot] == nil {
			groups[slot] = &nativeProjectionGroup{path: path, finals: make(map[agent.NativeResultKey][]int)}
		}
		return groups[slot]
	}
	for m := range messages {
		p := &messages[m]
		for _, ref := range p.refs {
			p.count += int(ref.Last - ref.First + 1)
		}
		rows := positions[:p.count]
		positions = positions[p.count:]
		first, selected := effectiveNativePositions(rows, p.final != nil)
		p.firstText = first
		for _, j := range selected {
			row := rows[j]
			group := groupFor(row.ref.Key)
			group.rows = append(group.rows, nativeSelection{message: m, order: j, row: row})
		}
		if p.final != nil {
			group := groupFor(*p.final)
			group.finals[*p.final] = append(group.finals[*p.final], m)
		}
	}
	for slot, group := range groups {
		if err := readNativeProjectionGroup(ctx, slot, group, messages); err != nil {
			return nil, err
		}
	}
	result := make([]agent.StoredMessage, 0, len(messages))
	for _, p := range messages {
		result = append(result, foldNativeParts(p))
	}
	return result, nil
}

func effectiveNativePositions(rows []projectedNativeRow, final bool) (int, []int) {
	type snapshotKey struct {
		// native, message and tool identify the snapshot owner.
		native, message, tool string
		// content keeps tool input and execution snapshots independent.
		content constants.RuntimeEventType
	}
	lastSnapshot := make(map[snapshotKey]int)
	lastReplace, firstText := -1, -1
	for i, row := range rows {
		switch row.meta.Kind {
		case indexNativeTool:
			lastSnapshot[snapshotKey{string(row.meta.Native), row.meta.Message, row.meta.Tool, row.meta.Content}] = i
		case indexNativeTextAppend, indexNativeTextReplace:
			if !isFinalText(row.meta.Content) {
				continue
			}
			if firstText < 0 {
				firstText = i
			}
			if row.meta.Kind == indexNativeTextReplace {
				lastReplace = i
			}
		}
	}
	selected := make([]int, 0, len(rows))
	for i, row := range rows {
		if row.meta.Kind == indexNativeTool && lastSnapshot[snapshotKey{string(row.meta.Native), row.meta.Message, row.meta.Tool, row.meta.Content}] != i {
			continue
		}
		if (row.meta.Kind == indexNativeTextAppend || row.meta.Kind == indexNativeTextReplace) &&
			isFinalText(row.meta.Content) && (final || i < lastReplace) {
			continue
		}
		selected = append(selected, i)
	}
	if firstText < 0 {
		firstText = len(rows)
	}
	return firstText, selected
}

func readNativeProjectionGroup(ctx context.Context, slot *streamSlot, group *nativeProjectionGroup, messages []messageProjection) error {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, group.path, false)
	if err != nil {
		return err
	}
	file, err := os.Open(group.path)
	if err != nil {
		return status_error.ChatLogError
	}
	defer file.Close()
	needed := make([]rowIndex, 0, len(group.rows))
	for _, selection := range group.rows {
		needed = append(needed, selection.row.meta)
	}
	positions, err := idx.withPatchBases(needed)
	if err != nil {
		return err
	}
	for _, selected := range group.rows {
		if err := contextError(ctx); err != nil {
			return err
		}
		row := selected.row
		f, err := readFragment(file, positions, row.cursor, 0)
		if err != nil || f.Key != row.ref.Key || f.MessageID != row.ref.MessageID {
			return status_error.ChatLogCorrupted
		}
		m := &messages[selected.message]
		if m.nativeParts == nil {
			m.nativeParts = make(map[int]agent.MessageChange)
		}
		m.nativeParts[selected.order] = f.Change
	}
	if len(group.finals) == 0 {
		return nil
	}
	return readProjectionFinals(ctx, idx, file, group.finals, messages)
}

func readProjectionFinals(ctx context.Context, idx *streamIndex, file *os.File, finals map[agent.NativeResultKey][]int, messages []messageProjection) error {
	keys := make([][]byte, 0, len(finals))
	for key := range finals {
		keys = append(keys, nativeRowID(key))
	}
	rows, err := idx.nativeTerminals(keys)
	if err != nil {
		return err
	}
	if len(rows) != len(finals) {
		return status_error.ChatLogCorrupted
	}
	byKey := make(map[agent.NativeResultKey]nativeTerminal)
	var bodies []nativeBodyLookup
	for _, pos := range rows {
		row, err := readDiskRow(file, pos.Where)
		if err != nil || row.Terminal == nil || len(finals[row.Terminal.Result.Key]) == 0 {
			return status_error.ChatLogCorrupted
		}
		t := *row.Terminal
		byKey[t.Result.Key] = t
		if t.Body != nil {
			if t.Body.Last >= pos.Cursor {
				return status_error.ChatLogCorrupted
			}
			bodies = append(bodies, nativeBodyLookup{Key: t.Result.Key, Body: *t.Body})
		}
	}
	locations, err := idx.bodyPositions(bodies)
	if err != nil {
		return err
	}
	for key, terminal := range byKey {
		final := terminal.Result.FinalMessage
		if terminal.Body != nil {
			final, err = readNativeBody(ctx, file, key, *terminal.Body, locations[string(nativeRowID(key))])
			if err != nil {
				return err
			}
		}
		if final == nil {
			return status_error.ChatLogCorrupted
		}
		for _, m := range finals[key] {
			messages[m].finalText = final
		}
	}
	return nil
}

func foldNativeParts(p messageProjection) agent.StoredMessage {
	m := p.message
	var text strings.Builder
	text.WriteString(m.Text)
	if p.finalText != nil {
		text.Reset()
		text.WriteString(p.finalText.Text)
	}
	for i := 0; i <= p.count; i++ {
		if p.finalText != nil && i == p.firstText {
			m.Parts = append(m.Parts, agent.MessagePart{Type: constants.RuntimeEventTypeTextDelta, Text: p.finalText.Text})
		}
		c, ok := p.nativeParts[i]
		if !ok {
			continue
		}
		kind := c.ContentType
		if kind == "" {
			if c.Kind == agent.MessageToolSnapshot {
				kind = constants.RuntimeEventTypeToolUpdate
			} else {
				kind = constants.RuntimeEventTypeTextDelta
			}
		}
		m.Revision = max(m.Revision, c.Revision)
		m.Parts = append(m.Parts, agent.MessagePart{Type: kind, Text: c.Text, ToolCallID: c.ToolCallID, Data: c.Data})
		if c.Kind == agent.MessageReplace {
			text.Reset()
		}
		if kind == constants.RuntimeEventTypeTextDelta {
			text.WriteString(c.Text)
		}
	}
	m.Text = text.String()
	return m
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
	r, err := s.openChat(ctx, chatID)
	if err != nil {
		return agent.StoredMessage{}, err
	}
	defer r.close()
	anchor, err := r.index.anchor(messageID)
	if err != nil {
		return agent.StoredMessage{}, err
	}
	if anchor.Cursor == 0 {
		return agent.StoredMessage{}, status_error.ChatMessageNotFound
	}
	groups, err := r.index.forMessages([]int64{anchor.Ref})
	if err != nil {
		return agent.StoredMessage{}, err
	}
	rows := groups[anchor.Ref]
	reader := newRecordReader(r, rows)
	if err := reader.prepareLegacy(); err != nil {
		return agent.StoredMessage{}, err
	}
	p, err := readChatMessage(ctx, reader, anchor, rows)
	if err != nil {
		return agent.StoredMessage{}, err
	}
	messages, err := s.projectMessages(ctx, []messageProjection{p})
	if err != nil {
		return agent.StoredMessage{}, err
	}
	return messages[0], nil
}
