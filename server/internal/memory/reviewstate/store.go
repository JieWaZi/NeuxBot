package reviewstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const (
	SourcePartner      = "partner"
	SourceGlobal       = "global"
	ModeOff            = "off"
	ModeAsk            = "ask"
	ModeAutomatic      = "automatic"
	ActionCreate       = "create"
	ActionUpdate       = "update"
	DecisionSave       = "save"
	DecisionIgnore     = "ignore"
	StatusPending      = "pending"
	StatusQueued       = "queued"
	StatusRunning      = "running"
	StatusSaved        = "saved"
	StatusIgnored      = "ignored"
	StatusFailed       = "failed"
	StatusIdle         = "idle"
	retryFailedReviews = "UPDATE review_chats SET due_at=0,failures=0 WHERE failures>0"
)

// Settings 保存共用复盘配置。
type Settings struct {
	// Source 是跟随伙伴或应用级专用 CLI。
	Source string `json:"source"`
	// RuntimeID 是专用 CLI。
	RuntimeID string `json:"runtimeId,omitempty"`
	// AccountID 是专用账号。
	AccountID string `json:"accountId,omitempty"`
	// Model 是专用模型。
	Model string `json:"model,omitempty"`
	// EveryTurns 是触发整理所需的合格轮次。
	EveryTurns int `json:"everyTurns"`
}

// Partner 保存每个功能独立的伙伴授权。
type Partner struct {
	// MemoryEnabled 表示自动记忆开关。
	MemoryEnabled bool `json:"memoryEnabled"`
	// SkillMode 表示关闭、先询问或自动保存。
	SkillMode string `json:"skillMode"`
	// MemoryGeneration 标识当前记忆开关代次。
	MemoryGeneration int64 `json:"-"`
	// SkillGeneration 标识当前技能开关代次。
	SkillGeneration int64 `json:"-"`
}

// Admission 保存轮次接纳时的已授权功能代次。
type Admission struct {
	// Memory 是允许记忆复盘的伙伴设置代次。
	Memory int64 `json:"memory,omitempty"`
	// Skill 是允许技能学习的伙伴设置代次。
	Skill int64 `json:"skill,omitempty"`
}

// Progress 保存一个 Chat 的稳定复盘边界。
type Progress struct {
	// ChatID 定位原权威聊天。
	ChatID string
	// WakerID 定位来源伙伴。
	WakerID string
	// Cursor 是已提交的最后终态。
	Cursor int64
	// Through 是已触发的终态上界。
	Through int64
	// DueAt 是可再次领取的 Unix 毫秒。
	DueAt int64
	// Failures 是当前批次连续失败次数。
	Failures int
}

// Suggestion 保存短建议及第二阶段执行状态。
type Suggestion struct {
	// ID 是发布幂等键。
	ID string `json:"id"`
	// ChatID 是归属聊天。
	ChatID string `json:"chatId"`
	// WakerID 是归属伙伴。
	WakerID string `json:"wakerId"`
	// Name 是建议技能的名称。
	Name string `json:"name"`
	// Lesson 是需沉淀的方法。
	Lesson string `json:"lesson"`
	// Action 是新建或修订。
	Action string `json:"action"`
	// Status 是可保存、已排队、运行、成功、忽略或失败。
	Status string `json:"status"`
	// SkillID 是目标或已保存技能。
	SkillID string `json:"skillId,omitempty"`
	// VersionID 是目标或已发布版本。
	VersionID string `json:"versionId,omitempty"`
	// SharedPartnerIDs 是需确认的安装范围。
	SharedPartnerIDs []string `json:"sharedPartnerIds,omitempty"`
	// Error 是稳定失败码。
	Error string `json:"error,omitempty"`
	// SourceMessageIDs 是本批的安全公开定位标识。
	SourceMessageIDs []string `json:"sourceMessageIds,omitempty"`
	// EvidenceIDs 是重读权威来源所需的原始消息或工具步骤标识。
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
	// Origin 是判断时的授权代次。
	Origin Admission `json:"origin"`
	// From 是原复盘区间起点（不包含）。
	From int64 `json:"from"`
	// Through 是原复盘区间终点。
	Through int64 `json:"through"`
	// ConfirmedScope 区分人工确认的影响范围与后台自动派发。
	ConfirmedScope bool `json:"confirmedScope,omitempty"`
	// CreatedAt 是建议首次提交时间。
	CreatedAt int64 `json:"createdAt"`
	// PublishedAt 是技能发布回执记录的时间，用于固定已保存提示的位置。
	PublishedAt int64 `json:"publishedAt,omitempty"`
}

