package controllers

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// WorkflowController 是源码版本与运行持久事实的唯一数据库边界。
type WorkflowController interface {
	// CreateDefinition 建立没有运行版本的空定义。
	CreateDefinition(context.Context, models.WorkflowDefinition) error
	// DeleteDefinition 删除资源入口但保留不可变版本与历史，活跃运行和触发依赖必须先解除。
	DeleteDefinition(context.Context, string, string) error
	// GetDefinition 按稳定身份读取定义。
	GetDefinition(context.Context, string) (models.WorkflowDefinition, error)
	// ListDefinitions 使用字面搜索和更新时间游标读取有界列表。
	ListDefinitions(context.Context, string, int64, string, int) ([]models.WorkflowDefinition, error)
	// UpdateHead 在当前版本仍匹配时提交新版本指针及摘要。
	UpdateHead(context.Context, models.WorkflowDefinition, string) error
	// CreateRevision 插入不可变源码、投影和执行程序。
	CreateRevision(context.Context, models.WorkflowRevision) error
	// DeleteRevision 从历史列表移除版本，保留运行已固定的不可变程序。
	DeleteRevision(context.Context, string, string, int64) error
	// GetRevision 读取同一工作流拥有的完整版本。
	GetRevision(context.Context, string, string) (models.WorkflowRevision, error)
	// ListRevisions 倒序读取有界历史，不返回源码和指令。
	ListRevisions(context.Context, string, int64, int) ([]models.WorkflowRevision, error)
	// FindReceipt 读取请求的首次提交回执。
	FindReceipt(context.Context, string, string) (models.WorkflowCommandReceipt, bool, error)
	// CreateReceipt 保存不可变幂等回执。
	CreateReceipt(context.Context, models.WorkflowCommandReceipt) error
	// AppendEvent 在当前事务追加一个连续持久事件。
	AppendEvent(context.Context, models.WorkflowEvent) (models.WorkflowEvent, error)
	// ListEvents 按聚合和序号读取有界重放窗口。
	ListEvents(context.Context, string, int64, int) ([]models.WorkflowEvent, error)
	// ListLogEvents 按原序号读取仅包含运行日志的有界窗口。
	ListLogEvents(context.Context, string, int64, int) ([]models.WorkflowEvent, error)
}

// workflowController 始终使用 Context 中的事务连接。
type workflowController struct {
	// db 是没有上层事务时使用的默认数据库连接。
	db *gorm.DB
}

// NewWorkflowController 建立 Workflow 的持久事实边界。
func NewWorkflowController(db *gorm.DB) WorkflowController { return &workflowController{db: db} }

func (c *workflowController) CreateDefinition(ctx context.Context, value models.WorkflowDefinition) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		return databaseError("create_workflow_definition", value.ID, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("workflow", value.ID))
	return nil
}

