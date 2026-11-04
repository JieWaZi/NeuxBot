package eventlog_next

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	storesqlite "github.com/JieWaZi/NeuxBot/server/internal/sqlite"
	glebarez "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const (
	indexFilename   = "conversation-index-v3.sqlite"
	indexWriteBatch = 128
	indexReadBatch  = 256
)

// indexDB owns the disposable SQLite locator and its connection lifecycle.
type indexDB struct {
	// mu serializes short database operations and exclusive rebuilds.
	mu sync.Mutex
	// path is the managed location of the derived database.
	path string
	// db is opened lazily and shared across all streams.
	db *gorm.DB
	// identity is the actual active SQLite file, independent of its name.
	identity os.FileInfo
	// epoch changes when database replacement invalidates the reverse lookup coverage.
	epoch uint64
	// dirty contains authoritative streams whose latest commit did not update this database.
	dirty map[string]struct{}
	// temporary is the private fallback directory when the managed path cannot host an index.
	temporary string
}

type streamCoverage struct {
	// Stream is the compact technical reference used in derived rows.
	Stream int64 `gorm:"primaryKey;autoIncrement"`
	// Path locates the authoritative file relative to the managed root.
	Path string `gorm:"uniqueIndex;not null"`
	// Size is the validated end of the JSONL prefix.
	Size int64
	// Stamp identifies the file modification time that supplied this projection.
	Stamp int64
	// Version is the durable logical row count at Size.
	Version int64
	// Binding is the most recent v2 binding or clear cursor.
	Binding int64
}

func (streamCoverage) TableName() string { return "conversation_streams" }

type indexBatch struct {
	// Stream owns the idempotency namespace.
	Stream int64 `gorm:"primaryKey;autoIncrement:false"`
	// Digest is a binary lookup for the command key in the authoritative frame header.
	Digest []byte `gorm:"primaryKey"`
	// Where locates the authoritative receipt header without loading its rows.
	Where location `gorm:"embedded;embeddedPrefix:loc_"`
}

func (indexBatch) TableName() string { return "conversation_batches" }

type messageAnchor struct {
	// Stream scopes each message identifier.
	Stream int64 `gorm:"primaryKey;autoIncrement:false;index:conversation_anchors_cursor,priority:1;index:conversation_anchors_history,priority:1;index:conversation_anchors_execution,priority:1,where:execution_ref <> 0"`
	// Message is the public message identity.
	Message indexIdentity `gorm:"primaryKey"`
	// Ref is the first reference to this message in the stream.
	Ref int64
	// Cursor is its first visible committed position, used for upscroll and history.
	Cursor int64 `gorm:"index:conversation_anchors_cursor,priority:2;index:conversation_anchors_history,priority:3"`
	// ExecutionRef is the compact execution identity for completion projection.
	ExecutionRef int64 `gorm:"index:conversation_anchors_execution,priority:2"`
	// Kind identifies the anchor's originating event.
	Kind indexKind
	// Revision is the greatest producer revision linked to this public message.
	Revision int64
	// History marks an inherited or successfully completed candidate.
	History bool `gorm:"index:conversation_anchors_history,priority:2"`
}

func (messageAnchor) TableName() string { return "conversation_messages" }

type executionLocator struct {
	// Stream scopes the stable execution identity.
	Stream int64 `gorm:"primaryKey;autoIncrement:false"`
	// Key is the authority's execution identity, stored once per execution.
	Key indexIdentity `gorm:"primaryKey"`
	// Ref is the first committed occurrence of this execution.
	Ref int64
}

func (executionLocator) TableName() string { return "conversation_executions" }

func openIndexFile(path string) (*gorm.DB, error) {
	db, err := gorm.Open(glebarez.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: true})
	if err != nil {
		return nil, err
	}
	if err := storesqlite.ProtectTransactions(db); err != nil {
		if pool, e := db.DB(); e == nil {
			_ = pool.Close()
		}
		return nil, err
	}
	if err := migrateIndex(db); err != nil {
		if pool, e := db.DB(); e == nil {
			_ = pool.Close()
		}
		return nil, err
	}
	return db, nil
}

func migrateIndex(db *gorm.DB) error {
	if err := db.AutoMigrate(&streamCoverage{}, &messageAnchor{}); err != nil {
		return err
	}
	// Composite position and digest keys are the table keys, avoiding a second rowid tree.
	return db.Set("gorm:table_options", "WITHOUT ROWID").AutoMigrate(&indexBatch{}, &rowIndex{}, &executionLocator{}, &nativeIdentityLocator{}, &nativeMessageLocator{}, &displayPosition{})
}

