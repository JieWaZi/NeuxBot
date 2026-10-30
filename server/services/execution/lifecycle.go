package execution

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// activeExecution 保存进程内对话执行的取消入口和退出通知。
type activeExecution struct {
	// executionID 是当前占用对话的执行标识。
	executionID string
	// cancel 结束 Runtime 运行使用的 Context。
	cancel context.CancelFunc
	// done 在执行完成日志和查询索引处理结束后关闭。
	done chan struct{}
}

// executionRegistry 统一维护 Chat 执行的进程生命周期和单执行约束。
//
// 注册表只管理进程内占用关系，不替代 JSONL 中的权威执行状态。调用方必须先取得
// reservation，再提交消息和执行事实；提交失败时释放 reservation，提交成功后由
// launch 保证后台流程退出时释放。
type executionRegistry struct {
	// mutex 保护生命周期状态和活跃执行集合。
	mutex sync.Mutex
	// active 按对话标识保存当前唯一活跃执行。
	active map[string]activeExecution
	// lifecycleContext 控制全部后台执行。
	lifecycleContext context.Context
	// lifecycleCancel 取消全部后台执行。
	lifecycleCancel context.CancelFunc
	// waitGroup 等待已经启动的后台执行退出。
	waitGroup sync.WaitGroup
	// started 表示注册表已经绑定进程根 Context。
	started bool
}

// newExecutionRegistry 创建尚未绑定进程生命周期的执行注册表。
func newExecutionRegistry() *executionRegistry {
	return &executionRegistry{active: make(map[string]activeExecution)}
}

// start 绑定进程根 Context；重复调用保持幂等。
func (r *executionRegistry) start(ctx context.Context) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.started {
		return
	}
	r.lifecycleContext, r.lifecycleCancel = context.WithCancel(ctx)
	r.started = true
}

// close 取消全部执行并等待后台流程完成。
func (r *executionRegistry) close() {
	r.mutex.Lock()
	if !r.started {
		r.mutex.Unlock()
		return
	}
	cancel := r.lifecycleCancel
	r.started = false
	r.mutex.Unlock()

	cancel()
	r.waitGroup.Wait()
}

// reserve 在写入任何权威事实前占用 Chat 的唯一执行位置。
func (r *executionRegistry) reserve(
	chatID string,
	executionID string,
) (context.Context, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !r.started {
		return nil, status_error.ChatServiceNotStarted
	}
	if _, exists := r.active[chatID]; exists {
		return nil, status_error.ChatHasActiveExecution
	}
	executionContext, cancel := context.WithCancel(r.lifecycleContext)
	// 从预留成功开始计入生命周期，而不是等到 goroutine 启动后再 Add。这样 Close
	// 与发送准备或权威提交并发时，不会在 Wait 已返回后才出现新的后台执行。
	r.waitGroup.Add(1)
	r.active[chatID] = activeExecution{
		executionID: executionID,
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	return executionContext, nil
}

// launch 启动已经提交权威事实的执行，并保证退出时释放占用。
func (r *executionRegistry) launch(
	chatID string,
	executionID string,
	executionContext context.Context,
	run func(context.Context),
) {
	go func() {
		defer r.release(chatID, executionID)
		run(executionContext)
	}()
}

// release 仅释放与 executionID 匹配的占用，避免旧流程误删新执行。
func (r *executionRegistry) release(chatID string, executionID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	active, exists := r.active[chatID]
	if !exists || active.executionID != executionID {
		return
	}
	delete(r.active, chatID)
	close(active.done)
	r.waitGroup.Done()
}

// complete 在同一临界区内释放执行占用并发布终态，避免订阅者先于下一轮入口观察到完成。
func (r *executionRegistry) complete(
	chatID string,
	executionID string,
	publish func(),
) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	active, exists := r.active[chatID]
	matched := exists && active.executionID == executionID
	if matched {
		delete(r.active, chatID)
	}
	if publish != nil {
		publish()
	}
	if matched {
		close(active.done)
		r.waitGroup.Done()
	}
}

