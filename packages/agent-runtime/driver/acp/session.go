package acp

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// session 是单次 Prompt 的连接所有者，不复用无法区分迟到回合的 ACP 通知。
type session struct {
	owner          *Driver                   // owner 保留尚未确认停止的原生使用者。
	native         sdk.Agent                 // native 是唯一由本会话负责关闭的公开Agent。
	closeGate      chan struct{}             // closeGate 串行化可取消的原生Close尝试。
	nativeClosed   bool                      // nativeClosed 只在公开Close成功后置位，受closeGate保护。
	client         *sdk.ClientSideConnection // client 是唯一客户端协议连接。
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
	request        contract.TurnRequest // request 提供有效调用与修订身份。
	sink           contract.DriverSink  // sink 是本轮同步有界交付端口。
	ctx            context.Context      // ctx 控制交付及原生 Prompt。
	cancel         context.CancelFunc   // cancel 在交付失败时停止本轮。
	err            error                // err 保存首个交付或交互错误。
	interactionSeq uint64               // interactionSeq 为同一工具的多次审批分配独立请求身份。
	contextTokens  int64                // contextTokens 是最后一次公开ACP上下文快照，不累计回合用量。
}

var _ contract.DriverSession = (*session)(nil)
var _ sdk.Client = (*session)(nil)

// Prompt 只发送本轮文本，响应返回前依赖 SDK 水位排空通知；最终资源关闭由宿主调用 Close。
func (s *session) Prompt(ctx context.Context, r contract.TurnRequest, sink contract.DriverSink) contract.DriverResult {
	failed := func(f *contract.Fault) contract.DriverResult {
		return contract.DriverResult{Status: contract.StatusFailed, StopReason: contract.StopFailed, Fault: f}
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
	turn := &turnState{request: r, sink: sink, ctx: turnCtx, cancel: cancel}
	s.turn = turn
	s.mu.Unlock()
	defer cancel()
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
	s.deliveryMu.Lock()
	s.mu.Lock()
	deliveryErr := turn.err
	contextTokens := turn.contextTokens
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
		return contract.DriverResult{Status: contract.StatusCancelled, StopReason: contract.StopCancelled, Fault: fault(contract.FaultCancelled, "prompt", "ACP prompt cancelled", cancellationCause)}
	}
	if err != nil {
		return failed(fault(contract.FaultDriver, "prompt", "ACP prompt failed", err))
	}
	result := contract.DriverResult{Status: contract.StatusCompleted, NativeHandle: string(s.nativeID), Usage: contract.Usage{ContextTokens: contextTokens}}
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
	if response.Usage != nil {
		result.Usage.InputTokens = int64(response.Usage.InputTokens)
		result.Usage.OutputTokens = int64(response.Usage.OutputTokens)
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
	if c := n.Update.AgentMessageChunk; c != nil {
		phase := contract.PhaseUnknown
		if meta, ok := c.Meta["codex"].(map[string]any); ok {
			switch meta["phase"] {
			case "commentary":
				phase = contract.PhaseCommentary
			case "final_answer":
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
		return s.deliver(contract.DriverEvent{Kind: contract.EventAssistantMessage, Message: &contract.AssistantMessage{MessageID: id, Phase: phase, Delta: text}})
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
		}
		s.mu.Unlock()
		return s.deliver(contract.DriverEvent{Kind: contract.EventUsage, Usage: &contract.Usage{ContextTokens: int64(usage.Used)}})
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

func (s *session) configure(ctx context.Context, options []sdk.SessionConfigOption, profile contract.Profile) error {
	for _, selection := range []struct {
		category sdk.SessionConfigOptionCategory // category 标识公开配置类别。
		value    string                          // value 是本次明确请求值。
	}{{sdk.SessionConfigOptionCategoryModel, profile.Model}, {sdk.SessionConfigOptionCategoryThoughtLevel, profile.Reasoning}} {
		if selection.value == "" {
			continue
		}
		var selected *sdk.SessionConfigOptionSelect
		for _, option := range options {
			if c := option.Select; c != nil && c.Category != nil && *c.Category == selection.category {
				selected = c
				break
			}
		}
		if selected == nil || !containsValue(selected.Options, selection.value) {
			return fault(contract.FaultUnsupported, "configure", "ACP requested model or reasoning is not advertised", nil)
		}
		response, err := s.client.SetSessionConfigOption(ctx, sdk.SetSessionConfigOptionRequest{ValueId: &sdk.SetSessionConfigOptionValueId{SessionId: s.nativeID, ConfigId: selected.Id, Value: sdk.SessionConfigValueId(selection.value)}})
		if err != nil {
			return fault(contract.FaultPreparation, "configure", "ACP configuration selection failed", err)
		}
		confirmed := false
		for _, option := range response.ConfigOptions {
			if option.Select != nil && option.Select.Id == selected.Id && string(option.Select.CurrentValue) == selection.value {
				confirmed = true
			}
		}
		if !confirmed {
			return fault(contract.FaultProtocol, "configure", "ACP configuration selection was not confirmed", nil)
		}
		options = response.ConfigOptions
	}
	return nil
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
