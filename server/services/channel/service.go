// Package channel 编排团队对话、成员 Chat、任务板和负责人结果通知。
package channel

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// Service 是团队 API 使用的稳定业务用例集合。
type Service interface {
	// Start 恢复进行中的任务监听并开始接受团队工作。
	Start(ctx context.Context) error
	// Close 停止后台监听，不取消已经由 Execution Core 管理的 Chat 执行。
	Close()
	// AcquireChatInput 让直接成员 Chat 输入共享团队接纳保护，并在手动输入前封闭已结束的自动运行。
	AcquireChatInput(context.Context, string, bool) (context.Context, func(), error)
	// SubmitMessage 统一接纳团队首次发送和既有对话续发。
	SubmitMessage(ctx context.Context, input SubmitMessageInput) (SubmitMessageResult, error)
	// DeleteChannel 永久删除团队、全部团队对话及其成员 Chat。
	DeleteChannel(ctx context.Context, channelID string) error
	// DeleteThread 永久删除一个团队对话及其成员 Chat。
	DeleteThread(ctx context.Context, channelID string, threadID string) error
	// DetachWaker 从全部团队中移除指定伙伴，并在成员不足时级联删除团队。
	DetachWaker(ctx context.Context, wakerID string) error
	// ValidateWakerDetachment 校验伙伴退出全部团队时不会被活跃工作阻止。
	ValidateWakerDetachment(ctx context.Context, wakerID string) error
	// ReadTimeline 读取负责人主 Chat 的公开消息，并包含持久投递的成员结果报告。
	ReadTimeline(ctx context.Context, input TimelineQuery) (TimelinePage, error)
	// CreateTask 创建负责人维护的正式任务，并按入口语义立即或在负责人回合后派发。
	CreateTask(ctx context.Context, input CreateTaskInput) (TaskResult, error)
	// FollowUpTask 把保留真实作者的补充持久化到原任务并在当前尝试结束后续跑。
	FollowUpTask(ctx context.Context, taskID string, input execution.WorkInput) error
	// StopTask 取消单个任务对应的当前 Chat Execution。
	StopTask(ctx context.Context, taskID string) error
	// StopWork 以可恢复 canceling intent 停止一个团队对话的全部当前工作。
	StopWork(ctx context.Context, channelID string, threadID string) error
	// StopWorkForSubmission 只停止指定输入及其已经接纳的工作。
	StopWorkForSubmission(ctx context.Context, channelID, threadID, submissionID string) error
	// StopFirstWork 以团队和首发请求标识停止原负责人占位。
	StopFirstWork(ctx context.Context, channelID, submissionID string) error
	// ReconcileThread 补齐已接纳的首发，再推进任务、负责人输入和结果通知队列。
	ReconcileThread(ctx context.Context, threadID string) error
	// ReconcileThreadPublication 只补交团队首发，供持有原机会的入口核对接纳结果。
	ReconcileThreadPublication(ctx context.Context, threadID string) (models.ChannelThread, error)
	// ListPendingInteractions 聚合负责人和成员 Chat 尚待用户处理的审批与结构化输入。
	ListPendingInteractions(ctx context.Context, threadID string) ([]PendingTeamInteraction, []TeamMemberActivity, error)
	// SyncInteractionEvents 把成员 Chat 的交互事实幂等投影到团队持久事件流。
	SyncInteractionEvents(ctx context.Context, threadID string) error
	// ListEvents 读取团队公开事件，过滤旧交互投影中的提交内容。
	ListEvents(ctx context.Context, threadID string, after string, limit int) ([]models.ChannelEvent, error)
	// SubscribeEvents 衔接 Team 持久事件 backlog、共享成员交互投影和实时 broker。
	SubscribeEvents(ctx context.Context, threadID string, after string) (EventSubscription, error)
}

