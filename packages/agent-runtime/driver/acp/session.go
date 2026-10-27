package acp

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// session 是单次 Prompt 的连接所有者，不复用无法区分迟到回合的 ACP 通知。
type session struct {
	owner          *Driver                   // owner 保留尚未确认停止的原生使用者。
	native         sdk.Agent                 // native 是唯一由本会话负责关闭的公开Agent。
	closeGate      chan struct{}             // closeGate 串行化可取消的原生Close尝试。
	nativeClosed   bool                      // nativeClosed 只在公开Close成功后置位，受closeGate保护。
	client         *sdk.ClientSideConnection // client 是唯一客户端协议连接。
	configuration  contract.Configuration    // configuration 保存准备时确认的模型、权限和推理配置。
	nativeID       sdk.SessionId             // nativeID 是原生会话返回的真实身份。
	serveCancel    context.CancelFunc        // serveCancel 通知 acpserver 结束并释放 Agent。
	closeTransport func() error              // closeTransport 中断本连接拥有的全部管道。
	stopOnce       sync.Once                 // stopOnce 保证清理启动一次。
	done           chan struct{}             // done 在ACP服务边界停止后关闭，不代表原生资源已释放。
	mu             sync.Mutex                // mu 保护调用槽与单次使用状态。
	turn           *turnState                // turn 是当前唯一事件与交互接收者。
	spent          bool                      // spent 阻止重复发送已经使用的原生会话。
	deliveryMu     sync.Mutex                // deliveryMu 串行交付并与本轮结束建立屏障。
}

// turnState 保存本轮交付、取消和故障，不保存累计正文。
type turnState struct {
	request            contract.TurnRequest  // request 提供有效调用与修订身份。
	sink               contract.DriverSink   // sink 是本轮同步有界交付端口。
	ctx                context.Context       // ctx 控制交付及原生 Prompt。
	cancel             context.CancelFunc    // cancel 在交付失败时停止本轮。
	err                error                 // err 保存首个交付或交互错误。
	interactionSeq     uint64                // interactionSeq 为同一工具的多次审批分配独立请求身份。
	activity           chan struct{}         // activity 合并原生更新与交互状态变化，供无活动监视器重置。
	pendingInput       int                   // pendingInput 统计等待中的交互，不将人工等待误判为原生静默。
	contextKnown       bool                  // contextKnown 表示已经收到公开上下文快照。
	contextTokens      int64                 // contextTokens 是最后一次公开ACP上下文快照，不累计回合用量。
	lastMessageID      string                // lastMessageID 是排空通知后最后出现的助手身份。
	lastPhase          contract.MessagePhase // lastPhase 保留该消息已知阶段，避免空边界抹掉过程分类。
	completionEligible bool                  // completionEligible 只在最后正文之后没有工具、思考或过程消息时成立。
}

var _ contract.DriverSession = (*session)(nil)
var _ sdk.Client = (*session)(nil)

