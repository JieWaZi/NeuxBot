package eventlog_next

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"gorm.io/gorm"
)

// streamSlot serializes authority access for one shard.
type streamSlot struct {
	// mu protects writes, recovery and reads of the same stream.
	mu sync.Mutex
	// indexes is the shared disk-backed positioning owner.
	indexes *indexDB
	// store resolves native dependencies before rebuilding a public stream.
	store *Store
}

// diskRow is one version-2 semantic operation.
type diskRow struct {
	// Chat is a public conversation change.
	Chat *agent.ConversationChange `json:"c,omitempty"`
	// Fragment is a canonical native message change.
	Fragment *nativeFragment `json:"f,omitempty"`
	// DetachedInput is an admitted input without a public Chat.
	DetachedInput *agent.HostInput `json:"h,omitempty"`
	// Terminal is a canonical native terminal fact.
	Terminal *nativeTerminal `json:"t,omitempty"`
}

// rowIndex stores just enough identity to select an exact read.
type rowIndex struct {
	// Stream is the compact technical stream reference.
	Stream int64 `gorm:"primaryKey;autoIncrement:false;index:conversation_row_message,priority:1,where:message_ref <> 0 AND NOT display_link;index:conversation_row_execution,priority:1,where:execution_ref <> 0;index:conversation_row_host,priority:1,where:host <> '';index:conversation_row_native,priority:1,where:native_ref <> 0"`
	// Cursor is the logical position within the authoritative stream.
	Cursor int64 `gorm:"primaryKey;autoIncrement:false;index:conversation_row_message,priority:3;index:conversation_row_execution,priority:3;index:conversation_row_host,priority:4;index:conversation_row_native,priority:5"`
	// Where locates the exact raw JSON row in the stream.
	Where location `gorm:"embedded;embeddedPrefix:loc_"`
	// At is the original commit time.
	At int64
	// LegacyStart is the legacy decoding scope cursor, or zero for a v2 row.
	LegacyStart int64
	// Message is supplied by the authority and interned before indexing.
	Message string `gorm:"-"`
	// MessageRef is the compact reference to the public message anchor.
	MessageRef int64 `gorm:"index:conversation_row_message,priority:2"`
	// Execution is the authoritative identity supplied to the index writer.
	Execution string `gorm:"-"`
	// ExecutionRef is the compact reference to the execution locator.
	ExecutionRef int64 `gorm:"index:conversation_row_execution,priority:2"`
	// Host identifies the associated host input.
	Host string `gorm:"index:conversation_row_host,priority:2"`
	// Native is the authoritative native identity supplied to the index writer.
	Native []byte `gorm:"-"`
	// NativeRef is the first cursor for this native invocation or detached input.
	NativeRef int64 `gorm:"index:conversation_row_native,priority:2"`
	// LaneRef is the first cursor for this native message.
	LaneRef int64 `gorm:"index:conversation_row_native,priority:4"`
	// Project is the original committed reference, used only when deriving effective display.
	Project *agent.MessageChange `gorm:"-"`
	// Revision is the producer-assigned revision of a native change.
	Revision int64
	// DisplayLink excludes native reference rows from ordinary message positions.
	DisplayLink bool
	// Tool identifies the tool affected by this row.
	Tool string
	// Kind drives derived index projections.
	Kind indexKind `gorm:"index:conversation_row_host,priority:3;index:conversation_row_native,priority:3"`
	// Content distinguishes final text and each kind of tool or reasoning segment.
	Content constants.RuntimeEventType
	// Adapter addresses a durable input by its frozen session and adapter ID.
	Adapter []byte `gorm:"index:conversation_row_adapter,where:adapter IS NOT NULL"`
	// PatchBase locates the previous complete or patched tool snapshot.
	PatchBase int64
}

func (rowIndex) TableName() string { return "conversation_positions" }

type indexKind uint8

const (
	indexOther indexKind = iota
	indexUserMessage
	indexAssistantMessage
	indexTextReplace
	indexMessageComplete
	indexInherited
	indexExecutionCreated
	indexExecutionCompleted
	indexExecutionEnded
	indexBinding
	indexInput
	indexConsumption
	indexNativeTerminal
	indexNativeTextAppend
	indexNativeTextReplace
	indexNativeTool
	indexNativeEvent
	indexDetachedInput
	indexNativeClassification
)

func contextError(ctx context.Context) error {
	switch ctx.Err() {
	case nil:
		return nil
	case context.DeadlineExceeded:
		return status_error.OperationTimeout
	default:
		return status_error.OperationCanceled
	}
}

