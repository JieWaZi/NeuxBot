package eventlog_next

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"gorm.io/gorm"
)

const maximumToolPatchDepth = 8

type nativeFragment struct {
	// Key identifies the one native execution that produced the fragment.
	Key agent.NativeResultKey `json:"k"`
	// MessageID identifies this message within that execution.
	MessageID string `json:"m"`
	// Change contains the normalized change.
	Change agent.MessageChange `json:"c"`
	// Patch is a bounded lossless alternative to a repeated tool snapshot.
	Patch *toolPatch `json:"p,omitempty"`
}

type toolPatch struct {
	// Base is the earlier native row with this tool's snapshot.
	Base int64 `json:"b"`
	// Depth limits reads to a bounded number of snapshot dependencies.
	Depth int `json:"d"`
	// Prefix is the number of preserved bytes at the beginning.
	Prefix int `json:"p"`
	// Suffix is the number of preserved bytes at the end.
	Suffix int `json:"s"`
	// Insert supplies the bytes replacing the range between prefix and suffix.
	Insert []byte `json:"i"`
}

type nativeTerminal struct {
	// Result carries terminal evidence and a final correction when needed.
	Result agent.NativeResult `json:"r"`
	// Body identifies already committed text when it exactly matches the native final.
	Body *nativeBody `json:"b,omitempty"`
}

type nativeBody struct {
	// MessageID is the original native message identity.
	MessageID string `json:"m"`
	// First is the inclusive start of the effective native text range.
	First int64 `json:"f"`
	// Last is the inclusive end of the effective native text range.
	Last int64 `json:"l"`
	// IncludeCommentary retains prior commentary if an exact later classification promoted it.
	IncludeCommentary bool `json:"c,omitempty"`
	// Tail is the exact suffix added by the terminal after the retained prefix.
	Tail string `json:"s,omitempty"`
	// Digest authenticates the complete reconstructed text.
	Digest [32]byte `json:"d"`
}

func validNativeSession(k agent.NativeSessionKey) bool {
	return validID(k.Provider) && validID(k.Subject) && validID(k.SessionID)
}

func validNativeKey(k agent.NativeResultKey) bool {
	return validNativeSession(k.Session) && validID(k.InvocationID)
}

func validNativeRange(r agent.NativeFragmentRange) bool {
	return validNativeKey(r.Key) && validID(r.MessageID) && r.First > 0 &&
		r.Last >= r.First && r.Last-r.First < maxBatchRows
}

func validNativeFragment(f nativeFragment) bool {
	m := f.Change
	if !validNativeKey(f.Key) || !validID(f.MessageID) || m.MessageID != "" ||
		m.ExecutionID != "" || m.Result != nil || m.Fragments != nil || m.Revision < 0 {
		return false
	}
	switch m.Kind {
	case agent.MessageAppend:
		return m.Text != "" && isTextContent(m.ContentType) && m.ToolCallID == "" && len(m.Data) == 0 && f.Patch == nil
	case agent.MessageReplace:
		return isFinalText(m.ContentType) && m.ToolCallID == "" && len(m.Data) == 0 && f.Patch == nil
	case agent.MessageToolSnapshot:
		if !validID(m.ToolCallID) || m.Text != "" ||
			(m.ContentType != "" && m.ContentType != constants.RuntimeEventTypeToolInputSnapshot &&
				m.ContentType != constants.RuntimeEventTypeToolUpdate && m.ContentType != constants.RuntimeEventTypeToolResult) {
			return false
		}
		if p := f.Patch; p != nil {
			return len(m.Data) == 0 && p.Base > 0 && p.Depth > 0 &&
				p.Depth <= maximumToolPatchDepth && p.Prefix >= 0 && p.Suffix >= 0
		}
		return json.Valid(m.Data)
	case agent.MessageClassify:
		return (m.ContentType == constants.RuntimeEventTypeTextDelta || m.ContentType == constants.RuntimeEventTypeCommentaryDelta) && m.Text == "" && m.ToolCallID == "" && len(m.Data) == 0 && f.Patch == nil
	case agent.MessageEvent:
		if m.Text != "" || f.Patch != nil || !json.Valid(m.Data) {
			return false
		}
		switch m.ContentType {
		case constants.RuntimeEventTypeThinkingStart, constants.RuntimeEventTypeToolUse,
			constants.RuntimeEventTypeToolUpdate, constants.RuntimeEventTypeToolResult,
			constants.RuntimeEventTypeTextDelta, constants.RuntimeEventTypeCommentaryDelta, constants.RuntimeEventTypeThinkingDelta:
			return true
		}
	}
	return false
}

