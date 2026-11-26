package chat

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

// ReplyTarget 保存接纳时冻结的公开引用和本轮模型摘录。
type ReplyTarget struct {
	// Reference 写入本轮公开用户消息。
	Reference eventlog.MessageReply
	// Text 是权威可见正文的有界摘录。
	Text string
	// Truncated 表示摘录省略了原正文的一部分。
	Truncated bool
}

// ReadReplyTarget 读取普通或成员 Chat 中已提交的同一会话正文。
func (m *service) ReadReplyTarget(ctx context.Context, chatID, messageID string) (ReplyTarget, error) {
	return m.readReplyTarget(ctx, "", chatID, messageID)
}

// ReadTeamReplyTarget 读取团队对话已公开到负责人 Chat 的同一条消息。
func (m *service) ReadTeamReplyTarget(ctx context.Context, threadID, leadChatID, messageID string) (ReplyTarget, error) {
	if threadID == "" {
		return ReplyTarget{}, status_error.ChatReplyTargetUnavailable
	}
	return m.readReplyTarget(ctx, threadID, leadChatID, messageID)
}

func (m *service) readReplyTarget(ctx context.Context, threadID, chatID, messageID string) (ReplyTarget, error) {
	journal, ok := m.logStore.(*conversationJournal)
	if !ok {
		return ReplyTarget{}, status_error.ChatLogError
	}
	created, err := journal.readAfter(ctx, chatID, 0, 1)
	if err != nil {
		return ReplyTarget{}, err
	}
	var data eventlog.ChatCreatedData
	if len(created) != 1 || created[0].Type != constants.ChatEventTypeCreated || json.Unmarshal(created[0].Data, &data) != nil {
		return ReplyTarget{}, status_error.ChatLogCorrupted
	}
	teamMain := threadID != ""
	if (teamMain && data.ThreadWorkspaceID != threadID) ||
		(!teamMain && data.HiddenFromWakerHistory && data.ThreadWorkspaceID == "") {
		return ReplyTarget{}, status_error.ChatReplyTargetUnavailable
	}
	message, err := m.queryIndexes.readPublicMessageThrough(ctx, chatID, messageID, math.MaxInt64)
	if err != nil {
		return ReplyTarget{}, err
	}
	body := replyMessageBody(message)
	role := constants.MessageRoleUnspecified
	author := ""
	priorTruncation := false
	if message != nil {
		role = message.Role
		if data.ThreadWorkspaceID != "" && role == constants.MessageRoleAssistant {
			author = data.WakerID
		}
		if teamMain && role == constants.MessageRoleUser {
			switch message.Actor {
			case string(constants.ChannelMessageActorUser):
				// 负责人时间线公开的真实用户输入。
			case string(constants.ChannelMessageActorMemberReport):
				// 正文已经由团队投递到负责人 Chat，不读取成员 Chat 的原文或工具过程。
				if message.CreatedSequence > 0 && message.SourceWakerID != "" && message.SourceChatID != "" {
					body, role, author = message.Content, constants.MessageRoleAssistant, message.SourceWakerID
					priorTruncation = message.Truncated
				}
			default:
				body = ""
			}
		}
	}
	if strings.TrimSpace(body) == "" {
		return ReplyTarget{}, status_error.ChatReplyTargetUnavailable
	}
	text, truncated := replyPrefix(body, constants.ChatReplyExcerptLimit)
	if truncated && strings.TrimSpace(text) == "" {
		text, _ = replyPrefix(strings.TrimLeftFunc(body, unicode.IsSpace), constants.ChatReplyExcerptLimit)
	}
	preview, previewTruncated := replyPrefix(strings.TrimSpace(body), constants.ChatReplyPreviewLimit)
	if previewTruncated {
		preview, _ = replyPrefix(preview, constants.ChatReplyPreviewLimit-1)
		preview = strings.TrimSpace(preview) + "…"
	}
	return ReplyTarget{
		Reference: eventlog.MessageReply{MessageID: message.ID, Role: role, SourceWakerID: author, Preview: preview},
		Text:      text,
		Truncated: truncated || priorTruncation,
	}, nil
}

func replyMessageBody(message *messageview.Message) string {
	if message == nil || message.CreatedSequence <= 0 || message.Question != nil ||
		(message.SourceChatID != "" && !message.Inherited) {
		return ""
	}
	if message.Role == constants.MessageRoleUser &&
		(message.Actor == "" || message.Actor == string(constants.ChannelMessageActorUser)) {
		if message.AttachmentOnly || message.CodeReferenceOnly {
			return ""
		}
		return message.Content
	}
	if message.Role != constants.MessageRoleAssistant {
		return ""
	}
	switch message.Status {
	case constants.ExecutionStatusCompleted:
		return message.AssistantResponseText(true)
	case constants.ExecutionStatusFailed, constants.ExecutionStatusCanceled:
		return message.AssistantResponseText(false)
	default:
		return ""
	}
}

// replyPrefix 按 Unicode 字符截断，保留合法的 UTF-8 边界。
func replyPrefix(text string, limit int) (string, bool) {
	for offset := range text {
		if limit == 0 {
			return text[:offset], true
		}
		limit--
	}
	return text, false
}
