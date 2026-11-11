package eventstream

import (
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
)

// LiveAdapter 将未持久化的规范 Runtime 事件转换为有序安全增量。
type LiveAdapter struct {
	// currentRevision 是实时流已经覆盖的版本，不以独立持久交互版本代替。
	currentRevision int64
	// generation 区分同一消息在进程重启或实时状态重建后的不同流。
	generation string
	// sequence 是本代次已交付的连续帧号，与持久游标分别计数。
	sequence int64
	// pending 只保留本交付窗口尚未公开的增量。
	pending []messageview.MessageIncrement

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

// NewNativeLiveAdapter 从已提交的执行开始版本建立原生消息实时投影。
func NewNativeLiveAdapter(messageID string, revision int64) *LiveAdapter {
	builder := messageview.NewPublicMessageViewBuilder()
	builder.SeedNativeLiveMessage(messageID, revision)
	return &LiveAdapter{generation: uuid.NewString(), messageID: messageID, currentRevision: revision, messageView: builder}
}

// AppendNative 消费已规范化的变化；工具部件随即时检查点交付。
func (a *LiveAdapter) AppendNative(change agent.MessageChange, recordedAt int64) error {
	update, err := a.messageView.ApplyNativeIncrement(a.messageID, change, recordedAt)
	if err != nil {
		return err
	}
	update.BaseRevision = a.currentRevision
	a.currentRevision = change.Revision
	if change.Kind == agent.MessageAppend {
		a.recordedAt = recordedAt
		a.pending = append(a.pending, update)
	}
	return nil
}

// NativeCheckpointNeedsRebase 判断检查点是否改变了已有的部件边界。
func (a *LiveAdapter) NativeCheckpointNeedsRebase(change agent.MessageChange) bool {
	switch change.Kind {
	case agent.MessageClassify, agent.MessageReplace:
		return true
	case agent.MessageToolSnapshot:
		// 权威索引保留最新工具快照的位置，旧位置上的相邻文本可能重新合并。
		return a.messageView.NativeToolMovesPart(a.messageID, change.ToolCallID)
	default:
		return false
	}
}

// RebaseNative 用已提交的定性或正文替换更新实时投影的部件边界。
func (a *LiveAdapter) RebaseNative(message agent.StoredMessage, cursor, recordedAt int64) error {
	if err := a.messageView.ReplaceStoredContent(message, cursor, recordedAt); err != nil {
		return err
	}
	a.currentRevision = message.Revision
	return nil
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
	messageID, change, err := a.messageView.ApplyIncrement(record, a.messageID)
	if err != nil {
		return false, err
	}
	a.appliedFacts[record.Sequence] = struct{}{}
	if messageID != "" {
		change.BaseRevision = a.currentRevision
		change.Revision = max(a.nextRevision, change.Revision)
		a.currentRevision = change.Revision
		a.nextRevision = change.Revision + 1
	}
	if messageID != "" {
		a.recordedAt = max(a.recordedAt, record.RecordedAt)
		a.pending = append(a.pending, change)
	}
	return messageID != "", nil
}

// MessageRevision 读取唯一实时状态的修订号，不物化整条消息快照。
func (a *LiveAdapter) MessageRevision() int64 {
	if a == nil || a.messageView == nil {
		return 0
	}
	return a.currentRevision
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
		generation:      uuid.NewString(),
		currentRevision: message.LiveRevision,
		messageView:     builder,
		messageID:       messageID,
		nextRevision:    message.Revision + 1,
		baseSequence:    baseSequence,
		appliedFacts:    make(map[int64]struct{}),
	}, nil
}

// Apply 应用一条实时事件并交付当前窗口的安全增量。
func (a *LiveAdapter) Apply(
	invocationID string,
	event agent.Event,
) (PublicEvent, int64, error) {
	revision, err := a.ApplyState(invocationID, event)
	if err != nil {
		return PublicEvent{}, 0, err
	}
	batch, err := a.Drain()
	return batch, revision, err
}

