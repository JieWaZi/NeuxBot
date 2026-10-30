package runtime

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/plan"
	"log/slog"
	"slices"
	"sync"
	"time"

	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/output"
)

type eventSink struct {
	mu                 sync.Mutex                    // mu 串行处理事件与交互，避免重复交付。
	call               *invocation                   // call 是唯一所属调用。
	spec               plan.Spec                     // spec 是本轮私有冻结描述。
	options            c.RunOptions                  // options 是本轮观察与交互端口。
	collector          *output.Collector             // collector 仅收集有界最终候选。
	sequence           uint64                        // sequence 是本轮交付顺序。
	bytes              int                           // bytes 是累计已接收事件字节。
	events             int                           // events 是累计已接收事件数量。
	closed             bool                          // closed 拒绝 Prompt 返回后的迟到生产。
	effects            c.Effects                     // effects 保存真实输入与工具事实。
	firstFault         *c.Fault                      // firstFault 保存同步消费的首个故障。
	deliveryFault      *c.Fault                      // deliveryFault 独立保存观察交付故障。
	interactions       map[string]bool               // interactions 保存有界已回答请求身份。
	toolNames          map[string]string             // toolNames 以有界调用身份关联已获授权的工具名称。
	activeInteractions map[string]context.CancelFunc // activeInteractions 允许 finish 撤销并等待已接纳的交互。
	interactionsDone   chan struct{}                 // interactionsDone 在当前活动交互全部退出时关闭。
	cleanupTimeout     time.Duration                 // cleanupTimeout 限制 Prompt 返回后的交互收敛等待。
	logger             *slog.Logger                  // logger 只接收技术诊断，不交付业务事实。
	promptStarted      time.Time                     // promptStarted 是提交当前输入的阶段起点，不包含 CLI 初始化。
}

func newSink(call *invocation, spec plan.Spec, options c.RunOptions, cleanupTimeout time.Duration, logger *slog.Logger) *eventSink {
	return &eventSink{call: call, spec: spec, options: options, collector: output.New(spec.Policies.Output, spec.Limits), interactions: make(map[string]bool), toolNames: make(map[string]string), activeInteractions: make(map[string]context.CancelFunc), cleanupTimeout: cleanupTimeout, logger: logger, promptStarted: time.Now()}
}
func (s *eventSink) fail(f *c.Fault) error {
	if s.firstFault == nil {
		s.firstFault = f
		s.call.cancel(f)
	}
	return s.firstFault
}

