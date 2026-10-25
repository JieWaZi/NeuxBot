package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// ChannelTaskController 定义团队任务板、尝试、补充和负责人通知的持久化操作。
type ChannelTaskController interface {
	// Create 创建任务主体及其依赖关系。
	Create(ctx context.Context, task models.ChannelTask, dependencies []models.ChannelTaskDependency) error
	// Get 按标识返回团队任务。
	Get(ctx context.Context, id string) (models.ChannelTask, error)
	// ListByThread 按创建顺序返回团队对话的全部任务。
	ListByThread(ctx context.Context, threadID string) ([]models.ChannelTask, error)
	// ListInProgress 返回全部正在等待 Chat Execution 终态的任务。
	ListInProgress(ctx context.Context) ([]models.ChannelTask, error)
	// ListPending 返回全部等待调度的团队任务。
	ListPending(ctx context.Context) ([]models.ChannelTask, error)
	// CountByThread 返回团队对话当前任务总数。
	CountByThread(ctx context.Context, threadID string) (int64, error)
	// LoadDispatchSnapshot 批量读取一次团队调度所需的任务、依赖和活跃配额事实。
	LoadDispatchSnapshot(ctx context.Context, threadID string) (ChannelDispatchSnapshot, error)
	// UpdateFields 更新任务服务已经校验过的明确字段集合。
	UpdateFields(ctx context.Context, taskID string, fields map[string]interface{}) error
	// ClaimForDispatch 按旧状态和围栏原子领取一项待派发任务。
	ClaimForDispatch(ctx context.Context, taskID string, threadID string, assigneeWakerID string, expectedFence int, newFence int, maxParallel int, startedAt int64) (bool, error)
	// GetActiveForAssignee 定向返回成员当前运行任务；没有时 exists 为 false。
	GetActiveForAssignee(ctx context.Context, threadID string, wakerID string) (task models.ChannelTask, exists bool, err error)
	// CreateAttempt 创建一次新任务尝试。
	CreateAttempt(ctx context.Context, attempt models.ChannelTaskAttempt) error
	// UpdateAttemptFields 更新任务尝试的执行映射或终态结果。
	UpdateAttemptFields(ctx context.Context, attemptID string, fields map[string]interface{}) error
	// BindAttemptExecution 把外部已接纳 Execution 原子绑定到未绑定尝试。
	BindAttemptExecution(ctx context.Context, attemptID string, executionID string, startedAt int64) (bool, error)
	// ReleaseDispatchClaim 把未完成启动的尝试终结并按围栏重新排队任务。
	ReleaseDispatchClaim(ctx context.Context, taskID string, attemptID string, fence int, status constants.ExecutionStatus, errorMessage string, updatedAt int64) (bool, error)
	// FailRunningAttempt 在调用方事务内按当前围栏把无法继续监听的运行尝试和任务提交为失败。
	FailRunningAttempt(ctx context.Context, taskID string, attemptID string, fence int, errorMessage string, updatedAt int64) (bool, error)
	// GetCurrentAttempt 返回与任务当前围栏匹配的尝试。
	GetCurrentAttempt(ctx context.Context, taskID string, fence int) (models.ChannelTaskAttempt, error)
	// CountFailureAttempts 返回任务已经失败的尝试累计数量。
	CountFailureAttempts(ctx context.Context, taskID string) (int64, error)
	// CreateFollowUp 持久化一条等待续跑的用户补充。
	CreateFollowUp(ctx context.Context, followUp models.ChannelTaskFollowUp) error
	// ListPendingFollowUps 按创建顺序返回任务尚未交付的用户补充。
	ListPendingFollowUps(ctx context.Context, taskID string) ([]models.ChannelTaskFollowUp, error)
	// ListFollowUps 按创建顺序返回任务的全部补充，用于稳定公开负责人协调消息。
	ListFollowUps(ctx context.Context, taskID string) ([]models.ChannelTaskFollowUp, error)
	// MarkFollowUpsDelivered 把一批用户补充原子归属到指定任务尝试。
	MarkFollowUpsDelivered(ctx context.Context, ids []string, attemptID string, deliveredAt int64) error
	// ReleaseAttemptFollowUps 释放尚未成功启动的尝试已经领取的补充。
	ReleaseAttemptFollowUps(ctx context.Context, attemptID string) error
	// ReadNotifications 读取同团队中已投影报告的最终交付判定。
	ReadNotifications(context.Context, string, []string) ([]models.ChannelLeadNotification, error)
	// CreateNotification 持久化一项等待交给团队负责人的任务结果。
	CreateNotification(ctx context.Context, notification models.ChannelLeadNotification) error
	// ListPendingNotifications 返回团队对话尚未交给负责人的结果通知。
	ListPendingNotifications(ctx context.Context, threadID string, limit int) ([]models.ChannelLeadNotification, error)
	// ListDirectDeliveredNotificationIDs 返回指定通知中已经作为完整成员答复交付的标识。
	ListDirectDeliveredNotificationIDs(ctx context.Context, threadID string, ids []string) ([]string, error)
	// HasLeadProcessingNotifications 判断负责人当前是否正等待或执行成员结果汇总。
	HasLeadProcessingNotifications(ctx context.Context, threadID string, activeExecutionID string) (bool, error)
	// UpdateNotifications 把一批通知推进到指定投递状态并关联负责人执行。
	UpdateNotifications(ctx context.Context, ids []string, status constants.ChannelNotificationStatus, leadExecutionID string, updatedAt int64) error
	// ConsumePendingNotification 在单项成果已投影时原子消费尚未派发的通知。
	ConsumePendingNotification(ctx context.Context, id string, updatedAt int64) (bool, error)
	// ConsumeOpenNotifications 在停止全部工作时消费尚未完成投递的负责人通知。
	ConsumeOpenNotifications(ctx context.Context, threadID string, updatedAt int64) error
	// FinalizeThreadCancellation 在真实执行全部退出后关闭仍开放的任务和当前尝试。
	FinalizeThreadCancellation(ctx context.Context, threadID string, updatedAt int64) error
	// RecoverNotifications 重置中断投递并返回仍有待处理通知的团队对话标识。
	RecoverNotifications(ctx context.Context, updatedAt int64) ([]string, error)
}