func (i *indexDB) open() error {
	if i.db != nil {
		path := i.path
		if i.temporary != "" {
			path = filepath.Join(i.temporary, indexFilename)
		}
		if current, err := os.Lstat(path); err == nil && i.identity != nil && os.SameFile(i.identity, current) {
			return nil
		}
		i.reset()
	}
	if err := os.MkdirAll(filepath.Dir(i.path), 0700); err == nil {
		info, statErr := os.Lstat(i.path)
		if errors.Is(statErr, os.ErrNotExist) {
			if file, err := os.OpenFile(i.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); err == nil {
				_ = file.Close()
			}
			info, statErr = os.Lstat(i.path)
		}
		if statErr == nil && info.Mode().IsRegular() {
			if db, err := openIndexFile(i.path); err == nil {
				i.db = db
				i.identity, _ = os.Stat(i.path)
				return nil
			}
			// This path has only derived data. Never remove an unexpected directory or symlink.
			_ = os.Remove(i.path)
			if file, err := os.OpenFile(i.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); err == nil {
				_ = file.Close()
				if db, err := openIndexFile(i.path); err == nil {
					i.db = db
					i.identity, _ = os.Stat(i.path)
					return nil
				}
			}
		}
	}
	dir, err := os.MkdirTemp("", "neux-conversation-index-")
	if err != nil {
		return status_error.ChatLogIndexCorrupted
	}
	db, err := openIndexFile(filepath.Join(dir, indexFilename))
	if err != nil {
		_ = os.RemoveAll(dir)
		return status_error.ChatLogIndexCorrupted
	}
	i.db, i.temporary = db, dir
	i.identity, _ = os.Stat(filepath.Join(dir, indexFilename))
	return nil
}

func (i *indexDB) reset() {
	i.epoch++
	i.identity = nil
	if i.db != nil {
		if pool, err := i.db.DB(); err == nil {
			_ = pool.Close()
		}
		i.db = nil
	}
	if i.temporary != "" {
		_ = os.RemoveAll(i.temporary)
		i.temporary = ""
	}
}

func (i *indexDB) close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.reset()
}

func (i *indexDB) with(ctx context.Context, fn func(*gorm.DB) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.open(); err != nil {
		return err
	}
	if err := fn(i.db.WithContext(ctx)); err != nil {
		var driverErr interface{ Code() int }
		if errors.As(err, &driverErr) {
			switch driverErr.Code() & 0xff {
			case 10, 11, 26: // SQLITE_IOERR, SQLITE_CORRUPT, SQLITE_NOTADB.
				i.reset()
			}
		}
		if c := contextError(ctx); c != nil {
			return c
		}
		return status_error.ChatLogIndexCorrupted
	}
	return nil
}

// streamIndex is a versioned locator for one stream. It holds no per-row cache.
type streamIndex struct {
	// db is the shared derived database owner.
	db *indexDB
	// ctx limits index work to the owning operation.
	ctx context.Context
	// coverage is the committed prefix made available to readers.
	coverage streamCoverage
	// epoch ties the compact stream identity to its database generation.
	epoch uint64
	// located says coverage has been checked against this database.
	located bool
}

func (i *streamIndex) version() int64 { return i.coverage.Version }
func (i *streamIndex) size() int64    { return i.coverage.Size }

func (i *streamIndex) query(fn func(*gorm.DB) error) error {
	return i.db.with(i.ctx, func(db *gorm.DB) error {
		if i.located && i.epoch != i.db.epoch {
			return status_error.ChatLogIndexCorrupted
		}
		return fn(db)
	})
}

func (i *streamIndex) batch(path, key string) (batchHeader, bool, error) {
	var value indexBatch
	if i.version() == 0 {
		return batchHeader{}, false, nil
	}
	hash := sha256.Sum256([]byte(key))
	err := i.query(func(db *gorm.DB) error {
		return db.Where("stream = ? AND digest = ?", i.coverage.Stream, hash[:]).Limit(1).Find(&value).Error
	})
	if err != nil || len(value.Digest) == 0 {
		return batchHeader{}, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		return batchHeader{}, false, status_error.ChatLogError
	}
	defer file.Close()
	raw, err := readRaw(file, value.Where)
	if err != nil {
		return batchHeader{}, false, err
	}
	h, err := decodeBatchPrefix(raw)
	if err != nil || h.Key != key || h.Receipt.Version > i.version() {
		return batchHeader{}, false, status_error.ChatLogCorrupted
	}
	return h, true, nil
}

