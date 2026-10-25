// Package models 定义 NeuxBot 的 GORM 持久化模型。
package models

import "github.com/JieWaZi/NeuxBot/server/constants"

// ActivitySummary 是列表查询聚合出的只读活动计数。
type ActivitySummary struct {
	// RunningCount 是当前处于运行中的会话或团队对话数量。
	RunningCount int `json:"runningCount"`
	// WaitingCount 是当前等待用户审批或输入的会话或团队对话数量。
	WaitingCount int `json:"waitingCount"`
}

// WakerActivityState 根据计数推导伙伴列表使用的聚合活动状态。
func (s ActivitySummary) WakerActivityState() constants.WakerActivityState {
	if s.WaitingCount > 0 {
		return constants.WakerActivityStateWaiting
	}
	if s.RunningCount > 0 {
		return constants.WakerActivityStateRunning
	}
	return constants.WakerActivityStateIdle
}

// ChannelActivityState 根据计数推导团队列表使用的聚合活动状态。
func (s ActivitySummary) ChannelActivityState() constants.ChannelActivityState {
	if s.WaitingCount > 0 {
		return constants.ChannelActivityStateWaiting
	}
	if s.RunningCount > 0 {
		return constants.ChannelActivityStateRunning
	}
	return constants.ChannelActivityStateIdle
}
