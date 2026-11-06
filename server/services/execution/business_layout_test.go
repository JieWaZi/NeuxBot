package execution

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestTurnPathsAndContextDoNotChangeStaticSessionPlan(t *testing.T) {
	root := t.TempDir()
	manager := &service{}
	state := messageChat{chat: models.Chat{WakerID: "partner", WorkspaceDirectory: root, AdditionalWorkingDirectories: []string{root}}}
	files := &preparedMessageFiles{executionWorkspaceDirectory: root, instructions: []agentapi.Instruction{{Name: "PARTNER.md", Content: "persistent partner"}}, taskDirectories: workspace.TaskDirectories{Outputs: filepath.Join(root, "outputs", "execution-one"), Temporary: filepath.Join(root, "tmp", "execution-one")}}
	request := pendingMessage{supplementalInstructions: []agentapi.Instruction{{Name: "TEAM.md", Content: "stable team role"}}, contextInstructions: []agentapi.Instruction{{Name: "Current Lead Turn", Content: "coordination"}}}
	firstPlan, firstTurn := manager.buildSessionPlan(state, request, files, nil, nil, models.RuntimeEnvironment{})
	files.taskDirectories = workspace.TaskDirectories{Outputs: filepath.Join(root, "outputs", "execution-two"), Temporary: filepath.Join(root, "tmp", "execution-two")}
	request.contextInstructions[0].Content = "direct execution"
	secondPlan, secondTurn := manager.buildSessionPlan(state, request, files, nil, nil, models.RuntimeEnvironment{})
	if !reflect.DeepEqual(firstPlan, secondPlan) {
		t.Fatal("turn data changed stable session configuration")
	}
	if reflect.DeepEqual(firstTurn.ContextInstructions, secondTurn.ContextInstructions) {
		t.Fatal("current task context was not delivered independently")
	}
	static, _ := json.Marshal(firstPlan)
	if strings.Contains(string(static), "execution-one") || strings.Contains(string(static), "Current Lead Turn") {
		t.Fatalf("dynamic context entered session fingerprint: %s", static)
	}
	if !strings.Contains(nativeTurnPrompt(secondTurn), "execution-two") || !strings.Contains(nativeTurnPrompt(secondTurn), "direct execution") {
		t.Fatal("native turn lost exact task paths or current mode")
	}
	if !strings.Contains(agentapi.DeveloperInstructions(secondPlan), "stable team role") {
		t.Fatal("stable team role was removed from developer instructions")
	}
}

