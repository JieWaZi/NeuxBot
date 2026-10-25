package schedule

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// TaskView 展示配置和运行概览，不暴露密钥摘要。
type TaskView struct {
	// WorkflowInputContract 是任务参数来源派生的只读调用说明。
	WorkflowInputContract *workflowinput.Contract `json:"workflowInputContract,omitempty"`
	// ExecutorName 是当前伙伴或团队的产品展示名称。
	ExecutorName string `json:"executorName"`
	// AutomaticTask 是任务配置与计划游标。
	models.AutomaticTask
	// RunCount 是已经进入 Chat 的运行次数。
	RunCount int64 `json:"runCount"`
	// DeletionRunCount 是删除任务时将一并移除的运行记录总数。
	DeletionRunCount int64 `json:"deletionRunCount"`
	// DeletionChatCount 是删除时将移除的关联会话数，团队运行按 Thread 展开。
	DeletionChatCount int64 `json:"deletionChatCount"`
	// LastRun 是最近一次机会的展示状态。
	LastRun *RunView `json:"lastRun"`
	// ActiveRun 是当前自动槽关联的运行，等待用户操作时可直接进入 Chat。
	ActiveRun *RunView `json:"activeRun"`
	// ConfigurationError 是当前依赖失效的安全原因，便于禁用运行并提示修复。
	ConfigurationError string `json:"configurationError,omitempty"`
}

// List 返回全局或指定伙伴的任务分页与未受筛选影响的概览统计。
func (s *Service) List(ctx context.Context, filter controllers.AutomaticTaskFilter) ([]TaskView, int64, controllers.AutomaticTaskCounts, error) {
	if filter.WakerID != "" {
		if _, err := s.config.Wakers.Get(ctx, filter.WakerID); err != nil {
			return nil, 0, controllers.AutomaticTaskCounts{}, err
		}
	}
	tasks, total, err := s.config.Tasks.List(ctx, filter)
	if err != nil {
		return nil, 0, controllers.AutomaticTaskCounts{}, err
	}
	counts, err := s.config.Tasks.Counts(ctx, filter.WakerID)
	if err != nil {
		return nil, 0, counts, err
	}
	views := make([]TaskView, 0, len(tasks))
	for _, task := range tasks {
		view, err := s.taskView(ctx, task)
		if err != nil {
			return nil, 0, counts, err
		}
		views = append(views, view)
	}
	return views, total, counts, nil
}

// Get 返回任务配置与当前运行概览；指定伙伴时同时校验归属。
func (s *Service) Get(ctx context.Context, wakerID, id string) (TaskView, error) {
	task, err := s.getTask(ctx, wakerID, id)
	if err != nil {
		return TaskView{}, err
	}
	return s.taskView(ctx, task)
}

func (s *Service) taskView(ctx context.Context, task models.AutomaticTask) (TaskView, error) {
	if err := s.refreshSlot(ctx, task); err != nil {
		return TaskView{}, err
	}
	task, err := s.config.Tasks.Get(ctx, task.ID)
	if err != nil {
		return TaskView{}, err
	}
	view := TaskView{AutomaticTask: task}
	if task.WorkflowID != "" && s.config.Workflows != nil {
		if definition, revision, err := s.config.Workflows.ReadTarget(ctx, task.WorkflowID, task.WorkflowInputRevisionID); err == nil {
			view.ExecutorName = definition.Title
			inputConfig := task.AutomaticTaskConfiguration
			if selected, err := s.resolveWorkflowConfiguration(ctx, task.WakerID, &inputConfig); err == nil {
				revision = selected
			}
			if contract, err := workflowinput.Analyze(revision.Manifest.Meta.InputSchema, task.WorkflowInputBindings, task.Trigger == constants.AutomaticTaskTriggerAPI, revision.Manifest.InputPropertyOrder[""]); err == nil {
				view.WorkflowInputContract = &contract
			}
		}
	} else if task.ChannelID != "" && s.config.Channels != nil {
		if team, err := s.config.Channels.Get(ctx, task.ChannelID); err == nil {
			view.ExecutorName = team.Name
		}
	} else if waker, err := s.config.Wakers.Get(ctx, task.WakerID); err == nil {
		view.ExecutorName = waker.Name
	}
	config := task.AutomaticTaskConfiguration
	if err := s.validateConfiguration(ctx, task.WakerID, &config); err != nil {
		view.ConfigurationError = util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key())
	}
	view.RunCount, err = s.config.Tasks.CountAcceptedRuns(ctx, task.ID)
	if err != nil {
		return TaskView{}, err
	}
	runs, _, err := s.config.Tasks.ListRuns(ctx, task.ID, 1, 0)
	if err != nil {
		return TaskView{}, err
	}
	if len(runs) > 0 {
		last, err := s.ReadRun(ctx, runs[0])
		if err != nil {
			return TaskView{}, err
		}
		view.LastRun = &last
	}
	if task.ActiveRunID != "" {
		run, err := s.config.Tasks.GetRun(ctx, task.ActiveRunID)
		if err != nil {
			return TaskView{}, err
		}
		active, err := s.ReadRun(ctx, run)
		if err != nil {
			return TaskView{}, err
		}
		view.ActiveRun = &active
	}
	view.DeletionRunCount, view.DeletionChatCount, err = s.deletionImpact(ctx, task.ID)
	if err != nil {
		return TaskView{}, err
	}
	return view, nil
}

// deletionImpact 统计删除任务将移除的运行记录与关联会话数量，供确认文案展示。
func (s *Service) deletionImpact(ctx context.Context, taskID string) (runRecords int64, linkedChats int64, err error) {
	runs, total, err := s.config.Tasks.ListRuns(ctx, taskID, 0, 0)
	if err != nil {
		return 0, 0, err
	}
	seenChats := map[string]bool{}
	for _, run := range runs {
		if run.ChatID == "" {
			continue
		}
		if run.ThreadID != "" {
			ids, listErr := s.config.Threads.ListChatIDs(ctx, run.ThreadID)
			if listErr != nil {
				return 0, 0, listErr
			}
			for _, chatID := range ids {
				if chatID != "" {
					seenChats[chatID] = true
				}
			}
			continue
		}
		seenChats[run.ChatID] = true
	}
	return total, int64(len(seenChats)), nil
}