// ChannelDispatchSnapshot 是一次团队调度循环使用的不可变持久化快照。
type ChannelDispatchSnapshot struct {
	// Tasks 是团队对话中的全部任务，供调度排序和状态筛选。
	Tasks []models.ChannelTask
	// DependenciesByTaskID 按待执行任务标识保存同一批次读取的上游任务。
	DependenciesByTaskID map[string][]models.ChannelTask
	// ActiveCount 是快照时正在运行的团队任务总数。
	ActiveCount int64
	// ActiveAssigneeWakerIDs 是快照时已有运行任务的执行伙伴集合。
	ActiveAssigneeWakerIDs map[string]struct{}
}

// ListInProgress 返回全部正在等待 Chat Execution 终态的任务。
func (c *channelTaskController) ListInProgress(
	ctx context.Context,
) ([]models.ChannelTask, error) {
	tasks := []models.ChannelTask{}
	err := controllerDB(ctx, c.db).
		Where("status = ?", constants.ChannelTaskStatusInProgress).
		Order("updated_at ASC, id ASC").
		Find(&tasks).Error
	if err != nil {
		return nil, databaseError("list_in_progress_channel_tasks", "channel_task", err)
	}
	return tasks, nil
}

// ListPending 返回全部等待调度的团队任务。
func (c *channelTaskController) ListPending(
	ctx context.Context,
) ([]models.ChannelTask, error) {
	tasks := []models.ChannelTask{}
	err := controllerDB(ctx, c.db).
		Where("status = ?", constants.ChannelTaskStatusPending).
		Order("updated_at ASC, id ASC").
		Find(&tasks).Error
	if err != nil {
		return nil, databaseError("list_pending_channel_tasks", "channel_task", err)
	}
	return tasks, nil
}

// channelTaskController 使用 GORM 实现团队任务持久化。
type channelTaskController struct {
	// db 是未进入事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewChannelTaskController 创建团队任务 Repository Controller。
func NewChannelTaskController(db *gorm.DB) ChannelTaskController {
	return &channelTaskController{db: db}
}

// Create 创建任务主体及其依赖关系。
func (c *channelTaskController) Create(
	ctx context.Context,
	task models.ChannelTask,
	dependencies []models.ChannelTaskDependency,
) error {
	db := controllerDB(ctx, c.db)
	if err := db.Create(&task).Error; err != nil {
		return databaseError("create_channel_task", "task_id="+task.ID, err)
	}
	if len(dependencies) == 0 {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("task", task.ID))
		if task.Status == constants.ChannelTaskStatusInReview {
			publishCommittedNoticeIDs(ctx, c.db, []string{"review:" + task.ID + ":" + fmt.Sprint(task.CurrentFence)}, nil)
		}
		return nil
	}
	if err := db.Create(&dependencies).Error; err != nil {
		return databaseError("create_channel_task_dependencies", "task_id="+task.ID, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("task", task.ID))
	if task.Status == constants.ChannelTaskStatusInReview {
		publishCommittedNoticeIDs(ctx, c.db, []string{"review:" + task.ID + ":" + fmt.Sprint(task.CurrentFence)}, nil)
	}
	return nil
}

// Get 按标识返回团队任务。
func (c *channelTaskController) Get(ctx context.Context, id string) (models.ChannelTask, error) {
	var task models.ChannelTask
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&task).Error
	if err == nil {
		return task, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ChannelTask{}, status_error.ChannelTaskNotFound.WithParams(
			map[string]interface{}{"TaskID": id},
		)
	}
	return models.ChannelTask{}, databaseError("get_channel_task", "task_id="+id, err)
}