func isFinalText(t constants.RuntimeEventType) bool {
	return t == "" || t == constants.RuntimeEventTypeTextDelta
}

func isClassifiableText(t constants.RuntimeEventType) bool {
	return isFinalText(t) || t == constants.RuntimeEventTypeCommentaryDelta
}
func isTextContent(t constants.RuntimeEventType) bool {
	return isFinalText(t) || t == constants.RuntimeEventTypeCommentaryDelta || t == constants.RuntimeEventTypeThinkingDelta
}

func validNativeTerminal(t nativeTerminal) bool {
	r := t.Result
	if !validNativeKey(r.Key) || r.Turn.InvocationID != r.Key.InvocationID {
		return false
	}
	if t.Body != nil && (r.FinalMessage != nil || !validID(t.Body.MessageID) || t.Body.First <= 0 || t.Body.Last < t.Body.First) {
		return false
	}
	if r.FinalMessage != nil && !validID(r.FinalMessage.ID) {
		return false
	}
	switch r.Turn.Origin {
	case acpmeta.NativeTurnOriginHost, acpmeta.NativeTurnOriginProvider, acpmeta.NativeTurnOriginUnknown:
	default:
		return false
	}
	switch r.Turn.Status {
	case acpmeta.NativeTurnCompleted, acpmeta.NativeTurnCancelled, acpmeta.NativeTurnFailed:
	default:
		return false
	}
	ids := make(map[string]bool, len(r.Turn.ConsumedInputIDs))
	for _, id := range r.Turn.ConsumedInputIDs {
		if !validID(id) || ids[id] {
			return false
		}
		ids[id] = true
	}
	return true
}

func nativeRowID(key agent.NativeResultKey) []byte {
	data, _ := json.Marshal(key)
	digest := sha256.Sum256(data)
	return digest[:]
}
func nativeID(key agent.NativeResultKey) string { return hex.EncodeToString(nativeRowID(key)) }

func nativeShard(session agent.NativeSessionKey) int {
	data, _ := json.Marshal(session)
	return int(sha256.Sum256(data)[0] % streamShards)
}
func (s *Store) nativeStream(key agent.NativeResultKey) (*streamSlot, string) {
	shard := nativeShard(key.Session)
	return &s.natives[shard], filepath.Join(s.root, nativeBucketPath(shard))
}

func (s *Store) CommitNativeFragments(ctx context.Context, batch agent.NativeFragmentBatch) (agent.NativeFragmentRange, error) {
	var result agent.NativeFragmentRange
	if !validNativeKey(batch.Key) || !validID(batch.CommitKey) || !validID(batch.MessageID) ||
		len(batch.Changes) == 0 || len(batch.Changes) > maxBatchRows {
		return result, status_error.ChatLogRecordInvalid
	}
	key := nativeID(batch.Key) + ":fragment:" + batch.CommitKey
	if !validID(key) {
		return result, status_error.ChatLogRecordInvalid
	}
	rows := make([]diskRow, 0, len(batch.Changes))
	for _, m := range batch.Changes {
		row := diskRow{Fragment: &nativeFragment{Key: batch.Key, MessageID: batch.MessageID, Change: m}}
		if !validDiskRow(row, false) {
			return result, status_error.ChatLogRecordInvalid
		}
		rows = append(rows, row)
	}
	_, request, err := marshalRows(rows)
	if err != nil {
		return result, err
	}
	slot, path := s.nativeStream(batch.Key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return result, err
	}
	old, ok, err := idx.batch(path, key)
	if err != nil {
		return result, err
	}
	if ok {
		if old.Request != request {
			return result, status_error.ChatLogIdempotencyConflict
		}
		return fragmentRange(batch, old), nil
	}
	terminal, err := idx.nativePosition(nativeRowID(batch.Key), indexNativeTerminal, "", "")
	if err != nil {
		return result, err
	}
	if terminal.Cursor > 0 {
		return result, status_error.ChatLogIdempotencyConflict
	}
	if err := compactToolRows(path, idx, rows); err != nil {
		return result, err
	}
	payload, _, err := marshalRows(rows)
	if err != nil {
		return result, err
	}
	header := makeHeader(key, request, idx.version(), len(rows))
	if err := slot.append(ctx, path, idx, header, rows, payload); err != nil {
		return result, err
	}
	return fragmentRange(batch, header), nil
}

