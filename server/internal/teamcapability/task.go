package teamcapability

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// TaskInput 是 channel_tasks 单工具多动作的结构化入参。
type TaskInput struct {
	// Action 是当前团队角色读取、维护任务或提交报告时选择的动作。
	Action constants.ChannelTaskToolAction `json:"action" jsonschema:"Action: list, search, get, create, update, follow_up, retry, cancel, or report"`
	// TaskID 是单任务动作需要的任务标识。
	TaskID string `json:"task_id,omitempty" jsonschema:"Task ID required by single-task actions"`
	// Query 是 search 动作使用的关键词。
	Query string `json:"query,omitempty" jsonschema:"Search terms used by the search action"`
	// AssigneeWakerID 是 create、update 或 retry 指定的成员标识。
	AssigneeWakerID string `json:"assignee_waker_id,omitempty" jsonschema:"Required for create; optional for update or retry"`
	// Subject 是 create 必填或 update 可选的任务主题。
	Subject string `json:"subject,omitempty" jsonschema:"Required for create; optional for update"`
	// Description 是 create 必填或 update 可选的任务说明。
	Description string `json:"description,omitempty" jsonschema:"Required for create; optional for update"`
	// Priority 是 create 或 update 可选的派发优先级。
	Priority *int `json:"priority,omitempty" jsonschema:"Higher values are executed first"`
	// DependencyTaskIDs 是 create 指定的前置任务标识。
	DependencyTaskIDs []string `json:"dependency_task_ids,omitempty" jsonschema:"Optional prerequisite task IDs for create"`
	// RequiresHumanReview 表示成功后是否等待用户验收。
	RequiresHumanReview bool `json:"requires_human_review,omitempty" jsonschema:"Enable only when user acceptance is genuinely required"`
	// DirectDelivery 表示负责人允许完整的单任务报告直接交付用户。
	DirectDelivery bool `json:"direct_delivery,omitempty" jsonschema:"Create only: allow one complete member report to reach the user without a lead summary"`
	// Text 是 follow_up 的完整补充或 report 的真实工作报告。
	Text string `json:"text,omitempty" jsonschema:"Complete text required by follow_up or report"`
	// ReadyForUser 表示 report 正文是完整且可直接展示给用户的成果。
	ReadyForUser bool `json:"ready_for_user,omitempty" jsonschema:"Report only: text is a complete user-facing result"`
}

// TaskOutput 是团队协作工具返回给当前调用伙伴的结构化结果。
type TaskOutput struct {
	// Tasks 是 list 或 search 返回的有序任务集合。
	Tasks []models.ChannelTask `json:"tasks,omitempty"`
	// Task 是 get 或 create 返回的单项任务。
	Task *models.ChannelTask `json:"task,omitempty"`
	// Message 是需要负责人据此修正调用的提示。
	Message string `json:"message,omitempty"`
	// Reported 表示成员工作报告已经由系统可靠接纳。
	Reported bool `json:"reported,omitempty"`
	// NextStep 告知成员成功提交报告后应如何用简短 final 收尾。
	NextStep string `json:"nextStep,omitempty"`
}

// Allows 只实施服务端团队角色授权；命令 Schema 由统一目录维护。
func Allows(action constants.ChannelTaskToolAction, role string, mutations bool) bool {
	if role == constants.ChannelCapabilityRoleMember {
		return action == constants.ChannelTaskToolActionReport
	}
	if role != constants.ChannelCapabilityRoleLead {
		return false
	}
	switch action {
	case constants.ChannelTaskToolActionList, constants.ChannelTaskToolActionSearch, constants.ChannelTaskToolActionGet:
		return true
	case constants.ChannelTaskToolActionCreate, constants.ChannelTaskToolActionUpdate, constants.ChannelTaskToolActionFollowUp, constants.ChannelTaskToolActionRetry, constants.ChannelTaskToolActionCancel:
		return mutations
	}
	return false
}
