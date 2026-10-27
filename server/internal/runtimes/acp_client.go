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
	acp "github.com/JieWaZi/acp-go/sdk"
)

const (
	acpUpdateQueueCapacity = 256
	acpUpdateQueueBytes    = 16 * 1024 * 1024
)

var errACPUpdateQueueFull = status_error.RuntimeEventQueueOverflow

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
	// sink 接收普通聊天的跨 Runtime 统一事件，纯文本调用不使用。
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
	updates chan *acpDelivery
	// queueMutex 保护尚未被消费者取走的队尾快照。
	queueMutex sync.Mutex
	// queuedBytes 限制仍在队列中等待交付的载荷字节，不包括当前 Sink 调用。
	queuedBytes int
	// queueChanged 在消费者取走项目后唤醒条数或字节预算等待者，受 queueMutex 保护。
	queueChanged chan struct{}
	// enqueueMutex 保持多个生产者的入队顺序，等待空位时不占用消费者的快照锁。
	enqueueMutex sync.Mutex
	// queueClosed 只在 enqueueMutex 下访问，禁止已关闭队列继续接收直接生产者。
	queueClosed bool
	// routeMutex 保护回调引用接纳与路由关闭之间的原子屏障。
	routeMutex sync.Mutex
	// routeClosed 禁止已移除的 Bridge 再取得新回调引用。
	routeClosed bool
	// routeReferences 等待已取得引用的通知完成后再关闭发送队列。
	routeReferences sync.WaitGroup
	// promptDone 让正在等待预算的回调立即响应本次 Prompt 的取消。
	promptDone <-chan struct{}
	// lastQueued 指向可合并的队尾，已开始交付的项目不能再修改。
	lastQueued *acpDelivery
	// cancelPrompt 在首次交付失败后停止原生调用，避免后台继续产生不可展示的副作用。
	cancelPrompt context.CancelFunc
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
	// deliveryContext 在排空窗口结束时阻止消费者继续接纳队列内容。
	deliveryContext context.Context
	// cancelDelivery 取消当前 Sink 的协作式 Context；同步文件 I/O 仍由持久化所有者收尾。
	cancelDelivery context.CancelFunc
	// activateOnce 保证启动屏障只关闭一次。
	activateOnce sync.Once
	// failureMutex 保护交付失败与调用终止首因快照。
	failureMutex sync.RWMutex
	// failure 保存 Sink、Context 或映射失败。
	failure error
	// primaryCause 保存最先已知的调用终止原因，独立于是否需要停止事件交付。
	primaryCause error
	// primaryStage 为空表示首因为交付失败；非空时保存 Prompt 等已知终止阶段。
	primaryStage string
	// failureSignal 在首个失败时关闭，唤醒等待队列空位的生产者。
	failureSignal chan struct{}
	// stateMutex 保护事件映射与终态读取之间的副作用快照。
	stateMutex sync.RWMutex
	// sideEffects 由交付 goroutine 更新，超时收尾通过 stateMutex 读取稳定副本。
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
	// bytes 是该项目在有界队列中占用的序列化载荷字节。
	bytes int
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
	deliveryContext, cancelDelivery := context.WithCancel(context.Background())
	return &acpBridge{
		sessionID:            sessionID,
		invocationID:         invocationID,
		sink:                 sink,
		workspace:            workspace,
		policy:               policy,
		binding:              binding,
		eventDeliveryTimeout: eventDeliveryTimeout,
		updates:              make(chan *acpDelivery, acpUpdateQueueCapacity),
		queueChanged:         make(chan struct{}),
		activity:             make(chan struct{}, 1),
		approvalActivity:     make(chan struct{}, 1),
		activate:             make(chan struct{}),
		done:                 make(chan struct{}),
		deliveryContext:      deliveryContext,
		cancelDelivery:       cancelDelivery,
		failureSignal:        make(chan struct{}),
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
	bridge.routeMutex.Lock()
	bridge.routeClosed = true
	bridge.routeMutex.Unlock()
	client.mutex.Unlock()
	bridge.routeReferences.Wait()
}

// SessionUpdate 将文本调用直接收集为结果，普通聊天更新写入匹配 Session 的有界队列。
func (client *acpClient) SessionUpdate(
	_ context.Context,
	notification acp.SessionNotification,
) error {
	client.mutex.RLock()
	bridge := client.bridges[notification.SessionId]
	if bridge == nil || !bridge.beginDelivery() {
		client.mutex.RUnlock()
		return nil
	}
	client.mutex.RUnlock()
	defer bridge.routeReferences.Done()
	// 首次失败已保存并取消 Prompt；排空迟到通知，避免每条 Token 重复报告同一错误。
	if bridge.currentFailure() != nil {
		return nil
	}
	bridge.observeFirstUpdate()

	clone := notification
	return bridge.enqueue(acpDelivery{notification: &clone})
}

