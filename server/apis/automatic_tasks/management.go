package automatic_tasks

import (
	"context"
	"fmt"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/teamwork"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// taskAPI 直接编排管理与本机任务入口，调度服务仅提供共享业务和协调能力。
type taskAPI struct {
	// service 协调冻结机会及后台生命周期。
	service *schedule.Service
	// config 提供当前进程装配的持久化与执行边界。
	config schedule.Config
}

func currentTaskAPI() *taskAPI {
	return &taskAPI{service: global.ScheduleService, config: schedule.Config{
		Tasks: global.AutomaticTaskControllerManager, Transactions: global.TransactionControllerManager,
		Wakers: global.WakerControllerManager, Projects: global.ProjectControllerManager,
		Chats: global.ChatService, Workflows: global.WorkflowService, WorkflowRuns: global.WorkflowRunControllerManager,
		Threads: global.ChannelThreadControllerManager, Channels: global.ChannelControllerManager, TeamTasks: global.ChannelTaskControllerManager,
		DataDirectory:         global.Config.DataDirectory,
		ManagedFilesDirectory: global.Config.ManagedFilesDirectory,
	}}
}

func (s *taskAPI) getTask(ctx context.Context, wakerID, id string) (models.AutomaticTask, error) {
	task, err := s.config.Tasks.Get(ctx, id)
	if err == nil && wakerID != "" && task.WakerID != wakerID {
		return models.AutomaticTask{}, status_error.AutomaticTaskNotFound
	}
	return task, err
}

// TriggerManual 接纳桌面立即运行，同键重试保持同一运行且不消耗计划游标。
func (s *taskAPI) TriggerManual(ctx context.Context, wakerID, taskID, key string, args ...map[string]any) (schedule.RunView, error) {
	ctx, release := s.service.AcquireMutation(ctx)
	defer release()
	task, err := s.getTask(ctx, wakerID, taskID)
	if err != nil {
		return schedule.RunView{}, err
	}
	var callArgs map[string]any
	if len(args) > 0 {
		callArgs = args[0]
	}
	if task.WorkflowID == "" && callArgs != nil {
		return schedule.RunView{}, status_error.BadRequest
	}
	return s.service.Admit(ctx, schedule.AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceManual, OccurrenceKey: "manual:" + key, RequestID: key, Args: callArgs, NextRunAt: task.NextRunAt})
}

// TriggerAPI 在当前任务密钥代际中接纳附加文本，鉴权先于幂等查询。
func (s *taskAPI) TriggerAPI(ctx context.Context, taskID, token, key, input string, args ...map[string]any) (schedule.RunView, error) {
	ctx, release := s.service.AcquireMutation(ctx)
	defer release()
	task, err := s.service.AuthenticateTask(ctx, taskID, token)
	if err != nil {
		return schedule.RunView{}, err
	}
	var callArgs map[string]any
	if len(args) > 0 {
		callArgs = args[0]
	}
	return s.service.Admit(ctx, schedule.AdmissionCommand{Task: task, Source: constants.AutomaticRunSourceAPI, OccurrenceKey: fmt.Sprintf("api:%d:%s", task.TriggerVersion, key), RequestID: key, Input: input, Args: callArgs, NextRunAt: task.NextRunAt})
}

// SetEnabled 复用保存逻辑启停后续机会；停用允许用户封闭已失效的配置。
func (s *taskAPI) SetEnabled(ctx context.Context, wakerID, id string, revision int64, enabled bool) error {
	ctx, release := s.service.AcquireMutation(ctx)
	defer release()
	task, err := s.getTask(ctx, wakerID, id)
	if err != nil {
		return err
	}
	config := task.AutomaticTaskConfiguration
	config.Enabled = enabled
	_, err = s.service.SaveConfiguration(ctx, wakerID, id, revision, config, enabled)
	return err
}

