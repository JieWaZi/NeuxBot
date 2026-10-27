package output

import (
	"errors"
	c "github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	"testing"
)

func TestFinalEvidenceAndLastMessage(t *testing.T) {
	cases := []struct {
		name     string               // name 是行为用例名称。
		messages []c.AssistantMessage // messages 是依次到达的消息。
		stop     c.StopReason         // stop 是真实结束原因。
		status   c.OutputStatus       // status 是预期输出状态。
		text     string               // text 是独立推导的最终正文。
	}{
		{"unknown", []c.AssistantMessage{{MessageID: "1", Delta: "guess"}}, c.StopEndTurn, c.OutputUnavailable, ""},
		{"last final", []c.AssistantMessage{{MessageID: "1", Phase: c.PhaseFinal, Delta: "first", Complete: true}, {MessageID: "2", Phase: c.PhaseCommentary, Delta: "tool progress"}, {MessageID: "3", Phase: c.PhaseFinal, Delta: "answer", Complete: true}}, c.StopEndTurn, c.OutputAvailable, "answer"},
		{"final phase", []c.AssistantMessage{{MessageID: "1", Phase: c.PhaseFinal, Delta: "answer"}}, c.StopEndTurn, c.OutputAvailable, "answer"},
		{"refused", nil, c.StopRefused, c.OutputRefused, ""},
		{"truncated", []c.AssistantMessage{{MessageID: "1", Phase: c.PhaseFinal, Delta: "part"}}, c.StopMaxTokens, c.OutputTruncated, "part"},
		{"cancelled", []c.AssistantMessage{{MessageID: "1", Phase: c.PhaseFinal, Delta: "part"}}, c.StopCancelled, c.OutputUnavailable, ""},
		{"empty", []c.AssistantMessage{{MessageID: "1", Phase: c.PhaseFinal, Complete: true}}, c.StopEndTurn, c.OutputEmpty, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := New(c.OutputStrict, c.Limits{MaxOutputBytes: 64, MaxEvents: 32})
			for _, m := range tt.messages {
				if err := co.Accept(m); err != nil {
					t.Fatal(err)
				}
			}
			got := co.Finish(tt.stop)
			if got.Status != tt.status || got.Text != tt.text {
				t.Fatalf("got %#v", got)
			}
		})
	}
}
func TestLimitAndInterleaving(t *testing.T) {
	co := New(c.OutputStrict, c.Limits{MaxOutputBytes: 4, MaxEvents: 32})
	if err := co.Accept(c.AssistantMessage{MessageID: "1", Phase: c.PhaseFinal, Delta: "12345"}); err == nil {
		t.Fatal("expected output limit")
	}
	if got := co.Finish(c.StopEndTurn); got.Status != c.OutputLimitExceeded || got.Text != "" {
		t.Fatalf("got %#v", got)
	}
	co = New(c.OutputStrict, c.Limits{MaxOutputBytes: 64, MaxEvents: 32})
	_ = co.Accept(c.AssistantMessage{MessageID: "1", Phase: c.PhaseFinal, Delta: "old"})
	_ = co.Accept(c.AssistantMessage{MessageID: "2", Phase: c.PhaseFinal, Delta: "new"})
	if err := co.Accept(c.AssistantMessage{MessageID: "1", Phase: c.PhaseFinal, Delta: "tail"}); err == nil {
		t.Fatal("interleaving must fail conservatively")
	}
	if got := co.Finish(c.StopEndTurn); got.Status != c.OutputUnavailable {
		t.Fatalf("got %#v", got)
	}
}
func BenchmarkCollector(b *testing.B) {
	for range b.N {
		co := New(c.OutputStrict, c.Limits{MaxOutputBytes: 1 << 20, MaxEvents: 4096})
		for range 100 {
			_ = co.Accept(c.AssistantMessage{MessageID: "a", Phase: c.PhaseFinal, Delta: "some answer token "})
		}
		_ = co.Finish(c.StopEndTurn)
	}
}

