package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
)

// AutomaticTaskFilter 是伙伴任务列表的稳定查询条件。
type AutomaticTaskFilter struct {
	// WorkflowID 限定同一工作流的整份触发配置。
	WorkflowID string
	// WakerID 是独立伙伴或团队负责人的可选筛选，空值枚举全部执行者。
	WakerID string
	// ChannelID 是需要匹配的团队，空值表示不按团队标识筛选。
	ChannelID string
	// ExecutorKind 按伙伴、团队或工作流筛选，空值表示全部。
	ExecutorKind string
	// Query 是任务名称或指令中的字面搜索词。
	Query string
	// Trigger 是需要匹配的触发类型，空值表示全部。
	Trigger constants.AutomaticTaskTrigger
	// Enabled 是可选启停条件。
	Enabled *bool
	// Limit 是本次最多返回的条目，零值表示后台完整枚举。
	Limit int
	// Offset 是分页起始位置。
	Offset int
}

// AutomaticTaskCounts 是当前查询范围全部任务按触发和启停分类的数量。
type AutomaticTaskCounts struct {
	// Total 是伙伴拥有的全部任务数。
	Total int64 `json:"total"`
	// Enabled 是当前已启用的任务数。
	Enabled int64 `json:"enabled"`
	// Scheduled 是采用定时触发的任务数。
	Scheduled int64 `json:"scheduled"`
	// API 是采用本机 API 触发的任务数。
	API int64 `json:"api"`
}

// AutomaticTaskController 定义自动配置、唯一机会和运行槽的持久化操作。
type AutomaticTaskController interface {
	// FindWorkflowTriggerReceipt 查找整份触发配置的幂等保存收据，不保存密钥明文。
	FindWorkflowTriggerReceipt(context.Context, string, string) (models.WorkflowTriggerReceipt, bool, error)
	// CreateWorkflowTriggerReceipt 在配置事务内登记固定的请求摘要和结果身份。
	CreateWorkflowTriggerReceipt(context.Context, models.WorkflowTriggerReceipt) error
	// Create 建立任务配置。
	Create(context.Context, models.AutomaticTask) error
	// Get 获取任务的完整配置及内部调度事实。
	Get(context.Context, string) (models.AutomaticTask, error)
	// List 按可选伙伴、执行者类型、名称、触发和启停条件分页枚举任务。
	List(context.Context, AutomaticTaskFilter) ([]models.AutomaticTask, int64, error)
	// Counts 获取全部任务或指定伙伴的统计。
	Counts(context.Context, string) (AutomaticTaskCounts, error)
	// UpdateConfiguration 按乐观版本替换允许编辑的完整任务配置。
	UpdateConfiguration(context.Context, models.AutomaticTask, int64) error
	// Due 枚举指定时间前待处理的启用定时任务。
	Due(context.Context, int64) ([]models.AutomaticTask, error)
	// ClaimRun 条件占用运行槽，同时提交下次计划时间。
	ClaimRun(context.Context, string, int64, string, *int64) (bool, error)
	// AdvanceSchedule 条件推进忙碌或错过的计划游标。
	AdvanceSchedule(context.Context, models.AutomaticTask, int64) (bool, error)
	// ReleaseRun 只释放仍由指定运行持有的槽位。
	ReleaseRun(context.Context, string, string) error
	// SetDeleting 封闭或恢复一个任务的接纳入口。
	SetDeleting(context.Context, string, bool) error
	// Delete 删除任务主体及其所有机会记录。
	Delete(context.Context, string) error
	// DeleteByWaker 删除伙伴拥有的全部任务及机会。
	DeleteByWaker(context.Context, string) error
	// CreateRun 建立受唯一机会约束保护的运行记录。
	CreateRun(context.Context, models.AutomaticRun) error
	// GetRun 按运行身份获取完整记录。
	GetRun(context.Context, string) (models.AutomaticRun, error)
	// FindRun 查找同一任务的已登记机会。
	FindRun(context.Context, string, string) (models.AutomaticRun, bool, error)
	// ListRuns 枚举任务的运行记录并返回完整数量。
	ListRuns(context.Context, string, int, int) ([]models.AutomaticRun, int64, error)
	// CountAcceptedRuns 返回任务已进入 Chat 的运行数量。
	CountAcceptedRuns(context.Context, string) (int64, error)
	// UpdateRunAdmission 只更新接纳阶段、关联身份和安全错误码。
	UpdateRunAdmission(context.Context, models.AutomaticRun) error
	// FindOpenTeamRun 查询团队对话关联且尚未冻结终态的自动运行。
	FindOpenTeamRun(context.Context, string) (models.AutomaticRun, bool, error)
	// CloseTeamRun 只在团队首次整体结束时保存终态投影，后续续聊不能覆盖。
	CloseTeamRun(context.Context, string, constants.AutomaticRunState, *int64) error
}

