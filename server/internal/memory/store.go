package memory

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	// ncruces/go-sqlite3 是 CGO-free 的 SQLite 驱动，仅 Memory 模块使用；
	// luist18/sqlite-vec-go 为每个 ncruces 连接自动注册 sqlite-vec 扩展。
	_ "github.com/luist18/sqlite-vec-go"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// 记忆存储的 schema 版本与元数据键，随结构演进递增。
const (
	// storeSchemaVersion 是当前 memory.db 的结构版本，写入 memory_meta。
	storeSchemaVersion = "1"
	// metaKeySchemaVersion 保存 memory.db 结构版本。
	metaKeySchemaVersion = "partner_schema_version"
	// metaKeyEmbeddingModel 保存已建语义索引使用的 Embedding 模型名指纹。
	metaKeyEmbeddingModel = "partner_embedding_model"
	// metaKeyEmbeddingDimensions 保存已建语义索引的向量维度指纹。
	metaKeyEmbeddingDimensions = "partner_embedding_dimensions"
	// metaKeyEmbeddingIndexBaseURL 保存已建语义索引使用的服务地址指纹。
	metaKeyEmbeddingIndexBaseURL = "partner_embedding_index_base_url"
	// metaKeyFTSAvailable 保存 FTS5 关键词索引是否成功建立。
	metaKeyFTSAvailable = "partner_fts_available"
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
	// PartnerID 是变更发生时的伙伴标识。
	PartnerID string
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
	// Source 是此次变更的操作来源。
	Source constants.MemoryTimelineSource
	// ReviewChatID 是自动复盘的来源会话。
	ReviewChatID string
	// ReviewAfter 是自动复盘冻结区间的前开边界。
	ReviewAfter int64
	// ReviewThrough 是自动复盘冻结区间的后闭边界。
	ReviewThrough int64
	// ReviewEvidence 是自动操作引用的用户消息标识。
	ReviewEvidence []string
}

// store 是 Memory 模块独立 SQLite 数据库的存储实现。
//
// partner_memories、sqlite-vec 索引、FTS 关键词索引和 history
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

