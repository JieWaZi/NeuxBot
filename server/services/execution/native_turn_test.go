package execution

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/changes"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	eventlognext "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog_next"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	usageservice "github.com/JieWaZi/NeuxBot/server/services/usage"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestNativeTurnPromptPreservesBusinessReferences(t *testing.T) {
	turn := nativePromptFixture()
	text := nativeTurnPrompt(turn)
	for _, want := range []string{"review this", "report.csv", "artifacts/report.csv", "main.go", "return 42", "search-guide", "docs-mcp"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt lost %q: %s", want, text)
		}
	}
	if strings.Contains(text, "previous answer") {
		t.Fatalf("consumer seeded history independently: %s", text)
	}
}

func TestNativeTurnResultUsesProviderTerminalEvidence(t *testing.T) {
	result := agent.TurnResult{BusinessAdmitted: true, Admission: "accepted", Submission: "sent", OperationError: context.Canceled}
	outcome := nativeExecutionOutcome(result)
	if string(outcome.status) != "canceled" {
		t.Fatalf("canceled native wait projected as %s", outcome.status)
	}
}

func nativePromptFixture() agentapi.TurnInput {
	return agentapi.TurnInput{
		Message: agentapi.Message{
			Text:           "review this",
			Attachments:    []agentapi.Attachment{{Name: "report.csv", Path: "artifacts/report.csv"}},
			CodeReferences: []agentapi.CodeReference{{Path: "main.go", StartLine: 2, EndLine: 2, Content: "return 42"}},
		},
		History:        []agentapi.HistoryMessage{{Role: "assistant", Content: "previous answer"}},
		SelectedSkills: []string{"search-guide"}, SelectedConnectors: []string{"docs-mcp"},
	}
}

type nativeProductFixture struct {
	// root 是 Core 重启时复用的存储根目录。
	root string
	// trace 是原生子进程接收的请求日志。
	trace string
	// accounts 记录原生连接获取账号租约的次数。
	accounts *atomic.Int32
	// usage 提供从原生终态重建用量查询的 SQLite 投影。
	usage *controllers.UsageController
	// open 重新装配独立的 Core、Thread、Provider 和 ConversationStore。
	open func() (Core, chatservice.Module, *eventlognext.Store, func())
}