// Prompt 只发送本轮文本，响应返回前依赖 SDK 水位排空通知；最终资源关闭由宿主调用 Close。
func (s *session) Prompt(ctx context.Context, r contract.TurnRequest, sink contract.DriverSink) contract.DriverResult {
	failed := func(f *contract.Fault) contract.DriverResult {
		return contract.DriverResult{Status: contract.StatusFailed, StopReason: contract.StopFailed, Fault: f, Configuration: s.configuration}
	}
	if sink == nil || strings.TrimSpace(r.Input.Text) == "" {
		return failed(fault(contract.FaultInvalidSpec, "prompt", "ACP prompt requires text and a sink", nil))
	}
	if len(r.Input.Attachments) > 0 {
		return failed(fault(contract.FaultUnsupported, "prompt", "ACP attachments are not supported", nil))
	}
	s.mu.Lock()
	if s.spent {
		s.mu.Unlock()
		return failed(fault(contract.FaultClosed, "prompt", "ACP session is single use", nil))
	}
	s.spent = true
	turnCtx, cancel := context.WithCancel(ctx)
	turn := &turnState{request: r, sink: sink, ctx: turnCtx, cancel: cancel, activity: make(chan struct{}, 1)}
	s.turn = turn
	s.mu.Unlock()
	defer cancel()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); s.watchInactivity(turn, r.Limits.InactivityTimeout) }()
	defer func() { cancel(); <-watchDone }()
	abort := context.AfterFunc(turnCtx, s.stop)
	defer abort()
	response, err := s.client.Prompt(turnCtx, sdk.PromptRequest{SessionId: s.nativeID, Prompt: []sdk.ContentBlock{sdk.TextBlock(r.Input.Text)}})
	// 在驱动自己的收尾取消前固定已有根因，避免把普通原生错误改成 cancelled。
	s.mu.Lock()
	cancellationCause := context.Cause(turnCtx)
	firstFault := turn.err
	s.mu.Unlock()
	// 成功响应已由 SDK 通知水位确认；错误返回仍需中断连接并解除活动交付。
	if err != nil {
		cancel()
		s.stop()
	}
	if err == nil && (response.StopReason == sdk.StopReasonEndTurn || response.StopReason == sdk.StopReasonMaxTokens || response.StopReason == sdk.StopReasonMaxTurnRequests) && s.owner.completionPolicy == CompletionLastAssistant {
		s.mu.Lock()
		id, eligible := turn.lastMessageID, turn.completionEligible && turn.lastPhase == contract.PhaseUnknown && turn.err == nil
		s.mu.Unlock()
		if eligible {
			_ = s.deliver(contract.DriverEvent{Kind: contract.EventAssistantMessage, Message: &contract.AssistantMessage{MessageID: id, Phase: contract.PhaseUnknown, Complete: true, CompletionVerified: true}})
		}
	}
	s.deliveryMu.Lock()
	s.mu.Lock()
	deliveryErr := turn.err
	contextTokens := turn.contextTokens
	contextKnown := turn.contextKnown
	s.turn = nil
	s.mu.Unlock()
	s.deliveryMu.Unlock()
	if err != nil {
		// 错误返回之后发生的交付取消属于收尾，不覆盖已经固定的原始失败。
		deliveryErr = firstFault
	}
	if deliveryErr != nil {
		var f *contract.Fault
		if !errors.As(deliveryErr, &f) {
			f = fault(contract.FaultDelivery, "delivery", "ACP event delivery failed", deliveryErr)
		}
		return failed(f)
	}
	if err != nil && cancellationCause != nil {
		return contract.DriverResult{Status: contract.StatusCancelled, StopReason: contract.StopCancelled, Fault: fault(contract.FaultCancelled, "prompt", "ACP prompt cancelled", cancellationCause), Configuration: s.configuration}
	}
	if err != nil {
		return failed(fault(contract.FaultDriver, "prompt", "ACP prompt failed", err))
	}
	result := contract.DriverResult{Status: contract.StatusCompleted, NativeHandle: string(s.nativeID), Usage: contract.Usage{ContextKnown: contextKnown, ContextTokens: contextTokens}, Configuration: s.configuration}
	switch response.StopReason {
	case sdk.StopReasonEndTurn:
		result.StopReason = contract.StopEndTurn
	case sdk.StopReasonCancelled:
		result.Status = contract.StatusCancelled
		result.StopReason = contract.StopCancelled
	case sdk.StopReasonRefusal:
		result.StopReason = contract.StopRefused
	case sdk.StopReasonMaxTokens, sdk.StopReasonMaxTurnRequests:
		result.StopReason = contract.StopMaxTokens
	default:
		return failed(fault(contract.FaultProtocol, "prompt", "ACP stop reason is unsupported", nil))
	}
	if usage := response.Usage; usage != nil {
		if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CachedReadTokens != nil && *usage.CachedReadTokens < 0 || usage.CachedWriteTokens != nil && *usage.CachedWriteTokens < 0 {
			return failed(fault(contract.FaultProtocol, "usage", "ACP prompt usage contains negative tokens", nil))
		}
		result.Usage.Known = true
		result.Usage.InputTokens = int64(usage.InputTokens)
		result.Usage.OutputTokens = int64(usage.OutputTokens)
		if usage.CachedReadTokens != nil {
			result.Usage.CachedReadTokens = int64(*usage.CachedReadTokens)
		}
		if usage.CachedWriteTokens != nil {
			result.Usage.CachedWriteTokens = int64(*usage.CachedWriteTokens)
		}
	}
	return result
}

