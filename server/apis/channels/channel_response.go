package channels

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 团队成员详情
type ChannelMemberDetail struct {
	// 伙伴
	Waker models.Waker `json:"waker"`
	// 运行时标识
	RuntimeID string `json:"runtimeId"`
	// 账号标识
	AccountID string `json:"accountId"`
	// 模型
	Model string `json:"model"`
	// 推理强度
	Reasoning string `json:"reasoning"`
	// 权限等级
	PermissionLevel constants.PermissionLevel `json:"permissionLevel"`
	// 展示顺序
	Position int `json:"position"`
}

// 团队详情
type ChannelDetail struct {
	// 团队
	Channel models.Channel `json:"channel"`
	// 成员
	Members []ChannelMemberDetail `json:"members"`
	// 聚合活动状态
	ActivityState constants.ChannelActivityState `json:"activityState"`
	// 运行中的团队对话数量
	RunningCount int `json:"runningCount"`
	// 待审批或待输入的团队对话数量
	WaitingCount int `json:"waitingCount"`
}

func loadChannelDetail(
	ctx context.Context,
	channel models.Channel,
	summary models.ActivitySummary,
) (ChannelDetail, error) {
	members, err := global.ChannelControllerManager.ListMembers(ctx, channel.ID)
	if err != nil {
		return ChannelDetail{}, err
	}
	details := make([]ChannelMemberDetail, 0, len(members))
	for _, member := range members {
		waker, err := global.WakerControllerManager.Get(ctx, member.WakerID)
		if err != nil {
			return ChannelDetail{}, err
		}
		details = append(details, ChannelMemberDetail{
			Waker:           waker,
			RuntimeID:       member.RuntimeID,
			AccountID:       member.AccountID,
			Model:           member.Model,
			Reasoning:       member.Reasoning,
			PermissionLevel: member.PermissionLevel,
			Position:        member.Position,
		})
	}
	activityState := summary.ChannelActivityState()
	if activityState == "" {
		activityState = constants.ChannelActivityStateIdle
	}
	return ChannelDetail{
		Channel:       channel,
		Members:       details,
		ActivityState: activityState,
		RunningCount:  summary.RunningCount,
		WaitingCount:  summary.WaitingCount,
	}, nil
}
