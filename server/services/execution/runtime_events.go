package execution

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventstream "github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

const (
	runtimeToolUpdateBatchSize  = 128
	runtimeToolUpdateBatchBytes = 256 * 1024
	runtimeLiveBatchInterval    = 12 * time.Millisecond
	runtimeLiveCheckpointBytes  = 128 * 1024
	runtimeLiveCheckpointEvents = 256
)

// pendingRuntimeMessage 保存尚未到达完整消息边界的 Assistant 内容。
type pendingRuntimeMessage struct {
	// invocationID 是产生该执行消息的 Runtime 调用标识。
	invocationID string
	// content 保存已经结束的规范内容段，当前连续文本只在语义边界编码一次。
	content []eventlog.RuntimeMessageContent
	// textType 是当前连续文本段的规范类型；空值表示没有待编码文本。
	textType agentapi.EventType
	// text 是当前连续文本的唯一可编辑缓冲，追加不解码或复制已有全文。
	text strings.Builder
	// messageRevision 是整条消息应用后的实时展示修订号。
	messageRevision int64
}

// pendingRuntimeToolInput 保存一个输入来源的最新参数及其原始应用位置。
type pendingRuntimeToolInput struct {
	// fields 包含稳定工具身份和本事件尚未被后续快照替换的参数字段。
	fields map[string]json.RawMessage
	// updateIndex 是该输入所属进度事件的位置，纯参数快照位于此进度事件之前。
	updateIndex int
	// withUpdate 表示参数须与该位置的原始元数据在同一事件中应用。
	withUpdate bool
}