// Accept 在同步消费链中核算预算；事实交付始终由本轮 Context 拥有。
// 来源 Context 不抹掉已发生的 Prompt/工具事实，Observer 使用本轮 Context 和独立交付期限。
func (s *eventSink) Accept(_ context.Context, event c.DriverEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fault(c.FaultProtocol, "event", "event arrived after prompt returned", nil)
	}
	// 排空仍属于已接纳回合：先登记收到的真实证据，再决定是否继续消费/交付。
	duplicateAcceptance := event.Kind == c.EventPromptAccepted && s.effects.PromptAccepted
	if event.Kind == c.EventPromptAccepted {
		if !duplicateAcceptance {
			logStage(s.logger, s.call, "input_acceptance", time.Since(s.promptStarted), true, false)
		}
		s.effects.PromptAccepted = true
	}
	if event.Kind == c.EventTool && event.Tool != nil {
		s.effects.ToolActivity = true
	}
	if s.firstFault != nil {
		return s.firstFault
	}
	if err := s.call.ctx.Err(); err != nil {
		return err
	}
	if duplicateAcceptance {
		return s.fail(fault(c.FaultProtocol, "event", "prompt acceptance emitted twice", nil))
	}
	// 按规范载荷的字符串实际字节与固定标量开销计费；事件热路径不做 JSON 编码。
	// 32 字节是每个事件的固定预算开销，消息完成标记另计 2 字节，用量的七个整数与两个标记另计 58 字节。
	size := 32 + len(event.Text) + len(event.Kind)
	if event.Message != nil {
		m := *event.Message
		event.Message = &m
		var contentBytes int
		m.Content, contentBytes = cloneContent(m.Content)
		size += len(m.MessageID) + len(m.Phase) + len(m.Delta) + contentBytes + 2
	}
	if event.Tool != nil {
		tool := *event.Tool
		event.Tool = &tool
		var contentBytes int
		tool.Content, contentBytes = cloneContent(tool.Content)
		tool.Input = slices.Clone(tool.Input)
		tool.Output = slices.Clone(tool.Output)
		tool.Locations = slices.Clone(tool.Locations)
		for i, location := range tool.Locations {
			size += len(location.Path) + 8
			if location.Line != nil {
				line := *location.Line
				tool.Locations[i].Line = &line
			}
		}
		size += len(tool.ID) + len(tool.Name) + len(tool.Title) + len(tool.Kind) + len(tool.Status) + len(tool.Text) + len(tool.Input) + len(tool.Output) + contentBytes
	}
	if event.Usage != nil {
		usage := *event.Usage
		event.Usage = &usage
		size += 58
	}
	if size > s.spec.Limits.MaxEventBytes || s.events >= s.spec.Limits.MaxEvents || size > s.spec.Limits.MaxTotalEventBytes-s.bytes {
		return s.fail(fault(c.FaultLimit, "event", "event delivery budget exceeded", nil))
	}
	var err error
	s.events++
	s.bytes += size
	switch event.Kind {
	case c.EventPromptAccepted:
	case c.EventAssistantMessage:
		if !s.effects.PromptAccepted || event.Message == nil {
			return s.fail(fault(c.FaultProtocol, "event", "assistant message before prompt acceptance or without payload", nil))
		}
		if err = s.collector.Accept(*event.Message); err != nil {
			return s.fail(asFault(err, c.FaultProtocol, "output", "invalid assistant message"))
		}
	case c.EventThinking:
		if !s.effects.PromptAccepted {
			return s.fail(fault(c.FaultProtocol, "event", "thinking event before prompt acceptance", nil))
		}
	case c.EventTool:
		if !s.effects.PromptAccepted || event.Tool == nil {
			return s.fail(fault(c.FaultProtocol, "event", "tool event before prompt acceptance or without payload", nil))
		}
		s.effects.ToolActivity = true
		if event.Tool.ID == "" {
			return s.fail(fault(c.FaultProtocol, "event", "tool activity identity missing", nil))
		}
		name := event.Tool.Name
		if previous, known := s.toolNames[event.Tool.ID]; known {
			if name == "" {
				name = previous
			} else if previous != "" && previous != name {
				return s.fail(fault(c.FaultProtocol, "event", "tool activity identity changed tool name", nil))
			}
		}
		if s.spec.Policies.Tools.Mode != c.ToolsAllowAll && !s.toolAllowed(name) {
			return s.fail(fault(c.FaultProtocol, "event", "driver emitted unauthorized tool activity", nil))
		}
		s.toolNames[event.Tool.ID] = name
	case c.EventUsage:
		if !s.effects.PromptAccepted || event.Usage == nil {
			return s.fail(fault(c.FaultProtocol, "event", "usage event before prompt acceptance or without payload", nil))
		}
	default:
		return s.fail(fault(c.FaultProtocol, "event", "unknown driver event kind", nil))
	}
	s.sequence++
	if s.options.Observer != nil {
		ctx, cancel := context.WithTimeout(s.call.ctx, s.spec.Limits.DeliveryTimeout)
		defer cancel()
		err = s.options.Observer.Accept(ctx, c.Event{Invocation: s.call.ref, Sequence: s.sequence, DriverEvent: event})
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			if s.call.ctx.Err() != nil {
				return s.call.ctx.Err()
			}
			s.deliveryFault = fault(c.FaultDelivery, "observer", "observer delivery failed", err)
			return s.fail(s.deliveryFault)
		}
	}
	return nil
}
func (s *eventSink) toolAllowed(name string) bool {
	return s.spec.Policies.Tools.Mode == c.ToolsAllowAll || name != "" && s.spec.Policies.Tools.Mode == c.ToolsAllowList && slices.Contains(s.spec.Policies.Tools.Allow, name)
}

