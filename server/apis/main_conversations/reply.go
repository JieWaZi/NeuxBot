package main_conversations

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// resolveReply 从原公开消息取回工作来源，把带作者角色的引用作为本轮的独立上下文交给 CLI。
func (op *SendMainConversationMessage) resolveReply(ctx context.Context, conversationID, messageID string) (models.MainConversationWork, workinput.Instruction, error) {
	var work models.MainConversationWork
	var quote workinput.Instruction
	service := global.MainConversationService
	quoted, err := service.Message(ctx, conversationID, messageID)
	if util.HasStatusCode(err, status_error.ChatMessageNotFound) {
		return work, quote, status_error.ChatReplyTargetUnavailable
	}
	if err != nil {
		return work, quote, err
	}
	if quoted.AdmissionPending || quoted.RemovedAt != 0 {
		return work, quote, status_error.ChatReplyTargetUnavailable
	}
	work, err = service.Source(ctx, op.SubjectType, op.SubjectID, quoted.SourceChatID)
	if err != nil {
		return work, quote, err
	}

	excerpt, truncated := quotePrefix(quoted.Content)
	if truncated && strings.TrimSpace(excerpt) == "" {
		excerpt, _ = quotePrefix(strings.TrimLeftFunc(quoted.Content, unicode.IsSpace))
	}
	reference, err := json.Marshal(map[string]any{"messageId": quoted.ID, "role": quoted.Role, "text": excerpt, "truncated": truncated})
	if err != nil {
		return work, quote, err
	}
	quote = workinput.Instruction{Name: constants.ChatReplyTargetContextName, Content: string(reference)}
	return work, quote, nil
}

// quotePrefix 取得一条公开发言的有界摘录，保留完整 Unicode 字符。
func quotePrefix(text string) (string, bool) {
	remaining := constants.ChatReplyExcerptLimit
	for offset := range text {
		if remaining == 0 {
			return text[:offset], true
		}
		remaining--
	}
	return text, false
}
