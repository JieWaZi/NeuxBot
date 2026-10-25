package runtimes

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	acp "github.com/coder/acp-go-sdk"
)

const acpUpdateQueueCapacity = 256

var errACPUpdateQueueFull = status_error.RuntimeUnavailable

// acpClient 实现 ACP Client 回调，并按 Session 路由当前 Invocation。
type acpClient struct {
	// mutex 保护 bridges。
	mutex sync.RWMutex
	// bridges 保存当前正在执行 Prompt 的 Session 事件接收器。
	bridges map[acp.SessionId]*acpBridge
	// binding 保存当前 Agent 的结构化交互路由策略。
	binding acpRuntimeBinding
}

// acpBridge 串行交付一个 Invocation 的 ACP 更新和 Runtime 事件。
type acpBridge struct {
	// rejectTextInteraction 在纯模型调用观察到工具或交互时立即取消，不执行宿主能力。
	rejectTextInteraction func()
	// sessionID 是当前调用使用的 ACP Session 标识。
	sessionID acp.SessionId
	// invocationID 是 Runtime 统一事件绑定的调用标识。
	invocationID string
	// runtimeID 是阶段耗时日志关联的 Runtime 标识。
	runtimeID string
	// timingStartedAt 是本次 Runtime 调用开始准备的单调时钟基准。
	timingStartedAt time.Time
	// firstUpdateOnce 保证首条 ACP Session Update 只记录一次。
	firstUpdateOnce sync.Once
	// firstDeliveryOnce 保证首条标准 Runtime 事件交付只记录一次。
	firstDeliveryOnce sync.Once
	// sink 接收跨 Runtime 统一事件。
	sink EventSink
	// workspace 保存权限判断使用的完整授权目录。
	workspace Workspace
	// policy 保存当前调用冻结的产品权限。
	policy ExecutionPolicy
	// approvalHandler 把 ACP 权限请求交给 Host 的用户交互协调器。
	approvalHandler ApprovalHandler
	// userInputState 反映宿主创建的结构化问答等待。
	userInputState UserInputState
	// elicitationHandler 把 ACP 结构化输入请求交给 Host 的用户交互协调器。
	elicitationHandler ElicitationHandler
	// binding 保存消息元数据与诊断所需的 Runtime 薄适配。
	binding acpRuntimeBinding
	// eventDeliveryTimeout 限制一次 Runtime Event Sink 调用。
	eventDeliveryTimeout time.Duration
	// updates 是有界且保持 ACP 到达顺序的交付队列。
	updates chan acpDelivery
	// activity 合并 ACP Session Update 和共享模块主动插入的事件活动。
	activity chan struct{}
	// approvalActivity 通知无活动监视器审批或结构化输入等待状态已经变化。
	approvalActivity chan struct{}
	// approvalMutex 保护 pendingApprovalCount 与 pendingElicitationCount。
	approvalMutex sync.RWMutex
	// pendingApprovalCount 是当前阻塞等待用户选择的 ACP 请求数量。
	pendingApprovalCount int
	// pendingElicitationCount 是当前阻塞等待用户提交的 ACP 结构化输入数量。
	pendingElicitationCount int
	// activate 在 invocation_started 成功交付后关闭。
	activate chan struct{}
	// done 在交付 goroutine 退出后关闭。
	done chan struct{}
	// activateOnce 保证启动屏障只关闭一次。
	activateOnce sync.Once
	// failureMutex 保护 failure。
	failureMutex sync.RWMutex
	// failure 保存 Sink、Context 或映射失败。
	failure error
	// sideEffects 只由交付 goroutine更新，并在 done 后读取。
	sideEffects SideEffectSummary
	// thinkingStarted 防止同一 Invocation 重复发送 thinking_start。
	thinkingStarted bool
	// latestContext 保存最近一次 ACP usage_update 上下文快照。
	latestContext *ContextUsageData
	// startedTools 去重权限卡片和 Runtime 事件重复发布的 ToolCall。
	startedTools map[acp.ToolCallId]struct{}
	// completedTools 去重重复终态 ToolCallUpdate。
	completedTools map[acp.ToolCallId]struct{}
	// planStarted 表示当前 Invocation 已发布过标准 ACP Plan 工具卡片。
	planStarted bool
	// planCompleted 表示当前 Invocation 的 Plan 工具卡片已经完成。
	planCompleted bool
}

