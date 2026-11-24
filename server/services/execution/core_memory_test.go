package execution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/memory"
	managedskills "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

type offlineOptionalSkills struct {
	controllers.SkillController
}

func (offlineOptionalSkills) ListEnabled(context.Context, string) ([]models.SkillWithState, error) {
	return nil, status_error.DatabaseError
}

func TestWorkbenchSendsAndContinuesWithOptionalResourcesUnavailable(t *testing.T) {
	ctx := t.Context()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()

	service := core.(*service)
	service.skillController = offlineOptionalSkills{}
	service.coreMemory = func(ctx context.Context, _ string) (memory.CoreSnapshot, error) {
		<-ctx.Done()
		return memory.CoreSnapshot{}, ctx.Err()
	}
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto, EntryPoint: WorkbenchEntryPoint}
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "optional-resources-down",
		Chat:  ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input: WorkInput{Actor: "user", Content: "send with an unavailable skill", SkillIDs: []string{"previously-selected-skill"}}, Options: options,
	})
	if err != nil {
		t.Fatalf("optional resources blocked the first send: %v", err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	followup, err := core.Start(ctx, WorkIntent{
		Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "continue after optional lookup timed out"}, Options: options,
	})
	if err != nil {
		t.Fatalf("optional resources blocked continuation: %v", err)
	}
	waitNativeProductStatus(t, ctx, store, followup, constants.ExecutionStatusCompleted)
	trace, err := os.ReadFile(fixture.trace)
	if err != nil || strings.Count(string(trace), `"method":"thread/start"`) != 1 {
		t.Fatalf("continuation did not keep its native session: %v", err)
	}
}