// automaticTaskController 使用当前事务中的 GORM 连接持久化任务与机会。
type automaticTaskController struct {
	// db 是没有上层事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewAutomaticTaskController 创建自动任务持久化边界。
func NewAutomaticTaskController(db *gorm.DB) AutomaticTaskController {
	return &automaticTaskController{db: db}
}

// Create 建立任务配置。
func (c *automaticTaskController) Create(ctx context.Context, task models.AutomaticTask) error {
	if err := controllerDB(ctx, c.db).Create(&task).Error; err != nil {
		return databaseError("create_automatic_task", "task_id="+task.ID, err)
	}
	publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	return nil
}

// Get 获取任务的完整配置及内部调度事实。
func (c *automaticTaskController) Get(ctx context.Context, id string) (models.AutomaticTask, error) {
	var task models.AutomaticTask
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return task, status_error.AutomaticTaskNotFound
	}
	if err != nil {
		return task, databaseError("get_automatic_task", "task_id="+id, err)
	}
	return task, nil
}

func automaticTaskQuery(db *gorm.DB, filter AutomaticTaskFilter) *gorm.DB {
	db = db.Model(&models.AutomaticTask{}).Where("workflow_trigger_detached = ?", false)
	if filter.WorkflowID != "" {
		db = db.Where("workflow_id = ?", filter.WorkflowID)
	}
	if filter.WakerID != "" {
		db = db.Where("waker_id = ?", filter.WakerID)
	}
	if filter.ChannelID != "" {
		db = db.Where("channel_id = ?", filter.ChannelID)
	}
	if filter.ExecutorKind == "waker" {
		db = db.Where("channel_id = '' AND workflow_id = ''")
	}
	if filter.ExecutorKind == "team" {
		db = db.Where("channel_id <> ''")
	}
	if filter.ExecutorKind == "workflow" {
		db = db.Where("workflow_id <> ''")
	}
	if query := strings.ToLower(strings.TrimSpace(filter.Query)); query != "" {
		db = db.Where("instr(lower(name), ?) > 0 OR instr(lower(instruction), ?) > 0", query, query)
	}
	if filter.Trigger != constants.AutomaticTaskTriggerUnspecified {
		db = db.Where("trigger = ?", filter.Trigger)
	}
	if filter.Enabled != nil {
		db = db.Where("enabled = ?", *filter.Enabled)
	}
	return db
}

// List 按可选伙伴、执行者类型、名称、触发和启停条件分页枚举任务。
func (c *automaticTaskController) List(ctx context.Context, filter AutomaticTaskFilter) ([]models.AutomaticTask, int64, error) {
	db := automaticTaskQuery(controllerDB(ctx, c.db), filter)
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return nil, 0, databaseError("count_automatic_tasks", "waker_id="+filter.WakerID, err)
	}
	var tasks []models.AutomaticTask
	if filter.Limit > 0 {
		db = db.Limit(filter.Limit).Offset(filter.Offset)
	}
	if err := db.Order("created_at DESC, id ASC").Find(&tasks).Error; err != nil {
		return nil, 0, databaseError("list_automatic_tasks", "waker_id="+filter.WakerID, err)
	}
	return tasks, count, nil
}

// Counts 获取全部任务或指定伙伴的统计。
func (c *automaticTaskController) Counts(ctx context.Context, wakerID string) (AutomaticTaskCounts, error) {
	var counts AutomaticTaskCounts
	err := automaticTaskQuery(controllerDB(ctx, c.db), AutomaticTaskFilter{WakerID: wakerID}).
		Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN enabled THEN 1 ELSE 0 END), 0) AS enabled, COALESCE(SUM(CASE WHEN trigger = ? THEN 1 ELSE 0 END), 0) AS scheduled, COALESCE(SUM(CASE WHEN trigger = ? THEN 1 ELSE 0 END), 0) AS api", constants.AutomaticTaskTriggerSchedule, constants.AutomaticTaskTriggerAPI).
		Find(&counts).Error
	if err != nil {
		return counts, databaseError("summarize_automatic_tasks", "waker_id="+wakerID, err)
	}
	return counts, nil
}

