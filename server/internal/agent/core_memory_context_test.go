package agent_test

import (
	"strings"
	"testing"

	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func TestCoreMemoryDeliveryFollowsNativeBinding(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	req.Connection.Launch.Environment["THREAD_FIXTURE_UNIQUE_CODEX_SESSION"] = "1"
	req.CoreMemoryOwner = "stable-partner"
	send := func(id int, binding agent.ThreadRequest, rev uint64, text, body string) agent.TurnResult {
		t.Helper()
		input := chatTurn(id, text)
		if body != "" {
			input.CoreMemory = &agent.CoreMemoryContext{Revision: rev, JSON: []byte(body)}
		}
		return completeThreadFixtureTurn(t, lifecycle, store, binding, input, id == 1)
	}
	first := send(1, req, 1, "first-user-prompt", `{"items":[{"id":"a","text":"preferred-language"}]}`)
	if line := fixturePromptLine(t, trace, "first-user-prompt"); strings.Count(line, "first-user-prompt") != 1 ||
		strings.Count(line, "preferred-language") != 1 || strings.Contains(line, `id\":`) || strings.Count(line, `\u003cneuxbot-turn-context\u003e`) != 1 {
		t.Fatalf("first binding missed its core snapshot: %s", line)
	}
	second := send(2, req, 1, "second-user-prompt", `{"items":[{"id":"a","text":"preferred-language"}]}`)
	if second.Binding.Session != first.Binding.Session {
		t.Fatal("same workbench entry did not reuse its session")
	}
	if line := fixturePromptLine(t, trace, "second-user-prompt"); strings.Contains(line, "preferred-language") {
		t.Fatalf("same version repeated its core snapshot: %s", line)
	}
	third := send(3, req, 2, "third-user-prompt", `{"items":[]}`)
	if line := fixturePromptLine(t, trace, "third-user-prompt"); !strings.Contains(line, `Core memories\":[]`) {
		t.Fatalf("empty revision did not reach its binding: %s", line)
	}

	external := req
	external.CoreMemoryOwner = ""
	fourth := send(4, external, 0, "external-user-prompt", "")
	if fourth.Binding.Session == third.Binding.Session {
		t.Fatal("external input reused a binding containing workbench memory")
	}
	if line := fixturePromptLine(t, trace, "external-user-prompt"); strings.Contains(line, "preferred-language") || strings.Contains(line, `Core memories\":[]`) {
		t.Fatalf("workbench memory leaked in native history handoff: %s", line)
	}
	replacement := req
	replacement.ForceNew = true
	send(5, replacement, 2, "replacement-user-prompt", `{"items":[]}`)
	if line := fixturePromptLine(t, trace, "replacement-user-prompt"); strings.Contains(line, "Core memories") ||
		strings.Count(line, "replacement-user-prompt") != 1 || strings.Contains(line, "Current user message:") {
		t.Fatalf("replacement reused previous delivery result: %s", line)
	}
}

func TestCoreMemoryDeliveryRetriesAfterUnknownOutcome(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "claude")
	configureThreadFixture(&req, trace)
	req.Connection.Launch.Environment["THREAD_FIXTURE_CLAUDE_LEGACY"] = "1"
	req.CoreMemoryOwner = "stable-partner"
	snapshot := &agent.CoreMemoryContext{Revision: 1, JSON: []byte(`{"items":[{"id":"a","text":"durable-memory-marker"}]}`)}
	first := chatTurn(0, "unconfirmed legacy input")
	first.CoreMemory = snapshot
	turn, err := lifecycle.Prepare(t.Context(), req, first, chatPorts(req.ChatID, first, true))
	if err != nil {
		t.Fatal(err)
	}
	result := turn.Run(t.Context())
	if result.Submission != "sent" || result.Admission != "unknown" || result.Outcome != nil {
		t.Fatalf("fixture did not simulate an unknown outcome: %+v", result)
	}
	followup := chatTurn(1, "only the next legacy input")
	followup.CoreMemory = snapshot
	result = completeThreadFixtureTurn(t, lifecycle, store, req, followup, false)
	if result.Admission != "confirmed" {
		t.Fatal("recovery failed to confirm the next input")
	}
	if line := fixturePromptLine(t, trace, followup.Text); !strings.Contains(line, "durable-memory-marker") {
		t.Fatalf("unconfirmed delivery suppressed the retry: %s", line)
	}
}

func TestCoreMemoryUnavailableKeepsAnUnscopedSession(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	req.Connection.Launch.Environment["THREAD_FIXTURE_UNIQUE_CODEX_SESSION"] = "1"
	first := completeThreadFixtureTurn(t, lifecycle, store, req, chatTurn(1, "prior unscoped conversation"), true)
	req.CoreMemoryOwner = "stable-partner"
	second := completeThreadFixtureTurn(t, lifecycle, store, req, chatTurn(2, "continue when memory is unavailable"), false)
	if second.Binding.Session != first.Binding.Session {
		t.Fatal("an unavailable optional snapshot replaced the existing native session")
	}
	recovered := chatTurn(3, "continue when memory recovers")
	recovered.CoreMemory = &agent.CoreMemoryContext{Revision: 1, JSON: []byte(`{"items":[{"id":"a","text":"recovered-note"}]}`)}
	third := completeThreadFixtureTurn(t, lifecycle, store, req, recovered, false)
	if third.Binding.Session != first.Binding.Session || !strings.Contains(fixturePromptLine(t, trace, recovered.Text), "recovered-note") {
		t.Fatal("the recovered snapshot should be delivered without replacing the native session")
	}
	req.CoreMemoryOwner = ""
	fourth := completeThreadFixtureTurn(t, lifecycle, store, req, chatTurn(4, "send through another entry"), false)
	if fourth.Binding.Session == third.Binding.Session {
		t.Fatal("another entry reused the session after the recovered memory was delivered")
	}
}
