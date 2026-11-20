package controllers

import (
	"context"
	"sort"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// WorkflowChatRequest 保留实际节点的请求身份，聊天和画布提交到同一入口。
type WorkflowChatRequest struct {
	// RunID 是实际提出问题或许可的运行。
	RunID string `json:"runId"`
	// DefinitionID 是实际运行的定义。
	DefinitionID string `json:"definitionId"`
	// InstanceID 是当前节点实例。
	InstanceID string `json:"instanceId"`
	// RequestID 是原请求身份。
	RequestID string `json:"requestId"`
	// Kind 区分业务问题、宿主许可及 Worker 的两类交互。
	Kind string `json:"kind"`
	// ChatID 指向 Worker 对话。
	ChatID string `json:"chatId,omitempty"`
	// Human 是冻结的业务问题及答案。
	Human *models.WorkflowHumanRequest `json:"human,omitempty"`
	// Operation 是供用户核对的操作许可。
	Operation *models.WorkflowOperationRequest `json:"operation,omitempty"`
	// Active 表示当前运行、祖先与请求状态均允许处理。
	Active bool `json:"-"`
	// createdAt 用于多个分支同时提问时稳定排序。
	createdAt int64
}

const chatRunFields = "id,definition_id,revision_id,source_chat_id,source_execution_id,source_message_id,source_name,source_run_id,source_workbench,chat_files,chat_delivery_error,chat_summary_finished_at,source_waker_id,source_thread_id,status,created_at,completed_at,history_deleted_at,root_run_id,parent_run_id,joined,deadline_at"

func (c *workflowRunController) ListChatRunOrigins(ctx context.Context) ([]models.WorkflowRun, error) {
	rows := []models.WorkflowRun{}
	terminal := []constants.WorkflowRunStatus{constants.WorkflowRunStatusSucceeded, constants.WorkflowRunStatusFailed, constants.WorkflowRunStatusCancelled}
	err := controllerDB(ctx, c.db).Select("DISTINCT source_chat_id, CASE WHEN source_workbench = false AND status NOT IN ? THEN source_execution_id ELSE '' END AS source_execution_id", terminal).
		Where("source_chat_id <> '' AND source_execution_id <> ''").Find(&rows).Error
	if err != nil {
		return nil, databaseError("list_chat_workflow_origins", "", err)
	}
	return rows, nil
}

func (c *workflowRunController) ListChatRunSources(ctx context.Context, chatID string, executionIDs []string) ([]models.WorkflowRun, error) {
	rows := []models.WorkflowRun{}
	ids := uniqueRowIDs(executionIDs)
	if chatID == "" || len(ids) == 0 {
		return rows, nil
	}
	query := controllerDB(ctx, c.db).Select(chatRunFields).Where("source_chat_id = ? AND source_execution_id IN ?", chatID, ids)
	if err := query.Order("created_at DESC,id DESC").Find(&rows).Error; err != nil {
		return nil, databaseError("list_chat_workflow_sources", chatID, err)
	}
	return rows, nil
}

func (c *workflowRunController) ListVisibleChatRunSources(ctx context.Context, chatID string, offset, limit int) ([]models.WorkflowRun, int64, error) {
	rows := []models.WorkflowRun{}
	if chatID == "" {
		return rows, 0, nil
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, status_error.BadRequest
	}
	query := controllerDB(ctx, c.db).Model(&models.WorkflowRun{}).
		Where("source_chat_id = ? AND history_deleted_at IS NULL", chatID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, databaseError("count_visible_chat_workflow_sources", chatID, err)
	}
	if int64(offset) >= total {
		return rows, total, nil
	}
	if err := query.Select("id,definition_id,revision_id,source_name,status,created_at").
		Order("created_at DESC,id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, databaseError("list_visible_chat_workflow_sources", chatID, err)
	}
	return rows, total, nil
}

// teamChatRuns 限定由团队聊天接纳、会随团队工作一起停止的运行。
func teamChatRuns(db *gorm.DB) *gorm.DB {
	return db.Model(&models.WorkflowRun{}).
		Where("source_chat_id <> '' AND source_execution_id <> '' AND source_workbench = false")
}

// countActiveTeamChatRuns 在终态前均保留工作所有权，包括取消收尾。
func countActiveTeamChatRuns(scope *gorm.DB, id string) (int64, error) {
	var count int64
	terminal := []constants.WorkflowRunStatus{constants.WorkflowRunStatusSucceeded, constants.WorkflowRunStatusFailed, constants.WorkflowRunStatusCancelled}
	if err := teamChatRuns(scope).Where("status NOT IN ?", terminal).Count(&count).Error; err != nil {
		return 0, databaseError("count_active_team_chat_workflow_runs", id, err)
	}
	return count, nil
}

func (c *workflowRunController) RequestTeamChatRunStop(ctx context.Context, threadID string, chatIDs []string, at int64) ([]models.WorkflowRun, error) {
	if len(chatIDs) == 0 {
		return nil, nil
	}
	statuses := []constants.WorkflowRunStatus{constants.WorkflowRunStatusQueued, constants.WorkflowRunStatusRunning, constants.WorkflowRunStatusWaiting, constants.WorkflowRunStatusRecovering}
	scope := func() *gorm.DB {
		return teamChatRuns(controllerDB(ctx, c.db)).
			Where("source_thread_id = ? AND source_chat_id IN ? AND status IN ?", threadID, chatIDs, statuses)
	}
	const batchSize = 128
	var stopped []models.WorkflowRun
	for {
		var rows []models.WorkflowRun
		if err := scope().Select("id", "definition_id", "source_chat_id", "version").Order("id").Limit(batchSize).Find(&rows).Error; err != nil {
			return nil, databaseError("read_thread_workflow_stop_targets", threadID, err)
		}
		if len(rows) == 0 {
			break
		}
		ids := make([]string, len(rows))
		for i := range rows {
			ids[i] = rows[i].ID
		}
		// 版本推进使旧 Runner 的在途提交失效，下一次由原租约或正常恢复推进停止。
		result := scope().Where("id IN ?", ids).UpdateColumns(map[string]any{"status": constants.WorkflowRunStatusCancelling, "updated_at": at, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return nil, databaseError("request_thread_workflow_stop", threadID, result.Error)
		}
		if result.RowsAffected != int64(len(rows)) {
			return nil, status_error.WorkflowFenceConflict
		}
		for i := range rows {
			rows[i].Status = constants.WorkflowRunStatusCancelling
			rows[i].Version++
			publishCommittedChange(ctx, c.db, "work-board", eventsResource("workflow_run", rows[i].ID))
		}
		stopped = append(stopped, rows...)
		if len(rows) < batchSize {
			break
		}
	}
	if len(stopped) > 0 {
		publishChatWorkflowChanges(ctx, c.db, stopped)
		publishWorkRequestRecheck(ctx, c.db, eventsResource("global", ""))
	}
	return stopped, nil
}

// ListChatRunRequests 用来源运行缩小读取范围；分离的子流程有自己的待办归属。
func (c *workflowRunController) ListChatRunRequests(ctx context.Context, roots []models.WorkflowRun, at int64) (map[string][]WorkflowChatRequest, error) {
	result := map[string][]WorkflowChatRequest{}
	runs := map[string]models.WorkflowRun{}
	rootIDs := []string{}
	for _, r := range roots {
		if r.HistoryDeletedAt == nil {
			rootIDs = append(rootIDs, r.ID)
			runs[r.ID] = r
		}
	}
	db := controllerDB(ctx, c.db)
	children, err := readRelatedRows[models.WorkflowRun](db.Where("parent_run_id <> ''"), "root_run_id", "id,definition_id,root_run_id,parent_run_id,joined,status,deadline_at,history_deleted_at", rootIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range children {
		runs[r.ID] = r
	}
	owners := map[string]string{}
	ids := []string{}
	live := []string{}
	for _, r := range runs {
		if r.HistoryDeletedAt != nil {
			continue
		}
		root := visibleWorkflowRun(r, runs)
		if root.SourceChatID == "" {
			continue
		}
		owners[r.ID] = root.ID
		ids = append(ids, r.ID)
		if boardWorkflowAlive(r, runs, at) {
			live = append(live, r.ID)
		}
	}
	add := func(q WorkflowChatRequest) {
		q.DefinitionID = runs[q.RunID].DefinitionID
		root := owners[q.RunID]
		result[root] = append(result[root], q)
	}
	humans, err := readRelatedRows[models.WorkflowHumanRequest](db, "run_id", "*", ids)
	if err != nil {
		return nil, err
	}
	for i := range humans {
		q := &humans[i]
		active := q.Status == constants.WorkflowStepStatusWaiting
		if active && (!boardWorkflowAlive(runs[q.RunID], runs, at) || (q.DeadlineAt != 0 && q.DeadlineAt <= at)) {
			continue
		}
		add(WorkflowChatRequest{RunID: q.RunID, InstanceID: q.InstanceID, RequestID: q.ID, Kind: "human", Human: q, Active: active, createdAt: q.CreatedAt})
	}
	operations, err := readRelatedRows[models.WorkflowOperationRequest](db, "run_id", "*", ids)
	if err != nil {
		return nil, err
	}
	for i := range operations {
		q := &operations[i]
		active := q.Status == constants.WorkflowStepStatusWaiting
		if active && !boardWorkflowAlive(runs[q.RunID], runs, at) {
			continue
		}
		add(WorkflowChatRequest{RunID: q.RunID, InstanceID: q.InstanceID, RequestID: q.ID, Kind: "operation", Operation: q, Active: active, createdAt: q.CreatedAt})
	}
	if err := chatWorkflowWorkerRequests(db, live, add); err != nil {
		return nil, err
	}
	for _, items := range result {
		sort.Slice(items, func(i, j int) bool {
			if items[i].createdAt == items[j].createdAt {
				return items[i].RequestID < items[j].RequestID
			}
			return items[i].createdAt < items[j].createdAt
		})
	}
	return result, nil
}

func chatWorkflowWorkerRequests(db *gorm.DB, live []string, add func(WorkflowChatRequest)) error {
	effects, err := readRelatedRows[models.WorkflowEffect](db.Where("execution_id<>'' AND (chat_id<>'' OR ("+unboundWorkerEffect+"))"), "run_id", "chat_id,execution_id,instance_id,run_id", live)
	if err != nil {
		return err
	}
	byExecution := map[string]models.WorkflowEffect{}
	ids := []string{}
	for _, effect := range effects {
		ids = append(ids, effect.ExecutionID)
		byExecution[effect.ExecutionID] = effect
	}
	chats, err := readRelatedRows[models.Chat](db.Where(pendingChatPredicate), "active_execution_id", "id,active_execution_id,waiting_approval,waiting_elicitation,pending_approvals,pending_elicitations", ids)
	if err != nil {
		return err
	}
	for _, chat := range chats {
		effect := byExecution[chat.ActiveExecutionID]
		if effect.ChatID != "" && effect.ChatID != chat.ID {
			continue
		}
		if chat.WaitingApproval {
			for _, q := range chat.PendingApprovals {
				add(WorkflowChatRequest{RunID: effect.RunID, InstanceID: effect.InstanceID, RequestID: q.ApprovalID, Kind: "approval", ChatID: chat.ID, Active: true, createdAt: q.RequestedAt})
			}
		}
		if chat.WaitingElicitation {
			for _, q := range chat.PendingElicitations {
				add(WorkflowChatRequest{RunID: effect.RunID, InstanceID: effect.InstanceID, RequestID: q.ElicitationID, Kind: "elicitation", ChatID: chat.ID, Active: true, createdAt: q.RequestedAt})
			}
		}
	}
	return nil
}

// publishChatWorkflowChanges 沿 joined 父级定位发起聊天；独立子流程保留独立归属。
func publishChatWorkflowChanges(ctx context.Context, db *gorm.DB, runs []models.WorkflowRun) {
	if _, ok := db.Get(eventPublisherSetting); !ok {
		return
	}
	chatIDs := []string{}
	parents := []string{}
	for _, run := range runs {
		if run.SourceChatID != "" {
			chatIDs = append(chatIDs, run.SourceChatID)
		} else if run.Joined && run.ParentRunID != "" {
			parents = append(parents, run.ParentRunID)
		}
	}
	if len(parents) > 0 {
		ancestors, err := boardWorkflowRuns(controllerDB(ctx, db), parents)
		if err == nil {
			for _, id := range parents {
				chatIDs = append(chatIDs, visibleWorkflowRun(ancestors[id], ancestors).SourceChatID)
			}
		}
	}
	for _, id := range uniqueRowIDs(chatIDs) {
		publishCommittedChange(ctx, db, "workflow", eventsResource("chat", id))
	}
}

// publishWorkerChatWorkflowChanges 在真实 Worker 的待答集合变化时通知来源聊天。
func publishWorkerChatWorkflowChanges(ctx context.Context, db *gorm.DB, chatID string, executionIDs []string) {
	ids := uniqueRowIDs(executionIDs)
	if len(ids) == 0 {
		return
	}
	var effects []models.WorkflowEffect
	query := controllerDB(ctx, db)
	if err := query.Select("run_id").Where("execution_id IN ? AND (chat_id=? OR ("+unboundWorkerEffect+"))", ids, chatID).Find(&effects).Error; err != nil {
		_ = databaseError("read_chat_workflow_wait_sources", chatID, err)
		return
	}
	runIDs := make([]string, 0, len(effects))
	for _, effect := range effects {
		runIDs = append(runIDs, effect.RunID)
	}
	runs, err := readRelatedRows[models.WorkflowRun](query, "id", "id,parent_run_id,joined,source_chat_id", runIDs)
	if err == nil {
		publishChatWorkflowChanges(ctx, db, runs)
	}
}