func (i *streamIndex) lookup(where string, args ...any) (rowIndex, error) {
	var value rowIndex
	if i.version() == 0 {
		return value, nil
	}
	err := i.query(func(db *gorm.DB) error {
		return db.Where("stream = ?", i.coverage.Stream).Where(where, args...).Order("cursor DESC").Limit(1).Find(&value).Error
	})
	return value, err
}

func (i *streamIndex) between(first, last int64) ([]rowIndex, error) {
	var rows []rowIndex
	err := i.query(func(db *gorm.DB) error {
		return db.Where("stream = ? AND cursor BETWEEN ? AND ?", i.coverage.Stream, first, last).
			Order("cursor").Find(&rows).Error
	})
	return rows, err
}

func (i *streamIndex) positions(cursors []int64) (map[int64]rowIndex, error) {
	out := make(map[int64]rowIndex, len(cursors))
	if len(cursors) == 0 {
		return out, nil
	}
	err := i.query(func(db *gorm.DB) error {
		for offset := 0; offset < len(cursors); offset += indexReadBatch {
			end := min(offset+indexReadBatch, len(cursors))
			var rows []rowIndex
			if err := db.Where("stream = ? AND cursor IN ?", i.coverage.Stream, cursors[offset:end]).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				out[row.Cursor] = row
			}
		}
		return nil
	})
	return out, err
}

func (i *streamIndex) forMessages(refs []int64) (map[int64][]rowIndex, error) {
	out := make(map[int64][]rowIndex, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	err := i.query(func(db *gorm.DB) error {
		const batch = 64
		for offset := 0; offset < len(refs); offset += batch {
			end := min(offset+batch, len(refs))
			var sql strings.Builder
			args := make([]any, 0, (end-offset)*2)
			for j, ref := range refs[offset:end] {
				if j > 0 {
					sql.WriteString(" UNION ALL ")
				}
				sql.WriteString("SELECT * FROM conversation_positions INDEXED BY conversation_row_message WHERE stream = ? AND message_ref <> 0 AND NOT display_link AND message_ref = ?")
				args = append(args, i.coverage.Stream, ref)
			}
			sql.WriteString(" ORDER BY message_ref,cursor")
			var rows []rowIndex
			if err := db.Raw(sql.String(), args...).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				out[row.MessageRef] = append(out[row.MessageRef], row)
			}
		}
		return nil
	})
	return out, err
}

func (i *streamIndex) forExecution(execution string) ([]rowIndex, error) {
	var out []rowIndex
	err := i.query(func(db *gorm.DB) error {
		var locator executionLocator
		if err := db.Where("stream = ? AND key = ?", i.coverage.Stream, indexIdentity(execution)).Limit(1).Find(&locator).Error; err != nil {
			return err
		}
		if locator.Ref == 0 {
			return nil
		}
		return db.Table("conversation_positions INDEXED BY conversation_row_execution").
			Where("stream = ? AND execution_ref <> 0 AND execution_ref = ?", i.coverage.Stream, locator.Ref).
			Order("cursor").Find(&out).Error
	})
	return out, err
}

func (i *streamIndex) anchor(message string) (messageAnchor, error) {
	var value messageAnchor
	err := i.query(func(db *gorm.DB) error {
		return db.Where("stream = ? AND message = ?", i.coverage.Stream, indexIdentity(message)).Limit(1).Find(&value).Error
	})
	return value, err
}

func (i *streamIndex) anchors(before int64, limit int, history bool) ([]messageAnchor, error) {
	var result []messageAnchor
	err := i.query(func(db *gorm.DB) error {
		query := db.Where("stream = ? AND cursor > 0 AND cursor < ?", i.coverage.Stream, before)
		if history {
			query = query.Where("history = ?", true)
		}
		return query.Order("cursor DESC").Limit(limit).Find(&result).Error
	})
	return result, err
}