// ListByThread 按创建顺序返回团队对话的全部任务。
func (c *channelTaskController) ListByThread(
	ctx context.Context,
	threadID string,
) ([]models.ChannelTask, error) {
	tasks := []models.ChannelTask{}
	err := controllerDB(ctx, c.db).
		Where("thread_id = ?", threadID).
		Order("created_at ASC, id ASC").
		Find(&tasks).Error
	if err != nil {
		return nil, databaseError("list_channel_tasks", "thread_id="+threadID, err)
	}
	return tasks, nil
}

// CountByThread 返回团队对话当前任务总数。
func (c *channelTaskController) CountByThread(
	ctx context.Context,
	threadID string,
) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).Model(&models.ChannelTask{}).
		Where("thread_id = ?", threadID).
		Count(&count).Error
	if err != nil {
		return 0, databaseError("count_channel_tasks", "thread_id="+threadID, err)
	}
	return count, nil
}

// LoadDispatchSnapshot 批量读取一次团队调度所需的任务、依赖和活跃配额事实。
func (c *channelTaskController) LoadDispatchSnapshot(
	ctx context.Context,
	threadID string,
) (ChannelDispatchSnapshot, error) {
	tasks := []models.ChannelTask{}
	db := controllerDB(ctx, c.db)
	if err := db.Where("thread_id = ?", threadID).
		Order("created_at ASC, id ASC").
		Find(&tasks).Error; err != nil {
		return ChannelDispatchSnapshot{}, databaseError(
			"load_channel_dispatch_tasks",
			"thread_id="+threadID,
			err,
		)
	}
	snapshot := ChannelDispatchSnapshot{
		Tasks:                  tasks,
		DependenciesByTaskID:   make(map[string][]models.ChannelTask),
		ActiveAssigneeWakerIDs: make(map[string]struct{}),
	}
	tasksByID := make(map[string]models.ChannelTask, len(tasks))
	pendingTaskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		tasksByID[task.ID] = task
		if task.Status == constants.ChannelTaskStatusPending {
			pendingTaskIDs = append(pendingTaskIDs, task.ID)
		}
		if task.Status == constants.ChannelTaskStatusInProgress {
			snapshot.ActiveCount++
			snapshot.ActiveAssigneeWakerIDs[task.AssigneeWakerID] = struct{}{}
		}
	}
	if len(pendingTaskIDs) == 0 {
		return snapshot, nil
	}
	edges := []models.ChannelTaskDependency{}
	if err := db.Where("task_id IN ?", pendingTaskIDs).
		Order("created_at ASC, depends_on_task_id ASC").
		Find(&edges).Error; err != nil {
		return ChannelDispatchSnapshot{}, databaseError(
			"load_channel_dispatch_dependencies",
			"thread_id="+threadID,
			err,
		)
	}
	for _, edge := range edges {
		dependency, exists := tasksByID[edge.DependsOnTaskID]
		if !exists {
			return ChannelDispatchSnapshot{}, status_error.ChannelTaskNotFound.WithParams(
				map[string]interface{}{"TaskID": edge.DependsOnTaskID},
			)
		}
		snapshot.DependenciesByTaskID[edge.TaskID] = append(
			snapshot.DependenciesByTaskID[edge.TaskID],
			dependency,
		)
	}
	return snapshot, nil
}

