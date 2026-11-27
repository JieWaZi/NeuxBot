package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/projectsource"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"go.opentelemetry.io/otel/trace"
)

func TestMainConversationLogsNativeFailureOnRequestTrace(t *testing.T) {
	const firstTrace = "00112233445566778899aabbccddeeff"
	const secondTrace = "ffeeddccbbaa99887766554433221100"
	requestContext := func(traceID string) context.Context {
		id, err := trace.TraceIDFromHex(traceID)
		if err != nil {
			t.Fatal(err)
		}
		return trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: id, SpanID: trace.SpanID{1}}))
	}
	ctx := requestContext(firstTrace)
	t.Setenv("NEUX_EXECUTION_FIXTURE_REFUSE_START", "1")
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeCore := fixture.open()
	defer closeCore()
	output, err := os.CreateTemp(t.TempDir(), "core-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	logger := ginxlogx.Instance()
	previous := logger.Out
	logger.SetOutput(output)
	defer logger.SetOutput(previous)

	chatID, executionID := util.New(), util.New()
	_, err = core.Start(ctx, WorkIntent{
		Chat:        ChatTarget{ChatID: chatID, MainConversationSource: &CreateChatSpec{WakerID: "native-waker", WorkingDirectory: t.TempDir()}},
		Input:       WorkInput{Actor: "user", Content: "private-input-fixture", SubmissionID: "first"},
		Options:     WorkOptions{RuntimeID: "codex", Model: "fixture-model", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto},
		ExecutionID: executionID, Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if !errors.Is(err, status_error.RuntimeSessionStartFailed) {
		t.Fatalf("unexpected admission result: %v", err)
	}
	t.Setenv("NEUX_EXECUTION_FIXTURE_REFUSE_START", "")
	ctx = requestContext(secondTrace)
	accepted, err := core.Start(ctx, WorkIntent{
		Chat: ChatTarget{ChatID: chatID}, Input: WorkInput{Actor: "user", Content: "provider-failure", SubmissionID: "second"},
		Options:     WorkOptions{RuntimeID: "codex", Model: "fixture-model", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto},
		ExecutionID: util.New(), Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, accepted, constants.ExecutionStatusFailed)
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	var adapter, preparation, settlement string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "trace_id="+firstTrace) && strings.Contains(line, "operation=session/new") {
			adapter = line
		}
		if strings.Contains(line, "trace_id="+firstTrace) && strings.Contains(line, "stage=prepare_thread") {
			preparation = line
		}
		if strings.Contains(line, "trace_id="+secondTrace) && strings.Contains(line, "native execution settlement failed") {
			settlement = line
		}
	}
	if !strings.Contains(adapter, "native_rpc_code=-32603") || !strings.Contains(adapter, "ACP agent did not verify restored history") ||
		!strings.Contains(preparation, "execution_id="+executionID) || !strings.Contains(preparation, "chat_id="+chatID) {
		t.Fatal("missing correlated native operation, protocol cause or host admission identifiers")
	}
	if !strings.Contains(settlement, "execution_id="+accepted.Execution.ID) || !strings.Contains(settlement, "failure_code="+string(constants.RuntimeFailureCodeExecutionFailed)) {
		t.Fatal("accepted native failure lost the originating request trace or failure classification")
	}
	for _, private := range []string{"private-input-fixture", "private-protocol-fixture"} {
		if strings.Contains(string(data), private) {
			t.Fatal("failure log leaked private data")
		}
	}
}

func TestMainConversationRecoversRefusedSessionWithoutRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	t.Setenv("NEUX_EXECUTION_FIXTURE_REFUSE_RESUME", "1")
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeCore := fixture.open()
	defer closeCore()
	chatID := util.New()
	intent := WorkIntent{
		Chat:        ChatTarget{ChatID: chatID, MainConversationSource: &CreateChatSpec{WakerID: "native-waker", WorkingDirectory: t.TempDir()}},
		Input:       WorkInput{Actor: "user", Content: "first assignment", SubmissionID: "first"},
		Options:     WorkOptions{RuntimeID: "codex", Model: "fixture-model", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto},
		ExecutionID: util.New(), Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	}
	first, err := core.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	// 仅回收空闲的 CLI 连接，保持 Core 与已持久化的工作会话运行。
	if err := core.(*service).threads.CloseThread(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	intent.Chat.MainConversationSource = nil
	intent.Input.Content, intent.Input.SubmissionID, intent.ExecutionID = "next assignment", "second", util.New()
	second, err := core.Start(ctx, intent)
	if err != nil {
		t.Fatalf("continue assignment after native resume rejection: %v", err)
	}
	waitNativeProductStatus(t, ctx, store, second, constants.ExecutionStatusCompleted)
	replayed, err := core.Start(ctx, intent)
	if err != nil || !replayed.Replayed || replayed.Execution.ID != second.Execution.ID || replayed.Chat.ID != chatID {
		t.Fatalf("original submission did not recover its receipt: %+v %v", replayed, err)
	}
	raw, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	trace := string(raw)
	if strings.Count(trace, `"method":"thread/resume"`) != 1 || strings.Count(trace, `"method":"thread/start"`) != 2 ||
		strings.Count(trace, `"method":"turn/start"`) != 2 {
		t.Fatal("expected one refused resume, one replacement session and exactly two user turns")
	}
	for _, line := range strings.Split(trace, "\n") {
		if strings.Contains(line, `"method":"turn/start"`) && strings.Contains(line, "next assignment") &&
			strings.Contains(line, "first assignment") && strings.Contains(line, "fixture reply") {
			return
		}
	}
	t.Fatal("replacement session did not receive the prior completed exchange with the new input")
}

func TestMainConversationSourceRunsTwoNativeTurnsInOneHiddenHistory(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := context.Background()
	id, directory := util.New(), t.TempDir()
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	intent := WorkIntent{
		Chat:        ChatTarget{ChatID: id, MainConversationSource: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: directory}},
		Input:       WorkInput{Actor: "user", Content: "first turn", SubmissionID: "first"},
		Options:     WorkOptions{RuntimeID: "codex", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto},
		ExecutionID: util.New(),
		Admission:   constants.ExecutionAdmissionCommitBeforeDispatch,
	}
	first, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, id)
	if !first.Chat.HiddenFromWakerHistory || !slices.Contains(first.Chat.AdditionalWorkingDirectories, canonical) {
		t.Fatalf("unexpected work scope: %#v", first.Chat)
	}
	intent.Input.Content, intent.Input.SubmissionID, intent.ExecutionID = "second turn", "second", util.New()
	intent.UserMessageID = util.New()
	intent.Chat.MainConversationSource = nil
	intent.Input.ReplyToMessageID = first.Execution.UserMessageID
	intent.Options.EntryPoint = MainConversationSourceEntryPoint
	staged := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(staged, []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	code := filepath.Join(first.Chat.WorkspaceDirectory, "sample.go")
	if err := os.WriteFile(code, []byte("package sample\n"), 0600); err != nil {
		t.Fatal(err)
	}
	intent.Input.CodeReferences = []CodeReferenceInput{{Path: code, StartLine: 1, EndLine: 1, Content: "package sample"}}
	intent.Input.Attachments = []AttachmentInput{{SourcePath: "upload:one/report.txt"}}
	claims := 0
	intent.ClaimAttachments = func(files []AttachmentInput) (func(bool), error) {
		claims++
		if files[0].SourcePath != "upload:one/report.txt" {
			t.Fatal("the source lost its original upload reference")
		}
		files[0].SourcePath = staged
		return func(accepted bool) {
			if accepted {
				if err := os.Remove(staged); err != nil {
					t.Error(err)
				}
			}
		}, nil
	}
	second, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, id)
	// 已被旧入口接纳的来源私聊重放时，不能补写公开用户输入。
	intent.AdmissionBoundary = func(context.Context, func(context.Context) error) error {
		t.Fatal("a historical source submission re-entered the public admission boundary")
		return nil
	}
	again, err := manager.execution.Start(ctx, intent)
	if err != nil || !again.Replayed || again.Execution.ID != second.Execution.ID || claims != 1 {
		t.Fatalf("consumed source upload was claimed again: %+v %d %v", again, claims, err)
	}
	if second.Chat.ID != first.Chat.ID || second.Execution.ID == first.Execution.ID {
		t.Fatal("the two turns did not share a single work history")
	}
	input, err := manager.chats.PublicReadingMessage(ctx, id, intent.UserMessageID)
	if err != nil || input == nil || input.ID != second.Execution.UserMessageID || input.Content != "second turn" ||
		len(input.Attachments) != 1 || input.Attachments[0].Name != "report.txt" || !input.Attachments[0].Managed || input.Attachments[0].Path == staged ||
		len(input.CodeReferences) != 1 || input.CodeReferences[0].Content != "package sample" || input.CodeReferences[0].StartLine != 1 {
		t.Fatalf("the exact source input lost its frozen file data: %+v, %v", input, err)
	}
	page, err := manager.chats.ReadMessageTimeline(ctx, id, 0, 0, 50)
	if err != nil || len(page.Resources) != 4 {
		t.Fatalf("private history has %d messages, want two user/assistant pairs: %v", len(page.Resources), err)
	}
	list, err := manager.chats.ListChats(ctx, chatservice.ListChatsInput{WakerID: "waker-1", Limit: 10})
	if err != nil || len(list.Resources) != 0 {
		t.Fatalf("source leaked into ordinary Chat history: %#v %v", list, err)
	}
}

