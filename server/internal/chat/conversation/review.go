package conversation

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"gorm.io/gorm"
)

// ReviewStart is the admission metadata and corresponding durable successful terminal position.
type ReviewStart struct {
	// Admission contains only the execution creation fact.
	Admission agent.ConversationRecord
	// Terminal is the successful terminal cursor used for restart and batch identity.
	Terminal int64
	// CompletedAt is the original terminal timestamp in Unix milliseconds.
	CompletedAt int64
}

// ReadReviewStarts reads only indexed success positions and review-tagged creation facts.
func (s *Store) ReadReviewStarts(ctx context.Context, chatID string, after, through int64, limit int) ([]ReviewStart, int64, error) {
	if limit < 1 || limit > maxPageSize {
		return nil, after, status_error.ChatEventCursorInvalid
	}
	reader, err := s.openChat(ctx, chatID)
	if err != nil {
		return nil, after, err
	}
	defer reader.close()
	var terminals []rowIndex
	err = reader.index.query(func(db *gorm.DB) error {
		query := db.Where("stream = ? AND cursor > ? AND kind = "+strconv.Itoa(int(indexExecutionCompleted)), reader.index.coverage.Stream, after)
		if through > 0 {
			query = query.Where("cursor <= ?", through)
		}
		return query.Order("cursor").Limit(limit).Find(&terminals).Error
	})
	if err != nil {
		return nil, after, err
	}
	refs := make([]int64, 0, len(terminals))
	for _, terminal := range terminals {
		if terminal.ExecutionRef > 0 {
			refs = append(refs, terminal.ExecutionRef)
		}
	}
	positions, err := reader.index.positions(refs)
	if err != nil {
		return nil, after, err
	}
	results := make([]ReviewStart, 0, len(positions))
	for _, terminal := range terminals {
		position := positions[terminal.ExecutionRef]
		if position.Kind != indexExecutionCreated || !position.ReviewAdmission {
			continue
		}
		record, err := reader.record(ctx, position)
		if err != nil {
			return nil, after, err
		}
		results = append(results, ReviewStart{Admission: record, Terminal: terminal.Cursor, CompletedAt: terminal.At})
	}
	next := int64(0)
	if len(terminals) == limit {
		next = terminals[len(terminals)-1].Cursor
	}
	return results, next, nil
}

// ListReviewChatIDs recovers review-tagged streams after Chat reconciles its authority.
func (s *Store) ListReviewChatIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := s.indexes.with(ctx, func(db *gorm.DB) error {
		var streams []int64
		if err := db.Model(&rowIndex{}).Where("review_admission = 1").Distinct("stream").Pluck("stream", &streams).Error; err != nil {
			return err
		}
		for len(streams) > 0 {
			size := min(indexReadBatch, len(streams))
			var paths []string
			if err := db.Model(&streamCoverage{}).Where("stream IN ?", streams[:size]).Pluck("path", &paths).Error; err != nil {
				return err
			}
			for _, path := range paths {
				id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
				if validID(id) {
					ids = append(ids, id)
				}
			}
			streams = streams[size:]
		}
		return nil
	})
	return ids, err
}
