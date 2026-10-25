package memory

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"io"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"

	// ncruces/go-sqlite3 是 CGO-free 的 SQLite 驱动，仅 Memory 模块使用；
	// luist18/sqlite-vec-go 为每个 ncruces 连接自动注册 sqlite-vec 扩展。
	_ "github.com/luist18/sqlite-vec-go"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// 记忆存储的 schema 版本与元数据键，随结构演进递增。
const (
	// storeSchemaVersion 是当前 memory.db 的结构版本，写入 memory_meta。
	storeSchemaVersion = "2"
	// metaKeySchemaVersion 保存 memory.db 结构版本。
	metaKeySchemaVersion = "schema_version"
	// metaKeyEmbeddingModel 保存已建语义索引使用的 Embedding 模型名指纹。
	metaKeyEmbeddingModel = "embedding_model"
	// metaKeyEmbeddingDimensions 保存已建语义索引的向量维度指纹。
	metaKeyEmbeddingDimensions = "embedding_dimensions"
	// metaKeyEmbeddingIndexBaseURL 保存已建语义索引使用的服务地址指纹。
	metaKeyEmbeddingIndexBaseURL = "embedding_index_base_url"
	// metaKeyFTSAvailable 保存 FTS5 关键词索引是否成功建立。
	metaKeyFTSAvailable = "fts_available"
	// metaKeyEmbeddingBaseURL 保存 OpenAI 兼容嵌入服务的基础地址。
	metaKeyEmbeddingBaseURL = "embedding_base_url"
	// metaKeyEmbeddingAPIKey 保存加密后的嵌入服务凭据。
	metaKeyEmbeddingAPIKey = "embedding_api_key"
	// metaKeySettingsKey 保存用于加密嵌入凭据的随机密钥。
	metaKeySettingsKey = "settings_key"
)

// HistoryEntry 是一次记忆内容变更及其前后值。
type HistoryEntry struct {
	// ID 是历史记录唯一标识，32 位无连字符 UUID。
	ID string
	// MemoryID 是变更发生的记忆标识。
	MemoryID string
	// Scope 是事件发生时的记忆范围，即使当前记忆已被遗忘也会保留。
	Scope constants.MemoryScope
	// ScopeID 是事件所属伙伴或项目标识。
	ScopeID string
	// OldMemory 是变更前正文；ADD 事件为空。
	OldMemory string
	// NewMemory 是变更后正文；DELETE 事件为空。
	NewMemory string
	// Event 是变更事件类型：ADD / UPDATE / DELETE。
	Event constants.MemoryEvent
	// CreatedAt 是记忆创建时间的 Unix 毫秒值。
	CreatedAt int64
	// UpdatedAt 是本次变更时间的 Unix 毫秒值。
	UpdatedAt int64
	// IsDeleted 表示本次变更是否删除了当前记忆。
	IsDeleted bool
	// SourceRunID 是产生本次变更的执行标识。
	SourceRunID string
}

// store 是 Memory 模块独立 SQLite 数据库的存储实现。
//
// memories、sqlite-vec 索引、FTS 关键词索引和 history
// 统一放入 memory.db，写入、更新、删除处于同一事务。
type store struct {
	// db 是 ncruces 驱动的数据库连接池，写操作经单连接串行化。
	db *sql.DB
	// ftsAvailable 表示当前 SQLite 构建是否支持 FTS5 trigram 关键词索引。
	ftsAvailable bool
}

