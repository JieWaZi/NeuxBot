package channels

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	channelservice "github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 团队委派记录
type ChannelDelegation struct {
	// 派发标识
	ID string `json:"id"`
	// 发起伙伴
	SenderWakerID string `json:"senderWakerId"`
	// 执行伙伴
	AssigneeWakerID string `json:"assigneeWakerId"`
	// 交付时间
	CreatedAt int64 `json:"createdAt"`
	// 接收执行
	ExecutionID string `json:"executionId,omitempty"`
	// 派发时间
	QueuedAt int64 `json:"queuedAt,omitempty"`
	// 发起时间
	SentAt int64 `json:"sentAt,omitempty"`
}

// 团队对话详情
type ChannelThreadDetail struct {
	// 团队对话
	Thread models.ChannelThread `json:"thread"`
	// 成员快照
	Members []models.ChannelThreadMember `json:"members"`
	// 任务板
	Tasks []models.ChannelTask `json:"tasks"`
	// 已交付委派
	Delegations []ChannelDelegation `json:"delegations"`
	// 负责人执行
	LeadActiveExecutionID string `json:"leadActiveExecutionId,omitempty"`
	// 负责人汇总中
	LeadProcessing bool `json:"leadProcessing"`
	// 关联工作流尚未结算
	ActiveWorkflows bool `json:"activeWorkflows,omitempty"`
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
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	delegationAttempts, err := global.ChannelTaskControllerManager.ListDelegationAttempts(ctx, taskIDs)
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
	activeWorkflows, err := global.ChannelThreadControllerManager.CountActiveWorkflows(ctx, thread.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ChannelThreadDetail{
		Thread:                thread,
		Members:               members,
		Tasks:                 tasks,
		Delegations:           threadDelegations(thread, tasks, delegationAttempts),
		LeadActiveExecutionID: leadSummary.Chat.ActiveExecutionID,
		LeadProcessing:        leadProcessing,
		ActiveWorkflows:       activeWorkflows > 0,
		PendingInteractions:   interactions,
		MemberActivities:      activities,
		EventCursor:           eventCursor,
	}, nil
}

// threadDelegations 使用尝试的实际执行成员与接纳时间，任务重分配不重写历史。
func threadDelegations(thread models.ChannelThread, tasks []models.ChannelTask, attempts []models.ChannelTaskAttempt) []ChannelDelegation {
	tasksByID := make(map[string]models.ChannelTask, len(tasks))
	for _, task := range tasks {
		tasksByID[task.ID] = task
	}
	delegations := make([]ChannelDelegation, 0, len(attempts))
	for _, attempt := range attempts {
		task, exists := tasksByID[attempt.TaskID]
		if !exists || attempt.StartedAt == nil {
			continue
		}
		sender := task.SenderWakerID
		if attempt.Reason == constants.ChannelAttemptReasonRetry || attempt.Reason == constants.ChannelAttemptReasonReassigned {
			sender = thread.LeadWakerID
		}
		if sender == "" || sender == attempt.AssigneeWakerID {
			continue
		}
		delegations = append(delegations, ChannelDelegation{
			ID: attempt.ID, SenderWakerID: sender, AssigneeWakerID: attempt.AssigneeWakerID, CreatedAt: *attempt.StartedAt,
			ExecutionID: attempt.ExecutionID, QueuedAt: attempt.CreatedAt, SentAt: attempt.DelegationSentAt,
		})
	}
	return delegations
}