// cancel 取消 Chat 当前活跃执行；没有活跃执行时保持幂等。
func (r *executionRegistry) cancel(chatID string) {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	r.mutex.Unlock()
	if exists && active.cancel != nil {
		active.cancel()
	}
}

// cancelAndWait 取消活跃执行并等待其完成持久化收尾。
func (r *executionRegistry) cancelAndWait(ctx context.Context, chatID string) error {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	r.mutex.Unlock()
	if !exists {
		return nil
	}
	active.cancel()
	select {
	case <-active.done:
		return nil
	case <-ctx.Done():
		return status_error.ChatHasActiveExecution
	}
}

// cancelExecutionAndWait 捕获精确执行的取消句柄，锁释放后新执行也不会成为取消目标。
func (r *executionRegistry) cancelExecutionAndWait(ctx context.Context, chatID, executionID string) error {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	matched := exists && active.executionID == executionID
	r.mutex.Unlock()
	if !matched {
		return nil
	}
	active.cancel()
	select {
	case <-active.done:
		return nil
	case <-ctx.Done():
		return status_error.ChatHasActiveExecution
	}
}

// messageExecution 保存 Chat 已解析的一次 Runtime 调用及其业务身份。
type messageExecution struct {
	// pluginUsageRelease 在 Runtime 返回后、Chat 重新接纳前清除本轮引用，不释放会话租约。
	pluginUsageRelease func()
	// messageFacts 交付同一执行已提交的交互与命令事实，保证实时与重放一致。
	messageFacts *executionMessageFacts
	// agentActivate 在执行接纳后激活短期 CLI 身份。
	agentActivate func()
	// agentSettle 记录该次调用返回的可靠原生会话身份。
	agentSettle func(runtimecontract.Result)
	// capabilityRelease 在回合结束后撤销内置工具能力。
	capabilityRelease func()
	// preparedRun 保存已准备但尚未提交本轮输入的原生会话。
	preparedRun runtimecontract.PreparedChatRun
	// plugins 是消息明确选择的插件展示快照。
	plugins []plugins.Snapshot
	// turn 是本轮提交给已准备消费者的增量输入与交互端口。
	turn runtimecontract.TurnInput
	// state 是首事件提交前仍未进入权威日志的对话状态。
	state messageChat
	// input 是首事件提交时持久化的本次用户输入。
	input pendingMessage
	// execution 是首事件提交时建立的执行状态。
	execution models.Execution
	// files 保存提交成功前可以回滚的附件和工作区副作用。
	files *preparedMessageFiles
	// skills 是当前消息需要保存的技能引用。
	skills []eventlog.MessageSkillReference
	// connectors 是当前消息需要保存的 MCP 连接器引用。
	connectors []eventlog.MessageConnectorReference
	// started 向发送请求报告 Runtime 是否已经接受首条输入。
	started chan<- executionStartResult
	// precommitted 表示输入和 pending Execution 已在 Runtime 派发前提交。
	precommitted bool
}

// executionStartResult 保存发送接口等待的唯一启动确认结果。
type executionStartResult struct {
	// chat 是启动成功后已进入权威日志的对话索引。
	chat models.Chat
	// execution 是启动成功后已进入 running 状态的执行索引。
	execution models.Execution
	// err 是 Runtime 接受输入或权威提交前发生的失败。
	err error
}

