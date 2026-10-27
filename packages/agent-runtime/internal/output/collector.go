// Package output 收集有界的单条最终消息，不推测缺失的最终语义。
package output

import (
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"strings"
)

// Collector 只保存当前消息和最近最终候选；已见身份集合受事件预算限制。
type Collector struct {
	policy      c.OutputPolicy  // policy 是最终证据要求。
	maxBytes    int             // maxBytes 是每个候选正文上限。
	maxMessages int             // maxMessages 限制消息身份数量。
	currentID   string          // currentID 是当前连续消息身份。
	phase       c.MessagePhase  // phase 是当前消息已知阶段。
	verified    bool            // verified 是驱动等价完成证明。
	complete    bool            // complete 是消息完成边界。
	text        strings.Builder // text 以摊销线性开销追加正文。
	seen        map[string]bool // seen 防止交错分片被误当作完整答案。
	final       c.Output        // final 是最近明确最终候选。
	fallback    c.Output        // fallback 是明确标注未验证的当前正文。
	finalID     string          // finalID 标识已验证候选所属消息，防止撤销无关答案。
	fallbackID  string          // fallbackID 标识未验证候选所属消息。
	exceeded    bool            // exceeded 表示正文预算耗尽。
	invalid     bool            // invalid 表示消息身份契约损坏。
}

// New 建立无后台工作的有界收集器。
func New(policy c.OutputPolicy, limits c.Limits) *Collector {
	return &Collector{policy: policy, maxBytes: limits.MaxOutputBytes, maxMessages: limits.MaxEvents, seen: make(map[string]bool)}
}

// Accept 接收同一消息的连续增量，拒绝会造成伪完整正文的交错消息。
func (co *Collector) Accept(m c.AssistantMessage) error {
	if co.exceeded {
		return &c.Fault{Code: c.FaultLimit, Stage: "output", Message: "final answer budget exceeded"}
	}
	if m.MessageID == "" {
		// 缺失身份只说明无法提取，不证明原生执行失败。
		return nil
	}
	if m.MessageID != co.currentID {
		co.snapshot()
		if co.seen[m.MessageID] || len(co.seen) >= co.maxMessages {
			co.invalid = true
			return &c.Fault{Code: c.FaultProtocol, Stage: "output", Message: "assistant message identity interleaved or over budget"}
		}
		co.seen[m.MessageID] = true
		co.currentID = m.MessageID
		co.phase = c.PhaseUnknown
		co.verified = false
		co.complete = false
		co.text.Reset()
	}
	if co.complete && m.Delta != "" {
		co.invalid = true
		return &c.Fault{Code: c.FaultProtocol, Stage: "output", Message: "text arrived after message completion"}
	}
	if m.Phase != "" && m.Phase != c.PhaseUnknown {
		if co.phase != c.PhaseUnknown && co.phase != m.Phase {
			co.invalid = true
			return &c.Fault{Code: c.FaultProtocol, Stage: "output", Message: "assistant phase changed incompatibly"}
		}
		co.phase = m.Phase
	}
	if co.phase == c.PhaseCommentary {
		if co.fallbackID == co.currentID {
			co.fallback = c.Output{}
			co.fallbackID = ""
		}
		if co.finalID == co.currentID {
			co.final = c.Output{}
			co.finalID = ""
		}
		co.text.Reset()
		co.complete = co.complete || m.Complete
		return nil
	}
	if len(m.Delta) > co.maxBytes-co.text.Len() {
		co.exceeded = true
		co.text.Reset()
		co.final = c.Output{}
		co.fallback = c.Output{}
		co.finalID = ""
		co.fallbackID = ""
		return &c.Fault{Code: c.FaultLimit, Stage: "output", Message: "final answer budget exceeded"}
	}
	co.text.WriteString(m.Delta)
	co.verified = co.verified || m.CompletionVerified
	co.complete = co.complete || m.Complete
	co.snapshot()
	return nil
}

func (co *Collector) snapshot() {
	if co.currentID == "" || co.phase == c.PhaseCommentary {
		return
	}
	value := c.Output{Text: co.text.String(), Reliability: c.ReliabilityUnverified}
	co.fallback = value
	co.fallbackID = co.currentID
	if co.phase == c.PhaseFinal {
		value.Reliability = c.ReliabilityVerifiedPhase
		co.final = value
		co.finalID = co.currentID
	} else if co.verified && co.complete {
		value.Reliability = c.ReliabilityVerifiedCompletion
		co.final = value
		co.finalID = co.currentID
	}
}

// Finish 仅在原生终态允许时提供最终答案，执行状态由 Engine 独立保留。
func (co *Collector) Finish(stop c.StopReason) c.Output {
	if co.exceeded {
		return c.Output{Status: c.OutputLimitExceeded, Reliability: c.ReliabilityUnverified}
	}
	if co.invalid {
		return c.Output{Status: c.OutputUnavailable, Reliability: c.ReliabilityUnverified}
	}
	if stop == c.StopRefused {
		return c.Output{Status: c.OutputRefused, Reliability: c.ReliabilityUnverified}
	}
	if stop != c.StopEndTurn && stop != c.StopMaxTokens {
		return c.Output{Status: c.OutputUnavailable, Reliability: c.ReliabilityUnverified}
	}
	result := co.final
	if result.Reliability == "" && co.policy == c.OutputBestEffort {
		result = co.fallback
	}
	if result.Reliability == "" {
		return c.Output{Status: c.OutputUnavailable, Reliability: c.ReliabilityUnverified}
	}
	if stop == c.StopMaxTokens {
		result.Status = c.OutputTruncated
		return result
	}
	if strings.TrimSpace(result.Text) == "" {
		result.Status = c.OutputEmpty
	} else {
		result.Status = c.OutputAvailable
	}
	return result
}