// openStore 打开或创建 memory.db 并执行结构迁移。
//
// 连接池限制为单连接并设置 busy_timeout，保证并发记忆命令下写入串行；
// 与主业务库默认模式的差异由 ADR 说明。
func openStore(path string) (*store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	db.SetMaxOpenConns(1)
	store := &store{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Close 关闭数据库连接。
func (s *store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// migrate 建立 memories、memory_history、memory_meta 与 FTS5 关键词索引。
// sqlite-vec 向量表的维度依赖首次 Embedding 配置，由 vecstore 延迟创建。
func (s *store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS memory_command_receipts (id TEXT PRIMARY KEY, request_key TEXT NOT NULL UNIQUE, request_hash TEXT NOT NULL, memory_id TEXT NOT NULL, duplicate INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS memories (
			seq           INTEGER PRIMARY KEY AUTOINCREMENT,
			id            TEXT NOT NULL UNIQUE,
			scope         TEXT NOT NULL,
			scope_id      TEXT NOT NULL,
			data          TEXT NOT NULL,
			hash          TEXT NOT NULL,
			source_run_id TEXT NOT NULL DEFAULT '',
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope, scope_id)`,
		`CREATE TABLE IF NOT EXISTS memory_history (
			id            TEXT PRIMARY KEY,
			memory_id     TEXT NOT NULL,
			scope         TEXT NOT NULL DEFAULT '',
			scope_id      TEXT NOT NULL DEFAULT '',
			old_memory    TEXT,
			new_memory    TEXT,
			event         TEXT NOT NULL,
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL,
			is_deleted    INTEGER NOT NULL DEFAULT 0,
			source_run_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_history_memory ON memory_history(memory_id)`,
		`CREATE TABLE IF NOT EXISTS memory_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return status_error.DatabaseError
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_memory_history_scope ON memory_history(scope, scope_id, updated_at DESC)`); err != nil {
		return status_error.DatabaseError
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_memories_scope_hash_unique ON memories(scope, scope_id, hash)`); err != nil {
		return status_error.DatabaseError
	}
	// FTS5 是关键词召回的前置能力；构建缺失时降级为 LIKE 扫描，不影响主流程。
	_, err := s.db.Exec(
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
			memory_id UNINDEXED, data, tokenize='trigram'
		)`,
	)
	s.ftsAvailable = err == nil
	if err := s.setMetaValue(metaKeySchemaVersion, storeSchemaVersion); err != nil {
		return err
	}
	return s.setMetaValue(metaKeyFTSAvailable, fmt.Sprint(s.ftsAvailable))
}

// withTx 在单一事务中执行记忆写入步骤，任何一步失败整体回滚，不留半成品。
func (s *store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return status_error.DatabaseError
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := insertAgentReceipt(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// scanMemory 从行扫描一条记忆。
func scanMemory(row interface{ Scan(...interface{}) error }) (*Memory, error) {
	memory := &Memory{}
	err := row.Scan(
		&memory.Seq,
		&memory.ID,
		&memory.Scope,
		&memory.ScopeID,
		&memory.Data,
		&memory.Hash,
		&memory.SourceRunID,
		&memory.CreatedAt,
		&memory.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return memory, nil
}

// memoryColumns 是 memories 表的完整查询列。
const memoryColumns = "seq, id, scope, scope_id, data, hash, source_run_id, created_at, updated_at"

// memoryColumnsAliased 是 memories 表使用 m 别名时的完整查询列。
const memoryColumnsAliased = "m.seq, m.id, m.scope, m.scope_id, m.data, m.hash, m.source_run_id, m.created_at, m.updated_at"

// scanMemories 扫描多行记忆并保证行集关闭。
func scanMemories(rows *sql.Rows) ([]Memory, error) {
	defer rows.Close()
	memories := []Memory{}
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, *memory)
	}
	return memories, rows.Err()
}

// insertMemory 在事务内插入一条记忆并回填自增序号。
func (s *store) insertMemory(tx *sql.Tx, memory *Memory) error {
	result, err := tx.Exec(
		`INSERT INTO memories (id, scope, scope_id, data, hash, source_run_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		memory.ID,
		string(memory.Scope),
		memory.ScopeID,
		memory.Data,
		memory.Hash,
		memory.SourceRunID,
		memory.CreatedAt,
		memory.UpdatedAt,
	)
	if err != nil {
		return status_error.DatabaseError
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return status_error.DatabaseError
	}
	memory.Seq = seq
	return nil
}

// updateMemory 在事务内更新记忆正文、哈希与更新时间，范围标识不可变。
func (s *store) updateMemory(tx *sql.Tx, memory Memory) error {
	_, err := tx.Exec(
		`UPDATE memories SET data = ?, hash = ?, source_run_id = ?, updated_at = ? WHERE id = ?`,
		memory.Data,
		memory.Hash,
		memory.SourceRunID,
		memory.UpdatedAt,
		memory.ID,
	)
	if err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteMemory 在事务内物理删除一条记忆。
func (s *store) deleteMemory(tx *sql.Tx, memoryID string) error {
	if _, err := tx.Exec(`DELETE FROM memories WHERE id = ?`, memoryID); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// getMemory 按标识查询一条记忆。
func (s *store) getMemory(id string) (*Memory, bool, error) {
	row := s.db.QueryRow(
		`SELECT `+memoryColumns+` FROM memories WHERE id = ?`,
		id,
	)
	memory, err := scanMemory(row)
	if err == nil {
		return memory, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return nil, false, status_error.DatabaseError
}

// findByHash 在同一范围内按正文哈希查找已有记忆，支撑 exact dedup。
func (s *store) findByHash(
	scope constants.MemoryScope,
	scopeID string,
	hash string,
) (*Memory, bool, error) {
	row := s.db.QueryRow(
		`SELECT `+memoryColumns+` FROM memories WHERE scope = ? AND scope_id = ? AND hash = ?`,
		string(scope),
		scopeID,
		hash,
	)
	memory, err := scanMemory(row)
	if err == nil {
		return memory, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return nil, false, status_error.DatabaseError
}

// listMemories 返回全部记忆，供语义索引重建使用。
func (s *store) listMemories() ([]Memory, error) {
	rows, err := s.db.Query(`SELECT ` + memoryColumns + ` FROM memories ORDER BY seq ASC`)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	memories, err := scanMemories(rows)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	return memories, nil
}

// insertHistory 在事务内追加一条变更历史。
func (s *store) insertHistory(tx *sql.Tx, entry HistoryEntry) error {
	var oldMemory, newMemory interface{}
	if entry.OldMemory != "" {
		oldMemory = entry.OldMemory
	}
	if entry.NewMemory != "" {
		newMemory = entry.NewMemory
	}
	_, err := tx.Exec(
		`INSERT INTO memory_history
		 (id, memory_id, scope, scope_id, old_memory, new_memory, event, created_at, updated_at, is_deleted, source_run_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		entry.MemoryID,
		string(entry.Scope),
		entry.ScopeID,
		oldMemory,
		newMemory,
		string(entry.Event),
		entry.CreatedAt,
		entry.UpdatedAt,
		entry.IsDeleted,
		entry.SourceRunID,
	)
	if err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// listHistory 返回一条记忆的全部变更历史，按时间升序。
func (s *store) listHistory(memoryID string) ([]HistoryEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, memory_id, scope, scope_id, old_memory, new_memory, event, created_at, updated_at, is_deleted, source_run_id
		 FROM memory_history WHERE memory_id = ? ORDER BY rowid ASC`,
		memoryID,
	)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	defer rows.Close()
	entries := []HistoryEntry{}
	for rows.Next() {
		entry := HistoryEntry{}
		var oldMemory, newMemory sql.NullString
		if err := rows.Scan(
			&entry.ID,
			&entry.MemoryID,
			&entry.Scope,
			&entry.ScopeID,
			&oldMemory,
			&newMemory,
			&entry.Event,
			&entry.CreatedAt,
			&entry.UpdatedAt,
			&entry.IsDeleted,
			&entry.SourceRunID,
		); err != nil {
			return nil, status_error.DatabaseError
		}
		entry.OldMemory = oldMemory.String
		entry.NewMemory = newMemory.String
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, status_error.DatabaseError
	}
	return entries, nil
}

// memoryCount 返回当前记忆总条数。
func (s *store) memoryCount() (int64, error) {
	var count int64
	if err := s.db.QueryRow(`SELECT count(*) FROM memories`).Scan(&count); err != nil {
		return 0, status_error.DatabaseError
	}
	return count, nil
}

// getMeta 读取一条元数据。
func (s *store) getMeta(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM memory_meta WHERE key = ?`, key).Scan(&value)
	if err == nil {
		return value, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return "", false, status_error.DatabaseError
}

// setMeta 在事务内写入一条元数据。
func (s *store) setMeta(tx *sql.Tx, key string, value string) error {
	if _, err := tx.Exec(
		`INSERT INTO memory_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		value,
	); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteMeta 在事务内删除一条元数据。
func (s *store) deleteMeta(tx *sql.Tx, key string) error {
	if _, err := tx.Exec(`DELETE FROM memory_meta WHERE key = ?`, key); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// setMetaValue 在事务外写入一条元数据。
func (s *store) setMetaValue(key string, value string) error {
	if _, err := s.db.Exec(
		`INSERT INTO memory_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		value,
	); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// upsertFTS 在事务内同步一条记忆的关键词索引；FTS5 不可用时跳过。
func (s *store) upsertFTS(tx *sql.Tx, memoryID string, data string) error {
	if !s.ftsAvailable {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM memory_fts WHERE memory_id = ?`, memoryID); err != nil {
		return status_error.DatabaseError
	}
	if _, err := tx.Exec(
		`INSERT INTO memory_fts (memory_id, data) VALUES (?, ?)`,
		memoryID,
		data,
	); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteFTS 在事务内删除一条记忆的关键词索引；FTS5 不可用时跳过。
func (s *store) deleteFTS(tx *sql.Tx, memoryID string) error {
	if !s.ftsAvailable {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM memory_fts WHERE memory_id = ?`, memoryID); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteScopeFTS 在事务内删除指定范围的全部关键词索引；FTS5 不可用时跳过。
func (s *store) deleteScopeFTS(tx *sql.Tx, scope constants.MemoryScope, scopeID string) error {
	if !s.ftsAvailable {
		return nil
	}
	if _, err := tx.Exec(
		`DELETE FROM memory_fts WHERE memory_id IN
		 (SELECT id FROM memories WHERE scope = ? AND scope_id = ?)`,
		string(scope),
		scopeID,
	); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteScopeData 在事务内删除指定范围的全部记忆与历史。
//
// 用于伙伴或项目删除时的级联清理；
// 级联删除连同 history 一起清除，因为属主对象本身已不存在，历史不再可审计。
func (s *store) deleteScopeData(tx *sql.Tx, scope constants.MemoryScope, scopeID string) error {
	if _, err := tx.Exec(
		`DELETE FROM memory_history WHERE scope = ? AND scope_id = ?`,
		string(scope),
		scopeID,
	); err != nil {
		return status_error.DatabaseError
	}
	if _, err := tx.Exec(
		`DELETE FROM memories WHERE scope = ? AND scope_id = ?`,
		string(scope),
		scopeID,
	); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// listScopeOwners 返回仍被当前记忆或时间线引用的全部范围属主。
func (s *store) listScopeOwners(ctx context.Context) ([]ScopeOwner, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT scope, scope_id FROM memories
		UNION
		SELECT scope, scope_id FROM memory_history WHERE scope <> '' AND scope_id <> ''
		ORDER BY scope, scope_id`)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	defer rows.Close()
	owners := []ScopeOwner{}
	for rows.Next() {
		owner := ScopeOwner{}
		if err := rows.Scan(&owner.Scope, &owner.ScopeID); err != nil {
			return nil, status_error.DatabaseError
		}
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, status_error.DatabaseError
	}
	return owners, nil
}

// listMemoryPage 返回指定范围的一页当前记忆。
func (s *store) listMemoryPage(ctx context.Context, filter ListFilter) (MemoryPage, error) {
	where, args := scopeCondition(filter)
	if filter.Query != "" {
		where += ` AND data LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(filter.Query)+"%")
	}
	if filter.UpdatedAtStart > 0 {
		where += ` AND updated_at >= ?`
		args = append(args, filter.UpdatedAtStart)
	}
	if filter.UpdatedAtEnd > 0 {
		where += ` AND updated_at <= ?`
		args = append(args, filter.UpdatedAtEnd)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE `+where, args...).Scan(&total); err != nil {
		return MemoryPage{}, status_error.DatabaseError
	}
	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+memoryColumns+` FROM memories WHERE `+where+` ORDER BY updated_at DESC, seq DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return MemoryPage{}, status_error.DatabaseError
	}
	items, err := scanMemories(rows)
	if err != nil {
		return MemoryPage{}, status_error.DatabaseError
	}
	return MemoryPage{Items: items, Total: total}, nil
}

// listTimelinePage 返回指定范围的一页追加式变更事件。
func (s *store) listTimelinePage(ctx context.Context, filter ListFilter) (TimelinePage, error) {
	where := `scope = ? AND scope_id = ?`
	args := []any{string(filter.Scope), filter.ScopeID}
	if filter.Query != "" {
		where += ` AND (COALESCE(old_memory, '') LIKE ? ESCAPE '\' OR COALESCE(new_memory, '') LIKE ? ESCAPE '\')`
		pattern := "%" + escapeLike(filter.Query) + "%"
		args = append(args, pattern, pattern)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM memory_history WHERE `+where, args...).Scan(&total); err != nil {
		return TimelinePage{}, status_error.DatabaseError
	}
	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, memory_id, scope, scope_id, old_memory, new_memory, event, created_at, updated_at, is_deleted, source_run_id FROM memory_history WHERE `+where+` ORDER BY updated_at DESC, rowid DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return TimelinePage{}, status_error.DatabaseError
	}
	defer rows.Close()
	items := []HistoryEntry{}
	for rows.Next() {
		entry := HistoryEntry{}
		var oldMemory, newMemory sql.NullString
		if err := rows.Scan(&entry.ID, &entry.MemoryID, &entry.Scope, &entry.ScopeID, &oldMemory, &newMemory, &entry.Event, &entry.CreatedAt, &entry.UpdatedAt, &entry.IsDeleted, &entry.SourceRunID); err != nil {
			return TimelinePage{}, status_error.DatabaseError
		}
		entry.OldMemory = oldMemory.String
		entry.NewMemory = newMemory.String
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return TimelinePage{}, status_error.DatabaseError
	}
	return TimelinePage{Items: items, Total: total}, nil
}

// scopeCondition 构造范围过滤条件；Scope 为 all 时按属主集合合并。
func scopeCondition(filter ListFilter) (string, []any) {
	if filter.Scope != constants.MemoryScopeAll {
		return `scope = ? AND scope_id = ?`, []any{string(filter.Scope), filter.ScopeID}
	}
	if len(filter.Owners) == 0 {
		return `1 = 0`, []any{}
	}
	conditions := make([]string, 0, len(filter.Owners))
	args := make([]any, 0, len(filter.Owners)*2)
	for _, owner := range filter.Owners {
		conditions = append(conditions, `(scope = ? AND scope_id = ?)`)
		args = append(args, string(owner.Scope), owner.ScopeID)
	}
	return `(` + strings.Join(conditions, ` OR `) + `)`, args
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// embeddingConfig 读取并解密嵌入配置。
func (s *store) embeddingConfig() (EmbeddingConfig, error) {
	baseURL, _, err := s.getMeta(metaKeyEmbeddingBaseURL)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	model, _, err := s.getMeta(metaKeyEmbeddingModel + "_configured")
	if err != nil {
		return EmbeddingConfig{}, err
	}
	ciphertext, exists, err := s.getMeta(metaKeyEmbeddingAPIKey)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	apiKey := ""
	if exists && ciphertext != "" {
		apiKey, err = s.decryptSetting(ciphertext)
		if err != nil {
			return EmbeddingConfig{}, err
		}
	}
	return EmbeddingConfig{BaseURL: baseURL, APIKey: apiKey, Model: model}, nil
}

// setEmbeddingConfig 原子保存嵌入配置，API Key 只写入密文。
func (s *store) setEmbeddingConfig(ctx context.Context, config EmbeddingConfig) error {
	ciphertext := ""
	var err error
	if config.APIKey != "" {
		ciphertext, err = s.encryptSetting(config.APIKey)
		if err != nil {
			return err
		}
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := s.setMeta(tx, metaKeyEmbeddingBaseURL, config.BaseURL); err != nil {
			return err
		}
		if err := s.setMeta(tx, metaKeyEmbeddingModel+"_configured", config.Model); err != nil {
			return err
		}
		if ciphertext == "" {
			return s.deleteMeta(tx, metaKeyEmbeddingAPIKey)
		}
		return s.setMeta(tx, metaKeyEmbeddingAPIKey, ciphertext)
	})
}

func (s *store) settingsAEAD() (cipher.AEAD, error) {
	encoded, exists, err := s.getMeta(metaKeySettingsKey)
	if err != nil {
		return nil, err
	}
	if !exists {
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, status_error.MemoryCredentialError
		}
		encoded = base64.RawStdEncoding.EncodeToString(key)
		if err := s.setMetaValue(metaKeySettingsKey, encoded); err != nil {
			return nil, err
		}
	}
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, status_error.MemoryCredentialError
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, status_error.MemoryCredentialError
	}
	return cipher.NewGCM(block)
}

func (s *store) encryptSetting(value string) (string, error) {
	aead, err := s.settingsAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", status_error.MemoryCredentialError
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), []byte(metaKeyEmbeddingAPIKey))
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *store) decryptSetting(value string) (string, error) {
	aead, err := s.settingsAEAD()
	if err != nil {
		return "", err
	}
	encoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(encoded) < aead.NonceSize() {
		return "", status_error.MemoryCredentialError
	}
	plaintext, err := aead.Open(nil, encoded[:aead.NonceSize()], encoded[aead.NonceSize():], []byte(metaKeyEmbeddingAPIKey))
	if err != nil {
		return "", status_error.MemoryCredentialError
	}
	return string(plaintext), nil
}