// acpDelivery 保存一条原始 ACP 更新或已归一化 Runtime 事件集合。
type acpDelivery struct {
	// notification 是 Agent 发出的 Session Update。
	notification *acp.SessionNotification
	// events 是 Runtime 主动插入的预检或终态 Usage 事件。
	events []Event
}

// newACPClient 创建没有活动 Session 的 ACP Client。
func newACPClient(bindings ...acpRuntimeBinding) *acpClient {
	binding := standardACPRuntimeBinding()
	if len(bindings) > 0 {
		binding = bindings[0]
	}
	return &acpClient{bridges: make(map[acp.SessionId]*acpBridge), binding: binding}
}

// newACPBridge 创建尚未越过 invocation_started 屏障的事件接收器。
func newACPBridge(
	sessionID acp.SessionId,
	invocationID string,
	sink EventSink,
	workspace Workspace,
	policy ExecutionPolicy,
	binding acpRuntimeBinding,
	eventDeliveryTimeout time.Duration,
) *acpBridge {
	return &acpBridge{
		sessionID:            sessionID,
		invocationID:         invocationID,
		sink:                 sink,
		workspace:            workspace,
		policy:               policy,
		binding:              binding,
		eventDeliveryTimeout: eventDeliveryTimeout,
		updates:              make(chan acpDelivery, acpUpdateQueueCapacity),
		activity:             make(chan struct{}, 1),
		approvalActivity:     make(chan struct{}, 1),
		activate:             make(chan struct{}),
		done:                 make(chan struct{}),
		startedTools:         make(map[acp.ToolCallId]struct{}),
		completedTools:       make(map[acp.ToolCallId]struct{}),
	}
}

// register 安装 Session 当前唯一 Bridge，拒绝同 Session 并发 Prompt。
func (client *acpClient) register(bridge *acpBridge) error {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.bridges[bridge.sessionID] != nil {
		return status_error.BadRequest
	}
	client.bridges[bridge.sessionID] = bridge
	return nil
}

// unregister 删除精确 Bridge，之后调用方才可以安全关闭其队列。
func (client *acpClient) unregister(bridge *acpBridge) {
	client.mutex.Lock()
	if client.bridges[bridge.sessionID] == bridge {
		delete(client.bridges, bridge.sessionID)
	}
	client.mutex.Unlock()
}

// SessionUpdate 把 Agent 更新写入匹配 Session 的有界队列。
func (client *acpClient) SessionUpdate(
	_ context.Context,
	notification acp.SessionNotification,
) error {
	client.mutex.RLock()
	bridge := client.bridges[notification.SessionId]
	if bridge == nil {
		client.mutex.RUnlock()
		return nil
	}
	bridge.observeFirstUpdate()
	if bridge.rejectTextInteraction != nil && (notification.Update.ToolCall != nil || notification.Update.ToolCallUpdate != nil || notification.Update.Plan != nil) {
		bridge.rejectTextInteraction()
		bridge.setFailure(status_error.RuntimePolicyUnsupported)
		client.mutex.RUnlock()
		return status_error.RuntimePolicyUnsupported
	}
	clone := notification
	err := bridge.enqueue(acpDelivery{notification: &clone})
	client.mutex.RUnlock()
	return err
}

// RequestPermission 把 ACP 请求归一化后交给当前 Invocation 的审批端口。
func (client *acpClient) RequestPermission(
	ctx context.Context,
	request acp.RequestPermissionRequest,
) (acp.RequestPermissionResponse, error) {
	client.mutex.RLock()
	bridge := client.bridges[request.SessionId]
	client.mutex.RUnlock()
	if bridge == nil {
		return acp.RequestPermissionResponse{
			Outcome: acp.NewRequestPermissionOutcomeCancelled(),
		}, nil
	}
	if bridge.rejectTextInteraction != nil {
		bridge.rejectTextInteraction()
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
	}
	return bridge.requestPermission(ctx, request), nil
}

