package channel

import (
	"fmt"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	messageview "github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const memberReportTruncationNotice = "> Report truncated: this is only a limited preview. The lead must not act on it; ask the member to resend a concise report."

// announceEntry 保存一个等待批量交给负责人的成员任务报告。
type announceEntry struct {
	// NotificationID 是报告进入负责人 Chat 时使用的幂等消息标识。
	NotificationID string
	// TaskID 是报告所属的正式任务标识，用于判断是否可直接交付。
	TaskID string
	// AttemptID 是报告对应的成员执行尝试标识。
	AttemptID string
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
	notice := []rune("\n\n" + memberReportTruncationNotice)
	keep := constants.ChannelMemberReportMaximumRunes - len(notice)
	if keep < 0 {
		keep = 0
	}
	return string(runes[:keep]) + string(notice), true
}

// buildTaskBoardSnapshot 创建不携带任务结果正文的结构化任务板快照。
func buildTaskBoardSnapshot(tasks []models.ChannelTask) string {
	var terminal int
	activeLines := make([]string, 0, len(tasks))
	for index, task := range tasks {
		switch task.Status {
		case constants.ChannelTaskStatusCompleted,
			constants.ChannelTaskStatusCancelled,
			constants.ChannelTaskStatusFailed:
			terminal++
		default:
			activeLines = append(activeLines, fmt.Sprintf("  #%d %s — %s", index+1, task.Subject, task.Status))
		}
	}
	if len(tasks) == 0 {
		return ""
	}
	if len(activeLines) == 0 {
		return fmt.Sprintf("=== Task Board ===\nAll %d tasks have reached a terminal state.", len(tasks))
	}
	return fmt.Sprintf(
		"=== Task Board ===\n%d/%d tasks have reached a terminal state. Remaining:\n%s",
		terminal,
		len(tasks),
		strings.Join(activeLines, "\n"),
	)
}

// teamNotificationPrompt 把本批非截断报告作为当前输入交给负责人，历史重放会排除报告投影。
func teamNotificationPrompt(
	entries []announceEntry,
	taskBoardSnapshot string,
	tasks []models.ChannelTask,
) string {
	truncatedMembers := make([]string, 0)
	for _, entry := range entries {
		if entry.Truncated {
			truncatedMembers = append(truncatedMembers, memberLabel(entry))
		}
	}
	prompt := fmt.Sprintf(
		"[System message] %d member result report(s) were durably delivered to this lead session. Continue coordination using only the non-truncated reports and structured task board below. Do not repeat this internal notification or claim to have seen the members' complete execution process.",
		len(entries),
	)
	for _, entry := range entries {
		if entry.Truncated {
			continue
		}
		prompt += fmt.Sprintf("\n\n--- Member report from %s ---\n%s", memberLabel(entry), entry.Content)
	}
	if taskBoardSnapshot != "" {
		prompt += "\n\n" + taskBoardSnapshot
	}
	if len(truncatedMembers) > 0 {
		prompt += fmt.Sprintf(
			"\n\nThe following member reports were truncated: %s. Do not draw conclusions, modify tasks, or claim completion based on these previews. Ask those members to resend concise reports of no more than 4,000 characters.",
			strings.Join(truncatedMembers, ", "),
		)
	}
	reviewCount := 0
	for _, task := range tasks {
		if task.Status == constants.ChannelTaskStatusInReview {
			reviewCount++
		}
	}
	if reviewCount > 0 {
		return fmt.Sprintf(
			"%s\n\n%d task(s) are still awaiting user review. Describe the results available for review; do not claim that all team work is complete.",
			prompt,
			reviewCount,
		)
	}
	if !teamTasksStillRunning(tasks) && len(truncatedMembers) == 0 {
		prompt += "\n\nAll tasks in this turn have reached a terminal state. Give the user one complete final summary based on all non-truncated member reports."
	}
	return prompt
}

// memberLabel 返回负责人提示中使用的成员名称。
func memberLabel(entry announceEntry) string {
	if label := strings.TrimSpace(entry.MemberDisplayName); label != "" {
		return label
	}
	return "Team member"
}