// Config 声明团队服务需要的稳定依赖。
type Config struct {
	// FreezeWorkflowSelection 固定团队接纳时用户选择的定义和可运行版本。
	FreezeWorkflowSelection func(context.Context, *workinput.WorkflowReference) (*workinput.WorkflowReference, error)
	// StopChatWorkflows 按仍有效的停止代次撤销原成员聊天关联的工作流。
	StopChatWorkflows func(ctx context.Context, threadID string, chatIDs []string, cancelingAt int64) error
	// ManagedFilesDirectory 是 Chat 引用写入和聚合删除共用操作门的受管文件根。
	ManagedFilesDirectory string
	// Commands 保存与任务事务一起提交的 CLI 回执。
	Commands capability.Store
	// AutomaticRun 读取原自动运行的封闭事实，区分自动内部接力与用户新工作。
	AutomaticRun func(context.Context, string) (models.AutomaticRun, error)
	// BeforeUserInput 在新用户输入提交前冻结已结束的自动运行，回调不持有调度全局锁。
	BeforeUserInput func(context.Context, string) error
	// RecordIMLeadInputFailure 在团队失败事务中登记原 IM 会话的待发结果。
	RecordIMLeadInputFailure func(context.Context, models.ChannelThread, models.ChannelLeadInput, string) error
	// Channels 读取团队当前配置。
	Channels controllers.ChannelController
	// Threads 持久化团队对话快照和事件。
	Threads controllers.ChannelThreadController
	// Tasks 持久化任务、尝试、补充和负责人通知。
	Tasks controllers.ChannelTaskController
	// Transactions 提供团队聚合的数据库事务边界。
	Transactions controllers.TransactionController
	// Wakers 读取团队成员面向分派的公开资料投影。
	Wakers controllers.WakerController
	// Chats 读取成员 Chat 事件并执行补偿或聚合删除。
	Chats chat.Service
	// Executions 建立成员 Chat 并运行负责人或执行伙伴工作。
	Executions execution.Service
	// ChatGroups 原子建立团队首发的一人一个成员 Chat 组。
	ChatGroups execution.ChatGroupPreparer
}

// service 是团队业务用例的唯一实现。
type service struct {
	// freezeWorkflowSelection 将已选版本随团队首发或队列 payload 保存。
	freezeWorkflowSelection func(context.Context, *workinput.WorkflowReference) (*workinput.WorkflowReference, error)
	// stopChatWorkflows 同时用于团队显式停止和启动恢复。
	stopChatWorkflows func(context.Context, string, []string, int64) error
	// managedFilesDirectory 标识成员会话引用与删除共用的操作门。
	managedFilesDirectory string
	// commands 持久化任务命令接纳事实。
	commands capability.Store
	// automaticRun 读取原运行的权威封闭事实，不另存工作来源状态。
	automaticRun func(context.Context, string) (models.AutomaticRun, error)
	// beforeUserInput 在新用户输入前封闭已结束的自动运行。
	beforeUserInput func(context.Context, string) error
	// recordIMLeadInputFailure 与队列结算共同提交原 IM 会话的失败交付。
	recordIMLeadInputFailure func(context.Context, models.ChannelThread, models.ChannelLeadInput, string) error
	// channels 读取团队当前配置。
	channels controllers.ChannelController
	// threads 持久化团队对话快照和事件。
	threads controllers.ChannelThreadController
	// tasks 持久化任务、尝试、补充和负责人通知。
	tasks controllers.ChannelTaskController
	// transactions 提供团队聚合的数据库事务边界。
	transactions controllers.TransactionController
	// wakers 读取团队成员面向分派的公开资料投影。
	wakers controllers.WakerController
	// chats 读取成员 Chat 事件并执行补偿或聚合删除。
	chats chat.Service
	// executions 建立成员 Chat 并运行负责人或执行伙伴工作。
	executions execution.Service
	// chatGroups 原子建立团队首发的一人一个成员 Chat 组。
	chatGroups execution.ChatGroupPreparer
	// lifecycleMutex 串行化 Start、启动失败回滚和 Close，避免跨代生命周期互相清理。
	lifecycleMutex sync.Mutex
	// lifecycleContext 控制任务终态和负责人通知后台监听。
	lifecycleContext context.Context
	// lifecycleCancel 停止全部团队后台监听。
	lifecycleCancel context.CancelFunc
	// watchersMutex 保护已启动的任务尝试监听集合。
	watchersMutex sync.Mutex
	// watchers 按尝试标识去重后台终态监听。
	watchers map[string]struct{}
	// closing 与 watcher 注册共用锁，阻止 Close 开始后继续 WaitGroup.Add。
	closing bool
	// interactionMutex 只保护成员 Chat 交互投影游标，不在锁内执行外部 I/O。
	interactionMutex sync.Mutex
	// interactionCursors 保存每个成员 Chat 已经完成持久投影的最大事件序号。
	interactionCursors map[string]int64
	// interactionShards 让同一成员 Chat 的补历史和实时投影串行，不阻塞其他 Team。
	interactionShards [channelInteractionProjectionShardCount]sync.Mutex
	// events 维护 Team 已提交事件的进程内唤醒和共享交互投影生命周期。
	events *channelEventBroker
	// waitGroup 等待团队后台监听退出。
	waitGroup sync.WaitGroup
	// capabilities 维护仅团队负责人执行可用的短期任务工具授权。
	capabilities *channeltool.Registry
	// watcherPolicy 控制持久投递监听的有限重试与补偿节奏。
	watcherPolicy deliveryWatcherPolicy
}