// UnstableCreateElicitation 把 ACP 结构化输入请求交给当前 Invocation 的协调端口。
func (client *acpClient) UnstableCreateElicitation(
	ctx context.Context,
	request acp.UnstableCreateElicitationRequest,
) (acp.UnstableCreateElicitationResponse, error) {
	client.mutex.RLock()
	sessionID, ok := acpElicitationSessionID(request, client.binding)
	bridge := client.bridges[sessionID]
	client.mutex.RUnlock()
	if !ok || bridge == nil {
		logx.Warnf(
			"ACP elicitation cancelled because session route is not exact: operation=create_elicitation session_id=%s session_recognized=%t",
			sessionID,
			ok,
		)
		return acp.NewUnstableCreateElicitationResponseCancel(), nil
	}
	if bridge.rejectTextInteraction != nil {
		bridge.rejectTextInteraction()
		return acp.NewUnstableCreateElicitationResponseCancel(), nil
	}
	return bridge.createElicitation(ctx, request), nil
}

// UnstableCompleteElicitation 接收 Agent 对 URL 结构化输入完成的通知；Host 当前无需额外动作。
func (*acpClient) UnstableCompleteElicitation(
	context.Context,
	acp.UnstableCompleteElicitationNotification,
) error {
	return nil
}

// ReadTextFile 未声明对应能力，任何意外请求都返回 method-not-found。
func (*acpClient) ReadTextFile(
	context.Context,
	acp.ReadTextFileRequest,
) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, acp.NewMethodNotFound("fs/read_text_file")
}

// WriteTextFile 未声明对应能力，任何意外请求都返回 method-not-found。
func (*acpClient) WriteTextFile(
	context.Context,
	acp.WriteTextFileRequest,
) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, acp.NewMethodNotFound("fs/write_text_file")
}

// CreateTerminal 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpClient) CreateTerminal(
	context.Context,
	acp.CreateTerminalRequest,
) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound("terminal/create")
}

// KillTerminal 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpClient) KillTerminal(
	context.Context,
	acp.KillTerminalRequest,
) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound("terminal/kill")
}

// TerminalOutput 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpClient) TerminalOutput(
	context.Context,
	acp.TerminalOutputRequest,
) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound("terminal/output")
}

// ReleaseTerminal 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpClient) ReleaseTerminal(
	context.Context,
	acp.ReleaseTerminalRequest,
) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound("terminal/release")
}

// WaitForTerminalExit 未声明 Terminal 能力，任何意外请求都返回 method-not-found。
func (*acpClient) WaitForTerminalExit(
	context.Context,
	acp.WaitForTerminalExitRequest,
) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound("terminal/wait_for_exit")
}

// enqueue 非阻塞写入有界队列，避免 Agent 通知 goroutine 被慢 Sink 卡住。
func (bridge *acpBridge) enqueue(delivery acpDelivery) error {
	if err := bridge.currentFailure(); err != nil {
		return err
	}
	select {
	case bridge.updates <- delivery:
		bridge.signalActivity()
		return nil
	default:
		bridge.setFailure(errACPUpdateQueueFull)
		return errACPUpdateQueueFull
	}
}

// signalActivity 非阻塞记录一次 ACP 或共享模块事件活动。
func (bridge *acpBridge) signalActivity() {
	select {
	case bridge.activity <- struct{}{}:
	default:
	}
}

// watchInactivity 在 Prompt 活跃期间按 Session Update 重置无活动计时器。
func (bridge *acpBridge) watchInactivity(
	ctx context.Context,
	timeout time.Duration,
	cancelPrompt context.CancelFunc,
) {
	if timeout <= 0 {
		return
	}
	var changes <-chan struct{}
	if bridge.userInputState != nil {
		changes = bridge.userInputState.Changed()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-bridge.activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if !bridge.approvalPending() {
				timer.Reset(timeout)
			}
		case <-changes:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if !bridge.approvalPending() {
				timer.Reset(timeout)
			}
		case <-bridge.approvalActivity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if !bridge.approvalPending() {
				timer.Reset(timeout)
			}
		case <-timer.C:
			if bridge.approvalPending() {
				continue
			}
			bridge.setFailure(status_error.RuntimeInactivityTimeout)
			cancelPrompt()
			return
		}
	}
}