// runExecution 运行已经提交的 Runtime 调用并持久化唯一终态。
func (m *service) runExecution(ctx context.Context, plan messageExecution) {
	if plan.precommitted && plan.agentActivate != nil {
		plan.agentActivate()
	}
	if plan.capabilityRelease != nil {
		defer plan.capabilityRelease()
	}
	chat := plan.state.chat
	execution := plan.execution

	runtimeSink := &runtimeEventSink{
		messageFacts: plan.messageFacts,
		journal:      m.journal,
		chatID:       chat.ID,
		executionID:  execution.ID,
	}
	sink := &chatRuntimeSink{
		runtime: runtimeSink,
		start: func(invocationID string) error {
			committedChat, committedExecution, err := m.commitStartedExecution(
				plan,
				invocationID,
			)
			if err != nil {
				return err
			}
			if plan.agentActivate != nil {
				plan.agentActivate()
			}
			chat = committedChat
			execution = committedExecution
			if !plan.precommitted {
				plan.files.commit()
				plan.started <- executionStartResult{
					chat:      chat,
					execution: execution,
				}
			}
			return nil
		},
	}
	plan.messageFacts.start(runtimeSink.scheduleMessageFacts)
	defer plan.messageFacts.stopDelivery()
	defer func() {
		if plan.preparedRun != nil {
			if err := plan.preparedRun.Close(context.WithoutCancel(ctx)); err != nil {
				logx.Errorf("close unused chat preparation failed: execution_id=%s error=%v", execution.ID, err)
			}
		}
	}()
	result := plan.preparedRun.Run(ctx, plan.turn, sink)
	// 本轮引用必须早于 Chat 占位释放与终态发布清空，避免旧轮收尾覆盖新轮引用。
	if plan.pluginUsageRelease != nil {
		plan.pluginUsageRelease()
	}
	if plan.capabilityRelease != nil {
		plan.capabilityRelease()
	}
	m.elicitations.finishQuestions(chat.ID, execution.ID)
	// worker 退出后再 Flush，禁止交互事实的迟到唤醒越过执行终态。
	plan.messageFacts.stopDelivery()
	if plan.agentSettle != nil {
		plan.agentSettle(result)
	}
	if !sink.accepted {
		if !plan.precommitted {
			plan.started <- executionStartResult{err: runtimeStartError(result)}
			return
		}
		if result.Failure == nil {
			result.Failure = &runtimecontract.Failure{
				Code:       runtimediagnostic.FailureCodeRuntimeUnavailable,
				Stage:      constants.RuntimeExecutionFailureStageStart,
				Message:    "runtime did not accept the invocation",
				Transience: runtimediagnostic.FailureTransiencePermanent,
			}
		}
		status, errorCode, errorMessage := executionResultStatus(result)
		m.finishExecution(
			chat,
			execution,
			status,
			errorCode,
			errorMessage,
			result.ExitCode,
			result.Signal,
			nil,
			0,
			false,
		)
		return
	}
	attempt := 1
	recoveryStrategy := constants.InvocationRecoveryInitial
	triggerReason := constants.InvocationTriggerUnspecified
	// Runtime 返回后强制刷新尚未形成完整消息的内存增量，再记录调用事实和执行终态。
	if err := runtimeSink.Flush(); err != nil {
		result.Failure = chatDeliveryFailure(constants.ChatDeliveryFailureStageFlush, err)
	}
	if err := m.recordInvocationResult(
		chat,
		plan.files.executionWorkspaceDirectory,
		execution,
		result,
		attempt,
		recoveryStrategy,
		triggerReason,
	); err != nil {
		result.Failure = chatDeliveryFailure(constants.ChatDeliveryFailureStageLog, err)
	}
	if sessionID := reliableRuntimeSessionID(result); sessionID != "" {
		if err := m.saveRuntimeSession(chat, execution, sessionID); err != nil {
			result.Failure = chatDeliveryFailure(constants.ChatDeliveryFailureStageSession, err)
		}
	}
	status, errorCode, errorMessage := executionResultStatus(result)
	m.finishExecution(
		chat,
		execution,
		status,
		errorCode,
		errorMessage,
		result.ExitCode,
		result.Signal,
		runtimeSink.Usage(),
		runtimeSink.MessageRevision(),
		runtimeFailureRetryable(result) && plan.state.threadWorkspaceID == "" && plan.input.message.Actor == "user",
	)
}

// chatRuntimeSink 将 Runtime 增量直接交给 Chat 事件管道。
type chatRuntimeSink struct {
	// runtime 聚合高频 Runtime 增量并维持实时与持久化投影一致。
	runtime *runtimeEventSink
	// invocationID 是启动确认事件中由 Runtime 建立的调用标识。
	invocationID string
	// start 在首事件到达时原子提交消息、执行和 running 事实。
	start func(invocationID string) error
	// accepted 表示首事件和权威提交均已成功完成。
	accepted bool
}