func fragmentRange(batch agent.NativeFragmentBatch, header batchHeader) agent.NativeFragmentRange {
	return agent.NativeFragmentRange{
		Key: batch.Key, MessageID: batch.MessageID, First: header.Receipt.First, Last: header.Receipt.Version,
	}
}

// Compact across commits. A periodic independent snapshot bounds seek and replay costs.
func compactToolRows(path string, idx *streamIndex, rows []diskRow) error {
	if idx.version() == 0 {
		return nil
	}
	var f *os.File
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	for _, row := range rows {
		fragment := row.Fragment
		if fragment.Change.Kind != agent.MessageToolSnapshot {
			continue
		}
		previous, err := idx.nativePosition(nativeRowID(fragment.Key), indexNativeTool, fragment.MessageID, fragment.Change.ToolCallID)
		if err != nil {
			return err
		}
		if previous.Cursor == 0 {
			continue
		}
		if f == nil {
			var err error
			f, err = os.Open(path)
			if err != nil {
				return status_error.ChatLogError
			}
		}
		positions, err := idx.withPatchBases([]rowIndex{previous})
		if err != nil {
			return err
		}
		base, err := readFragment(f, positions, previous.Cursor, 0)
		if err != nil {
			return err
		}
		depth := 1
		if base.Patch != nil {
			depth += base.Patch.Depth
		}
		if depth > maximumToolPatchDepth {
			continue
		}
		before, after := base.Change.Data, fragment.Change.Data
		prefix, suffix := 0, 0
		for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
			prefix++
		}
		for suffix < len(before)-prefix && suffix < len(after)-prefix &&
			before[len(before)-1-suffix] == after[len(after)-1-suffix] {
			suffix++
		}
		patch := &toolPatch{Base: previous.Cursor, Depth: depth, Prefix: prefix, Suffix: suffix,
			Insert: after[prefix : len(after)-suffix]}
		encoded, err := json.Marshal(patch)
		if err == nil && len(encoded)+5 < len(after) {
			fragment.Change.Data, fragment.Patch = nil, patch
		}
	}
	return nil
}

func readFragment(file *os.File, positions map[int64]rowIndex, cursor int64, depth int) (*nativeFragment, error) {
	pos := positions[cursor]
	if pos.Cursor == 0 || depth > maximumToolPatchDepth {
		return nil, status_error.ChatLogCorrupted
	}
	row, err := readDiskRow(file, pos.Where)
	if err != nil || row.Fragment == nil {
		return nil, status_error.ChatLogCorrupted
	}
	f := row.Fragment
	if patch := f.Patch; patch != nil {
		if patch.Base >= cursor || patch.Depth < 1 || patch.Depth > maximumToolPatchDepth ||
			patch.Prefix < 0 || patch.Suffix < 0 {
			return nil, status_error.ChatLogCorrupted
		}
		base, err := readFragment(file, positions, patch.Base, depth+1)
		if err != nil || base.Key != f.Key || base.MessageID != f.MessageID || base.Change.ToolCallID != f.Change.ToolCallID ||
			patch.Prefix > len(base.Change.Data) || patch.Suffix > len(base.Change.Data)-patch.Prefix {
			return nil, status_error.ChatLogCorrupted
		}
		orig := base.Change.Data
		f.Change.Data = append(append(append(json.RawMessage(nil), orig[:patch.Prefix]...), patch.Insert...), orig[len(orig)-patch.Suffix:]...)
		if !json.Valid(f.Change.Data) {
			return nil, status_error.ChatLogCorrupted
		}
	}
	return f, nil
}