// setApprovalPending 更新审批等待计数，并唤醒无活动监视器重新计算计时状态。
func (bridge *acpBridge) setApprovalPending(delta int) {
	bridge.approvalMutex.Lock()
	bridge.pendingApprovalCount += delta
	if bridge.pendingApprovalCount < 0 {
		bridge.pendingApprovalCount = 0
	}
	bridge.approvalMutex.Unlock()
	select {
	case bridge.approvalActivity <- struct{}{}:
	default:
	}
}

// approvalPending 判断当前 Invocation 是否仍在等待至少一个用户审批或结构化输入。
func (bridge *acpBridge) approvalPending() bool {
	bridge.approvalMutex.RLock()
	defer bridge.approvalMutex.RUnlock()
	return bridge.pendingApprovalCount > 0 || bridge.pendingElicitationCount > 0 || (bridge.userInputState != nil && bridge.userInputState.Pending())
}

// setElicitationPending 更新结构化输入等待计数，并唤醒无活动监视器重新计算计时状态。
func (bridge *acpBridge) setElicitationPending(delta int) {
	bridge.approvalMutex.Lock()
	bridge.pendingElicitationCount += delta
	if bridge.pendingElicitationCount < 0 {
		bridge.pendingElicitationCount = 0
	}
	bridge.approvalMutex.Unlock()
	select {
	case bridge.approvalActivity <- struct{}{}:
	default:
	}
}

// run 等待权威启动屏障，再按序映射并交付全部更新。
func (bridge *acpBridge) run(ctx context.Context) {
	defer close(bridge.done)
	select {
	case <-bridge.activate:
	case <-ctx.Done():
		bridge.setFailure(ctx.Err())
		return
	}
	for {
		select {
		case delivery, ok := <-bridge.updates:
			if !ok {
				return
			}
			if err := bridge.deliver(ctx, delivery); err != nil {
				bridge.setFailure(err)
				return
			}
		case <-ctx.Done():
			bridge.setFailure(ctx.Err())
			return
		}
	}
}

// deliver 将一条队列项展开为稳定 Runtime 事件并串行交付。
func (bridge *acpBridge) deliver(
	ctx context.Context,
	delivery acpDelivery,
) error {
	events := delivery.events
	if delivery.notification != nil {
		mapped, err := bridge.mapNotification(*delivery.notification)
		if err != nil {
			return err
		}
		events = mapped
	}
	for _, event := range events {
		completed, err := bridge.completeUsageEvent(event)
		if err != nil {
			return err
		}
		deliveryContext, cancelDelivery := context.WithTimeout(
			ctx,
			bridge.eventDeliveryTimeout,
		)
		err = bridge.sink.Accept(deliveryContext, completed)
		cancelDelivery()
		if err != nil {
			return err
		}
		bridge.observeFirstDelivery(completed.Type)
	}
	return nil
}

// observeFirstUpdate 记录 Agent 第一次通过 ACP 回传任意 Session Update 的时刻。
func (bridge *acpBridge) observeFirstUpdate() {
	if bridge.timingStartedAt.IsZero() {
		return
	}
	bridge.firstUpdateOnce.Do(func() {
		logx.Infof(
			"ACP invocation first update received: runtime_id=%s invocation_id=%s session_id=%s total_duration_ms=%d",
			bridge.runtimeID,
			bridge.invocationID,
			bridge.sessionID,
			time.Since(bridge.timingStartedAt).Milliseconds(),
		)
	})
}

// observeFirstDelivery 记录第一条标准 Runtime 事件已经被上层 Sink 接受的时刻。
func (bridge *acpBridge) observeFirstDelivery(eventType EventType) {
	if bridge.timingStartedAt.IsZero() {
		return
	}
	bridge.firstDeliveryOnce.Do(func() {
		logx.Infof(
			"ACP invocation first event delivered: runtime_id=%s invocation_id=%s session_id=%s event_type=%s total_duration_ms=%d",
			bridge.runtimeID,
			bridge.invocationID,
			bridge.sessionID,
			eventType,
			time.Since(bridge.timingStartedAt).Milliseconds(),
		)
	})
}