// UpdateFields 更新任务服务已经校验过的明确字段集合。
func (c *channelTaskController) UpdateFields(
	ctx context.Context,
	taskID string,
	fields map[string]interface{},
) error {
	publish, err := c.reviewNoticeChanges(ctx, taskID, fields)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return status_error.BadRequest
	}
	db := controllerDB(ctx, c.db)
	if rawPaths, exists := fields["changed_paths"]; exists {
		paths, valid := rawPaths.([]string)
		if !valid {
			return status_error.BadRequest
		}
		if err := db.Model(&models.ChannelTask{}).
			Where("id = ?", taskID).
			Select("ChangedPaths").
			Updates(&models.ChannelTask{ChangedPaths: paths}).Error; err != nil {
			return databaseError("update_channel_task_changed_paths", "task_id="+taskID, err)
		}
		fields = cloneChannelTaskFields(fields)
		delete(fields, "changed_paths")
	}
	if len(fields) == 0 {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("task", taskID))
		return nil
	}
	if err := db.Model(&models.ChannelTask{}).
		Where("id = ?", taskID).
		Updates(fields).Error; err != nil {
		return databaseError("update_channel_task", "task_id="+taskID, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("task", taskID))
	publish()
	return nil
}

// ClaimForDispatch 按旧状态和围栏原子领取一项待派发任务。
func (c *channelTaskController) ClaimForDispatch(
	ctx context.Context,
	taskID string,
	threadID string,
	assigneeWakerID string,
	expectedFence int,
	newFence int,
	maxParallel int,
	startedAt int64,
) (bool, error) {
	db := controllerDB(ctx, c.db)
	activeThreadTasks := db.Model(&models.ChannelTask{}).
		Select("COUNT(*)").
		Where("thread_id = ? AND status = ?", threadID, constants.ChannelTaskStatusInProgress)
	activeAssigneeTasks := db.Model(&models.ChannelTask{}).
		Select("COUNT(*)").
		Where(
			"thread_id = ? AND assignee_waker_id = ? AND status = ?",
			threadID,
			assigneeWakerID,
			constants.ChannelTaskStatusInProgress,
		)
	cancelingThread := db.Model(&models.ChannelThread{}).
		Select("COUNT(*)").
		Where("id = ? AND canceling_at IS NOT NULL", threadID)
	result := db.Model(&models.ChannelTask{}).
		Where(
			"id = ? AND thread_id = ? AND assignee_waker_id = ? AND status = ? AND current_fence = ?",
			taskID,
			threadID,
			assigneeWakerID,
			constants.ChannelTaskStatusPending,
			expectedFence,
		).
		Where("(?) < ?", activeThreadTasks, maxParallel).
		Where("(?) = 0", activeAssigneeTasks).
		Where("(?) = 0", cancelingThread).
		Updates(map[string]interface{}{
			"status":        constants.ChannelTaskStatusInProgress,
			"current_fence": newFence,
			"started_at":    startedAt,
			"finished_at":   nil,
			"error_message": "",
			"updated_at":    startedAt,
		})
	if result.Error != nil {
		return false, databaseError("claim_channel_task_for_dispatch", "task_id="+taskID, result.Error)
	}
	if result.RowsAffected == 1 {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", threadID))
	}
	return result.RowsAffected == 1, nil
}

// cloneChannelTaskFields 避免删除序列化字段时改写调用方持有的更新集合。
func cloneChannelTaskFields(fields map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(fields))
	for name, value := range fields {
		cloned[name] = value
	}
	return cloned
}

// GetActiveForAssignee 定向返回成员当前运行任务；没有时 exists 为 false。
func (c *channelTaskController) GetActiveForAssignee(
	ctx context.Context,
	threadID string,
	wakerID string,
) (models.ChannelTask, bool, error) {
	var task models.ChannelTask
	result := controllerDB(ctx, c.db).
		Where(
			"thread_id = ? AND assignee_waker_id = ? AND status = ?",
			threadID,
			wakerID,
			constants.ChannelTaskStatusInProgress,
		).
		Order("started_at ASC, id ASC").
		Limit(1).
		Find(&task)
	if result.Error != nil {
		return models.ChannelTask{}, false, databaseError(
			"get_active_channel_task_for_assignee",
			"thread_id="+threadID+" waker_id="+wakerID,
			result.Error,
		)
	}
	return task, result.RowsAffected == 1, nil
}

// CreateAttempt 创建一次新任务尝试。
func (c *channelTaskController) CreateAttempt(
	ctx context.Context,
	attempt models.ChannelTaskAttempt,
) error {
	if err := controllerDB(ctx, c.db).Create(&attempt).Error; err != nil {
		return databaseError("create_channel_task_attempt", "attempt_id="+attempt.ID, err)
	}
	return nil
}