// Interact 验证调用修订和一次性请求身份；无人值守输入立即结束调用。
func (s *eventSink) Interact(requestCtx context.Context, request c.InteractionRequest) (c.InteractionResponse, error) {
	empty := c.InteractionResponse{}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return empty, fault(c.FaultProtocol, "interaction", "interaction arrived after prompt returned", nil)
	}
	if s.firstFault != nil {
		err := s.firstFault
		s.mu.Unlock()
		return empty, err
	}
	if err := s.call.ctx.Err(); err != nil {
		s.mu.Unlock()
		return empty, err
	}
	if !s.effects.PromptAccepted || request.ID == "" {
		err := s.fail(fault(c.FaultProtocol, "interaction", "invalid interaction identity or ordering", nil))
		s.mu.Unlock()
		return empty, err
	}
	if request.Invocation != (c.InvocationRef{}) && request.Invocation != s.call.ref {
		err := s.fail(fault(c.FaultProtocol, "interaction", "interaction invocation identity mismatch", nil))
		s.mu.Unlock()
		return empty, err
	}
	request.Invocation = s.call.ref
	if s.interactions[request.ID] {
		err := s.fail(fault(c.FaultProtocol, "interaction", "duplicate interaction identity", nil))
		s.mu.Unlock()
		return empty, err
	}
	encoded, encodeErr := json.Marshal(request)
	if encodeErr != nil {
		err := s.fail(fault(c.FaultProtocol, "interaction", "interaction request is not JSON", encodeErr))
		s.mu.Unlock()
		return empty, err
	}
	size := len(encoded)
	if err := s.reserveInteractionBudget(size); err != nil {
		s.mu.Unlock()
		return empty, err
	}
	s.interactions[request.ID] = true
	// 即便撤销请求先于接纳到达，其身份也已失效，不能用新 Context 重用。
	if err := requestCtx.Err(); err != nil {
		s.mu.Unlock()
		return empty, err
	}
	response := c.InteractionResponse{Invocation: s.call.ref, ID: request.ID, Action: c.InteractionCancel}
	if request.Kind != c.InteractionApproval && request.Kind != c.InteractionInput {
		err := s.fail(fault(c.FaultProtocol, "interaction", "unknown interaction kind", nil))
		s.mu.Unlock()
		return empty, err
	}
	if request.Kind == c.InteractionApproval && !s.toolAllowed(request.ToolName) {
		s.mu.Unlock()
		return response, nil
	}
	if s.spec.Policies.Interaction == c.InteractionUnattended {
		if request.Kind == c.InteractionApproval {
			if request.Approval != nil {
				for _, option := range request.Approval.Options {
					if option.Kind == "allow_once" || option.Kind == "allow_always" {
						response.Action = c.InteractionAccept
						response.OptionID = option.ID
						break
					}
				}
			}
			s.mu.Unlock()
			return response, nil
		}
		err := s.fail(fault(c.FaultInteractionRequired, "interaction", "user input is required", nil))
		s.mu.Unlock()
		return empty, err
	}
	if s.options.InteractionHandler == nil {
		err := s.fail(fault(c.FaultInteractionRequired, "interaction", "interaction handler is not available", nil))
		s.mu.Unlock()
		return empty, err
	}
	handlerCtx, cancel := context.WithTimeout(s.call.ctx, s.spec.Limits.InteractionTimeout)
	stopRequestCancellation := context.AfterFunc(requestCtx, cancel)
	if len(s.activeInteractions) == 0 {
		s.interactionsDone = make(chan struct{})
	}
	s.activeInteractions[request.ID] = cancel
	handler := s.options.InteractionHandler
	s.mu.Unlock()
	// 交互端口不占用事件消费锁；单请求撤销只使本问题失效。
	response, err := handler.Handle(handlerCtx, request)
	if err == nil {
		err = handlerCtx.Err()
	}
	stopRequestCancellation()
	cancel()
	s.mu.Lock()
	defer func() {
		delete(s.activeInteractions, request.ID)
		if len(s.activeInteractions) == 0 {
			close(s.interactionsDone)
		}
		s.mu.Unlock()
	}()
	if requestErr := requestCtx.Err(); requestErr != nil {
		return empty, requestErr
	}
	if callErr := s.call.ctx.Err(); callErr != nil {
		return empty, callErr
	}
	if s.closed {
		return empty, fault(c.FaultCancelled, "interaction", "interaction no longer belongs to an active turn", nil)
	}
	if s.firstFault != nil {
		return empty, s.firstFault
	}

	if err != nil {
		return empty, s.fail(fault(c.FaultInteractionRequired, "interaction", "interaction delivery failed", err))
	}
	if response.Invocation != s.call.ref || response.ID != request.ID {
		return empty, s.fail(fault(c.FaultProtocol, "interaction", "interaction response identity mismatch", nil))
	}
	if response.Action != c.InteractionAccept && response.Action != c.InteractionDecline && response.Action != c.InteractionCancel {
		return empty, s.fail(fault(c.FaultProtocol, "interaction", "unknown interaction response action", nil))
	}
	encoded, encodeErr = json.Marshal(response)
	if encodeErr != nil {
		return empty, s.fail(fault(c.FaultProtocol, "interaction", "interaction response is not JSON", encodeErr))
	}
	if err := s.reserveInteractionBudget(len(encoded)); err != nil {
		return empty, err
	}
	return response, nil
}

