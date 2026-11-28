package schedule

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (s *Service) queueReportRun(ctx context.Context, task models.AutomaticTask, run models.AutomaticRun, next *int64) (RunView, error) {
	run.AwaitingDispatch = run.Admission == constants.AutomaticRunAdmissionPending
	task.NextRunAt = next
	err := s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
		advanced, err := s.config.Tasks.AdvanceSchedule(tx, task, task.Revision)
		if err != nil {
			return err
		}
		if !advanced {
			return status_error.AutomaticTaskRevisionConflict
		}
		return s.config.Tasks.CreateRun(tx, run)
	})
	if err != nil {
		return RunView{}, err
	}
	return s.ReadRun(ctx, run)
}

func (s *Service) dispatchQueuedReportRuns(ctx context.Context) {
	const batch = 100
	for afterTime, afterID := int64(-1), ""; ctx.Err() == nil; {
		runs, err := s.config.Tasks.QueuedReportRuns(ctx, afterTime, afterID, batch)
		if err != nil {
			logx.Warnf("read queued automatic report runs failed: error=%v", err)
			return
		}
		for _, run := range runs {
			afterTime, afterID = run.CreatedAt, run.ID
			if err := s.dispatchQueuedReportRun(ctx, run); err != nil {
				logx.Warnf("dispatch queued automatic report run failed: run_id=%s error_code=%s", run.ID, util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key()))
			}
		}
		if len(runs) < batch {
			return
		}
	}
}

func (s *Service) dispatchQueuedReportRun(ctx context.Context, run models.AutomaticRun) error {
	task, err := s.config.Tasks.Get(ctx, run.TaskID)
	if err != nil {
		return err
	}
	if task.Deleting || s.sealedWakers[task.WakerID] {
		return nil
	}
	if !task.Enabled || task.ConfigurationVersion != run.ConfigurationRevision {
		return s.config.Transactions.Transaction(ctx, func(tx context.Context) error {
			if err := s.interrupt(tx, &run); err != nil {
				return err
			}
			return s.config.Tasks.ClearReportRunQueue(tx, run.ID)
		})
	}
	_, err = s.Admit(ctx, AdmissionCommand{Task: task, Source: run.Source, OccurrenceKey: run.OccurrenceKey,
		RequestID: run.ID, Input: run.Input, ScheduledAt: run.ScheduledAt, NextRunAt: task.NextRunAt})
	return err
}