// UpdateAttemptFields 更新任务尝试的执行映射或终态结果。
func (c *channelTaskController) UpdateAttemptFields(
	ctx context.Context,
	attemptID string,
	fields map[string]interface{},
) error {
	if len(fields) == 0 {
		return status_error.BadRequest
	}
	if err := controllerDB(ctx, c.db).Model(&models.ChannelTaskAttempt{}).
		Where("id = ?", attemptID).
		Updates(fields).Error; err != nil {
		return databaseError("update_channel_task_attempt", "attempt_id="+attemptID, err)
	}
	return nil
}

// BindAttemptExecution 把外部已接纳 Execution 原子绑定到未绑定尝试。
func (c *channelTaskController) BindAttemptExecution(
	ctx context.Context,
	attemptID string,
	executionID string,
	startedAt int64,
) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.ChannelTaskAttempt{}).
		Where("id = ? AND execution_id = ? AND status = ?", attemptID, executionID, constants.ExecutionStatusPending).
		Updates(map[string]interface{}{
			"status":     constants.ExecutionStatusRunning,
			"started_at": startedAt,
			"updated_at": startedAt,
		})
	if result.Error != nil {
		return false, databaseError("bind_channel_task_attempt_execution", "attempt_id="+attemptID, result.Error)
	}
	return result.RowsAffected == 1, nil
}

// ReleaseDispatchClaim 把未完成启动的尝试终结并按围栏重新排队任务。
func (c *channelTaskController) ReleaseDispatchClaim(
	ctx context.Context,
	taskID string,
	attemptID string,
	fence int,
	status constants.ExecutionStatus,
	errorMessage string,
	updatedAt int64,
) (bool, error) {
	db := controllerDB(ctx, c.db)
	attemptResult := db.Model(&models.ChannelTaskAttempt{}).
		Where(
			"id = ? AND task_id = ? AND fence = ? AND status IN ?",
			attemptID,
			taskID,
			fence,
			[]constants.ExecutionStatus{constants.ExecutionStatusUnspecified, constants.ExecutionStatusPending},
		).
		Updates(map[string]interface{}{
			"status":        status,
			"error_message": errorMessage,
			"finished_at":   updatedAt,
			"updated_at":    updatedAt,
		})
	if attemptResult.Error != nil {
		return false, databaseError("release_channel_task_attempt", "attempt_id="+attemptID, attemptResult.Error)
	}
	if attemptResult.RowsAffected != 1 {
		return false, nil
	}
	result := db.Model(&models.ChannelTask{}).
		Where(
			"id = ? AND status = ? AND current_fence = ?",
			taskID,
			constants.ChannelTaskStatusInProgress,
			fence,
		).
		Updates(map[string]interface{}{
			"status":        constants.ChannelTaskStatusPending,
			"started_at":    nil,
			"finished_at":   nil,
			"error_message": "",
			"updated_at":    updatedAt,
		})
	if result.Error != nil {
		return false, databaseError("requeue_channel_task_dispatch", "task_id="+taskID, result.Error)
	}
	return result.RowsAffected == 1, nil
}

// FailRunningAttempt 在调用方事务内按当前围栏把无法继续监听的运行尝试和任务提交为失败。
func (c *channelTaskController) FailRunningAttempt(
	ctx context.Context,
	taskID string,
	attemptID string,
	fence int,
	errorMessage string,
	updatedAt int64,
) (bool, error) {
	db := controllerDB(ctx, c.db)
	taskResult := db.Model(&models.ChannelTask{}).
		Where(
			"id = ? AND status = ? AND current_fence = ?",
			taskID,
			constants.ChannelTaskStatusInProgress,
			fence,
		).
		Updates(map[string]interface{}{
			"status":        constants.ChannelTaskStatusFailed,
			"error_message": errorMessage,
			"finished_at":   updatedAt,
			"updated_at":    updatedAt,
		})
	if taskResult.Error != nil {
		return false, databaseError("fail_running_channel_task", "task_id="+taskID, taskResult.Error)
	}
	if taskResult.RowsAffected != 1 {
		return false, nil
	}
	attemptResult := db.Model(&models.ChannelTaskAttempt{}).
		Where(
			"id = ? AND task_id = ? AND fence = ? AND status IN ?",
			attemptID,
			taskID,
			fence,
			[]constants.ExecutionStatus{
				constants.ExecutionStatusUnspecified,
				constants.ExecutionStatusPending,
				constants.ExecutionStatusRunning,
			},
		).
		Updates(map[string]interface{}{
			"status":        constants.ExecutionStatusFailed,
			"error_message": errorMessage,
			"finished_at":   updatedAt,
			"updated_at":    updatedAt,
		})
	if attemptResult.Error != nil {
		return false, databaseError("fail_running_channel_task_attempt", "attempt_id="+attemptID, attemptResult.Error)
	}
	if attemptResult.RowsAffected != 1 {
		return false, status_error.ChannelTaskAttemptStale
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("global", ""))
	return true, nil
}