// pendingRuntimeTool 保存同一工具的累计参数快照或有序进度批次。
type pendingRuntimeTool struct {
	// invocationID 是这组工具更新所属的 Runtime 调用。
	invocationID string
	// toolID 是待提交状态所属的稳定工具调用身份。
	toolID string
	// inputs 仅保存每个参数字段的最新来源事件，保留参数原始身份上下文。
	inputs []pendingRuntimeToolInput
	// updates 按交付顺序保留剥离累计参数后的工具正文和执行进度，不能按快照覆盖。
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
	// journal 提交完整消息事实并发布 transient 增量。
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
	// livePending 表示唯一实时状态比已公开增量更新，不缓存另一份消息副本。
	livePending bool
	// liveGeneration 使已开始等待锁的旧定时回调失效，防止终态之后补发旧增量。
	liveGeneration uint64
	// liveCheckpointBytes 统计上次完整消息段提交后新增的字节，限制恢复后缀。
	liveCheckpointBytes int
	// liveCheckpointEvents 限制尚未被完整消息段覆盖的输入数量。
	liveCheckpointEvents int
	// publishedRevision 是已公开安全增量覆盖的消息修订号。
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
	event agentapi.Event,
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

// acceptLocked 按 Runtime 交付顺序更新实时状态与完整消息，并在显示窗口或语义边界发布增量。
func (s *runtimeEventSink) acceptLocked(
	invocationID string,
	event agentapi.Event,
) error {
	if s.failure != nil {
		return s.failure
	}
	// 深拷贝 Runtime 载荷，避免调用方复用缓冲区破坏实时视图或待提交批次。
	cloned := agentapi.Event{
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
	s.liveCheckpointBytes += len(cloned.Data)
	s.liveCheckpointEvents++
	// 高频变化先进入唯一状态，在交付窗口才打包与广播新增变化；工具边界立即交付。
	if err := s.publishOrScheduleLiveLocked(cloned.Type); err != nil {
		return err
	}
	switch cloned.Type {
	case agentapi.EventTypeUsage:
		// Usage 可能由 Runtime 高频覆盖报告；只保留最后一次并在执行终态写入一次。
		s.usage = append(json.RawMessage(nil), cloned.Data...)
		return nil
	case agentapi.EventTypeTextDelta,
		agentapi.EventTypeCommentaryDelta,
		agentapi.EventTypeThinkingStart,
		agentapi.EventTypeThinkingDelta:
		if err := s.flushToolLocked(); err != nil {
			return err
		}
		if err := s.appendAssistantContentLocked(invocationID, cloned, revision); err != nil {
			return err
		}
		// 预算边界结束当前完整内容段，JSONL 不保存原始 token。
		if s.liveCheckpointBytes >= runtimeLiveCheckpointBytes || s.liveCheckpointEvents >= runtimeLiveCheckpointEvents {
			if err := s.publishLiveBatchLocked(); err != nil {
				return err
			}
			return s.flushLocked()
		}
		return nil
	case agentapi.EventTypeToolUse:
		if err := s.flushLocked(); err != nil {
			return err
		}
		return s.commitMessageLocked(constants.ChatEventTypeAssistantMessage, invocationID,
			[]eventlog.RuntimeMessageContent{runtimeMessageContent(cloned)}, revision)
	case agentapi.EventTypeToolInputSnapshot:
		if err := s.acceptToolInputSnapshotLocked(invocationID, cloned, revision); err != nil {
			return err
		}
		return s.checkpointLiveBudgetLocked()
	case agentapi.EventTypeToolUpdate:
		if err := s.appendToolUpdateLocked(invocationID, cloned, revision); err != nil {
			return err
		}
		return s.checkpointLiveBudgetLocked()
	case agentapi.EventTypeToolResult:
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

// checkpointLiveBudgetLocked 完成当前有界消息段，使新订阅者可从耐久基线恢复全部后缀。
func (s *runtimeEventSink) checkpointLiveBudgetLocked() error {
	if s.liveCheckpointBytes < runtimeLiveCheckpointBytes && s.liveCheckpointEvents < runtimeLiveCheckpointEvents {
		return nil
	}
	if err := s.publishLiveBatchLocked(); err != nil {
		return err
	}
	return s.flushLocked()
}

// publishOrScheduleLiveLocked 将实时打包频率限制在显示窗口，原始内容仍逐条归并与持久化。
func (s *runtimeEventSink) publishOrScheduleLiveLocked(kind agentapi.EventType) error {
	if s.liveAdapter.PendingCount() >= 128 {
		return s.publishLiveBatchLocked()
	}
	switch kind {
	case agentapi.EventTypeTextDelta, agentapi.EventTypeCommentaryDelta, agentapi.EventTypeThinkingDelta,
		agentapi.EventTypeToolInputSnapshot, agentapi.EventTypeToolUpdate, agentapi.EventTypeUsage:
		if s.livePending {
			return nil
		}
		s.livePending = true
		s.liveGeneration++
		generation := s.liveGeneration
		s.liveTimer = time.AfterFunc(runtimeLiveBatchInterval, func() {
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
		return s.publishLiveBatchLocked()
	}
}

// publishLiveLocked 在消费者可见边界交付当前增量，transient 不推进 JSONL 游标。
func (s *runtimeEventSink) publishLiveLocked() error {
	s.stopLiveTimerLocked()
	if _, err := s.consumeMessageFactsLocked(); err != nil {
		return err
	}
	return s.publishLiveBatchLocked()
}

// publishLiveBatchLocked 交付当前尚未发布的有序增量，不在 Runtime 应用与消息提交之间插入外部事实。
func (s *runtimeEventSink) publishLiveBatchLocked() error {
	s.stopLiveTimerLocked()
	if s.liveAdapter.MessageRevision() <= s.publishedRevision {
		return nil
	}
	for s.liveAdapter.PendingCount() > 0 {
		event, err := s.liveAdapter.Drain()
		if err != nil {
			return err
		}
		if event.Live == nil {
			break
		}
		s.journal.PublishLive(s.chatID, event)
		s.publishedRevision = event.Live.Revision
	}
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
	if err := s.publishLiveLocked(); err != nil {
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
func (s *runtimeEventSink) acceptToolInputSnapshotLocked(invocationID string, event agentapi.Event, revision int64) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &fields); err != nil {
		return status_error.RuntimeProtocolIncompatible
	}
	if fields["content"] != nil || fields["output"] != nil || fields["rawOutput"] != nil {
		return status_error.RuntimeProtocolIncompatible
	}
	return s.appendToolFieldsLocked(invocationID, event, revision, fields)
}

// appendToolUpdateLocked 批量保存同一工具的有序正文和执行进度，不把它们当参数快照覆盖。
func (s *runtimeEventSink) appendToolUpdateLocked(invocationID string, event agentapi.Event, revision int64) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &fields); err != nil {
		return status_error.RuntimeProtocolIncompatible
	}
	return s.appendToolFieldsLocked(invocationID, event, revision, fields)
}

// appendToolFieldsLocked 仅覆盖参数字段，其余快照字段与工具进度一起按原始顺序提交。
func (s *runtimeEventSink) appendToolFieldsLocked(invocationID string, event agentapi.Event, revision int64, fields map[string]json.RawMessage) error {
	toolID := runtimeToolID(fields)
	if s.pending != nil || s.pendingTool != nil && (s.pendingTool.toolID != toolID || s.pendingTool.invocationID != invocationID) {
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
	// 混合事件中的参数是替换快照，正文和扩展元数据仍是有序进度。
	// 二者分开保存，防止每个字符都重复持久化不断增长的完整参数。
	progress := event
	var inputFields map[string]json.RawMessage
	for _, key := range []string{"rawInput", "input"} {
		if value, present := fields[key]; present {
			if inputFields == nil {
				identity, _ := json.Marshal(toolID)
				inputFields = map[string]json.RawMessage{"toolCallId": identity}
			}
			inputFields[key] = value
			delete(fields, key)
		}
	}
	if inputFields != nil {
		data, err := json.Marshal(fields)
		if err != nil {
			return status_error.RuntimeEventEncodingFailed
		}
		progress.Data = data
	}
	// 纯参数快照只需保留最新输入；名称、状态、位置和扩展字段保持原始交付顺序。
	keepProgress := event.Type != agentapi.EventTypeToolInputSnapshot || inputFields == nil
	for key := range fields {
		switch key {
		case "id", "tool_use_id", "toolCallId", "tool_call_id", "call_id":
		default:
			keepProgress = true
		}
	}
	if inputFields != nil {
		// 每个参数字段只保留最新来源；两种规范输入字段交替出现时仍各自在原事件上下文中投影。
		retained := s.pendingTool.inputs[:0]
		for _, input := range s.pendingTool.inputs {
			for key := range inputFields {
				if key != "toolCallId" {
					s.liveCheckpointBytes = max(0, s.liveCheckpointBytes-len(input.fields[key]))
					delete(input.fields, key)
				}
			}
			if len(input.fields) > 1 {
				retained = append(retained, input)
			}
		}
		clear(s.pendingTool.inputs[len(retained):])
		s.pendingTool.inputs = append(retained, pendingRuntimeToolInput{
			fields: inputFields, updateIndex: len(s.pendingTool.updates), withUpdate: keepProgress,
		})
	}
	if keepProgress {
		s.pendingTool.updates = append(s.pendingTool.updates, runtimeMessageContent(progress))
		s.pendingTool.updateBytes += len(progress.Data)
	}
	s.pendingTool.messageRevision = revision
	if len(s.pendingTool.updates) >= runtimeToolUpdateBatchSize || s.pendingTool.updateBytes >= runtimeToolUpdateBatchBytes {
		return s.flushToolLocked()
	}
	return nil
}

func runtimeToolID(fields map[string]json.RawMessage) string {
	var id string
	_ = json.Unmarshal(fields["toolCallId"], &id)
	return id
}

// flushToolLocked 提交最新工具参数或有序进度批次；开始事实已经独立持久化。
func (s *runtimeEventSink) flushToolLocked() error {
	if s.pendingTool == nil {
		return nil
	}
	pending := s.pendingTool
	content := make([]eventlog.RuntimeMessageContent, 0, len(pending.updates)+len(pending.inputs))
	inputIndex := 0
	for index := 0; index <= len(pending.updates); index++ {
		var update eventlog.RuntimeMessageContent
		if index < len(pending.updates) {
			update = pending.updates[index]
		}
		for inputIndex < len(pending.inputs) && pending.inputs[inputIndex].updateIndex == index {
			input := pending.inputs[inputIndex]
			if input.withUpdate {
				// 同事件身份字段会影响参数的安全投影，必须把最新参数放回原事件而非提前或延后应用。
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(update.Data, &fields); err != nil {
					return status_error.RuntimeProtocolIncompatible
				}
				for key, value := range input.fields {
					fields[key] = value
				}
				data, err := json.Marshal(fields)
				if err != nil {
					return status_error.RuntimeEventEncodingFailed
				}
				update.Data = data
			} else {
				data, err := json.Marshal(input.fields)
				if err != nil {
					return status_error.RuntimeEventEncodingFailed
				}
				content = append(content, eventlog.RuntimeMessageContent{Type: agentapi.EventTypeToolInputSnapshot, Data: data})
			}
			inputIndex++
		}
		if index < len(pending.updates) {
			content = append(content, update)
		}
	}
	s.pendingTool = nil
	return s.commitMessageLocked(constants.ChatEventTypeAssistantMessage, pending.invocationID,
		content, pending.messageRevision)
}

// appendAssistantContentLocked 将相邻同类文本合并进当前完整 Assistant 消息。
func (s *runtimeEventSink) appendAssistantContentLocked(
	invocationID string,
	event agentapi.Event,
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

func runtimeMessageContent(event agentapi.Event) eventlog.RuntimeMessageContent {
	return eventlog.RuntimeMessageContent{
		Type: event.Type,
		Data: append(json.RawMessage(nil), event.Data...),
	}
}

func isMergeableRuntimeText(eventType agentapi.EventType) bool {
	return eventType == agentapi.EventTypeTextDelta ||
		eventType == agentapi.EventTypeCommentaryDelta ||
		eventType == agentapi.EventTypeThinkingDelta
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
	s.liveCheckpointBytes = 0
	s.liveCheckpointEvents = 0
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