func newNativeProductFixture(t *testing.T, ctx context.Context, providerID, child string, permission constants.PermissionLevel) nativeProductFixture {
	t.Helper()
	root, trace := t.TempDir(), filepath.Join(t.TempDir(), providerID+"-trace.jsonl")
	t.Setenv("NEUX_EXECUTION_FIXTURE_TRACE", trace)
	t.Setenv("PWD", filepath.Join(root, "first-host-directory"))
	t.Setenv("NEUX_EXECUTION_FIXTURE_BASE_VALUE", "inherited-first")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "execution.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	db := database.DB
	sql, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sql.Close() })
	wakers := controllers.NewWakerController(db)
	profiles := controllers.NewProfileDocumentController(db)
	projects := controllers.NewProjectController(db)
	if err := wakers.Create(ctx, models.Waker{ID: "native-waker", Name: providerID + " fixture"}); err != nil {
		t.Fatal(err)
	}
	var documents []models.WakerDocument
	for i, kind := range []constants.ProfileDocumentKind{constants.ProfileDocumentKindIdentity, constants.ProfileDocumentKindPersona, constants.ProfileDocumentKindPlaybook, constants.ProfileDocumentKindMemory} {
		documents = append(documents, models.WakerDocument{ID: fmt.Sprintf("fixture-document-%d", i), WakerID: "native-waker", Kind: kind, Content: "Fixture instructions."})
	}
	if err := profiles.CreateWakerDocuments(ctx, documents); err != nil {
		t.Fatal(err)
	}
	summary := agentapi.RuntimeSummary{ID: providerID, BaseRuntimeID: providerID, InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"}
	switch providerID {
	case "claude":
		summary.InstructionFileName, summary.SkillDirectoryName = "CLAUDE.md", ".claude"
	case "cursor":
		summary.SkillDirectoryName = ".cursor"
	}
	model := agentapi.ModelOption{ID: "fixture-model", PermissionLevels: []constants.PermissionLevel{permission}}
	if providerID != "codex" {
		model.ReasoningOptions = []agentapi.ReasoningOption{{ID: "high"}, {ID: "low"}}
	}
	fastOn := true
	environment := nativeFixtureEnvironment{entry: models.RuntimeEnvironment{
		DefaultFast: &fastOn,
		RuntimeID:   providerID, BaseRuntimeID: providerID, Enabled: true, Binary: executable, PrefixArgs: []string{"-test.run=^" + child + "$", "--"},
		Detection: agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: summary, ResolvedBinary: executable,
			Models: []agentapi.ModelOption{model}},
	}}
	accounts := new(atomic.Int32)
	open := func() (Core, chatservice.Module, *eventlognext.Store, func()) {
		t.Helper()
		store, err := eventlognext.NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{
			AcquireAccount: func(_ context.Context, account agent.AccountRequest) (agent.AccountLease, error) {
				if account.Provider != providerID || account.Subject != "fixture-subject" {
					return agent.AccountLease{}, status_error.AIAccountConfigurationInvalid
				}
				accounts.Add(1)
				return agent.AccountLease{}, nil
			},
		})
		threads := agent.NewThreadLifecycle(provider, store)
		chats, err := chatservice.New(chatservice.Config{Conversations: store, StorageDirectory: root, Projects: projects,
			QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(db), RuntimeSessions: controllers.NewRuntimeSessionController(db),
				Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db)), DeletionIntents: controllers.NewChatDeletionIntentController(db)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := chats.Start(ctx); err != nil {
			t.Fatal(err)
		}
		core, err := New(Config{Threads: threads, ResolveAccount: func(_ context.Context, request agent.AccountRequest, _ string) (agent.AccountRequest, error) {
			request.Subject = "fixture-subject"
			return request, nil
		}, Journal: chats, DormantChats: chats, Wakers: wakers, Profiles: profiles, Projects: projects, RuntimeSummaries: []agentapi.RuntimeSummary{summary},
			RuntimeEnvironments: environment, SkillDataDirectory: root, StorageDirectory: root})
		if err != nil {
			t.Fatal(err)
		}
		if err := core.Open(ctx); err != nil {
			t.Fatal(err)
		}
		return core, chats, store, func() {
			core.Close()
			if err := threads.Close(context.Background()); err != nil {
				t.Error(err)
			}
			if err := provider.Close(context.Background()); err != nil {
				t.Error(err)
			}
			chats.Close()
			store.Close()
		}
	}
	return nativeProductFixture{root: root, trace: trace, accounts: accounts, usage: controllers.NewUsageController(db), open: open}
}

