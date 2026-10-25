package execution

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// pendingRuntimeMessage 保存尚未到达完整消息边界的 Assistant 内容。
type pendingRuntimeMessage struct {
	// invocationID 是产生该执行消息的 Runtime 调用标识。
	invocationID string
	// content 保存按发生顺序聚合的规范消息内容。
	content []eventlog.RuntimeMessageContent
	// messageRevision 是整条消息应用后的实时展示修订号。
	messageRevision int64
}

// mergeRuntimeTextData 解码并拼接两个同类型文本载荷。
func mergeRuntimeTextData(left []byte, right []byte) ([]byte, error) {
	var leftPayload struct {
		// Text 是已经等待持久化的连续文本。
		Text string `json:"text"`
	}
	if err := json.Unmarshal(left, &leftPayload); err != nil {
		return nil, err
	}
	var rightPayload struct {
		// Text 是新到达并等待合并的连续文本。
		Text string `json:"text"`
	}
	if err := json.Unmarshal(right, &rightPayload); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		// Text 是同一语义阶段合并后的连续文本。
		Text string `json:"text"`
	}{Text: leftPayload.Text + rightPayload.Text})
}

// runtimeEventSink 将 Runtime 事件分别交付给实时消息视图与完整消息持久化消费者。
type runtimeEventSink struct {
	// agentFacts 提供本执行已持久化但尚未投影的命令事实。
	agentFacts *agentCommandFacts
	// journal 提交完整消息事实并发布 transient 快照。
	journal chatservice.Journal
	// chatID 是事件所属对话标识。
	chatID string
	// executionID 是事件所属执行标识。
	executionID string
	// mutex 串行化 Runtime 交付、完整消息提交和终态 Flush。
	mutex sync.Mutex
	// liveAdapter 保存不等待持久化窗口的实时消息视图状态。
	liveAdapter *eventstream.LiveAdapter
	// pending 保存尚未遇到工具或 Invocation 边界的 Assistant 内容。
	pending *pendingRuntimeMessage
	// usage 保存 Runtime 最后一次报告的规范化 Token 用量。
	usage json.RawMessage
	// messageRevision 保存实时消息视图已经分配的最后修订号。
	messageRevision int64
	// failure 保存完整消息提交遇到的首个持久化错误。
	failure error
}

// acceptInvocation 把 Invocation 标识和事件作为一个不可分割输入处理，避免并发事件
// 在“切换标识”和“写入事件”之间互相覆盖。
func (s *runtimeEventSink) acceptInvocation(
	_ context.Context,
	invocationID string,
	event runtimes.Event,
) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.acceptLocked(invocationID, event)
}

// acceptLocked 按 Runtime 交付顺序立即发布实时快照，并聚合完整执行消息。
func (s *runtimeEventSink) acceptLocked(
	invocationID string,
	event runtimes.Event,
) error {
	if s.failure != nil {
		return s.failure
	}
	// 深拷贝 Runtime 载荷，避免调用方复用缓冲区破坏实时视图或待提交批次。
	cloned := runtimes.Event{
		Type: event.Type,
		Data: append([]byte(nil), event.Data...),
	}
	if err := s.ensureLiveAdapterLocked(); err != nil {
		return err
	}
	for _, record := range s.agentFacts.take() {
		if err := s.liveAdapter.ApplyCommandFact(record); err != nil {
			return err
		}
	}
	liveEvent, revision, err := s.liveAdapter.Apply(invocationID, cloned)
	if err != nil {
		return err
	}
	s.messageRevision = revision
	// transient 快照优先发布以降低界面延迟，但它不携带权威 Sequence，也不能推进恢复游标。
	s.journal.PublishLive(s.chatID, liveEvent)
	switch cloned.Type {
	case runtimes.EventTypeUsage:
		// Usage 可能由 Runtime 高频覆盖报告；只保留最后一次并在执行终态写入一次。
		s.usage = append(json.RawMessage(nil), cloned.Data...)
		return nil
	case runtimes.EventTypeTextDelta,
		runtimes.EventTypeCommentaryDelta,
		runtimes.EventTypeThinkingStart,
		runtimes.EventTypeThinkingDelta:
		return s.appendAssistantContentLocked(invocationID, cloned, revision)
	case runtimes.EventTypeToolUse:
		if err := s.flushLocked(); err != nil {
			return err
		}
		return s.commitMessageLocked(
			constants.ChatEventTypeAssistantMessage,
			invocationID,
			[]eventlog.RuntimeMessageContent{runtimeMessageContent(cloned)},
			revision,
		)
	case runtimes.EventTypeToolResult:
		if err := s.flushLocked(); err != nil {
			return err
		}
		return s.commitMessageLocked(
			constants.ChatEventTypeToolMessage,
			invocationID,
			[]eventlog.RuntimeMessageContent{runtimeMessageContent(cloned)},
			revision,
		)
	default:
		// 状态、Session、错误和诊断由现有生命周期事实持久化；Raw 心跳不进入 JSONL。
		return nil
	}
}

