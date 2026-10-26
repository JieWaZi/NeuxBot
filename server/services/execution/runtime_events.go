package execution

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

const (
	runtimeToolUpdateBatchSize  = 128
	runtimeToolUpdateBatchBytes = 256 * 1024
	runtimeLiveSnapshotInterval = 16 * time.Millisecond
)

// pendingRuntimeMessage 保存尚未到达完整消息边界的 Assistant 内容。
type pendingRuntimeMessage struct {
	// invocationID 是产生该执行消息的 Runtime 调用标识。
	invocationID string
	// content 保存已经结束的规范内容段，当前连续文本只在语义边界编码一次。
	content []eventlog.RuntimeMessageContent
	// textType 是当前连续文本段的规范类型；空值表示没有待编码文本。
	textType runtimes.EventType
	// text 是当前连续文本的唯一可编辑缓冲，追加不解码或复制已有全文。
	text strings.Builder
	// messageRevision 是整条消息应用后的实时展示修订号。
	messageRevision int64
}

// pendingRuntimeTool 保存同一工具的累计参数快照或有序进度批次。
type pendingRuntimeTool struct {
	// invocationID 是这组工具更新所属的 Runtime 调用。
	invocationID string
	// toolID 是待提交状态所属的稳定工具调用身份。
	toolID string
	// fields 按字段替换保存当前完整载荷，省略字段沿用前值。
	fields map[string]json.RawMessage
	// updates 按交付顺序保留工具正文和执行进度，不能按输入快照覆盖。
	updates []eventlog.RuntimeMessageContent
	// updateBytes 限制本次工具进度批次占用的原始载荷字节。
	updateBytes int
	// messageRevision 是最新一次中间更新的消息修订号。
	messageRevision int64
}

// finishText 在类型切换或持久化边界把当前文本段编码一次，保持原有完整消息协议。
func (pending *pendingRuntimeMessage) finishText() error {
	if pending.textType == "" {
		return nil
	}
	data, err := json.Marshal(struct {
		// Text 是当前语义段已经按原始交付顺序拼接的完整文本。
		Text string `json:"text"`
	}{Text: pending.text.String()})
	if err != nil {
		return status_error.RuntimeEventEncodingFailed
	}
	pending.content = append(pending.content, eventlog.RuntimeMessageContent{Type: pending.textType, Data: data})
	pending.textType = ""
	pending.text.Reset()
	return nil
}

// runtimeEventSink 将 Runtime 事件分别交付给实时消息视图与完整消息持久化消费者。
type runtimeEventSink struct {
	// messageFacts 提供本执行已持久化但尚未投影的交互与命令事实。
	messageFacts *executionMessageFacts
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
	// liveTimer 只在有未公开变化时安排一次窗口交付，由 Flush 或语义边界释放。
	liveTimer *time.Timer
	// livePending 表示唯一实时状态比已公开快照更新，不缓存另一份消息副本。
	livePending bool
	// liveGeneration 使已开始等待锁的旧定时回调失效，防止终态之后补发旧快照。
	liveGeneration uint64
	// publishedRevision 是已公开消息的修订号，未改变消息的交互事实不重复物化整条快照。
	publishedRevision int64
	// pending 保存尚未遇到工具或 Invocation 边界的 Assistant 内容。
	pending *pendingRuntimeMessage
	// pendingTool 合并累计参数或收集有界进度批次，避免每个 Token 同步落盘。
	pendingTool *pendingRuntimeTool
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
	ctx context.Context,
	invocationID string,
	event runtimes.Event,
) error {
	if err := runtimeDeliveryContextError(ctx); err != nil {
		return err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := runtimeDeliveryContextError(ctx); err != nil {
		return err
	}
	return s.acceptLocked(invocationID, event)
}

// runtimeDeliveryContextError 在交付开始前拒绝迟到事件；已开始的耐久提交由日志所有者完成后再记录终态。
func runtimeDeliveryContextError(ctx context.Context) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return status_error.OperationTimeout
	}
	return status_error.OperationCanceled
}