// RotateToken 立即撤销旧密钥及旧代际的新接纳，明文只随本次收据返回。
func (s *taskAPI) RotateToken(ctx context.Context, wakerID, id string, revision int64) (schedule.SavedTask, error) {
	ctx, release := s.service.AcquireMutation(ctx)
	defer release()
	task, err := s.getTask(ctx, wakerID, id)
	if err != nil {
		return schedule.SavedTask{}, err
	}
	if task.Trigger != constants.AutomaticTaskTriggerAPI || task.Deleting || task.ConfigurationVersion != revision {
		return schedule.SavedTask{}, status_error.AutomaticTaskRevisionConflict
	}
	token, digest, err := util.NewAutomaticTaskToken()
	if err != nil {
		return schedule.SavedTask{}, err
	}
	storedRevision := task.Revision
	task.TokenHash = digest
	task.TriggerVersion++
	task.ConfigurationVersion++
	task.Revision++
	if err := s.config.Tasks.UpdateConfiguration(ctx, task, storedRevision); err != nil {
		return schedule.SavedTask{}, err
	}
	return schedule.SavedTask{TaskID: id, Token: token}, nil
}

// Delete 封闭新触发，并通过 Chat 的统一 staging 事务删除任务完整聚合。
func (s *taskAPI) Delete(ctx context.Context, wakerID, id string) error {
	ctx, release := s.service.AcquireMutation(ctx)
	defer release()
	if _, err := s.getTask(ctx, wakerID, id); err != nil {
		return err
	}
	if err := s.service.WithAuthorizationWrite(ctx, func(ctx context.Context) error { return s.config.Tasks.SetDeleting(ctx, id, true) }); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.service.WithAuthorizationWrite(context.WithoutCancel(ctx), func(ctx context.Context) error { return s.config.Tasks.SetDeleting(ctx, id, false) })
		}
	}()
	runs, _, err := s.config.Tasks.ListRuns(ctx, id, 0, 0)
	if err != nil {
		return err
	}
	channelIDs := map[string]bool{}
	for _, run := range runs {
		if run.Configuration.ChannelID != "" {
			channelIDs[run.Configuration.ChannelID] = true
		}
	}
	ordered := make([]string, 0, len(channelIDs))
	for id := range channelIDs {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		locked, release, lockErr := teamwork.Acquire(ctx, id)
		if lockErr != nil {
			return lockErr
		}
		ctx = locked
		defer release()
	}
	chatIDs := []string{}
	threadIDs := []string{}
	for _, run := range runs {
		if err := s.service.ReconcileRun(ctx, &run); err != nil {
			return err
		}
		if run.WorkflowRunID != "" {
			view, err := s.service.ReadRun(ctx, run)
			if err != nil {
				return err
			}
			if schedule.Unfinished(view.State) {
				return status_error.AutomaticTaskBusy
			}
			continue
		}
		if run.ChatID == "" {
			continue
		}
		if run.ThreadID != "" {
			view, err := s.service.ReadRun(ctx, run)
			if err != nil {
				return err
			}
			if schedule.Unfinished(view.State) {
				return status_error.AutomaticTaskBusy
			}
			active, err := s.config.Threads.CountActiveWork(ctx, run.ThreadID)
			if err != nil {
				return err
			}
			if active > 0 {
				return status_error.AutomaticTaskBusy
			}
			ids, err := s.config.Threads.ListChatIDs(ctx, run.ThreadID)
			if err != nil {
				return err
			}
			chatIDs = append(chatIDs, ids...)
			threadIDs = append(threadIDs, run.ThreadID)
			continue
		}
		state, err := s.config.Chats.ReadInitialExecution(ctx, run.ChatID)
		if util.HasStatusCode(err, status_error.ChatNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if state.Chat.ActiveExecutionID != "" {
			return status_error.AutomaticTaskBusy.WithParams(map[string]interface{}{"ChatID": run.ChatID})
		}
		chatIDs = append(chatIDs, run.ChatID)
	}
	finalize := func(tx context.Context) error {
		for _, threadID := range threadIDs {
			if err := s.config.Threads.Delete(tx, threadID); err != nil {
				return err
			}
		}
		return s.config.Tasks.Delete(tx, id)
	}
	// 最终删除必须先进入授权屏障，再获取数据库写锁；同一 Context 让任务仓库重入，
	// 避免 Chat 索引已写入后才等待撤权锁，与 Key 更新形成反向锁序。
	err = s.service.WithAuthorizationWrite(ctx, func(ctx context.Context) error {
		if len(chatIDs) == 0 {
			return s.config.Transactions.Transaction(ctx, finalize)
		}
		return s.config.Chats.DeleteChatsAtomically(ctx, chatIDs, finalize)
	})
	committed = err == nil
	if committed {
		s.service.ForgetTask(ctx, id)
	}
	return err
}

