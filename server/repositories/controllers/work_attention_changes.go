package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// chatAttentionChanges 在投影 CAS 前校验待办数据，并为已装配的事件通道捕获语义差异。
func (c *chatController) chatAttentionChanges(ctx context.Context, id string, fields map[string]any) (func(), error) {
	var approvals []models.PendingApproval
	approvalField, hasApprovals := fields["pending_approvals"]
	if hasApprovals {
		if err := decodeAttentionField(approvalField, &approvals); err != nil {
			logx.Errorf("decode chat pending approvals: chat_id=%s error=%v", id, err)
			return nil, status_error.ChatLogIndexCorrupted
		}
	}
	var elicitations []models.PendingElicitation
	elicitationField, hasElicitations := fields["pending_elicitations"]
	if hasElicitations {
		if err := decodeAttentionField(elicitationField, &elicitations); err != nil {
			logx.Errorf("decode chat pending elicitations: chat_id=%s error=%v", id, err)
			return nil, status_error.ChatLogIndexCorrupted
		}
	}
	nothing := func() {}
	if _, enabled := c.db.Get(eventPublisherSetting); !enabled {
		return nothing, nil
	}
	keys := []string{"title", "active_execution_id", "waiting_approval", "pending_approvals", "waiting_elicitation", "pending_elicitations", "last_ended_execution_status", "last_ended_execution_id", "hidden_from_waker_history"}
	needed := false
	for _, key := range keys {
		if _, exists := fields[key]; exists {
			needed = true
			break
		}
	}
	if !needed {
		return nothing, nil
	}
	changed := func() {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("chat", id))
	}
	var old models.Chat
	if err := controllerDB(ctx, c.db).Select("id", "waker_id", "title", "active_execution_id", "waiting_approval", "pending_approvals", "waiting_elicitation", "pending_elicitations", "last_ended_execution_status", "last_ended_execution_id", "hidden_from_waker_history").Where("id = ?", id).First(&old).Error; err != nil {
		_ = databaseError("capture_chat_notification_changes", id, err)
		return changed, nil
	}
	current := old
	if value, ok := fields["title"].(string); ok {
		current.Title = value
	}
	if value, ok := fields["active_execution_id"].(string); ok {
		current.ActiveExecutionID = value
	}
	if value, ok := fields["waiting_approval"].(bool); ok {
		current.WaitingApproval = value
	}
	if value, ok := fields["waiting_elicitation"].(bool); ok {
		current.WaitingElicitation = value
	}
	if value, ok := fields["hidden_from_waker_history"].(bool); ok {
		current.HiddenFromWakerHistory = value
	}
	if v, ok := fields["last_ended_execution_id"].(string); ok {
		current.LastEndedExecutionID = v
	}
	if value, exists := fields["last_ended_execution_status"]; exists {
		current.LastEndedExecutionStatus = constants.ExecutionStatus(fmt.Sprint(value))
	}
	if hasApprovals {
		current.PendingApprovals = approvals
	}
	if hasElicitations {
		current.PendingElicitations = elicitations
	}
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(current)
	if string(before) == string(after) {
		return nothing, nil
	}
	return changed, nil
}

// PublishChatAttention 保留实际 Chat 身份，展示时才核对可见工作归属。
func (c *chatController) PublishChatAttention(ctx context.Context, change models.WorkBoardLiveEvent) {
	publishChatAttention(ctx, c.db, change)
}

func publishChatAttention(ctx context.Context, db *gorm.DB, change models.WorkBoardLiveEvent) {
	change.Work = models.WorkRef{Kind: constants.WorkOwnerKindChat, ID: change.ChatID}
	publishWorkLive(ctx, db, change)
}

// publishLinkedChatAttention 发布刚获得公开工作归属的请求和本次接纳执行的终态。
func publishLinkedChatAttention(ctx context.Context, database *gorm.DB, resource events.Resource, executionID string) {
	if _, enabled := database.Get(eventPublisherSetting); !enabled {
		return
	}
	events.AfterCommit(ctx, func() {
		readCtx := context.Background()
		db := database.WithContext(readCtx)
		query := db.Select("id,active_execution_id,pending_approvals,pending_elicitations,last_ended_execution_id,last_ended_execution_status").
			Where("(active_execution_id<>'' AND (waiting_approval=1 OR waiting_elicitation=1)) OR (?<>'' AND last_ended_execution_id=?)", executionID, executionID)
		if resource.Kind == "team_thread" {
			lead := db.Model(&models.ChannelThread{}).Select("lead_chat_id").Where("id=? AND ready_at IS NOT NULL", resource.ID)
			query = query.Where("id IN (?)", lead)
		} else {
			query = query.Where("id=?", resource.ID)
		}
		var chat models.Chat
		if err := query.Limit(1).Find(&chat).Error; err != nil {
			_ = databaseError("read_linked_pending_chat_requests", resource.ID, err)
			return
		}
		change := models.WorkBoardLiveEvent{Operation: "request_opened", ChatID: chat.ID, ExecutionID: chat.ActiveExecutionID}
		for _, request := range chat.PendingApprovals {
			change.RequestID, change.RequestKind = request.ApprovalID, "approval"
			publishChatAttention(readCtx, database, change)
		}
		for _, request := range chat.PendingElicitations {
			change.RequestID, change.RequestKind = request.ElicitationID, "input"
			publishChatAttention(readCtx, database, change)
		}
		if executionID == "" || executionID != chat.LastEndedExecutionID {
			return
		}
		operation := "completed"
		switch chat.LastEndedExecutionStatus {
		case constants.ExecutionStatusCompleted:
		case constants.ExecutionStatusFailed:
			operation = "failed"
		default:
			return
		}
		publishChatAttention(readCtx, database, models.WorkBoardLiveEvent{
			Operation: operation, ChatID: chat.ID, MessageID: executionID, ExecutionID: executionID,
		})
	})
}

