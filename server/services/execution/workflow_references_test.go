package execution

import (
	"reflect"
	"strings"
	"testing"

	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkflowReferencesReachNativeResumePromptWithoutReloadingInstructions(t *testing.T) {
	manager := agentPreparationFixture(t)
	journal := manager.journal.(*agentTestJournal)
	manager.agentBindings = func(models.Chat, string, []string, string) []capability.Binding {
		return []capability.Binding{agentPreparationBinding(t, "workflow.source.save")}
	}
	request := recordedRun{
		TurnInput:   agentapi.TurnInput{Message: agentapi.Message{Text: "修改选中的步骤"}},
		SessionPlan: agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: "codex"}, Instructions: []agentapi.Instruction{{Name: "WORKFLOW_CANVAS_REFERENCES.md", Content: `{"revisionId":"original-revision","kind":"phase","id":"exact-phase"}`}, {Name: "WORKFLOW_RUN_REFERENCE.md", Content: `{"runId":"original-run"}`}, {Name: "TEAM.md", Content: "unrelated team instructions"}}},
	}
	_, release, err := manager.prepareAgent(&request.SessionPlan, &request.TurnInput, journal.chat, "turn", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	release()
	stableInstructions := request.Instructions
	for _, instruction := range request.Instructions {
		if instruction.Name == "WORKFLOW_CANVAS_REFERENCES.md" || instruction.Name == "WORKFLOW_RUN_REFERENCE.md" {
			t.Fatalf("per-turn references entered stable session plan: %+v", request.Instructions)
		}
	}
	// 模拟原生进程复用已有会话，只发送本轮 Message，不重新读取 Instructions。
	request.Instructions = nil
	for _, expected := range []string{"original-revision", "exact-phase", "original-run"} {
		if !strings.Contains(request.Message.Text, expected) {
			t.Fatalf("resume prompt lost exact reference %q: %s", expected, request.Message.Text)
		}
	}
	if strings.Contains(request.Message.Text, "unrelated team instructions") {
		t.Fatal("workflow reference injection changed unrelated context or native continuation")
	}
	next := recordedRun{SessionPlan: agentapi.SessionPlan{Target: request.Target, Instructions: []agentapi.Instruction{{Name: "TEAM.md", Content: "unrelated team instructions"}}}, TurnInput: agentapi.TurnInput{Message: agentapi.Message{Text: "继续"}}}
	_, releaseNext, err := manager.prepareAgent(&next.SessionPlan, &next.TurnInput, journal.chat, "turn", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseNext()
	if !reflect.DeepEqual(next.Instructions, stableInstructions) {
		t.Fatal("changing canvas or run references changed the stable authoring plan")
	}
	if !strings.Contains(next.Message.Text, "\n[]\n</workflow-reference-context>") || strings.Contains(next.Message.Text, "exact-phase") {
		t.Fatal("removed references were reused on continuation")
	}
}