// migrate 建立 partner_memories、partner_memory_history、memory_meta 与 FTS5 关键词索引。
// sqlite-vec 向量表的维度依赖首次 Embedding 配置，由 vecstore 延迟创建。
func (s *store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS partner_memory_command_receipts (id TEXT PRIMARY KEY, partner_id TEXT NOT NULL, request_key TEXT NOT NULL UNIQUE, request_hash TEXT NOT NULL, memory_id TEXT NOT NULL, duplicate INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS partner_memories (
			seq           INTEGER PRIMARY KEY AUTOINCREMENT,
			id            TEXT NOT NULL UNIQUE,
			partner_id    TEXT NOT NULL,
			data          TEXT NOT NULL,
			hash          TEXT NOT NULL,
			source_run_id TEXT NOT NULL DEFAULT '',
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL,
			core          INTEGER NOT NULL DEFAULT 0,
			revision      INTEGER NOT NULL,
			origin        TEXT NOT NULL,
			user_controlled INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_partner_memories_group_recency ON partner_memories(partner_id, core, updated_at DESC, seq DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_partner_memories_recency ON partner_memories(partner_id, updated_at DESC, seq DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_partner_memories_hash_unique ON partner_memories(partner_id, hash)`,
		`CREATE TABLE IF NOT EXISTS partner_memory_history (
			id            TEXT PRIMARY KEY,
			memory_id     TEXT NOT NULL,
			partner_id    TEXT NOT NULL,
			old_memory    TEXT,
			new_memory    TEXT,
			event         TEXT NOT NULL,
			created_at    INTEGER NOT NULL,
			updated_at    INTEGER NOT NULL,
			is_deleted    INTEGER NOT NULL DEFAULT 0,
			source_run_id TEXT NOT NULL DEFAULT '',
			source        TEXT NOT NULL,
			review_chat_id TEXT NOT NULL DEFAULT '',
			review_after    INTEGER NOT NULL DEFAULT 0,
			review_through  INTEGER NOT NULL DEFAULT 0,
			review_evidence TEXT NOT NULL DEFAULT 'null'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_partner_history_memory ON partner_memory_history(memory_id)`,
		`CREATE INDEX IF NOT EXISTS idx_partner_history_recency ON partner_memory_history(partner_id, updated_at)`,
		`DROP INDEX IF EXISTS idx_partner_memory_history_owner`,
		`DROP INDEX IF EXISTS idx_partner_memories_group`,
		`CREATE TABLE IF NOT EXISTS partner_core_memory (
			partner_id TEXT PRIMARY KEY,
			revision INTEGER NOT NULL,
			body BLOB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS partner_forgotten_memory (
			partner_id TEXT NOT NULL,
			hash TEXT NOT NULL,
			PRIMARY KEY (partner_id, hash)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_partner_memory_receipts_owner ON partner_memory_command_receipts(partner_id)`,
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
	// FTS5 是关键词召回的前置能力；构建缺失时降级为 LIKE 扫描，不影响主流程。
	_, err := s.db.Exec(
		`CREATE VIRTUAL TABLE IF NOT EXISTS partner_memory_fts USING fts5(
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
func scanMemory(row interface{ Scan(...interface{}) error }, extra ...any) (*Memory, error) {
	memory := &Memory{}
	fields := []any{&memory.Seq, &memory.ID, &memory.PartnerID, &memory.Data, &memory.Hash,
		&memory.SourceRunID, &memory.CreatedAt, &memory.UpdatedAt, &memory.Core,
		&memory.Revision, &memory.Origin, &memory.control}
	if err := row.Scan(append(fields, extra...)...); err != nil {
		return nil, err
	}
	return memory, nil
}

// memoryColumns 是 partner_memories 表的完整查询列。
const memoryColumns = "seq, id, partner_id, data, hash, source_run_id, created_at, updated_at, core, revision, origin, user_controlled"

// memoryColumnsAliased 是 partner_memories 表使用 m 别名时的完整查询列。
const memoryColumnsAliased = "m.seq, m.id, m.partner_id, m.data, m.hash, m.source_run_id, m.created_at, m.updated_at, m.core, m.revision, m.origin, m.user_controlled"

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
		`INSERT INTO partner_memories (id, partner_id, data, hash, source_run_id, created_at, updated_at, core, revision, origin, user_controlled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		memory.ID,
		memory.PartnerID,
		memory.Data,
		memory.Hash,
		memory.SourceRunID,
		memory.CreatedAt,
		memory.UpdatedAt,
		memory.Core, memory.Revision, memory.Origin, memory.control,
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

// updateMemory 在事务内更新记忆正文、哈希与更新时间，伙伴归属不可变。
func (s *store) updateMemory(tx *sql.Tx, memory Memory) error {
	_, err := tx.Exec(
		`UPDATE partner_memories SET data = ?, hash = ?, source_run_id = ?, updated_at = ?, core = ?, revision = ?, user_controlled = ? WHERE id = ?`,
		memory.Data,
		memory.Hash,
		memory.SourceRunID,
		memory.UpdatedAt,
		memory.Core, memory.Revision, memory.control,
		memory.ID,
	)
	if err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deleteMemory 在事务内物理删除一条记忆。
func (s *store) deleteMemory(tx *sql.Tx, memoryID string) error {
	if _, err := tx.Exec(`DELETE FROM partner_memories WHERE id = ?`, memoryID); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// getMemory 按标识查询一条记忆。
func getMemory(queryer queryRower, id string) (*Memory, bool, error) {
	row := queryer.QueryRow(
		`SELECT `+memoryColumns+` FROM partner_memories WHERE id = ?`,
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
func findByHash(queryer queryRower, partnerID, hash string) (*Memory, bool, error) {
	row := queryer.QueryRow(
		`SELECT `+memoryColumns+` FROM partner_memories WHERE partner_id = ? AND hash = ?`,
		partnerID, hash,
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
	rows, err := s.db.Query(`SELECT ` + memoryColumns + ` FROM partner_memories ORDER BY seq ASC`)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	memories, err := scanMemories(rows)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	return memories, nil
}

// recentMemoriesInGroup 读取供复盘使用的一页同类记忆，无需统计总数或请求向量服务。
func (s *store) recentMemoriesInGroup(ctx context.Context, partnerID string, core bool, limit int) ([]Memory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+memoryColumns+` FROM partner_memories
		WHERE partner_id = ? AND core = ? ORDER BY updated_at DESC, seq DESC LIMIT ?`, partnerID, core, limit)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	items, err := scanMemories(rows)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	return items, nil
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
	evidence, err := json.Marshal(entry.ReviewEvidence)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO partner_memory_history
		 (id, memory_id, partner_id, old_memory, new_memory, event, created_at, updated_at, is_deleted, source_run_id, source, review_chat_id, review_after, review_through, review_evidence)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		entry.MemoryID,
		entry.PartnerID,
		oldMemory,
		newMemory,
		string(entry.Event),
		entry.CreatedAt,
		entry.UpdatedAt,
		entry.IsDeleted,
		entry.SourceRunID, entry.Source, entry.ReviewChatID, entry.ReviewAfter, entry.ReviewThrough, string(evidence),
	)
	if err != nil {
		return status_error.DatabaseError
	}
	return nil
}

func scanHistory(row interface{ Scan(...any) error }) (HistoryEntry, error) {
	var entry HistoryEntry
	var oldMemory, newMemory sql.NullString
	var evidence string
	if err := row.Scan(&entry.ID, &entry.MemoryID, &entry.PartnerID, &oldMemory, &newMemory,
		&entry.Event, &entry.CreatedAt, &entry.UpdatedAt, &entry.IsDeleted, &entry.SourceRunID,
		&entry.Source, &entry.ReviewChatID, &entry.ReviewAfter, &entry.ReviewThrough, &evidence); err != nil {
		return HistoryEntry{}, status_error.DatabaseError
	}
	if err := json.Unmarshal([]byte(evidence), &entry.ReviewEvidence); err != nil {
		return HistoryEntry{}, status_error.DatabaseError
	}
	entry.OldMemory, entry.NewMemory = oldMemory.String, newMemory.String
	return entry, nil
}

// listHistory 返回一条记忆的全部变更历史，按时间升序。
func (s *store) listHistory(memoryID string) ([]HistoryEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, memory_id, partner_id, old_memory, new_memory, event, created_at, updated_at, is_deleted, source_run_id, source, review_chat_id, review_after, review_through, review_evidence
		 FROM partner_memory_history WHERE memory_id = ? ORDER BY rowid ASC`,
		memoryID,
	)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	defer rows.Close()
	entries := []HistoryEntry{}
	for rows.Next() {
		entry, err := scanHistory(rows)
		if err != nil {
			return nil, err
		}
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
	if err := s.db.QueryRow(`SELECT count(*) FROM partner_memories`).Scan(&count); err != nil {
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
	if _, err := tx.Exec(`DELETE FROM partner_memory_fts WHERE memory_id = ?`, memoryID); err != nil {
		return status_error.DatabaseError
	}
	if _, err := tx.Exec(
		`INSERT INTO partner_memory_fts (memory_id, data) VALUES (?, ?)`,
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
	if _, err := tx.Exec(`DELETE FROM partner_memory_fts WHERE memory_id = ?`, memoryID); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deletePartnerFTS 清除伙伴的全文索引项。
func (s *store) deletePartnerFTS(tx *sql.Tx, partnerID string) error {
	if !s.ftsAvailable {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM partner_memory_fts WHERE memory_id IN
		(SELECT id FROM partner_memories WHERE partner_id = ?)`, partnerID); err != nil {
		return status_error.DatabaseError
	}
	return nil
}

// deletePartnerData 清除伙伴当前内容、历史、核心快照和命令回执。
func (s *store) deletePartnerData(tx *sql.Tx, partnerID string) error {
	for _, query := range []string{
		`DELETE FROM partner_memories WHERE partner_id = ?`,
		`DELETE FROM partner_memory_history WHERE partner_id = ?`,
		`DELETE FROM partner_core_memory WHERE partner_id = ?`,
		`DELETE FROM partner_forgotten_memory WHERE partner_id = ?`,
		`DELETE FROM partner_memory_command_receipts WHERE partner_id = ?`,
	} {
		if _, err := tx.Exec(query, partnerID); err != nil {
			return status_error.DatabaseError
		}
	}
	return nil
}

func (s *store) listPartnerOwners(ctx context.Context) ([]string, error) {
	owners := []string{}
	for _, query := range []string{
		`SELECT DISTINCT partner_id FROM partner_memories`,
		`SELECT DISTINCT partner_id FROM partner_memory_history`,
		`SELECT partner_id FROM partner_core_memory`,
		`SELECT DISTINCT partner_id FROM partner_forgotten_memory`,
		`SELECT DISTINCT partner_id FROM partner_memory_command_receipts`,
	} {
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return nil, status_error.DatabaseError
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, status_error.DatabaseError
			}
			owners = append(owners, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, status_error.DatabaseError
		}
	}
	slices.Sort(owners)
	return slices.Compact(owners), nil
}

// listMemoryPage 返回指定范围的一页当前记忆。
func (s *store) listMemoryPage(ctx context.Context, filter ListFilter) (MemoryPage, error) {
	where := `partner_id = ?`
	args := []any{filter.PartnerID}
	if filter.Core != nil {
		where += ` AND core = ?`
		args = append(args, *filter.Core)
	}
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
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM partner_memories WHERE `+where, args...).Scan(&total); err != nil {
		return MemoryPage{}, status_error.DatabaseError
	}
	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+memoryColumns+` FROM partner_memories WHERE `+where+` ORDER BY updated_at DESC, seq DESC LIMIT ? OFFSET ?`, queryArgs...)
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
	where := `partner_id = ?`
	args := []any{filter.PartnerID}
	if filter.Query != "" {
		where += ` AND (COALESCE(old_memory, '') LIKE ? ESCAPE '\' OR COALESCE(new_memory, '') LIKE ? ESCAPE '\')`
		pattern := "%" + escapeLike(filter.Query) + "%"
		args = append(args, pattern, pattern)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM partner_memory_history WHERE `+where, args...).Scan(&total); err != nil {
		return TimelinePage{}, status_error.DatabaseError
	}
	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, memory_id, partner_id, old_memory, new_memory, event, created_at, updated_at, is_deleted, source_run_id, source, review_chat_id, review_after, review_through, review_evidence FROM partner_memory_history WHERE `+where+` ORDER BY updated_at DESC, rowid DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return TimelinePage{}, status_error.DatabaseError
	}
	defer rows.Close()
	items := []HistoryEntry{}
	for rows.Next() {
		entry, err := scanHistory(rows)
		if err != nil {
			return TimelinePage{}, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return TimelinePage{}, status_error.DatabaseError
	}
	return TimelinePage{Items: items, Total: total}, nil
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