func TestNativeProductCodexCompletesStreamsCancelsAndRestarts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, "codex", "TestNativeExecutionCodexChild", constants.PermissionLevelAuto)
	root, trace, accounts, open := fixture.root, fixture.trace, fixture.accounts, fixture.open
	fastOff := false
	core, chats, store, closeRuntime := open()
	defer func() { closeRuntime() }()
	options := WorkOptions{RuntimeID: "codex", Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}
	first, err := core.Start(ctx, WorkIntent{ClientRequestID: "codex-native-first", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}},
		Input: WorkInput{Actor: "user", Content: "first-success"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if first.Execution.Status != constants.ExecutionStatusRunning || first.Execution.Fast == nil || !*first.Execution.Fast {
		t.Fatalf("start = %+v", first.Execution)
	}
	waitNativeProductStatus(t, ctx, store, first, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, first.Execution.ID); err != nil {
		t.Fatal(err)
	}
	outcome, err := chats.ReadExecutionOutcome(ctx, first.Chat.ID, first.Execution.ID)
	if err != nil || outcome.InvocationID == "" || outcome.TerminationReason != constants.RuntimeTerminationReasonNormal {
		t.Fatalf("public native outcome: %+v %v", outcome, err)
	}
	fileChanges, err := chats.GetMessageFileChanges(ctx, first.Chat.ID, first.Execution.ID)
	if err != nil || fileChanges.State != "ready" || !slices.ContainsFunc(fileChanges.Files, func(file changes.File) bool {
		return file.Path == "fixture-change.txt"
	}) {
		t.Fatalf("file capture lost an actual workspace write: %+v %v", fileChanges, err)
	}

	input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: first.Chat.ID, HostInvocationID: first.Execution.ID})
	if err != nil || input.Session.SessionID == "" || consumed == nil {
		t.Fatalf("durable admission/consumption: input=%+v consumption=%+v err=%v", input, consumed, err)
	}
	result, err := store.ReadNativeResult(ctx, consumed.Result)
	if err != nil || result.Turn.Usage == nil || result.Turn.Usage.InputTokens == nil || *result.Turn.Usage.InputTokens != 15 ||
		result.Turn.Usage.Complete || result.Turn.Usage.Coverage != "last_request" {
		t.Fatalf("canonical partial usage = %+v, err = %v", result.Turn.Usage, err)
	}
	history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
	if err != nil || len(history.Resources) != 2 || history.Resources[1].Content != "fixture reply" ||
		history.Resources[1].RunMetrics == nil || history.Resources[1].RunMetrics.StartedAt == 0 || history.Resources[1].RunMetrics.InputTokens != 0 {
		t.Fatalf("Chat history: %+v %v", history, err)
	}

	options.Model, options.Reasoning, options.Fast = "fixture-other", "medium", &fastOff
	held, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "hold-for-stream"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStream(t, ctx, chats, held, "fixture partial")
	if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, held.Execution.ID); err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, held, constants.ExecutionStatusCanceled)
	heldInput, _, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: held.Chat.ID, HostInvocationID: held.Execution.ID})
	if err != nil || heldInput.Session != input.Session || held.Execution.Fast == nil || *held.Execution.Fast {
		t.Fatalf("model/Fast edit replaced the thread or lost explicit false: %+v %+v %v", heldInput, held.Execution, err)
	}

	failed, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "provider-failure"}, Options: options, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, failed, constants.ExecutionStatusFailed)
	if got := accounts.Load(); got != 1 {
		t.Fatalf("compatible hot sends took %d native account leases", got)
	}

	closeRuntime()
	// A restarted host has a different inherited snapshot and the same managed launch.
	t.Setenv("PWD", filepath.Join(root, "second-host-directory"))
	t.Setenv("NEUX_EXECUTION_FIXTURE_BASE_VALUE", "inherited-second")
	core, chats, store, closeRuntime = open()
	reloaded, err := chats.LoadExecutionChat(ctx, first.Chat.ID)
	if err != nil || reloaded.LastFast == nil || *reloaded.LastFast {
		t.Fatalf("Chat restart lost explicit Fast=false: %+v %v", reloaded.LastFast, err)
	}
	resumed, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "cold-resume-current-only"}, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, resumed, constants.ExecutionStatusCompleted)
	if err := core.CancelExecutionAndWait(ctx, resumed.Chat.ID, resumed.Execution.ID); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "child inherited inherited-second") {
		t.Fatal("restarted process lost its inherited environment")
	}
	if !strings.Contains(string(body), `"method":"thread/resume"`) || accounts.Load() != 2 {
		t.Fatalf("native trace did not resume once: %s", body)
	}
	traceLines := strings.Split(string(body), "\n")
	if !slices.ContainsFunc(traceLines, func(line string) bool {
		return strings.Contains(line, "first-success") && strings.Contains(line, `"effort":"high"`) && strings.Contains(line, `"serviceTier":"fixture-priority"`)
	}) || !slices.ContainsFunc(traceLines, func(line string) bool {
		return strings.Contains(line, "cold-resume-current-only") && strings.Contains(line, `"model":"fixture-other"`) && strings.Contains(line, `"serviceTier":null`)
	}) {
		t.Fatalf("requested model, effort or Fast did not reach the actual turn/start frame: %s", body)
	}
	for _, line := range traceLines {
		if strings.Contains(line, "cold-resume-current-only") && strings.Contains(line, "first-success") {
			t.Fatalf("resume replayed previous input: %s", line)
		}
	}
	rotationOptions := options
	rotationOptions.SessionPolicy = constants.RuntimeSessionPolicyNew
	rotationOptions.HistoryMode = constants.HistoryModeSummary
	rotated, err := core.Start(ctx, WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID},
		Input: WorkInput{Actor: "user", Content: "rotate-with-summary"}, Options: rotationOptions})
	if err != nil {
		t.Fatal(err)
	}
	waitNativeProductStatus(t, ctx, store, rotated, constants.ExecutionStatusCompleted)
	binding, err := store.ReadBinding(ctx, first.Chat.ID)
	if err != nil || binding == nil || binding.Session.SessionID == input.Session.SessionID {
		t.Fatalf("explicit replacement did not replace the native session: %+v %v", binding, err)
	}
	body, err = os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	hiddenDelivered := slices.ContainsFunc(strings.Split(string(body), "\n"), func(line string) bool {
		return strings.Contains(line, "rotate-with-summary") && strings.Contains(line, "private fixture handoff")
	})
	if !hiddenDelivered {
		t.Fatal("replacement did not receive the old native session's private summary")
	}
	history, err = chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 30)
	if err != nil || slices.ContainsFunc(history.Resources, func(message messageview.Message) bool {
		return strings.Contains(message.Content, "private fixture handoff")
	}) {
		t.Fatalf("private summary entered public history: %+v %v", history, err)
	}

}