func TestMissingMessageIdentityIsUnavailableWithoutExecutionFailure(t *testing.T) {
	co := New(c.OutputStrict, c.Limits{MaxOutputBytes: 64, MaxEvents: 32})
	if err := co.Accept(c.AssistantMessage{Phase: c.PhaseUnknown, Delta: "no identity"}); err != nil {
		t.Fatalf("missing proof must not fail execution: %v", err)
	}
	if got := co.Finish(c.StopEndTurn); got.Status != c.OutputUnavailable {
		t.Fatalf("%#v", got)
	}
}

func TestLateCommentaryClassificationRevokesOnlyItsOwnCandidates(t *testing.T) {
	cases := []struct {
		name               string         // name 是需要保留或撤销候选的行为。
		priorFinal         bool           // priorFinal 表示存在无关的已验证答案。
		verifiedCompletion bool           // verifiedCompletion 表示当前消息曾获等价完成证明。
		status             c.OutputStatus // status 是预期输出状态。
		text               string         // text 是独立推导的最终正文。
	}{{"unknown becomes commentary", false, false, c.OutputUnavailable, ""}, {"previous final survives", true, false, c.OutputAvailable, "real answer"}, {"completion candidate becomes commentary", false, true, c.OutputUnavailable, ""}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := New(c.OutputBestEffort, c.Limits{MaxOutputBytes: 128, MaxEvents: 16})
			if tt.priorFinal {
				if err := co.Accept(c.AssistantMessage{MessageID: "answer", Phase: c.PhaseFinal, Delta: "real answer", Complete: true}); err != nil {
					t.Fatal(err)
				}
			}
			if err := co.Accept(c.AssistantMessage{MessageID: "process", Delta: "process only", Complete: tt.verifiedCompletion, CompletionVerified: tt.verifiedCompletion}); err != nil {
				t.Fatal(err)
			}
			if err := co.Accept(c.AssistantMessage{MessageID: "process", Phase: c.PhaseCommentary, Complete: true}); err != nil {
				t.Fatal(err)
			}
			got := co.Finish(c.StopEndTurn)
			if got.Status != tt.status || got.Text != tt.text {
				t.Fatalf("commentary candidate leaked or unrelated final cleared: %#v", got)
			}
			if tt.priorFinal && got.Reliability != c.ReliabilityVerifiedPhase {
				t.Fatalf("unrelated final proof changed: %#v", got)
			}
		})
	}
}

// TestCompletionReclassifiesCurrentMessage 捕获完成边界重分类丢失正文、泄漏候选或覆盖旧最终答案的错误。
func TestCompletionReclassifiesCurrentMessage(t *testing.T) {
	cases := []struct {
		name       string         // name 是完成重分类场景。
		priorFinal bool           // priorFinal 表示另一个消息已有最终答案。
		before     c.MessagePhase // before 是流式正文最初的阶段。
		after      c.MessagePhase // after 是完成边界提供的权威阶段。
		tail       string         // tail 是完成通知独有的追加正文。
		status     c.OutputStatus // status 是完成后的预期输出状态。
		text       string         // text 是独立推导的最终正文。
	}{
		{name: "commentary becomes final", before: c.PhaseCommentary, after: c.PhaseFinal, status: c.OutputAvailable, text: "body"},
		{name: "completion appends final tail", before: c.PhaseCommentary, after: c.PhaseFinal, tail: " tail", status: c.OutputAvailable, text: "body tail"},
		{name: "final becomes commentary", before: c.PhaseFinal, after: c.PhaseCommentary, status: c.OutputUnavailable},
		{name: "previous final survives retraction", priorFinal: true, before: c.PhaseFinal, after: c.PhaseCommentary, status: c.OutputAvailable, text: "prior answer"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			co := New(c.OutputBestEffort, c.Limits{MaxOutputBytes: 32, MaxEvents: 16})
			if tt.priorFinal {
				if err := co.Accept(c.AssistantMessage{MessageID: "prior", Phase: c.PhaseFinal, Delta: "prior answer", Complete: true}); err != nil {
					t.Fatal(err)
				}
			}
			if err := co.Accept(c.AssistantMessage{MessageID: "current", Phase: tt.before, Delta: "body"}); err != nil {
				t.Fatal(err)
			}
			// 查询当前结果不能把仍可被完成阶段撤销的候选固化。
			_ = co.Finish(c.StopEndTurn)
			if err := co.Accept(c.AssistantMessage{MessageID: "current", Phase: tt.after, Delta: tt.tail, Complete: true}); err != nil {
				t.Fatalf("authoritative completion was rejected: %v", err)
			}
			got := co.Finish(c.StopEndTurn)
			if got.Status != tt.status || got.Text != tt.text {
				t.Fatalf("reclassification lost or leaked a candidate: %+v", got)
			}
			if tt.status == c.OutputAvailable && got.Reliability != c.ReliabilityVerifiedPhase {
				t.Fatalf("final phase proof lost: %+v", got)
			}
		})
	}
}

