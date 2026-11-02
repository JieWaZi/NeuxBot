package chat

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// locateExecution 追平定位索引并按原身份读取准确事实位置及已覆盖游标。
func (m *service) locateExecution(ctx context.Context, chatID, executionID, submissionID string) (*models.ChatExecutionLocation, int64, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, chatContextStatus(err)
		}
		recovery, err := m.logStore.RecoverIndex(chatID)
		if err != nil {
			return nil, 0, err
		}
		cursor, err := m.queryIndexes.chatController.ReadExecutionLocationCursor(ctx, chatID)
		if err != nil {
			return nil, 0, err
		}
		covered := recovery.LatestSequence
		var tailLocation *models.ChatExecutionLocation
		authoritativeTailRead := false
		indexWriteFailed := false
		if cursor != covered {
			from := cursor
			if from > covered {
				from = 0
			}
			tail, err := m.logStore.ReadExecutionIndexTail(chatID, from)
			if err != nil {
				return nil, 0, err
			}
			covered = tail.LatestSequence
			authoritativeTailRead = true
			locations := make([]models.ChatExecutionLocation, 0, len(tail.Records))
			byStart := map[int64]int{}
			for _, record := range tail.Records {
				location := models.ChatExecutionLocation{ChatID: chatID, ExecutionID: record.ExecutionID, Sequence: record.Sequence}
				if record.Type == constants.ChatEventTypeMessageInherited || record.Type == constants.ChatEventTypeUserMessageCreated {
					location.ExecutionID, location.MessageOnly = record.MessageID, true
				} else {
					var created eventlog.ExecutionCreatedData
					if json.Unmarshal(record.Data, &created) != nil {
						return nil, 0, status_error.ChatLogCorrupted
					}
					location.SubmissionID = created.SubmissionID
					byStart[record.Sequence] = len(locations)
				}
				locations = append(locations, location)
			}
			for _, position := range tail.Positions {
				i, ok := byStart[position.ExecutionStartSequence]
				if !ok {
					return nil, 0, status_error.ChatLogIndexCorrupted
				}
				location := &locations[i]
				if position.Lifecycle {
					location.LifecycleSequences = append(location.LifecycleSequences, position.Sequence)
				}
				if position.InvocationBoundary {
					location.InvocationBoundarySequence = position.Sequence
				}
				if position.InvocationFinished {
					location.InvocationFinishedSequence = position.Sequence
					location.FinishedInvocationBoundarySequence = location.InvocationBoundarySequence
				}
			}
			for _, location := range locations {
				matches := (executionID != "" && location.ExecutionID == executionID) || (submissionID != "" && location.SubmissionID == submissionID) || (executionID == "" && submissionID == "" && !location.MessageOnly)
				if matches && (tailLocation == nil || location.Sequence < tailLocation.Sequence) {
					copy := location
					tailLocation = &copy
				}
			}
			err = m.queryIndexes.transactionController.Transaction(ctx, func(tx context.Context) error {
				return m.queryIndexes.chatController.AdvanceExecutionLocations(tx, chatID, cursor, covered, locations)
			})
			if hasStatusErrorCode(err, status_error.Conflict.Code()) {
				continue
			}
			if err != nil {
				indexWriteFailed = true
				logx.Errorf("index chat execution locations failed: chat_id=%s error=%v", chatID, err)
			} else {
				tailLocation = nil
			}
		}
		var location *models.ChatExecutionLocation
		var snapshotCursor int64
		err = m.queryIndexes.transactionController.Transaction(ctx, func(tx context.Context) error {
			var readErr error
			snapshotCursor, readErr = m.queryIndexes.chatController.ReadExecutionLocationCursor(tx, chatID)
			if readErr != nil {
				return readErr
			}
			location, readErr = m.queryIndexes.chatController.ReadExecutionLocation(tx, chatID, executionID, submissionID)
			return readErr
		})
		if err != nil {
			if !authoritativeTailRead {
				return nil, 0, err
			}
			// 投影写回不可用时仍可使用本次已读权威尾部；再次核对游标，避免合并竞争快照。
			location, err = m.queryIndexes.chatController.ReadExecutionLocation(ctx, chatID, executionID, submissionID)
			if err != nil {
				return nil, 0, err
			}
			snapshotCursor, err = m.queryIndexes.chatController.ReadExecutionLocationCursor(ctx, chatID)
			if err != nil {
				return nil, 0, err
			}
		}
		if !indexWriteFailed && snapshotCursor != covered {
			continue
		}
		if indexWriteFailed && snapshotCursor != cursor && snapshotCursor != covered {
			continue
		}

		if tailLocation != nil {
			if location != nil && location.Sequence == tailLocation.Sequence && cursor > 0 && cursor <= covered {
				merged := models.MergeExecutionLocationTail(location, *tailLocation)
				location = &merged
			} else if location == nil || tailLocation.Sequence <= location.Sequence {
				location = tailLocation
			}
		}
		return location, covered, nil
	}
}

// readExecutionWindow 只读取已覆盖的生命周期位置和最后一次调用结论。
func (m *service) readExecutionWindow(ctx context.Context, chatID, executionID, submissionID string) (eventlog.ExecutionWindow, error) {
	location, covered, err := m.locateExecution(ctx, chatID, executionID, submissionID)
	if err != nil {
		return eventlog.ExecutionWindow{}, err
	}
	positions := eventlog.ExecutionFactPositions{LatestSequence: covered}
	if location != nil && !location.MessageOnly {
		positions.StartSequence = location.Sequence
		positions.LifecycleSequences = location.LifecycleSequences
		positions.InvocationFinishedSequence = location.InvocationFinishedSequence
		positions.InvocationBoundarySequence = location.FinishedInvocationBoundarySequence
	}
	return m.logStore.ReadExecutionFacts(chatID, positions)
}
