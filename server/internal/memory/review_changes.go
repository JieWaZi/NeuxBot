package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

const (
	// reviewMemoryBytes 保留复盘证据总预算中已有记忆可用的空间。
	reviewMemoryBytes = 4 * 1024
	// reviewMemoryLimit 限定每组本地候选和最终模型可见的条目数量。
	reviewMemoryLimit = 20
)

// ReviewMemory 记录模型可见的既有记忆及服务端提交所需的身份。
type ReviewMemory struct {
	// ID 是已有条目的标识。
	ID string `json:"id"`
	// Content 是条目正文；外发前由复盘编排执行脱敏。
	Content string `json:"content"`
	// Origin 是提交时核对的首次写入来源。
	Origin string `json:"-"`
	// Revision 是防止在途覆盖的内部修订。
	Revision uint64 `json:"-"`
	// Core 是该候选的本地分类。
	Core bool `json:"-"`
	// ReadOnly 保护手工、已接管或被脱敏的条目。
	ReadOnly bool `json:"readOnly,omitempty"`
}

// ReviewMemoryChange 是模型从合格用户证据中提出的一项变更。
type ReviewMemoryChange struct {
	// Action 为 add、update 或 forget。
	Action string `json:"action"`
	// MemoryID 指向本批模型可见的已有条目。
	MemoryID string `json:"memoryId,omitempty"`
	// Content 是新增或更新后的正文。
	Content string `json:"content,omitempty"`
	// Core 提议将新增的跨会话知识列为核心。
	Core bool `json:"core,omitempty"`
	// EvidenceIDs 引用本批中支持操作的用户消息。
	EvidenceIDs []string `json:"evidenceIds"`
}

// ReviewMemoryBatch 将模型候选与服务端冻结的来源约束绑定。
type ReviewMemoryBatch struct {
	// PartnerID 是来源 Chat 所属的伙伴。
	PartnerID string
	// ChatID 是权威证据所在的 Chat。
	ChatID string
	// After 是已排除的前一序号。
	After int64
	// Through 是批次覆盖的最后序号。
	Through int64
	// EvidenceIDs 是真正向模型提供的合格用户消息。
	EvidenceIDs []string
	// Snapshot 是本批真正向模型提供的已有记忆，脱敏条目标为只读。
	Snapshot []ReviewMemory
	// Changes 是模型所提操作。
	Changes []ReviewMemoryChange
}

type preparedReviewMemory struct {
	// change 是通过本地有界验证的操作。
	change ReviewMemoryChange
	// revision 是既有条目在本批外发时的版本。
	revision uint64
	// vector 是事务外根据当前嵌入配置取得的向量。
	vector []float32
}

// PreparedReviewChanges 保存提交前已经验证并嵌入的候选。
type PreparedReviewChanges struct {
	// partnerID 是候选实际归属。
	partnerID string
	// source 保存同一批次的历史来源字段。
	source HistoryEntry
	// changes 是按遗忘、更新、新增排序的候选。
	changes []preparedReviewMemory
	// embedding 是准备时实际选中的嵌入配置。
	embedding EmbeddingConfig
	// checkedEmbedding 表示本批包含需要保存正文的操作。
	checkedEmbedding bool
}

// ReviewDB 为同包复盘状态提供同库事务的底层连接。
func (s *Service) ReviewDB() *sql.DB { return s.store.db }

