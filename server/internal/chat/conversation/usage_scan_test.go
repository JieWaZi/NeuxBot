package conversation

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestUsageTerminalScanSeeksSparsePositions(t *testing.T) {
	s := openTestStore(t, t.TempDir())
	var plan []struct {
		// Detail is the SQLite planner's access description.
		Detail string
	}
	err := s.indexes.with(t.Context(), func(db *gorm.DB) error {
		return db.Raw("EXPLAIN QUERY PLAN "+usageTerminalQuery, 1, 4, 400, indexReadBatch).Scan(&plan).Error
	})
	if err != nil || len(plan) == 0 || !strings.Contains(plan[0].Detail, "conversation_usage_terminals") {
		t.Fatalf("canonical usage scan plan = %+v, %v", plan, err)
	}
	t.Log(plan[0].Detail)
}
