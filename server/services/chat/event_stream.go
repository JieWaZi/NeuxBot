package chat

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

const subscriberBufferSize = 256

// PublicEvent 是 Chat 服务向 API 和 SSE 调用方交付的安全事件边界。
type PublicEvent struct {
	// SchemaVersion 标识公开事件载荷的结构版本。
	SchemaVersion int `json:"schemaVersion"`
	// Sequence 是持久化事件的恢复游标；transient 事件为零。
	Sequence int64 `json:"sequence,omitempty"`
	// Transient 标识事件仅用于实时展示，不推进恢复游标。
	Transient bool `json:"transient,omitempty"`
	// RecordedAt 是事件发生时间的 Unix 毫秒时间戳。
	RecordedAt int64 `json:"recordedAt"`
	// Type 标识公开事件的业务类型。
	Type constants.ChatEventType `json:"type"`
	// ExecutionID 是事件所属执行标识；Chat 级事件为空。
	ExecutionID string `json:"-"`
	// Message 是已经完成安全投影的消息视图。
	Message *messageview.Message `json:"message,omitempty"`
	// ExecutionStatus 是对话执行状态；非执行状态事件为空值。
	ExecutionStatus constants.ExecutionStatus `json:"executionStatus,omitempty"`
	// ErrorCode 是可以安全暴露给调用方的错误代码。
	ErrorCode string `json:"errorCode,omitempty"`
	// ErrorMessage 是可以安全暴露给调用方的错误说明。
	ErrorMessage string `json:"errorMessage,omitempty"`
	// ApprovalRequest 是审批请求事件携带的安全交互内容。
	ApprovalRequest *PublicApprovalRequest `json:"approvalRequest,omitempty"`
	// ApprovalResolution 是审批处理事件携带的安全权威结果。
	ApprovalResolution *PublicApprovalResolution `json:"approvalResolution,omitempty"`
	// ElicitationRequest 是结构化输入请求事件携带的安全交互内容。
	ElicitationRequest *PublicElicitationRequest `json:"elicitationRequest,omitempty"`
	// ElicitationResolution 是结构化输入处理事件携带的安全权威结果。
	ElicitationResolution *PublicElicitationResolution `json:"elicitationResolution,omitempty"`
}

// PublicApprovalOption 是公开审批请求中可供用户选择的一项操作。
type PublicApprovalOption struct {
	// ID 是必须原样回传 Runtime 的选项标识。
	ID string `json:"id"`
	// Name 是 Runtime 提供的用户可见名称。
	Name string `json:"name"`
	// Kind 是 Runtime 提供的选项类别。
	Kind string `json:"kind"`
}

// PublicApprovalLocation 是公开审批请求声明的一处安全文件位置。
type PublicApprovalLocation struct {
	// Path 是 Runtime 提供的文件路径。
	Path string `json:"path"`
	// Line 是 Runtime 提供的可选行号；为空表示未提供。
	Line *int `json:"line,omitempty"`
}

// PublicApprovalRequest 是服务向 API 暴露的安全审批请求。
type PublicApprovalRequest struct {
	// ApprovalID 是 NeuxBot 为本次审批生成的稳定交互标识。
	ApprovalID string `json:"approvalId"`
	// ToolCallID 是 Runtime Session 内对应工具调用的标识。
	ToolCallID string `json:"toolCallId"`
	// Title 是 Runtime 提供的安全操作标题。
	Title string `json:"title"`
	// Kind 是 Runtime 提供的工具类别。
	Kind string `json:"kind,omitempty"`
	// Summary 是 Runtime 提供或由标准字段形成的安全摘要。
	Summary string `json:"summary,omitempty"`
	// Command 是经过长度限制的命令展示摘要。
	Command string `json:"command,omitempty"`
	// Locations 是 Runtime 提供的安全文件位置。
	Locations []PublicApprovalLocation `json:"locations,omitempty"`
	// Options 是 Runtime 允许用户选择的全部有效选项。
	Options []PublicApprovalOption `json:"options"`
}