// RelevantReviewMemories 交错提供核心、关键词相关及近期普通记忆，供模型判断是否修改。
func (s *Service) RelevantReviewMemories(ctx context.Context, partnerID, query string) ([]ReviewMemory, error) {
	core, coreErr := s.store.recentMemoriesInGroup(ctx, partnerID, true, reviewMemoryLimit)
	recent, recentErr := s.store.recentMemoriesInGroup(ctx, partnerID, false, reviewMemoryLimit)
	var related []Memory
	var relatedErr error
	if strings.TrimSpace(query) != "" {
		var hits []ScoredMemory
		hits, relatedErr = s.keyword.searchReview(ctx, query, reviewMemoryLimit, partnerID)
		for _, hit := range hits {
			related = append(related, hit.Memory)
		}
	}
	groups := [][]Memory{core, related, recent}
	items := []ReviewMemory{}
	seen := make(map[string]bool, reviewMemoryLimit)
	size := 2
	for i := 0; i < max(len(core), len(related), len(recent)) && len(items) < reviewMemoryLimit; i++ {
		for _, group := range groups {
			if i >= len(group) || len(items) == reviewMemoryLimit {
				continue
			}
			value := group[i]
			if seen[value.ID] {
				continue
			}
			seen[value.ID] = true
			item := ReviewMemory{ID: value.ID, Content: value.Data, Origin: string(value.Origin),
				Core: value.Core, Revision: value.Revision, ReadOnly: !value.reviewEditable()}
			body, err := json.Marshal(item)
			if err != nil {
				return items, err
			}
			extra := len(body)
			if len(items) > 0 {
				extra++
			}
			if size+extra <= reviewMemoryBytes {
				size += extra
				items = append(items, item)
			}
		}
	}
	return items, errors.Join(coreErr, relatedErr, recentErr)
}

// PrepareReviewChanges 过滤无证据或不可编辑的候选，在写锁外准备实际存储需要的向量。
func (s *Service) PrepareReviewChanges(ctx context.Context, batch ReviewMemoryBatch) (*PreparedReviewChanges, error) {
	if batch.PartnerID == "" || batch.ChatID == "" || batch.After < 0 || batch.Through <= batch.After {
		return nil, status_error.BadRequest
	}
	prepared := &PreparedReviewChanges{partnerID: batch.PartnerID, source: HistoryEntry{
		Source: constants.MemoryTimelineSourceReview, ReviewChatID: batch.ChatID, ReviewAfter: batch.After, ReviewThrough: batch.Through,
	}}
	evidence := make(map[string]bool, len(batch.EvidenceIDs))
	for _, id := range batch.EvidenceIDs {
		if id != "" {
			evidence[id] = true
		}
	}
	editable := make(map[string]uint64, len(batch.Snapshot))
	for _, item := range batch.Snapshot {
		if !item.ReadOnly && item.Origin == string(constants.MemoryTimelineSourceReview) && item.Revision != 0 {
			editable[item.ID] = item.Revision
		}
	}

	changes := batch.Changes[:min(len(batch.Changes), reviewMemoryLimit)]
	seen := map[string]bool{}
	var state embeddingState
	for _, action := range []string{"forget", "update", "add"} {
		for _, change := range changes {
			if change.Action != action || len(change.EvidenceIDs) == 0 || len(change.EvidenceIDs) > reviewMemoryLimit {
				continue
			}
			validEvidence := true
			for _, id := range change.EvidenceIDs {
				if !evidence[id] {
					validEvidence = false
					break
				}
			}
			if !validEvidence {
				continue
			}
			revision := editable[change.MemoryID]
			if action == "add" && change.MemoryID != "" || action != "add" && (revision == 0 || seen[change.MemoryID]) {
				continue
			}
			if revision != 0 {
				target, err := editableReviewTarget(s.store.db, batch.PartnerID, change.MemoryID, revision)
				if err != nil {
					return nil, err
				}
				if target == nil {
					continue
				}
			}
			var vector []float32
			if action != "forget" {
				content, err := validateContent(change.Content)
				if err != nil {
					continue
				}
				change.Content = content
				if !prepared.checkedEmbedding {
					state, err = s.resolveEmbedding(ctx)
					if err != nil {
						return nil, err
					}
					prepared.checkedEmbedding, prepared.embedding = true, state.config
				}
				embedAction := constants.MemoryEmbeddingActionAdd
				if action == "update" {
					embedAction = constants.MemoryEmbeddingActionUpdate
				}
				vector, err = s.embedMemory(ctx, state, content, embedAction)
				if errors.Is(err, status_error.MemoryEmbeddingConflict) {
					vector = nil
				} else if err != nil {
					return nil, err
				}
			}
			change.EvidenceIDs = slices.Clone(change.EvidenceIDs)
			prepared.changes = append(prepared.changes, preparedReviewMemory{change: change, revision: revision, vector: vector})
			seen[change.MemoryID] = true
		}
	}
	return prepared, nil
}

