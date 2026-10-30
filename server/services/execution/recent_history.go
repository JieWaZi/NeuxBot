package execution

import (
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

// completedRuntimeTurn 是可交接的完整成功输入和最终回复。
type completedRuntimeTurn struct {
	// input 是已按原字节上限裁剪的用户输入或团队协调输入。
	input runtimecontract.HistoryMessage
	// answer 是已按原字节上限裁剪的助手最终回复。
	answer runtimecontract.HistoryMessage
}

// completedRuntimeTurns 从候选正文提取完整成功轮，不把失败、过程或省略消息计入轮数。
func completedRuntimeTurns(messages []messageview.Message, team bool) []completedRuntimeTurn {
	for _, message := range messages {
		if message.Actor == string(constants.ChannelMessageActorMemberReport) {
			team = true
			break
		}
	}
	completed := []completedRuntimeTurn{}
	var input *messageview.Message
	for i := range messages {
		message := &messages[i]
		switch message.Role {
		case constants.MessageRoleUser:
			if team || message.Actor == "" || message.Actor == "user" {
				input = message
			} else {
				input = nil
			}
		case constants.MessageRoleAssistant:
			if input != nil && (message.Status == constants.ExecutionStatusCompleted || message.Inherited) {
				answer := finalReplyText(*message)
				if answer != "" {
					content := input.Content
					if team && input.Actor == string(constants.ChannelMessageActorMemberReport) {
						content = teamMemberHistoryContent(*input)
					}
					completed = append(completed, completedRuntimeTurn{input: historyMessage(*input, content), answer: historyMessage(*message, answer)})
				}
			}
			input = nil
		}
	}
	return completed
}

// recentRuntimeHistory 仅选择最近完整成功轮并保留历史文本总量上限。
func recentRuntimeHistory(messages []messageview.Message, turns int, team bool) []runtimecontract.HistoryMessage {
	if turns == 0 {
		turns = constants.RuntimeHistoryDefaultTurns
	}
	completed := completedRuntimeTurns(messages, team)
	result := make([]runtimecontract.HistoryMessage, 0, turns*2)
	bytes := 0
	for i := len(completed) - 1; i >= 0 && len(result) < turns*2; i-- {
		pair := completed[i]
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
func historyMessage(message messageview.Message, content string) runtimecontract.HistoryMessage {
	return runtimecontract.HistoryMessage{
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