// PublicApprovalResolution 是服务向 API 暴露的审批处理结果。
type PublicApprovalResolution struct {
	// ApprovalID 是被处理的 NeuxBot 审批标识。
	ApprovalID string `json:"approvalId"`
	// ToolCallID 是对应 Runtime 工具调用标识。
	ToolCallID string `json:"toolCallId"`
	// OptionID 是用户选择的 Runtime 选项标识。
	OptionID string `json:"optionId,omitempty"`
	// OptionName 是用户选择的 Runtime 选项名称。
	OptionName string `json:"optionName,omitempty"`
	// OptionKind 是用户选择的 Runtime 选项类别。
	OptionKind string `json:"optionKind,omitempty"`
	// Command 是本次审批对应的受限命令展示摘要。
	Command string `json:"command,omitempty"`
	// Result 是审批处理的稳定业务结果。
	Result constants.ApprovalResult `json:"result"`
}

// PublicElicitationRequest 是服务向 API 暴露的安全结构化输入请求。
type PublicElicitationRequest struct {
	// AllowSkip 是 CLI 问答明确冻结的跳过约束。
	AllowSkip *bool `json:"allowSkip,omitempty"`
	// ElicitationID 是 NeuxBot 为本次交互生成的稳定标识。
	ElicitationID string `json:"elicitationId"`
	// ToolCallID 是 Runtime Session 内对应工具调用的标识。
	ToolCallID string `json:"toolCallId,omitempty"`
	// Mode 是 form 或 url。
	Mode string `json:"mode"`
	// Message 是 Runtime 提供的用户可见说明。
	Message string `json:"message"`
	// RequestedSchema 是 form 模式下的 JSON Schema 对象。
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	// URL 是 url 模式下需要用户访问的地址。
	URL string `json:"url,omitempty"`
}

// PublicElicitationResolution 是服务向 API 暴露的结构化输入处理结果。
type PublicElicitationResolution struct {
	// ElicitationID 是被处理的 NeuxBot 交互标识。
	ElicitationID string `json:"elicitationId"`
	// ToolCallID 是对应 Runtime 工具调用标识。
	ToolCallID string `json:"toolCallId,omitempty"`
	// Content 是用户提交的表单内容，拒绝或取消时为空。
	Content map[string]any `json:"content,omitempty"`
	// Result 是结构化输入处理的稳定业务结果。
	Result constants.ElicitationResult `json:"result"`
}

// Subscription 保存订阅建立时的公开历史事件和后续公开实时事件。
type Subscription struct {
	// Backlog 是游标之后已经提交并完成安全转换的历史事件。
	Backlog []PublicEvent
	// Events 持续接收安全的持久化 checkpoint 和 transient 实时快照。
	Events <-chan PublicEvent
	// Close 注销订阅并释放通道。
	Close func()
}

// streamEvent 是模块内部实时订阅中二选一的原始事件载荷。
type streamEvent struct {
	// Record 是已经写入 JSONL、可以推进恢复游标的权威记录。
	Record *eventlog.Record
	// Live 是不等待 JSONL 聚合窗口的 transient 实时消息快照。
	Live *eventstream.PublicEvent
}

// streamSubscriber 在同一注册表声明消费范围，生命周期观察不领取正文或实时快照。
type streamSubscriber struct {
	// listener 复用统一的有界通道和注销生命周期。
	listener chan streamEvent
	// executionID 非空时只接收该执行的生命周期提交信号。
	executionID string
}

// executionLifecycleRecord 限定后台业务观察需要的稀疏持久事实。
func executionLifecycleRecord(kind constants.ChatEventType) bool {
	switch kind {
	case constants.ChatEventTypeExecutionCreated, constants.ChatEventTypeExecutionStarted, constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
		return true
	}
	return false
}

