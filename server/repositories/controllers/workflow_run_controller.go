package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkflowRunState 是同一运行在一个事务提交的持久状态集合。
type WorkflowRunState struct {
	// Run 是租约和版本的聚合根。
	Run models.WorkflowRun `json:"run"`
	// Fibers 是独立的持久指令帧。
	Fibers []models.WorkflowFiber `json:"fibers"`
	// Groups 是并行或管道实例。
	Groups []models.WorkflowGroupRun `json:"groups"`
	// Instances 是节点实际执行记录。
	Instances []models.WorkflowNodeInstance `json:"instances"`
	// Effects 是准备或已经收到收据的宿主调用。
	Effects []models.WorkflowEffect `json:"effects"`
	// Humans 是业务人工请求。
	Humans []models.WorkflowHumanRequest `json:"humanRequests"`
	// Operations 是单独审批的宿主操作许可。
	Operations []models.WorkflowOperationRequest `json:"operationRequests"`
	// ActionCalls 是 handler 内真实工具步骤的持久回执。
	ActionCalls []models.WorkflowActionCall `json:"actionCalls"`
}

// WorkflowRunController 只暴露接纳、租约、快照提交和有界查询。
type WorkflowRunController interface {
	// FailChatRuns 随会话删除直接终结其来源运行及节点任务，返回需要停止的实际执行。
	FailChatRuns(context.Context, []string, int64) (WorkflowChatFailure, error)
	// OwnedInputRunIDs identifies the durable runs that own inputs left by interrupted admission.
	OwnedInputRunIDs(context.Context, string, []string) ([]string, error)
	// ListChatRunSources 按聊天与明确的发起回合读取运行，包括已移除的历史标记。
	ListChatRunSources(context.Context, string, []string) ([]models.WorkflowRun, error)
	// ListVisibleChatRunSources 在同一读事务内读取可用的来源运行窗口和总量。
	ListVisibleChatRunSources(ctx context.Context, chatID string, offset, limit int) ([]models.WorkflowRun, int64, error)
	// HasChatRunSources 按聊天身份判断是否存在来源运行，不建立进程内历史状态。
	HasChatRunSources(context.Context, string) (bool, error)
	// RequestTeamChatRunStop 按持久来源登记团队工作流停止，保留 Runner 原有租约。
	RequestTeamChatRunStop(ctx context.Context, threadID string, chatIDs []string, at int64) ([]models.WorkflowRun, error)
	// ListChatRunRequests 批量读取来源运行及其受管子流程的人工问题与授权。
	ListChatRunRequests(context.Context, []models.WorkflowRun, int64) (map[string][]WorkflowChatRequest, error)
	// NodeCounts 读取所有实际实例的按职责和状态分组计数，不用静态节点数估算进度。
	NodeCounts(context.Context, string) ([]WorkflowNodeCount, error)
	// CountReservedEffects 统计跨进程事务内预留而尚未终止的真实外部调用槽。
	CountReservedEffects(context.Context) (int64, error)
	// GetInstance 读取所属运行内的一份完整节点事实。
	GetInstance(context.Context, string, string) (models.WorkflowNodeInstance, error)
	// ListInstances 以持久创建顺序读取节点实例页及总数。
	ListInstances(context.Context, string, int, int) ([]models.WorkflowNodeInstance, int64, error)
	// ListChildren 读取父运行拥有的全部 joined 和 detached 子运行。
	ListChildren(context.Context, string) ([]models.WorkflowRun, error)
	// ListHumanRequests 读取有界人工请求和答案历史。
	ListHumanRequests(context.Context, string) ([]models.WorkflowHumanRequest, error)
	// ListOperations 读取有界宿主许可历史。
	ListOperations(context.Context, string) ([]models.WorkflowOperationRequest, error)
	// ListActiveWorkers 读取实际未终止的独立 Worker Chat，用于待工具授权状态。
	ListActiveWorkers(context.Context, string) ([]models.WorkflowNodeInstance, error)
	// ListActionCalls 读取一个效果拥有的有界内部工具回执。
	ListActionCalls(context.Context, string, string) ([]models.WorkflowActionCall, error)
	// CountTreeRuns 返回同一调用树的持久运行数量。
	CountTreeRuns(context.Context, string) (int64, error)
	// ListJoinedChildren 读取跟随父运行生命周期的子运行。
	ListJoinedChildren(context.Context, string) ([]models.WorkflowRun, error)
	// ListJoinedRetryConflicts 返回已有未知效果的 joined 分支入口。
	ListJoinedRetryConflicts(context.Context, models.WorkflowRun) ([]string, error)
	// ReleaseLeases 在关闭时释放当前服务的非终态租约。
	ReleaseLeases(context.Context, string, int64) error
	// CreateRun 在接纳事务中插入运行和根帧。
	CreateRun(context.Context, models.WorkflowRun, models.WorkflowFiber) error
	// GetEffect 读取 scoped 工具绑定的效果。
	GetEffect(context.Context, string) (models.WorkflowEffect, error)
	// ListChatAttempts 读取独立 worker Chat 绑定的效果，不读取消息或返回候选结果。
	ListChatAttempts(context.Context, string) ([]models.WorkflowEffect, error)
	// ListInstanceAttempts 按代际读取节点全部至多五次独立尝试，包含旧失败证据。
	ListInstanceAttempts(context.Context, string, string) ([]models.WorkflowEffect, error)
	// GetRun 读取稳定运行身份。
	GetRun(context.Context, string) (models.WorkflowRun, error)
	// ListRuns 读取同一定义的有界历史。
	ListRuns(context.Context, string, int64, string, int) ([]models.WorkflowRun, error)
	// DeleteRunHistory 移除已结束执行的历史入口，保留完整运行事实。
	DeleteRunHistory(context.Context, string, string, int64) error
	// ListRunnable 返回仍需调度或对账的有界运行。
	ListRunnable(context.Context, int) ([]models.WorkflowRun, error)
	// ListPendingChatDeliveries 返回终态后尚未结算附件的来源运行身份。
	ListPendingChatDeliveries(context.Context, string, int) ([]models.WorkflowRun, error)
	// FinishChatRunFiles 结算附件交付，并保存原快照验证新发现的错误。
	FinishChatRunFiles(ctx context.Context, chatID, runID, errorCode string, files map[string]models.WorkflowOutputFile, at int64) error
	// FinishChatRunSummary 记录原回合已完成的手动补写。
	FinishChatRunSummary(ctx context.Context, chatID, runID string, at int64) error
	// ListPendingConclusions 读取已提交终态且尚未结算回件的运行。
	ListPendingConclusions(context.Context, string, int) ([]models.WorkflowRun, error)
	// FinishConclusion 持久确认本次回件或记录无法送达的原因。
	FinishConclusion(ctx context.Context, runID, errorCode string, at int64) error
	// ListMainWorkflowRuns 返回一页主会话交办关联的可见运行。
	ListMainWorkflowRuns(context.Context, string, []string) ([]models.WorkflowRun, error)

	// AcquireLease 续期或取得过期租约，首次接管增加 fence。
	AcquireLease(context.Context, string, string, int64, int64) (models.WorkflowRun, bool, error)
	// LoadRunState 读取一个运行全部持久执行事实。
	LoadRunState(context.Context, string) (WorkflowRunState, error)
	// CommitRunState 只在租约、fence 和版本仍匹配时提交状态。
	CommitRunState(context.Context, WorkflowRunState, int64, int64) error
}

