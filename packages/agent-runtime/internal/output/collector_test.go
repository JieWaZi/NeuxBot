package output

import (
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