// CommitReviewChanges 在同一短事务验证版本、提交变更，再调用复盘状态回执。
func (s *Service) CommitReviewChanges(ctx context.Context, prepared *PreparedReviewChanges, commit func(*sql.Tx, bool) error) error {
	if prepared == nil {
		return status_error.BadRequest
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	writeVectors := false
	if prepared.checkedEmbedding {
		state, err := s.resolveEmbedding(ctx)
		if err != nil {
			return err
		}
		writeVectors = !state.conflict && state.config == prepared.embedding
	}
	if writeVectors {
		var dimensions int
		for _, candidate := range prepared.changes {
			if len(candidate.vector) == 0 {
				continue
			}
			if dimensions == 0 {
				dimensions = len(candidate.vector)
				if err := s.checkDimensions(candidate.vector); err != nil {
					if !errors.Is(err, status_error.MemoryEmbeddingConflict) {
						return err
					}
					writeVectors = false
					break
				}
			} else if len(candidate.vector) != dimensions {
				writeVectors = false
				break
			}
		}
	}
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		changed, coreChanged := false, false
		forgotten := map[string]bool{}
		for _, candidate := range prepared.changes {
			if !writeVectors {
				candidate.vector = nil
			}
			applied, core, err := s.applyReviewChange(tx, prepared, candidate, forgotten)
			if err != nil {
				return err
			}
			changed, coreChanged = changed || applied, coreChanged || core
		}
		if coreChanged {
			if err := updateCoreSnapshot(tx, prepared.partnerID); err != nil {
				return err
			}
		}
		if commit != nil {
			return commit(tx, changed)
		}
		return nil
	})
}

func (s *Service) applyReviewChange(tx *sql.Tx, batch *PreparedReviewChanges, candidate preparedReviewMemory, forgotten map[string]bool) (changed, coreChanged bool, err error) {
	change := candidate.change
	history := batch.source
	history.ReviewEvidence = change.EvidenceIDs
	now := time.Now().UnixMilli()
	memory := &Memory{ID: util.New(), PartnerID: batch.partnerID, CreatedAt: now, Origin: constants.MemoryTimelineSourceReview, Core: change.Core}
	if change.Action != "add" {
		memory, err = editableReviewTarget(tx, batch.partnerID, change.MemoryID, candidate.revision)
		if err != nil || memory == nil {
			return false, false, err
		}
	}
	if change.Action == "forget" {
		forgotten[memory.Hash] = true
		err := s.applyMemoryForget(tx, *memory, history)
		return true, memory.Core, err
	}
	hash := contentHash(change.Content)
	if forgotten[hash] {
		return false, false, nil
	}
	if suppressed, err := reviewHashSuppressed(tx, batch.partnerID, hash); err != nil || suppressed {
		return false, false, err
	}
	if _, exists, err := findByHash(tx, batch.partnerID, hash); err != nil || exists {
		return false, false, err
	}

	history.Event = constants.MemoryEventAdd
	if change.Action == "update" {
		history.Event, history.OldMemory = constants.MemoryEventUpdate, memory.Data
	}
	memory.Data, memory.Hash, memory.UpdatedAt = change.Content, hash, now
	memory.Revision++
	if memory.Core {
		if _, err := coreDocument(tx, batch.partnerID, memory); errors.Is(err, status_error.MemoryCoreBudgetExceeded) {
			if change.Action == "update" {
				return false, false, nil
			}
			memory.Core = false
		} else if err != nil {
			return false, false, err
		}
	}
	err = s.applyMemoryContent(tx, memory, candidate.vector, batch.embedding, history)
	return true, memory.Core, err
}

func editableReviewTarget(queryer queryRower, partnerID, memoryID string, revision uint64) (*Memory, error) {
	memory, err := getAccessibleMemory(queryer, memoryID, partnerID)
	if errors.Is(err, status_error.MemoryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if memory.Revision != revision || !memory.reviewEditable() {
		return nil, nil
	}
	return memory, nil
}

func reviewHashSuppressed(tx *sql.Tx, partnerID, hash string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM partner_forgotten_memory WHERE partner_id = ? AND hash = ?)`, partnerID, hash).Scan(&exists); err != nil {
		return false, status_error.DatabaseError
	}
	return exists, nil
}