// WorkflowNodeCount 是完整运行事实的有界节点状态汇总。
type WorkflowNodeCount struct {
	// NodeID 是源码职责身份。
	NodeID string `json:"nodeId"`
	// Status 是实际实例状态。
	Status constants.WorkflowStepStatus `json:"status"`
	// Count 是这一状态下的实际实例数量。
	Count int `json:"count"`
	// InstanceID 指向本组一份可读取的真实实例。
	InstanceID string `json:"instanceId"`
}

func (c *workflowRunController) NodeCounts(ctx context.Context, runID string) ([]WorkflowNodeCount, error) {
	values := []WorkflowNodeCount{}
	if err := controllerDB(ctx, c.db).Model(&models.WorkflowNodeInstance{}).Select("node_id,status,COUNT(*) AS count,MIN(id) AS instance_id").Where("run_id = ?", runID).Group("node_id,status").Order("node_id ASC,status ASC").Limit(10000).Find(&values).Error; err != nil {
		return nil, databaseError("workflow_node_counts", runID, err)
	}
	return values, nil
}

func (c *workflowRunController) CountReservedEffects(ctx context.Context) (int64, error) {
	var count int64
	err := controllerDB(ctx, c.db).Model(&models.WorkflowEffect{}).Where("slot_reserved = ? AND status IN ?", true, []constants.WorkflowStepStatus{constants.WorkflowStepStatusPrepared, constants.WorkflowStepStatusRunning, constants.WorkflowStepStatusUnknown}).Count(&count).Error
	if err != nil {
		return 0, databaseError("count_reserved_workflow_effects", "", err)
	}
	return count, nil
}

