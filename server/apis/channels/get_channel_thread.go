package channels

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 团队对话详情
type ChannelThreadDetail struct {
	// 团队对话
	Thread models.ChannelThread `json:"thread"`
	// 成员快照
	Members []models.ChannelThreadMember `json:"members"`
	// 任务板
	Tasks []models.ChannelTask `json:"tasks"`
	// 负责人执行
	LeadActiveExecutionID string `json:"leadActiveExecutionId,omitempty"`
	// 负责人汇总中
	LeadProcessing bool `json:"leadProcessing"`
	// 等待用户处理的成员交互
	PendingInteractions []channelservice.PendingTeamInteraction `json:"pendingInteractions"`
	// 成员运行与等待操作状态
	MemberActivities []channelservice.TeamMemberActivity `json:"memberActivities"`
	// 最新团队事件游标
	EventCursor string `json:"eventCursor,omitempty"`
}

// 获取团队对话
type GetChannelThread struct {
	ginx.MethodGet
	// 团队标识
	ChannelID string `in:"path" name:"channelId" validate:"required,max=32"`
	// 团队对话标识
	ThreadID string `in:"path" name:"threadId" validate:"required,max=32"`
}

// Path 返回团队对话详情接口路径。
func (g *GetChannelThread) Path() string {
	return "/:channelId/threads/:threadId"
}

// Output 返回成员 Chat 映射、当前任务板和工作状态元数据。
func (g *GetChannelThread) Output(ctx *gin.Context) (interface{}, error) {
	thread, err := channelThread(ctx, g.ChannelID, g.ThreadID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	members, err := global.ChannelThreadControllerManager.ListMembers(ctx, thread.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	tasks, err := global.ChannelTaskControllerManager.ListByThread(ctx, thread.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	leadSummary, err := global.ChatService.GetChatSummary(ctx, thread.LeadChatID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	leadProcessing, err := global.ChannelTaskControllerManager.HasLeadProcessingNotifications(
		ctx,
		thread.ID,
		leadSummary.Chat.ActiveExecutionID,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if err := global.ChannelService.SyncInteractionEvents(ctx, thread.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	interactions, activities, err := global.ChannelService.ListPendingInteractions(ctx, thread.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	eventCursor, err := global.ChannelThreadControllerManager.LatestEventCursor(ctx, thread.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ChannelThreadDetail{
		Thread:                thread,
		Members:               members,
		Tasks:                 tasks,
		LeadActiveExecutionID: leadSummary.Chat.ActiveExecutionID,
		LeadProcessing:        leadProcessing,
		PendingInteractions:   interactions,
		MemberActivities:      activities,
		EventCursor:           eventCursor,
	}, nil
}