func TestNativeProductClaudeAndCursorContinueAndRecover(t *testing.T) {
	for _, provider := range []string{"claude", "cursor"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			permission := constants.PermissionLevelAuto
			if provider == "cursor" {
				permission = constants.PermissionLevelFullAccess
			}
			t.Setenv("NEUX_EXECUTION_FIXTURE_PROVIDER", provider)
			fixture := newNativeProductFixture(t, ctx, provider, "TestNativeExecutionClaudeCursorChild", permission)
			core, chats, store, closeRuntime := fixture.open()
			defer func() { closeRuntime() }()
			fastOff := false
			options := WorkOptions{RuntimeID: provider, Model: "fixture-model", Reasoning: "high", PermissionLevel: permission, SessionPolicy: constants.RuntimeSessionPolicyContinue}
			start := func(target ChatTarget, text string) AcceptedExecution {
				t.Helper()
				intent := WorkIntent{Chat: target, Input: WorkInput{Actor: "user", Content: text}, Options: options}
				if target.Create != nil {
					intent.ClientRequestID = "native-create-" + provider
				}
				work, err := core.Start(ctx, intent)
				if err != nil {
					trace, _ := os.ReadFile(fixture.trace)
					t.Fatalf("start %q: %v\n%s", text, err, trace)
				}
				return work
			}
			verify := func(work AcceptedExecution, want constants.ExecutionStatus) agent.NativeResultKey {
				t.Helper()
				waitNativeProductStatus(t, ctx, store, work, want)
				if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
					t.Fatal(err)
				}
				input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: work.Chat.ID, HostInvocationID: work.Execution.ID})
				if err != nil || consumed == nil || input.Session.SessionID == "" {
					t.Fatalf("durable consumption: %+v %+v %v", input, consumed, err)
				}
				result, err := store.ReadNativeResultMetadata(ctx, consumed.Result)
				if err != nil || result.Attribution == nil || result.Attribution.Source != "chat" ||
					result.Attribution.WakerID != "native-waker" || result.Attribution.ChatID != work.Chat.ID || result.Attribution.AccountID != agent.SystemAccountID {
					t.Fatalf("native terminal attribution: %+v %v", result, err)
				}
				nativeStatus := acpmeta.NativeTurnCompleted
				if want == constants.ExecutionStatusCanceled {
					nativeStatus = acpmeta.NativeTurnCancelled
				}
				if result.Turn.Status != nativeStatus || !slices.Equal(result.Turn.ConsumedInputIDs, []string{input.AdapterInputID}) {
					t.Fatalf("native status or exact input receipt: %+v", result.Turn)
				}
				reported := result.Turn.Usage
				if provider == "claude" && (reported == nil || reported.InputTokens == nil || *reported.InputTokens != 43) {
					t.Fatalf("Claude lost provider-reported usage: %+v", reported)
				}
				if provider == "cursor" && reported != nil {
					t.Fatalf("Cursor invented usage for a response without counters: %+v", reported)
				}
				return consumed.Result
			}
			create := ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}
			readOnly := options
			readOnly.WorkspaceReadOnly = true
			_, err := core.Start(ctx, WorkIntent{ClientRequestID: "native-reject-read-only", Chat: create,
				Input: WorkInput{Actor: "user", Content: "do-not-dispatch-read-only"}, Options: readOnly})
			if !errors.Is(err, status_error.RuntimePolicyUnsupported) || fixture.accounts.Load() != 0 {
				t.Fatalf("unsupported read-only policy reached native preparation: err=%v leases=%d", err, fixture.accounts.Load())
			}
			first := start(create, "first-question")
			if first.Execution.Fast == nil || !*first.Execution.Fast {
				t.Fatalf("model-supported default Fast was not applied: %+v", first.Execution.Fast)
			}
			firstResult := verify(first, constants.ExecutionStatusCompleted)
			target := ChatTarget{ChatID: first.Chat.ID}
			options.Fast, options.Reasoning = &fastOff, "low"
			second := start(target, "second-question")
			if second.Execution.Fast == nil || *second.Execution.Fast {
				t.Fatalf("explicit Fast off was not preserved: %+v", second.Execution.Fast)
			}
			secondResult := verify(second, constants.ExecutionStatusCompleted)
			if secondResult.Session != firstResult.Session || secondResult == firstResult || fixture.accounts.Load() != 1 {
				t.Fatalf("compatible turns did not reuse one session with distinct native invocations: %+v %+v leases=%d", firstResult, secondResult, fixture.accounts.Load())
			}
			history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
			if err != nil || len(history.Resources) != 4 || history.Resources[1].Content != "native fixture answer" ||
				history.Resources[2].Content != "second-question" || history.Resources[3].Content != "native fixture answer" {
				t.Fatalf("public history: %+v %v", history, err)
			}

			held := start(target, "hold-for-native-cancel")
			waitNativeProductStream(t, ctx, chats, held, "native fixture partial")
			if err := core.CancelExecutionAndWait(ctx, first.Chat.ID, held.Execution.ID); err != nil {
				t.Fatal(err)
			}
			verify(held, constants.ExecutionStatusCanceled)

			closeRuntime()
			core, chats, store, closeRuntime = fixture.open()
			loaded, err := chats.LoadExecutionChat(ctx, first.Chat.ID)
			if err != nil || loaded.LastFast == nil || *loaded.LastFast {
				t.Fatalf("reloaded settings: %+v %v", loaded.LastFast, err)
			}
			metrics, err := usageservice.NewService(store, fixture.usage).Summary(ctx, usageservice.Filter{
				From: time.Now().Add(-time.Minute).UnixMilli(), To: time.Now().Add(time.Minute).UnixMilli(),
				Timezone: "UTC", ChatID: first.Chat.ID, Source: "chat", AccountID: agent.SystemAccountID,
			}, "")
			if err != nil || metrics.Metrics.Invocations != 3 {
				t.Fatalf("reopened usage should count each native call once: %+v %v", metrics, err)
			}
			if provider == "claude" {
				input := metrics.Metrics.KnownTokens.Input
				if input == nil || *input != 129 || metrics.Metrics.MissingUsageInvocations != 0 {
					t.Fatalf("reopened Claude usage: %+v", metrics)
				}
			} else if metrics.Metrics.KnownTokens.Input != nil || metrics.Metrics.MissingUsageInvocations != 3 {
				t.Fatalf("reopened Cursor calls with unknown usage: %+v", metrics)
			}

			resumed := start(target, "current-after-restart")
			if result := verify(resumed, constants.ExecutionStatusCompleted); result.Session != firstResult.Session || fixture.accounts.Load() != 2 {
				t.Fatalf("restarted Core did not recover the original native session: %+v leases=%d", result, fixture.accounts.Load())
			}
			assertNativeProviderTrace(t, provider, fixture.trace, firstResult.Session.SessionID)
			if provider == "claude" {
				// The native model setter downgrades Auto when the target model lacks it.
				// This must be detected before admitting another prompt.
				options.Model, options.Reasoning, options.Fast = "fixture-default", "", nil
				for attempt := 1; attempt <= 2; attempt++ {
					_, err := core.Start(ctx, WorkIntent{Chat: target, Options: options,
						Input: WorkInput{Actor: "user", Content: "reject-auto-on-unsupported-model"}})
					trace, readErr := os.ReadFile(fixture.trace)
					if err == nil || readErr != nil || !strings.Contains(string(trace), `"subtype":"set_model","model":"fixture-default"`) ||
						strings.Contains(string(trace), "reject-auto-on-unsupported-model") {
						t.Fatalf("Claude submitted a prompt after losing Auto mode on attempt %d: err=%v traceErr=%v\n%s", attempt, err, readErr, trace)
					}
				}
			}
		})
	}
}