// Accept 消费 Runtime 首事件并将后续可见事件直接交给既有聚合器。
func (s *chatRuntimeSink) Accept(
	ctx context.Context,
	event runtimecontract.Event,
) error {
	if event.Type == runtimecontract.EventTypeInvocationStarted {
		var started runtimecontract.InvocationStartedData
		if err := json.Unmarshal(event.Data, &started); err != nil {
			return status_error.RuntimeUnavailable
		}
		if started.InvocationID == "" {
			return status_error.BadRequest
		}
		s.invocationID = started.InvocationID
		s.runtime.messageFacts.setInvocation(started.InvocationID)
		if s.accepted {
			return nil
		}
		if s.start != nil {
			if err := s.start(started.InvocationID); err != nil {
				return err
			}
		}
		s.accepted = true
		return nil
	}
	if s.invocationID == "" {
		return status_error.RuntimeUnavailable
	}
	return s.runtime.acceptInvocation(ctx, s.invocationID, event)
}

func (m *service) commitStartedExecution(
	plan messageExecution, invocationID string,
) (models.Chat, models.Execution, error) {
	if plan.precommitted {
		execution, err := m.journal.MarkExecutionStarted(
			context.Background(),
			plan.execution,
			invocationID,
		)
		if err != nil {
			return models.Chat{}, models.Execution{}, err
		}
		return plan.state.chat, execution, nil
	}
	inputs := startedMessageEvents(
		plan.state,
		plan.input,
		plan.execution,
		invocationID,
		plan.files.attachmentReferences,
		plan.files.codeReferences,
		plan.skills,
		plan.connectors,
		plan.plugins,
	)
	inputs = append(inputs, eventlog.AppendInput{
		Type:         constants.ChatEventTypeExecutionStarted,
		ExecutionID:  plan.execution.ID,
		InvocationID: invocationID,
	})
	accepted, err := m.journal.CommitStartedTurn(context.Background(), chatservice.StartedTurn{
		NewChat:   plan.state.isNew,
		Chat:      plan.state.chat,
		Execution: plan.execution,
		Facts:     inputs,
	})
	if err != nil {
		return models.Chat{}, models.Execution{}, err
	}
	return accepted.Chat, accepted.Execution, nil
}

// commitAcceptedExecution 在可靠入口派发 Runtime 前提交输入和 pending Execution。
func (m *service) commitAcceptedExecution(
	plan messageExecution,
) (chatservice.AcceptedTurn, error) {
	inputs := startedMessageEvents(
		plan.state,
		plan.input,
		plan.execution,
		"",
		plan.files.attachmentReferences,
		plan.files.codeReferences,
		plan.skills,
		plan.connectors,
		plan.plugins,
	)
	return m.journal.CommitAcceptedTurn(context.Background(), chatservice.StartedTurn{
		NewChat:   plan.state.isNew,
		Chat:      plan.state.chat,
		Execution: plan.execution,
		Facts:     inputs,
	})
}

func runtimeStartError(result runtimecontract.Result) error {
	if result.Failure == nil {
		return status_error.RuntimeUnavailable
	}
	if result.Failure.Stage == constants.RuntimeRunFailureStageRuntimeFiles {
		return status_error.SkillUnavailable
	}
	switch result.Failure.Code {
	case runtimediagnostic.FailureCodeSessionNotFound:
		return status_error.RuntimeSessionNotFound
	case runtimediagnostic.FailureCodePolicyUnsupported:
		return status_error.RuntimePolicyUnsupported
	case runtimediagnostic.FailureCodeProtocolIncompatible:
		return status_error.RuntimeProtocolIncompatible
	case runtimediagnostic.FailureCodeInactivityTimeout:
		return status_error.RuntimeInactivityTimeout
	case runtimediagnostic.FailureCodeAuthRequired:
		return status_error.RuntimeAuthRequired
	case runtimediagnostic.FailureCodeRuntimeUnavailable:
		return status_error.RuntimeUnavailable
	default:
		detail := runtimeFailureDisplayMessage(result.Failure)
		if detail == "" {
			return status_error.RuntimeUnavailable
		}
		return status_error.RuntimeExecutionFailed.WithParams(map[string]interface{}{
			"Detail": detail,
		})
	}
}

