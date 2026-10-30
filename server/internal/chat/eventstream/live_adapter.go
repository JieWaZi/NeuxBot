package eventstream

import (
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

// LiveAdapter 将未持久化的 canonical Runtime Event 增量转换为安全完整消息快照。
type LiveAdapter struct {
	// messageView 保存包括尚未持久化事件在内的实时消息展示状态。
	messageView *messageview.MessageViewBuilder
	// messageID 是接收实时 Runtime 输出的 Assistant 消息标识。
	messageID string
	// nextRevision 保存目标 Assistant 消息下一次可分配的修订号。
	nextRevision int64
	// recordedAt 是最后一次状态变化的时间，窗口交付不改写事件时间。
	recordedAt int64
	// baseSequence 是初始化已折叠的权威日志边界，跳过随后排队的同一事实。
	baseSequence int64
	// appliedFacts 去重初始化之后交付的事实身份，不保留事实正文。
	appliedFacts map[int64]struct{}
}

// ApplyFact 同步当前执行已提交的交互与命令事实，不重放 Runtime 消息或伪造持久游标。
func (a *LiveAdapter) ApplyFact(record eventlog.Record) (bool, error) {
	if a == nil || a.messageView == nil || record.ExecutionID != a.messageID || record.Sequence <= 0 {
		return false, status_error.ChatLogError
	}
	switch record.Type {
	case constants.ChatEventTypeAgentCommandCommitted, constants.ChatEventTypeApprovalRequested,
		constants.ChatEventTypeApprovalResolved, constants.ChatEventTypeElicitationRequested,
		constants.ChatEventTypeElicitationResolved:
	default:
		return false, status_error.ChatLogError
	}
	if record.Sequence <= a.baseSequence {
		return false, nil
	}
	if _, exists := a.appliedFacts[record.Sequence]; exists {
		return false, nil
	}
	messageID, err := a.messageView.Apply(record)
	if err != nil {
		return false, err
	}
	a.appliedFacts[record.Sequence] = struct{}{}
	a.nextRevision = max(a.nextRevision, a.MessageRevision()+1)
	if messageID != "" {
		a.recordedAt = max(a.recordedAt, record.RecordedAt)
	}
	return messageID != "", nil
}

// MessageRevision 读取唯一实时状态的修订号，不物化整条消息快照。
func (a *LiveAdapter) MessageRevision() int64 {
	if a == nil || a.messageView == nil {
		return 0
	}
	return a.messageView.MessageRevision(a.messageID)
}

// NewLiveAdapter 从当前权威日志建立实时消息视图起点。
func NewLiveAdapter(records []eventlog.Record, messageID string) (*LiveAdapter, error) {
	builder := messageview.NewPublicMessageViewBuilder()
	var baseSequence int64
	for _, record := range records {
		if _, err := builder.Apply(record); err != nil {
			return nil, err
		}
		baseSequence = max(baseSequence, record.Sequence)
	}
	message := builder.PublicMessage(messageID)
	if message == nil {
		return nil, status_error.ChatLogError
	}
	return &LiveAdapter{
		messageView:  builder,
		messageID:    messageID,
		nextRevision: message.Revision + 1,
		baseSequence: baseSequence,
		appliedFacts: make(map[int64]struct{}),
	}, nil
}

// Apply 应用一条实时事件并返回前端可直接合并的 transient 快照及其消息修订号。
func (a *LiveAdapter) Apply(
	invocationID string,
	event runtimecontract.Event,
) (PublicEvent, int64, error) {
	revision, err := a.ApplyState(invocationID, event)
	if err != nil {
		return PublicEvent{}, 0, err
	}
	snapshot, err := a.Snapshot()
	return snapshot, revision, err
}

// ApplyState 按顺序更新唯一实时状态，不为尚未到交付窗口的增量复制整条消息。
func (a *LiveAdapter) ApplyState(invocationID string, event runtimecontract.Event) (int64, error) {
	if a == nil || a.messageView == nil {
		return 0, status_error.ChatLogError
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
		return 0, err
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
	_, err = a.messageView.Apply(record)
	if err != nil {
		return 0, err
	}
	a.recordedAt = recordedAt
	return revision, nil
}

// Snapshot 只在实时交付窗口或工具、终态边界物化当前完整安全消息。
func (a *LiveAdapter) Snapshot() (PublicEvent, error) {
	if a == nil || a.messageView == nil {
		return PublicEvent{}, status_error.ChatLogError
	}
	message := a.messageView.PublicMessage(a.messageID)
	if message == nil {
		return PublicEvent{}, status_error.ChatLogError
	}
	return PublicEvent{
		SchemaVersion: 1,
		RecordedAt:    a.recordedAt,
		Type:          constants.ChatEventTypeRuntimeEvent,
		Message:       message,
		Transient:     true,
	}, nil
}