// RecheckChatRequests 提示桌面核对该 Chat 中已显示的请求。
func (c *chatController) RecheckChatRequests(ctx context.Context, id string) {
	publishWorkRequestRecheck(ctx, c.db, eventsResource("chat", id))
}

func publishWorkRequestRecheck(ctx context.Context, db *gorm.DB, resource events.Resource) {
	configured, _ := db.Get(eventPublisherSetting)
	publisher, ok := configured.(events.Publisher)
	if !ok {
		return
	}
	events.AfterCommit(ctx, func() {
		_ = events.EmitTransient(publisher, "work-board", "work-board.changed", resource, map[string]string{"reason": "requests-changed"})
	})
}

func publishRequestChanges(ctx context.Context, db *gorm.DB, before, after map[string]models.WorkBoardLiveEvent) {
	keys := make([]string, 0, len(before)+len(after))
	for id := range before {
		keys = append(keys, id)
	}
	for id := range after {
		if _, seen := before[id]; !seen {
			keys = append(keys, id)
		}
	}
	sort.Strings(keys)
	for _, id := range keys {
		old, oldFound := before[id]
		current, found := after[id]
		if oldFound && (!found || old != current) {
			old.Operation = "request_closed"
			publishWorkLive(ctx, db, old)
		}
		if found && (!oldFound || old != current) {
			current.Operation = "request_opened"
			publishWorkLive(ctx, db, current)
		}
	}
}

