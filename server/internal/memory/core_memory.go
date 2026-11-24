package memory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// CoreMemoryBudget 是伙伴最新核心快照序列化为 UTF-8 后的最大字节数。
const CoreMemoryBudget = 8 * 1024

const emptyCoreMemory = `{"items":[]}`

// CoreSnapshot 是同一伙伴的一版完整核心上下文。
type CoreSnapshot struct {
	// Revision 在内容或选择实际改变后推进。
	Revision uint64
	// JSON 是按标识排序的完整上下文，始终符合 CoreMemoryBudget。
	JSON []byte
}

type coreMemoryItem struct {
	// ID 是记忆条目的稳定标识。
	ID string `json:"id"`
	// Text 是条目正文。
	Text string `json:"text"`
}

type coreMemoryDocument struct {
	// Items 是当前选为核心的条目。
	Items []coreMemoryItem `json:"items"`
}

// CoreSnapshot 只读取事务内更新的有界本地快照。
func (s *Service) CoreSnapshot(ctx context.Context, partnerID string) (CoreSnapshot, error) {
	if strings.TrimSpace(partnerID) == "" {
		return CoreSnapshot{}, status_error.BadRequest
	}
	var result CoreSnapshot
	err := s.store.db.QueryRowContext(ctx, `SELECT revision, CASE WHEN length(body) <= ? THEN body END FROM partner_core_memory WHERE partner_id = ?`, CoreMemoryBudget, partnerID).Scan(&result.Revision, &result.JSON)
	if errors.Is(err, sql.ErrNoRows) {
		return CoreSnapshot{JSON: []byte(emptyCoreMemory)}, nil
	}
	if err != nil || len(result.JSON) > CoreMemoryBudget || !json.Valid(result.JSON) {
		return CoreSnapshot{}, status_error.DatabaseError
	}
	return result, nil
}

// SetCore 按伙伴权限调整分类，在同一事务中更新条目和核心快照。
func (s *Service) SetCore(ctx context.Context, partnerID, memoryID string, core bool) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	return s.store.withTx(ctx, func(tx *sql.Tx) error {
		memory, err := getAccessibleMemory(tx, memoryID, partnerID)
		if err != nil {
			return err
		}
		control := memory.control
		if memory.Origin == constants.MemoryTimelineSourceReview && control != memoryControlUser {
			control = memoryControlAutomatic
			if core {
				control = memoryControlPinned
			}
		}
		if memory.Core == core && memory.control == control {
			return nil
		}
		changed := memory.Core != core
		memory.Core, memory.control = core, control
		memory.Revision++
		if err := s.store.updateMemory(tx, *memory); err != nil {
			return err
		}
		if changed {
			return updateCoreSnapshot(tx, partnerID)
		}
		return nil
	})
}

// coreDocument 按同一编码规则检查已提交条目和候选替换所需空间。
func coreDocument(tx *sql.Tx, partnerID string, replacement *Memory) ([]byte, error) {
	rows, err := tx.Query(`SELECT id, data FROM partner_memories WHERE partner_id = ? AND core = 1`, partnerID)
	if err != nil {
		return nil, status_error.DatabaseError
	}
	defer rows.Close()
	document := coreMemoryDocument{Items: []coreMemoryItem{}}
	for rows.Next() {
		var item coreMemoryItem
		if err := rows.Scan(&item.ID, &item.Text); err != nil {
			return nil, status_error.DatabaseError
		}
		if replacement == nil || item.ID != replacement.ID {
			document.Items = append(document.Items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, status_error.DatabaseError
	}
	if replacement != nil && replacement.Core {
		document.Items = append(document.Items, coreMemoryItem{ID: replacement.ID, Text: replacement.Data})
	}
	slices.SortFunc(document.Items, func(a, b coreMemoryItem) int { return strings.Compare(a.ID, b.ID) })
	body, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	if len(body) > CoreMemoryBudget {
		return nil, status_error.MemoryCoreBudgetExceeded
	}
	return body, nil
}

func updateCoreSnapshot(tx *sql.Tx, partnerID string) error {
	body, err := coreDocument(tx, partnerID, nil)
	if err != nil {
		return err
	}
	var previous []byte
	err = tx.QueryRow(`SELECT body FROM partner_core_memory WHERE partner_id = ?`, partnerID).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return status_error.DatabaseError
	}
	if bytes.Equal(previous, body) || previous == nil && string(body) == emptyCoreMemory {
		return nil
	}
	if _, err := tx.Exec(
		`INSERT INTO partner_core_memory (partner_id, revision, body) VALUES (?, 1, ?)
		 ON CONFLICT(partner_id) DO UPDATE SET revision = revision + 1, body = excluded.body`, partnerID, body,
	); err != nil {
		return status_error.DatabaseError
	}
	return nil
}