// completeUsageEvent 让每次 Usage 都携带当前已知的自包含上下文快照。
func (bridge *acpBridge) completeUsageEvent(event Event) (Event, error) {
	if event.Type != EventTypeUsage {
		return event, nil
	}
	var usage UsageData
	if err := json.Unmarshal(event.Data, &usage); err != nil {
		return Event{}, status_error.RuntimeUnavailable
	}
	if usage.Context != nil {
		contextCopy := *usage.Context
		bridge.latestContext = &contextCopy
	} else if bridge.latestContext != nil {
		contextCopy := *bridge.latestContext
		usage.Context = &contextCopy
	}
	data, err := json.Marshal(usage)
	if err != nil {
		return Event{}, status_error.RuntimeUnavailable
	}
	return Event{Type: EventTypeUsage, Data: data}, nil
}

// activateDelivery 允许 Bridge 开始交付已缓存的 Session 更新。
func (bridge *acpBridge) activateDelivery() {
	bridge.activateOnce.Do(func() { close(bridge.activate) })
}

// closeDelivery 关闭唯一发送队列并等待全部已接收更新完成。
func (bridge *acpBridge) closeDelivery(ctx context.Context) error {
	close(bridge.updates)
	select {
	case <-bridge.done:
		return bridge.currentFailure()
	case <-ctx.Done():
		return runtimeContextStatus(ctx.Err())
	}
}

// setFailure 保存首次交付失败。
func (bridge *acpBridge) setFailure(err error) {
	if err == nil {
		return
	}
	bridge.failureMutex.Lock()
	if bridge.failure == nil {
		bridge.failure = err
	}
	bridge.failureMutex.Unlock()
}

// currentFailure 返回当前失败快照。
func (bridge *acpBridge) currentFailure() error {
	bridge.failureMutex.RLock()
	defer bridge.failureMutex.RUnlock()
	return bridge.failure
}

// mapNotification 将 ACP SessionUpdate 转换为零到多条 Runtime Event。
func (bridge *acpBridge) mapNotification(
	notification acp.SessionNotification,
) ([]Event, error) {
	update := notification.Update
	switch {
	case update.AgentMessageChunk != nil:
		text := acpText(update.AgentMessageChunk.Content)
		if text == "" {
			return []Event{}, nil
		}
		bridge.sideEffects.VisibleOutput = true
		eventType := EventTypeTextDelta
		if bridge.binding.messagePhase(update.AgentMessageChunk.Meta) ==
			constants.RuntimeACPMessagePhaseCommentary {
			eventType = EventTypeCommentaryDelta
		}
		event, err := newEvent(eventType, map[string]any{"text": text})
		return []Event{event}, err
	case update.AgentThoughtChunk != nil:
		text := acpText(update.AgentThoughtChunk.Content)
		if text == "" {
			return []Event{}, nil
		}
		bridge.sideEffects.ReasoningOutput = true
		events := make([]Event, 0, 2)
		if !bridge.thinkingStarted {
			started, err := newEvent(EventTypeThinkingStart, map[string]any{})
			if err != nil {
				return nil, err
			}
			bridge.thinkingStarted = true
			events = append(events, started)
		}
		delta, err := newEvent(EventTypeThinkingDelta, map[string]any{"text": text})
		if err != nil {
			return nil, err
		}
		return append(events, delta), nil
	case update.ToolCall != nil:
		if _, exists := bridge.startedTools[update.ToolCall.ToolCallId]; exists {
			return []Event{}, nil
		}
		bridge.startedTools[update.ToolCall.ToolCallId] = struct{}{}
		bridge.observeToolSideEffects(update.ToolCall.Kind)
		payload, err := acpToolPayload(update.ToolCall, bridge.binding)
		if err != nil {
			return nil, err
		}
		event, err := newEvent(EventTypeToolUse, payload)
		if err != nil {
			return nil, err
		}
		status := update.ToolCall.Status
		if status != acp.ToolCallStatusCompleted && status != acp.ToolCallStatusFailed {
			return []Event{event}, nil
		}
		bridge.completedTools[update.ToolCall.ToolCallId] = struct{}{}
		result, err := newEvent(EventTypeToolResult, payload)
		if err != nil {
			return nil, err
		}
		return []Event{event, result}, nil
	case update.ToolCallUpdate != nil:
		status := update.ToolCallUpdate.Status
		if status == nil ||
			(*status != acp.ToolCallStatusCompleted &&
				*status != acp.ToolCallStatusFailed) {
			if !acpToolCallUpdateHasVisibleChange(update.ToolCallUpdate) {
				return []Event{}, nil
			}
			payload, err := acpToolPayload(update.ToolCallUpdate, bridge.binding)
			if err != nil {
				return nil, err
			}
			event, err := newEvent(EventTypeToolUse, payload)
			return []Event{event}, err
		}
		if _, exists := bridge.completedTools[update.ToolCallUpdate.ToolCallId]; exists {
			return []Event{}, nil
		}
		bridge.completedTools[update.ToolCallUpdate.ToolCallId] = struct{}{}
		kind := acp.ToolKindOther
		if update.ToolCallUpdate.Kind != nil {
			kind = *update.ToolCallUpdate.Kind
		}
		bridge.observeToolSideEffects(kind)
		payload, err := acpToolPayload(update.ToolCallUpdate, bridge.binding)
		if err != nil {
			return nil, err
		}
		event, err := newEvent(EventTypeToolResult, payload)
		return []Event{event}, err
	case update.UsageUpdate != nil:
		event, err := newEvent(EventTypeUsage, UsageData{
			Context: &ContextUsageData{
				UsedTokens:   int64(update.UsageUpdate.Used),
				WindowTokens: int64(update.UsageUpdate.Size),
			},
		})
		return []Event{event}, err
	case update.Plan != nil:
		return bridge.mapPlan(update.Plan)
	case update.PlanRemoved != nil:
		return bridge.removePlan()
	default:
		return []Event{}, nil
	}
}