// subscriptionHub 只负责进程内实时订阅者的注册、广播和释放。
// 权威 backlog 由调用方从 eventlog 读取，避免将持久化与内存广播混为一体。
type subscriptionHub struct {
	// mutex 保护订阅者集合和递增标识。
	mutex sync.Mutex
	// subscribers 按对话标识保存实时事件订阅者。
	subscribers map[string]map[uint64]streamSubscriber
	// latestLive 按对话保留当前执行尚未持久化的最新安全消息快照。
	latestLive map[string]eventstream.PublicEvent
	// nextSubscriberID 为进程内订阅生成递增标识。
	nextSubscriberID uint64
}

// newSubscriptionHub 创建空的实时订阅中心。
func newSubscriptionHub() *subscriptionHub {
	return &subscriptionHub{
		subscribers: make(map[string]map[uint64]streamSubscriber),
		latestLive:  make(map[string]eventstream.PublicEvent),
	}
}

// add 在读取 backlog 前注册订阅者，防止提交与订阅建立之间出现事件缺口。
func (h *subscriptionHub) add(chatID string, executionID string) (uint64, <-chan streamEvent) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.nextSubscriberID++
	subscriberID := h.nextSubscriberID
	if h.subscribers[chatID] == nil {
		h.subscribers[chatID] = make(map[uint64]streamSubscriber)
	}
	listener := make(chan streamEvent, subscriberBufferSize)
	h.subscribers[chatID][subscriberID] = streamSubscriber{listener: listener, executionID: executionID}
	if event, exists := h.latestLive[chatID]; exists && executionID == "" {
		live := eventstream.ClonePublicEvent(event)
		listener <- streamEvent{Live: &live}
	}
	return subscriberID, listener
}

// publishCommitted 按提交顺序广播可以推进恢复游标的权威记录。
func (h *subscriptionHub) publishCommitted(
	chatID string,
	records []eventlog.Record,
) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	for _, record := range records {
		if record.Type == constants.ChatEventTypeExecutionCompleted ||
			record.Type == constants.ChatEventTypeExecutionFailed ||
			record.Type == constants.ChatEventTypeExecutionCanceled {
			delete(h.latestLive, chatID)
			break
		}
	}
	for subscriberID, subscriber := range h.subscribers[chatID] {
		listener := subscriber.listener
		delivered := true
		for _, record := range records {
			if subscriber.executionID != "" && (record.ExecutionID != subscriber.executionID || !executionLifecycleRecord(record.Type)) {
				continue
			}
			committed := record
			select {
			case listener <- streamEvent{Record: &committed}:
			default:
				delivered = false
			}
			if !delivered {
				break
			}
		}
		if !delivered {
			close(listener)
			delete(h.subscribers[chatID], subscriberID)
		}
	}
}

// publishLive 广播不带权威 Sequence 的 transient 消息快照。
func (h *subscriptionHub) publishLive(
	chatID string,
	event eventstream.PublicEvent,
) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.latestLive[chatID] = eventstream.ClonePublicEvent(event)
	for subscriberID, subscriber := range h.subscribers[chatID] {
		if subscriber.executionID != "" {
			continue
		}
		listener := subscriber.listener
		live := eventstream.ClonePublicEvent(event)
		select {
		case listener <- streamEvent{Live: &live}:
		default:
			close(listener)
			delete(h.subscribers[chatID], subscriberID)
		}
	}
}

// remove 注销指定订阅者；重复调用保持幂等。
func (h *subscriptionHub) remove(chatID string, subscriberID uint64) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	listeners := h.subscribers[chatID]
	subscriber, exists := listeners[subscriberID]
	if !exists {
		return
	}
	close(subscriber.listener)
	delete(listeners, subscriberID)
	if len(listeners) == 0 {
		delete(h.subscribers, chatID)
	}
}

// closeAll 关闭进程内全部订阅，不影响任何权威事件。
func (h *subscriptionHub) closeAll() {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	for chatID, listeners := range h.subscribers {
		for subscriberID, subscriber := range listeners {
			close(subscriber.listener)
			delete(listeners, subscriberID)
		}
		delete(h.subscribers, chatID)
	}
	clear(h.latestLive)
}

