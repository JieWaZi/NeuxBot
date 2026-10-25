package chat

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

// ParticipantMessageInput 描述由其他业务域投递进 Chat、但不触发 Runtime 的参与者消息。
type ParticipantMessageInput struct {
	// ID 是调用方提供并用于重试去重的稳定消息标识。
	ID string
	// Actor 是消息参与者的入口无关语义类型。
	Actor string
	// Content 是进入 Chat 上下文和界面的消息正文。
	Content string
	// SourceWakerID 是消息原始伙伴标识。
	SourceWakerID string
	// SourceChatID 是消息原始 Chat 标识。
	SourceChatID string
	// Truncated 表示正文只是受限预览，不能作为完整行动依据。
	Truncated bool
}

// AppendParticipantMessages 把一批外部参与者消息幂等提交到权威 Chat 日志。
func (m *service) AppendParticipantMessages(
	_ context.Context,
	chatID string,
	messages []ParticipantMessageInput,
) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || len(messages) == 0 {
		return status_error.BadRequest
	}
	records, state, err := m.loadChatState(chatID)
	if err != nil {
		return err
	}
	if state.chat.ActiveExecutionID != "" {
		return status_error.ChatHasActiveExecution
	}
	existing := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.Type == constants.ChatEventTypeUserMessageCreated && record.MessageID != "" {
			existing[record.MessageID] = struct{}{}
		}
	}
	facts := make([]eventlog.AppendInput, 0, len(messages))
	seen := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		message.ID = strings.TrimSpace(message.ID)
		message.Actor = strings.TrimSpace(message.Actor)
		message.Content = strings.TrimSpace(message.Content)
		message.SourceWakerID = strings.TrimSpace(message.SourceWakerID)
		message.SourceChatID = strings.TrimSpace(message.SourceChatID)
		if message.ID == "" || message.Actor == "" || message.Content == "" ||
			message.SourceWakerID == "" || message.SourceChatID == "" {
			return status_error.BadRequest
		}
		if _, duplicated := seen[message.ID]; duplicated {
			return status_error.BadRequest
		}
		seen[message.ID] = struct{}{}
		if _, alreadyCommitted := existing[message.ID]; alreadyCommitted {
			continue
		}
		facts = append(facts, eventlog.AppendInput{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: message.ID,
			Data: marshalData(eventlog.MessageCreatedData{
				Role:          constants.MessageRoleUser,
				Actor:         message.Actor,
				SourceWakerID: message.SourceWakerID,
				SourceChatID:  message.SourceChatID,
				Truncated:     message.Truncated,
				Content:       message.Content,
			}),
		})
	}
	if len(facts) == 0 {
		return nil
	}
	committed, err := m.eventCommitter.append(chatID, facts)
	if err != nil {
		return err
	}
	last := committed[len(committed)-1]
	m.queryIndexes.indexChatCursor(chatID, committed[0].Sequence-1, last)
	m.eventCommitter.publishCommitted(committed)
	return nil
}