// acceptLocked 按 Runtime 交付顺序更新实时状态与完整消息，并在显示窗口或语义边界发布快照。
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
	if _, err := s.consumeMessageFactsLocked(); err != nil {
		return err
	}
	revision, err := s.liveAdapter.ApplyState(invocationID, cloned)
	if err != nil {
		return err
	}
	s.messageRevision = revision
	// 高频变化先进入唯一状态，在交付窗口才复制与广播整条消息；工具边界立即交付。
	if err := s.publishOrScheduleLiveLocked(cloned.Type); err != nil {
		return err
	}
	switch cloned.Type {
	case runtimes.EventTypeUsage:
		// Usage 可能由 Runtime 高频覆盖报告；只保留最后一次并在执行终态写入一次。
		s.usage = append(json.RawMessage(nil), cloned.Data...)
		return nil
	case runtimes.EventTypeTextDelta,
		runtimes.EventTypeCommentaryDelta,
		runtimes.EventTypeThinkingStart,
		runtimes.EventTypeThinkingDelta:
		if err := s.flushToolLocked(); err != nil {
			return err
		}
		return s.appendAssistantContentLocked(invocationID, cloned, revision)
	case runtimes.EventTypeToolUse:
		if err := s.flushLocked(); err != nil {
			return err
		}
		return s.commitMessageLocked(constants.ChatEventTypeAssistantMessage, invocationID,
			[]eventlog.RuntimeMessageContent{runtimeMessageContent(cloned)}, revision)
	case runtimes.EventTypeToolInputSnapshot:
		return s.acceptToolInputSnapshotLocked(invocationID, cloned, revision)
	case runtimes.EventTypeToolUpdate:
		return s.appendToolUpdateLocked(invocationID, cloned, revision)
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

// publishOrScheduleLiveLocked 将实时物化频率限制在显示窗口，原始内容仍逐条归并与持久化。
func (s *runtimeEventSink) publishOrScheduleLiveLocked(kind runtimes.EventType) error {
	switch kind {
	case runtimes.EventTypeTextDelta, runtimes.EventTypeCommentaryDelta, runtimes.EventTypeThinkingDelta,
		runtimes.EventTypeToolInputSnapshot, runtimes.EventTypeToolUpdate, runtimes.EventTypeUsage:
		if s.livePending {
			return nil
		}
		s.livePending = true
		s.liveGeneration++
		generation := s.liveGeneration
		s.liveTimer = time.AfterFunc(runtimeLiveSnapshotInterval, func() {
			s.mutex.Lock()
			defer s.mutex.Unlock()
			if generation != s.liveGeneration || !s.livePending || s.failure != nil {
				return
			}
			if err := s.publishLiveLocked(); err != nil {
				s.failure = err
			}
		})
		return nil
	default:
		// 入口已在 ApplyState 前追平事实；不能在新事件与前段提交之间再次改变修订号。
		return s.publishLiveSnapshotLocked()
	}
}

// publishLiveLocked 在消费者可见边界生成当前快照，transient 不推进 JSONL 游标。
func (s *runtimeEventSink) publishLiveLocked() error {
	s.stopLiveTimerLocked()
	if _, err := s.consumeMessageFactsLocked(); err != nil {
		return err
	}
	return s.publishLiveSnapshotLocked()
}

// publishLiveSnapshotLocked 物化当前已折叠状态，不在 Runtime 应用与消息提交之间插入外部事实。
func (s *runtimeEventSink) publishLiveSnapshotLocked() error {
	s.stopLiveTimerLocked()
	if s.liveAdapter.MessageRevision() <= s.publishedRevision {
		return nil
	}
	event, err := s.liveAdapter.Snapshot()
	if err != nil {
		return err
	}
	s.journal.PublishLive(s.chatID, event)
	s.publishedRevision = event.Message.Revision
	return nil
}

// consumeMessageFactsLocked 在 Runtime、窗口和终态边界追平已提交事实，不扫描历史日志。
func (s *runtimeEventSink) consumeMessageFactsLocked() (bool, error) {
	records := s.messageFacts.take()
	if len(records) == 0 {
		return false, nil
	}
	if err := s.ensureLiveAdapterLocked(); err != nil {
		return false, err
	}
	changed := false
	for _, record := range records {
		applied, err := s.liveAdapter.ApplyFact(record)
		if err != nil {
			return false, err
		}
		changed = changed || applied
	}
	s.messageRevision = max(s.messageRevision, s.liveAdapter.MessageRevision())
	if changed {
		// 事实先于下一条 Runtime 事件折叠，只推进当时尚未提交的 batch，不能借用未来事件的修订号。
		if s.pending != nil {
			s.pending.messageRevision = max(s.pending.messageRevision, s.messageRevision)
		}
		if s.pendingTool != nil {
			s.pendingTool.messageRevision = max(s.pendingTool.messageRevision, s.messageRevision)
		}
	}
	return changed, nil
}

// scheduleMessageFacts 只由执行拥有的事实 worker 调用，复用已有窗口且不提前初始化 pending 消息。
func (s *runtimeEventSink) scheduleMessageFacts() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.liveAdapter == nil || s.failure != nil || !s.messageFacts.hasPending() {
		return
	}
	if err := s.publishOrScheduleLiveLocked(runtimes.EventTypeToolUpdate); err != nil {
		s.failure = err
	}
}

// stopLiveTimerLocked 使旧窗口失效，不让迟到回调越过工具或执行终态。
func (s *runtimeEventSink) stopLiveTimerLocked() {
	s.liveGeneration++
	s.livePending = false
	if s.liveTimer != nil {
		s.liveTimer.Stop()
		s.liveTimer = nil
	}
}