// UpdateConfiguration 按乐观版本替换允许编辑的完整任务配置。
func (c *automaticTaskController) UpdateConfiguration(ctx context.Context, task models.AutomaticTask, revision int64) error {
	args, err := json.Marshal(task.WorkflowInputBindings)
	if err != nil {
		return status_error.BadRequest
	}
	members, err := json.Marshal(task.MemberConfigs)
	if err != nil {
		return status_error.BadRequest
	}
	knowledge, err := json.Marshal(task.Knowledge)
	if err != nil {
		return status_error.BadRequest
	}
	schedule, err := json.Marshal(task.Schedule)
	if err != nil {
		return status_error.BadRequest
	}
	// 显式编码可空 JSON，避免 ORM 对 nil interface 执行字段赋值时 panic。
	updates := map[string]any{"knowledge": string(knowledge), "workflow_trigger_detached": task.WorkflowTriggerDetached, "target_kind": task.TargetKind, "workflow_id": task.WorkflowID, "workflow_revision_policy": task.WorkflowRevisionPolicy, "workflow_revision_id": task.WorkflowRevisionID, "workflow_input_bindings": string(args), "workflow_input_revision_id": task.WorkflowInputRevisionID, "workspace_read_only": task.WorkspaceReadOnly, "member_configs": string(members), "waker_id": task.WakerID, "channel_id": task.ChannelID, "executor_waker_id": task.ExecutorWakerID, "name": task.Name, "instruction": task.Instruction, "trigger": task.Trigger, "schedule": string(schedule), "project_id": task.ProjectID, "working_directory": task.WorkingDirectory, "runtime_id": task.RuntimeID, "account_id": task.AccountID, "model": task.Model, "reasoning": task.Reasoning, "permission_level": task.PermissionLevel, "enabled": task.Enabled, "revision": task.Revision, "configuration_version": task.ConfigurationVersion, "trigger_version": task.TriggerVersion, "token_hash": task.TokenHash, "next_run_at": task.NextRunAt, "updated_at": task.UpdatedAt}
	result := controllerDB(ctx, c.db).Model(&models.AutomaticTask{}).Where("id = ? AND revision = ? AND deleting = ?", task.ID, revision, false).
		Updates(updates)
	if result.Error != nil {
		return databaseError("update_automatic_task", "task_id="+task.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		return status_error.AutomaticTaskRevisionConflict
	}
	publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	return nil
}

// Due 枚举指定时间前待处理的启用定时任务。
func (c *automaticTaskController) Due(ctx context.Context, before int64) ([]models.AutomaticTask, error) {
	var tasks []models.AutomaticTask
	err := controllerDB(ctx, c.db).Where("enabled = ? AND deleting = ? AND trigger = ? AND next_run_at <= ?", true, false, constants.AutomaticTaskTriggerSchedule, before).
		Order("next_run_at ASC, id ASC").Find(&tasks).Error
	if err != nil {
		return nil, databaseError("list_due_automatic_tasks", "", err)
	}
	return tasks, nil
}

// ClaimRun 条件占用运行槽，同时提交下次计划时间。
func (c *automaticTaskController) ClaimRun(ctx context.Context, id string, revision int64, runID string, next *int64) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.AutomaticTask{}).
		Where("id = ? AND revision = ? AND enabled = ? AND deleting = ? AND active_run_id = ?", id, revision, true, false, "").
		Updates(map[string]any{"active_run_id": runID, "next_run_at": next, "revision": revision + 1})
	if result.Error != nil {
		return false, databaseError("claim_automatic_task_run", "task_id="+id, result.Error)
	}
	if result.RowsAffected == 1 {
		publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	}
	return result.RowsAffected == 1, nil
}

// AdvanceSchedule 条件推进忙碌或错过的计划游标。
func (c *automaticTaskController) AdvanceSchedule(ctx context.Context, task models.AutomaticTask, revision int64) (bool, error) {
	result := controllerDB(ctx, c.db).Model(&models.AutomaticTask{}).Where("id = ? AND revision = ? AND deleting = ?", task.ID, revision, false).
		Updates(map[string]any{"next_run_at": task.NextRunAt, "missed_count": task.MissedCount, "last_missed_at": task.LastMissedAt, "revision": revision + 1})
	if result.Error != nil {
		return false, databaseError("advance_automatic_task_schedule", "task_id="+task.ID, result.Error)
	}
	if result.RowsAffected == 1 {
		publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	}
	return result.RowsAffected == 1, nil
}