func assertNativeProviderTrace(t *testing.T, provider, trace, sessionID string) {
	t.Helper()
	body, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(body)
	requireOrder := func(parts ...string) {
		t.Helper()
		rest := wire
		for _, part := range parts {
			_, next, found := strings.Cut(rest, part)
			if !found {
				t.Fatalf("missing %q in native sequence %q:\n%s", part, parts, wire)
			}
			rest = next
		}
	}
	if provider == "claude" {
		requireOrder(`"subtype":"set_model","model":"fixture-model"`, `"effortLevel":"high"`, `"fastMode":true`,
			`"subtype":"set_permission_mode","mode":"auto"`, "first-question", `"effortLevel":"low"`, `"fastMode":false`, "second-question",
			"hold-for-native-cancel", `"subtype":"interrupt"`, "--resume="+sessionID, `"subtype":"set_permission_mode","mode":"auto"`, "current-after-restart")
	} else {
		requireOrder("launch --force acp", `"configId":"model","sessionId":"`+sessionID+`","value":"fixture-model"`, `"value":"high"`, `"value":"true"`,
			"first-question", `"value":"low"`, `"value":"false"`, "second-question", "hold-for-native-cancel", `"method":"session/cancel"`,
			"launch --force acp", `"method":"session/resume"`, "current-after-restart")
	}
	for _, line := range strings.Split(wire, "\n") {
		if strings.Contains(line, "second-question") || strings.Contains(line, "current-after-restart") {
			if strings.Contains(line, "first-question") || strings.Contains(line, "Recent conversation history") || strings.Contains(line, "native fixture answer") {
				t.Fatalf("follow-up prompt replayed previous turns: %s", line)
			}
		}
	}
	for _, question := range []string{"first-question", "second-question", "hold-for-native-cancel", "current-after-restart"} {
		if strings.Count(wire, question) != 1 {
			t.Fatalf("prompt %q should reach the native process once: %s", question, wire)
		}
	}
}