var _ Service = (*service)(nil)

// New 校验依赖并创建尚未启动的团队服务。
func New(config Config) (Service, error) {
	if config.Channels == nil || config.Threads == nil || config.Tasks == nil ||
		config.Transactions == nil || config.Wakers == nil || config.Chats == nil ||
		config.Executions == nil || config.ChatGroups == nil {
		return nil, status_error.CoreChannelInitializationFailed
	}
	return &service{
		freezeWorkflowSelection:  config.FreezeWorkflowSelection,
		stopChatWorkflows:        config.StopChatWorkflows,
		managedFilesDirectory:    config.ManagedFilesDirectory,
		commands:                 config.Commands,
		channels:                 config.Channels,
		beforeUserInput:          config.BeforeUserInput,
		recordIMLeadInputFailure: config.RecordIMLeadInputFailure,
		automaticRun:             config.AutomaticRun,

		threads:            config.Threads,
		tasks:              config.Tasks,
		transactions:       config.Transactions,
		wakers:             config.Wakers,
		chats:              config.Chats,
		executions:         config.Executions,
		chatGroups:         config.ChatGroups,
		watchers:           make(map[string]struct{}),
		interactionCursors: make(map[string]int64),
		events:             newChannelEventBroker(),
		capabilities:       channeltool.NewRegistry(),
		watcherPolicy:      defaultDeliveryWatcherPolicy(),
	}, nil
}

// deliveryWatcherPolicy 是成员任务和负责人投递共用的有限重试策略。
type deliveryWatcherPolicy struct {
	// maximumFailures 是进入持久补偿前允许的连续订阅或提交失败次数。
	maximumFailures int
	// initialDelay 是首次重试前的可取消等待时间。
	initialDelay time.Duration
	// maximumDelay 是指数退避允许达到的最长等待时间。
	maximumDelay time.Duration
}

// defaultDeliveryWatcherPolicy 返回生产环境使用的统一持久投递监听策略。
func defaultDeliveryWatcherPolicy() deliveryWatcherPolicy {
	return deliveryWatcherPolicy{
		maximumFailures: constants.ChannelDeliveryWatcherMaximumConsecutiveFailures,
		initialDelay: time.Duration(
			constants.ChannelAttemptWatcherInitialRetryDelayMilliseconds,
		) * time.Millisecond,
		maximumDelay: time.Duration(
			constants.ChannelAttemptWatcherMaximumRetryDelayMilliseconds,
		) * time.Millisecond,
	}
}