func (s *Store) ReadNativeFragments(ctx context.Context, r agent.NativeFragmentRange) ([]agent.MessageChange, error) {
	result := make([]agent.MessageChange, 0)
	err := s.visitNativeRange(ctx, r, func(change agent.MessageChange) {
		result = append(result, change)
	})
	return result, err
}

func (s *Store) visitNativeRange(ctx context.Context, r agent.NativeFragmentRange, visit func(agent.MessageChange)) error {
	slot, path := s.nativeStream(r.Key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return err
	}
	if !validNativeRange(r) || r.Last > idx.version() {
		return status_error.ChatLogNativeOutcomePending
	}
	var identity, lane int64
	err = idx.query(func(db *gorm.DB) error {
		var e error
		identity, lane, e = findNativeRefs(db, idx.coverage.Stream, nativeRowID(r.Key), r.MessageID)
		return e
	})
	if err != nil {
		return err
	}
	rows, err := idx.between(r.First, r.Last)
	if err != nil {
		return err
	}
	if int64(len(rows)) != r.Last-r.First+1 {
		return status_error.ChatLogCorrupted
	}
	for _, row := range rows {
		if identity == 0 || lane == 0 || row.NativeRef != identity || row.LaneRef != lane || !isNativeFragmentKind(row.Kind) {
			return status_error.ChatLogRecordInvalid
		}
	}
	if visit == nil {
		return nil
	}
	positions, err := idx.withPatchBases(rows)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return status_error.ChatLogError
	}
	defer file.Close()
	for _, row := range rows {
		f, err := readFragment(file, positions, row.Cursor, 0)
		if err != nil {
			return err
		}
		if f.Key != r.Key || f.MessageID != r.MessageID {
			return status_error.ChatLogCorrupted
		}
		visit(f.Change)
	}
	return nil
}

// CommitNativeResult makes canonical terminal evidence independent of any Chat.
func (s *Store) CommitNativeResult(ctx context.Context, result agent.NativeResult) error {
	terminal := nativeTerminal{Result: result}
	if !validNativeTerminal(terminal) {
		return status_error.ChatLogRecordInvalid
	}
	rows := []diskRow{{Terminal: &terminal}}
	_, request, err := marshalRows(rows)
	if err != nil {
		return err
	}
	id := nativeID(result.Key)
	key := id + ":terminal"
	slot, path := s.nativeStream(result.Key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return err
	}
	old, ok, err := idx.batch(path, key)
	if err != nil {
		return err
	}
	if ok {
		if old.Request != request {
			return status_error.ChatLogIdempotencyConflict
		}
		return nil
	}
	if result.FinalMessage != nil {
		body, err := findNativeBody(ctx, path, idx, result.Key, result.FinalMessage)
		if err != nil {
			return err
		}
		if body != nil {
			terminal.Body, terminal.Result.FinalMessage = body, nil
		}
	}
	payload, _, err := marshalRows(rows)
	if err != nil {
		return err
	}
	header := makeHeader(key, request, idx.version(), 1)
	return slot.append(ctx, path, idx, header, rows, payload)
}

func findNativeBody(ctx context.Context, path string, idx *streamIndex, key agent.NativeResultKey, final *agent.NativeFinalMessage) (*nativeBody, error) {
	classification, err := idx.nativePosition(nativeRowID(key), indexNativeClassification, final.ID, "")
	if err != nil {
		return nil, err
	}
	includeCommentary := classification.Content == constants.RuntimeEventTypeTextDelta
	ranges, err := idx.nativeText(nativeRowID(key), final.ID, includeCommentary)
	if err != nil {
		return nil, err
	}
	if len(ranges) == 0 {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, status_error.ChatLogError
	}
	defer file.Close()
	h := sha256.New()
	length, err := collectNativeText(ctx, file, key, final.ID, ranges, h, nil)
	if err != nil {
		return nil, err
	}
	if length > len(final.Text) {
		return nil, nil
	}
	prefix := sha256.Sum256([]byte(final.Text[:length]))
	if !bytes.Equal(h.Sum(nil), prefix[:]) {
		return nil, nil
	}
	return &nativeBody{MessageID: final.ID, First: ranges[0].Cursor, Last: ranges[len(ranges)-1].Cursor, Digest: sha256.Sum256([]byte(final.Text)), Tail: final.Text[length:], IncludeCommentary: includeCommentary}, nil
}

