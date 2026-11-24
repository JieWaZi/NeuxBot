package channel

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	messageview "github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// announceEntry 保存一个等待批量交给负责人的成员任务报告。
type announceEntry struct {
	// NotificationID 是报告进入负责人 Chat 时使用的幂等消息标识。
	NotificationID string
	// TaskID 是报告所属的正式任务标识，用于判断是否可直接交付。
	TaskID string
	// AttemptID 是报告对应的成员执行尝试标识。
	AttemptID string
	// IMReplyLinkID 是该报告对应的成员尝试冻结的原 IM 会话标识。
	IMReplyLinkID string
	// MemberWakerID 是报告作者对应的伙伴标识。
	MemberWakerID string
	// MemberChatID 是报告完整执行所在的权威 Chat 标识。
	MemberChatID string
	// MemberDisplayName 是团队对话首发时冻结的伙伴展示名称。
	MemberDisplayName string
	// Content 是严格受字符上限约束的结构化成员报告。
	Content string
	// Truncated 表示 Content 只是预览，负责人不得据此行动。
	Truncated bool
}

// finalResultContent 仅提取聊天界面会作为最终答复展示的文本部件。
func finalResultContent(message messageview.Message) string {
	parts := make([]string, 0, len(message.DisplayParts))
	for _, part := range message.DisplayParts {
		if part.Type != constants.MessageDisplayPartText {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// teammateReportContent 保留成员提交的工作摘要，并在缺少摘要时提供最小终态说明。
func teammateReportContent(
	status constants.ChannelTaskStatus,
	result string,
	errorMessage string,
	changedPaths []string,
) (string, bool) {
	result = strings.TrimSpace(result)
	errorMessage = strings.TrimSpace(errorMessage)
	content := result
	if content == "" &&
		(status == constants.ChannelTaskStatusFailed || status == constants.ChannelTaskStatusCancelled) &&
		errorMessage != "" {
		content = errorMessage
	}
	if content == "" {
		content = memberReportStatus(status)
		paths := make([]string, 0, len(changedPaths))
		for _, path := range changedPaths {
			if path = strings.TrimSpace(path); path != "" {
				paths = append(paths, "- "+path)
			}
		}
		if len(paths) > 0 {
			content += "\n\nChanged files or artifacts:\n" + strings.Join(paths, "\n")
		}
	}
	return truncateMemberReport(content)
}

// memberReportStatus 返回结果报告中面向负责人的稳定完成状态。
func memberReportStatus(status constants.ChannelTaskStatus) string {
	switch status {
	case constants.ChannelTaskStatusCompleted:
		return "Completed."
	case constants.ChannelTaskStatusInReview:
		return "Submitted and awaiting user review."
	case constants.ChannelTaskStatusFailed:
		return "Execution failed."
	case constants.ChannelTaskStatusCancelled:
		return "Cancelled."
	default:
		return fmt.Sprintf("Current status: %s.", status)
	}
}

// truncateMemberReport 截断超长报告，并把警示文字计入四千字符上限。
func truncateMemberReport(content string) (string, bool) {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= constants.ChannelMemberReportMaximumRunes {
		return content, false
	}
	notice := []rune("\n\n" + agent.TeamReportTruncationNotice)
	keep := constants.ChannelMemberReportMaximumRunes - len(notice)
	if keep < 0 {
		keep = 0
	}
	return string(runes[:keep]) + string(notice), true
}

// teamNotificationFacts 携带本批报告及任务板状态，供负责人处理。
type teamNotificationFacts struct {
	// Reports 是本批完整报告，投影不包含内部任务标识。
	Reports []teamNotificationReport `json:"reports"`
	// TruncatedMembers 是需要重新提交完整报告的成员。
	TruncatedMembers []string `json:"truncatedMembers,omitempty"`
	// TaskCount 是该来源工作组的任务总数。
	TaskCount int `json:"taskCount"`
	// TerminalCount 是已经进入终态的任务数。
	TerminalCount int `json:"terminalCount"`
	// RemainingTasks 是尚未进入终态的任务状态。
	RemainingTasks []teamNotificationTask `json:"remainingTasks,omitempty"`
	// AwaitingReview 是等待用户验收的任务数。
	AwaitingReview int `json:"awaitingReview"`
	// FinalSummaryAllowed 表示本批具备汇总所需的完整报告且无待完成任务。
	FinalSummaryAllowed bool `json:"finalSummaryAllowed"`
}

// teamNotificationReport 是负责人可用的一份完整成员报告。
type teamNotificationReport struct {
	// Member 是报告来源成员的展示名称。
	Member string `json:"member"`
	// Content 是原报告正文。
	Content string `json:"content"`
}

// teamNotificationTask 提供尚未终止的任务状态。
type teamNotificationTask struct {
	// Subject 是任务标题。
	Subject string `json:"subject"`
	// Status 是当前任务记录中的状态值。
	Status string `json:"status"`
}

func teamNotificationContext(entries []announceEntry, tasks []models.ChannelTask) workinput.Instruction {
	facts := teamNotificationFacts{Reports: make([]teamNotificationReport, 0, len(entries)), TaskCount: len(tasks)}
	for _, entry := range entries {
		if entry.Truncated {
			facts.TruncatedMembers = append(facts.TruncatedMembers, memberLabel(entry))
			continue
		}
		facts.Reports = append(facts.Reports, teamNotificationReport{Member: memberLabel(entry), Content: entry.Content})
	}
	for _, task := range tasks {
		if task.Status.Terminal() {
			facts.TerminalCount++
			continue
		}
		if task.Status == constants.ChannelTaskStatusInReview {
			facts.AwaitingReview++
		}
		facts.RemainingTasks = append(facts.RemainingTasks, teamNotificationTask{Subject: task.Subject, Status: string(task.Status)})
	}
	facts.FinalSummaryAllowed = facts.TerminalCount == facts.TaskCount && len(facts.TruncatedMembers) == 0
	encoded, _ := json.Marshal(facts)
	return workinput.Instruction{Name: agent.TeamNotificationContextName, Content: string(encoded)}
}

// memberLabel 返回负责人提示中使用的成员名称。
func memberLabel(entry announceEntry) string {
	if label := strings.TrimSpace(entry.MemberDisplayName); label != "" {
		return label
	}
	return "Team member"
}