func TestCoreMemoryInjectionUsesTrustedWorkbenchOrigin(t *testing.T) {
	ctx := t.Context()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, chats, store, closeRuntime := fixture.open()
	defer closeRuntime()

	memories, err := memory.NewService(memory.ServiceConfig{DBPath: filepath.Join(t.TempDir(), "memory.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer memories.Close()
	entry, err := memories.Write(ctx, memory.Context{PartnerID: "native-waker"}, "private-memory-language-guidance")
	if err != nil {
		t.Fatal(err)
	}
	if err := memories.SetCore(ctx, "native-waker", entry.Memory.ID, true); err != nil {
		t.Fatal(err)
	}
	core.(*service).coreMemory = memories.CoreSnapshot
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto, EntryPoint: WorkbenchEntryPoint}
	intent := WorkIntent{
		ClientRequestID: "workbench-core-check",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker", WorkingDirectory: t.TempDir()}},
		Input:           WorkInput{Actor: "user", Content: "workbench-origin-test-message"},
		Options:         options,
	}
	work, err := core.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, work, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
		t.Fatal(err)
	}
	history, err := chats.ReadMessageHistory(ctx, work.Chat.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundOriginal := false
	for _, message := range history.Resources {
		foundOriginal = foundOriginal || message.Content == intent.Input.Content
		if strings.Contains(message.Content, "private-memory-language-guidance") {
			t.Fatal("public history includes the core snapshot")
		}
	}
	if !foundOriginal {
		t.Fatal("public user input was rewritten")
	}
	records, err := core.(*service).journal.ReadExecutionRecords(work.Chat.ID, work.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	var workbenchObserved bool
	for _, record := range records {
		if record.Type == constants.ChatEventTypeExecutionCreated {
			var data eventlog.ExecutionCreatedData
			workbenchObserved = json.Unmarshal(record.Data, &data) == nil && data.EntryPoint == WorkbenchEntryPoint
		}
	}
	if !workbenchObserved {
		t.Fatal("canonical execution facts omitted workbench origin")
	}

	childID, err := core.ForkChat(ctx, chatservice.ForkInput{ChatID: work.Chat.ID, ExecutionID: work.Execution.ID, ClientRequestID: "fork-workbench-core"})
	if err != nil {
		t.Fatalf("workbench fork: %v", err)
	}
	forked, err := core.Start(ctx, WorkIntent{
		Chat: ChatTarget{ChatID: childID}, Input: WorkInput{Actor: "user", Content: "forked-workbench-test-message"}, Options: options,
	})
	if err != nil {
		t.Fatalf("workbench fork follow-up: %v", err)
	}
	waitNativeProductStatus(t, ctx, store, forked, constants.ExecutionStatusCompleted)

	// The IM bridge uses the same user actor, including on its visible partner conversations.
	im, err := core.Start(ctx, WorkIntent{
		ClientRequestID: "external-core-check",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input:           WorkInput{Actor: "user", Content: "external-origin-test-message", IMReplyLinkID: "published-link"},
		Options:         options,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, im, constants.ExecutionStatusCompleted)
	wire, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	workbenchPrompt, forkedPrompt, imPrompt := "", "", ""
	for _, line := range strings.Split(string(wire), "\n") {
		if !strings.Contains(line, `"method":"turn/start"`) {
			continue
		}
		if strings.Contains(line, intent.Input.Content) {
			workbenchPrompt = line
		}
		if strings.Contains(line, "forked-workbench-test-message") {
			forkedPrompt = line
		}
		if strings.Contains(line, "external-origin-test-message") {
			imPrompt = line
		}
	}
	if !strings.Contains(workbenchPrompt, "private-memory-language-guidance") {
		t.Fatal("ordinary workbench prompt with a working directory omitted its core memory")
	}
	if !strings.Contains(forkedPrompt, "private-memory-language-guidance") {
		t.Fatal("first workbench turn on the fork omitted its core snapshot")
	}
	if imPrompt == "" || strings.Contains(imPrompt, "private-memory-language-guidance") {
		t.Fatal("unexpected native prompt for IM-origin work", imPrompt)
	}
}

func TestProfileOptimizationContinuationDoesNotUsePartnerReviewContext(t *testing.T) {
	ctx := t.Context()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	manager, tools := core.(*service), agentPreparationFixture(t)
	manager.agent, manager.agentEndpoint, manager.systemSkillDirectory = tools.agent, tools.agentEndpoint, tools.systemSkillDirectory
	manager.resourceManagementBindings = func(models.Chat, string, func(context.Context, string) error) []capability.Binding {
		return []capability.Binding{agentPreparationBinding(t, "skills.packages.revise"), agentPreparationBinding(t, "mcp.connectors.update")}
	}
	manager.coreMemory = func(context.Context, string) (memory.CoreSnapshot, error) {
		return memory.CoreSnapshot{Revision: 1, JSON: []byte(`{"items":[{"id":"1","text":"private-profile-memory-marker"}]}`)}, nil
	}
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto, EntryPoint: WorkbenchEntryPoint}
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "profile-optimization-context",
		Chat:  ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker", WorkingDirectory: t.TempDir(), ProfileOptimization: true}},
		Input: WorkInput{Actor: "user", Content: "polish-profile-start", ContextInstructions: []workinput.Instruction{{Name: agentapi.ProfileEditContextName, Content: agentapi.ProfileEditAdmission}}}, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	followup, err := core.Start(ctx, WorkIntent{
		Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "polish-profile-continue"}, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, followup, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, followup.Chat.ID, followup.Execution.ID); err != nil {
		t.Fatal(err)
	}
	forkID, err := core.ForkChat(ctx, chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: followup.Execution.ID, ClientRequestID: "fork-profile-optimization"})
	if err != nil {
		t.Fatal(err)
	}
	forked, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: forkID}, Input: WorkInput{Actor: "user", Content: "polish-profile-fork"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, forked, constants.ExecutionStatusCompleted)
	wire, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "private-profile-memory-marker") {
		t.Error("profile optimization received private memory")
	}
	if strings.Contains(string(wire), "NeuxBot resource management") {
		t.Error("profile optimization received general resource management guidance")
	}
	for _, turn := range []AcceptedExecution{first, followup, forked} {
		records, err := manager.journal.ReadExecutionRecords(turn.Chat.ID, turn.Execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range records {
			if record.Type != constants.ChatEventTypeExecutionCreated {
				continue
			}
			var data eventlog.ExecutionCreatedData
			if err := json.Unmarshal(record.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data.EntryPoint == WorkbenchEntryPoint {
				t.Errorf("profile optimization turn %s became eligible for workbench review", turn.Execution.ID)
			}
		}
	}
}

// optionalSkillCatalogFixture 替代本地技能目录查询，投影和原生执行沿用真实链路。
type optionalSkillCatalogFixture struct {
	// SkillController 提供测试路径无需调用的其余目录操作。
	controllers.SkillController
	// skills 是本次成功查询返回的伙伴安装快照。
	skills []models.SkillWithState
	// err 模拟本地目录暂时不可用。
	err error
}

func (f *optionalSkillCatalogFixture) ListEnabled(context.Context, string) ([]models.SkillWithState, error) {
	return f.skills, f.err
}

func TestWorkbenchSkillCatalogErrorKeepsProjectionUntilConfirmedEmpty(t *testing.T) {
	ctx := t.Context()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	manager := core.(*service)
	skillParent := t.TempDir()
	origin := filepath.Join(skillParent, "published-skill")
	if err := os.Mkdir(origin, 0700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: example-skill\ndescription: Test a verified skill.\n---\nPreviously verified skill content.\n"
	if err := os.WriteFile(filepath.Join(origin, "SKILL.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := managedskills.Digest(origin)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &optionalSkillCatalogFixture{skills: []models.SkillWithState{{CurrentVersion: models.SkillVersion{
		ID: "skill-version", Name: "example-skill", ContentPath: filepath.Base(origin), ContentDigest: digest,
	}}}}
	manager.skillController = catalog
	manager.skillDataDirectory = skillParent
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", PermissionLevel: constants.PermissionLevelAuto, EntryPoint: WorkbenchEntryPoint}
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "installed-skill-projection",
		Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "install the active projection"}, Options: options,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	projected := filepath.Join(first.Chat.WorkspaceDirectory, ".agents", "skills", "example-skill", "SKILL.md")
	checkProjected := func() {
		t.Helper()
		data, err := os.ReadFile(projected)
		if err != nil || string(data) != content {
			t.Errorf("verified skill projection missing or changed: %q %v", data, err)
		}
	}
	checkProjected()
	catalog.skills, catalog.err = nil, status_error.DatabaseError
	send := func(text string) {
		t.Helper()
		turn, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: text}, Options: options})
		if err != nil {
			t.Fatal(err)
		}
		waitNativeProductStatus(t, ctx, store, turn, constants.ExecutionStatusCompleted)
	}
	send("continue when catalogue is unavailable")
	checkProjected()
	if launches := fixture.accounts.Load(); launches != 1 {
		t.Errorf("catalogue failure replaced the native runtime: %d launches", launches)
	}
	catalog.err = nil
	send("continue after the skill has been disabled")
	if _, err := os.Lstat(projected); !os.IsNotExist(err) {
		t.Fatalf("confirmed empty catalogue kept a disabled skill: %v", err)
	}
}