func (i *streamIndex) nativeText(key []byte, message string, includeCommentary bool) ([]rowIndex, error) {
	var rows []rowIndex
	err := i.query(func(db *gorm.DB) error {
		identity, lane, err := findNativeRefs(db, i.coverage.Stream, key, message)
		if err != nil || lane == 0 {
			return err
		}
		source := func() *gorm.DB {
			return db.Table("conversation_positions INDEXED BY conversation_row_native").
				Where("stream = ? AND native_ref <> 0 AND native_ref = ? AND lane_ref = ?", i.coverage.Stream, identity, lane)
		}
		var last rowIndex
		if err := source().Where("kind = ?", indexNativeTextReplace).Order("cursor DESC").Limit(1).Find(&last).Error; err != nil {
			return err
		}
		return source().Where("kind IN ? AND content IN ? AND cursor >= ?",
			[]indexKind{indexNativeTextAppend, indexNativeTextReplace}, nativeTextContents(includeCommentary), last.Cursor).
			Order("cursor").Find(&rows).Error
	})
	return rows, err
}
func clearIndexStream(db *gorm.DB, stream int64) error {
	for _, table := range []any{&streamCoverage{}, &rowIndex{}, &indexBatch{}, &messageAnchor{}, &executionLocator{}, &nativeIdentityLocator{}, &nativeMessageLocator{}, &displayPosition{}} {
		if err := db.Where("stream = ?", stream).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}

func (i *indexDB) delete(ctx context.Context, path string) {
	_ = i.with(ctx, func(db *gorm.DB) error {
		var value streamCoverage
		if err := db.Where("path = ?", path).Limit(1).Find(&value).Error; err != nil {
			return err
		}
		if value.Stream == 0 {
			return nil
		}
		return db.Transaction(func(tx *gorm.DB) error { return clearIndexStream(tx, value.Stream) })
	})
}

func indexRows(db *gorm.DB, stream int64, entries []rowIndex, headers []batchHeader) error {
	if len(entries) > 0 {
		if err := internNativeIdentities(db, stream, entries); err != nil {
			return err
		}
		if err := internIndexIdentities(db, stream, entries); err != nil {
			return err
		}
		if err := indexConsumptionExecutions(db, stream, entries); err != nil {
			return err
		}
		if err := db.CreateInBatches(entries, indexWriteBatch).Error; err != nil {
			return err
		}
		if err := indexDisplay(db, stream, entries); err != nil {
			return err
		}
	}
	if len(headers) > 0 {
		values := make([]indexBatch, 0, len(headers))
		for _, h := range headers {
			digest := sha256.Sum256([]byte(h.Key))
			values = append(values, indexBatch{Stream: stream, Digest: digest[:], Where: h.Where})
		}
		if err := db.CreateInBatches(values, indexWriteBatch).Error; err != nil {
			return err
		}
	}
	return nil
}

func internIndexIdentities(db *gorm.DB, stream int64, rows []rowIndex) error {
	executions := make(map[string]executionLocator)
	for j := range rows {
		row := &rows[j]
		row.Stream = stream
		if row.Execution != "" && executions[row.Execution].Ref == 0 {
			executions[row.Execution] = executionLocator{Stream: stream, Key: indexIdentity(row.Execution), Ref: row.Cursor}
		}
	}
	if err := resolveExecutionRefs(db, stream, executions); err != nil {
		return err
	}
	messages := make(map[string]messageAnchor)
	var complete []int64
	for j := range rows {
		row := &rows[j]
		row.ExecutionRef = executions[row.Execution].Ref
		if row.Kind == indexExecutionCompleted && row.ExecutionRef > 0 {
			complete = append(complete, row.ExecutionRef)
		}
	}
	if err := assignTerminalMessages(db, stream, rows); err != nil {
		return err
	}
	for j := range rows {
		row := &rows[j]
		if row.Message == "" || len(row.Native) > 0 {
			continue
		}
		anchor := messages[row.Message]
		if anchor.Ref == 0 {
			anchor = messageAnchor{Stream: stream, Message: indexIdentity(row.Message), Ref: row.Cursor}
		}
		if anchor.Cursor == 0 {
			switch row.Kind {
			case indexUserMessage, indexAssistantMessage, indexTextReplace, indexInherited, indexExecutionCreated:
				anchor.Cursor, anchor.Kind, anchor.ExecutionRef = row.Cursor, row.Kind, row.ExecutionRef
				anchor.History = row.Kind == indexInherited
			}
		}
		messages[row.Message] = anchor
	}
	if err := resolveMessageRefs(db, stream, messages); err != nil {
		return err
	}
	for j := range rows {
		row := &rows[j]
		if len(row.Native) == 0 && row.Message != "" {
			row.MessageRef = messages[row.Message].Ref
			row.Message = ""
		}
	}
	if len(complete) > 0 {
		return db.Model(&messageAnchor{}).Where("stream = ? AND execution_ref <> 0 AND execution_ref IN ? AND kind = ?", stream, complete, indexExecutionCreated).
			Update("history", true).Error
	}
	return nil
}

func assignTerminalMessages(db *gorm.DB, stream int64, rows []rowIndex) error {
	known := make(map[int64]string)
	for _, row := range rows {
		if row.Kind == indexExecutionCreated && row.ExecutionRef > 0 {
			known[row.ExecutionRef] = row.Message
		}
	}
	var missing []int64
	for _, row := range rows {
		if (row.Kind == indexExecutionCompleted || row.Kind == indexExecutionEnded) &&
			row.Message == "" && row.ExecutionRef > 0 && known[row.ExecutionRef] == "" {
			missing = append(missing, row.ExecutionRef)
		}
	}
	if len(missing) > 0 {
		var anchors []messageAnchor
		if err := db.Where("stream = ? AND execution_ref <> 0 AND execution_ref IN ? AND kind = ?", stream, missing, indexExecutionCreated).
			Find(&anchors).Error; err != nil {
			return err
		}
		for _, anchor := range anchors {
			known[anchor.ExecutionRef] = string(anchor.Message)
		}
	}
	for j, row := range rows {
		if (row.Kind == indexExecutionCompleted || row.Kind == indexExecutionEnded) && row.Message == "" {
			rows[j].Message = known[row.ExecutionRef]
		}
	}
	return nil
}

func resolveExecutionRefs(db *gorm.DB, stream int64, known map[string]executionLocator) error {
	if len(known) == 0 {
		return nil
	}
	values := make([]executionLocator, 0, len(known))
	keys := make([]indexIdentity, 0, len(known))
	for key, value := range known {
		keys = append(keys, indexIdentity(key))
		values = append(values, value)
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(values, indexWriteBatch).Error; err != nil {
		return err
	}
	if err := db.Where("stream = ? AND key IN ?", stream, keys).Find(&values).Error; err != nil {
		return err
	}
	for _, value := range values {
		known[string(value.Key)] = value
	}
	return nil
}

func resolveMessageRefs(db *gorm.DB, stream int64, known map[string]messageAnchor) error {
	if len(known) == 0 {
		return nil
	}
	values := make([]messageAnchor, 0, len(known))
	keys := make([]indexIdentity, 0, len(known))
	for key, value := range known {
		keys = append(keys, indexIdentity(key))
		values = append(values, value)
	}
	conflict := clause.OnConflict{
		Columns:   []clause.Column{{Name: "stream"}, {Name: "message"}},
		DoUpdates: clause.AssignmentColumns([]string{"cursor", "kind", "execution_ref", "history"}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "conversation_messages.cursor = 0 AND excluded.cursor > 0"}}},
	}
	if err := db.Clauses(conflict).CreateInBatches(values, indexWriteBatch).Error; err != nil {
		return err
	}
	if err := db.Where("stream = ? AND message IN ?", stream, keys).Find(&values).Error; err != nil {
		return err
	}
	for _, value := range values {
		known[string(value.Message)] = value
	}
	return nil
}