// acpToolCallUpdateHasVisibleChange 判断非终态更新是否携带产品可展示的标准 ACP 字段。
func acpToolCallUpdateHasVisibleChange(update *acp.SessionToolCallUpdate) bool {
	return update != nil && (update.Status != nil || update.Title != nil || update.Kind != nil ||
		len(update.Content) > 0 || len(update.Locations) > 0 ||
		update.RawInput != nil || update.RawOutput != nil || len(update.Meta) > 0)
}

// mapPlan 把 ACP 标准 Plan 投影为现有产品工具卡片的增量与终态。
func (bridge *acpBridge) mapPlan(plan *acp.SessionUpdatePlan) ([]Event, error) {
	if plan == nil {
		return []Event{}, nil
	}
	entries := append([]acp.PlanEntry(nil), plan.Entries...)
	completed := len(entries) > 0
	for _, entry := range entries {
		if entry.Status != acp.PlanEntryStatusCompleted {
			completed = false
			break
		}
	}
	status := acp.ToolCallStatusInProgress
	if completed {
		status = acp.ToolCallStatusCompleted
	}
	payload := map[string]any{
		"toolCallId": acp.ToolCallId("acp-plan"),
		"name":       "update_plan",
		"title":      "Update plan",
		"kind":       acp.ToolKindThink,
		"status":     status,
		"input":      map[string]any{"plan": entries},
	}
	use, err := newEvent(EventTypeToolUse, payload)
	if err != nil {
		return nil, err
	}
	events := []Event{use}
	bridge.sideEffects.ToolUse = true
	bridge.planStarted = true
	if completed && !bridge.planCompleted {
		result, resultErr := newEvent(EventTypeToolResult, payload)
		if resultErr != nil {
			return nil, resultErr
		}
		bridge.planCompleted = true
		events = append(events, result)
	}
	return events, nil
}

// removePlan 关闭仍在展示的 ACP Plan 工具卡片。
func (bridge *acpBridge) removePlan() ([]Event, error) {
	if !bridge.planStarted || bridge.planCompleted {
		return []Event{}, nil
	}
	payload := map[string]any{
		"toolCallId": acp.ToolCallId("acp-plan"),
		"name":       "update_plan",
		"title":      "Update plan",
		"kind":       acp.ToolKindThink,
		"status":     acp.ToolCallStatusCompleted,
	}
	event, err := newEvent(EventTypeToolResult, payload)
	if err == nil {
		bridge.planCompleted = true
	}
	return []Event{event}, err
}

