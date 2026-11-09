package execution

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestNativeProductCanonicalFork(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, chats, store, closeRuntime := fixture.open()
	defer func() { closeRuntime() }()
	attachment := filepath.Join(t.TempDir(), "fork-attachment.txt")
	if err := os.WriteFile(attachment, []byte("independent attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "canonical-fork-parent", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "first-success", Attachments: []AttachmentInput{{SourcePath: attachment}}}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	waitFiles := func(work AcceptedExecution) {
		t.Helper()
		for {
			summary, err := chats.GetMessageFileChanges(ctx, work.Chat.ID, work.Execution.ID)
			if err != nil {
				t.Fatal(err)
			}
			if summary.State != "pending" {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("file result did not settle")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	waitFiles(first)
	options.SessionPolicy = constants.RuntimeSessionPolicyNew
	parentLater, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "parent-after-boundary"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, parentLater, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, parentLater.Execution.ID); err != nil {
		t.Fatal(err)
	}
	waitFiles(parentLater)
	options.SessionPolicy = constants.RuntimeSessionPolicyContinue
	history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	message := history.Resources[1]
	if message.Fork == nil || !message.Fork.Available {
		t.Errorf("actual native terminal has no safe fork: %+v", message.Fork)
	}
	child, err := core.ForkChat(ctx, chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: first.Execution.ID, ClientRequestID: "canonical-fork"})
	if err != nil {
		t.Fatalf("Core.ForkChat from actual native turn: %v", err)
	}
	binding, err := store.ReadBinding(ctx, child)
	if err != nil || binding == nil || binding.Session.SessionID == "" {
		t.Fatalf("missing child binding: %+v %v", binding, err)
	}
	inherited, err := chats.ReadMessageHistory(ctx, child, 0, 20)
	if err != nil || len(inherited.Resources) != 2 || len(inherited.Resources[0].Attachments) != 1 {
		t.Fatalf("exact fork did not preserve only selected history: %+v %v", inherited, err)
	}
	inheritedID := inherited.Resources[1].ID
	summary, err := chats.GetMessageFileChanges(ctx, child, inheritedID)
	if err != nil || summary.TotalFiles != 1 {
		t.Fatalf("inherited actual file result: %+v %v", summary, err)
	}
	childRoot, err := core.(*service).storage.ChatRoot(child)
	if err != nil {
		t.Fatal(err)
	}
	if err := chats.DeleteChat(ctx, first.Chat.ID); err != nil {
		t.Fatal(err)
	}
	attachmentBody, err := os.ReadFile(filepath.Join(childRoot, inherited.Resources[0].Attachments[0].Path))
	if err != nil || string(attachmentBody) != "independent attachment" {
		t.Fatalf("child attachment depends on deleted source: %q %v", attachmentBody, err)
	}
	diff, err := chats.GetMessageFileChange(ctx, child, inheritedID, summary.Files[0].AbsolutePath)
	if err != nil || diff.Comparison == nil {
		t.Fatalf("child result depends on deleted source: %+v %v", diff, err)
	}
	loaded, err := chats.LoadExecutionChat(ctx, child)
	if err != nil || loaded.LastRuntimeID != "codex" || loaded.LastModel != "fixture-model" || loaded.LastReasoning != "high" || loaded.LastPermissionLevel != constants.PermissionLevelAuto || loaded.LastFast == nil || !*loaded.LastFast {
		t.Fatalf("fork lost selected execution configuration: %+v %v", loaded, err)
	}
	childSession := binding.Session
	verify := func(chatID, text string) AcceptedExecution {
		t.Helper()
		work, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: chatID}, Input: WorkInput{Actor: "user", Content: text}, Options: options})
		if err != nil {
			t.Fatal(err)
		}
		waitNativeProductStatus(t, ctx, store, work, constants.ExecutionStatusCompleted)
		if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
			t.Fatal(err)
		}
		input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: chatID, HostInvocationID: work.Execution.ID})
		if err != nil || consumed == nil || input.Session != childSession || consumed.Result.Session != childSession {
			t.Fatalf("child did not resume its native fork: input=%+v consumption=%+v err=%v", input, consumed, err)
		}
		return work
	}
	verify(child, "child-first-input")
	closeRuntime()
	core, chats, store, closeRuntime = fixture.open()
	second := verify(child, "child-after-restart")
	grandchild, err := core.ForkChat(ctx, chatservice.ForkInput{ChatID: child, ExecutionID: second.Execution.ID, ClientRequestID: "canonical-refork"})
	if err != nil || grandchild == child || grandchild == "" {
		t.Fatalf("refork after restart: %s %v", grandchild, err)
	}
	trace, err := os.ReadFile(fixture.trace)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(trace)
	for _, text := range []string{"parent-after-boundary", "child-first-input", "child-after-restart"} {
		if strings.Count(wire, text) != 1 {
			t.Fatalf("input %q replayed or missing:\n%s", text, wire)
		}
	}
	for _, line := range strings.Split(wire, "\n") {
		if (strings.Contains(line, "child-first-input") || strings.Contains(line, "child-after-restart")) && (strings.Contains(line, "first-success") || strings.Contains(line, "Recent conversation history")) {
			t.Fatalf("fork child replayed parent history: %s", line)
		}
	}
	if strings.Count(wire, `"method":"thread/fork"`) != 2 || strings.Count(wire, `"method":"turn/start"`) != 4 || strings.Count(wire, `"method":"thread/start"`) != 2 {
		t.Fatalf("fork submitted a prompt or rebuilt history:\n%s", wire)
	}
	if !strings.Contains(wire, `"method":"thread/resume"`) || !strings.Contains(wire, `"lastTurnId":"fixture-turn-`) {
		t.Fatalf("missing exact fork/resume protocol:\n%s", wire)
	}
}