// acceptToolInputSnapshotLocked 仅聚合 Runtime 已归一化的累计参数；工具正文与输出另走完整消息边界。
func (s *runtimeEventSink) acceptToolInputSnapshotLocked(invocationID string, event runtimes.Event, revision int64) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &fields); err != nil {
		return status_error.RuntimeProtocolIncompatible
	}
	toolID := ""
	for _, key := range []string{"id", "tool_use_id", "toolCallId", "tool_call_id", "call_id"} {
		if json.Unmarshal(fields[key], &toolID) == nil && toolID != "" {
			break
		}
	}
	if fields["content"] != nil || fields["output"] != nil || fields["rawOutput"] != nil {
		return status_error.RuntimeProtocolIncompatible
	}
	if s.pending != nil || s.pendingTool != nil && (s.pendingTool.toolID != toolID || s.pendingTool.invocationID != invocationID || s.pendingTool.updates != nil) {
		if err := s.flushLocked(); err != nil {
			return err
		}
	}
	if toolID == "" {
		return s.commitMessageLocked(constants.ChatEventTypeAssistantMessage, invocationID,
			[]eventlog.RuntimeMessageContent{runtimeMessageContent(event)}, revision)
	}
	if s.pendingTool == nil {
		s.pendingTool = &pendingRuntimeTool{invocationID: invocationID, toolID: toolID, fields: fields, messageRevision: revision}
		return nil
	}
	for key, value := range fields {
		s.pendingTool.fields[key] = value
	}
	s.pendingTool.messageRevision = revision
	return nil
}

// appendToolUpdateLocked 批量保存同一工具的有序正文和执行进度，不把它们当参数快照覆盖。
func (s *runtimeEventSink) appendToolUpdateLocked(invocationID string, event runtimes.Event, revision int64) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &fields); err != nil {
		return status_error.RuntimeProtocolIncompatible
	}
	toolID := ""
	for _, key := range []string{"id", "tool_use_id", "toolCallId", "tool_call_id", "call_id"} {
		if json.Unmarshal(fields[key], &toolID) == nil && toolID != "" {
			break
		}
	}
	if s.pending != nil || s.pendingTool != nil && (s.pendingTool.toolID != toolID || s.pendingTool.invocationID != invocationID || s.pendingTool.fields != nil) {
		if err := s.flushLocked(); err != nil {
			return err
		}
	}
	if toolID == "" {
		return s.commitMessageLocked(constants.ChatEventTypeAssistantMessage, invocationID, []eventlog.RuntimeMessageContent{runtimeMessageContent(event)}, revision)
	}
	if s.pendingTool == nil {
		s.pendingTool = &pendingRuntimeTool{invocationID: invocationID, toolID: toolID}
	}
	s.pendingTool.updates = append(s.pendingTool.updates, runtimeMessageContent(event))
	s.pendingTool.updateBytes += len(event.Data)
	s.pendingTool.messageRevision = revision
	if len(s.pendingTool.updates) >= runtimeToolUpdateBatchSize || s.pendingTool.updateBytes >= runtimeToolUpdateBatchBytes {
		return s.flushToolLocked()
	}
	return nil
}

// flushToolLocked 提交最新工具参数或有序进度批次；开始事实已经独立持久化。
func (s *runtimeEventSink) flushToolLocked() error {
	if s.pendingTool == nil {
		return nil
	}
	pending := s.pendingTool
	content := pending.updates
	if content == nil {
		data, err := json.Marshal(pending.fields)
		if err != nil {
			return status_error.RuntimeEventEncodingFailed
		}
		content = []eventlog.RuntimeMessageContent{{Type: runtimes.EventTypeToolInputSnapshot, Data: data}}
	}
	s.pendingTool = nil
	return s.commitMessageLocked(constants.ChatEventTypeAssistantMessage, pending.invocationID,
		content, pending.messageRevision)
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
	if !isMergeableRuntimeText(event.Type) {
		if err := s.pending.finishText(); err != nil {
			return err
		}
		s.pending.content = append(s.pending.content, runtimeMessageContent(event))
	} else {
		var payload struct {
			// Text 是本次新到达的增量，已累计文本不参与重复解析。
			Text string `json:"text"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return status_error.RuntimeUnavailable
		}
		if s.pending.textType != event.Type {
			if err := s.pending.finishText(); err != nil {
				return err
			}
			s.pending.textType = event.Type
		}
		s.pending.text.WriteString(payload.Text)
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
	needsLive := s.livePending
	// 任何错误返回也必须使旧 callback 失效，不能让窗口交付越过本次终态屏障。
	s.stopLiveTimerLocked()
	if s.failure != nil {
		return s.failure
	}
	changed, err := s.consumeMessageFactsLocked()
	if err != nil {
		return err
	}
	if needsLive || changed {
		if err := s.publishLiveLocked(); err != nil {
			return err
		}
	}
	return s.flushLocked()
}

func (s *runtimeEventSink) flushLocked() error {
	if s.pending == nil {
		return s.flushToolLocked()
	}
	if err := s.pending.finishText(); err != nil {
		return err
	}
	if len(s.pending.content) == 0 {
		return s.flushToolLocked()
	}
	pending := s.pending
	s.pending = nil
	if err := s.commitMessageLocked(
		constants.ChatEventTypeAssistantMessage,
		pending.invocationID,
		pending.content,
		pending.messageRevision,
	); err != nil {
		return err
	}
	return s.flushToolLocked()
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
