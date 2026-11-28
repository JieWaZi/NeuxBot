package controllers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReserveWork 对同一会话、伙伴和范围只预留一个服务端工作 Chat。
func (c *MainConversationController) ReserveWork(ctx context.Context, convID, wakerID, scope string) (models.MainConversationWork, error) {
	return c.reserveWork(ctx, convID, wakerID, scope, "")
}

// ActiveScopedSources 分页读取重启时仍占用执行槽的指定来源。
func (c *MainConversationController) ActiveScopedSources(ctx context.Context, scope, after string, limit int) ([]models.Chat, error) {
	db := controllerDB(ctx, c.db)
	owned := db.Model(&models.MainConversationWork{}).Select("work_chat_id").Where("scope_key = ? AND work_chat_id > ?", scope, after)
	var rows []models.Chat
	if err := db.Select("id", "active_execution_id").Where("id IN (?) AND hidden_from_waker_history AND active_execution_id <> ''", owned).
		Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, databaseError("list_active_main_scoped_sources", "", err)
	}
	return rows, nil
}

// ReserveExactWork 将已有持久派工身份与授权的来源工作绑定。
func (c *MainConversationController) ReserveExactWork(ctx context.Context, convID, wakerID, scope, chatID string) (models.MainConversationWork, error) {
	row, err := c.reserveWork(ctx, convID, wakerID, scope, chatID)
	if err == nil && row.WorkChatID != chatID {
		err = status_error.CreateRequestConflict
	}
	return row, err
}

func (c *MainConversationController) reserveWork(ctx context.Context, convID, wakerID, scope, chatID string) (models.MainConversationWork, error) {
	db := controllerDB(ctx, c.db)
	filter := models.MainConversationWork{ConversationID: convID, WakerID: wakerID, ScopeKey: scope}
	var row models.MainConversationWork
	if err := db.Where(&filter).Limit(1).Find(&row).Error; err != nil {
		return row, databaseError("find_main_work_scope", convID, err)
	}
	if row.WorkChatID != "" {
		return row, nil
	}
	row = filter
	row.WorkChatID = chatID
	if row.WorkChatID == "" {
		row.WorkChatID = util.New()
	}
	res := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "conversation_id"}, {Name: "waker_id"}, {Name: "scope_key"}}, DoNothing: true}).Create(&row)
	if res.Error != nil {
		return row, databaseError("reserve_main_work", convID, res.Error)
	}
	if res.RowsAffected == 0 {
		row = models.MainConversationWork{}
		if err := db.Where(&filter).Take(&row).Error; err != nil {
			return row, databaseError("resolve_main_work", convID, err)
		}
	}
	return row, nil
}

// FindWork 使用工作 Chat 主键确定此来源是否由主会话拥有。
func (c *MainConversationController) FindWork(ctx context.Context, id string) (models.MainConversationWork, bool, error) {
	var row models.MainConversationWork
	if err := controllerDB(ctx, c.db).Where("work_chat_id = ?", id).Limit(1).Find(&row).Error; err != nil {
		return row, false, databaseError("find_main_work", id, err)
	}
	return row, row.WorkChatID != "", nil
}

// Work 按服务端生成的工作 Chat 身份读取冻结的绑定。
func (c *MainConversationController) Work(ctx context.Context, id string) (models.MainConversationWork, error) {
	row, found, err := c.FindWork(ctx, id)
	if err == nil && !found {
		err = status_error.ChatNotFound.WithParams(map[string]interface{}{"ChatID": id})
	}
	return row, err
}

// OwnedWorkChatIDs 核对某一页公开交办仍可读取的原工作来源。
func (c *MainConversationController) OwnedWorkChatIDs(ctx context.Context, conversationID string, candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	var ids []string
	if err := controllerDB(ctx, c.db).Model(&models.MainConversationWork{}).
		Where("conversation_id = ? AND work_chat_id IN ?", conversationID, candidates).
		Pluck("work_chat_id", &ids).Error; err != nil {
		return nil, databaseError("find_main_owned_work_chats", conversationID, err)
	}
	return ids, nil
}