// beginDelivery 在路由锁仍有效时取得一次回调引用；移除路由后不能再增加引用。
func (bridge *acpBridge) beginDelivery() bool {
	bridge.routeMutex.Lock()
	defer bridge.routeMutex.Unlock()
	if bridge.routeClosed {
		return false
	}
	bridge.routeReferences.Add(1)
	return true
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

// enqueue 合并安全输入快照，并以有界背压吸收短暂突发；持续堵塞才终止调用。
func (bridge *acpBridge) enqueue(delivery acpDelivery) error {
	bridge.enqueueMutex.Lock()
	defer bridge.enqueueMutex.Unlock()

	if bridge.queueClosed {
		return status_error.OperationCanceled
	}
	bytes, err := acpDeliveryBytes(delivery)
	if err != nil {
		return err
	}
	delivery.bytes = bytes
	if bytes > acpUpdateQueueBytes {
		bridge.setFailure(errACPUpdateQueueFull)
		return errACPUpdateQueueFull
	}
	timer := time.NewTimer(bridge.eventDeliveryTimeout)
	defer timer.Stop()
	for {
		bridge.queueMutex.Lock()
		if err := bridge.currentFailure(); err != nil {
			bridge.queueMutex.Unlock()
			return err
		}
		if bridge.lastQueued != nil {
			candidate := *bridge.lastQueued
			if bridge.mergeQueuedACPToolUpdate(&candidate, delivery) {
				mergedBytes, mergeErr := acpDeliveryBytes(candidate)
				if mergeErr != nil {
					bridge.queueMutex.Unlock()
					return mergeErr
				}
				if bridge.queuedBytes-bridge.lastQueued.bytes+mergedBytes <= acpUpdateQueueBytes {
					bridge.queuedBytes += mergedBytes - bridge.lastQueued.bytes
					candidate.bytes = mergedBytes
					*bridge.lastQueued = candidate
					bridge.queueMutex.Unlock()
					bridge.signalActivity()
					return nil
				}
			}
		}
		if bridge.queuedBytes+bytes <= acpUpdateQueueBytes {
			select {
			case bridge.updates <- &delivery:
				bridge.queuedBytes += bytes
				bridge.lastQueued = &delivery
				bridge.queueMutex.Unlock()
				bridge.signalActivity()
				return nil
			default:
			}
		}
		changed := bridge.queueChanged
		bridge.queueMutex.Unlock()
		// 所有等待都在快照锁之外，消费者总能取走事件并归还预算。
		select {
		case <-changed:
		case <-bridge.failureSignal:
			return bridge.currentFailure()
		case <-bridge.done:
			if cause := bridge.currentFailure(); cause != nil {
				return cause
			}
			return status_error.OperationCanceled
		case <-bridge.promptDone:
			return status_error.OperationCanceled
		case <-timer.C:
			bridge.setFailure(errACPUpdateQueueFull)
			return errACPUpdateQueueFull
		}
	}
}

// acpDeliveryBytes 计量等待队列持有的规范载荷，不保留额外序列化副本。
func acpDeliveryBytes(delivery acpDelivery) (int, error) {
	if delivery.notification != nil {
		encoded, err := json.Marshal(delivery.notification)
		if err != nil {
			return 0, status_error.RuntimeEventEncodingFailed
		}
		return len(encoded), nil
	}
	bytes := 0
	for _, event := range delivery.events {
		bytes += len(event.Type) + len(event.Data)
	}
	return bytes, nil
}

// mergeQueuedACPToolUpdate 合并同工具相邻的非终态替换快照；正文、其他工具和终态都是顺序屏障。
func (bridge *acpBridge) mergeQueuedACPToolUpdate(previous *acpDelivery, incoming acpDelivery) bool {
	if previous.notification == nil || incoming.notification == nil || previous.notification.SessionId != incoming.notification.SessionId {
		return false
	}
	old := previous.notification.Update.ToolCallUpdate
	next := incoming.notification.Update.ToolCallUpdate
	if old == nil || next == nil || old.ToolCallId != next.ToolCallId {
		return false
	}
	// 只合并已经确认的累计参数状态；工具正文、执行输出、Diff 与终态必须保持每条交付。
	if !bridge.isToolInputSnapshot(old) || !bridge.isToolInputSnapshot(next) {
		return false
	}
	if old.Status != nil && next.Status != nil && *old.Status != *next.Status {
		return false
	}
	// ACP 更新字段表示替换，省略表示保留；不拼接累积参数或内容，也不修改调用方持有的通知。
	merged := *old
	if next.Meta != nil {
		merged.Meta = next.Meta
	}
	if next.Content != nil {
		merged.Content = next.Content
	}
	if next.Kind != nil {
		merged.Kind = next.Kind
	}
	if next.Locations != nil {
		merged.Locations = next.Locations
	}
	if next.RawInput != nil {
		merged.RawInput = next.RawInput
	}
	if next.RawOutput != nil {
		merged.RawOutput = next.RawOutput
	}
	if next.Status != nil {
		merged.Status = next.Status
	}
	if next.Title != nil {
		merged.Title = next.Title
	}
	notification := *previous.notification
	notification.Update.ToolCallUpdate = &merged
	previous.notification = &notification
	return true
}

// isToolInputSnapshot 将累计参数状态与工具执行正文、进度和结果区分。
func (bridge *acpBridge) isToolInputSnapshot(update *acp.SessionToolCallUpdate) bool {
	if update == nil || update.RawInput == nil || update.RawOutput != nil ||
		update.Content != nil || len(update.Meta) > 0 ||
		(update.Status != nil && *update.Status != acp.ToolCallStatusPending) {
		return false
	}
	return true
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
	// 排空取消沿 Context 父子关系同步传播，closeDelivery 返回后不能再接纳下一条事件。
	runContext, cancelRun := context.WithCancel(bridge.deliveryContext)
	stopCancel := context.AfterFunc(ctx, cancelRun)
	defer stopCancel()
	defer cancelRun()
	ctx = runContext
	select {
	case <-bridge.activate:
	case <-ctx.Done():
		bridge.setFailure(ctx.Err())
		return
	}
	for {
		if ctx.Err() != nil {
			bridge.setFailure(ctx.Err())
			return
		}
		select {
		case delivery, ok := <-bridge.updates:
			if !ok {
				return
			}
			bridge.queueMutex.Lock()
			accepted := *delivery
			bridge.queuedBytes -= accepted.bytes
			close(bridge.queueChanged)
			bridge.queueChanged = make(chan struct{})
			if bridge.lastQueued == delivery {
				bridge.lastQueued = nil
			}
			bridge.queueMutex.Unlock()
			if err := bridge.deliver(ctx, accepted); err != nil {
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
		bridge.stateMutex.Lock()
		mapped, err := bridge.mapNotification(*delivery.notification)
		bridge.stateMutex.Unlock()
		if err != nil {
			return err
		}
		events = mapped
	}
	for _, event := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
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
	defer bridge.cancelDelivery()
	bridge.enqueueMutex.Lock()
	if !bridge.queueClosed {
		bridge.queueClosed = true
		close(bridge.updates)
	}
	bridge.enqueueMutex.Unlock()
	select {
	case <-bridge.done:
		return bridge.currentFailure()
	case <-ctx.Done():
		bridge.setFailure(runtimeContextStatus(ctx.Err()))
		return bridge.currentFailure()
	}
}

// sideEffectSnapshot 在消费者排空超时后仍能安全取得已经观察到的副作用事实。
func (bridge *acpBridge) sideEffectSnapshot() SideEffectSummary {
	bridge.stateMutex.RLock()
	defer bridge.stateMutex.RUnlock()
	return bridge.sideEffects
}

// setFailure 保存首次交付失败。
func (bridge *acpBridge) setFailure(err error) {
	if err == nil {
		return
	}
	bridge.failureMutex.Lock()
	first := bridge.failure == nil
	if bridge.failure == nil {
		bridge.failure = err
		if bridge.primaryCause == nil {
			bridge.primaryCause = err
		}
		close(bridge.failureSignal)
	}
	bridge.failureMutex.Unlock()
	if first {
		logx.Errorf("ACP event delivery stopped: runtime_id=%s invocation_id=%s session_id=%s error=%v", bridge.runtimeID, bridge.invocationID, bridge.sessionID, err)
		if bridge.cancelPrompt != nil {
			bridge.cancelPrompt()
		}
	}
}

// recordPrimaryCause 冻结已知终止原因，不停止消费者，也不丢弃已经接纳的队列内容。
func (bridge *acpBridge) recordPrimaryCause(err error, stage string) {
	if err == nil {
		return
	}
	bridge.failureMutex.Lock()
	defer bridge.failureMutex.Unlock()
	if bridge.primaryCause == nil {
		bridge.primaryCause, bridge.primaryStage = err, stage
	}
}

// primaryCauseSnapshot 返回首因及其阶段，后续排空或会话清理不能覆盖它。
func (bridge *acpBridge) primaryCauseSnapshot() (error, string) {
	bridge.failureMutex.RLock()
	defer bridge.failureMutex.RUnlock()
	return bridge.primaryCause, bridge.primaryStage
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
			eventType := EventTypeToolUpdate
			// 非终态更新可能缺失开始通知，保守记录工具尝试，但不据此推断文件已写入。
			bridge.sideEffects.ToolUse = true
			if bridge.isToolInputSnapshot(update.ToolCallUpdate) {
				eventType = EventTypeToolInputSnapshot
			}
			event, err := newEvent(eventType, payload)
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
		update.Content != nil || update.Locations != nil ||
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
	// 更新中的显式空集合用于清除旧内容；省略字段才表示沿用前值。
	if update, ok := value.(*acp.SessionToolCallUpdate); ok {
		if update.Content != nil && len(update.Content) == 0 {
			payload["content"] = []any{}
		}
		if update.Locations != nil && len(update.Locations) == 0 {
			payload["locations"] = []any{}
		}
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