func TestCapabilityFileRootsFreezeTaskOutputsAndRealProjects(t *testing.T) {
	manager := agentPreparationFixture(t)
	chat := manager.journal.(*agentTestJournal).chat
	cwd, project := t.TempDir(), t.TempDir()
	task, err := workspace.EnsureTaskDirectories(cwd, "turn")
	if err != nil {
		t.Fatal(err)
	}
	chat.WorkspaceDirectory = cwd
	chat.AdditionalWorkingDirectories = []string{cwd, project}
	var roots []string
	manager.agentBindings = func(_ models.Chat, _ string, frozen []string, _ string) []capability.Binding {
		roots = append([]string(nil), frozen...)
		return nil
	}
	plan := agentapi.SessionPlan{Target: agentapi.SessionTarget{RuntimeID: "codex"}, Workspace: agentapi.Workspace{Path: cwd, ProjectDirectories: []agentapi.WorkspaceProjectDirectory{{Path: project}}}}
	_, release, err := manager.prepareAgent(&plan, &agentapi.TurnInput{}, chat, "turn", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	actualOutputs, _ := filepath.EvalSymlinks(task.Outputs)
	actualProject, _ := filepath.EvalSymlinks(project)
	if !reflect.DeepEqual(roots, []string{actualOutputs, actualProject}) {
		t.Fatalf("delivery roots contain private/shared cwd or lost real project: %v", roots)
	}
}

func TestWakerDeletionGateBlocksNewAndExistingChatPreparation(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := context.Background()
	first, err := manager.sendTestMessage(ctx, testMessageInput{ClientRequestID: "gate-initial", NewChat: &testChatInput{WakerID: "waker-1"}, Message: userMessage{Text: "initial"}, Runtime: runtimeSelection{RuntimeID: agentapi.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	core := manager.execution.(*service)
	release, err := chatstorage.AcquireOperation(core.storage.Root(), "waker:waker-1")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, target := range []ChatTarget{{Create: &CreateChatSpec{WakerID: "waker-1"}}, {ChatID: first.Chat.ID}} {
		_, err := core.Start(ctx, WorkIntent{ClientRequestID: "gate-new", Chat: target, Input: WorkInput{Actor: "user", Content: "while deleting"}, Options: WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}})
		if !errors.Is(err, status_error.ChatHasActiveExecution) {
			t.Fatalf("preparation escaped Waker deletion: %+v %v", target, err)
		}
	}
	if calls := manager.nativeCalls(t, "turn/start"); len(calls) != 1 {
		t.Fatalf("rejected preparation submitted native work: %s", calls)
	}
}

func TestProjectEntryDeliveryUsesNativeEvidenceAndIsolatesSharedWakerChats(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	core := manager.execution.(*service)
	core.runtimeSummaries = append(core.runtimeSummaries, agentapi.RuntimeSummary{ID: agentapi.RuntimeIDCursor, BaseRuntimeID: agentapi.RuntimeIDCursor, InstructionFileName: "AGENTS.md"})
	root, err := core.storage.AgentWorkspace("waker-1")
	if err != nil {
		t.Fatal(err)
	}
	projectOne, projectTwo := t.TempDir(), t.TempDir()
	sourceFile := filepath.Join(projectOne, "main.txt")
	if err := os.WriteFile(sourceFile, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	prepare := func(chatID, project string, evidence agentapi.CapabilitySet) (*preparedMessageFiles, messageChat) {
		t.Helper()
		state := messageChat{chat: models.Chat{ID: chatID, WakerID: "waker-1", WorkspaceDirectory: root, AdditionalWorkingDirectories: []string{project}}, workspaceDirectory: root}
		if project != "" {
			state.projectSources = []eventlog.ProjectSourceSnapshot{{EntryName: "same-name", SourcePath: project}}
		} else {
			state.chat.AdditionalWorkingDirectories = []string{root}
		}
		detection := agentapi.DetectionResult{Runtime: agentapi.RuntimeSummary{ID: agentapi.RuntimeIDCursor, BaseRuntimeID: agentapi.RuntimeIDCursor}, Capabilities: evidence}
		var references []CodeReferenceInput
		if project == projectOne {
			references = []CodeReferenceInput{{Path: sourceFile}}
		}
		files, err := core.prepareMessageFiles(t.Context(), state, "message-"+chatID, "execution-"+chatID, nil, references, agentapi.RuntimeIDCursor, detection)
		if err != nil {
			t.Fatal(err)
		}
		return files, state
	}
	native, nativeState := prepare("native-chat", projectOne, agentapi.CapabilitySet{agentapi.CapabilityAddDirectory: true})
	defer native.rollback()
	if _, err := os.Lstat(filepath.Join(root, "projects")); !os.IsNotExist(err) {
		t.Fatalf("native delivery created links: %v", err)
	}
	nativePlan, _ := core.buildSessionPlan(nativeState, pendingMessage{}, native, nil, nil, models.RuntimeEnvironment{Detection: agentapi.DetectionResult{Capabilities: agentapi.CapabilitySet{agentapi.CapabilityAddDirectory: true}}})
	if got := nativeThreadRequest("native-chat", nativePlan, pendingMessage{}, models.RuntimeEnvironment{}, agentapi.ResourceSelection{}, "stable").Session.AdditionalDirectories; !reflect.DeepEqual(got, []string{projectOne}) {
		t.Fatalf("native roots lost: %v", got)
	}
	empty, _ := prepare("empty-chat", "", agentapi.CapabilitySet{agentapi.CapabilityAddDirectory: false})
	defer empty.rollback()
	if _, err := os.Lstat(filepath.Join(root, "projects")); !os.IsNotExist(err) {
		t.Fatalf("no-directory Chat created links: %v", err)
	}
	first, firstState := prepare("chat-one", projectOne, agentapi.CapabilitySet{agentapi.CapabilityAddDirectory: false})
	defer first.rollback()
	second, _ := prepare("chat-two", projectTwo, agentapi.CapabilitySet{agentapi.CapabilityAddDirectory: false})
	defer second.rollback()
	for chat, source := range map[string]string{"chat-one": projectOne, "chat-two": projectTwo} {
		linked, err := filepath.EvalSymlinks(filepath.Join(root, "projects", chat, "same-name"))
		if err != nil {
			t.Fatal(err)
		}
		actual, err := filepath.EvalSymlinks(source)
		if err != nil || linked != actual {
			t.Fatalf("Chat %s entry replaced another project: %s %v", chat, linked, err)
		}
	}
	if _, _, err := workspace.PrepareProjectEntries(root, "chat-one", []workspace.ProjectLink{{EntryName: "same-name", SourcePath: projectTwo}}); err == nil {
		t.Fatal("existing Chat entry was silently replaced")
	}
	stillFirst, err := filepath.EvalSymlinks(filepath.Join(root, "projects", "chat-one", "same-name"))
	expectedFirst, _ := filepath.EvalSymlinks(projectOne)
	if err != nil || stillFirst != expectedFirst {
		t.Fatalf("rejected replacement changed first Chat: %s %v", stillFirst, err)
	}
	plan, input := core.buildSessionPlan(firstState, pendingMessage{}, first, nil, nil, models.RuntimeEnvironment{Detection: agentapi.DetectionResult{Capabilities: agentapi.CapabilitySet{agentapi.CapabilityAddDirectory: false}}})
	thread := nativeThreadRequest("chat-one", plan, pendingMessage{}, models.RuntimeEnvironment{}, agentapi.ResourceSelection{}, "stable")
	if thread.Connection.Account.CWD != root || len(thread.Session.AdditionalDirectories) != 0 {
		t.Fatalf("fallback changed cwd or sent unsupported roots: %+v", thread)
	}
	actualFile, err := filepath.EvalSymlinks(sourceFile)
	if err != nil || len(first.codeReferences) != 1 || first.codeReferences[0].Path != actualFile || len(input.Message.CodeReferences) != 1 || input.Message.CodeReferences[0].Path != "projects/chat-one/same-name/main.txt" {
		t.Fatalf("native projection changed durable file identity: durable=%+v native=%+v err=%v", first.codeReferences, input.Message.CodeReferences, err)
	}
	if !strings.Contains(nativeTurnPrompt(input), "projects/chat-one/same-name") || len(plan.Workspace.ProjectDirectories) != 1 || plan.Workspace.ProjectDirectories[0].Path != projectOne {
		t.Fatal("projection lost actual project identity or native discovery entry")
	}
	unknown, _ := prepare("unknown-chat", projectOne, nil)
	defer unknown.rollback()
	if _, err := os.Lstat(filepath.Join(root, "projects", "unknown-chat")); !os.IsNotExist(err) {
		t.Fatalf("unknown capability pretended unsupported: %v", err)
	}
}