func (s *session) stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.spent = true
		if s.turn != nil {
			s.turn.cancel()
		}
		s.mu.Unlock()
		if s.client != nil {
			s.client.Close()
		}
		if s.serveCancel != nil {
			s.serveCancel()
		}
	})
}

// Close 先结束ACP边界，再串行调用公开原生Close；失败保留所有者，成功不重复释放。
func (s *session) Close(ctx context.Context) error {
	s.stop()
	select {
	case <-s.done:
	case <-ctx.Done():
		return fault(contract.FaultCleanup, "close", "ACP boundary cleanup deadline exceeded", ctx.Err())
	}
	select {
	case s.closeGate <- struct{}{}:
	case <-ctx.Done():
		return fault(contract.FaultCleanup, "close", "ACP native cleanup wait cancelled", ctx.Err())
	}
	defer func() { <-s.closeGate }()
	if s.nativeClosed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fault(contract.FaultCleanup, "close", "ACP native cleanup cancelled", err)
	}
	if closer, ok := s.native.(interface{ Close(context.Context) error }); ok {
		if err := closer.Close(ctx); err != nil {
			return fault(contract.FaultCleanup, "close", "ACP native cleanup failed", err)
		}
	}
	s.nativeClosed = true
	s.owner.release(s)
	return nil
}

func (s *session) deliver(event contract.DriverEvent) error {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	s.mu.Lock()
	turn := s.turn
	s.mu.Unlock()
	if turn == nil {
		return nil
	}
	if err := turn.ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if message := event.Message; message != nil {
		if message.MessageID != turn.lastMessageID {
			turn.lastMessageID, turn.lastPhase, turn.completionEligible = message.MessageID, contract.PhaseUnknown, false
		}
		if message.Phase != "" && message.Phase != contract.PhaseUnknown {
			turn.lastPhase = message.Phase
		}
		if turn.lastPhase != contract.PhaseUnknown || message.MessageID == "" {
			turn.completionEligible = false
		} else if message.Delta != "" {
			turn.completionEligible = true
		}
	} else if event.Kind == contract.EventTool || event.Kind == contract.EventThinking {
		turn.completionEligible = false
	}
	s.mu.Unlock()
	err := turn.sink.Accept(turn.ctx, event)
	if err != nil {
		err = fault(contract.FaultDelivery, "delivery", "ACP event delivery failed", err)
		s.failTurn(turn, err)
	}
	return err
}
func (s *session) failTurn(turn *turnState, err error) {
	s.mu.Lock()
	if s.turn == turn && turn.err == nil {
		turn.err = err
	}
	s.mu.Unlock()
	turn.cancel()
}

// SessionUpdate 只转换公开 ACP 事实；缺失消息身份或阶段保持未知。
func (s *session) SessionUpdate(ctx context.Context, n sdk.SessionNotification) error {
	s.mu.Lock()
	turn := s.turn
	s.mu.Unlock()
	if turn == nil || n.SessionId != s.nativeID {
		return nil
	}
	s.markActivity(turn)
	if c := n.Update.AgentMessageChunk; c != nil {
		phase := contract.PhaseUnknown
		complete := false
		if info, ok := acpmeta.ReadMessageInfo(c.Meta); ok {
			complete = info.Complete
			switch info.Phase {
			case acpmeta.MessagePhaseCommentary:
				phase = contract.PhaseCommentary
			case acpmeta.MessagePhaseFinal:
				phase = contract.PhaseFinal
			}
		}
		id := ""
		if c.MessageId != nil {
			id = *c.MessageId
		}
		text := ""
		if c.Content.Text != nil {
			text = c.Content.Text.Text
		}
		return s.deliver(contract.DriverEvent{Kind: contract.EventAssistantMessage, Message: &contract.AssistantMessage{MessageID: id, Phase: phase, Delta: text, Complete: complete}})
	}
	if usage := n.Update.UsageUpdate; usage != nil {
		if usage.Used < 0 {
			err := fault(contract.FaultProtocol, "usage", "ACP context token count is negative", nil)
			// session/update 没有响应，SDK 只记录返回错误；必须同时使本轮失败。
			s.failTurn(turn, err)
			return err
		}
		s.mu.Lock()
		if s.turn != nil {
			s.turn.contextTokens = int64(usage.Used)
			s.turn.contextKnown = true
		}
		s.mu.Unlock()
		return s.deliver(contract.DriverEvent{Kind: contract.EventUsage, Usage: &contract.Usage{ContextKnown: true, ContextTokens: int64(usage.Used)}})
	}
	if c := n.Update.AgentThoughtChunk; c != nil {
		if c.Content.Text != nil {
			return s.deliver(contract.DriverEvent{Kind: contract.EventThinking, Text: c.Content.Text.Text})
		}
		return nil
	}
	if c := n.Update.ToolCall; c != nil {
		return s.deliver(contract.DriverEvent{Kind: contract.EventTool, Tool: &contract.ToolEvent{ID: string(c.ToolCallId), Name: c.Title, Status: string(c.Status)}})
	}
	if c := n.Update.ToolCallUpdate; c != nil {
		event := &contract.ToolEvent{ID: string(c.ToolCallId)}
		if c.Title != nil {
			event.Name = *c.Title
		}
		if c.Status != nil {
			event.Status = string(*c.Status)
		}
		return s.deliver(contract.DriverEvent{Kind: contract.EventTool, Tool: event})
	}
	return nil
}