func (c *workflowRunController) ListActiveWorkers(ctx context.Context, runID string) ([]models.WorkflowNodeInstance, error) {
	values := []models.WorkflowNodeInstance{}
	if err := controllerDB(ctx, c.db).Select("id", "chat_id", "execution_id").Where("run_id = ? AND kind = ? AND chat_id <> '' AND completed_at IS NULL", runID, "worker").Limit(32).Find(&values).Error; err != nil {
		return nil, databaseError("list_active_workflow_workers", runID, err)
	}
	return values, nil
}

func (c *workflowRunController) ListInstances(ctx context.Context, id string, limit, offset int) ([]models.WorkflowNodeInstance, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	var total int64
	db := controllerDB(ctx, c.db).Model(&models.WorkflowNodeInstance{}).Where("run_id = ?", id)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_workflow_instances", id, err)
	}
	values := []models.WorkflowNodeInstance{}
	if err := db.Omit("input", "options", "output", "failure").Order("created_at ASC, id ASC").Limit(limit).Offset(offset).Find(&values).Error; err != nil {
		return nil, total, databaseError("list_workflow_instances", id, err)
	}
	return values, total, nil
}

func (c *workflowRunController) GetInstance(ctx context.Context, runID, instanceID string) (models.WorkflowNodeInstance, error) {
	var value models.WorkflowNodeInstance
	err := controllerDB(ctx, c.db).Where("id = ? AND run_id = ?", instanceID, runID).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, status_error.WorkflowNotFound
	}
	if err != nil {
		return value, databaseError("get_workflow_instance", runID, err)
	}
	return value, nil
}
func (c *workflowRunController) ListChildren(ctx context.Context, id string) ([]models.WorkflowRun, error) {
	values := []models.WorkflowRun{}
	if err := controllerDB(ctx, c.db).Where("parent_run_id = ?", id).Order("created_at ASC, id ASC").Limit(constants.WorkflowMaxChildren).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_children", id, err)
	}
	return values, nil
}
func (c *workflowRunController) ListHumanRequests(ctx context.Context, id string) ([]models.WorkflowHumanRequest, error) {
	values := []models.WorkflowHumanRequest{}
	if err := controllerDB(ctx, c.db).Where("run_id = ?", id).Order("CASE WHEN status = 'waiting' THEN 0 ELSE 1 END, created_at DESC, id ASC").Limit(200).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_humans", id, err)
	}
	return values, nil
}
func (c *workflowRunController) ListOperations(ctx context.Context, id string) ([]models.WorkflowOperationRequest, error) {
	values := []models.WorkflowOperationRequest{}
	if err := controllerDB(ctx, c.db).Where("run_id = ?", id).Order("CASE WHEN status = 'waiting' THEN 0 ELSE 1 END, id ASC").Limit(200).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_operations", id, err)
	}
	return values, nil
}
func (c *workflowRunController) ListActionCalls(ctx context.Context, runID, effectID string) ([]models.WorkflowActionCall, error) {
	values := []models.WorkflowActionCall{}
	if err := controllerDB(ctx, c.db).Where("run_id = ? AND effect_id = ?", runID, effectID).Order("id ASC").Limit(200).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_action_calls", runID, err)
	}
	return values, nil
}