func indexConsumptionExecutions(db *gorm.DB, stream int64, rows []rowIndex) error {
	var hosts []string
	known := make(map[string]int64)
	for _, row := range rows {
		if row.Kind == indexInput {
			known[row.Host] = row.ExecutionRef
		}
		if row.Kind == indexConsumption {
			hosts = append(hosts, row.Host)
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	var admitted []rowIndex
	if err := db.Where("stream = ? AND host <> '' AND host IN ? AND kind = ?", stream, hosts, indexInput).Find(&admitted).Error; err != nil {
		return err
	}
	for _, row := range admitted {
		known[row.Host] = row.ExecutionRef
	}
	for j, row := range rows {
		if row.Kind == indexConsumption {
			rows[j].ExecutionRef = known[row.Host]
		}
	}
	return nil
}

func (i *streamIndex) publish(info os.FileInfo, rows []rowIndex, header batchHeader) error {
	return i.query(func(db *gorm.DB) error {
		return db.Transaction(func(tx *gorm.DB) error {
			coverage := i.coverage
			if coverage.Stream == 0 {
				var stale streamCoverage
				if err := tx.Where("path = ?", coverage.Path).Limit(1).Find(&stale).Error; err != nil {
					return err
				}
				if stale.Stream != 0 {
					if err := clearIndexStream(tx, stale.Stream); err != nil {
						return err
					}
				}
				if err := tx.Create(&coverage).Error; err != nil {
					return err
				}
			}
			if err := indexRows(tx, coverage.Stream, rows, []batchHeader{header}); err != nil {
				return err
			}
			for _, row := range rows {
				if row.Kind == indexBinding {
					coverage.Binding = row.Cursor
				}
			}
			return tx.Model(&streamCoverage{}).Where("stream = ?", coverage.Stream).Updates(map[string]any{
				"size": info.Size(), "stamp": info.ModTime().UnixNano(), "version": header.Receipt.Version, "binding": coverage.Binding,
			}).Error
		})
	})
}

func (i *streamIndex) loadCoverage() error {
	path := i.coverage.Path
	load := func(db *gorm.DB) error {
		i.coverage = streamCoverage{Path: path}
		if err := db.Where("path = ?", path).Limit(1).Find(&i.coverage).Error; err != nil {
			return err
		}
		i.located, i.epoch = true, i.db.epoch
		return nil
	}
	err := i.db.with(i.ctx, load)
	if errors.Is(err, status_error.ChatLogIndexCorrupted) {
		i.db.mu.Lock()
		i.db.reset()
		i.db.mu.Unlock()
		err = i.db.with(i.ctx, load)
	}
	return err
}

func (i *streamIndex) withPatchBases(rows []rowIndex) (map[int64]rowIndex, error) {
	out := make(map[int64]rowIndex, len(rows))
	for _, row := range rows {
		out[row.Cursor] = row
	}
	for depth := 0; depth <= maximumToolPatchDepth; depth++ {
		missing := make([]int64, 0)
		seen := make(map[int64]bool)
		for _, row := range rows {
			if base := row.PatchBase; base > 0 && out[base].Cursor == 0 && !seen[base] {
				if base >= row.Cursor {
					return nil, status_error.ChatLogCorrupted
				}
				seen[base] = true
				missing = append(missing, base)
			}
		}
		if len(missing) == 0 {
			return out, nil
		}
		found, err := i.positions(missing)
		if err != nil {
			return nil, err
		}
		if len(found) != len(missing) {
			return nil, status_error.ChatLogCorrupted
		}
		rows = rows[:0]
		for cursor, row := range found {
			out[cursor] = row
			rows = append(rows, row)
		}
	}
	return nil, status_error.ChatLogCorrupted
}

func (i *streamIndex) hostRows(host string) ([]rowIndex, error) {
	var rows []rowIndex
	err := i.query(func(db *gorm.DB) error {
		return db.Where("stream = ? AND host <> '' AND host = ? AND kind IN ?", i.coverage.Stream, host,
			[]indexKind{indexInput, indexConsumption}).Find(&rows).Error
	})
	return rows, err
}

type nativeBodyLookup struct {
	// identity is the canonical invocation locator.
	identity int64
	// lane is the canonical native message locator.
	lane int64
	// body is the durable range from the terminal.
	body nativeBody
}

func (i *streamIndex) bodyPositions(wanted []nativeBodyLookup) (map[int64][]rowIndex, error) {
	out := make(map[int64][]rowIndex, len(wanted))
	if len(wanted) == 0 {
		return out, nil
	}
	err := i.query(func(db *gorm.DB) error {
		const batch = 48
		for start := 0; start < len(wanted); start += batch {
			end := min(start+batch, len(wanted))
			var sql strings.Builder
			args := make([]any, 0, (end-start)*10)
			for j, e := range wanted[start:end] {
				if j > 0 {
					sql.WriteString(" UNION ALL ")
				}
				sql.WriteString("SELECT * FROM conversation_positions INDEXED BY conversation_row_native WHERE stream = ? AND native_ref <> 0 AND native_ref = ? AND lane_ref = ? AND kind IN (?,?) AND content IN (?,?,?) AND cursor BETWEEN ? AND ?")
				contents := nativeTextContents(e.body.IncludeCommentary)
				args = append(args, i.coverage.Stream, e.identity, e.lane, indexNativeTextAppend, indexNativeTextReplace,
					contents[0], contents[1], contents[2], e.body.First, e.body.Last)
			}
			sql.WriteString(" ORDER BY cursor")
			var rows []rowIndex
			if err := db.Raw(sql.String(), args...).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				out[row.LaneRef] = append(out[row.LaneRef], row)
			}
		}
		return nil
	})
	return out, err
}

// reverseState snapshots the coverage generation and any commits whose projection failed.
func (i *indexDB) reverseState() (uint64, []string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	dirty := make([]string, 0, len(i.dirty))
	for path := range i.dirty {
		dirty = append(dirty, path)
	}
	return i.epoch, dirty
}

func (i *indexDB) markDirty(path string, dirty bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !dirty {
		delete(i.dirty, path)
		return
	}
	if i.dirty == nil {
		i.dirty = make(map[string]struct{})
	}
	i.dirty[path] = struct{}{}
}

func (i *indexDB) adapterPositions(ctx context.Context, keys [][]byte) ([]rowIndex, map[int64]string, error) {
	var rows []rowIndex
	paths := make(map[int64]string)
	err := i.with(ctx, func(db *gorm.DB) error {
		for start := 0; start < len(keys); start += indexReadBatch {
			end := min(start+indexReadBatch, len(keys))
			var selected []rowIndex
			if err := db.Where("adapter IS NOT NULL AND adapter IN ?", keys[start:end]).Find(&selected).Error; err != nil {
				return err
			}
			rows = append(rows, selected...)
		}
		streams := make([]int64, 0, len(rows))
		seen := make(map[int64]bool)
		for _, row := range rows {
			if !seen[row.Stream] {
				seen[row.Stream] = true
				streams = append(streams, row.Stream)
			}
		}
		if len(streams) == 0 {
			return nil
		}
		for start := 0; start < len(streams); start += indexReadBatch {
			end := min(start+indexReadBatch, len(streams))
			var coverage []streamCoverage
			if err := db.Select("stream", "path").Where("stream IN ?", streams[start:end]).Find(&coverage).Error; err != nil {
				return err
			}
			for _, c := range coverage {
				paths[c.Stream] = c.Path
			}
		}
		return nil
	})
	return rows, paths, err
}

func (i *streamIndex) bindingPosition() (rowIndex, error) {
	if i.coverage.Binding == 0 {
		return rowIndex{}, nil
	}
	return i.lookup("cursor = ?", i.coverage.Binding)
}

func (i *streamIndex) nativePosition(key []byte, kind indexKind, message, tool string) (rowIndex, error) {
	var out rowIndex
	if i.version() == 0 {
		return out, nil
	}
	err := i.query(func(db *gorm.DB) error {
		identity, lane, err := findNativeRefs(db, i.coverage.Stream, key, message)
		if err != nil || identity == 0 || (message != "" && lane == 0) {
			return err
		}
		query := db.Table("conversation_positions INDEXED BY conversation_row_native").
			Where("stream = ? AND native_ref <> 0 AND native_ref = ? AND kind = ?", i.coverage.Stream, identity, kind)
		if message != "" {
			query = query.Where("lane_ref = ?", lane)
		}
		if tool != "" {
			query = query.Where("tool = ?", tool)
		}
		return query.Order("cursor DESC").Limit(1).Find(&out).Error
	})
	return out, err
}

type cursorSpan struct {
	// first is the inclusive beginning of a selected legacy decoding dependency.
	first int64
	// last is the inclusive end of that dependency.
	last int64
}

func (i *streamIndex) legacyPositions(spans []cursorSpan) ([]rowIndex, error) {
	if len(spans) == 0 {
		return nil, nil
	}
	var out []rowIndex
	err := i.query(func(db *gorm.DB) error {
		const batch = 64
		for start := 0; start < len(spans); start += batch {
			end := min(start+batch, len(spans))
			var sql strings.Builder
			args := make([]any, 0, (end-start)*3)
			for j, span := range spans[start:end] {
				if j > 0 {
					sql.WriteString(" UNION ALL ")
				}
				sql.WriteString("SELECT * FROM conversation_positions WHERE stream = ? AND cursor BETWEEN ? AND ?")
				args = append(args, i.coverage.Stream, span.first, span.last)
			}
			var rows []rowIndex
			if err := db.Raw(sql.String(), args...).Scan(&rows).Error; err != nil {
				return err
			}
			out = append(out, rows...)
		}
		return nil
	})
	return out, err
}