func TestNativeProductForkNegotiatesLatestAndUnsupported(t *testing.T) {
	for _, provider := range []string{"kimi", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			t.Setenv("NEUX_EXECUTION_FIXTURE_PROVIDER", provider)
			if provider == "kimi" {
				t.Setenv("NEUX_EXECUTION_FIXTURE_FORK", "latest")
			}
			fixture := newNativeProductFixture(t, ctx, provider, "TestNativeExecutionACPChild", constants.PermissionLevelDefault)
			core, chats, store, closeRuntime := fixture.open()
			defer closeRuntime()
			options := WorkOptions{RuntimeID: provider, Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelDefault, SessionPolicy: constants.RuntimeSessionPolicyContinue}
			first, err := core.Start(ctx, WorkIntent{ClientRequestID: "fork-capability", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: acpFirstQuestion}, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
			if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
				t.Fatal(err)
			}
			options.Reasoning = "low"
			second, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: acpSecondQuestion}, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			waitNativeProductStatus(t, ctx, store, second, constants.ExecutionStatusCompleted)
			if err := core.CancelExecutionAndWait(ctx, second.Chat.ID, second.Execution.ID); err != nil {
				t.Fatal(err)
			}
			history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range history.Resources {
				if message.ExecutionID == "" {
					continue
				}
				want := provider == "kimi" && message.ExecutionID == second.Execution.ID
				if message.Fork == nil || message.Fork.Available != want {
					t.Errorf("negotiated %s fork for %s: %+v", provider, message.ExecutionID, message.Fork)
				}
			}
			if _, err := core.ForkChat(ctx, chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: first.Execution.ID, ClientRequestID: "reject-old"}); err == nil {
				t.Fatal("fork accepted unsupported or nonlatest boundary")
			}
			child, err := core.ForkChat(ctx, chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: second.Execution.ID, ClientRequestID: "latest-request"})
			if provider == "gemini" {
				if err == nil {
					t.Fatal("unsupported CLI accepted fork")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			binding, err := store.ReadBinding(ctx, child)
			if err != nil || binding == nil {
				t.Fatal(binding, err)
			}
			next, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: child}, Input: WorkInput{Actor: "user", Content: acpResumeQuestion}, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			waitNativeProductStatus(t, ctx, store, next, constants.ExecutionStatusCompleted)
			if err := core.CancelExecutionAndWait(ctx, child, next.Execution.ID); err != nil {
				t.Fatal(err)
			}
			input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: child, HostInvocationID: next.Execution.ID})
			if err != nil || consumed == nil || input.Session != binding.Session {
				t.Fatalf("latest fork not resumed: %+v %+v %v", input, consumed, err)
			}
			wire, err := os.ReadFile(fixture.trace)
			if err != nil || strings.Count(string(wire), `"method":"session/fork"`) != 1 || strings.Count(string(wire), `"method":"session/prompt"`) != 3 {
				t.Fatalf("latest fork wire: %s %v", wire, err)
			}
		})
	}
}