// ListMainWorkChatIDs 列出整队删除时应按 Chat 归属安全清理的工作记录。
func (c *channelController) ListMainWorkChatIDs(ctx context.Context, id string) ([]string, error) {
	db := controllerDB(ctx, c.db)
	convID, err := publicSubjectConversationID(db, "team", id)
	if err != nil || convID == "" {
		return nil, err
	}
	var ids []string
	if err := db.Model(&models.MainConversationWork{}).Where("conversation_id = ?", convID).Pluck("work_chat_id", &ids).Error; err != nil {
		return nil, databaseError("list_team_main_work", id, err)
	}
	return ids, nil
}

// releaseMainWorkChat 使来源资格随工作 Chat 一起失效，公开正文仍由主会话持有。
func releaseMainWorkChat(db *gorm.DB, chatID string) error {
	if err := db.Where("work_chat_id = ?", chatID).Delete(&models.MainConversationInput{}).Error; err != nil {
		return databaseError("delete_main_work_inputs", chatID, err)
	}
	if err := db.Where("work_chat_id = ?", chatID).Delete(&models.MainConversationWork{}).Error; err != nil {
		return databaseError("release_main_work_chat", chatID, err)
	}
	return nil
}

// mainWorkRequestTarget 保留原来源绑定和仍允许用户操作的主体名称。
type mainWorkRequestTarget struct {
	// Work 是隐藏来源 Chat 的主会话和实际执行伙伴。
	Work models.MainConversationWork
	// Owner 是来源绑定的公开会话。
	Owner models.MainConversation
	// Title 为仍授权该来源的主体名称，nil 表示待办已不可操作。
	Title *string
}

// mainWorkRequestTargets 批量关联候选来源的会话、主体和当前成员。
func mainWorkRequestTargets(db *gorm.DB, sources []models.MainConversationWork) (map[string]mainWorkRequestTarget, error) {
	conversationIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		conversationIDs = append(conversationIDs, source.ConversationID)
	}
	owners, err := readRelatedRows[models.MainConversation](db, "id", "id,subject_type,subject_id", conversationIDs)
	if err != nil {
		return nil, err
	}
	byConversation := make(map[string]models.MainConversation, len(owners))
	var wakerIDs, teamIDs []string
	for _, owner := range owners {
		byConversation[owner.ID] = owner
		switch owner.SubjectType {
		case "waker":
			wakerIDs = append(wakerIDs, owner.SubjectID)
		case "team":
			teamIDs = append(teamIDs, owner.SubjectID)
		}
	}
	wakers, err := readRelatedRows[models.Waker](db, "id", "id,name", wakerIDs)
	if err != nil {
		return nil, err
	}
	teams, err := readRelatedRows[models.Channel](db, "id", "id,name,lead_waker_id", teamIDs)
	if err != nil {
		return nil, err
	}
	members, err := readRelatedRows[models.ChannelMember](db, "channel_id", "channel_id,waker_id", teamIDs)
	if err != nil {
		return nil, err
	}
	wakersByID := make(map[string]models.Waker, len(wakers))
	for _, waker := range wakers {
		wakersByID[waker.ID] = waker
	}
	teamsByID := make(map[string]models.Channel, len(teams))
	for _, team := range teams {
		teamsByID[team.ID] = team
	}
	membersByTeam := make(map[string][]models.ChannelMember, len(teams))
	for _, member := range members {
		membersByTeam[member.ChannelID] = append(membersByTeam[member.ChannelID], member)
	}
	targets := make(map[string]mainWorkRequestTarget, len(sources))
	for _, source := range sources {
		owner := byConversation[source.ConversationID]
		target := mainWorkRequestTarget{Work: source, Owner: owner}
		switch owner.SubjectType {
		case "waker":
			if waker := wakersByID[owner.SubjectID]; waker.ID != "" && waker.ID == source.WakerID {
				target.Title = &waker.Name
			}
		case "team":
			if team := teamsByID[owner.SubjectID]; team.AuthorizesWorkActor(source.WakerID, membersByTeam[team.ID]) {
				target.Title = &team.Name
			}
		}
		targets[source.WorkChatID] = target
	}
	return targets, nil
}