func (c *workflowRunController) CountTreeRuns(ctx context.Context, id string) (int64, error) {
	var count int64
	if err := controllerDB(ctx, c.db).Model(&models.WorkflowRun{}).Where("root_run_id = ?", id).Count(&count).Error; err != nil {
		return 0, databaseError("count_workflow_tree", id, err)
	}
	return count, nil
}
func (c *workflowRunController) ListJoinedChildren(ctx context.Context, id string) ([]models.WorkflowRun, error) {
	values := []models.WorkflowRun{}
	if err := controllerDB(ctx, c.db).Where("parent_run_id = ? AND joined = ?", id, true).Order("created_at ASC, id ASC").Limit(100).Find(&values).Error; err != nil {
		return nil, databaseError("list_joined_workflow_children", id, err)
	}
	return values, nil
}

func (c *workflowRunController) ListJoinedRetryConflicts(ctx context.Context, source models.WorkflowRun) ([]string, error) {
	rootID := source.RootRunID
	if rootID == "" {
		rootID = source.ID
	}
	db := controllerDB(ctx, c.db)
	var links []models.WorkflowRun
	if err := db.Select("id", "parent_run_id").Where("root_run_id = ? AND joined = ?", rootID, true).
		Limit(constants.WorkflowMaxChildren + 1).Find(&links).Error; err != nil {
		return nil, databaseError("list_workflow_retry_tree", rootID, err)
	}
	if len(links) > constants.WorkflowMaxChildren {
		return nil, status_error.WorkflowStateInvalid
	}
	children := make(map[string][]string)
	for _, link := range links {
		children[link.ParentRunID] = append(children[link.ParentRunID], link.ID)
	}
	var descendantIDs []string
	branches := map[string]string{source.ID: ""}
	var collect func(string, string)
	collect = func(id, branch string) {
		if _, seen := branches[id]; seen {
			return
		}
		branches[id] = branch
		descendantIDs = append(descendantIDs, id)
		for _, child := range children[id] {
			collect(child, branch)
		}
	}
	for _, branch := range children[source.ID] {
		collect(branch, branch)
	}
	if len(descendantIDs) == 0 {
		return nil, nil
	}
	var uncertainEffects []models.WorkflowEffect
	if err := db.Select("DISTINCT run_id").Where("run_id IN ? AND status = ?", descendantIDs, constants.WorkflowStepStatusUnknown).Find(&uncertainEffects).Error; err != nil {
		return nil, databaseError("list_workflow_retry_unknown_effects", rootID, err)
	}
	var uncertainCalls []models.WorkflowActionCall
	if err := db.Select("DISTINCT run_id").Where("run_id IN ? AND status = ?", descendantIDs, constants.WorkflowStepStatusUnknown).Find(&uncertainCalls).Error; err != nil {
		return nil, databaseError("list_workflow_retry_unknown_calls", rootID, err)
	}
	var blocked []string
	for _, effect := range uncertainEffects {
		blocked = append(blocked, branches[effect.RunID])
	}
	for _, call := range uncertainCalls {
		blocked = append(blocked, branches[call.RunID])
	}
	return uniqueRowIDs(blocked), nil
}
func (c *workflowRunController) ReleaseLeases(ctx context.Context, owner string, now int64) error {
	if err := controllerDB(ctx, c.db).Model(&models.WorkflowRun{}).Where("lease_owner = ?", owner).Update("lease_until", now).Error; err != nil {
		return databaseError("release_workflow_leases", owner, err)
	}
	return nil
}

