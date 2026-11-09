package execution

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkflowReferencesReachNativeResumePromptWithoutReloadingInstructions(t *testing.T) {
	manager := agentPreparationFixture(t)
	journal := manager.journal.(*agentTestJournal)
	manager.agentBindings = func(models.Chat, string, []string, string) []capability.Binding {
		return []capability.Binding{agentPreparationBinding(t, "workflow.source.save")}
	}
	root := t.TempDir()
	state := messageChat{chat: journal.chat}
	files := &preparedMessageFiles{executionWorkspaceDirectory: root, instructions: []agentapi.Instruction{{Name: "PARTNER.md", Content: "persistent partner"}}, taskDirectories: workspace.TaskDirectories{Outputs: filepath.Join(root, "outputs", "turn"), Temporary: filepath.Join(root, "tmp", "turn")}}
	var stableThread agentapi.ThreadRequest
	for index, references := range [][]agentapi.Instruction{
		{{Name: "WORKFLOW_CANVAS_REFERENCES.md", Content: `{"revisionId":"original-revision","kind":"phase","id":"exact-phase"}`}, {Name: "WORKFLOW_RUN_REFERENCE.md", Content: `{"runId":"original-run"}`}},
		{{Name: "WORKFLOW_CANVAS_REFERENCES.md", Content: `{"revisionId":"new-revision","kind":"node","id":"new-node"}`}},
		nil,
	} {
		input := pendingMessage{message: userMessage{Text: "修改选中的步骤"}, runtime: runtimeSelection{RuntimeID: "codex"}, supplementalInstructions: []agentapi.Instruction{{Name: "TEAM.md", Content: "stable team role"}}, contextInstructions: references}
		plan := recordBusinessInputs(manager, state, input, files, nil, nil, models.RuntimeEnvironment{})

		turn := plan
		_, release, err := plan.prepare(manager, journal.chat, "turn", nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		release()
		turn.ContextInstructions = plan.ContextInstructions
		if index == 0 {
			stableThread = plan.thread()
		} else if !reflect.DeepEqual(plan.thread(), stableThread) {
			t.Fatal("changing canvas or run references changed the stable authoring session settings")
		}
		if turn.Message.Text != input.message.Text {
			t.Fatal("preparation rewrote the accepted business message")
		}
		// 热续聊仅通过统一新增输入发送当前上下文，不依赖重新读取稳定指令。
		native, err := turn.turnRequest("turn", agentapi.DetectionResult{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(native.Text, "<neuxbot-turn-context>") != 1 || strings.Contains(native.Text, "stable team role") {
			t.Fatalf("native continuation duplicated context or replayed stable instructions: %s", native.Text)
		}
		var selected []agentapi.Instruction
		for _, instruction := range turn.ContextInstructions {
			if instruction.Name != "Current workflow references" {
				continue
			}
			_, encoded, found := strings.Cut(instruction.Content, "\n")
			if !found || json.Unmarshal([]byte(encoded), &selected) != nil {
				t.Fatalf("authoritative reference list is not valid JSON: %s", instruction.Content)
			}
		}
		if len(selected) != len(references) || len(selected) > 0 && !reflect.DeepEqual(selected, references) {
			t.Fatalf("authoritative selection lost current references: got %+v, want %+v", selected, references)
		}
		for _, reference := range references {
			if strings.Count(native.Text, reference.Name) != 1 {
				t.Fatalf("native continuation duplicated or lost reference %s: %s", reference.Name, native.Text)
			}
			var content map[string]string
			if err := json.Unmarshal([]byte(reference.Content), &content); err != nil {
				t.Fatal(err)
			}
			for _, value := range content {
				if !strings.Contains(native.Text, value) {
					t.Fatalf("resume input lost exact reference %q: %s", value, native.Text)
				}
			}
		}
		if index > 0 && strings.Contains(native.Text, "original-revision") {
			t.Fatal("continuation replayed an earlier reference")
		}
		if index == 2 && (!strings.Contains(native.Text, "empty list") || !strings.Contains(native.Text, "[]") || strings.Contains(native.Text, "new-node")) {
			t.Fatalf("removed references were not explicitly cleared: %s", native.Text)
		}
	}
}

func TestWorkflowResultsReachNativeTurnOnceWithoutChangingSessionSettings(t *testing.T) {
	manager := agentPreparationFixture(t)
	journal := manager.journal.(*agentTestJournal)
	root := t.TempDir()
	files := &preparedMessageFiles{executionWorkspaceDirectory: root, taskDirectories: workspace.TaskDirectories{Outputs: filepath.Join(root, "outputs", "turn"), Temporary: filepath.Join(root, "tmp", "turn")}}
	state := messageChat{chat: journal.chat}
	var stableThread agentapi.ThreadRequest
	for index, contract := range []string{"actual attempt one result schema", "actual attempt two result schema"} {
		input := pendingMessage{message: userMessage{Text: "验证原始证据"}, runtime: runtimeSelection{RuntimeID: "codex"}, contextInstructions: []agentapi.Instruction{{Name: constants.WorkflowResultsInstructionFileName, Content: contract}}}
		plan := recordBusinessInputs(manager, state, input, files, nil, nil, models.RuntimeEnvironment{})

		turn := plan
		_, release, err := plan.prepare(manager, journal.chat, "turn", []ToolCapability{{Bindings: []capability.Binding{agentPreparationBinding(t, "workflow.results.submit")}}}, &[]string{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		release()
		turn.ContextInstructions = plan.ContextInstructions
		if index == 0 {
			stableThread = plan.thread()
		} else if !reflect.DeepEqual(plan.thread(), stableThread) {
			t.Fatal("current result contract changed stable session configuration")
		}
		prompt := turn.prompt()
		if turn.Message.Text != input.message.Text || strings.Count(prompt, contract) != 1 || strings.Contains(agentapi.DeveloperInstructions(plan.Instructions), contract) {
			t.Fatalf("result contract rewrote business input, was duplicated, or entered stable instructions: %s", prompt)
		}
	}
}