// GetCurrentAttempt 返回与任务当前围栏匹配的尝试。
func (c *channelTaskController) GetCurrentAttempt(
	ctx context.Context,
	taskID string,
	fence int,
) (models.ChannelTaskAttempt, error) {
	var attempt models.ChannelTaskAttempt
	err := controllerDB(ctx, c.db).
		Where("task_id = ? AND fence = ?", taskID, fence).
		First(&attempt).Error
	if err == nil {
		return attempt, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ChannelTaskAttempt{}, status_error.ChannelTaskAttemptNotFound.WithParams(
			map[string]interface{}{"TaskID": taskID},
		)
	}
	return models.ChannelTaskAttempt{}, databaseError(
		"get_channel_task_attempt",
		"task_id="+taskID,
		err,
	)
}

// CountFailureAttempts 返回任务已经失败的尝试累计数量。
func (c *channelTaskController) CountFailureAttempts(
	ctx context.Context,
	taskID string,
) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).Model(&models.ChannelTaskAttempt{}).
		Where("task_id = ? AND status = ?", taskID, constants.ExecutionStatusFailed).
		Count(&count).Error
	if err != nil {
		return 0, databaseError("count_channel_task_failure_attempts", "task_id="+taskID, err)
	}
	return count, nil
}

// CreateFollowUp 持久化一条等待续跑的用户补充。
func (c *channelTaskController) CreateFollowUp(
	ctx context.Context,
	followUp models.ChannelTaskFollowUp,
) error {
	if err := controllerDB(ctx, c.db).Create(&followUp).Error; err != nil {
		return databaseError("create_channel_task_follow_up", "task_id="+followUp.TaskID, err)
	}
	return nil
}

// ListPendingFollowUps 按创建顺序返回任务尚未交付的用户补充。
func (c *channelTaskController) ListPendingFollowUps(
	ctx context.Context,
	taskID string,
) ([]models.ChannelTaskFollowUp, error) {
	followUps := []models.ChannelTaskFollowUp{}
	err := controllerDB(ctx, c.db).
		Where("task_id = ? AND delivered_at IS NULL", taskID).
		Order("created_at ASC, id ASC").
		Find(&followUps).Error
	if err != nil {
		return nil, databaseError("list_channel_task_follow_ups", "task_id="+taskID, err)
	}
	return followUps, nil
}

// ListFollowUps 按创建顺序返回任务的全部补充。
func (c *channelTaskController) ListFollowUps(
	ctx context.Context,
	taskID string,
) ([]models.ChannelTaskFollowUp, error) {
	followUps := []models.ChannelTaskFollowUp{}
	err := controllerDB(ctx, c.db).
		Where("task_id = ?", taskID).
		Order("created_at ASC, id ASC").
		Find(&followUps).Error
	if err != nil {
		return nil, databaseError("list_all_channel_task_follow_ups", "task_id="+taskID, err)
	}
	return followUps, nil
}

// MarkFollowUpsDelivered 把一批用户补充原子归属到指定任务尝试。
func (c *channelTaskController) MarkFollowUpsDelivered(
	ctx context.Context,
	ids []string,
	attemptID string,
	deliveredAt int64,
) error {
	if len(ids) == 0 || attemptID == "" {
		return nil
	}
	if err := controllerDB(ctx, c.db).Model(&models.ChannelTaskFollowUp{}).
		Where("id IN ? AND delivered_at IS NULL", ids).
		Updates(map[string]interface{}{
			"delivered_at":         deliveredAt,
			"delivered_attempt_id": attemptID,
		}).Error; err != nil {
		return databaseError("deliver_channel_task_follow_ups", "follow_up_count", err)
	}
	return nil
}

// ReleaseAttemptFollowUps 释放尚未成功启动的尝试已经领取的补充。
func (c *channelTaskController) ReleaseAttemptFollowUps(
	ctx context.Context,
	attemptID string,
) error {
	if attemptID == "" {
		return nil
	}
	if err := controllerDB(ctx, c.db).Model(&models.ChannelTaskFollowUp{}).
		Where("delivered_attempt_id = ?", attemptID).
		Updates(map[string]interface{}{
			"delivered_at":         nil,
			"delivered_attempt_id": "",
		}).Error; err != nil {
		return databaseError("release_channel_task_follow_ups", "attempt_id="+attemptID, err)
	}
	return nil
}