func (s *streamSlot) load(ctx context.Context, path string, legacy bool) (*streamIndex, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(filepath.Dir(s.indexes.path), path)
	if err != nil {
		return nil, status_error.ChatLogError
	}
	idx := &streamIndex{db: s.indexes, ctx: ctx, coverage: streamCoverage{Path: rel}}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		s.indexes.markDirty(rel, false)
		return idx, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, status_error.ChatLogError
	}
	if err := idx.loadCoverage(); err != nil {
		return nil, err
	}
	if idx.coverage.Stream != 0 && idx.coverage.Size == info.Size() && idx.coverage.Stamp == info.ModTime().UnixNano() {
		s.indexes.markDirty(rel, false)
		return idx, nil
	}
	if legacy {
		// Native authority precedes a public link. Resolve its locator before taking SQLite's transaction lock.
		if err := s.store.prepareNativeCoverage(ctx); err != nil {
			return nil, err
		}
		if err := idx.loadCoverage(); err != nil {
			return nil, err
		}
	}
	if err := idx.rebuild(path, legacy); err != nil {
		return nil, err
	}
	s.indexes.markDirty(rel, false)
	return idx, nil
}

func (i *streamIndex) rebuild(path string, legacy bool) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return status_error.ChatLogError
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64<<10)
	var scanError error
	err = i.query(func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			coverage := streamCoverage{Path: i.coverage.Path}
			if i.coverage.Stream != 0 {
				if err := clearIndexStream(tx, i.coverage.Stream); err != nil {
					return err
				}
			}
			if err := tx.Create(&coverage).Error; err != nil {
				return err
			}
			info, err := scanStream(i.ctx, file, reader, path, legacy, func(rows []rowIndex, headers []batchHeader) error {
				if err := indexRows(tx, coverage.Stream, rows, headers); err != nil {
					return err
				}
				coverage.Version += int64(len(rows))
				for _, row := range rows {
					if row.Kind == indexBinding {
						coverage.Binding = row.Cursor
					}
				}
				return nil
			})
			if err != nil {
				scanError = err
				return err
			}
			coverage.Size, coverage.Stamp = info.Size(), info.ModTime().UnixNano()
			if err := tx.Model(&streamCoverage{}).Where("stream = ?", coverage.Stream).Updates(map[string]any{
				"size": coverage.Size, "stamp": coverage.Stamp, "version": coverage.Version, "binding": coverage.Binding,
			}).Error; err != nil {
				return err
			}
			i.coverage = coverage
			return nil
		})
	})
	if scanError != nil {
		if errors.Is(scanError, gorm.ErrDuplicatedKey) {
			return status_error.ChatLogCorrupted
		}
		if errors.Is(scanError, status_error.ChatLogCorrupted) || errors.Is(scanError, status_error.ChatLogError) {
			return scanError
		}
	}
	return err
}

func scanStream(ctx context.Context, file *os.File, reader *bufio.Reader, path string, legacy bool, visit func([]rowIndex, []batchHeader) error) (os.FileInfo, error) {
	var offset, version int64
	nextFormat := false
	scope := legacyScope{}
	var rows []rowIndex
	var headers []batchHeader
	for {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		line, err := readLine(reader, legacy && !nextFormat)
		if errors.Is(err, io.EOF) {
			if len(line) > 0 {
				if err := file.Truncate(offset); err != nil {
					return nil, status_error.ChatLogError
				}
			}
			// A failed prior fsync may have left a complete frame without a reply.
			if file.Sync() != nil || syncDirectory(filepath.Dir(path)) != nil {
				return nil, status_error.ChatLogError
			}
			break
		}
		if err != nil {
			return nil, err
		}
		switch {
		case len(line) > 0 && line[0] == '[':
			nextFormat = true
			first := len(rows)
			header, err := decodeBatch(line, offset, version, func(raw []byte, at location) error {
				var row diskRow
				if json.Unmarshal(raw, &row) != nil || !validDiskRow(row, legacy) {
					return status_error.ChatLogCorrupted
				}
				version++
				loc := describe(row, at, 0)
				loc.Cursor = version
				rows = append(rows, loc)
				return nil
			})
			if err != nil {
				return nil, err
			}
			for j := first; j < len(rows); j++ {
				rows[j].At = header.Receipt.RecordedAt
			}
			headers = append(headers, header)
		case legacy && !nextFormat:
			raw := bytes.TrimSuffix(line, []byte{'\n'})
			if len(raw) > 0 {
				fact, _, start, err := decodeLegacy(raw, version+1, &scope)
				if err != nil {
					return nil, err
				}
				version++
				row := describe(diskRow{Chat: &agent.ConversationChange{Fact: &fact}}, rowLocation(offset, raw), 0)
				row.LegacyStart, row.Cursor = start, version
				rows = append(rows, row)
			}
		default:
			return nil, status_error.ChatLogCorrupted
		}
		offset += int64(len(line))
		if len(rows) >= indexWriteBatch {
			if err := visit(rows, headers); err != nil {
				return nil, err
			}
			rows, headers = rows[:0], headers[:0]
		}
	}
	if err := visit(rows, headers); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, status_error.ChatLogError
	}
	return info, nil
}