// ReleaseRun 只释放仍由指定运行持有的槽位。
func (c *automaticTaskController) ReleaseRun(ctx context.Context, taskID, runID string) error {
	if err := controllerDB(ctx, c.db).Model(&models.AutomaticTask{}).Where("id = ? AND active_run_id = ?", taskID, runID).
		Updates(map[string]any{"active_run_id": "", "revision": gorm.Expr("revision + 1")}).Error; err != nil {
		return databaseError("release_automatic_task_run", "task_id="+taskID, err)
	}
	publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	return nil
}

// SetDeleting 封闭或恢复一个任务的接纳入口。
func (c *automaticTaskController) SetDeleting(ctx context.Context, id string, deleting bool) error {
	if err := controllerDB(ctx, c.db).Model(&models.AutomaticTask{}).Where("id = ?", id).
		Updates(map[string]any{"deleting": deleting, "revision": gorm.Expr("revision + 1")}).Error; err != nil {
		return databaseError("mark_automatic_task_deleting", "task_id="+id, err)
	}
	publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	return nil
}

// Delete 原子删除任务主体、机会记录和 API Key 授权引用，可加入上层事务。
func (c *automaticTaskController) Delete(ctx context.Context, id string) error {
	return NewTransactionController(dbhelper.NewGormTransactionManager(c.db)).Transaction(ctx, func(tx context.Context) error {
		db := controllerDB(tx, c.db)
		if err := removeIntegrationResourceGrants(tx, c.db, constants.IntegrationResourceKindAutomaticTask, []string{id}); err != nil {
			return err
		}
		if err := db.Where("task_id = ?", id).Delete(&models.AutomaticRun{}).Error; err != nil {
			return databaseError("delete_automatic_runs", id, err)
		}
		if err := db.Where("id = ?", id).Delete(&models.AutomaticTask{}).Error; err != nil {
			return databaseError("delete_automatic_task", id, err)
		}
		publishCommittedChange(tx, c.db, "automatic-tasks", eventsResource("global", ""))
		return nil
	})
}

// DeleteByWaker 删除由伙伴独立执行的任务及机会；团队任务保留团队身份。
func (c *automaticTaskController) DeleteByWaker(ctx context.Context, wakerID string) error {
	return NewTransactionController(dbhelper.NewGormTransactionManager(c.db)).Transaction(ctx, func(tx context.Context) error {
		for {
			var tasks []models.AutomaticTask
			if err := controllerDB(tx, c.db).Select("id").Where("waker_id = ? AND channel_id = ''", wakerID).Limit(100).Find(&tasks).Error; err != nil {
				return databaseError("list_deleted_waker_tasks", wakerID, err)
			}
			ids := make([]string, 0, len(tasks))
			for _, task := range tasks {
				ids = append(ids, task.ID)
			}
			if len(ids) > 0 {
				if err := removeIntegrationResourceGrants(tx, c.db, constants.IntegrationResourceKindAutomaticTask, ids); err != nil {
					return err
				}
				if err := controllerDB(tx, c.db).Where("task_id IN ?", ids).Delete(&models.AutomaticRun{}).Error; err != nil {
					return databaseError("delete_waker_automatic_runs", wakerID, err)
				}
				if err := controllerDB(tx, c.db).Where("id IN ?", ids).Delete(&models.AutomaticTask{}).Error; err != nil {
					return databaseError("delete_waker_automatic_tasks", wakerID, err)
				}
				publishCommittedChange(tx, c.db, "automatic-tasks", eventsResource("global", ""))
			}
			if len(tasks) < 100 {
				return nil
			}
		}
	})
}

// CreateRun 建立受唯一机会约束保护的运行记录。
func (c *automaticTaskController) CreateRun(ctx context.Context, run models.AutomaticRun) error {
	err := controllerDB(ctx, c.db).Create(&run).Error
	if err != nil && dbhelper.IsDuplicateError(err) {
		return status_error.AutomaticTaskRequestConflict
	}
	if err != nil {
		return databaseError("create_automatic_run", "task_id="+run.TaskID, err)
	}
	publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	if run.Admission == constants.AutomaticRunAdmissionFailed {
		publishCommittedNoticeIDs(ctx, c.db, []string{"automatic-run-failed:" + run.ID}, nil)
	}
	return nil
}

// GetRun 按运行身份获取完整记录。
func (c *automaticTaskController) GetRun(ctx context.Context, id string) (models.AutomaticRun, error) {
	var run models.AutomaticRun
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, status_error.AutomaticRunNotFound
	}
	if err != nil {
		return run, databaseError("get_automatic_run", "run_id="+id, err)
	}
	return run, nil
}

