package execution

import (
	"bufio"
	"context"
	"encoding/json"
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

func TestNativeProductCodexCompletesStreamsCancelsAndRestarts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	root, trace := t.TempDir(), filepath.Join(t.TempDir(), "codex-trace.jsonl")
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
	defer sql.Close()
	wakers := controllers.NewWakerController(db)
	profiles := controllers.NewProfileDocumentController(db)
	projects := controllers.NewProjectController(db)
	if err := wakers.Create(ctx, models.Waker{ID: "native-waker", Name: "Codex fixture"}); err != nil {
		t.Fatal(err)
	}
	var documents []models.WakerDocument
	for i, kind := range []constants.ProfileDocumentKind{constants.ProfileDocumentKindIdentity, constants.ProfileDocumentKindPersona, constants.ProfileDocumentKindPlaybook, constants.ProfileDocumentKindMemory} {
		documents = append(documents, models.WakerDocument{ID: fmt.Sprintf("fixture-document-%d", i), WakerID: "native-waker", Kind: kind, Content: "Fixture instructions."})
	}
	if err := profiles.CreateWakerDocuments(ctx, documents); err != nil {
		t.Fatal(err)
	}
	summary := agentapi.RuntimeSummary{ID: "codex", BaseRuntimeID: "codex", InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"}
	fastOn, fastOff := true, false
	environment := nativeFixtureEnvironment{entry: models.RuntimeEnvironment{
		DefaultFast: &fastOn,
		RuntimeID:   "codex", BaseRuntimeID: "codex", Enabled: true, Binary: executable, PrefixArgs: []string{"-test.run=^TestNativeExecutionCodexChild$", "--"},
		Detection: agentapi.DetectionResult{State: agentapi.DetectionStateReady, Runtime: summary, ResolvedBinary: executable,
			Models: []agentapi.ModelOption{{ID: "fixture-model", PermissionLevels: []constants.PermissionLevel{constants.PermissionLevelAuto}}}},
	}}
	var accounts atomic.Int32
	open := func() (Core, chatservice.Module, *eventlognext.Store, func()) {
		t.Helper()
		store, err := eventlognext.NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{
			AcquireAccount: func(_ context.Context, account agent.AccountRequest) (agent.AccountLease, error) {
				if account.Provider != "codex" || account.Subject != "fixture-subject" {
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
	subscription, err := chats.SubscribeEvents(ctx, first.Chat.ID, held.Chat.LatestSequence)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	streamed := false
	for !streamed {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatal("SSE closed before native checkpoint")
			}
			streamed = event.Message != nil && strings.Contains(event.Message.Content, "fixture partial")
		case <-ctx.Done():
			t.Fatal("no native body reached the public subscription")
		}
	}
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

type nativeFixtureEnvironment struct {
	// entry is a frozen, locally executable Codex environment.
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