// RequestPermission 经独立端口询问当前调用，拒绝错配和迟到回应。
func (s *session) RequestPermission(ctx context.Context, p sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	s.mu.Lock()
	turn := s.turn
	var sequence uint64
	if turn != nil {
		turn.interactionSeq++
		sequence = turn.interactionSeq
	}
	s.mu.Unlock()
	if turn == nil || p.SessionId != s.nativeID || ctx.Err() != nil || turn.ctx.Err() != nil {
		return permissionCancelled(), nil
	}
	s.setPendingInput(turn, 1)
	defer s.setPendingInput(turn, -1)
	interactionCtx, cancel := context.WithCancel(turn.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	id := "approval-" + strconv.FormatUint(sequence, 10)
	name := ""
	if p.ToolCall.Title != nil {
		name = *p.ToolCall.Title
	}
	request := contract.InteractionRequest{Invocation: turn.request.Invocation, ID: id, Kind: contract.InteractionApproval, ToolName: name, Prompt: name}
	response, err := turn.sink.Interact(interactionCtx, request)
	// 单个原生请求撤销只使该审批失效，不是整个健康回合的交付故障。
	if ctx.Err() != nil || turn.ctx.Err() != nil {
		return permissionCancelled(), nil
	}
	if err != nil {
		s.failTurn(turn, err)
		return permissionCancelled(), nil
	}
	if response.Invocation != request.Invocation || response.ID != id {
		s.failTurn(turn, fault(contract.FaultProtocol, "interaction", "ACP permission response identity mismatch", nil))
		return permissionCancelled(), nil
	}
	for _, option := range p.Options {
		if response.Allowed && option.Kind == sdk.PermissionOptionKindAllowOnce || !response.Allowed && option.Kind == sdk.PermissionOptionKindRejectOnce {
			return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Selected: &sdk.RequestPermissionOutcomeSelected{OptionId: option.OptionId}}}, nil
		}
	}
	return permissionCancelled(), nil
}
func permissionCancelled() sdk.RequestPermissionResponse {
	return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Cancelled: &sdk.RequestPermissionOutcomeCancelled{}}}
}