// ListEvents 返回游标之后已经完成安全转换的权威事件。
func (m *service) ListEvents(
	_ context.Context,
	chatID string,
	after int64,
	limit int,
) ([]PublicEvent, error) {
	if after < 0 || limit < 0 {
		return nil, status_error.BadRequest
	}
	window, err := m.readProjectionWindow(chatID, after, limit)
	if err != nil {
		return nil, err
	}
	adapter, err := eventstream.ReplayAfter(window.Records, after)
	if err != nil {
		logx.Errorf(
			"project public chat events failed: chat_id=%s error=%v",
			strings.TrimSpace(chatID),
			err,
		)
		return nil, status_error.ChatLogError
	}
	return publicEventsFromStream(adapter.EventsAfter(after, limit)), nil
}

// SubscribeEvents 先注册实时监听者，再建立公开事件视图，避免订阅窗口出现事件缺口。
func (m *service) SubscribeEvents(
	ctx context.Context,
	chatID string,
	after int64,
) (Subscription, error) {
	chatID = strings.TrimSpace(chatID)
	if after < 0 {
		return Subscription{}, status_error.BadRequest
	}
	// 先注册再回读；期间新提交的原始事件进入 listener，随后由同一个 Adapter 去重并转换。
	subscriberID, listener := m.subscriptions.add(chatID, "")
	window, err := m.readProjectionWindow(chatID, after, 0)
	if err != nil {
		m.subscriptions.remove(chatID, subscriberID)
		return Subscription{}, err
	}
	adapter, err := eventstream.ReplayAfter(window.Records, after)
	if err != nil {
		m.subscriptions.remove(chatID, subscriberID)
		logx.Errorf(
			"project chat event subscription failed: chat_id=%s error=%v",
			chatID,
			err,
		)
		return Subscription{}, status_error.ChatLogError
	}
	backlog := publicEventsFromStream(adapter.EventsAfter(after, 0))
	revisions := adapter.MessageRevisions()
	adapter.ReleaseHistory()
	latestSequence := window.LatestSequence
	streamContext, cancel := context.WithCancel(ctx)
	events := make(chan PublicEvent, subscriberBufferSize)
	go m.forwardPublicEvents(
		streamContext,
		chatID,
		subscriberID,
		listener,
		adapter,
		latestSequence,
		revisions,
		events,
	)
	var once sync.Once
	return Subscription{
		Backlog: backlog,
		Events:  events,
		Close: func() {
			once.Do(func() {
				cancel()
				m.subscriptions.remove(chatID, subscriberID)
			})
		},
	}, nil
}

// SubscribeExecutionEvents 在原订阅中心先注册精确执行，再用窄权威快照补偿，不构建 UI 消息。
func (m *service) SubscribeExecutionEvents(ctx context.Context, chatID, executionID string) (Subscription, error) {
	chatID, executionID = strings.TrimSpace(chatID), strings.TrimSpace(executionID)
	if chatID == "" || executionID == "" {
		return Subscription{}, status_error.BadRequest
	}
	subscriberID, listener := m.subscriptions.add(chatID, executionID)
	snapshot, err := m.ReadExecutionOutcome(ctx, chatID, executionID)
	if err != nil {
		m.subscriptions.remove(chatID, subscriberID)
		return Subscription{}, err
	}
	streamContext, cancel := context.WithCancel(ctx)
	events := make(chan PublicEvent, subscriberBufferSize)
	go func() {
		defer close(events)
		defer m.subscriptions.remove(chatID, subscriberID)
		sequence := snapshot.LatestSequence
		for {
			select {
			case <-streamContext.Done():
				return
			case incoming, open := <-listener:
				if !open {
					return
				}
				if incoming.Record == nil || incoming.Record.Sequence <= sequence {
					continue
				}
				outcome, err := m.ReadExecutionOutcome(streamContext, chatID, executionID)
				if err != nil {
					logx.Errorf("read subscribed execution state failed: chat_id=%s execution_id=%s error=%v", chatID, executionID, err)
					return
				}
				sequence = outcome.LatestSequence
				if !sendPublicEvent(streamContext, events, executionOutcomeEvent(outcome)) {
					return
				}
			}
		}
	}()
	var once sync.Once
	return Subscription{Backlog: []PublicEvent{executionOutcomeEvent(snapshot)}, Events: events, Close: func() { once.Do(func() { cancel(); m.subscriptions.remove(chatID, subscriberID) }) }}, nil
}

