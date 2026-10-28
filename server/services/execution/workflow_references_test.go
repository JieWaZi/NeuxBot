package execution

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

func TestWorkflowReferencesReachNativeResumePromptWithoutReloadingInstructions(t *testing.T) {
	request := runtimes.RunRequest{
		Session:      runtimes.SessionDirective{Mode: runtimes.SessionModeResume, ID: "native-session"},
		Message:      runtimes.Message{Text: "修改选中的步骤"},
		Instructions: []runtimes.Instruction{{Name: "WORKFLOW_CANVAS_REFERENCES.md", Content: `{"revisionId":"original-revision","kind":"phase","id":"exact-phase"}`}, {Name: "WORKFLOW_RUN_REFERENCE.md", Content: `{"runId":"original-run"}`}, {Name: "TEAM.md", Content: "unrelated team instructions"}},
	}
	appendCurrentWorkflowReferences(&request)
	// 模拟原生进程复用已有会话，只发送本轮 Message，不重新读取 Instructions。
	request.Instructions = nil
	for _, expected := range []string{"original-revision", "exact-phase", "original-run"} {
		if !strings.Contains(request.Message.Text, expected) {
			t.Fatalf("resume prompt lost exact reference %q: %s", expected, request.Message.Text)
		}
	}
	if strings.Contains(request.Message.Text, "unrelated team instructions") || request.Session.Mode != runtimes.SessionModeResume || request.Session.ID != "native-session" {
		t.Fatal("workflow reference injection changed unrelated context or native continuation")
	}
	next := runtimes.RunRequest{Session: request.Session, Message: runtimes.Message{Text: "继续"}}
	appendCurrentWorkflowReferences(&next)
	if !strings.Contains(next.Message.Text, "\n[]\n</workflow-reference-context>") || strings.Contains(next.Message.Text, "exact-phase") {
		t.Fatal("removed references were reused on continuation")
	}
}