func TestNativeProductForkColdFailureRetriesBeforeSideEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "cold-fork-parent", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "cold-fork-question"}, Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	input := chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: first.Execution.ID, ClientRequestID: "cold-fork-retry"}
	fixture.failAccountAcquire.Store(2)
	if _, err := core.ForkChat(ctx, input); err == nil {
		t.Fatal("expected actual cold account acquisition failure")
	}
	operation, exists, err := chatstorage.ReadForkOperation(core.(*service).storage.ForkDirectory(input.ChatID, input.ClientRequestID))
	if err != nil || !exists || operation.NativeStarted {
		t.Fatalf("cold acquisition crossed native side-effect boundary: exists=%v nativeStarted=%v err=%v", exists, operation.NativeStarted, err)
	}
	fixture.failAccountAcquire.Store(0)
	child, err := core.ForkChat(ctx, input)
	if err != nil || child == "" {
		t.Fatalf("same request did not recover after cold acquisition repair: %s %v", child, err)
	}
	body, err := os.ReadFile(fixture.trace)
	if err != nil || strings.Count(string(body), `"method":"thread/fork"`) != 1 || strings.Count(string(body), `"method":"turn/start"`) != 1 {
		t.Fatalf("cold failure retry duplicated native work: %s %v", body, err)
	}
}

func TestNativeProductForkReconcilesReceiptAfterCleanupFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "cleanup-fork-parent", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "cleanup-fork-question"}, Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	input := chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: first.Execution.ID, ClientRequestID: "cleanup-fork-retry"}
	fixture.failAccountClose.Store(2)
	if _, err := core.ForkChat(ctx, input); err == nil {
		t.Fatal("expected actual post-fork lease cleanup failure")
	}
	operation, exists, err := chatstorage.ReadForkOperation(core.(*service).storage.ForkDirectory(input.ChatID, input.ClientRequestID))
	if err != nil || !exists || !operation.NativeStarted || operation.SessionID != "" {
		t.Fatalf("unpublished native result lost original receipt address: %+v %v", operation, err)
	}
	fixture.failAccountClose.Store(0)
	child, err := core.ForkChat(ctx, input)
	if err != nil || child == "" {
		t.Fatalf("same receipt did not reconcile after cleanup repair: %s %v", child, err)
	}
	body, err := os.ReadFile(fixture.trace)
	if err != nil || strings.Count(string(body), `"method":"thread/fork"`) != 1 || strings.Count(string(body), `"method":"turn/start"`) != 1 {
		t.Fatalf("receipt reconciliation duplicated native work: %s %v", body, err)
	}
}

func TestNativeProductForkUnknownResponseOnlyReconciles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "unknown-fork-parent", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "unknown-fork-question"}, Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	input := chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: first.Execution.ID, ClientRequestID: "unknown-fork-retry"}
	t.Setenv("NEUX_EXECUTION_FIXTURE_FORK_UNCERTAIN", "1")
	timeout, stop := context.WithTimeout(ctx, time.Second)
	_, err = core.ForkChat(timeout, input)
	stop()
	if err == nil {
		t.Fatal("unconfirmed native fork became success")
	}
	t.Setenv("NEUX_EXECUTION_FIXTURE_FORK_UNCERTAIN", "")
	if _, err := core.ForkChat(ctx, input); err == nil {
		t.Fatal("unknown receipt was retried as a fresh native fork")
	}
	body, err := os.ReadFile(fixture.trace)
	if err != nil || strings.Count(string(body), `"method":"thread/fork"`) != 1 || strings.Count(string(body), `"method":"turn/start"`) != 1 {
		t.Fatalf("unknown native response duplicated work: %s %v", body, err)
	}
}

func TestNativeProductForkRejectsSameSubjectDifferentHistoryHome(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	t.Setenv("NEUX_EXECUTION_FIXTURE_ACCOUNT_HOME", t.TempDir())
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	core, _, store, closeRuntime := fixture.open()
	defer closeRuntime()
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "home-fork-parent", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, Input: WorkInput{Actor: "user", Content: "home-fork-question"}, Options: WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	prior, err := store.ReadBinding(ctx, first.Chat.ID)
	if err != nil || prior == nil {
		t.Fatal(prior, err)
	}
	t.Setenv("NEUX_EXECUTION_FIXTURE_ACCOUNT_HOME", t.TempDir())
	input := chatservice.ForkInput{ChatID: first.Chat.ID, ExecutionID: first.Execution.ID, ClientRequestID: "home-fork-retry"}
	if _, err := core.ForkChat(ctx, input); err == nil {
		t.Errorf("same-subject fork crossed a different native history root: subject=%s", prior.Session.Subject)
	}
	operation, exists, err := chatstorage.ReadForkOperation(core.(*service).storage.ForkDirectory(input.ChatID, input.ClientRequestID))
	if err != nil || !exists || operation.NativeStarted {
		t.Errorf("history mismatch crossed native side-effect boundary: exists=%v started=%v err=%v", exists, operation.NativeStarted, err)
	}
	body, err := os.ReadFile(fixture.trace)
	if err != nil || strings.Contains(string(body), `"method":"thread/fork"`) {
		t.Fatalf("history mismatch called native fork: %s %v", body, err)
	}
}
