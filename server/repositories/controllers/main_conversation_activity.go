package controllers

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// ActiveWork 读取本会话当前回合及其公开发言状态，独立于消息分页。
func (c *MainConversationController) ActiveWork(ctx context.Context, conversationID string) ([]models.MainConversationActivity, error) {
	db := controllerDB(ctx, c.db)
	result, err := activeMainWork(db, []string{conversationID})
	active := result[conversationID]
	if err != nil || len(active) == 0 {
		return []models.MainConversationActivity{}, err
	}

	executions := make([]string, 0, len(active))
	bySourceExecution := make(map[[2]string]int, len(active))
	for i, work := range active {
		executions = append(executions, work.ExecutionID)
		bySourceExecution[[2]string{work.WorkChatID, work.ExecutionID}] = i
	}
	encoded, _ := json.Marshal(executions)
	var public []models.MainConversationActivity
	// 只查看本轮的公开发言，避免翻页时丢失更早的答复或错认旧轮消息。
	if err := db.Table("main_conversation_messages INDEXED BY idx_main_conversation_messages_source_execution_id").
		Select("source_chat_id AS work_chat_id, source_execution_id AS execution_id, "+
			"MAX(CASE WHEN role = ? THEN id ELSE '' END) AS input_message_id, MAX(role = ?) AS has_public_reply",
			constants.MessageRoleUser, constants.MessageRoleAssistant).
		Where("conversation_id = ? AND removed_at = 0 AND admission_pending = 0 AND source_execution_id IN (SELECT value FROM json_each(?))", conversationID, string(encoded)).
		Group("source_chat_id, source_execution_id").Find(&public).Error; err != nil {
		return nil, databaseError("active_main_work_publications", conversationID, err)
	}
	for _, row := range public {
		if i, ok := bySourceExecution[[2]string{row.WorkChatID, row.ExecutionID}]; ok {
			active[i].InputMessageID, active[i].HasPublicReply = row.InputMessageID, row.HasPublicReply
		}
	}
	return active, nil
}

// activeMainWork 一次读取当前范围内活动的来源；尚未物化或已结束的工作不形成状态。
func activeMainWork(db *gorm.DB, conversationIDs []string) (map[string][]models.MainConversationActivity, error) {
	result := map[string][]models.MainConversationActivity{}
	if len(conversationIDs) == 0 {
		return result, nil
	}
	owned := db.Model(&models.MainConversationWork{}).Select("work_chat_id").Where("conversation_id IN ?", conversationIDs)
	var active []models.MainConversationActivity
	err := db.Model(&models.Chat{}).Select("id AS work_chat_id, active_execution_id AS execution_id, waker_id AS source_waker_id, waiting_approval, waiting_elicitation, "+
		"CASE WHEN waiting_approval THEN COALESCE(json_array_length(NULLIF(pending_approvals,'')),0) ELSE 0 END + "+
		"CASE WHEN waiting_elicitation THEN COALESCE(json_array_length(NULLIF(pending_elicitations,'')),0) ELSE 0 END AS request_count").
		Where("id IN (?) AND hidden_from_waker_history = ? AND active_execution_id <> ''", owned, true).Order("id").Find(&active).Error
	if err != nil {
		return nil, databaseError("active_main_work", "", err)
	}
	if len(active) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(active))
	for _, work := range active {
		ids = append(ids, work.WorkChatID)
	}
	encoded, _ := json.Marshal(ids)
	var sources []models.MainConversationWork
	if err := db.Select("conversation_id", "work_chat_id", "waker_id").Where("work_chat_id IN (SELECT value FROM json_each(?))", string(encoded)).Find(&sources).Error; err != nil {
		return nil, databaseError("active_main_work_owners", "", err)
	}
	owners := make(map[string]models.MainConversationWork, len(sources))
	for _, source := range sources {
		owners[source.WorkChatID] = source
	}
	var waitingSources []models.MainConversationWork
	for _, work := range active {
		if source := owners[work.WorkChatID]; source.ConversationID != "" && (work.WaitingApproval || work.WaitingElicitation) {
			waitingSources = append(waitingSources, source)
		}
	}
	waiting, err := mainWorkRequestTargets(db, waitingSources)
	if err != nil {
		return nil, err
	}
	for _, work := range active {
		if target, hasRequest := waiting[work.WorkChatID]; hasRequest && target.Title == nil {
			continue
		}
		if source := owners[work.WorkChatID]; source.ConversationID != "" && source.WakerID == work.SourceWakerID {
			result[source.ConversationID] = append(result[source.ConversationID], work)
		}
	}
	return result, nil
}

// mainWorkCounts 为侧栏提供正在运行的来源数，待答请求通过看板的统一查询统计。
func mainWorkCounts(db *gorm.DB, subjects []string) (map[string]MainConversationSubjectActivity, error) {
	var convs []models.MainConversation
	if err := db.Select("id", "subject_id", "subject_type").Where("subject_type IN ? AND subject_id IN ?", []string{"waker", "team"}, subjects).Find(&convs).Error; err != nil {
		return nil, databaseError("main_work_subjects", "", err)
	}
	ids := make([]string, 0, len(convs))
	for _, conv := range convs {
		ids = append(ids, conv.ID)
	}
	active, err := activeMainWork(db, ids)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]MainConversationSubjectActivity, len(convs))
	for _, conv := range convs {
		summary := MainConversationSubjectActivity{SubjectType: conv.SubjectType}
		for _, work := range active[conv.ID] {
			if !work.WaitingApproval && !work.WaitingElicitation {
				summary.RunningCount++
			}
		}
		counts[conv.SubjectID] = summary
	}
	return counts, nil
}

// mainWorkSubject 按已授权的来源及实际执行者读取事件应通知的公开主体。
func mainWorkSubject(db *gorm.DB, chatID, wakerID string) (models.MainConversation, error) {
	var owner models.MainConversation
	source := db.Model(&models.MainConversationWork{}).Select("conversation_id").Where("work_chat_id=? AND waker_id=?", chatID, wakerID)
	err := db.Select("id", "subject_id", "subject_type").Where("id IN (?)", source).Limit(1).Find(&owner).Error
	return owner, err
}