func decodeAttentionField(value any, target any) error {
	if encoded, ok := value.(string); ok {
		return json.Unmarshal([]byte(encoded), target)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

// reviewAttentionChanges 使新的验收围栏不继承旧提示回执。
func (c *channelTaskController) reviewAttentionChanges(ctx context.Context, id string, fields map[string]interface{}) func() {
	nothing := func() {}
	if _, enabled := c.db.Get(eventPublisherSetting); !enabled {
		return nothing
	}
	if _, exists := fields["status"]; !exists {
		return nothing
	}
	var old models.ChannelTask
	if err := controllerDB(ctx, c.db).Select("id", "status", "current_fence", "thread_id").Where("id = ?", id).First(&old).Error; err != nil {
		_ = databaseError("capture_review_notification", id, err)
		return func() { publishWorkRequestRecheck(ctx, c.db, eventsResource("global", "")) }
	}
	current := old
	current.Status = constants.ChannelTaskStatus(fmt.Sprint(fields["status"]))
	if value, exists := fields["current_fence"]; exists {
		parsed, err := strconv.Atoi(fmt.Sprint(value))
		if err == nil {
			current.CurrentFence = parsed
		}
	}
	if old.Status == current.Status && old.CurrentFence == current.CurrentFence {
		return nothing
	}
	return func() {
		if old.Status == constants.ChannelTaskStatusInReview {
			c.publishReview(ctx, old, "request_closed")
		}
		c.publishNewReview(ctx, current)
	}
}

func (c *channelTaskController) publishNewReview(ctx context.Context, task models.ChannelTask) {
	if task.Status == constants.ChannelTaskStatusInReview {
		c.publishReview(ctx, task, "request_opened")
	}
}

func (c *channelTaskController) publishReview(ctx context.Context, task models.ChannelTask, operation string) {
	publishWorkLive(ctx, c.db, models.WorkBoardLiveEvent{
		Operation: operation, Work: models.WorkRef{Kind: constants.WorkOwnerKindTeamThread, ID: task.ThreadID},
		RequestID: task.ID, RequestKind: "review", RequestFence: int64(task.CurrentFence),
	})
}

// workflowAttentionChanges 捕获人工请求的实际新增和解决，不因日志或租约续期产生提醒。
func (c *workflowRunController) workflowAttentionChanges(ctx context.Context, value WorkflowRunState) func() {
	nothing := func() {}
	if _, enabled := c.db.Get(eventPublisherSetting); !enabled {
		return nothing
	}
	resource := eventsResource("workflow_run", value.Run.ID)
	recheck := func() {
		publishCommittedChange(ctx, c.db, "work-board", resource)
		publishWorkRequestRecheck(ctx, c.db, eventsResource("global", ""))
	}
	var old models.WorkflowRun
	if err := controllerDB(ctx, c.db).Select("id", "status", "completed_at").Where("id = ?", value.Run.ID).First(&old).Error; err != nil {
		_ = databaseError("capture_workflow_notification", value.Run.ID, err)
		return recheck
	}
	var humans []models.WorkflowHumanRequest
	var operations []models.WorkflowOperationRequest
	db := controllerDB(ctx, c.db)
	if err := db.Select("id").Where("run_id = ? AND status = 'waiting'", value.Run.ID).Find(&humans).Error; err != nil {
		_ = databaseError("capture_workflow_inputs", value.Run.ID, err)
		return recheck
	}
	if err := db.Select("id").Where("run_id = ? AND status = 'waiting'", value.Run.ID).Find(&operations).Error; err != nil {
		_ = databaseError("capture_workflow_approvals", value.Run.ID, err)
		return recheck
	}
	work := models.WorkRef{Kind: constants.WorkOwnerKindWorkflowRun, ID: value.Run.ID}
	before, after := map[string]models.WorkBoardLiveEvent{}, map[string]models.WorkBoardLiveEvent{}
	request := func(id, kind string) models.WorkBoardLiveEvent {
		return models.WorkBoardLiveEvent{Work: work, RequestID: id, RequestKind: kind, ExecutionID: value.Run.ID}
	}
	for _, v := range humans {
		before["input:"+v.ID] = request(v.ID, "input")
	}
	for _, v := range operations {
		before["approval:"+v.ID] = request(v.ID, "approval")
	}
	at := time.Now().UnixMilli()
	if value.Run.AllowsOwnHumanAction(at) {
		for _, v := range value.Humans {
			if v.Status == constants.WorkflowStepStatusWaiting && (v.DeadlineAt == 0 || v.DeadlineAt > at) {
				after["input:"+v.ID] = request(v.ID, "input")
			}
		}
		for _, v := range value.Operations {
			if v.Status == constants.WorkflowStepStatusWaiting {
				after["approval:"+v.ID] = request(v.ID, "approval")
			}
		}
	}

	linkedChats := map[string]bool{}
	terminalEffects := []string{}
	for _, effect := range value.Effects {
		if effect.ChatID != "" && (effect.FinalMessageID != "" || effect.Status == constants.WorkflowStepStatusSucceeded || effect.Status == constants.WorkflowStepStatusFailed || effect.Status == constants.WorkflowStepStatusCancelled) {
			terminalEffects = append(terminalEffects, effect.ID)
		}
	}
	if len(terminalEffects) > 0 {
		var previousEffects []models.WorkflowEffect
		if err := db.Select("id", "chat_id", "status", "final_message_id").Where("id IN ?", terminalEffects).Find(&previousEffects).Error; err != nil {
			_ = databaseError("capture_workflow_result_links", value.Run.ID, err)
			return recheck
		}
		previous := map[string]models.WorkflowEffect{}
		for _, effect := range previousEffects {
			previous[effect.ID] = effect
		}
		for _, effect := range value.Effects {
			old, exists := previous[effect.ID]
			if effect.ChatID != "" && exists && (old.ChatID != effect.ChatID || old.Status != effect.Status || old.FinalMessageID != effect.FinalMessageID) {
				linkedChats[effect.ChatID] = true
			}
		}
	}
	statusChanged := old.Status != value.Run.Status || !maps.Equal(before, after) || (old.CompletedAt == nil) != (value.Run.CompletedAt == nil)
	return func() {
		publishRequestChanges(ctx, c.db, before, after)
		run := value.Run
		if (!run.Joined || run.ParentRunID == "") && run.HistoryDeletedAt == nil && old.CompletedAt == nil && run.CompletedAt != nil {
			if run.Status == constants.WorkflowRunStatusSucceeded && run.Result != nil {
				publishWorkLive(ctx, c.db, models.WorkBoardLiveEvent{Operation: "completed", Work: work})
			}
			if run.Status == constants.WorkflowRunStatusFailed && run.Failure != nil {
				publishWorkLive(ctx, c.db, models.WorkBoardLiveEvent{Operation: "failed", Work: work})
			}
		}
		// 原 worker 的后置成果关联只失效精确查询，不创建用户成果回执。
		for chatID := range linkedChats {
			publishCommittedChange(ctx, c.db, "work-board", eventsResource("chat", chatID))
		}
		if statusChanged {
			publishCommittedChange(ctx, c.db, "work-board", resource)
		}
	}
}