// workflowRunController 让所有查询加入上层统一事务。
type workflowRunController struct {
	// db 是默认连接。
	db *gorm.DB
}

// NewWorkflowRunController 构造运行持久化边界。
func NewWorkflowRunController(db *gorm.DB) WorkflowRunController {
	return &workflowRunController{db: db}
}

func (c *workflowRunController) OwnedInputRunIDs(ctx context.Context, definitionID string, ids []string) ([]string, error) {
	var owned []string
	for batch := range slices.Chunk(uniqueRowIDs(ids), 128) {
		var found []string
		if err := controllerDB(ctx, c.db).Model(&models.WorkflowRun{}).Select("id").
			Where("definition_id = ? AND id IN ?", definitionID, batch).Find(&found).Error; err != nil {
			return nil, databaseError("read_workflow_input_owners", definitionID, err)
		}
		owned = append(owned, found...)
	}
	return owned, nil
}

func (c *workflowRunController) CreateRun(ctx context.Context, run models.WorkflowRun, fiber models.WorkflowFiber) error {
	db := controllerDB(ctx, c.db)
	if err := db.Create(&run).Error; err != nil {
		return databaseError("create_workflow_run", run.ID, err)
	}
	if err := db.Create(&fiber).Error; err != nil {
		return databaseError("create_workflow_fiber", fiber.ID, err)
	}
	if err := c.pruneRunHistory(ctx, run.DefinitionID, time.Now().UnixMilli()); err != nil {
		return err
	}
	publishChatWorkflowChanges(ctx, c.db, []models.WorkflowRun{run})
	if err := c.publishMainWorkflowAdmission(ctx, run); err != nil {
		return err
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("workflow_run", run.ID))
	return nil
}
func (c *workflowRunController) GetRun(ctx context.Context, id string) (models.WorkflowRun, error) {
	var value models.WorkflowRun
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, status_error.WorkflowNotFound
	}
	if err != nil {
		return value, databaseError("get_workflow_run", id, err)
	}
	return value, nil
}
func (c *workflowRunController) ListRuns(ctx context.Context, definitionID string, before int64, beforeID string, limit int) ([]models.WorkflowRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	db := controllerDB(ctx, c.db).Where("definition_id = ? AND history_deleted_at IS NULL", definitionID)
	if before > 0 {
		db = db.Where("created_at < ? OR (created_at = ? AND id < ?)", before, before, beforeID)
	}
	values := []models.WorkflowRun{}
	if err := db.Order("created_at DESC, id DESC").Limit(limit).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_runs", definitionID, err)
	}
	return values, nil
}

// DeleteRunHistory 仅移除终态运行的历史入口，不改写聚合版本或活动时间。
func (c *workflowRunController) DeleteRunHistory(ctx context.Context, definitionID, runID string, removedAt int64) error {
	statuses := []constants.WorkflowRunStatus{constants.WorkflowRunStatusSucceeded, constants.WorkflowRunStatusFailed, constants.WorkflowRunStatusCancelled}
	result := controllerDB(ctx, c.db).Model(&models.WorkflowRun{}).Where("definition_id = ? AND id = ? AND history_deleted_at IS NULL AND status IN ?", definitionID, runID, statuses).UpdateColumn("history_deleted_at", removedAt)
	if err := result.Error; err != nil {
		return databaseError("delete_workflow_run_history", runID, err)
	}
	if _, enabled := c.db.Get(eventPublisherSetting); enabled && result.RowsAffected > 0 {
		runs, err := readRelatedRows[models.WorkflowRun](controllerDB(ctx, c.db), "id", "id,parent_run_id,joined,source_chat_id", []string{runID})
		if err == nil {
			publishChatWorkflowChanges(ctx, c.db, runs)
		}
	}
	publishCommittedChange(ctx, c.db, "work-board", eventsResource("global", ""))
	return nil
}