// configure 只把原生目录和每次写入回执中的当前值当作实际配置。
func (s *session) configure(ctx context.Context, options []sdk.SessionConfigOption, modes *sdk.SessionModeState, profile contract.Profile) error {
	selectValue := func(category sdk.SessionConfigOptionCategory, value string) error {
		if value == "" {
			return nil
		}
		selected := categoryOption(options, category)
		if selected == nil || !containsValue(selected.Options, value) {
			return fault(contract.FaultUnsupported, "configure", "ACP requested configuration is not advertised", nil)
		}
		response, err := s.client.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{ValueId: &sdk.SetSessionConfigOptionValueId{SessionId: s.nativeID, ConfigId: selected.Id, Value: sdk.SessionConfigValueId(value)}})
		if err != nil {
			return fault(contract.FaultPreparation, "configure", "ACP configuration selection failed", err)
		}
		confirmed := categoryOption(response.ConfigOptions, category)
		if confirmed == nil || confirmed.Id != selected.Id || string(confirmed.CurrentValue) != value {
			return fault(contract.FaultProtocol, "configure", "ACP configuration selection was not confirmed", nil)
		}
		options = response.ConfigOptions
		return nil
	}
	mode := profile.NativeMode
	if mode == "" && len(s.owner.modeCandidates) > 0 {
		for _, candidate := range s.owner.modeCandidates {
			if option := categoryOption(options, sdk.SessionConfigOptionCategoryMode); option != nil && containsValue(option.Options, candidate) {
				mode = candidate
				break
			}
			if modes != nil {
				for _, advertised := range modes.AvailableModes {
					if string(advertised.Id) == candidate {
						mode = candidate
						break
					}
				}
			}
			if mode != "" {
				break
			}
		}
		if fallback := s.owner.singleModeFallback; mode == "" && fallback != "" && modes != nil && len(modes.AvailableModes) == 1 && string(modes.AvailableModes[0].Id) == fallback {
			mode = fallback
		}
		if mode == "" {
			return fault(contract.FaultUnsupported, "configure", "ACP permission mode is not advertised", nil)
		}
	}
	modeViaConfig := false
	if mode != "" {
		if categoryOption(options, sdk.SessionConfigOptionCategoryMode) != nil {
			if err := selectValue(sdk.SessionConfigOptionCategoryMode, mode); err != nil {
				return err
			}
			modeViaConfig = true
		} else {
			advertised := false
			if modes != nil {
				for _, available := range modes.AvailableModes {
					advertised = advertised || string(available.Id) == mode
				}
			}
			if !advertised {
				return fault(contract.FaultUnsupported, "configure", "ACP permission mode is not advertised", nil)
			}
			if _, err := s.client.SetSessionMode(ctx, sdk.SetSessionModeRequest{SessionId: s.nativeID, ModeId: sdk.SessionModeId(mode)}); err != nil {
				return fault(contract.FaultPreparation, "configure", "ACP permission mode selection failed", err)
			}
		}
	}
	if err := selectValue(sdk.SessionConfigOptionCategoryModel, profile.Model); err != nil {
		return err
	}
	reasoning := profile.Reasoning
	fallback := ""
	if reasoning == "" && profile.Thinking != nil && !*profile.Thinking {
		reasoning, fallback = minimumReasoning(categoryOption(options, sdk.SessionConfigOptionCategoryThoughtLevel))
	}
	if err := selectValue(sdk.SessionConfigOptionCategoryThoughtLevel, reasoning); err != nil {
		return err
	}
	actual := func(category sdk.SessionConfigOptionCategory) string {
		if option := categoryOption(options, category); option != nil {
			return string(option.CurrentValue)
		}
		return ""
	}
	if profile.Model != "" && actual(sdk.SessionConfigOptionCategoryModel) != profile.Model {
		return fault(contract.FaultProtocol, "configure", "ACP later configuration reverted the requested model", nil)
	}
	if modeViaConfig && actual(sdk.SessionConfigOptionCategoryMode) != mode {
		return fault(contract.FaultProtocol, "configure", "ACP later configuration reverted the requested permission mode", nil)
	}
	if mode == "" {
		mode = actual(sdk.SessionConfigOptionCategoryMode)
	}
	s.configuration = contract.Configuration{
		ActualMode:        mode,
		ActualModel:       actual(sdk.SessionConfigOptionCategoryModel),
		ActualReasoning:   actual(sdk.SessionConfigOptionCategoryThoughtLevel),
		ReasoningFallback: fallback,
	}
	s.configuration.ThinkingDisabled = reasoningDisabled(s.configuration.ActualReasoning)
	return nil
}

func categoryOption(options []sdk.SessionConfigOption, category sdk.SessionConfigOptionCategory) *sdk.SessionConfigOptionSelect {
	// Claude 的 fast 选项也标为 model；标准模型身份优先，避免把模型写成开关。
	preferred := ""
	if category == sdk.SessionConfigOptionCategoryModel {
		preferred = "model"
	}
	if preferred != "" {
		for _, option := range options {
			if c := option.Select; c != nil && string(c.Id) == preferred {
				return c
			}
		}
	}
	for _, option := range options {
		if c := option.Select; c != nil && c.Category != nil && *c.Category == category {
			return c
		}
	}
	return nil
}

