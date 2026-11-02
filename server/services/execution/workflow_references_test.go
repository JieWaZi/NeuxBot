package execution

import (
	"strings"
	"testing"

	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func TestWorkflowReferencesReachNativeResumePromptWithoutReloadingInstructions(t *testing.T) {
	request := recordedRun{
		Session:     agentapi.SessionDirective{Mode: agentapi.SessionModeResume, ID: "native-session"},
		TurnInput:   agentapi.TurnInput{Message: agentapi.Message{Text: "修改选中的步骤"}},
		SessionPlan: agentapi.SessionPlan{Instructions: []agentapi.Instruction{{Name: "WORKFLOW_CANVAS_REFERENCES.md", Content: `{"revisionId":"original-revision","kind":"phase","id":"exact-phase"}`}, {Name: "WORKFLOW_RUN_REFERENCE.md", Content: `{"runId":"original-run"}`}, {Name: "TEAM.md", Content: "unrelated team instructions"}}},
	}
	appendCurrentWorkflowReferences(&request.SessionPlan, &request.TurnInput)
	if len(request.Instructions) != 1 || request.Instructions[0].Name != "TEAM.md" {
		t.Fatalf("per-turn references entered stable session plan: %+v", request.Instructions)
	}
	// 模拟原生进程复用已有会话，只发送本轮 Message，不重新读取 Instructions。
	request.Instructions = nil
	for _, expected := range []string{"original-revision", "exact-phase", "original-run"} {
		if !strings.Contains(request.Message.Text, expected) {
			t.Fatalf("resume prompt lost exact reference %q: %s", expected, request.Message.Text)
		}
	}
	if strings.Contains(request.Message.Text, "unrelated team instructions") || request.Session.Mode != agentapi.SessionModeResume || request.Session.ID != "native-session" {
		t.Fatal("workflow reference injection changed unrelated context or native continuation")
	}
	next := recordedRun{Session: request.Session, TurnInput: agentapi.TurnInput{Message: agentapi.Message{Text: "继续"}}}
	appendCurrentWorkflowReferences(&next.SessionPlan, &next.TurnInput)
	if !strings.Contains(next.Message.Text, "\n[]\n</workflow-reference-context>") || strings.Contains(next.Message.Text, "exact-phase") {
		t.Fatal("removed references were reused on continuation")
	}
}
