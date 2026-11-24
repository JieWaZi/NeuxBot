package execution

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWorkflowReferencesReachNativeResumePromptWithoutReloadingInstructions(t *testing.T) {
	manager := agentPreparationFixture(t)
	journal := manager.journal.(*agentTestJournal)
	manager.agentBindings = func(models.Chat, string, []string, string, string) []capability.Binding {
		return []capability.Binding{agentPreparationBinding(t, "workflow.source.save")}
	}
	root := t.TempDir()
	state := messageChat{chat: journal.chat}
	files := &preparedMessageFiles{executionWorkspaceDirectory: root, instructions: []agentapi.Instruction{{Name: "PARTNER.md", Content: "persistent partner"}}}
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
		context, err := json.Marshal(native.Context)
		if err != nil || native.Text != input.message.Text || strings.Contains(string(context), "stable team role") {
			t.Fatalf("native continuation rewrote user text or replayed stable instructions: %q %s %v", native.Text, context, err)
		}
		var selected []struct {
			// Name 是引用的稳定语义。
			Name string `json:"name"`
			// Value 是原生输入使用的结构化引用。
			Value json.RawMessage `json:"value"`
		}
		for _, instruction := range turn.ContextInstructions {
			if instruction.Name != "Current workflow references" {
				continue
			}
			if json.Unmarshal([]byte(instruction.Content), &selected) != nil {
				t.Fatalf("authoritative reference list is not valid JSON: %s", instruction.Content)
			}
		}
		if len(selected) != len(references) {
			t.Fatalf("authoritative selection lost current references: got %+v, want %+v", selected, references)
		}
		for i, reference := range references {
			if selected[i].Name != reference.Name || string(selected[i].Value) != reference.Content {
				t.Fatalf("reference %d lost its structured value: %+v", i, selected[i])
			}
			if strings.Count(string(context), reference.Name) != 1 {
				t.Fatalf("native continuation duplicated or lost reference %s: %s", reference.Name, context)
			}
			var content map[string]string
			if err := json.Unmarshal([]byte(reference.Content), &content); err != nil {
				t.Fatal(err)
			}
			for _, value := range content {
				if !strings.Contains(string(context), value) {
					t.Fatalf("resume input lost exact reference %q: %s", value, context)
				}
			}
		}
		if index > 0 && strings.Contains(string(context), "original-revision") {
			t.Fatal("continuation replayed an earlier reference")
		}
		if strings.Contains(string(context), "Never infer the selected node") || !strings.Contains(*plan.thread().Session.DeveloperInstructions, "Never infer the selected node") {
			t.Fatal("reference handling rules were not delivered through the native instruction port")
		}
		if index == 2 && len(native.Context) != 0 {
			t.Fatalf("turn without references must omit empty context: %s", context)
		}
	}
}

func TestWorkflowResultsReachNativeTurnOnceWithoutChangingSessionSettings(t *testing.T) {
	manager := agentPreparationFixture(t)
	journal := manager.journal.(*agentTestJournal)
	root := t.TempDir()
	files := &preparedMessageFiles{executionWorkspaceDirectory: root}
	state := messageChat{chat: journal.chat}
	var stableThread agentapi.ThreadRequest
	for index, contract := range []string{`{"type":"object"}`, `{"type":"array"}`} {
		input := pendingMessage{
			message: userMessage{Text: "验证原始证据"}, runtime: runtimeSelection{RuntimeID: "codex"},
			contextInstructions: []agentapi.Instruction{{Name: constants.WorkflowResultsInstructionFileName, Content: agentapi.WorkflowResultInstructions + contract + agentapi.WorkflowResultCompletion}},
		}
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
		if turn.Message.Text != input.message.Text ||
			!reflect.DeepEqual(turn.ContextInstructions, []agentapi.Instruction{{Name: constants.WorkflowResultsInstructionFileName, Content: contract}}) ||
			strings.Contains(prompt, "Final chat text is not a result submission") ||
			!strings.Contains(agentapi.DeveloperInstructions(plan.Instructions), "Final chat text is not a result submission") {
			t.Fatalf("result contract rewrote business input, was duplicated, or entered stable instructions: %s", prompt)
		}
	}
}