func waitNativeProductStream(t *testing.T, ctx context.Context, chats chatservice.Module, held AcceptedExecution, text string) {
	t.Helper()
	subscription, err := chats.SubscribeEvents(ctx, held.Chat.ID, held.Chat.LatestSequence)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatal("SSE closed before native checkpoint")
			}
			if event.Message != nil && strings.Contains(event.Message.Content, text) {
				return
			}
		case <-ctx.Done():
			t.Fatal("no native body reached the public subscription")
		}
	}
}

type nativeFixtureEnvironment struct {
	// entry 是固定到本地测试程序的智能体环境。
	entry models.RuntimeEnvironment
}

func (r nativeFixtureEnvironment) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	return r.entry, nil
}

func waitNativeProductStatus(t *testing.T, ctx context.Context, store *eventlognext.Store, accepted AcceptedExecution, want constants.ExecutionStatus) {
	t.Helper()
	for {
		rows, err := store.ReadTurn(ctx, accepted.Chat.ID, accepted.Execution.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			fact := row.Change.Fact
			if fact == nil {
				continue
			}
			switch fact.Type {
			case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionCanceled, constants.ChatEventTypeExecutionFailed:
				var finished eventlog.ExecutionFinishedData
				if err := json.Unmarshal(fact.Data, &finished); err != nil || finished.Status != want {
					t.Fatalf("terminal = %+v, want %s, err = %v", finished, want, err)
				}
				if len(finished.Usage) != 0 {
					t.Fatalf("duplicated native usage in business terminal: %s", finished.Usage)
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no durable %s terminal for %s", want, accepted.Execution.ID)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestNativeExecutionCodexChild(t *testing.T) {
	trace := os.Getenv("NEUX_EXECUTION_FIXTURE_TRACE")
	if trace == "" {
		return
	}
	file, err := os.OpenFile(trace, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	defer file.Close()
	_, _ = fmt.Fprintln(file, "child inherited", os.Getenv("NEUX_EXECUTION_FIXTURE_BASE_VALUE"))
	scanner, out := bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout)
	thread, turnID := "", ""
	serial := 0
	emit := func(method string, params any) {
		_ = out.Encode(map[string]any{"method": method, "params": params})
	}
	turn := func(status string) map[string]any {
		return map[string]any{"threadId": thread, "turn": map[string]any{"id": turnID, "status": status}}
	}
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(file, line)
		var frame map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			os.Exit(2)
		}
		ack := func(result any) {
			_ = out.Encode(map[string]any{"id": frame["id"], "result": result})
		}
		switch frame["method"] {
		case "initialize":
			ack(map[string]any{"userAgent": "codex/0.163.0"})
		case "account/read":
			ack(map[string]any{"account": nil, "requiresOpenaiAuth": false})
		case "model/list":
			ack(map[string]any{"data": []any{
				map[string]any{"id": "fixture-model", "model": "fixture-model", "displayName": "Fixture", "isDefault": true,
					"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "high"}},
					"serviceTiers":              []any{map[string]any{"id": "fixture-priority", "name": "Fast"}}},
				map[string]any{"id": "fixture-other", "model": "fixture-other", "displayName": "Other",
					"supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "medium"}},
					"serviceTiers":              []any{map[string]any{"id": "other-priority", "name": "Fast"}}},
			}})
		case "config/read":
			ack(map[string]any{"config": map[string]any{}})
		case "thread/start", "thread/resume":
			params, _ := frame["params"].(map[string]any)
			thread, _ = params["threadId"].(string)
			if thread == "" {
				thread = fmt.Sprintf("fixture-thread-%d", os.Getpid())
			}
			ack(map[string]any{"thread": map[string]any{"id": thread}, "model": "fixture-model"})
		case "turn/start":
			if strings.Contains(line, "first-success") {
				params, _ := frame["params"].(map[string]any)
				cwd, _ := params["cwd"].(string)
				if cwd == "" || os.WriteFile(filepath.Join(cwd, "fixture-change.txt"), []byte("native write"), 0600) != nil {
					os.Exit(3)
				}
			}
			serial++
			turnID = fmt.Sprintf("fixture-turn-%d-%d", os.Getpid(), serial)
			ack(turn("inProgress"))
			emit("turn/started", turn("inProgress"))
			tokens := map[string]any{"inputTokens": 19, "outputTokens": 7, "cachedInputTokens": 4, "reasoningOutputTokens": 2, "totalTokens": 26}
			emit("thread/tokenUsage/updated", map[string]any{"threadId": thread, "turnId": turnID,
				"tokenUsage": map[string]any{"last": tokens, "total": tokens, "modelContextWindow": 100}})
			if strings.Contains(line, "provider-failure") {
				emit("turn/completed", turn("failed"))
				continue
			}
			reply := "fixture reply"
			switch {
			case strings.Contains(line, "hold-for-stream"):
				reply = "fixture partial"
			case strings.Contains(line, "Prepare a concise private handoff"):
				reply = "private fixture handoff"
			}
			emit("item/agentMessage/delta", map[string]any{"threadId": thread, "turnId": turnID, "itemId": turnID + "-message", "delta": reply})
			if strings.Contains(line, "hold-for-stream") {
				continue
			}
			emit("item/completed", map[string]any{"threadId": thread, "turnId": turnID,
				"item": map[string]any{"id": turnID + "-message", "type": "agentMessage", "phase": "final_answer", "text": reply}})
			emit("turn/completed", turn("completed"))
		case "turn/interrupt":
			ack(map[string]any{})
			emit("turn/completed", turn("interrupted"))
		}
	}
	os.Exit(0)
}

func TestNativeExecutionClaudeCursorChild(t *testing.T) {
	provider := os.Getenv("NEUX_EXECUTION_FIXTURE_PROVIDER")
	if provider == "" {
		return
	}
	if slices.Contains(os.Args, "--version") {
		if provider == "claude" {
			fmt.Println("2.1.309 (Claude Code)")
		} else {
			fmt.Println("2026.10.22-27dcf5f")
		}
		os.Exit(0)
	}
	trace, err := os.OpenFile(os.Getenv("NEUX_EXECUTION_FIXTURE_TRACE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	fmt.Fprintln(trace, "launch", strings.Join(os.Args[3:], " "))
	scanner, out := bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout)
	read := func() (map[string]any, string, bool) {
		if !scanner.Scan() {
			return nil, "", false
		}
		line := scanner.Text()
		fmt.Fprintln(trace, line)
		var frame map[string]any
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(2)
		}
		return frame, line, true
	}
	if provider == "claude" {
		serveNativeExecutionClaude(out, read)
	} else {
		serveNativeExecutionCursor(out, read)
	}
	trace.Close()
	os.Exit(0)
}

func serveNativeExecutionClaude(out *json.Encoder, read func() (map[string]any, string, bool)) {
	cwd, _ := os.Getwd()
	var session, active string
	for _, arg := range os.Args {
		for _, prefix := range []string{"--session-id=", "--resume="} {
			if id, ok := strings.CutPrefix(arg, prefix); ok {
				session = id
			}
		}
	}
	model := "fixture-default"
	var effort any
	fast := false
	serial := 0
	usage := map[string]any{"input_tokens": 43, "output_tokens": 7, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}
	emit := func(body map[string]any) {
		body["session_id"] = session
		_ = out.Encode(body)
	}
	finish := func(text, reason string) {
		emit(map[string]any{"type": "result", "subtype": "success", "uuid": "fixture-result-" + active,
			"result_index": serial, "user_message_uuid": active, "terminal_reason": reason,
			"result": text, "is_error": false, "usage": usage})
		active = ""
		serial++
	}
	for {
		frame, line, ok := read()
		if !ok {
			return
		}
		switch frame["type"] {
		case "control_request":
			request := frame["request"].(map[string]any)
			reply := map[string]any{}
			switch request["subtype"] {
			case "initialize":
				emit(map[string]any{"type": "system", "subtype": "init", "cwd": cwd, "model": model,
					"permissionMode": "default", "claude_code_version": "2.1.309"})
				reply["models"] = []any{
					map[string]any{"value": "fixture-default", "displayName": "Default"},
					map[string]any{"value": "fixture-model", "displayName": "Fixture", "supportsAutoMode": true,
						"supportsEffort": true, "supportedEffortLevels": []string{"high", "low"}, "supportsFastMode": true},
				}
			case "get_settings":
				reply["applied"] = map[string]any{"model": model, "effort": effort}
				reply["effective"] = map[string]any{"fastMode": fast}
			case "set_model":
				model = request["model"].(string)
			case "apply_flag_settings":
				settings := request["settings"].(map[string]any)
				if value, exists := settings["effortLevel"]; exists {
					effort = value
				}
				if value, exists := settings["fastMode"]; exists {
					fast = value.(bool)
				}
			case "set_permission_mode":
			case "get_context_usage":
				reply["totalTokens"], reply["rawMaxTokens"] = 50, 1000
			case "interrupt":
				finish("", "aborted_streaming")
			default:
				os.Exit(3)
			}
			_ = out.Encode(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": frame["request_id"], "response": reply,
			}})
		case "user":
			active = frame["uuid"].(string)
			held := strings.Contains(line, "hold-for-native-cancel")
			reply := "native fixture answer"
			if held {
				reply = "native fixture partial"
			}
			emit(map[string]any{"type": "assistant", "user_message_uuid": active, "parent_tool_use_id": nil,
				"message": map[string]any{"id": "fixture-message-" + active, "model": model, "usage": usage,
					"content": []any{map[string]any{"type": "text", "text": reply}}}})
			if !held {
				finish(reply, "completed")
			}
		}
	}
}

