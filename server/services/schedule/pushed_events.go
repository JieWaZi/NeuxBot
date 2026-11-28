package schedule

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
)

// AcceptPushedEvents 验证推送的来源后提交观察和游标，交付方可在返回成功后确认。
func (s *Service) AcceptPushedEvents(ctx context.Context, wakerID, taskID string, payload []byte, signature string) error {
	if s.config.EventPushAdapter == nil {
		return status_error.AgentOperationForbidden
	}
	ctx, finish, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer finish()
	task, err := s.eventRule(ctx, wakerID, taskID)
	if err != nil {
		return err
	}
	items, err := s.config.EventPushAdapter.Verify(ctx, *task.Event, payload, signature)
	if err != nil {
		return err
	}
	return s.persistPushedEvents(ctx, taskID, eventInstance(*task.Event), wakerID, items)
}

func (s *Service) persistPushedEvents(ctx context.Context, taskID, instance, wakerID string, items []eventsource.Event) error {
	for range 3 {
		task, err := s.eventRule(ctx, wakerID, taskID)
		if err != nil {
			return err
		}
		if eventInstance(*task.Event) != instance {
			return status_error.AutomaticTaskAdmissionRevoked
		}
		cursor, err := s.config.Events.Cursor(ctx, taskID, instance)
		if err != nil {
			return err
		}
		// Push is a verified live observation instead of an existing snapshot.
		cursor.Initialized = true
		cursor.CheckedAt = s.config.Now().UnixMilli()
		cursor.Outcome, cursor.ErrorCode = "observed", ""
		rows, err := s.classifyEvents(ctx, task, cursor, items)
		if err != nil {
			return err
		}
		won, err := s.commitEventCheck(ctx, task, cursor, rows)
		if err != nil || won {
			return err
		}
	}
	return status_error.AutomaticTaskRevisionConflict
}