// pruneRunHistory 与接纳或终态提交共用事务，超限时移除最早一半，活跃执行始终保留。
func (c *workflowRunController) pruneRunHistory(ctx context.Context, definitionID string, removedAt int64) error {
	db := controllerDB(ctx, c.db)
	statuses := []constants.WorkflowRunStatus{constants.WorkflowRunStatusSucceeded, constants.WorkflowRunStatusFailed, constants.WorkflowRunStatusCancelled}
	removed := 0
	for {
		var count int64
		if err := db.Model(&models.WorkflowRun{}).Where("definition_id = ? AND history_deleted_at IS NULL", definitionID).Count(&count).Error; err != nil {
			return databaseError("count_workflow_run_history", definitionID, err)
		}
		if count <= constants.WorkflowRunHistoryLimit {
			break
		}
		var oldest []models.WorkflowRun
		if err := db.Select("id,parent_run_id,joined,source_chat_id").Where("definition_id = ? AND history_deleted_at IS NULL AND status IN ?", definitionID, statuses).Order("created_at ASC, id ASC").Limit(int(count / 2)).Find(&oldest).Error; err != nil {
			return databaseError("select_workflow_run_history", definitionID, err)
		}
		if len(oldest) == 0 {
			break
		}
		ids := make([]string, len(oldest))
		for index, run := range oldest {
			ids[index] = run.ID
		}
		result := db.Model(&models.WorkflowRun{}).Where("id IN ? AND history_deleted_at IS NULL AND status IN ?", ids, statuses).UpdateColumn("history_deleted_at", removedAt)
		if result.Error != nil {
			return databaseError("prune_workflow_run_history", definitionID, result.Error)
		}
		if result.RowsAffected == 0 {
			break
		}
		publishChatWorkflowChanges(ctx, c.db, oldest)
		removed += int(result.RowsAffected)
	}
	if removed == 0 {
		return nil
	}
	_, err := NewWorkflowController(c.db).AppendEvent(ctx, models.WorkflowEvent{StreamID: definitionID, EventID: util.New(), Type: constants.WorkflowRunHistoryPrunedEvent, EntityID: definitionID, Payload: map[string]any{"removedCount": removed}})
	return err
}

func (c *workflowRunController) ListRunnable(ctx context.Context, limit int) ([]models.WorkflowRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	values := []models.WorkflowRun{}
	statuses := []constants.WorkflowRunStatus{
		constants.WorkflowRunStatusQueued, constants.WorkflowRunStatusRunning,
		constants.WorkflowRunStatusWaiting, constants.WorkflowRunStatusRecovering,
		constants.WorkflowRunStatusCancelling,
	}
	if err := controllerDB(ctx, c.db).Where("status IN ?", statuses).Order("updated_at ASC, id ASC").Limit(limit).Find(&values).Error; err != nil {
		return nil, databaseError("list_runnable_workflows", "", err)
	}
	return values, nil
}

func (c *workflowRunController) ListPendingChatDeliveries(ctx context.Context, after string, limit int) ([]models.WorkflowRun, error) {
	values := []models.WorkflowRun{}
	statuses := []constants.WorkflowRunStatus{constants.WorkflowRunStatusSucceeded, constants.WorkflowRunStatusFailed, constants.WorkflowRunStatusCancelled}
	err := controllerDB(ctx, c.db).Select("id", "source_chat_id").
		Where("source_chat_id <> '' AND source_workbench = false AND id > ? AND status IN ?", after, statuses).
		Where("chat_delivery_finished_at IS NULL").
		Order("id ASC").Limit(limit).Find(&values).Error
	if err != nil {
		return nil, databaseError("list_pending_chat_workflow_deliveries", "", err)
	}
	return values, nil
}

func (c *workflowRunController) FinishChatRunFiles(ctx context.Context, chatID, runID, errorCode string, files map[string]models.WorkflowOutputFile, at int64) error {
	fields := map[string]any{"chat_delivery_finished_at": at, "chat_delivery_error": errorCode}
	if files != nil {
		encoded, err := json.Marshal(files)
		if err != nil {
			return databaseError("encode_workflow_chat_files", runID, err)
		}
		fields["chat_files"] = string(encoded)
	}
	return c.finishChatRunDelivery(ctx, chatID, runID, fields)
}