func readLine(r *bufio.Reader, allowLegacy bool) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if err == nil || errors.Is(err, io.EOF) {
		return line, err
	}
	maximum := maxBatchBytes
	if allowLegacy && len(line) > 0 && line[0] != '[' {
		maximum = maxLegacyLineBytes
	}
	out := append([]byte(nil), line...)
	for errors.Is(err, bufio.ErrBufferFull) {
		line, err = r.ReadSlice('\n')
		if len(line) > maximum-len(out) {
			return nil, status_error.ChatLogCorrupted
		}
		out = append(out, line...)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, status_error.ChatLogError
	}
	return out, err
}

func (s *streamSlot) append(ctx context.Context, path string, idx *streamIndex, header batchHeader, rows []diskRow, payload [][]byte) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	frame, positions, err := encodeBatch(header, payload, idx.size())
	if err != nil {
		return err
	}
	if err := makeDurableDirs(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return status_error.ChatLogError
	}
	// A complete frame can survive an ambiguous sync error. Admission checks
	// recover this stream before trusting the inverse input index again.
	s.indexes.markDirty(idx.coverage.Path, true)
	n, writeErr := file.Write(frame)
	syncErr := file.Sync()
	closeErr := file.Close()
	if n != len(frame) || writeErr != nil || syncErr != nil || closeErr != nil {
		return status_error.ChatLogError
	}
	if idx.size() == 0 && syncDirectory(filepath.Dir(path)) != nil {
		return status_error.ChatLogError
	}
	info, err := os.Stat(path)
	if err != nil {
		return status_error.ChatLogError
	}
	additions := make([]rowIndex, 0, len(rows))
	for j, row := range rows {
		pos := describe(row, positions[j], header.Receipt.RecordedAt)
		pos.Cursor = header.Receipt.First + int64(j)
		additions = append(additions, pos)
	}
	// The authoritative commit has succeeded. Stale coverage forces the next access to rebuild.
	header.Where = rowLocation(idx.size(), frame[:positions[0].Offset-idx.size()])
	err = idx.publish(info, additions, header)
	s.indexes.markDirty(idx.coverage.Path, err != nil)
	return nil
}

func marshalRows(rows []diskRow) ([][]byte, [32]byte, error) {
	var size int
	hash := sha256.New()
	payload := make([][]byte, 0, len(rows))
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil || len(raw) > maxBatchBytes-size {
			return nil, [32]byte{}, status_error.ChatLogRecordInvalid
		}
		size += len(raw)
		payload = append(payload, raw)
		_, _ = hash.Write(raw)
		_, _ = hash.Write([]byte{'\n'})
	}
	return payload, [32]byte(hash.Sum(nil)), nil
}

func readRaw(file io.ReaderAt, at location) ([]byte, error) {
	if at.Length <= 0 || at.Length > maxLegacyLineBytes || at.Offset < 0 {
		return nil, status_error.ChatLogIndexCorrupted
	}
	body := make([]byte, at.Length)
	if _, err := file.ReadAt(body, at.Offset); err != nil || crc32.ChecksumIEEE(body) != at.Checksum {
		return nil, status_error.ChatLogCorrupted
	}
	return body, nil
}

func readDiskRow(file io.ReaderAt, at location) (diskRow, error) {
	var row diskRow
	body, err := readRaw(file, at)
	if err != nil {
		return row, err
	}
	if json.Unmarshal(body, &row) != nil {
		return row, status_error.ChatLogCorrupted
	}
	return row, nil
}

func makeDurableDirs(path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil
	}
	parent := filepath.Dir(path)
	if parent == path {
		return status_error.ChatLogError
	}
	if err := makeDurableDirs(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return status_error.ChatLogError
	}
	return syncDirectory(parent)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return status_error.ChatLogError
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return status_error.ChatLogError
	}
	return nil
}
