package execution

import (
	"context"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// MainConversationMessages 根据宿主预留的工作 Chat 读取公开历史、发表消息并查询耐久回执。
type MainConversationMessages interface {
	WorkMessages(ctx context.Context, workChatID string, before int64, limit int) ([]models.MainConversationMessage, bool, error)
	PublishWithReceipt(ctx context.Context, chatID, executionID, callKey, content, replyID string) (models.MainConversationMessage, bool, error)
	Published(ctx context.Context, chatID, executionID, callKey string) (models.MainConversationMessage, bool, error)
}

// mainConversationInstruction 仅在本轮拥有公开发言能力时提供对应的模型指引。
func mainConversationInstruction(bindings []capability.Binding) (agentapi.Instruction, bool) {
	for _, binding := range bindings {
		if binding.Operation == constants.ConversationMessagePostOperation {
			return agentapi.Instruction{
				Name: "Public conversation messages",
				Content: "Publish complete messages with conversations.messages.post (conversations_messages_post over MCP). " +
					"Each call delivers immediately to the same main conversation from either input view. " +
					"For short answers, one post is enough. For longer work, publish a useful, self-contained finding or a change that affects the user's next decision as soon as you have it, then continue. " +
					"Publish the finished result when ready. Use a structured question when you need the user's answer to proceed. " +
					"While a question is pending, keep the turn open. Call interaction.questions.wait with its questionId. On completed=false, repeat the wait with the same ID until resolved. Continue from the actual answer. " +
					"Keep reasoning, tool steps and routine progress in the work transcript. Give each message its own requestId. " +
					"If its receipt is uncertain, look it up or retry with the original requestId and content. " +
					"Read earlier public messages when needed via conversations.messages.list (conversations_messages_list over MCP).",
			}, true
		}
	}
	return agentapi.Instruction{}, false
}

// MainConversationTools 只用于主会话的工作接纳。调用者传入接纳的 Chat、执行和原始交办，领域服务校验其归属。
func MainConversationTools(workChatID, executionID, originalPublicMessageID string, conversation MainConversationMessages) []ToolCapability {
	if workChatID == "" || executionID == "" || conversation == nil {
		return nil
	}
	var attempted atomic.Bool
	markAttempt := func() { attempted.Store(true) }
	post := capability.Binding{
		Attempt:   markAttempt,
		Operation: constants.ConversationMessagePostOperation,
		Call: func(ctx context.Context, req capability.Request) (capability.Result, error) {
			markAttempt()
			content, _ := req.Params["content"].(string)
			msg, replayed, err := conversation.PublishWithReceipt(ctx, workChatID, executionID, req.RequestID, content, originalPublicMessageID)
			if util.HasStatusCode(err, status_error.CreateRequestConflict) {
				return capability.Result{}, status_error.AgentCommandRequestConflict
			}
			if err != nil {
				return capability.Result{}, err
			}
			return publicMessageReceipt(msg.ID, replayed), nil
		},
		Receipt: func(ctx context.Context, key string) (capability.Result, error) {
			msg, found, err := conversation.Published(ctx, workChatID, executionID, key)
			if err != nil {
				return capability.Result{}, err
			}
			if !found {
				return capability.Result{}, status_error.AgentReceiptNotFound
			}
			return publicMessageReceipt(msg.ID, true), nil
		},
	}
	return []ToolCapability{{
		ID: constants.ConversationMessagesGuide, Name: "主会话消息", Bindings: []capability.Binding{publicHistoryBinding(workChatID, conversation), post},
		publishFinal: func(ctx context.Context, answer func(context.Context) (string, error)) error {
			if attempted.Load() || originalPublicMessageID == "" {
				return nil
			}
			text, err := answer(ctx)
			if err != nil || text == "" {
				return err
			}
			// 宿主保留键保证同一回合的直接答复只投递一次。
			_, _, err = conversation.PublishWithReceipt(ctx, workChatID, executionID, "\x00final-answer", text, originalPublicMessageID)
			return err
		},
		validateWork: func(chat models.Chat, actualExecutionID string) error {
			if !chat.HiddenFromWakerHistory || chat.ID != workChatID || actualExecutionID != executionID {
				return status_error.AgentContextUnauthorized
			}
			return nil
		},
	}}
}

// publicHistoryBinding 返回同一公开会话的消息，私有工作来源与执行元数据留在宿主。
func publicHistoryBinding(workChatID string, conversation MainConversationMessages) capability.Binding {
	return capability.Binding{Operation: constants.ConversationMessagesListOperation, Call: func(ctx context.Context, req capability.Request) (capability.Result, error) {
		var page struct {
			// BeforeSequence 只读取该游标之前的公开发言。
			BeforeSequence int64 `json:"beforeSequence"`
			// Limit 是本次最多返回的发言数。
			Limit int `json:"limit"`
		}
		if err := capability.DecodeParams(req.Params, &page); err != nil {
			return capability.Result{}, err
		}
		if page.Limit == 0 {
			page.Limit = 20
		}
		rows, more, err := conversation.WorkMessages(ctx, workChatID, page.BeforeSequence, page.Limit)
		if err != nil {
			return capability.Result{}, err
		}
		messages := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			msg := map[string]any{"id": row.ID, "role": row.Role, "content": row.Content, "createdAt": row.CreatedAt}
			if row.ReplyToMessageID != "" {
				msg["replyToMessageId"] = row.ReplyToMessageID
			}
			messages = append(messages, msg)
		}
		data := map[string]any{"messages": messages, "hasMore": more}
		if more {
			data["nextBeforeSequence"] = rows[0].Sequence
		}
		return capability.Result{Data: data}, nil
	}}
}

func publicMessageReceipt(id string, replayed bool) capability.Result {
	return capability.Result{CommandID: id, Replayed: replayed, Data: map[string]any{"messageId": id}}
}
