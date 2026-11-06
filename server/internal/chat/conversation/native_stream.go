package conversation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"gorm.io/gorm"
)

const (
	maximumToolPatchDepth = 8
	toolOutputRecordBytes = 8 << 10
)

type nativeFragment struct {
	// Key 标识产生片段的唯一原生调用。
	Key agent.NativeResultKey `json:"k"`
	// MessageID 标识原生调用内的消息。
	MessageID string `json:"m"`
	// Change 保存规范变化。
	Change agent.MessageChange `json:"c"`
	// Patch 对重复工具元数据提供有限深度的无损差量。
	Patch *toolPatch `json:"p,omitempty"`
	// Output 引用本次快照已包含的独立终端输出范围。
	Output *toolOutputRange `json:"o,omitempty"`
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
		return isClassifiableText(m.ContentType) && m.ToolCallID == "" && len(m.Data) == 0 && f.Patch == nil
	case nativeToolOutput:
		return m.ToolCallID != "" && m.Text != "" && len(m.Data) == 0 && f.Patch == nil && f.Output == nil
	case agent.MessageToolUpdate:
		return validID(m.ToolCallID) && utf8.ValidString(m.Text) && json.Valid(m.Data) && f.Patch == nil && f.Output == nil
	case agent.MessageToolSnapshot:
		if f.Output != nil && (!validID(f.Output.MessageID) || f.Output.First <= 0 || f.Output.Last < f.Output.First || f.Output.Bytes <= 0) {
			return false
		}
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
	digest := key.Digest()
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

// PrepareNativeToolUpdate 按本 Store 的编码和行数预算选择单次逻辑工具更新，不预留执行席位。
// 返回的操作由调用者冻结重试；CommitNativeFragments 仍执行最终原子预算校验。
func (s *Store) PrepareNativeToolUpdate(ctx context.Context, batch agent.NativeFragmentBatch) (agent.NativeFragmentBatch, error) {
	if !validNativeKey(batch.Key) || !validID(batch.CommitKey) || !validID(batch.MessageID) || len(batch.Changes) != 1 ||
		batch.Changes[0].Kind != agent.MessageToolUpdate || !validNativeFragment(nativeFragment{Key: batch.Key, MessageID: batch.MessageID, Change: batch.Changes[0]}) {
		return agent.NativeFragmentBatch{}, status_error.ChatLogRecordInvalid
	}
	slot, path := s.nativeStream(batch.Key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	idx, err := slot.load(ctx, path, false)
	if err != nil {
		return agent.NativeFragmentBatch{}, err
	}
	original := batch.Changes[0]
	partial := original.Data
	fields, err := decodeToolUpdateMetadata(original.Data, original.ToolCallID)
	if err != nil {
		return agent.NativeFragmentBatch{}, err
	}
	var status string
	_ = json.Unmarshal(fields["status"], &status)
	if status == "completed" || status == "failed" || status == "cancelled" {
		delete(fields, "status")
		partial, _ = json.Marshal(fields)
	}
	candidate := func(n int) (agent.NativeFragmentBatch, bool, error) {
		change := original
		change.Text = original.Text[:n]
		if n < len(original.Text) {
			change.Data = partial
		}
		prepared := batch
		prepared.Changes = []agent.MessageChange{change}
		rows := []diskRow{{Fragment: &nativeFragment{Key: batch.Key, MessageID: batch.MessageID, Change: change}}}
		rows, err := normalizeToolRows(path, idx, prepared, rows)
		if err != nil {
			if errors.Is(err, status_error.ChatLogRecordInvalid) {
				return prepared, false, nil
			}
			return prepared, false, err
		}
		skeleton := make([][]byte, len(rows))
		size := 0
		for i, row := range rows {
			text := ""
			if row.Fragment.Change.Kind == nativeToolOutput {
				// JSON needs at most six bytes per UTF-8 input byte. Size a
				// constant payload here; the real bytes are encoded only at commit.
				fragment := *row.Fragment
				text, fragment.Change.Text = fragment.Change.Text, " "
				row.Fragment = &fragment
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				return prepared, false, err
			}
			if text != "" {
				size += len(encoded) + 6*len(text) - 1
			} else {
				size += len(encoded)
			}
			skeleton[i] = []byte("{}")
			size -= 2
		}
		header := makeHeader(nativeID(batch.Key)+":fragment:"+batch.CommitKey, [32]byte{}, idx.version(), len(rows))
		header.Receipt.RecordedAt = math.MaxInt64
		frame, _, err := encodeBatch(header, skeleton, 0)
		// A concurrent different invocation in this shard can grow three cursor
		// integers (header, output First/Last) to their maximum encoded width.
		reserve := 3 * len(strconv.FormatInt(math.MaxInt64, 10))
		return prepared, err == nil && size+len(frame)+reserve <= maxBatchBytes, nil
	}
	// At least one metadata snapshot accompanies this Store's physical output
	// records. Neither this row budget nor the codec layout crosses into Agent.
	maxText := min(len(original.Text), (maxBatchRows-1)*toolOutputRecordBytes)
	for maxText < len(original.Text) && !utf8.RuneStart(original.Text[maxText]) {
		maxText--
	}
	prepared, fits, err := candidate(maxText)
	if err != nil || fits {
		return prepared, err
	}
	low, high := 0, maxText
	var best agent.NativeFragmentBatch
	for low < high {
		mid := low + (high-low+1)/2
		n := mid
		for n < len(original.Text) && n > 0 && !utf8.RuneStart(original.Text[n]) {
			n--
		}
		trial, fits, err := candidate(n)
		if err != nil {
			return agent.NativeFragmentBatch{}, err
		}
		if fits && n > 0 {
			low, best = mid, trial
		} else {
			high = mid - 1
		}
	}
	if len(best.Changes) == 0 || len(best.Changes[0].Text) == 0 {
		return agent.NativeFragmentBatch{}, status_error.ChatLogRecordInvalid
	}
	return best, nil
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
	rows, err = normalizeToolRows(path, idx, batch, rows)
	if err != nil {
		return result, err
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
		Key: batch.Key, MessageID: batch.MessageID,
		First: header.Receipt.Version - int64(len(batch.Changes)) + 1, Last: header.Receipt.Version,
	}
}

// nativeToolOutput is a private append record; public links select snapshots only.
const nativeToolOutput agent.MessageChangeKind = "tool_output"

type toolOutputRange struct {
	// MessageID 标识仅包含此工具输出的原生 lane，复用现有定位索引。
	MessageID string `json:"m"`
	// First 是此快照包含的首个输出段游标。
	First int64 `json:"f"`
	// Last 是此快照包含的末个输出段游标，不读取未来输出。
	Last int64 `json:"l"`
	// Bytes 是范围内完整 UTF-8 输出的字节总数。
	Bytes int64 `json:"b"`
}

func normalizeToolRows(path string, idx *streamIndex, batch agent.NativeFragmentBatch, rows []diskRow) ([]diskRow, error) {
	var file *os.File
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	latest := make(map[string]*nativeFragment)
	var outputs []diskRow
	for _, row := range rows {
		fragment := row.Fragment
		tool := fragment.Change.ToolCallID
		fields := make(map[string]json.RawMessage)
		var output *toolOutputRange
		delta := fragment.Change.Text
		switch fragment.Change.Kind {
		case agent.MessageToolSnapshot:
			if json.Unmarshal(fragment.Change.Data, &fields) != nil || fields == nil || fields["terminalOutput"] == nil {
				latest[tool] = fragment
				continue
			}
			// A current complete replacement establishes a new output frontier.
			// Normalize it on input, exactly where partial updates are normalized.
			if json.Unmarshal(fields["terminalOutput"], &delta) != nil {
				return nil, status_error.ChatLogRecordInvalid
			}
			delete(fields, "terminalOutput")
		case agent.MessageToolUpdate:
			base := latest[tool]
			if base == nil {
				pos, err := idx.nativePosition(nativeRowID(batch.Key), indexNativeTool, batch.MessageID, tool)
				if err != nil {
					return nil, err
				}
				if pos.Cursor > 0 {
					if file == nil {
						file, err = os.Open(path)
						if err != nil {
							return nil, status_error.ChatLogError
						}
					}
					positions, err := idx.withPatchBases([]rowIndex{pos})
					if err != nil {
						return nil, err
					}
					base, err = readFragment(file, positions, pos.Cursor, 0)
					if err != nil {
						return nil, err
					}
				}
			}
			if base != nil {
				if json.Unmarshal(base.Change.Data, &fields) != nil || fields == nil || fields["terminalOutput"] != nil || fields["terminalDelta"] != nil {
					return nil, status_error.ChatLogCorrupted
				}
				if base.Output != nil {
					copy := *base.Output
					output = &copy
				}
			}
			update, err := decodeToolUpdateMetadata(fragment.Change.Data, tool)
			if err != nil {
				return nil, err
			}
			for name, value := range update {
				fields[name] = value
			}
		default:
			continue
		}
		data, err := json.Marshal(fields)
		if err != nil {
			return nil, status_error.ChatLogRecordInvalid
		}
		lane := toolOutputLane(batch.MessageID, tool)
		for text := delta; len(text) > 0; {
			n := min(len(text), toolOutputRecordBytes)
			for n < len(text) && !utf8.RuneStart(text[n]) {
				n--
			}
			cursor := idx.version() + int64(len(outputs)) + 1
			if output == nil {
				output = &toolOutputRange{MessageID: lane, First: cursor}
			}
			if output.Bytes > math.MaxInt64-int64(n) || output.MessageID != lane {
				return nil, status_error.ChatLogRecordInvalid
			}
			output.Last, output.Bytes = cursor, output.Bytes+int64(n)
			outputs = append(outputs, diskRow{Fragment: &nativeFragment{Key: batch.Key, MessageID: lane,
				Change: agent.MessageChange{Kind: nativeToolOutput, ToolCallID: tool, Text: text[:n]}}})
			text = text[n:]
			if len(outputs)+len(rows) > maxBatchRows {
				return nil, status_error.ChatLogRecordInvalid
			}
		}
		fragment.Change.Kind, fragment.Change.Data, fragment.Change.Text = agent.MessageToolSnapshot, data, ""
		fragment.Output = output
		latest[tool] = fragment
	}
	return append(outputs, rows...), nil
}

func decodeToolUpdateMetadata(data json.RawMessage, tool string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil || fields["terminalDelta"] != nil || fields["terminalOutput"] != nil {
		return nil, status_error.ChatLogRecordInvalid
	}
	var id string
	if raw := fields["id"]; raw != nil && (json.Unmarshal(raw, &id) != nil || id != tool) {
		return nil, status_error.ChatLogRecordInvalid
	}
	return fields, nil
}

func toolOutputLane(message, tool string) string {
	data, _ := json.Marshal([]string{message, tool})
	hash := sha256.Sum256(data)
	return "tool-output:" + hex.EncodeToString(hash[:])
}

func hydrateToolOutput(ctx context.Context, file *os.File, idx *streamIndex, fragment *nativeFragment) error {
	ref := fragment.Output
	if ref == nil {
		return nil
	}
	var identity, lane int64
	err := idx.query(func(db *gorm.DB) error {
		var err error
		identity, lane, err = findNativeRefs(db, idx.coverage.Stream, nativeRowID(fragment.Key), ref.MessageID)
		return err
	})
	if err != nil {
		return err
	}
	if identity == 0 || lane == 0 || ref.First <= 0 || ref.Last < ref.First || ref.Bytes <= 0 {
		return status_error.ChatLogCorrupted
	}
	var output strings.Builder
	next := ref.First
	var last int64
	for next <= ref.Last {
		if err := contextError(ctx); err != nil {
			return err
		}
		var rows []rowIndex
		err := idx.query(func(db *gorm.DB) error {
			return db.Table("conversation_positions INDEXED BY conversation_row_native").
				Where("stream = ? AND native_ref <> 0 AND native_ref = ? AND kind = ? AND lane_ref = ? AND cursor BETWEEN ? AND ?",
					idx.coverage.Stream, identity, indexNativeToolOutput, lane, next, ref.Last).
				Order("cursor").Limit(indexReadBatch).Find(&rows).Error
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return status_error.ChatLogCorrupted
		}
		for _, pos := range rows {
			row, err := readDiskRow(file, pos.Where)
			if err != nil || row.Fragment == nil || row.Fragment.Key != fragment.Key || row.Fragment.MessageID != ref.MessageID ||
				row.Fragment.Change.Kind != nativeToolOutput || row.Fragment.Change.ToolCallID != fragment.Change.ToolCallID {
				return status_error.ChatLogCorrupted
			}
			if int64(output.Len())+int64(len(row.Fragment.Change.Text)) > ref.Bytes {
				return status_error.ChatLogCorrupted
			}
			output.WriteString(row.Fragment.Change.Text)
			last = pos.Cursor
		}
		next = last + 1
	}
	if last != ref.Last || int64(output.Len()) != ref.Bytes {
		return status_error.ChatLogCorrupted
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(fragment.Change.Data, &fields) != nil || fields == nil {
		return status_error.ChatLogCorrupted
	}
	fields["terminalOutput"], err = json.Marshal(output.String())
	if err != nil {
		return status_error.ChatLogCorrupted
	}
	fragment.Change.Data, err = json.Marshal(fields)
	return err
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
		if err := hydrateToolOutput(ctx, file, idx, f); err != nil {
			return err
		}
		visit(f.Change)
	}
	return nil
}

// CommitNativeResult makes canonical terminal evidence independent of any Chat.
func (s *Store) CommitNativeResult(ctx context.Context, result agent.NativeResult) error {
	// Attribution is resolved by the Store from a committed admission. It is
	// not an adapter input and must not change an existing idempotency hash.
	result.Attribution = nil
	terminal := nativeTerminal{Result: result}
	if !validNativeTerminal(terminal) {
		return status_error.ChatLogRecordInvalid
	}
	rows := []diskRow{{Terminal: &terminal}}
	_, request, err := marshalRows(rows)
	if err != nil {
		return err
	}
	terminal.Result.Attribution = s.nativeUsageAttribution(ctx, result)
	id := nativeID(result.Key)
	key := id + ":terminal"
	slot, path := s.nativeStream(result.Key)
	committed := false
	defer func() {
		if committed {
			if notify := s.usageChanged.Load(); notify != nil {
				(*notify)()
			}
		}
	}()
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
	err = slot.append(ctx, path, idx, header, rows, payload)
	committed = err == nil
	return err
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
	return k == indexNativeClassification || k == indexNativeToolOutput || (k >= indexNativeTextAppend && k <= indexNativeEvent)
}