func collectNativeText(ctx context.Context, file *os.File, key agent.NativeResultKey, message string, rows []rowIndex, h hash.Hash, b *strings.Builder) (int, error) {
	var length int
	for _, pos := range rows {
		if err := contextError(ctx); err != nil {
			return 0, err
		}
		row, err := readDiskRow(file, pos.Where)
		if err != nil || row.Fragment == nil || row.Fragment.Key != key || row.Fragment.MessageID != message || !isClassifiableText(row.Fragment.Change.ContentType) {
			return 0, status_error.ChatLogCorrupted
		}
		length += len(row.Fragment.Change.Text)
		_, _ = h.Write([]byte(row.Fragment.Change.Text))
		if b != nil {
			b.WriteString(row.Fragment.Change.Text)
		}
	}
	return length, nil
}

func (s *Store) readNativeTerminal(ctx context.Context, key agent.NativeResultKey) (nativeTerminal, error) {
	return s.readTerminal(ctx, key, false)
}

func (s *Store) ReadNativeResult(ctx context.Context, key agent.NativeResultKey) (agent.NativeResult, error) {
	t, err := s.readTerminal(ctx, key, true)
	return t.Result, err
}

func (s *Store) readTerminal(ctx context.Context, key agent.NativeResultKey, body bool) (nativeTerminal, error) {
	var t nativeTerminal
	if !validNativeKey(key) {
		return t, status_error.ChatLogRecordInvalid
	}
	slot, path := s.nativeStream(key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return t, err
	}
	id := nativeRowID(key)
	pos, err := idx.nativePosition(id, indexNativeTerminal, "", "")
	if err != nil {
		return t, err
	}
	if pos.Cursor == 0 {
		return t, status_error.ChatLogNativeOutcomePending
	}
	file, err := os.Open(path)
	if err != nil {
		return t, status_error.ChatLogError
	}
	defer file.Close()
	row, err := readDiskRow(file, pos.Where)
	if err != nil || row.Terminal == nil || row.Terminal.Result.Key != key {
		return t, status_error.ChatLogCorrupted
	}
	t = *row.Terminal
	if !body || t.Body == nil {
		return t, nil
	}
	cursors, err := idx.nativeText(id, t.Body.MessageID, t.Body.IncludeCommentary)
	if err != nil {
		return nativeTerminal{}, err
	}
	if t.Body.Last >= pos.Cursor {
		return nativeTerminal{}, status_error.ChatLogCorrupted
	}
	t.Result.FinalMessage, err = readNativeBody(ctx, file, key, *t.Body, cursors)
	if err != nil {
		return nativeTerminal{}, err
	}
	return t, nil
}

func readNativeBody(ctx context.Context, file *os.File, key agent.NativeResultKey, body nativeBody, rows []rowIndex) (*agent.NativeFinalMessage, error) {
	if len(rows) == 0 || rows[0].Cursor != body.First || rows[len(rows)-1].Cursor != body.Last {
		return nil, status_error.ChatLogCorrupted
	}
	h, text := sha256.New(), new(strings.Builder)
	if _, err := collectNativeText(ctx, file, key, body.MessageID, rows, h, text); err != nil {
		return nil, err
	}
	_, _ = h.Write([]byte(body.Tail))
	text.WriteString(body.Tail)
	if !bytes.Equal(h.Sum(nil), body.Digest[:]) {
		return nil, status_error.ChatLogCorrupted
	}
	return &agent.NativeFinalMessage{ID: body.MessageID, Text: text.String()}, nil
}