// TestPhaseConflictWithoutCompletionRemainsInvalid 防止允许非完成通知任意改写已知消息阶段。
func TestPhaseConflictWithoutCompletionRemainsInvalid(t *testing.T) {
	for _, phase := range []c.MessagePhase{c.PhaseCommentary, c.PhaseFinal} {
		t.Run(string(phase), func(t *testing.T) {
			co := New(c.OutputBestEffort, c.Limits{MaxOutputBytes: 32, MaxEvents: 16})
			if err := co.Accept(c.AssistantMessage{MessageID: "a", Phase: phase, Delta: "body"}); err != nil {
				t.Fatal(err)
			}
			next := c.PhaseFinal
			if phase == c.PhaseFinal {
				next = c.PhaseCommentary
			}
			var fault *c.Fault
			if err := co.Accept(c.AssistantMessage{MessageID: "a", Phase: next}); !errors.As(err, &fault) || fault.Code != c.FaultProtocol {
				t.Fatalf("non-completion phase conflict accepted: %v", err)
			}
			if got := co.Finish(c.StopEndTurn); got.Status != c.OutputUnavailable {
				t.Fatalf("conflicting message became output: %+v", got)
			}
		})
	}
}

// TestCommentaryBudgetIsDeferredUntilFinal 验证过程正文有界保留，溢出只在该消息被证实为最终答案时影响输出。
func TestCommentaryBudgetIsDeferredUntilFinal(t *testing.T) {
	for _, promote := range []bool{false, true} {
		t.Run(map[bool]string{false: "discard oversized process", true: "oversized process becomes final"}[promote], func(t *testing.T) {
			co := New(c.OutputBestEffort, c.Limits{MaxOutputBytes: 4, MaxEvents: 16})
			if err := co.Accept(c.AssistantMessage{MessageID: "prior", Phase: c.PhaseFinal, Delta: "real", Complete: true}); err != nil {
				t.Fatal(err)
			}
			for range 1024 {
				if err := co.Accept(c.AssistantMessage{MessageID: "process", Phase: c.PhaseCommentary, Delta: "work"}); err != nil {
					t.Fatalf("process text consumed final output budget: %v", err)
				}
				if co.text.Len() > 4 {
					t.Fatalf("process retention exceeded output memory bound: %d", co.text.Len())
				}
			}
			if got := co.Finish(c.StopEndTurn); got.Status != c.OutputAvailable || got.Text != "real" {
				t.Fatalf("oversized process damaged unrelated final: %+v", got)
			}
			if promote {
				var fault *c.Fault
				if err := co.Accept(c.AssistantMessage{MessageID: "process", Phase: c.PhaseFinal, Complete: true}); !errors.As(err, &fault) || fault.Code != c.FaultLimit {
					t.Fatalf("discarded body silently became final: %v", err)
				}
				if got := co.Finish(c.StopEndTurn); got.Status != c.OutputLimitExceeded || got.Text != "" {
					t.Fatalf("discarded final body reported as valid output: %+v", got)
				}
				return
			}
			if err := co.Accept(c.AssistantMessage{MessageID: "next", Phase: c.PhaseFinal, Delta: "done", Complete: true}); err != nil {
				t.Fatalf("process overflow poisoned next message: %v", err)
			}
			if got := co.Finish(c.StopEndTurn); got.Status != c.OutputAvailable || got.Text != "done" {
				t.Fatalf("next final lost: %+v", got)
			}
		})
	}
}