// reserveInteractionBudget 与普通事件共用本轮总量，不把交互身份集合变成另一条无界缓存。
func (s *eventSink) reserveInteractionBudget(size int) error {
	if size > s.spec.Limits.MaxEventBytes || s.events >= s.spec.Limits.MaxEvents || size > s.spec.Limits.MaxTotalEventBytes-s.bytes {
		return s.fail(fault(c.FaultLimit, "interaction", "interaction delivery budget exceeded", nil))
	}
	s.events++
	s.bytes += size
	return nil
}

func (s *eventSink) finish() (*c.Fault, c.Effects, *c.Fault) {
	s.mu.Lock()
	s.closed = true
	var done <-chan struct{}
	if len(s.activeInteractions) > 0 {
		done = s.interactionsDone
		for _, cancel := range s.activeInteractions {
			cancel()
		}
	}
	s.mu.Unlock()
	if done != nil {
		timer := time.NewTimer(s.cleanupTimeout)
		select {
		case <-done:
		case <-timer.C:
			s.mu.Lock()
			s.fail(fault(c.FaultProtocol, "interaction", "driver returned while interaction handler did not stop", nil))
			s.mu.Unlock()
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveryFault, s.effects, s.firstFault
}

// cloneContent 将规范内容与观察者分离，并按实际字符串字节计入本轮预算。
func cloneContent(content []c.ContentBlock) ([]c.ContentBlock, int) {
	result := slices.Clone(content)
	size := 0
	for i, block := range result {
		size += 32 + len(block.Type) + len(block.Text) + len(block.MIMEType) + len(block.Data) + len(block.URI) + len(block.Name) + len(block.TerminalID)
		if block.Diff != nil {
			diff := *block.Diff
			result[i].Diff = &diff
			size += len(diff.Path) + len(diff.OldText) + len(diff.NewText)
		}
	}
	return result, size
}
