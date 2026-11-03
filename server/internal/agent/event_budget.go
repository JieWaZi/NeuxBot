package agent

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
)

// toolEventState 保存授权与预算共用的最近工具事实，身份数量和保留字节均受本轮预算约束。
type toolEventState struct {
	name      string            // name 是同一调用已证明且不得改变的工具名。
	input     json.RawMessage   // input 是最近参数快照，与观察者副本隔离。
	title     string            // title 是最后提供的显示标题，用于识别真实状态变化。
	kind      string            // kind 是最后提供的公开工具类别。
	status    string            // status 是最后提供的原生活动阶段。
	locations []native.Location // locations 是最后提供的文件位置快照，不保留正文。
}

// messageEventState 只保存当前消息的边界证据，不累计正文或第二份消息历史。
type messageEventState struct {
	seen  bool                // seen 区分尚未收到消息与没有原生身份的消息流。
	id    string              // id 是当前消息身份。
	phase native.MessagePhase // phase 是当前消息最后明确提供的阶段。
}

// usagePayloadBytes 让流式与最终用量按相同标量和元数据字符串规则核算字节。
func usagePayloadBytes(usage native.Usage) int {
	size := 58
	if reported := usage.Reported; reported != nil {
		size += 42 + len(reported.Source) + len(reported.Coverage) + len(reported.Model)
	}
	return size
}

// reserveEventBudget 在调用方持锁时共用本轮预算；错误交由调用方保留原终态或中止流式交付。
func (s *eventSink) reserveEventBudget(size, chargedBytes int, discrete bool) *native.Fault {
	if size > s.spec.Limits.MaxEventBytes {
		return eventLimit(native.LimitSingleEventBytes, "event", "event delivery budget exceeded: single event bytes")
	}
	if discrete && s.events >= s.spec.Limits.MaxEvents {
		return eventLimit(native.LimitEventCount, "event", "event delivery budget exceeded: discrete event count")
	}
	if chargedBytes > s.spec.Limits.MaxTotalEventBytes-s.bytes {
		return eventLimit(native.LimitTotalEventBytes, "event", "event delivery budget exceeded: cumulative event bytes")
	}
	if discrete {
		s.events++
	}
	s.bytes += chargedBytes
	return nil
}

// discreteEvent 区分需要计数的事实边界与只按字节核算的流式载荷。
// 同步交付不积压原始片段；其固定处理开销仍逐条计入累计字节，空/重复无进展通知仍逐条计数。
func (s *eventSink) discreteEvent(event native.Event) bool {
	switch event.Kind {
	case native.EventThinking:
		return event.Text == ""
	case native.EventAssistantMessage:
		message := event.Message
		if message == nil {
			return true
		}
		previous := s.messageState
		if !previous.seen || message.MessageID != previous.id || message.Complete || message.CompletionVerified {
			return true
		}
		if message.Phase != "" && message.Phase != native.PhaseUnknown && message.Phase != previous.phase {
			return true
		}
		hasText, onlyText := textStreamContent(message.Content)
		return !onlyText || message.Delta == "" && !hasText
	case native.EventTool:
		tool := event.Tool
		if tool == nil {
			return true
		}
		previous, known := s.tools[tool.ID]
		if !known || !tool.Update || tool.Output != nil || tool.Status == "completed" || tool.Status == "failed" || tool.Status == "cancelled" {
			return true
		}
		if tool.Name != "" && tool.Name != previous.name || tool.Title != "" && tool.Title != previous.title || tool.Kind != "" && tool.Kind != previous.kind || tool.Status != "" && tool.Status != previous.status {
			return true
		}
		if tool.Locations != nil && !sameToolLocations(previous.locations, tool.Locations) {
			return true
		}
		hasText, onlyText := textStreamContent(tool.Content)
		if !onlyText {
			return true
		}
		if hasText {
			return false
		}
		// Text 是活动摘要；真正追加正文由 Content 表达，不能把重复摘要当作无限进展。
		if tool.Text != "" {
			return true
		}
		return tool.Input == nil || len(tool.Input) == 0 || bytes.Equal(previous.input, tool.Input)
	default:
		// 接纳、用量及未知事件不是正文流；协议校验仍由唯一消费路径负责。
		return true
	}
}

// textStreamContent 分别识别新增文本与非文本边界；空文本块不抹掉同帧其他真实载荷的进展。
func textStreamContent(content []native.ContentBlock) (hasText, onlyText bool) {
	for _, block := range content {
		if block.Type != "text" || block.Diff != nil || block.Data != "" || block.URI != "" || block.TerminalID != "" {
			return false, false
		}
		hasText = hasText || block.Text != ""
	}
	return hasText, true
}

func sameToolLocations(left, right []native.Location) bool {
	if len(left) != len(right) {
		return false
	}
	for i, a := range left {
		b := right[i]
		if a.Path != b.Path || (a.Line == nil) != (b.Line == nil) || a.Line != nil && *a.Line != *b.Line {
			return false
		}
	}
	return true
}

func (s *eventSink) rememberMessage(message native.AssistantMessage) {
	if !s.messageState.seen || message.MessageID != s.messageState.id {
		s.messageState = messageEventState{seen: true, id: message.MessageID, phase: native.PhaseUnknown}
	}
	if message.Phase != "" && message.Phase != native.PhaseUnknown {
		s.messageState.phase = message.Phase
	}
}

func (s *eventSink) rememberTool(tool native.ToolEvent, name string) {
	previous := s.tools[tool.ID]
	previous.name = name
	if tool.Input != nil {
		previous.input = slices.Clone(tool.Input)
	}
	if tool.Title != "" {
		previous.title = tool.Title
	}
	if tool.Kind != "" {
		previous.kind = tool.Kind
	}
	if tool.Status != "" {
		previous.status = tool.Status
	}
	if tool.Locations != nil {
		previous.locations = slices.Clone(tool.Locations)
		for i, location := range previous.locations {
			if location.Line != nil {
				line := *location.Line
				previous.locations[i].Line = &line
			}
		}
	}
	s.tools[tool.ID] = previous
}

// eventLimit 保留结构化预算维度，产品分类不得依赖错误文本猜测单帧或整轮限制。
func eventLimit(kind native.LimitKind, stage, message string) *native.Fault {
	return &native.Fault{Code: native.FaultLimit, Limit: kind, Stage: stage, Message: message}
}