// observeToolSideEffects 更新失败重放判断需要的工具和文件写入事实。
func (bridge *acpBridge) observeToolSideEffects(kind acp.ToolKind) {
	bridge.sideEffects.ToolUse = true
	if kind == acp.ToolKindEdit || kind == acp.ToolKindDelete || kind == acp.ToolKindMove {
		bridge.sideEffects.FileWrite = true
	}
}

// acpText 只消费 Agent 当前声明支持的 Text 内容块。
func acpText(content acp.ContentBlock) string {
	if content.Text == nil {
		return ""
	}
	return content.Text.Text
}

// acpToolPayload 保留 ACP ToolCall 结构并归一化产品投影使用的稳定字段。
func acpToolPayload(value any, binding acpRuntimeBinding) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, status_error.RuntimeUnavailable
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, status_error.RuntimeUnavailable
	}
	if rawInput, ok := payload["rawInput"].(map[string]any); ok {
		if _, exists := payload["input"]; !exists {
			payload["input"] = rawInput
		}
		for _, key := range []string{
			"server", "tool", "arguments", "command", "cwd", "path", "query", "action",
		} {
			if _, exists := payload[key]; !exists && rawInput[key] != nil {
				payload[key] = rawInput[key]
			}
		}
	}
	name, nativeType := acpToolIdentity(payload, binding)
	if name != "" {
		payload["name"] = name
	}
	if nativeType != "" {
		payload["type"] = nativeType
	}
	if _, exists := payload["path"]; !exists {
		locations, _ := payload["locations"].([]any)
		if len(locations) > 0 {
			location, _ := locations[0].(map[string]any)
			if path, _ := location["path"].(string); path != "" {
				payload["path"] = path
			}
		}
	}
	return payload, nil
}

// acpToolIdentity 从 ACP 标准字段派生跨 Runtime 名称和可选原生动作类型。
func acpToolIdentity(
	payload map[string]any,
	binding acpRuntimeBinding,
) (string, string) {
	rawInput, _ := payload["rawInput"].(map[string]any)
	server, _ := rawInput["server"].(string)
	tool, _ := rawInput["tool"].(string)
	if server != "" && tool != "" {
		return "mcp__" + server + "__" + tool, ""
	}
	title, _ := payload["title"].(string)
	skill, _ := rawInput["skill"].(string)
	if skill != "" && strings.HasPrefix(strings.ToLower(title), "load skill") {
		return "skill", ""
	}
	itemType, _ := rawInput["type"].(string)
	if itemType == "webSearch" {
		return "web_search", "webSearch"
	}
	if acpToolPayloadHasDiff(payload) {
		return "file_modify", ""
	}
	kind, _ := payload["kind"].(string)
	switch acp.ToolKind(kind) {
	case acp.ToolKindEdit, acp.ToolKindDelete, acp.ToolKindMove:
		return "file_modify", ""
	}
	if binding.toolIdentity != nil {
		if name, nativeType := binding.toolIdentity(payload); name != "" || nativeType != "" {
			return name, nativeType
		}
	}
	if acp.ToolKind(kind) == acp.ToolKindRead &&
		strings.HasPrefix(strings.ToLower(title), "view image") {
		return "view_image", "imageView"
	}
	switch acp.ToolKind(kind) {
	case acp.ToolKindRead:
		return "read", ""
	case acp.ToolKindSearch:
		return "search", ""
	case acp.ToolKindExecute:
		return "command", ""
	case acp.ToolKindFetch:
		return "web_fetch", ""
	case acp.ToolKindThink:
		return "think", ""
	case acp.ToolKindSwitchMode:
		return "switch_mode", ""
	default:
		if title, _ := payload["title"].(string); title != "" {
			return title, ""
		}
		return "", ""
	}
}

// acpToolPayloadHasDiff 判断标准 ACP 工具内容是否包含文件 diff。
func acpToolPayloadHasDiff(payload map[string]any) bool {
	content, _ := payload["content"].([]any)
	for _, rawItem := range content {
		item, _ := rawItem.(map[string]any)
		if itemType, _ := item["type"].(string); itemType == "diff" {
			return true
		}
	}
	return false
}

var _ acp.Client = (*acpClient)(nil)
