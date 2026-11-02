package chat

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// countingLocationWrites 记录真实仓储拒写后的重试次数，不替代 SQLite 读写行为。
type countingLocationWrites struct {
	// ChatController 是真实临时数据库仓储。
	controllers.ChatController
	// attempts 是定位写回尝试次数。
	attempts atomic.Int64
}

func (c *countingLocationWrites) AdvanceExecutionLocations(ctx context.Context, id string, after, latest int64, locations []models.ChatExecutionLocation) error {
	c.attempts.Add(1)
	return c.ChatController.AdvanceExecutionLocations(ctx, id, after, latest, locations)
}

// TestExecutionLocationWriteOnlyFailureReturnsMissingAndOlderFacts 验证只拒写时仍返回已读权威尾部的空匹配和旧执行。
func TestExecutionLocationWriteOnlyFailureReturnsMissingAndOlderFacts(t *testing.T) {
	db, m := newRecoveryTestService(t, t.TempDir())
	id := "93939393939393939393939393939396"
	if _, err := m.logStore.Create(id, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{})}, {Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "old", Data: marshalData(eventlog.ExecutionCreatedData{})}, {Type: constants.ChatEventTypeExecutionCompleted, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadExecutionOutcome(context.Background(), id, "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.logStore.Append(id, []eventlog.AppendInput{{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "tail-other", Data: marshalData(eventlog.ExecutionCreatedData{})}, {Type: constants.ChatEventTypeExecutionCompleted, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_location_updates BEFORE UPDATE ON chat_execution_location_cursors BEGIN SELECT RAISE(ABORT, 'fixture writes rejected'); END").Error; err != nil {
		t.Fatal(err)
	}
	observed := &countingLocationWrites{ChatController: m.queryIndexes.chatController}
	m.queryIndexes.chatController = observed
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	missing, err := m.ReadSubmission(ctx, id, "new-submission")
	if err != nil || missing != nil || observed.attempts.Load() != 1 {
		t.Fatalf("write-only miss=%+v err=%v writes=%d", missing, err, observed.attempts.Load())
	}
	before := observed.attempts.Load()
	old, err := m.ReadExecutionOutcome(ctx, id, "old")
	if err != nil || old.Execution.Status != constants.ExecutionStatusCompleted || old.LatestSequence != 5 || observed.attempts.Load()-before != 1 {
		t.Fatalf("write-only old=%+v err=%v writes=%d", old, err, observed.attempts.Load()-before)
	}
}