// CreateNotification 持久化一项等待交给团队负责人的任务结果。
func (c *channelTaskController) CreateNotification(
	ctx context.Context,
	notification models.ChannelLeadNotification,
) error {
	if err := controllerDB(ctx, c.db).Create(&notification).Error; err != nil {
		return databaseError(
			"create_channel_lead_notification",
			"attempt_id="+notification.AttemptID,
			err,
		)
	}
	return nil
}

// ListPendingNotifications 返回团队对话尚未交给负责人的结果通知。
func (c *channelTaskController) ListPendingNotifications(
	ctx context.Context,
	threadID string,
	limit int,
) ([]models.ChannelLeadNotification, error) {
	notifications := []models.ChannelLeadNotification{}
	err := controllerDB(ctx, c.db).
		Where("thread_id = ? AND status = ?", threadID, constants.ChannelNotificationStatusPending).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Find(&notifications).Error
	if err != nil {
		return nil, databaseError(
			"list_channel_lead_notifications",
			"thread_id="+threadID,
			err,
		)
	}
	return notifications, nil
}

// ListDirectDeliveredNotificationIDs 批量读取已直接交付的成员报告标识。
func (c *channelTaskController) ListDirectDeliveredNotificationIDs(
	ctx context.Context,
	threadID string,
	ids []string,
) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	result := []string{}
	err := controllerDB(ctx, c.db).Model(&models.ChannelLeadNotification{}).
		Where("thread_id = ? AND id IN ? AND direct_delivered = ?", threadID, ids, true).
		Pluck("id", &result).Error
	if err != nil {
		return nil, databaseError("list_direct_channel_notifications", "thread_id="+threadID, err)
	}
	return result, nil
}

// HasLeadProcessingNotifications 判断负责人当前是否正等待或执行成员结果汇总。
func (c *channelTaskController) HasLeadProcessingNotifications(
	ctx context.Context,
	threadID string,
	activeExecutionID string,
) (bool, error) {
	var count int64
	db := controllerDB(ctx, c.db).Model(&models.ChannelLeadNotification{})
	if activeExecutionID != "" {
		db = db.Where(
			"thread_id = ? AND status = ? AND lead_execution_id = ?",
			threadID,
			constants.ChannelNotificationStatusDelivering,
			activeExecutionID,
		)
	} else {
		db = db.Where(
			"thread_id = ? AND status IN ?",
			threadID,
			[]constants.ChannelNotificationStatus{
				constants.ChannelNotificationStatusPending,
				constants.ChannelNotificationStatusDelivering,
			},
		)
	}
	err := db.Count(&count).Error
	if err != nil {
		return false, databaseError(
			"check_channel_lead_processing_notifications",
			"thread_id="+threadID,
			err,
		)
	}
	return count > 0, nil
}

// UpdateNotifications 把一批通知推进到指定投递状态并关联负责人执行。
func (c *channelTaskController) UpdateNotifications(
	ctx context.Context,
	ids []string,
	status constants.ChannelNotificationStatus,
	leadExecutionID string,
	updatedAt int64,
) error {
	if len(ids) == 0 {
		return nil
	}
	db := controllerDB(ctx, c.db).Model(&models.ChannelLeadNotification{}).
		Where("id IN ?", ids)
	if status == constants.ChannelNotificationStatusDelivering {
		db = db.Where("status = ?", constants.ChannelNotificationStatusPending)
	} else {
		db = db.Where("status = ?", constants.ChannelNotificationStatusDelivering)
	}
	if err := db.
		Updates(map[string]interface{}{
			"status":            status,
			"lead_execution_id": leadExecutionID,
			"updated_at":        updatedAt,
		}).Error; err != nil {
		return databaseError("update_channel_lead_notifications", "notification_count", err)
	}
	return nil
}

// ConsumePendingNotification 只消费仍为 pending 的单项通知，避免与负责人执行争抢同一结果。
func (c *channelTaskController) ConsumePendingNotification(
	ctx context.Context,
	id string,
	updatedAt int64,
) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.ChannelLeadNotification{}).
		Where("id = ? AND status = ?", id, constants.ChannelNotificationStatusPending).
		Updates(map[string]interface{}{
			"status":            constants.ChannelNotificationStatusConsumed,
			"direct_delivered":  true,
			"lead_execution_id": "",
			"updated_at":        updatedAt,
		})
	if result.Error != nil {
		return false, databaseError("consume_direct_channel_notification", "notification_id="+id, result.Error)
	}
	return result.RowsAffected == 1, nil
}

