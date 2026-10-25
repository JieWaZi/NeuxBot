package eventstream

import (
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

// LiveAdapter 将未持久化的 canonical Runtime Event 增量转换为安全完整消息快照。
type LiveAdapter struct {
	// messageView 保存包括尚未持久化事件在内的实时消息展示状态。
	messageView *messageview.MessageViewBuilder
	// messageID 是接收实时 Runtime 输出的 Assistant 消息标识。
	messageID string
	// nextRevision 保存目标 Assistant 消息下一次可分配的修订号。
	nextRevision int64
}

// ApplyCommandFact 只同步当前执行的权威命令事实，不将业务载荷伪装为 Runtime 事件。
func (a *LiveAdapter) ApplyCommandFact(record eventlog.Record) error {
	if a == nil || a.messageView == nil || record.ExecutionID != a.messageID || record.Type != constants.ChatEventTypeAgentCommandCommitted {
		return status_error.ChatLogError
	}
	_, err := a.messageView.Apply(record)
	return err
}

// NewLiveAdapter 从当前权威日志建立实时消息视图起点。
func NewLiveAdapter(records []eventlog.Record, messageID string) (*LiveAdapter, error) {
	builder := messageview.NewMessageViewBuilder()
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			return nil, err
		}
	}
	message := builder.PublicMessage(messageID)
	if message == nil {
		return nil, status_error.ChatLogError
	}
	return &LiveAdapter{
		messageView:  builder,
		messageID:    messageID,
		nextRevision: message.Revision + 1,
	}, nil
}

// Apply 应用一条实时事件并返回前端可直接合并的 transient 快照及其消息修订号。
func (a *LiveAdapter) Apply(
	invocationID string,
	event runtimes.Event,
) (PublicEvent, int64, error) {
	if a == nil || a.messageView == nil {
		return PublicEvent{}, 0, status_error.ChatLogError
	}
	revision := a.nextRevision
	a.nextRevision++
	recordedAt := time.Now().UnixMilli()
	data, err := eventlog.MarshalData(eventlog.RuntimeEventData{
		Type:            event.Type,
		Data:            append([]byte(nil), event.Data...),
		MessageRevision: revision,
	})
	if err != nil {
		return PublicEvent{}, 0, err
	}
	record := eventlog.Record{
		SchemaVersion: 1,
		Sequence:      revision,
		RecordedAt:    recordedAt,
		Type:          constants.ChatEventTypeRuntimeEvent,
		MessageID:     a.messageID,
		InvocationID:  invocationID,
		Data:          data,
	}
	changedMessageID, err := a.messageView.Apply(record)
	if err != nil {
		return PublicEvent{}, 0, err
	}
	message := a.messageView.PublicMessage(changedMessageID)
	if message == nil {
		return PublicEvent{}, 0, status_error.ChatLogError
	}
	return PublicEvent{
		SchemaVersion: 1,
		RecordedAt:    recordedAt,
		Type:          constants.ChatEventTypeRuntimeEvent,
		Message:       message,
		Transient:     true,
	}, revision, nil
}