// Store 维护独立记忆库中的复盘状态。
type Store struct {
	// db 与记忆写入共享事务。
	db *sql.DB
}

// Open 建立复盘状态表。
func Open(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS review_config (id TEXT PRIMARY KEY, body BLOB NOT NULL, revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS review_chats (chat_id TEXT PRIMARY KEY, waker_id TEXT NOT NULL, cursor INTEGER NOT NULL DEFAULT 0, through_seq INTEGER NOT NULL DEFAULT 0, due_at INTEGER NOT NULL, failures INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS review_chats_due ON review_chats(due_at, chat_id);
CREATE INDEX IF NOT EXISTS review_chats_waker ON review_chats(waker_id);
CREATE TABLE IF NOT EXISTS review_suggestions (id TEXT PRIMARY KEY, chat_id TEXT NOT NULL, waker_id TEXT NOT NULL, name TEXT NOT NULL, lesson TEXT NOT NULL, status TEXT NOT NULL, created_at INTEGER NOT NULL, body BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS review_suggestions_queue ON review_suggestions(status, created_at);
CREATE INDEX IF NOT EXISTS review_suggestions_chat ON review_suggestions(chat_id, created_at);
CREATE INDEX IF NOT EXISTS review_suggestions_timeline ON review_suggestions(chat_id, ` + suggestionTimelineTime + `, id) WHERE status <> 'ignored';
DROP INDEX IF EXISTS review_suggestions_scope_recent;
DROP INDEX IF EXISTS review_suggestions_scope_exact;
CREATE INDEX IF NOT EXISTS review_suggestions_skill_recent ON review_suggestions(
	waker_id, json_extract(body, '$.origin.skill'), created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS review_suggestions_skill_exact ON review_suggestions(
	waker_id, json_extract(body, '$.origin.skill'), name COLLATE NOCASE, lesson);
`)
	return s, mapDB(err)
}

// Settings 读取全局配置。
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	v := Settings{Source: SourcePartner, EveryTurns: DefaultEveryTurns}
	var data []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM review_config WHERE id='settings'").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, mapDB(err)
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, mapDB(err)
	}
	if !validFrequency(v.EveryTurns) {
		v.EveryTurns = DefaultEveryTurns
	}
	return v, nil
}

// SetSettings 保存配置，降低整理轮次门槛时重新检查等待的批次。
func (s *Store) SetSettings(ctx context.Context, v Settings, checkWaiting bool) error {
	data, err := json.Marshal(v)
	if err != nil {
		return mapDB(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDB(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_config(id,body,revision) VALUES('settings',?,1)
		ON CONFLICT(id) DO UPDATE SET body=excluded.body`, data); err != nil {
		return mapDB(err)
	}
	if checkWaiting {
		if _, err = tx.ExecContext(ctx, `UPDATE review_chats SET due_at=0
			WHERE due_at>0 AND due_at<? AND through_seq=0 AND failures=0`, idleReviewDueAt); err != nil {
			return mapDB(err)
		}
	}
	return mapDB(tx.Commit())
}

func validFrequency(everyTurns int) bool {
	switch everyTurns {
	case 5, 10, 20, 30, 50:
		return true
	}
	return false
}

// Partner 读取伙伴配置。
func (s *Store) Partner(ctx context.Context, id string) (Partner, error) {
	v := Partner{SkillMode: ModeOff}
	var data []byte
	err := s.db.QueryRowContext(ctx, "SELECT body FROM review_config WHERE id=?", "waker:"+id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, mapDB(err)
	}
	var stored struct {
		// MemoryEnabled 保存开关。
		MemoryEnabled bool
		// SkillMode 保存模式。
		SkillMode string
		// MemoryGeneration 保存记忆代次。
		MemoryGeneration int64
		// SkillGeneration 保存技能代次。
		SkillGeneration int64
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return v, mapDB(err)
	}
	return Partner(stored), nil
}

// PartnerPreference 是伙伴配置页使用的已保存开关。
type PartnerPreference struct {
	// MemoryEnabled 表示自动记忆开关。
	MemoryEnabled bool `json:"memoryEnabled"`
	// SkillMode 表示关闭、先询问或自动保存。
	SkillMode string `json:"skillMode"`
}

// PartnerPreferences 批量读取一页伙伴的已保存开关，缺少的记录由调用方补充默认值。
func (s *Store) PartnerPreferences(ctx context.Context, ids []string) (map[string]PartnerPreference, error) {
	preferences := make(map[string]PartnerPreference, len(ids))
	if len(ids) == 0 {
		return preferences, nil
	}
	keys := make([]any, len(ids))
	for i, id := range ids {
		keys[i] = "waker:" + id
	}
	placeholders := strings.Repeat("?,", len(keys)-1) + "?"
	rows, err := s.db.QueryContext(ctx, "SELECT id, body FROM review_config WHERE id IN ("+placeholders+")", keys...)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			return nil, mapDB(err)
		}
		var preference PartnerPreference
		if err := json.Unmarshal(data, &preference); err != nil {
			return nil, mapDB(err)
		}
		preferences[strings.TrimPrefix(id, "waker:")] = preference
	}
	if err := rows.Err(); err != nil {
		return nil, mapDB(err)
	}
	return preferences, nil
}

// SetPartner 保存伙伴功能设置及开关代次。
func (s *Store) SetPartner(ctx context.Context, id string, v Partner) error {
	data, err := json.Marshal(struct {
		// MemoryEnabled 保存开关。
		MemoryEnabled bool
		// SkillMode 保存模式。
		SkillMode string
		// MemoryGeneration 保存记忆代次。
		MemoryGeneration int64
		// SkillGeneration 保存技能代次。
		SkillGeneration int64
	}{v.MemoryEnabled, v.SkillMode, v.MemoryGeneration, v.SkillGeneration})
	if err != nil {
		return mapDB(err)
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO review_config(id,body,revision) VALUES(?,?,1) ON CONFLICT(id) DO UPDATE SET body=excluded.body", "waker:"+id, data)
	return mapDB(err)
}

// Observe 登记会话并安排一次后台对账。
func (s *Store) Observe(ctx context.Context, chat, waker string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO review_chats(chat_id,waker_id,due_at) VALUES(?,?,0) ON CONFLICT(chat_id) DO UPDATE SET due_at=CASE WHEN failures=0 THEN 0 ELSE due_at END", chat, waker)
	return mapDB(err)
}

// Ready 返回最先到期的批次。
func (s *Store) Ready(ctx context.Context, now time.Time) (Progress, bool, error) {
	var p Progress
	err := s.db.QueryRowContext(ctx, "SELECT chat_id,waker_id,cursor,through_seq,due_at,failures FROM review_chats WHERE due_at<=? ORDER BY due_at,chat_id LIMIT 1", now.UnixMilli()).Scan(&p.ChatID, &p.WakerID, &p.Cursor, &p.Through, &p.DueAt, &p.Failures)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, mapDB(err)
}

// UpdateProgress 根据读取时的状态保存批次边界、跳过结果或重试计划。
func (s *Store) UpdateProgress(ctx context.Context, before, after Progress) error {
	result, err := s.db.ExecContext(ctx, `UPDATE review_chats SET cursor=?,through_seq=?,due_at=?,failures=?
		WHERE chat_id=? AND cursor=? AND through_seq=? AND due_at=? AND failures=?`,
		after.Cursor, after.Through, after.DueAt, after.Failures,
		before.ChatID, before.Cursor, before.Through, before.DueAt, before.Failures)
	if err != nil {
		return mapDB(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return mapDB(err)
	}
	if rows != 1 {
		return status_error.Conflict
	}
	return nil
}

// Retry 重新领取指定伙伴的失败批次。
func (s *Store) Retry(ctx context.Context, waker string) error {
	_, err := s.db.ExecContext(ctx, retryFailedReviews+" AND waker_id=?", waker)
	return mapDB(err)
}

// ProgressStatus 返回伙伴是否还有失败批次。
func (s *Store) ProgressStatus(ctx context.Context, waker string) (string, error) {
	var failed, pending int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_chats WHERE waker_id=? AND failures>0),
        EXISTS(SELECT 1 FROM review_chats WHERE waker_id=? AND through_seq>0)`, waker, waker).Scan(&failed, &pending)
	if failed != 0 {
		return StatusFailed, mapDB(err)
	}
	if pending != 0 {
		return StatusPending, mapDB(err)
	}
	return StatusIdle, mapDB(err)
}

// ForgetChat 清理不存在的会话残留。
func (s *Store) ForgetChat(ctx context.Context, chat string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDB(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM review_suggestions WHERE chat_id=?", chat); err != nil {
		return mapDB(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM review_chats WHERE chat_id=?", chat); err != nil {
		return mapDB(err)
	}
	return mapDB(tx.Commit())
}

// CommitProgress 和记忆变更一起提交游标，报告本次是否写入了新建议。
func CommitProgress(ctx context.Context, tx *sql.Tx, p Progress, next int64, proposal *Suggestion) (bool, error) {
	through := p.Through
	if next >= through {
		through = 0
	}
	r, err := tx.ExecContext(ctx, "UPDATE review_chats SET cursor=?,through_seq=?,due_at=0,failures=0 WHERE chat_id=? AND cursor=?", next, through, p.ChatID, p.Cursor)
	if err != nil {
		return false, mapDB(err)
	}
	rows, err := r.RowsAffected()
	if err != nil {
		return false, mapDB(err)
	}
	if rows != 1 {
		return false, status_error.Conflict
	}
	if proposal == nil {
		return false, nil
	}
	data, err := json.Marshal(proposal)
	if err != nil {
		return false, mapDB(err)
	}
	r, err = tx.ExecContext(ctx, `INSERT INTO review_suggestions(id,chat_id,waker_id,name,lesson,status,created_at,body)
SELECT ?,?,?,?,?,?,?,? WHERE NOT EXISTS(
	SELECT 1 FROM review_suggestions WHERE `+suggestionScope+` AND name=? COLLATE NOCASE AND lesson=?
	AND json_extract(body, '$.action')=? AND (? <> 'update' OR (
		json_extract(body, '$.skillId')=? AND json_extract(body, '$.versionId')=?)))
ON CONFLICT(id) DO NOTHING`, proposal.ID, proposal.ChatID, proposal.WakerID, proposal.Name, proposal.Lesson, proposal.Status, proposal.CreatedAt, data,
		proposal.WakerID, proposal.Origin.Skill, proposal.Name, proposal.Lesson, proposal.Action,
		proposal.Action, proposal.SkillID, proposal.VersionID)
	if err != nil {
		return false, mapDB(err)
	}
	rows, err = r.RowsAffected()
	return rows == 1, mapDB(err)
}

// suggestionScope 匹配仍属于同一伙伴和当前学习授权的历史。
const suggestionScope = `waker_id=? AND json_extract(body, '$.origin.skill')=?`

// RecentSkillSuggestions 返回同一学习授权下最近的短建议，供模型判断是否已记录该经验。
func (s *Store) RecentSkillSuggestions(ctx context.Context, waker string, generation int64) ([]Suggestion, error) {
	return s.listSuggestions(ctx, `SELECT body FROM review_suggestions WHERE `+suggestionScope+`
		ORDER BY created_at DESC,id DESC LIMIT 5`, waker, generation)
}

// SaveSuggestion 通过预期状态比较提交建议变化。
func (s *Store) SaveSuggestion(ctx context.Context, v Suggestion, expected string) error {
	data, err := json.Marshal(v)
	if err != nil {
		return mapDB(err)
	}
	result, err := s.db.ExecContext(ctx, "UPDATE review_suggestions SET status=?,body=? WHERE id=? AND chat_id=? AND status=?", v.Status, data, v.ID, v.ChatID, expected)
	if err != nil {
		return mapDB(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return mapDB(err)
	}
	if rows != 1 {
		return status_error.Conflict
	}
	return nil
}

// Suggestion 读取原聊天的一条建议。
func (s *Store) Suggestion(ctx context.Context, chat, id string) (Suggestion, error) {
	return scanSuggestion(s.db.QueryRowContext(ctx, "SELECT body FROM review_suggestions WHERE chat_id=? AND id=?", chat, id))
}

// ReceiptRetirable 判断建议是否已终结或随其来源被清理。失败建议仍可重试。
func (s *Store) ReceiptRetirable(ctx context.Context, id string) (bool, error) {
	var done bool
	err := s.db.QueryRowContext(ctx, "SELECT NOT EXISTS(SELECT 1 FROM review_suggestions WHERE id=? AND status NOT IN (?,?))",
		id, StatusSaved, StatusIgnored).Scan(&done)
	return done, mapDB(err)
}

// Queued 返回下一条待生成建议。
func (s *Store) Queued(ctx context.Context) (Suggestion, error) {
	return scanSuggestion(s.db.QueryRowContext(ctx, "SELECT body FROM review_suggestions WHERE status IN (?,?) ORDER BY created_at,id LIMIT 1", StatusQueued, StatusRunning))
}

// suggestionTimelineTime 以实际发布回执定位已保存建议，兼容旧回执没有发布时间。
const suggestionTimelineTime = `COALESCE(NULLIF(CASE WHEN status = 'saved' THEN CAST(json_extract(body, '$.publishedAt') AS INTEGER) END, 0), created_at)`

// Suggestions 按时间窗口返回原聊天可展示的建议。
func (s *Store) Suggestions(ctx context.Context, chat string, fromTime int64) ([]Suggestion, error) {
	return s.listSuggestions(ctx, `SELECT body FROM review_suggestions
		WHERE chat_id=? AND status <> 'ignored' AND `+suggestionTimelineTime+` >= ?
		ORDER BY `+suggestionTimelineTime+`, id`, chat, fromTime)
}

func (s *Store) listSuggestions(ctx context.Context, query string, args ...any) ([]Suggestion, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	list := make([]Suggestion, 0)
	for rows.Next() {
		v, err := scanSuggestion(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, mapDB(rows.Err())
}

func scanSuggestion(row interface{ Scan(...any) error }) (Suggestion, error) {
	var v Suggestion
	var data []byte
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, nil
		}
		return v, mapDB(err)
	}
	return v, mapDB(json.Unmarshal(data, &v))
}

func mapDB(err error) error {
	if err == nil {
		return nil
	}
	logx.Errorf("review store: %v", err)
	return status_error.DatabaseError
}

// CommitOnly 提交无记忆变更的游标与建议。
func (s *Store) CommitOnly(ctx context.Context, apply func(*sql.Tx, bool) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDB(err)
	}
	defer tx.Rollback()
	if err = apply(tx, false); err != nil {
		return err
	}
	return mapDB(tx.Commit())
}

// ObservedIDs 列出需要在 Core 启动时复核来源删除的复盘记录。
func (s *Store) ObservedIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT chat_id FROM review_chats")
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, mapDB(err)
		}
		ids = append(ids, id)
	}
	return ids, mapDB(rows.Err())
}

// ForgetPartner 在伙伴已删除后清理其复盘状态。
func (s *Store) ForgetPartner(ctx context.Context, waker string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapDB(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM review_suggestions WHERE waker_id=?", waker); err != nil {
		return mapDB(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM review_chats WHERE waker_id=?", waker); err != nil {
		return mapDB(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM review_config WHERE id=?", "waker:"+waker); err != nil {
		return mapDB(err)
	}
	return mapDB(tx.Commit())
}

// ConfiguredPartnerIDs 列出需在启动时核验的伙伴偏好归属。
func (s *Store) ConfiguredPartnerIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT substr(id,7) FROM review_config WHERE id LIKE 'waker:%'")
	if err != nil {
		return nil, mapDB(err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, mapDB(err)
		}
		ids = append(ids, id)
	}
	return ids, mapDB(rows.Err())
}