// Runs 按任务归属分页返回运行历史。
func (s *taskAPI) Runs(ctx context.Context, wakerID, id string, limit, offset int) ([]schedule.RunView, int64, error) {
	if _, err := s.getTask(ctx, wakerID, id); err != nil {
		return nil, 0, err
	}
	runs, total, err := s.config.Tasks.ListRuns(ctx, id, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	views := make([]schedule.RunView, 0, len(runs))
	for _, run := range runs {
		view, err := s.service.ReadRun(ctx, run)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, view)
	}
	return views, total, nil
}

// GetAttention 无分页上限地列出一位伙伴当前需要跟踪的自动运行。
func (s *taskAPI) GetAttention(ctx context.Context, wakerID string) (Attention, error) {
	if _, err := s.config.Wakers.Get(ctx, wakerID); err != nil {
		return Attention{}, err
	}
	tasks, _, err := s.config.Tasks.List(ctx, controllers.AutomaticTaskFilter{WakerID: wakerID})
	if err != nil {
		return Attention{}, err
	}
	result := Attention{Runs: []schedule.RunView{}, ActiveChats: []models.Chat{}}
	seenChats := map[string]bool{}
	states := map[string]chat.InitialExecutionState{}
	for _, task := range tasks {
		var run models.AutomaticRun
		if task.ActiveRunID != "" {
			run, err = s.config.Tasks.GetRun(ctx, task.ActiveRunID)
		} else {
			var runs []models.AutomaticRun
			runs, _, err = s.config.Tasks.ListRuns(ctx, task.ID, 1, 0)
			if len(runs) > 0 {
				run = runs[0]
			}
		}
		if err != nil {
			return Attention{}, err
		}
		if run.ID == "" {
			continue
		}
		if err := s.service.ReconcileRun(ctx, &run); err != nil {
			return Attention{}, err
		}
		view, err := s.service.ReadRunWithSnapshots(ctx, run, states)
		if err != nil {
			return Attention{}, err
		}
		if schedule.Unfinished(view.State) || view.State == constants.AutomaticRunStateFailed || view.State == constants.AutomaticRunStateInterrupted {
			result.Runs = append(result.Runs, view)
		}
		chatIDs := []string{run.ChatID}
		if run.ThreadID != "" {
			chatIDs, err = s.config.Threads.ListChatIDs(ctx, run.ThreadID)
			if err != nil {
				return Attention{}, err
			}
		}
		for _, chatID := range chatIDs {
			if chatID == "" || seenChats[chatID] {
				continue
			}
			seenChats[chatID] = true
			state, err := s.service.ReadExecutionSnapshot(ctx, chatID, states)
			if err != nil {
				if util.HasStatusCode(err, status_error.ChatNotFound) {
					continue
				}
				return Attention{}, err
			}
			if state.Chat.ActiveExecutionID != "" {
				result.ActiveChats = append(result.ActiveChats, state.Chat)
			}
		}
	}
	return result, nil
}

