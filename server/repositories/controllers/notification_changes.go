package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// chatNoticeChanges 在原投影 CAS 前捕获语义差异；正文游标推进不重建全局通知。
func (c *chatController) chatNoticeChanges(ctx context.Context, id string, fields map[string]any) (func(), error) {
	nothing := func() {}
	if _, enabled := c.db.Get(eventPublisherSetting); !enabled {
		return nothing, nil
	}
	keys := []string{"title", "active_execution_id", "waiting_approval", "pending_approvals", "waiting_elicitation", "pending_elicitations", "last_ended_execution_status", "hidden_from_waker_history"}
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
	var old models.Chat
	if err := controllerDB(ctx, c.db).Select("id", "title", "active_execution_id", "waiting_approval", "pending_approvals", "waiting_elicitation", "pending_elicitations", "last_ended_execution_status", "hidden_from_waker_history").Where("id = ?", id).First(&old).Error; err != nil {
		return nothing, databaseError("capture_chat_notification_changes", id, err)
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
	if value, exists := fields["last_ended_execution_status"]; exists {
		current.LastEndedExecutionStatus = constants.ExecutionStatus(fmt.Sprint(value))
	}
	if value, exists := fields["pending_approvals"]; exists {
		if err := decodeNotificationField(value, &current.PendingApprovals); err != nil {
			logx.Errorf("decode chat notification projection failed: chat_id=%s field=pending_approvals error=%v", id, err)
			return nothing, status_error.ChatLogIndexCorrupted
		}
	}
	if value, exists := fields["pending_elicitations"]; exists {
		if err := decodeNotificationField(value, &current.PendingElicitations); err != nil {
			logx.Errorf("decode chat notification projection failed: chat_id=%s field=pending_elicitations error=%v", id, err)
			return nothing, status_error.ChatLogIndexCorrupted
		}
	}
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(current)
	if string(before) == string(after) {
		return nothing, nil
	}
	upserts, removals := noticeSetDifference(chatPendingNoticeIDs(old), chatPendingNoticeIDs(current))
	return func() {
		publishCommittedChange(ctx, c.db, "work-board", eventsResource("chat", id))
		publishCommittedNoticeIDs(ctx, c.db, upserts, removals)
	}, nil
}

func decodeNotificationField(value any, target any) error {
	if encoded, ok := value.(string); ok {
		return json.Unmarshal([]byte(encoded), target)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

func chatPendingNoticeIDs(chat models.Chat) map[string]bool {
	result := map[string]bool{}
	if chat.ActiveExecutionID == "" {
		return result
	}
	if chat.WaitingApproval {
		for _, approval := range chat.PendingApprovals {
			result["approval:"+approval.ApprovalID] = true
		}
	}
	if chat.WaitingElicitation {
		for _, input := range chat.PendingElicitations {
			result["elicitation:"+input.ElicitationID] = true
		}
	}
	return result
}

func noticeSetDifference(before, after map[string]bool) ([]string, []string) {
	upserts, removals := []string{}, []string{}
	for id := range after {
		if !before[id] {
			upserts = append(upserts, id)
		}
	}
	for id := range before {
		if !after[id] {
			removals = append(removals, id)
		}
	}
	sort.Strings(upserts)
	sort.Strings(removals)
	return upserts, removals
}

// reviewNoticeChanges 使新的验收围栏不继承旧提示回执。
func (c *channelTaskController) reviewNoticeChanges(ctx context.Context, id string, fields map[string]interface{}) (func(), error) {
	nothing := func() {}
	if _, enabled := c.db.Get(eventPublisherSetting); !enabled {
		return nothing, nil
	}
	if _, exists := fields["status"]; !exists {
		return nothing, nil
	}
	var old models.ChannelTask
	if err := controllerDB(ctx, c.db).Select("id", "status", "current_fence").Where("id = ?", id).First(&old).Error; err != nil {
		return nothing, databaseError("capture_review_notification", id, err)
	}
	current := old
	current.Status = constants.ChannelTaskStatus(fmt.Sprint(fields["status"]))
	if value, exists := fields["current_fence"]; exists {
		parsed, err := strconv.Atoi(fmt.Sprint(value))
		if err == nil {
			current.CurrentFence = parsed
		}
	}
	before, after := map[string]bool{}, map[string]bool{}
	if old.Status == constants.ChannelTaskStatusInReview {
		before["review:"+id+":"+strconv.Itoa(old.CurrentFence)] = true
	}
	if current.Status == constants.ChannelTaskStatusInReview {
		after["review:"+id+":"+strconv.Itoa(current.CurrentFence)] = true
	}
	upserts, removals := noticeSetDifference(before, after)
	return func() { publishCommittedNoticeIDs(ctx, c.db, upserts, removals) }, nil
}

// workflowNoticeChanges 捕获人工请求的实际新增和解决，不因日志或租约续期产生提醒。
func (c *workflowRunController) workflowNoticeChanges(ctx context.Context, value WorkflowRunState) (func(), error) {
	nothing := func() {}
	if _, enabled := c.db.Get(eventPublisherSetting); !enabled {
		return nothing, nil
	}
	var old models.WorkflowRun
	if err := controllerDB(ctx, c.db).Select("id", "status", "completed_at").Where("id = ?", value.Run.ID).First(&old).Error; err != nil {
		return nothing, databaseError("capture_workflow_notification", value.Run.ID, err)
	}
	var humans []models.WorkflowHumanRequest
	var operations []models.WorkflowOperationRequest
	db := controllerDB(ctx, c.db)
	if err := db.Select("id").Where("run_id = ? AND status = 'waiting'", value.Run.ID).Find(&humans).Error; err != nil {
		return nothing, databaseError("capture_workflow_inputs", value.Run.ID, err)
	}
	if err := db.Select("id").Where("run_id = ? AND status = 'waiting'", value.Run.ID).Find(&operations).Error; err != nil {
		return nothing, databaseError("capture_workflow_approvals", value.Run.ID, err)
	}
	before, after := map[string]bool{}, map[string]bool{}
	for _, v := range humans {
		before["human:"+v.ID] = true
	}
	for _, v := range operations {
		before["operation:"+v.ID] = true
	}
	for _, v := range value.Humans {
		if fmt.Sprint(v.Status) == "waiting" {
			after["human:"+v.ID] = true
		}
	}
	for _, v := range value.Operations {
		if fmt.Sprint(v.Status) == "waiting" {
			after["operation:"+v.ID] = true
		}
	}
	// 结束或停止的运行不再提供有效请求；旧 waiting 行仍可留作历史。
	if value.Run.Status != constants.WorkflowRunStatusQueued && value.Run.Status != constants.WorkflowRunStatusRunning && value.Run.Status != constants.WorkflowRunStatusWaiting && value.Run.Status != constants.WorkflowRunStatusRecovering {
		clear(after)
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
			return nothing, databaseError("capture_workflow_result_links", value.Run.ID, err)
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
	upserts, removals := noticeSetDifference(before, after)
	statusChanged := old.Status != value.Run.Status || len(upserts)+len(removals) > 0 || (old.CompletedAt == nil) != (value.Run.CompletedAt == nil)
	return func() {
		publishCommittedNoticeIDs(ctx, c.db, upserts, removals)
		// 原 worker 的后置成果关联只失效精确查询，不创建用户成果回执。
		for chatID := range linkedChats {
			publishCommittedChange(ctx, c.db, "work-board", eventsResource("chat", chatID))
		}
		if statusChanged {
			publishCommittedChange(ctx, c.db, "work-board", eventsResource("workflow_run", value.Run.ID))
		}
	}, nil
}