func (c *workflowController) DeleteDefinition(ctx context.Context, id, expectedRevisionID string) error {
	db := controllerDB(ctx, c.db)
	var active int64
	if err := db.Model(&models.WorkflowRun{}).Where("definition_id = ? AND status NOT IN ?", id, []constants.WorkflowRunStatus{constants.WorkflowRunStatusSucceeded, constants.WorkflowRunStatusFailed, constants.WorkflowRunStatusCancelled}).Count(&active).Error; err != nil {
		return databaseError("check_workflow_delete_runs", id, err)
	}
	if active > 0 {
		return status_error.WorkflowStateConflict.WithField("reason", "active_runs")
	}
	var triggers int64
	if err := db.Model(&models.AutomaticTask{}).Where("workflow_id = ? AND workflow_trigger_detached = ?", id, false).Count(&triggers).Error; err != nil {
		return databaseError("check_workflow_delete_triggers", id, err)
	}
	if triggers > 0 {
		return status_error.WorkflowStateConflict.WithField("reason", "trigger_dependencies")
	}
	result := db.Where("id = ? AND current_revision_id = ?", id, expectedRevisionID).Delete(&models.WorkflowDefinition{})
	if result.Error != nil {
		return databaseError("delete_workflow_definition", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.WorkflowRevisionConflict
	}
	if err := db.Where("owner_kind = ? AND owner_id IN (?)", constants.WorkResultOwnerKindWorkflowRun, db.Model(&models.WorkflowRun{}).Select("id").Where("definition_id = ?", id)).Delete(&models.WorkResultReceipt{}).Error; err != nil {
		return databaseError("delete_workflow_results", id, err)
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("global", ""))
	return nil
}
func (c *workflowController) GetDefinition(ctx context.Context, id string) (models.WorkflowDefinition, error) {
	var value models.WorkflowDefinition
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, status_error.WorkflowNotFound
	}
	if err != nil {
		return value, databaseError("get_workflow_definition", id, err)
	}
	return value, nil
}
func (c *workflowController) ListDefinitions(ctx context.Context, query string, before int64, beforeID string, limit int) ([]models.WorkflowDefinition, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	db := controllerDB(ctx, c.db).Model(&models.WorkflowDefinition{})
	if query = strings.ToLower(strings.TrimSpace(query)); query != "" {
		db = db.Where("instr(lower(title), ?) > 0 OR instr(lower(description), ?) > 0", query, query)
	}
	if before > 0 {
		db = db.Where("updated_at < ? OR (updated_at = ? AND id < ?)", before, before, beforeID)
	}
	values := []models.WorkflowDefinition{}
	if err := db.Order("updated_at DESC, id DESC").Limit(limit).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_definitions", "", err)
	}
	return values, nil
}
func (c *workflowController) UpdateHead(ctx context.Context, value models.WorkflowDefinition, expected string) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkflowDefinition{}).Where("id = ? AND current_revision_id = ?", value.ID, expected).Updates(map[string]any{"current_revision_id": value.CurrentRevisionID, "ordinal": value.Ordinal, "title": value.Title, "description": value.Description, "when_to_use": value.WhenToUse})
	if result.Error != nil {
		return databaseError("update_workflow_head", value.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.WorkflowRevisionConflict
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("global", ""))
	return nil
}
func (c *workflowController) CreateRevision(ctx context.Context, value models.WorkflowRevision) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		return databaseError("create_workflow_revision", value.ID, err)
	}
	return nil
}
func (c *workflowController) GetRevision(ctx context.Context, definitionID, id string) (models.WorkflowRevision, error) {
	var value models.WorkflowRevision
	err := controllerDB(ctx, c.db).Where("definition_id = ? AND id = ?", definitionID, id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, status_error.WorkflowNotFound
	}
	if err != nil {
		return value, databaseError("get_workflow_revision", id, err)
	}
	return value, nil
}
func (c *workflowController) DeleteRevision(ctx context.Context, definitionID, id string, deletedAt int64) error {
	db := controllerDB(ctx, c.db)
	head := db.Model(&models.WorkflowDefinition{}).Select("current_revision_id").Where("id = ?", definitionID)
	result := db.Model(&models.WorkflowRevision{}).Where("definition_id = ? AND id = ? AND deleted_at IS NULL AND id NOT IN (?)", definitionID, id, head).Update("deleted_at", deletedAt)
	if result.Error != nil {
		return databaseError("delete_workflow_revision", id, result.Error)
	}
	return nil
}
func (c *workflowController) ListRevisions(ctx context.Context, definitionID string, before int64, limit int) ([]models.WorkflowRevision, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	db := controllerDB(ctx, c.db).Omit("source", "bundle").Where("definition_id = ? AND deleted_at IS NULL", definitionID)
	if before > 0 {
		db = db.Where("ordinal < ?", before)
	}
	values := []models.WorkflowRevision{}
	if err := db.Order("ordinal DESC").Limit(limit).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_revisions", definitionID, err)
	}
	return values, nil
}
func (c *workflowController) FindReceipt(ctx context.Context, aggregateID, requestID string) (models.WorkflowCommandReceipt, bool, error) {
	var value models.WorkflowCommandReceipt
	err := controllerDB(ctx, c.db).Where("aggregate_id = ? AND request_id = ?", aggregateID, requestID).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, false, nil
	}
	if err != nil {
		return value, false, databaseError("get_workflow_receipt", aggregateID, err)
	}
	return value, true, nil
}
func (c *workflowController) CreateReceipt(ctx context.Context, value models.WorkflowCommandReceipt) error {
	if err := controllerDB(ctx, c.db).Create(&value).Error; err != nil {
		return databaseError("create_workflow_receipt", value.AggregateID, err)
	}
	return nil
}
func (c *workflowController) AppendEvent(ctx context.Context, value models.WorkflowEvent) (models.WorkflowEvent, error) {
	db := controllerDB(ctx, c.db)
	var last int64
	if err := db.Model(&models.WorkflowEvent{}).Where("stream_id = ?", value.StreamID).Select("COALESCE(MAX(sequence), 0)").Find(&last).Error; err != nil {
		return value, databaseError("get_workflow_event_sequence", value.StreamID, err)
	}
	value.Sequence = last + 1
	if err := db.Create(&value).Error; err != nil {
		return value, databaseError("append_workflow_event", value.StreamID, err)
	}
	publishCommittedChange(ctx, c.db, "workflow", eventsResource("workflow", value.StreamID))
	return value, nil
}
func (c *workflowController) ListEvents(ctx context.Context, streamID string, after int64, limit int) ([]models.WorkflowEvent, error) {
	return c.listEvents(ctx, streamID, after, limit, nil)
}

// ListLogEvents 在数据库中过滤日志及用户跳过记录，不传输无关执行事件。
func (c *workflowController) ListLogEvents(ctx context.Context, streamID string, after int64, limit int) ([]models.WorkflowEvent, error) {
	return c.listEvents(ctx, streamID, after, limit, []string{constants.WorkflowLogEvent, constants.WorkflowWorkerSkipRequestedEvent})
}

func (c *workflowController) listEvents(ctx context.Context, streamID string, after int64, limit int, types []string) ([]models.WorkflowEvent, error) {
	if limit <= 0 || limit > constants.WorkflowEventHistoryPageSize {
		limit = 100
	}
	query := controllerDB(ctx, c.db).Where("stream_id = ? AND sequence > ?", streamID, after)
	if len(types) > 0 {
		query = query.Where("type IN ?", types)
	}
	values := []models.WorkflowEvent{}
	if err := query.Order("sequence ASC").Limit(limit).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_events", streamID, err)
	}
	return values, nil
}