// executionOutcomeEvent 只交付执行身份、持久位置和生命周期结论，正文按业务需要另读。
func executionOutcomeEvent(outcome ExecutionOutcome) PublicEvent {
	kind := constants.ChatEventTypeExecutionCreated
	switch outcome.Execution.Status {
	case constants.ExecutionStatusRunning:
		kind = constants.ChatEventTypeExecutionStarted
	case constants.ExecutionStatusCompleted:
		kind = constants.ChatEventTypeExecutionCompleted
	case constants.ExecutionStatusFailed:
		kind = constants.ChatEventTypeExecutionFailed
	case constants.ExecutionStatusCanceled:
		kind = constants.ChatEventTypeExecutionCanceled
	}
	return PublicEvent{SchemaVersion: constants.ChatLogSchemaVersion, Sequence: outcome.LatestSequence, RecordedAt: outcome.Execution.UpdatedAt, Type: kind, ExecutionID: outcome.Execution.ID, ExecutionStatus: outcome.Execution.Status, ErrorCode: outcome.Execution.ErrorCode, ErrorMessage: outcome.FailureMessage}
}

// readProjectionWindow 读取公开事件适配所需的最小权威记录窗口并统一错误语义。
func (m *service) readProjectionWindow(
	chatID string,
	after int64,
	limit int,
) (eventlog.ProjectionWindow, error) {
	chatID = strings.TrimSpace(chatID)
	window, err := m.logStore.ReadProjectionAfter(chatID, after, limit)
	if err == nil {
		return window, nil
	}
	return eventlog.ProjectionWindow{}, err
}

// forwardPublicEvents 将订阅窗口内积压的原始事件衔接到同一个安全公开事件视图。
func (m *service) forwardPublicEvents(
	ctx context.Context,
	chatID string,
	subscriberID uint64,
	listener <-chan streamEvent,
	adapter *eventstream.Adapter,
	latestSequence int64,
	revisions map[string]int64,
	events chan<- PublicEvent,
) {
	defer close(events)
	defer m.subscriptions.remove(chatID, subscriberID)
	for {
		select {
		case <-ctx.Done():
			return
		case incoming, open := <-listener:
			if !open {
				return
			}
			if incoming.Live != nil {
				live := publicEventFromStream(*incoming.Live)
				if live.Message != nil {
					messageID := live.Message.ID
					if live.Message.Revision <= revisions[messageID] {
						continue
					}
				}
				if !sendPublicEvent(ctx, events, live) {
					return
				}
				if live.Message != nil {
					revisions[live.Message.ID] = live.Message.Revision
				}
				continue
			}
			if incoming.Record == nil || incoming.Record.Sequence <= latestSequence {
				continue
			}
			streamPublic, err := adapter.Apply(*incoming.Record)
			if err != nil {
				logx.Errorf(
					"project committed chat event failed: chat_id=%s sequence=%d error=%v",
					chatID,
					incoming.Record.Sequence,
					err,
				)
				return
			}
			public := publicEventFromStream(streamPublic)
			latestSequence = public.Sequence
			if public.Message != nil &&
				public.Message.Revision > revisions[public.Message.ID] {
				revisions[public.Message.ID] = public.Message.Revision
			}
			if !sendPublicEvent(ctx, events, public) {
				return
			}
		}
	}
}

