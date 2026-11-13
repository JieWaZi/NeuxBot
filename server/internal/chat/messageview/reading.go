package messageview

import (
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

// ReadingKind 按公开展示正文分类已持久消息。
func ReadingKind(message Message) string {
	if message.CreatedSequence <= 0 {
		return ""
	}
	if message.Role == constants.MessageRoleUser {
		if message.Actor == string(constants.ChannelMessageActorMemberReport) && strings.TrimSpace(message.Content) != "" {
			return "member_report"
		}
		return ""
	}
	if message.Role != constants.MessageRoleAssistant {
		return ""
	}
	if message.Status == constants.ExecutionStatusFailed && (message.ErrorCode != "" || strings.TrimSpace(message.ErrorMessage) != "") {
		return "failure"
	}
	for _, part := range message.DisplayParts {
		if part.Type == constants.MessageDisplayPartText && strings.TrimSpace(part.Text) != "" {
			return "answer"
		}
	}
	if len(message.DisplayParts) == 0 && strings.TrimSpace(message.Content) != "" {
		return "answer"
	}
	return ""
}