// appendAssistantContentLocked 将相邻同类文本合并进当前完整 Assistant 消息。
func (s *runtimeEventSink) appendAssistantContentLocked(
	invocationID string,
	event runtimes.Event,
	messageRevision int64,
) error {
	if s.pending != nil && s.pending.invocationID != invocationID {
		if err := s.flushLocked(); err != nil {
			return err
		}
	}
	if s.pending == nil {
		s.pending = &pendingRuntimeMessage{
			invocationID: invocationID,
			content:      make([]eventlog.RuntimeMessageContent, 0, 1),
		}
	}
	content := runtimeMessageContent(event)
	lastIndex := len(s.pending.content) - 1
	if lastIndex >= 0 &&
		isMergeableRuntimeText(event.Type) &&
		s.pending.content[lastIndex].Type == content.Type {
		merged, err := mergeRuntimeTextData(s.pending.content[lastIndex].Data, content.Data)
		if err != nil {
			return status_error.RuntimeUnavailable
		}
		s.pending.content[lastIndex].Data = merged
	} else {
		s.pending.content = append(s.pending.content, content)
	}
	s.pending.messageRevision = messageRevision
	return nil
}

func runtimeMessageContent(event runtimes.Event) eventlog.RuntimeMessageContent {
	return eventlog.RuntimeMessageContent{
		Type: event.Type,
		Data: append(json.RawMessage(nil), event.Data...),
	}
}

func isMergeableRuntimeText(eventType runtimes.EventType) bool {
	return eventType == runtimes.EventTypeTextDelta ||
		eventType == runtimes.EventTypeCommentaryDelta ||
		eventType == runtimes.EventTypeThinkingDelta
}

func (s *runtimeEventSink) ensureLiveAdapterLocked() error {
	if s.liveAdapter != nil {
		return nil
	}
	records, err := s.journal.ReadExecutionRecords(s.chatID, s.executionID)
	if err != nil {
		return err
	}
	adapter, err := eventstream.NewLiveAdapter(records, s.executionID)
	if err != nil {
		return err
	}
	s.liveAdapter = adapter
	return nil
}

// Flush 在 Invocation 边界同步提交已经完整形成的 Assistant 消息。
func (s *runtimeEventSink) Flush() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.failure != nil {
		return s.failure
	}
	return s.flushLocked()
}

func (s *runtimeEventSink) flushLocked() error {
	if s.pending == nil || len(s.pending.content) == 0 {
		return nil
	}
	pending := s.pending
	s.pending = nil
	return s.commitMessageLocked(
		constants.ChatEventTypeAssistantMessage,
		pending.invocationID,
		pending.content,
		pending.messageRevision,
	)
}

func (s *runtimeEventSink) commitMessageLocked(
	eventType constants.ChatEventType,
	invocationID string,
	content []eventlog.RuntimeMessageContent,
	messageRevision int64,
) error {
	input := eventlog.AppendInput{
		Type:         eventType,
		ExecutionID:  s.executionID,
		InvocationID: invocationID,
		Data: marshalData(eventlog.RuntimeMessageData{
			Content:         append([]eventlog.RuntimeMessageContent(nil), content...),
			MessageRevision: messageRevision,
		}),
	}
	// Journal 保证先 fsync JSONL、再广播 checkpoint，失败时禁止发布权威事件。
	_, err := s.journal.AppendExecutionFacts(context.Background(), s.chatID, []eventlog.AppendInput{input})
	if err != nil {
		s.failure = err
		return err
	}
	return nil
}

// Usage 返回 Runtime 最后一次报告的 Token 用量防御性副本。
func (s *runtimeEventSink) Usage() json.RawMessage {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append(json.RawMessage(nil), s.usage...)
}

// MessageRevision 返回实时消息视图已经分配的最后修订号。
func (s *runtimeEventSink) MessageRevision() int64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.messageRevision
}
