package execution

import (
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

// recentRuntimeHistory 仅选取最近已完成的用户与最终回复，为新原生会话提供有界上下文。
func recentRuntimeHistory(messages []messageview.Message, turns int, team bool) []runtimes.HistoryMessage {
	if turns == 0 {
		turns = constants.RuntimeHistoryDefaultTurns
	}
	for _, message := range messages {
		if message.Actor == string(constants.ChannelMessageActorMemberReport) {
			team = true
			break
		}
	}
	if team {
		return recentTeamHistory(messages, turns)
	}
	type turn struct {
		// user 保存一个完整回合的用户输入。
		user runtimes.HistoryMessage
		// assistant 保存对应回合的助手最终回复，不包含执行过程。
		assistant runtimes.HistoryMessage
	}
	completed := make([]turn, 0, turns)
	var user *messageview.Message
	for index := range messages {
		message := &messages[index]
		switch message.Role {
		case constants.MessageRoleUser:
			if message.Actor == "" || message.Actor == "user" {
				user = message
			} else {
				user = nil
			}
		case constants.MessageRoleAssistant:
			if user == nil {
				continue
			}
			if message.Status == constants.ExecutionStatusCompleted || message.Inherited {
				answer := finalReplyText(*message)
				if answer != "" {
					completed = append(completed, turn{
						user:      historyMessage(*user, user.Content),
						assistant: historyMessage(*message, answer),
					})
				}
			}
			user = nil
		}
	}
	result := make([]runtimes.HistoryMessage, 0, turns*2)
	bytes := 0
	for index := len(completed) - 1; index >= 0 && len(result) < turns*2; index-- {
		pair := completed[index]
		pairBytes := len(pair.user.Content) + len(pair.assistant.Content)
		if bytes+pairBytes > constants.RuntimeHistoryMaximumBytes {
			break
		}
		result = append(result, pair.assistant, pair.user)
		bytes += pairBytes
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

// recentTeamHistory 保留团队协调输入和报告，但仍限制新会话的消息数量与长度。
func recentTeamHistory(messages []messageview.Message, turns int) []runtimes.HistoryMessage {
	type turn struct {
		// input 保存团队协调输入或成员报告形成的回合输入。
		input runtimes.HistoryMessage
		// answer 保存与团队输入对应的负责人最终回复。
		answer runtimes.HistoryMessage
	}
	completed := make([]turn, 0, turns)
	var input *messageview.Message
	for index := range messages {
		message := &messages[index]
		switch message.Role {
		case constants.MessageRoleUser:
			input = message
		case constants.MessageRoleAssistant:
			if input == nil {
				continue
			}
			if message.Status == constants.ExecutionStatusCompleted || message.Inherited {
				answer := finalReplyText(*message)
				if answer != "" {
					content := input.Content
					if input.Actor == string(constants.ChannelMessageActorMemberReport) {
						content = teamMemberHistoryContent(*input)
					}
					completed = append(completed, turn{
						input:  historyMessage(*input, content),
						answer: historyMessage(*message, answer),
					})
				}
			}
			input = nil
		}
	}
	result := make([]runtimes.HistoryMessage, 0, turns*2)
	bytes := 0
	for index := len(completed) - 1; index >= 0 && len(result) < turns*2; index-- {
		pair := completed[index]
		pairBytes := len(pair.input.Content) + len(pair.answer.Content)
		if bytes+pairBytes > constants.RuntimeHistoryMaximumBytes {
			break
		}
		result = append(result, pair.answer, pair.input)
		bytes += pairBytes
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

// finalReplyText 从展示投影提取最终正文，排除过程说明、推理与工具结果。
func finalReplyText(message messageview.Message) string {
	parts := make([]string, 0, len(message.DisplayParts))
	for _, part := range message.DisplayParts {
		if part.Type == constants.MessageDisplayPartText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// historyMessage 将消息转换为 Runtime 输入并限制单条文本大小。
func historyMessage(message messageview.Message, content string) runtimes.HistoryMessage {
	return runtimes.HistoryMessage{
		Role: string(message.Role), Content: boundedHistoryText(content),
	}
}

// boundedHistoryText 保留单条历史文本的末尾，并保持 UTF-8 字符边界。
func boundedHistoryText(content string) string {
	const maximum = constants.RuntimeHistoryMaximumBytes / 2
	if len(content) <= maximum {
		return content
	}
	const marker = "[Earlier text omitted]\n"
	start := len(content) - maximum + len(marker)
	for start < len(content) && !utf8.RuneStart(content[start]) {
		start++
	}
	return marker + content[start:]
}