func reasoningDisabled(value string) bool {
	return strings.EqualFold(value, "none") || strings.EqualFold(value, "off") || strings.EqualFold(value, "disabled")
}

// minimumReasoning 使用当前模型公布的真实取值，不把低强度推理宣称为关闭。
func minimumReasoning(option *sdk.SessionConfigOptionSelect) (string, string) {
	if option == nil {
		return "", "reasoning_not_configurable"
	}
	var values []string
	if option.Options.Ungrouped != nil {
		for _, value := range *option.Options.Ungrouped {
			values = append(values, string(value.Value))
		}
	}
	if option.Options.Grouped != nil {
		for _, group := range *option.Options.Grouped {
			for _, value := range group.Options {
				values = append(values, string(value.Value))
			}
		}
	}
	for _, wanted := range []string{"none", "off", "disabled", "minimal", "min", "low", "medium", "high", "max", "xhigh"} {
		for _, value := range values {
			if strings.EqualFold(value, wanted) {
				if reasoningDisabled(value) {
					return value, ""
				}
				return value, "thinking_off_unsupported: selected " + value
			}
		}
	}
	if len(values) > 0 {
		return values[0], "thinking_off_unsupported: selected " + values[0]
	}
	return "", "thinking_off_unsupported: retained native default " + string(option.CurrentValue)
}

// markActivity 不持有交付锁，原生过程更新即使不对外投射也刷新无活动计时器。
func (s *session) markActivity(turn *turnState) {
	select {
	case turn.activity <- struct{}{}:
	default:
	}
}

func (s *session) setPendingInput(turn *turnState, delta int) {
	s.mu.Lock()
	turn.pendingInput += delta
	s.mu.Unlock()
	s.markActivity(turn)
}

// watchInactivity 将原生静默、调用取消和人工交互等待分别处理。
func (s *session) watchInactivity(turn *turnState, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-turn.ctx.Done():
			return
		case <-turn.activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			s.mu.Lock()
			pending := turn.pendingInput > 0
			s.mu.Unlock()
			if !pending {
				timer.Reset(timeout)
			}
		case <-timer.C:
			s.mu.Lock()
			pending := turn.pendingInput > 0
			s.mu.Unlock()
			if pending {
				continue
			}
			s.failTurn(turn, fault(contract.FaultInactivityTimeout, "inactivity_watchdog", "ACP prompt exceeded inactivity timeout", nil))
			return
		}
	}
}

func containsValue(options sdk.SessionConfigSelectOptions, value string) bool {
	if options.Ungrouped != nil {
		for _, option := range *options.Ungrouped {
			if string(option.Value) == value {
				return true
			}
		}
	}
	if options.Grouped != nil {
		for _, group := range *options.Grouped {
			for _, option := range group.Options {
				if string(option.Value) == value {
					return true
				}
			}
		}
	}
	return false
}

// ReadTextFile 拒绝未协商的客户端文件能力。
func (s *session) ReadTextFile(context.Context, sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	return sdk.ReadTextFileResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodFsReadTextFile)
}

// WriteTextFile 拒绝未协商的客户端文件能力。
func (s *session) WriteTextFile(context.Context, sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	return sdk.WriteTextFileResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodFsWriteTextFile)
}

// CreateTerminal 拒绝未协商的客户端终端能力。
func (s *session) CreateTerminal(context.Context, sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	return sdk.CreateTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalCreate)
}

// KillTerminal 拒绝未协商的客户端终端能力。
func (s *session) KillTerminal(context.Context, sdk.KillTerminalRequest) (sdk.KillTerminalResponse, error) {
	return sdk.KillTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalKill)
}

// TerminalOutput 拒绝未协商的客户端终端能力。
func (s *session) TerminalOutput(context.Context, sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	return sdk.TerminalOutputResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalOutput)
}

// ReleaseTerminal 拒绝未协商的客户端终端能力。
func (s *session) ReleaseTerminal(context.Context, sdk.ReleaseTerminalRequest) (sdk.ReleaseTerminalResponse, error) {
	return sdk.ReleaseTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalRelease)
}

// WaitForTerminalExit 拒绝未协商的客户端终端能力。
func (s *session) WaitForTerminalExit(context.Context, sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	return sdk.WaitForTerminalExitResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalWaitForExit)
}