// ApplyState 按顺序更新唯一实时状态，不为尚未到交付窗口的增量复制整条消息。
func (a *LiveAdapter) ApplyState(invocationID string, event agent.Event) (int64, error) {
	if a == nil || a.messageView == nil {
		return 0, status_error.ChatLogError
	}
	// 一次上游大文本也按 UTF-8 边界拆成有界规范输入，不能绕过帧字节预算。
	switch event.Type {
	case agent.EventTypeTextDelta, agent.EventTypeCommentaryDelta, agent.EventTypeThinkingDelta:
		var text struct { // Text 是规范输入的新增正文。
			Text string `json:"text"`
		}
		if json.Unmarshal(event.Data, &text) == nil && len(text.Text) > 16*1024 {
			var revision int64
			for len(text.Text) > 0 {
				n := min(len(text.Text), 16*1024)
				for n < len(text.Text) && !utf8.RuneStart(text.Text[n]) {
					n--
				}
				data, _ := json.Marshal(struct { // Text 是按 UTF-8 边界拆分的新增片段。
					Text string `json:"text"`
				}{Text: text.Text[:n]})
				var err error
				revision, err = a.ApplyState(invocationID, agent.Event{Type: event.Type, Data: data})
				if err != nil {
					return 0, err
				}
				text.Text = text.Text[n:]
			}
			return revision, nil
		}
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
	_, change, err := a.messageView.ApplyIncrement(record, a.messageID)
	if err != nil {
		return 0, err
	}
	change.BaseRevision = a.currentRevision
	change.Revision = revision
	a.currentRevision = revision
	a.recordedAt = recordedAt
	a.pending = append(a.pending, change)
	return revision, nil
}

// LiveMessageIncrement 是临时消息流的一帧，正文只包含本帧新增字节。
type LiveMessageIncrement struct {
	// Generation 是本实时状态的唯一代次。
	Generation string `json:"generation"`
	// Sequence 是本代次严格连续的交付帧号。
	Sequence int64 `json:"sequence"`
	// MessageID 是增量目标的稳定消息身份。
	MessageID string `json:"messageId"`
	// ExecutionID 是增量所属的既有执行身份。
	ExecutionID string `json:"executionId"`
	// BaseRevision 是本帧首次变化前要求的消息覆盖版本。
	BaseRevision int64 `json:"baseRevision"`
	// Revision 是应用完整帧后覆盖的消息版本。
	Revision int64 `json:"revision"`
	// Updates 按原始输入顺序携带安全变化；每项可以单独与持久覆盖版本核验。
	Updates []messageview.MessageIncrement `json:"updates"`
}

// PendingCount 返回尚未交付的输入数量，供生产者执行明确的批次上限。
func (a *LiveAdapter) PendingCount() int {
	if a == nil {
		return 0
	}
	return len(a.pending)
}

// Drain 交付尚未公开的增量，不访问累计正文或未变化的工具部件。
func (a *LiveAdapter) Drain() (PublicEvent, error) {
	if a == nil || a.messageView == nil {
		return PublicEvent{}, status_error.ChatLogError
	}
	if len(a.pending) == 0 {
		return PublicEvent{}, nil
	}
	a.sequence++
	// 每帧按新增载荷与操作数量有界；序列化的仅是新变化。
	count, bytes := 0, 0
	for count < len(a.pending) && count < 128 {
		encoded, err := json.Marshal(a.pending[count])
		if err != nil {
			return PublicEvent{}, status_error.ChatLogError
		}
		if count > 0 && bytes+len(encoded) > 64*1024 {
			break
		}
		bytes += len(encoded)
		count++
	}
	updates := a.pending[:count:count]
	a.pending = a.pending[count:]
	if len(a.pending) == 0 {
		a.pending = nil
	}
	return PublicEvent{SchemaVersion: 1, RecordedAt: a.recordedAt, Type: constants.ChatEventTypeRuntimeEvent, Transient: true,
		Live: &LiveMessageIncrement{Generation: a.generation, Sequence: a.sequence, MessageID: a.messageID, ExecutionID: a.messageID,
			BaseRevision: updates[0].BaseRevision, Revision: updates[len(updates)-1].Revision, Updates: updates}}, nil
}