// 待处理自动运行
type Attention struct {
	// 待处理运行
	Runs []schedule.RunView `json:"runs"`
	// 活跃会话
	ActiveChats []models.Chat `json:"activeChats"`
}

// 自动运行状态
type PublicRun struct {
	// 运行标识
	RunID string `json:"runId"`
	// 运行状态
	State constants.AutomaticRunState `json:"state"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
	// 完成时间
	CompletedAt *int64 `json:"completedAt"`
	// 错误码
	ErrorCode string `json:"errorCode,omitempty"`
}

func publicRun(view schedule.RunView) PublicRun {
	return PublicRun{RunID: view.ID, State: view.State, CreatedAt: view.CreatedAt, CompletedAt: view.CompletedAt, ErrorCode: view.ErrorCode}
}

// ReadAPIState 验证当前任务密钥并仅返回该任务运行的安全状态。
func (s *taskAPI) ReadAPIState(ctx context.Context, taskID, token, runID string) (PublicRun, error) {
	// 鉴权只在读取接纳封闭标记时持有短锁；权威日志和团队投影不占用全局接纳锁。
	_, authErr := s.service.AuthenticateTask(ctx, taskID, token)
	if authErr != nil {
		return PublicRun{}, authErr
	}
	run, err := s.config.Tasks.GetRun(ctx, runID)
	if err != nil {
		return PublicRun{}, err
	}
	if run.TaskID != taskID {
		return PublicRun{}, status_error.AutomaticRunNotFound
	}
	return s.readRunState(ctx, run)
}

func (s *taskAPI) authenticatedWorkflowRun(ctx context.Context, taskID, token, runID string) (models.AutomaticRun, error) {
	_, err := s.service.AuthenticateTask(ctx, taskID, token)
	if err != nil {
		return models.AutomaticRun{}, err
	}
	run, err := s.config.Tasks.GetRun(ctx, runID)
	if err != nil {
		return run, err
	}
	if run.TaskID != taskID || run.Configuration.WorkflowID == "" || s.config.Workflows == nil {
		return run, status_error.AutomaticRunNotFound
	}
	if err := s.service.ReconcileRun(ctx, &run); err != nil {
		return run, err
	}
	return run, nil
}

// ReadAPIResult 在任务认证和运行归属检查后读取结构化结果，轮询不触发新执行。
func (s *taskAPI) ReadAPIResult(ctx context.Context, taskID, token, runID string) (PublicWorkflowResult, error) {
	run, err := s.authenticatedWorkflowRun(ctx, taskID, token, runID)
	if err != nil {
		return PublicWorkflowResult{}, err
	}
	return s.workflowResult(ctx, run)
}

// 工作流运行结果
type PublicWorkflowResult struct {
	// 运行状态
	PublicRun
	// 运行结果
	Result any `json:"result"`
	// 结果文件
	Files []workflow.OutputFile `json:"files"`
}

func (s *taskAPI) readRunState(ctx context.Context, run models.AutomaticRun) (PublicRun, error) {
	if err := s.service.ReconcileRun(ctx, &run); err != nil {
		return PublicRun{}, err
	}
	view, err := s.service.ReadRun(ctx, run)
	return publicRun(view), err
}

func (s *taskAPI) workflowResult(ctx context.Context, run models.AutomaticRun) (PublicWorkflowResult, error) {
	view, err := s.service.ReadRun(ctx, run)
	if err != nil {
		return PublicWorkflowResult{}, err
	}
	response := PublicWorkflowResult{PublicRun: publicRun(view), Files: []workflow.OutputFile{}}
	if run.WorkflowRunID == "" {
		return response, nil
	}
	actual, err := s.config.WorkflowRuns.GetRun(ctx, run.WorkflowRunID)
	if err != nil {
		return response, err
	}
	if actual.Status == constants.WorkflowRunStatusSucceeded {
		response.Result = actual.Result
		response.Files, err = s.config.Workflows.OutputFiles(ctx, actual)
	}
	return response, err
}