// finishExecution 先提交执行终态，再以尽力而为方式追平 SQLite 查询索引。
func (m *service) finishExecution(
	chat models.Chat,
	execution models.Execution,
	status constants.ExecutionStatus,
	errorCode string,
	errorMessage string,
	exitCode int,
	signal string,
	usage json.RawMessage,
	messageRevision int64,
	retryable bool,
) {
	eventType := constants.ChatEventTypeExecutionCompleted
	if status == constants.ExecutionStatusFailed {
		eventType = constants.ChatEventTypeExecutionFailed
		logx.Errorf(
			"chat execution failed: chat_id=%s execution_id=%s error_code=%s error_message=%s exit_code=%d",
			chat.ID,
			execution.ID,
			errorCode,
			errorMessage,
			exitCode,
		)
	} else if status == constants.ExecutionStatusCanceled {
		eventType = constants.ChatEventTypeExecutionCanceled
	}
	terminalData := marshalData(eventlog.ExecutionFinishedData{
		Retryable:       retryable,
		Status:          status,
		ErrorCode:       errorCode,
		ErrorMessage:    errorMessage,
		ExitCode:        exitCode,
		Signal:          signal,
		Usage:           append(json.RawMessage(nil), usage...),
		MessageRevision: messageRevision,
	})
	commit, err := m.journal.CommitTerminal(context.Background(), chatservice.TerminalFact{
		Execution: execution,
		Type:      eventType,
		Data:      terminalData,
		Status:    status,
		ErrorCode: errorCode,
	})
	if err != nil {
		return
	}
	m.executions.complete(chat.ID, execution.ID, func() {
		m.journal.PublishTerminal(commit)
	})
}

// recordInvocationResult 将 Runtime 归一化的调用事实提交到权威日志。
func (m *service) recordInvocationResult(
	chat models.Chat,
	workingDirectory string,
	execution models.Execution,
	result runtimecontract.Result,
	attempt int,
	recoveryStrategy constants.InvocationRecoveryStrategy,
	triggerReason constants.InvocationTriggerReason,
) error {
	capabilities := make([]constants.RuntimeCapability, 0, len(result.Facts.Capabilities))
	for capability, enabled := range result.Facts.Capabilities {
		if enabled {
			capabilities = append(capabilities, capability)
		}
	}
	sort.Slice(capabilities, func(left int, right int) bool {
		return capabilities[left] < capabilities[right]
	})
	terminationReason := result.TerminationReason
	if terminationReason == constants.RuntimeTerminationReasonUnspecified {
		terminationReason = constants.RuntimeTerminationReasonUnknown
	}
	data := eventlog.InvocationFinishedData{
		StopReason:                    result.StopReason,
		TerminationReason:             terminationReason,
		PrefixArgs:                    result.Facts.PrefixArgs,
		ConfigHome:                    result.Facts.ConfigHome,
		EnvironmentFingerprint:        result.Facts.EnvironmentFingerprint,
		AdditionalWritableDirectories: chat.AdditionalWorkingDirectories,
		WorkingDirectory:              workingDirectory,
		Attempt:                       attempt,
		RecoveryStrategy:              recoveryStrategy,
		TriggerReason:                 triggerReason,
		RuntimeID:                     result.Facts.RuntimeID,
		BaseRuntimeID:                 result.Facts.BaseRuntimeID,
		Binary:                        result.Facts.Binary,
		Version:                       result.Facts.Version,
		Model:                         result.Facts.Model,
		Reasoning:                     result.Facts.Reasoning,
		PermissionLevel:               result.Facts.Policy.PermissionLevel,
		WorkspaceReadOnly:             result.Facts.Policy.WorkspaceReadOnly,
		Capabilities:                  capabilities,
		SessionMode:                   result.Session.Mode,
		SessionInputID:                result.Session.InputID,
		SessionOutputID:               result.Session.OutputID,
		ForkMode:                      result.Session.ForkMode,
		ForkPosition:                  result.Session.ForkPosition,
		SessionState:                  result.Session.State,
		ExitCode:                      result.ExitCode,
		Signal:                        result.Signal,
		ProtocolHealthy:               result.ProtocolHealthy,
		VisibleOutput:                 result.SideEffects.VisibleOutput,
		ReasoningOutput:               result.SideEffects.ReasoningOutput,
		ToolUse:                       result.SideEffects.ToolUse,
		FileWrite:                     result.SideEffects.FileWrite,
		Artifact:                      result.SideEffects.Artifact,
		UndeliveredEvents:             result.UndeliveredEvents,
		UndeliveredBytes:              result.UndeliveredBytes,
	}
	if result.Failure != nil {
		data.FailureCode = result.Failure.Code
		data.FailureStage = result.Failure.Stage
		data.FailureMessage = result.Failure.Message
		data.FailureTransience = result.Failure.Transience
	}
	_, err := m.journal.AppendExecutionFacts(context.Background(), chat.ID, []eventlog.AppendInput{{
		Type:         constants.ChatEventTypeInvocationFinished,
		ExecutionID:  execution.ID,
		InvocationID: result.Facts.InvocationID,
		Data:         marshalData(data),
	}})
	if err != nil {
		return err
	}
	return nil
}