// publicEventsFromStream 将内部事件流投影为稳定的 Chat 服务结果。
func publicEventsFromStream(events []eventstream.PublicEvent) []PublicEvent {
	result := make([]PublicEvent, len(events))
	for index, event := range events {
		result[index] = publicEventFromStream(event)
	}
	return result
}

// publicEventFromStream 隔离内部事件流类型，避免 API 和生成契约依赖 internal 包。
func publicEventFromStream(event eventstream.PublicEvent) PublicEvent {
	return PublicEvent{
		SchemaVersion:         event.SchemaVersion,
		Sequence:              event.Sequence,
		Transient:             event.Transient,
		RecordedAt:            event.RecordedAt,
		Type:                  event.Type,
		ExecutionID:           event.ExecutionID,
		Message:               event.Message,
		ExecutionStatus:       event.ExecutionStatus,
		ErrorCode:             event.ErrorCode,
		ErrorMessage:          event.ErrorMessage,
		ApprovalRequest:       publicApprovalRequest(event.ApprovalRequest),
		ApprovalResolution:    publicApprovalResolution(event.ApprovalResolution),
		ElicitationRequest:    publicElicitationRequest(event.ElicitationRequest),
		ElicitationResolution: publicElicitationResolution(event.ElicitationResolution),
	}
}

func publicApprovalRequest(request *eventlog.ApprovalRequestedData) *PublicApprovalRequest {
	if request == nil {
		return nil
	}
	locations := make([]PublicApprovalLocation, len(request.Locations))
	for index, location := range request.Locations {
		locations[index] = PublicApprovalLocation{Path: location.Path, Line: location.Line}
	}
	options := make([]PublicApprovalOption, len(request.Options))
	for index, option := range request.Options {
		options[index] = PublicApprovalOption{ID: option.ID, Name: option.Name, Kind: option.Kind}
	}
	return &PublicApprovalRequest{
		ApprovalID: request.ApprovalID,
		ToolCallID: request.ToolCallID,
		Title:      request.Title,
		Kind:       request.Kind,
		Summary:    request.Summary,
		Command:    request.Command,
		Locations:  locations,
		Options:    options,
	}
}

func publicApprovalResolution(resolution *eventlog.ApprovalResolvedData) *PublicApprovalResolution {
	if resolution == nil {
		return nil
	}
	return &PublicApprovalResolution{
		ApprovalID: resolution.ApprovalID,
		ToolCallID: resolution.ToolCallID,
		OptionID:   resolution.OptionID,
		OptionName: resolution.OptionName,
		OptionKind: resolution.OptionKind,
		Command:    resolution.Command,
		Result:     resolution.Result,
	}
}

func publicElicitationRequest(request *eventlog.ElicitationRequestedData) *PublicElicitationRequest {
	if request == nil {
		return nil
	}
	return &PublicElicitationRequest{
		AllowSkip:       request.AllowSkip,
		ElicitationID:   request.ElicitationID,
		ToolCallID:      request.ToolCallID,
		Mode:            request.Mode,
		Message:         request.Message,
		RequestedSchema: append(json.RawMessage(nil), request.RequestedSchema...),
		URL:             request.URL,
	}
}

func publicElicitationResolution(
	resolution *eventlog.ElicitationResolvedData,
) *PublicElicitationResolution {
	if resolution == nil {
		return nil
	}
	var content map[string]any
	if resolution.Content != nil {
		content = make(map[string]any, len(resolution.Content))
		for key, value := range resolution.Content {
			content[key] = value
		}
	}
	return &PublicElicitationResolution{
		ElicitationID: resolution.ElicitationID,
		ToolCallID:    resolution.ToolCallID,
		Content:       content,
		Result:        resolution.Result,
	}
}

// sendPublicEvent 在订阅关闭时停止等待慢消费者。
func sendPublicEvent(ctx context.Context, target chan<- PublicEvent, event PublicEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case target <- event:
		return true
	}
}