// ConfirmNativeResult is the gate before acknowledging a provider receipt.
func (s *Store) ConfirmNativeResult(ctx context.Context, key agent.NativeResultKey, refs []agent.HostInputRef) error {
	t, err := s.readNativeTerminal(ctx, key)
	if err != nil {
		return err
	}
	ids := t.Result.Turn.ConsumedInputIDs
	if len(refs) != len(ids) || slices.Contains(t.Result.Unavailable, acpmeta.ReceiptConsumedInputsUnavailable) {
		return status_error.ChatLogNativeOutcomePending
	}
	expected := make(map[string]bool, len(ids))
	for _, id := range ids {
		expected[id] = true
	}
	seen := make(map[string]bool, len(ids))
	for _, ref := range refs {
		var input agent.HostInput
		if ref.ChatID == "" {
			input, err = s.readDetachedInput(ctx, key.Session, ref.HostInvocationID)
		} else {
			var relation *agent.InputConsumption
			input, relation, err = s.ReadHostInputConsumption(ctx, ref)
			if err == nil && (relation == nil || relation.Result != key) {
				return status_error.ChatLogNativeOutcomePending
			}
		}
		if err != nil {
			return err
		}
		if input.Session != key.Session || !expected[input.AdapterInputID] || seen[input.AdapterInputID] {
			return status_error.ChatLogNativeOutcomePending
		}
		seen[input.AdapterInputID] = true
	}
	return nil
}

func validHostInput(input agent.HostInput) bool {
	return validID(input.HostInvocationID) && validID(input.AdapterInputID) &&
		len(input.NativeInputID) <= maxIdentityBytes && validNativeSession(input.Session) &&
		(input.AssistantMessageID == "" || validID(input.AssistantMessageID))
}

func detachedRowID(session agent.NativeSessionKey, host string) []byte {
	return nativeRowID(agent.NativeResultKey{Session: session, InvocationID: host})
}
func detachedID(session agent.NativeSessionKey, host string) string {
	return hex.EncodeToString(detachedRowID(session, host)) + ":input"
}

// CommitDetachedInput durably admits private work without creating public Chat facts.
func (s *Store) CommitDetachedInput(ctx context.Context, input agent.HostInput) error {
	row := diskRow{DetachedInput: &input}
	if !validDiskRow(row, false) {
		return status_error.ChatLogRecordInvalid
	}
	key := detachedID(input.Session, input.HostInvocationID)
	raw, request, err := marshalRows([]diskRow{row})
	if err != nil {
		return err
	}
	check, release, err := s.prepareInputClaims(ctx, []agent.HostInput{input})
	if err != nil {
		return err
	}
	defer release()
	slot, path := s.nativeStream(agent.NativeResultKey{Session: input.Session})
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return err
	}
	old, ok, err := idx.batch(path, key)
	if err != nil {
		return err
	}
	if ok {
		if old.Request != request {
			return status_error.ChatLogIdempotencyConflict
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	header := makeHeader(key, request, idx.version(), 1)
	return slot.append(ctx, path, idx, header, []diskRow{row}, raw)
}

func (s *Store) readDetachedInput(ctx context.Context, session agent.NativeSessionKey, host string) (agent.HostInput, error) {
	var input agent.HostInput
	slot, path := s.nativeStream(agent.NativeResultKey{Session: session})
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return input, err
	}
	pos, err := idx.nativePosition(detachedRowID(session, host), indexDetachedInput, "", "")
	if err != nil {
		return input, err
	}
	if pos.Cursor == 0 {
		return input, status_error.ChatLogNativeOutcomePending
	}
	file, err := os.Open(path)
	if err != nil {
		return input, status_error.ChatLogError
	}
	defer file.Close()
	row, err := readDiskRow(file, pos.Where)
	if err != nil || row.DetachedInput == nil || row.DetachedInput.Session != session || row.DetachedInput.HostInvocationID != host {
		return input, status_error.ChatLogCorrupted
	}
	return *row.DetachedInput, nil
}

func isNativeFragmentKind(k indexKind) bool {
	return k == indexNativeClassification || (k >= indexNativeTextAppend && k <= indexNativeEvent)
}
