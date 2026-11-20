package workflow

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// StopTeamWorkflowTurns 撤销原停止范围内的使用权，并为已接纳的运行登记停止。
func (s *Service) StopTeamWorkflowTurns(ctx context.Context, threadID string, chatIDs []string, cancelingAt int64) error {
	if threadID == "" || cancelingAt <= 0 {
		return status_error.BadRequest
	}
	thread, err := s.channelThreads.Get(ctx, threadID)
	if err != nil {
		return err
	}
	if thread.CancelingAt == nil || *thread.CancelingAt != cancelingAt {
		return nil
	}
	mayHaveWork := s.chatTurns.stopTeamTurns(chatIDs)
	if s.chatTurns.teamStopsReady.Load() && s.chatTurns.restored.Load() && !mayHaveWork {
		return nil
	}
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	return s.transactions.Transaction(ctx, func(ctx context.Context) error {
		thread, err := s.channelThreads.Get(ctx, threadID)
		if err != nil {
			return err
		}
		if thread.CancelingAt == nil || *thread.CancelingAt != cancelingAt {
			return nil
		}
		if err := s.consumers.StopThreadConsumers(ctx, threadID, chatIDs, cancelingAt); err != nil {
			return err
		}
		runs, err := s.runs.RequestTeamChatRunStop(ctx, threadID, chatIDs, time.Now().UnixMilli())
		if err != nil {
			return err
		}
		for _, run := range runs {
			if _, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: run.ID, EventID: util.New(), Type: constants.WorkflowRunChangedEvent, EntityID: run.ID,
				Payload: map[string]any{"runId": run.ID, "status": run.Status, "version": run.Version},
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) ensureTeamWorkflowWritable(ctx context.Context, threadID string) error {
	if s.channelThreads == nil {
		return status_error.WorkflowCapabilityUnsupported
	}
	thread, err := s.channelThreads.Get(ctx, threadID)
	if err != nil {
		return err
	}
	if thread.CancelingAt != nil {
		return status_error.ChannelTaskStateConflict
	}
	return nil
}