func (c *workflowRunController) FinishChatRunSummary(ctx context.Context, chatID, runID string, at int64) error {
	return c.finishChatRunDelivery(ctx, chatID, runID, map[string]any{"chat_summary_finished_at": at})
}

func (c *workflowRunController) finishChatRunDelivery(ctx context.Context, chatID, runID string, fields map[string]any) error {
	result := controllerDB(ctx, c.db).Model(&models.WorkflowRun{}).
		Where("id = ? AND source_chat_id = ? AND source_workbench = false", runID, chatID).
		UpdateColumns(fields)
	if result.Error != nil {
		return databaseError("finish_chat_workflow_delivery", runID, result.Error)
	}
	if result.RowsAffected > 0 {
		publishCommittedChange(ctx, c.db, "workflow", eventsResource("chat", chatID))
	}
	return nil
}
func (c *workflowRunController) AcquireLease(ctx context.Context, id, owner string, now, until int64) (models.WorkflowRun, bool, error) {
	db := controllerDB(ctx, c.db)
	update := db.Model(&models.WorkflowRun{}).Where("id = ? AND lease_owner = ? AND lease_until > ?", id, owner, now).Updates(map[string]any{"lease_until": until})
	if update.Error != nil {
		return models.WorkflowRun{}, false, databaseError("renew_workflow_lease", id, update.Error)
	}
	if update.RowsAffected == 0 {
		update = db.Model(&models.WorkflowRun{}).Where("id = ? AND lease_until <= ?", id, now).Updates(map[string]any{"lease_owner": owner, "lease_until": until, "fence": gorm.Expr("fence + 1")})
		if update.Error != nil {
			return models.WorkflowRun{}, false, databaseError("acquire_workflow_lease", id, update.Error)
		}
	}
	if update.RowsAffected == 0 {
		return models.WorkflowRun{}, false, nil
	}
	run, err := c.GetRun(ctx, id)
	return run, true, err
}
func (c *workflowRunController) LoadRunState(ctx context.Context, id string) (WorkflowRunState, error) {
	run, err := c.GetRun(ctx, id)
	if err != nil {
		return WorkflowRunState{}, err
	}
	value := WorkflowRunState{Run: run, Fibers: []models.WorkflowFiber{}, Groups: []models.WorkflowGroupRun{}, Instances: []models.WorkflowNodeInstance{}, Effects: []models.WorkflowEffect{}, Humans: []models.WorkflowHumanRequest{}}
	value.Operations = []models.WorkflowOperationRequest{}
	value.ActionCalls = []models.WorkflowActionCall{}
	for _, table := range []any{&value.Fibers, &value.Groups, &value.Instances, &value.Effects, &value.Humans, &value.Operations, &value.ActionCalls} {
		if err := controllerDB(ctx, c.db).Where("run_id = ?", id).Order("id ASC").Find(table).Error; err != nil {
			return value, databaseError("load_workflow_run_state", id, err)
		}
	}
	return value, nil
}
func (c *workflowRunController) CommitRunState(ctx context.Context, value WorkflowRunState, expected, now int64) error {
	publish := c.workflowAttentionChanges(ctx, value)
	db := controllerDB(ctx, c.db)
	run := value.Run
	run.Version = expected + 1
	// Updates(struct) 在 GORM 1.31 的 nil interface serializer 路径会调用空扫描器。
	// 只在 Repository 更新边界编码这两个可空列；Model 仍使用 serializer:json 读取与创建。
	output, err := json.Marshal(run.Result)
	if err != nil {
		return databaseError("encode_workflow_output", run.ID, err)
	}
	failure, err := json.Marshal(run.Failure)
	if err != nil {
		return databaseError("encode_workflow_failure", run.ID, err)
	}
	fields := map[string]any{"status": run.Status, "result": string(output), "failure": string(failure), "version": run.Version, "completed_at": run.CompletedAt}
	if len(run.OutputFiles) > 0 {
		files, err := json.Marshal(run.OutputFiles)
		if err != nil {
			return databaseError("encode_workflow_output_files", run.ID, err)
		}
		fields["chat_files"] = string(files)
	}
	result := db.Model(&models.WorkflowRun{}).Where("id = ? AND version = ? AND lease_owner = ? AND fence = ? AND lease_until > ?", run.ID, expected, run.LeaseOwner, run.Fence, now).Updates(fields)
	if result.Error != nil {
		return databaseError("commit_workflow_run", run.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return status_error.WorkflowFenceConflict
	}
	// 只有租约条件提交成功的事务才写子记录；唯一身份限制 upsert 不扩大聚合范围。
	collections := []any{&value.Fibers, &value.Groups, &value.Instances, &value.Effects, &value.Humans, &value.Operations, &value.ActionCalls}
	columns := [][]string{
		{"state", "status", "boundary", "waiting_id", "stage_index"},
		{"results", "finished", "status", "failure"},
		{"status", "chat_id", "execution_id", "effect_id", "attempt_generation", "output", "failure", "completed_at", "reused_from"},
		{"status", "chat_id", "final_message_id", "execution_status", "child_run_id", "candidate", "result", "failure", "deadline_at", "builtin_state", "slot_reserved", "skip_requested", "output_repairs"},
		{"status", "value", "source", "answered_at"},
		{"status", "allowed", "answered_at"},
		{"status", "result", "failure"},
	}
	for index, collection := range collections {
		switch index {
		case 0:
			if len(value.Fibers) == 0 {
				continue
			}
		case 1:
			if len(value.Groups) == 0 {
				continue
			}
		case 2:
			if len(value.Instances) == 0 {
				continue
			}
		case 3:
			if len(value.Effects) == 0 {
				continue
			}
		case 4:
			if len(value.Humans) == 0 {
				continue
			}
		case 5:
			if len(value.Operations) == 0 {
				continue
			}
		case 6:
			if len(value.ActionCalls) == 0 {
				continue
			}
		}
		if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns(columns[index])}).Create(collection).Error; err != nil {
			return databaseError("commit_workflow_execution_facts", run.ID, err)
		}
	}
	terminal := run.Status == constants.WorkflowRunStatusSucceeded || run.Status == constants.WorkflowRunStatusFailed || run.Status == constants.WorkflowRunStatusCancelled
	if terminal {
		if err := c.pruneRunHistory(ctx, run.DefinitionID, now); err != nil {
			return err
		}
	}
	publish()
	return nil
}

func (c *workflowRunController) GetEffect(ctx context.Context, id string) (models.WorkflowEffect, error) {
	var value models.WorkflowEffect
	err := controllerDB(ctx, c.db).Where("id = ?", id).First(&value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return value, status_error.WorkflowNotFound
	}
	if err != nil {
		return value, databaseError("get_workflow_effect", id, err)
	}
	return value, nil
}

func (c *workflowRunController) ListInstanceAttempts(ctx context.Context, runID, instanceID string) ([]models.WorkflowEffect, error) {
	values := []models.WorkflowEffect{}
	if err := controllerDB(ctx, c.db).Where("run_id = ? AND instance_id = ?", runID, instanceID).Order("attempt_generation ASC").Limit(5).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_attempts", instanceID, err)
	}
	return values, nil
}

func (c *workflowRunController) ListChatAttempts(ctx context.Context, chatID string) ([]models.WorkflowEffect, error) {
	values := []models.WorkflowEffect{}
	if err := controllerDB(ctx, c.db).Where("chat_id = ?", chatID).Order("created_at ASC").Limit(5).Find(&values).Error; err != nil {
		return nil, databaseError("list_workflow_chat_attempts", chatID, err)
	}
	return values, nil
}