// effectiveWatcherPolicy 用默认值补全包内测试或旧构造路径留下的空策略。
func (s *service) effectiveWatcherPolicy() deliveryWatcherPolicy {
	policy := s.watcherPolicy
	defaults := defaultDeliveryWatcherPolicy()
	if policy.maximumFailures <= 0 {
		policy.maximumFailures = defaults.maximumFailures
	}
	if policy.initialDelay <= 0 {
		policy.initialDelay = defaults.initialDelay
	}
	if policy.maximumDelay < policy.initialDelay {
		policy.maximumDelay = defaults.maximumDelay
	}
	return policy
}

// ensureThreadWritable 以事务内最新对话状态校验团队存在且没有正在进行的停止意图。
func (s *service) ensureThreadWritable(
	ctx context.Context,
	thread models.ChannelThread,
) error {
	current, err := s.threads.Get(ctx, thread.ID)
	if err != nil {
		return err
	}
	if current.CancelingAt != nil {
		return status_error.ChannelTaskStateConflict
	}
	_, err = s.channels.Get(ctx, current.ChannelID)
	return err
}

// Start 恢复团队首发、任务和监听并开始接受团队工作。
func (s *service) Start(ctx context.Context) error {
	s.lifecycleMutex.Lock()
	defer s.lifecycleMutex.Unlock()
	s.watchersMutex.Lock()
	if s.lifecycleCancel != nil {
		s.watchersMutex.Unlock()
		return nil
	}
	s.lifecycleContext, s.lifecycleCancel = context.WithCancel(ctx)
	s.closing = false
	s.watchersMutex.Unlock()
	started := false
	defer func() {
		if started {
			return
		}
		s.shutdownLifecycle()
	}()
	if s.channels != nil && s.wakers != nil {
		if err := s.repairOrphanMembership(ctx); err != nil {
			return err
		}
	}
	initializingThreads, err := s.threads.ListInitializing(ctx)
	if err != nil {
		return err
	}
	for _, thread := range initializingThreads {
		if _, err := s.recoverInitializingThread(ctx, thread, true); err != nil {
			return err
		}
	}
	cancelingThreads, err := s.threads.ListCanceling(ctx)
	if err != nil {
		return err
	}
	for _, thread := range cancelingThreads {
		if stopErr := s.finishStopWork(ctx, thread); stopErr != nil {
			return stopErr
		}
	}
	tasks, err := s.tasks.ListInProgress(ctx)
	if err != nil {
		return err
	}
	recoveryThreads := make(map[string]struct{})
	for _, task := range tasks {
		thread, getThreadErr := s.threads.Get(ctx, task.ThreadID)
		if getThreadErr != nil {
			return getThreadErr
		}
		if writableErr := s.ensureThreadWritable(ctx, thread); writableErr != nil {
			return writableErr
		}
		attempt, attemptErr := s.tasks.GetCurrentAttempt(ctx, task.ID, task.CurrentFence)
		if attemptErr != nil && !hasStatusErrorCode(
			attemptErr,
			status_error.ChannelTaskAttemptNotFound.Code(),
		) {
			return attemptErr
		}
		if hasStatusErrorCode(attemptErr, status_error.ChannelTaskAttemptNotFound.Code()) {
			if updateErr := s.failMissingAttempt(ctx, task); updateErr != nil {
				return updateErr
			}
			recoveryThreads[task.ThreadID] = struct{}{}
			continue
		}
		if attempt.Status == constants.ExecutionStatusPending {
			recovered, recoveryErr := s.recoverReservedAttempt(ctx, thread, task, attempt)
			if recoveryErr != nil {
				return recoveryErr
			}
			if !recovered {
				recoveryThreads[task.ThreadID] = struct{}{}
			}
			continue
		}
		if attempt.ExecutionID == "" {
			if updateErr := s.failInterruptedAttempt(ctx, task, attempt); updateErr != nil {
				return updateErr
			}
			recoveryThreads[task.ThreadID] = struct{}{}
			continue
		}
		s.watchAttempt(attempt, task.ThreadID, "")
		if task.CancelRequestedAt != nil && task.CancelRequestedFence == attempt.Fence {
			s.watchTaskCancellation(attempt)
		}
	}
	pendingTasks, err := s.tasks.ListPending(ctx)
	if err != nil {
		return err
	}
	for _, task := range pendingTasks {
		recoveryThreads[task.ThreadID] = struct{}{}
	}
	now := time.Now().UnixMilli()
	inputThreads, err := s.threads.RecoverLeadInputs(ctx, now)
	if err != nil {
		return err
	}
	notificationThreads, err := s.tasks.RecoverNotifications(ctx, now)
	if err != nil {
		return err
	}
	for _, threadID := range notificationThreads {
		thread, getErr := s.threads.Get(ctx, threadID)
		if getErr != nil {
			return getErr
		}
		if _, eventErr := s.appendEvent(
			ctx,
			thread.ChannelID,
			thread.ID,
			constants.ChannelEventTeamLeaderProcessing,
			leaderProcessingEvent{
				Active:  false,
				AgentID: thread.LeadWakerID,
			},
		); eventErr != nil {
			return eventErr
		}
	}
	pendingThreads := make(map[string]struct{}, len(inputThreads)+len(notificationThreads))
	for threadID := range recoveryThreads {
		pendingThreads[threadID] = struct{}{}
	}
	for _, threadID := range inputThreads {
		pendingThreads[threadID] = struct{}{}
	}
	for _, threadID := range notificationThreads {
		pendingThreads[threadID] = struct{}{}
	}
	for threadID := range pendingThreads {
		if reconcileErr := s.ReconcileThread(ctx, threadID); reconcileErr != nil {
			return reconcileErr
		}
	}
	started = true
	return nil
}