func serveNativeExecutionCursor(out *json.Encoder, read func() (map[string]any, string, bool)) {
	session, model, effort, fast := "cursor-fixture", "fixture-default", "low", "false"
	var held any
	selectOption := func(id, category, current string, choices ...string) map[string]any {
		options := make([]map[string]string, 0, len(choices))
		for _, value := range choices {
			options = append(options, map[string]string{"value": value, "name": value})
		}
		return map[string]any{"id": id, "name": id, "type": "select", "category": category, "currentValue": current, "options": options}
	}
	settings := func() []any {
		return []any{selectOption("model", "model", model, "fixture-default", "fixture-model"),
			selectOption("effort", "thought_level", effort, "low", "high"),
			selectOption("fast", "model_config", fast, "false", "true")}
	}
	reply := func(id, result any) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	for {
		frame, line, ok := read()
		if !ok {
			return
		}
		id := frame["id"]
		params, _ := frame["params"].(map[string]any)
		switch frame["method"] {
		case "initialize":
			reply(id, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
				"loadSession": true, "sessionCapabilities": map[string]any{"close": map[string]any{}, "resume": map[string]any{}},
			}})
		case "session/new", "session/resume":
			if frame["method"] == "session/resume" && params["sessionId"] != session {
				os.Exit(3)
			}
			reply(id, map[string]any{"sessionId": session, "configOptions": settings()})
		case "session/set_config_option":
			value := params["value"].(string)
			switch params["configId"] {
			case "model":
				model = value
			case "effort":
				effort = value
			case "fast":
				fast = value
			default:
				os.Exit(3)
			}
			reply(id, map[string]any{"configOptions": settings()})
		case "session/prompt":
			text := "native fixture answer"
			if strings.Contains(line, "hold-for-native-cancel") {
				held, text = id, "native fixture partial"
			}
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": session, "update": map[string]any{"sessionUpdate": "agent_message_chunk",
					"content": map[string]any{"type": "text", "text": text}},
			}})
			if held == nil {
				reply(id, map[string]any{"stopReason": "end_turn"})
			}
		case "session/cancel":
			if held != nil {
				reply(held, map[string]any{"stopReason": "cancelled"})
				held = nil
			}
		case "session/close":
			reply(id, map[string]any{})
		default:
			if id != nil {
				os.Exit(3)
			}
		}
	}
}