// FindRun 查找同一任务的已登记机会。
func (c *automaticTaskController) FindRun(ctx context.Context, taskID, key string) (models.AutomaticRun, bool, error) {
	var run models.AutomaticRun
	err := controllerDB(ctx, c.db).Where("task_id = ? AND occurrence_key = ?", taskID, key).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, false, nil
	}
	if err != nil {
		return run, false, databaseError("find_automatic_run", "task_id="+taskID, err)
	}
	return run, true, nil
}

// ListRuns 枚举任务的运行记录并返回完整数量。
func (c *automaticTaskController) ListRuns(ctx context.Context, taskID string, limit, offset int) ([]models.AutomaticRun, int64, error) {
	db := controllerDB(ctx, c.db).Model(&models.AutomaticRun{}).Where("task_id = ?", taskID)
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return nil, 0, databaseError("count_automatic_runs", "task_id="+taskID, err)
	}
	if limit > 0 {
		db = db.Limit(limit).Offset(offset)
	}
	var runs []models.AutomaticRun
	if err := db.Order("created_at DESC, id DESC").Find(&runs).Error; err != nil {
		return nil, 0, databaseError("list_automatic_runs", "task_id="+taskID, err)
	}
	return runs, count, nil
}

// CountAcceptedRuns 返回任务已进入 Chat 的运行数量。
func (c *automaticTaskController) CountAcceptedRuns(ctx context.Context, taskID string) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).Model(&models.AutomaticRun{}).Where("task_id = ? AND admission = ?", taskID, constants.AutomaticRunAdmissionAccepted).Count(&count).Error
	if err != nil {
		return 0, databaseError("count_accepted_automatic_runs", "task_id="+taskID, err)
	}
	return count, nil
}

// UpdateRunAdmission 只更新接纳阶段、关联身份和安全错误码。
func (c *automaticTaskController) UpdateRunAdmission(ctx context.Context, run models.AutomaticRun) error {
	var wasFailed bool
	if _, enabled := c.db.Get(eventPublisherSetting); enabled && run.Admission == constants.AutomaticRunAdmissionFailed {
		previous, err := c.GetRun(ctx, run.ID)
		if err != nil {
			return err
		}
		wasFailed = previous.Admission == constants.AutomaticRunAdmissionFailed
	}
	err := controllerDB(ctx, c.db).Model(&models.AutomaticRun{}).Where("id = ?", run.ID).
		Updates(map[string]any{"admission": run.Admission, "waker_id": run.WakerID, "chat_id": run.ChatID, "thread_id": run.ThreadID, "workflow_run_id": run.WorkflowRunID, "execution_id": run.ExecutionID, "error_code": run.ErrorCode, "updated_at": run.UpdatedAt}).Error
	if err != nil {
		return databaseError("update_automatic_run_admission", "run_id="+run.ID, err)
	}
	publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	if !wasFailed && run.Admission == constants.AutomaticRunAdmissionFailed {
		publishCommittedNoticeIDs(ctx, c.db, []string{"automatic-run-failed:" + run.ID}, nil)
	}
	return nil
}

// CloseTeamRun 只在团队首次整体结束时保存终态投影，后续续聊不能覆盖。
func (c *automaticTaskController) CloseTeamRun(ctx context.Context, id string, state constants.AutomaticRunState, completedAt *int64) error {
	result := controllerDB(ctx, c.db).Model(&models.AutomaticRun{}).Where("id = ? AND final_state = '' AND thread_id <> ''", id).
		Updates(map[string]any{"final_state": state, "final_completed_at": completedAt})
	if result.Error != nil {
		return databaseError("close_team_automatic_run", "run_id="+id, result.Error)
	}
	if result.RowsAffected == 0 {
		return nil
	}
	publishCommittedChange(ctx, c.db, "automatic-tasks", eventsResource("global", ""))
	if state == constants.AutomaticRunStateFailed {
		publishCommittedNoticeIDs(ctx, c.db, []string{"automatic-run-failed:" + id}, nil)
	}
	return nil
}

// FindOpenTeamRun 用团队对话索引定位需要在续聊前封闭的自动运行。
func (c *automaticTaskController) FindOpenTeamRun(ctx context.Context, threadID string) (models.AutomaticRun, bool, error) {
	var run models.AutomaticRun
	err := controllerDB(ctx, c.db).Where("thread_id = ? AND final_state = '' AND admission = ?", threadID, constants.AutomaticRunAdmissionAccepted).First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return run, false, nil
	}
	if err != nil {
		return run, false, databaseError("find_open_team_automatic_run", "thread_id="+threadID, err)
	}
	return run, true, nil
}