// hasStatusErrorCode 比较带动态参数的稳定状态错误，不依赖 errors.Is 的实例相等语义。
func hasStatusErrorCode(err error, code int64) bool {
	var commonErr statuserror.CommonError
	return errors.As(err, &commonErr) && commonErr.Code() == code
}

// Close 停止后台监听，不取消已经由 Execution Core 管理的 Chat 执行。
func (s *service) Close() {
	s.lifecycleMutex.Lock()
	defer s.lifecycleMutex.Unlock()
	s.shutdownLifecycle()
}

// shutdownLifecycle 停止当前代 watcher 和端点；调用方必须持有 lifecycleMutex。
func (s *service) shutdownLifecycle() {
	s.watchersMutex.Lock()
	if s.lifecycleCancel == nil || s.closing {
		s.watchersMutex.Unlock()
		return
	}
	s.closing = true
	cancel := s.lifecycleCancel
	s.watchersMutex.Unlock()
	cancel()
	if s.events != nil {
		s.events.closeAll()
	}
	s.waitGroup.Wait()
	s.capabilities.Close()
	s.watchersMutex.Lock()
	s.lifecycleCancel = nil
	s.lifecycleContext = nil
	s.closing = false
	s.watchersMutex.Unlock()
}

// registerWatcher 在 closing gate 内完成去重与 WaitGroup.Add，避免 Add/Wait 竞争。
func (s *service) registerWatcher(watcherID string) (context.Context, bool) {
	s.watchersMutex.Lock()
	defer s.watchersMutex.Unlock()
	if s.closing || s.lifecycleContext == nil || watcherID == "" {
		return nil, false
	}
	if _, exists := s.watchers[watcherID]; exists {
		return nil, false
	}
	s.watchers[watcherID] = struct{}{}
	s.waitGroup.Add(1)
	return s.lifecycleContext, true
}

// unregisterWatcher 配对完成 watcher 去重状态和 WaitGroup 生命周期。
func (s *service) unregisterWatcher(watcherID string) {
	s.watchersMutex.Lock()
	delete(s.watchers, watcherID)
	s.watchersMutex.Unlock()
	s.waitGroup.Done()
}