// TestMainProjectScopeSurvivesProjectEdits 验证接纳时的目录和指令策略贯穿首次执行。
func TestMainProjectScopeSurvivesProjectEdits(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	core := manager.execution.(*service)
	ctx := t.Context()
	project := models.Project{ID: "project", WakerID: "waker-1", Name: "initial"}
	if err := manager.projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	oldRoot, err := projectsource.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	newRoot, err := projectsource.Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []models.ProjectSource{oldRoot, newRoot} {
		if err := manager.projectSources.Ensure(ctx, root); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.projects.ReplaceDirectories(ctx, project.ID, []models.ProjectDirectory{{Source: oldRoot, IsDefault: true, LoadInstructions: true}}); err != nil {
		t.Fatal(err)
	}
	capture := func() *MainProjectScope {
		t.Helper()
		scope, err := CaptureMainProjectScope(ctx, manager.projects, core.onboardingTransactions, project.WakerID, project.ID)
		if err != nil {
			t.Fatal(err)
		}
		return scope
	}
	if _, err := CaptureMainProjectScope(ctx, manager.projects, core.onboardingTransactions, "another-partner", project.ID); !util.HasStatusCode(err, status_error.ProjectNotFound) {
		t.Fatalf("granted another partner's project: %v", err)
	}
	original := capture()
	project.Name = "renamed"
	project.Brief = "new description"
	if err := manager.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	oldRoot.UpdatedAt++
	if err := manager.projectSources.Ensure(ctx, oldRoot); err != nil {
		t.Fatal(err)
	}
	if got := capture().ScopeKey(); got != original.ScopeKey() {
		t.Fatalf("project description or source confirmation changed the scope key: %s", got)
	}
	seen := map[string]bool{original.ScopeKey(): true}
	for _, changed := range [][]models.ProjectDirectory{
		{{Source: oldRoot, IsDefault: true}},
		{{Source: newRoot, IsDefault: true}},
		{{Source: oldRoot, IsDefault: true}, {Source: newRoot}},
		{{Source: newRoot}, {Source: oldRoot, IsDefault: true}},
		{{Source: newRoot, IsDefault: true}, {Source: oldRoot}},
	} {
		if err := manager.projects.ReplaceDirectories(ctx, project.ID, changed); err != nil {
			t.Fatal(err)
		}
		key := capture().ScopeKey()
		if seen[key] {
			t.Fatalf("different paths, order, default, or load policy reused an authorization: %+v", changed)
		}
		seen[key] = true
	}
	id := util.New()
	accepted, err := core.Start(ctx, WorkIntent{
		Chat:        ChatTarget{ChatID: id, MainConversationSource: &CreateChatSpec{WakerID: project.WakerID, ProjectID: project.ID, ProjectScope: original}},
		Input:       WorkInput{Actor: "user", Content: "check this project", SubmissionID: "frozen-project"},
		Options:     WorkOptions{RuntimeID: "codex", EntryPoint: MainConversationEntryPoint, PermissionLevel: constants.PermissionLevelAuto},
		ExecutionID: util.New(), Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, id)
	state, err := core.journal.LoadExecutionContext(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(accepted.Chat.AdditionalWorkingDirectories, []string{oldRoot.Path}) ||
		len(state.ProjectSources) != 1 || state.ProjectSources[0].SourcePath != oldRoot.Path || !state.ProjectSources[0].LoadInstructions {
		t.Fatalf("first turn used the edited project: %+v %+v", accepted.Chat.AdditionalWorkingDirectories, state.ProjectSources)
	}
}

func TestMainConversationSourceValidatesReservationUnderDeletionLock(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	core := manager.execution.(*service)
	chatID := util.New()
	root := core.storage.Root()
	var validated bool
	core.validateIntent = func(_ context.Context, intent WorkIntent) error {
		validated = true
		release, err := chatstorage.AcquireOperation(root, intent.Chat.ChatID)
		if release != nil {
			release()
		}
		if !errors.Is(err, status_error.ChatHasActiveExecution) {
			t.Errorf("source validation did not hold the deletion boundary: %v", err)
		}
		return status_error.AgentOperationForbidden
	}
	_, err := core.Start(t.Context(), WorkIntent{
		Chat:    ChatTarget{ChatID: chatID, MainConversationSource: &CreateChatSpec{WakerID: "waker-1"}},
		Options: WorkOptions{EntryPoint: MainConversationEntryPoint},
	})
	if !validated || !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("a deleted binding must reject dormant materialization: %t %v", validated, err)
	}
	if _, err := core.journal.LoadExecutionChat(t.Context(), chatID); !util.HasStatusCode(err, status_error.ChatNotFound) {
		t.Fatalf("unexpected dormant log or index: %v", err)
	}
}