// ConsumeOpenNotifications 在停止全部工作时消费尚未完成投递的负责人通知。
func (c *channelTaskController) ConsumeOpenNotifications(
	ctx context.Context,
	threadID string,
	updatedAt int64,
) error {
	if err := controllerDB(ctx, c.db).Model(&models.ChannelLeadNotification{}).
		Where("thread_id = ? AND status IN ?", threadID, []constants.ChannelNotificationStatus{
			constants.ChannelNotificationStatusPending,
			constants.ChannelNotificationStatusDelivering,
		}).
		Updates(map[string]interface{}{
			"status":            constants.ChannelNotificationStatusConsumed,
			"lead_execution_id": "",
			"updated_at":        updatedAt,
		}).Error; err != nil {
		return databaseError("consume_open_channel_notifications", "thread_id="+threadID, err)
	}
	return nil
}

// FinalizeThreadCancellation 在真实执行全部退出后关闭仍开放的任务和当前尝试。
func (c *channelTaskController) FinalizeThreadCancellation(
	ctx context.Context,
	threadID string,
	updatedAt int64,
) error {
	db := controllerDB(ctx, c.db)
	openTaskIDs := db.Model(&models.ChannelTask{}).
		Select("id").
		Where("thread_id = ? AND status IN ?", threadID, []constants.ChannelTaskStatus{
			constants.ChannelTaskStatusPending,
			constants.ChannelTaskStatusInProgress,
		})
	if err := db.Model(&models.ChannelTaskAttempt{}).
		Where("task_id IN (?) AND status IN ?", openTaskIDs, []constants.ExecutionStatus{
			constants.ExecutionStatusPending,
			constants.ExecutionStatusRunning,
		}).
		Updates(map[string]interface{}{
			"status":      constants.ExecutionStatusCanceled,
			"finished_at": updatedAt,
			"updated_at":  updatedAt,
		}).Error; err != nil {
		return databaseError("cancel_open_channel_task_attempts", "thread_id="+threadID, err)
	}
	if err := db.Model(&models.ChannelTask{}).
		Where("thread_id = ? AND status IN ?", threadID, []constants.ChannelTaskStatus{
			constants.ChannelTaskStatusPending,
			constants.ChannelTaskStatusInProgress,
		}).
		Updates(map[string]interface{}{
			"status":      constants.ChannelTaskStatusCancelled,
			"finished_at": updatedAt,
			"updated_at":  updatedAt,
		}).Error; err != nil {
		return databaseError("cancel_open_channel_tasks", "thread_id="+threadID, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("thread", threadID))
	return nil
}

// RecoverNotifications 重置中断投递并返回仍有待处理通知的团队对话标识。
func (c *channelTaskController) RecoverNotifications(
	ctx context.Context,
	updatedAt int64,
) ([]string, error) {
	db := controllerDB(ctx, c.db)
	writableThreadIDs := db.Model(&models.ChannelThread{}).
		Select("channel_threads.id")
	if err := db.Model(&models.ChannelLeadNotification{}).
		Where("thread_id IN (?)", writableThreadIDs).
		Where("status = ?", constants.ChannelNotificationStatusDelivering).
		Updates(map[string]interface{}{
			"status":            constants.ChannelNotificationStatusPending,
			"lead_execution_id": "",
			"updated_at":        updatedAt,
		}).Error; err != nil {
		return nil, databaseError("recover_delivering_channel_notifications", "channel_notification", err)
	}
	threadIDs := []string{}
	if err := db.Model(&models.ChannelLeadNotification{}).
		Distinct("thread_id").
		Where("thread_id IN (?)", writableThreadIDs).
		Where("status = ?", constants.ChannelNotificationStatusPending).
		Order("thread_id ASC").
		Pluck("thread_id", &threadIDs).Error; err != nil {
		return nil, databaseError("list_pending_channel_notification_threads", "channel_notification", err)
	}
	return threadIDs, nil
}

// ReadNotifications 读取同团队中已投影报告的最终交付判定。
func (c *channelTaskController) ReadNotifications(ctx context.Context, threadID string, ids []string) ([]models.ChannelLeadNotification, error) {
	result := []models.ChannelLeadNotification{}
	if len(ids) == 0 {
		return result, nil
	}
	if err := controllerDB(ctx, c.db).Where("thread_id = ? AND id IN ?", threadID, ids).Find(&result).Error; err != nil {
		return nil, databaseError("read_channel_notifications", threadID, err)
	}
	return result, nil
}