func executionResultStatus(
	result runtimecontract.Result,
) (constants.ExecutionStatus, string, string) {
	if result.Failure == nil {
		return constants.ExecutionStatusCompleted, "", ""
	}
	status := constants.ExecutionStatusFailed
	if result.Failure.Code == runtimediagnostic.FailureCodeContextCanceled {
		status = constants.ExecutionStatusCanceled
	}
	return status, string(result.Failure.Code), runtimeFailureDisplayMessage(result.Failure)
}

// runtimeFailureDisplayMessage 统一执行前拒绝与执行终态的展示原因，原始诊断仍保存在 Failure.Message。
func runtimeFailureDisplayMessage(failure *runtimecontract.Failure) string {
	if message := strings.TrimSpace(failure.DisplayMessage); message != "" {
		return message
	}
	return strings.TrimSpace(failure.Message)
}

func reliableRuntimeSessionID(result runtimecontract.Result) string {
	if result.Session.OutputID == "" {
		return ""
	}
	if result.Session.State != runtimecontract.SessionStateCreated &&
		result.Session.State != runtimecontract.SessionStateResumed {
		return ""
	}
	return result.Session.OutputID
}

func chatDeliveryFailure(
	stage constants.ChatDeliveryFailureStage,
	err error,
) *runtimecontract.Failure {
	return &runtimecontract.Failure{
		Code:       runtimediagnostic.FailureCodeEventDelivery,
		Stage:      stage.String(),
		Message:    err.Error(),
		Transience: runtimediagnostic.FailureTransiencePermanent,
	}
}

// clearRuntimeSession 提交 Session 失效事实，并删除可重建的续接索引。
func (m *service) clearRuntimeSession(
	chat models.Chat,
	execution models.Execution,
	reason constants.InvocationTriggerReason,
) error {
	_, err := m.journal.AppendExecutionFacts(context.Background(), chat.ID, []eventlog.AppendInput{{
		Type:        constants.ChatEventTypeNativeSessionCleared,
		ExecutionID: execution.ID,
		Data: marshalData(eventlog.RuntimeSessionData{
			RuntimeID:       execution.RuntimeID,
			LastExecutionID: execution.ID,
			Reason:          reason,
		}),
	}})
	if err != nil {
		return err
	}
	return nil
}

// saveRuntimeSession 提交可靠 Session 句柄，并更新可重建的续接索引。
func (m *service) saveRuntimeSession(
	chat models.Chat,
	execution models.Execution,
	sessionID string,
) error {
	_, err := m.journal.AppendExecutionFacts(context.Background(), chat.ID, []eventlog.AppendInput{{
		Type:        constants.ChatEventTypeNativeSessionUpdated,
		ExecutionID: execution.ID,
		Data: marshalData(eventlog.RuntimeSessionData{
			RuntimeID:       execution.RuntimeID,
			SessionID:       sessionID,
			LastExecutionID: execution.ID,
		}),
	}})
	if err != nil {
		return err
	}
	return nil
}
