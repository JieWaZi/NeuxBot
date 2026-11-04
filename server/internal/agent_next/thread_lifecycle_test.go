package agent_test

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
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog_next"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

func TestThreadTurnGateRejectsBeforeNativePrompt(t *testing.T) {
	lifecycle, _, req, trace := newTurnFixture(t, "cursor")
	turn, err := lifecycle.Prepare(t.Context(), req, agent.TurnRequest{Text: "never send", HostInvocationID: "gate-host", ExecutionID: "execution", AssistantMessageID: "assistant"}, agent.TurnPorts{
		Admit: func(context.Context, agent.TurnAdmission) (agent.ConversationBatch, error) {
			return agent.ConversationBatch{}, status_error.AgentOperationForbidden
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := turn.Run(t.Context())
	if !errors.Is(res.OperationError, status_error.AgentOperationForbidden) || res.Submission != "not_sent" || res.Outcome != nil {
		t.Fatalf("gate rejection = %+v", res)
	}
	data, err := os.ReadFile(trace)
	if err != nil || strings.Contains(string(data), "session/prompt") {
		t.Fatalf("rejected prompt reached child: %q %v", data, err)
	}
}

func newTurnFixture(t *testing.T, provider string) (*agent.ThreadLifecycle, *eventlog.Store, agent.ThreadRequest, string) {
	t.Helper()
	cwd := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	providerRuntime := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{
		AcquireAccount: func(context.Context, agent.AccountRequest) (agent.AccountLease, error) {
			return agent.AccountLease{}, nil
		},
	})
	lifecycle := agent.NewThreadLifecycle(providerRuntime, store)
	t.Cleanup(func() {
		if err := lifecycle.Close(context.Background()); err != nil {
			t.Error(err)
		}
		if err := providerRuntime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	trace := filepath.Join(cwd, "trace")
	return lifecycle, store, agent.ThreadRequest{
		ChatID: "c88e145db9694abbbdcb840a5099b129",
		Connection: agent.ProviderConnectionRequest{
			Account: agent.AccountRequest{Provider: provider, AccountID: "fixture-account", Subject: "fixture-subject", CWD: cwd},
			Launch: agent.ProviderLaunch{
				Executable: executable, PrefixArgs: []string{"-test.run=^TestProviderRuntimeChild$", "--"},
				Environment: map[string]string{"FIXTURE_PROVIDER": provider, "FIXTURE_TRACE": trace, "GORACE": "atexit_sleep_ms=0"},
			},
		},
	}, trace
}

func TestThreadTurnNativeUsageSurvivesObserverAndResume(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	instructions := "stable developer instructions"
	req.Session.DeveloperInstructions = &instructions
	req.Connection.Account.Model = "alternate-model"
	req.Connection.Resources.Plugins = map[string]string{"local-skill": "fixed-v1"}
	req.Connection.Resources.ConnectorIDs = []string{"stable-connector"}
	acquisitions := 0
	resources := func(context.Context, agent.ResourceSelection) (agent.ResourceLease, error) {
		acquisitions++
		return agent.ResourceLease{MCPServers: []agent.AgentMCPServer{
			{Name: "stable-connector", Transport: "stdio", Command: "/fixture/mcp"},
		}}, nil
	}

	for i, text := range []string{"first input", "second input", "after reconnect"} {
		if i == 2 {
			if err := lifecycle.CloseThread(t.Context(), req.ChatID); err != nil {
				t.Fatal(err)
			}
		}
		input := chatTurn(i, text)
		ports := chatPorts(req.ChatID, input, i == 0)
		ports.AcquireResources = resources
		if i == 0 {
			ports.Observe = func(context.Context, agent.AgentScope, agent.AgentEvent) error {
				return status_error.AgentOperationForbidden
			}
		}
		turn, err := lifecycle.Prepare(t.Context(), req, input, ports)
		if err != nil {
			t.Fatal(err)
		}
		res := turn.Run(t.Context())
		if !res.BusinessAdmitted || res.Admission != "confirmed" || res.Outcome == nil ||
			res.Outcome.Turn.Status != acpmeta.NativeTurnCompleted || res.StorageError != nil || res.OperationError != nil {
			t.Fatalf("turn %d = %+v", i, res)
		}
		if (i == 0) != errors.Is(res.DeliveryError, status_error.AgentOperationForbidden) {
			t.Fatalf("observer error on turn %d = %v", i, res.DeliveryError)
		}
		result, err := store.ReadNativeResult(t.Context(), agent.NativeResultKey{
			Session: res.Binding.Session, InvocationID: res.Outcome.Turn.InvocationID,
		})
		if err != nil || result.Turn.Usage == nil || !slices.Equal(result.Turn.ConsumedInputIDs, []string{res.Input.InputID}) {
			t.Fatalf("canonical outcome %d = %+v, %v", i, result, err)
		}
		related, err := store.ReadNativeInputs(t.Context(), result.Key.Session, result.Turn.ConsumedInputIDs)
		if err != nil || len(related) != 1 || related[0].Ref.HostInvocationID != input.HostInvocationID {
			t.Fatalf("durable host identity %d = %+v, %v", i, related, err)
		}
		if err := store.ConfirmNativeResult(t.Context(), result.Key, []agent.HostInputRef{related[0].Ref}); err != nil {
			t.Fatal(err)
		}
		message, err := store.ReadMessage(t.Context(), req.ChatID, input.AssistantMessageID)
		if err != nil || message.Text != "native answer" || !message.Complete {
			t.Fatalf("projected answer %d = %+v, %v", i, message, err)
		}
		if i == 1 {
			turn.Cancel() // A completed owner's cancellation has no session-wide effect.
		}
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if acquisitions != 2 || strings.Count(log, `"method":"thread/start"`) != 1 ||
		strings.Count(log, `"method":"thread/resume"`) != 1 || strings.Count(log, instructions) != 2 ||
		strings.Count(log, "first input") != 1 || strings.Count(log, "second input") != 1 ||
		strings.Count(log, "after reconnect") != 1 || !strings.Contains(log, `"model":"alternate-model"`) {
		t.Fatalf("hot reuse/resume trace (acquired %d): %s", acquisitions, log)
	}
}

func TestThreadTurnPreparationAndRunningCancellation(t *testing.T) {
	lifecycle, _, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	acquiring := make(chan struct{})
	preparation := make(chan error, 1)
	input := chatTurn(0, "unused")
	ports := chatPorts(req.ChatID, input, true)
	ports.AcquireResources = func(ctx context.Context, _ agent.ResourceSelection) (agent.ResourceLease, error) {
		close(acquiring)
		<-ctx.Done()
		return agent.ResourceLease{}, ctx.Err()
	}
	go func() {
		_, err := lifecycle.Prepare(t.Context(), req, input, ports)
		preparation <- err
	}()
	<-acquiring
	if _, err := lifecycle.Prepare(t.Context(), req, input, ports); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("parallel preparation = %v", err)
	}
	if err := lifecycle.CloseThread(t.Context(), req.ChatID); err != nil {
		t.Fatal(err)
	}
	if err := <-preparation; !errors.Is(err, context.Canceled) {
		t.Fatalf("preparation error = %v", err)
	}
	unrun, err := lifecycle.Prepare(t.Context(), req, input, chatPorts(req.ChatID, input, true))
	if err != nil {
		t.Fatal(err)
	}
	if err := unrun.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if result := unrun.Run(t.Context()); result.OperationError == nil {
		t.Fatal("canceled preparation ran")
	}

	hold := chatTurn(1, "hold-native")
	holdPorts := chatPorts(req.ChatID, hold, true)
	observed := make(chan struct{}, 1)
	holdPorts.Observe = func(_ context.Context, _ agent.AgentScope, event agent.AgentEvent) error {
		if event.Kind == "text" {
			select {
			case observed <- struct{}{}:
			default:
			}
		}
		return nil
	}
	turn, err := lifecycle.Prepare(t.Context(), req, hold, holdPorts)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan agent.TurnResult, 1)
	go func() { result <- turn.Run(t.Context()) }()
	select {
	case <-observed:
	case <-time.After(5 * time.Second):
		t.Fatal("native turn never started")
	}
	turn.Cancel()
	select {
	case got := <-result:
		if got.Outcome == nil || got.Outcome.Turn.Status != acpmeta.NativeTurnCancelled || got.StorageError != nil {
			t.Fatalf("cancel result = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not settle")
	}
}

func TestThreadGenerateTextUsesStrictNativeSessionWithoutChat(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "claude")
	configureThreadFixture(&req, trace)
	result, err := lifecycle.GenerateText(t.Context(), agent.TextRequest{
		Connection: req.Connection, MaxTokens: 40, Temperature: 0.2,
		Messages: []agent.TextMessage{{Role: "system", Content: "stable system rule"}, {Role: "user", Content: "detached input"}},
	})
	if err != nil || result.Text != "private answer" || result.Turn.Outcome.Turn.Usage == nil ||
		!slices.Equal(result.Limitations, []string{"max_tokens_unsupported", "temperature_unsupported"}) {
		t.Fatalf("generation = %+v %v", result, err)
	}
	key := agent.NativeResultKey{Session: result.Turn.Binding.Session, InvocationID: result.Turn.Outcome.Turn.InvocationID}
	durable, err := store.ReadNativeResult(t.Context(), key)
	if err != nil || durable.Turn.Usage == nil {
		t.Fatalf("durable detached result = %+v %v", durable, err)
	}
	inputs, err := store.ReadNativeInputs(t.Context(), key.Session, durable.Turn.ConsumedInputIDs)
	if err != nil || len(inputs) != 1 || inputs[0].Ref.ChatID != "" || inputs[0].Input.ExecutionID != "" {
		t.Fatalf("detached admission = %+v %v", inputs, err)
	}
	if version, err := store.ReadVersion(t.Context(), req.ChatID); err != nil || version != 0 {
		t.Fatalf("detached work created Chat: %d %v", version, err)
	}
	data, err := os.ReadFile(trace)
	log := string(data)
	if err != nil || !strings.Contains(log, "--tools=") || !strings.Contains(log, "--strict-mcp-config") ||
		!strings.Contains(log, `"appendSystemPrompt":"stable system rule"`) || !strings.Contains(log, "detached input") {
		t.Fatalf("Claude strict launch evidence = %s %v", log, err)
	}
	req.Connection.Account.Provider = "cursor"
	if _, err := lifecycle.GenerateText(t.Context(), agent.TextRequest{Connection: req.Connection, Messages: []agent.TextMessage{{Role: "user", Content: "deny"}}}); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatalf("unsupported strict generation = %v", err)
	}
}

func chatTurn(i int, text string) agent.TurnRequest {
	return agent.TurnRequest{
		HostInvocationID: fmt.Sprintf("host-%d", i), ExecutionID: fmt.Sprintf("execution-%d", i),
		AssistantMessageID: fmt.Sprintf("assistant-%d", i), Text: text,
	}
}

func chatPorts(chatID string, input agent.TurnRequest, first bool) agent.TurnPorts {
	return agent.TurnPorts{
		Admit: func(_ context.Context, admission agent.TurnAdmission) (agent.ConversationBatch, error) {
			userID := "user-" + input.HostInvocationID
			body, _ := json.Marshal(map[string]string{"role": "user", "content": input.Text})
			execution, _ := json.Marshal(map[string]string{"userMessageId": userID})
			changes := make([]agent.ConversationChange, 0, 3)
			if first {
				changes = append(changes, agent.ConversationChange{Fact: &agent.ChatFact{Type: constants.ChatEventTypeCreated}})
			}
			changes = append(changes, agent.ConversationChange{Fact: &agent.ChatFact{
				Type: constants.ChatEventTypeUserMessageCreated, MessageID: userID, Data: body,
			}}, agent.ConversationChange{Fact: &agent.ChatFact{
				Type: constants.ChatEventTypeExecutionCreated, ExecutionID: input.ExecutionID, MessageID: input.AssistantMessageID,
				InvocationID: admission.Input.HostInvocationID, Data: execution,
			}})
			return agent.ConversationBatch{ChatID: chatID, Key: "admit-" + input.HostInvocationID, Changes: changes}, nil
		},
	}
}

func configureThreadFixture(req *agent.ThreadRequest, trace string) {
	req.Connection.Launch.PrefixArgs = []string{"-test.run=^TestThreadNativeChild$", "--"}
	req.Connection.Launch.Environment = map[string]string{
		"THREAD_FIXTURE_PROVIDER": req.Connection.Account.Provider, "THREAD_FIXTURE_TRACE": trace, "GORACE": "atexit_sleep_ms=0",
	}
}

func TestThreadNativeChild(t *testing.T) {
	provider := os.Getenv("THREAD_FIXTURE_PROVIDER")
	if provider == "" {
		return
	}
	trace, err := os.OpenFile(os.Getenv("THREAD_FIXTURE_TRACE"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	fmt.Fprintf(trace, "cwd=%s argv=%v\n", cwd, os.Args)
	if slices.Contains(os.Args, "--version") {
		fmt.Println("2.1.309 (Claude Code)")
		os.Exit(0)
	}
	if slices.Contains(os.Args, "status") {
		fmt.Println(`{"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"pro"}`)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	out := json.NewEncoder(os.Stdout)
	session := "fixture-thread"
	for _, arg := range os.Args {
		for _, prefix := range []string{"--session-id=", "--resume="} {
			if strings.HasPrefix(arg, prefix) {
				session = strings.TrimPrefix(arg, prefix)
			}
		}
	}
	nativeTurn := ""
	count := 0
	for scanner.Scan() {
		raw := scanner.Text()
		fmt.Fprintln(trace, raw)
		var frame map[string]any
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(2)
		}
		switch provider {
		case "codex":
			count++
			fixtureCodexFrame(out, frame, raw, session, &nativeTurn, count)
		case "claude":
			fixtureClaudeFrame(out, frame, session, cwd)
		}
	}
	_ = trace.Close()
	os.Exit(0)
}

func fixtureCodexFrame(out *json.Encoder, frame map[string]any, raw, session string, nativeTurn *string, seq int) {
	emit := func(method string, params map[string]any) {
		_ = out.Encode(map[string]any{"method": method, "params": params})
	}
	ack := func(body any) { _ = out.Encode(map[string]any{"id": frame["id"], "result": body}) }
	turn := func(status string) map[string]any {
		return map[string]any{"threadId": session, "turn": map[string]any{"id": *nativeTurn, "status": status}}
	}
	method, _ := frame["method"].(string)
	switch method {
	case "initialize":
		ack(map[string]any{"userAgent": "codex/0.163.0"})
	case "account/read":
		ack(map[string]any{"account": nil, "requiresOpenaiAuth": false})
	case "model/list":
		ack(map[string]any{"data": []any{
			map[string]any{"id": "default", "model": "default-model", "displayName": "Default", "isDefault": true},
			map[string]any{"id": "alternate", "model": "alternate-model", "displayName": "Alternate"},
		}})
	case "thread/start", "thread/resume":
		ack(map[string]any{"thread": map[string]any{"id": session}, "model": "default-model"})
	case "turn/start":
		*nativeTurn = fmt.Sprintf("fixture-turn-%d-%d", os.Getpid(), seq)
		ack(turn("inProgress"))
		emit("turn/started", turn("inProgress"))
		tokens := map[string]any{"inputTokens": 19, "outputTokens": 7, "cachedInputTokens": 4, "reasoningOutputTokens": 2, "totalTokens": 26}
		emit("thread/tokenUsage/updated", map[string]any{"threadId": session, "turnId": *nativeTurn,
			"tokenUsage": map[string]any{"last": tokens, "total": tokens, "modelContextWindow": 100}})
		if strings.Contains(raw, "rich-parts") {
			emit("item/agentMessage/delta", map[string]any{"threadId": session, "turnId": *nativeTurn, "itemId": "commentary", "delta": "checking sources"})
			emit("item/completed", map[string]any{"threadId": session, "turnId": *nativeTurn,
				"item": map[string]any{"id": "commentary", "type": "agentMessage", "phase": "commentary", "text": "checking sources"}})
			emit("item/reasoning/summaryTextDelta", map[string]any{"threadId": session, "turnId": *nativeTurn, "itemId": "thought", "delta": "reasoning text"})
			emit("item/started", map[string]any{"threadId": session, "turnId": *nativeTurn,
				"item": map[string]any{"id": "tool", "type": "commandExecution", "command": "fixture command", "status": "inProgress"}})
			emit("item/commandExecution/outputDelta", map[string]any{"threadId": session, "turnId": *nativeTurn, "itemId": "tool", "delta": "tool output"})
			emit("item/completed", map[string]any{"threadId": session, "turnId": *nativeTurn,
				"item": map[string]any{"id": "tool", "type": "commandExecution", "command": "fixture command", "status": "completed", "aggregatedOutput": "tool output"}})
		}
		answer := "native answer"
		if strings.Contains(raw, "Prepare a concise private handoff") {
			answer = "private switch summary"
		}
		emit("item/agentMessage/delta", map[string]any{"threadId": session, "turnId": *nativeTurn, "itemId": "message", "delta": answer})
		if strings.Contains(raw, "hold-native") {
			return
		}
		emit("item/completed", map[string]any{"threadId": session, "turnId": *nativeTurn,
			"item": map[string]any{"id": "message", "type": "agentMessage", "phase": "final_answer", "text": answer}})
		emit("turn/completed", turn("completed"))
		if strings.Contains(raw, "activate-autonomous") {
			*nativeTurn = "independent-native-result"
			started := turn("inProgress")
			started["unprompted"] = true
			emit("turn/started", started)
			total := map[string]any{"inputTokens": 38, "outputTokens": 14, "cachedInputTokens": 8, "reasoningOutputTokens": 4, "totalTokens": 52}
			emit("thread/tokenUsage/updated", map[string]any{"threadId": session, "turnId": *nativeTurn,
				"tokenUsage": map[string]any{"last": tokens, "total": total, "modelContextWindow": 100}})
			emit("item/completed", map[string]any{"threadId": session, "turnId": *nativeTurn,
				"item": map[string]any{"id": "autonomous-message", "type": "agentMessage", "phase": "final_answer", "text": "autonomous answer"}})
			emit("turn/completed", turn("completed"))
		}
	case "thread/compact/start":
		*nativeTurn = "native-compaction"
		ack(map[string]any{})
		emit("turn/started", turn("inProgress"))
		emit("turn/completed", turn("completed"))
	case "turn/steer":
		ack(map[string]any{"turnId": *nativeTurn})
		emit("item/completed", map[string]any{"threadId": session, "turnId": *nativeTurn,
			"item": map[string]any{"id": "message", "type": "agentMessage", "phase": "final_answer", "text": "native answer"}})
		emit("turn/completed", turn("completed"))
	case "turn/interrupt":
		ack(map[string]any{})
		emit("turn/completed", turn("interrupted"))
	default:
		if frame["id"] != nil {
			ack(map[string]any{})
		}
	}
}

func fixtureClaudeFrame(out *json.Encoder, frame map[string]any, session, cwd string) {
	emit := func(value map[string]any) {
		value["session_id"] = session
		_ = out.Encode(value)
	}
	switch frame["type"] {
	case "control_request":
		request, _ := frame["request"].(map[string]any)
		response := map[string]any{}
		switch request["subtype"] {
		case "initialize":
			emit(map[string]any{"type": "system", "subtype": "init", "cwd": cwd, "model": "sonnet",
				"permissionMode": "default", "claude_code_version": "2.1.309"})
			response["models"] = []map[string]any{{"value": "sonnet", "displayName": "Sonnet"}}
		case "get_settings":
			response["applied"] = map[string]any{"model": "sonnet"}
		}
		_ = out.Encode(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "success", "request_id": frame["request_id"], "response": response,
		}})
	case "user":
		usage := map[string]any{"input_tokens": 43, "output_tokens": 7, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}
		emit(map[string]any{"type": "assistant", "user_message_uuid": frame["uuid"], "parent_tool_use_id": nil,
			"message": map[string]any{"id": "assistant-native", "model": "sonnet", "usage": usage,
				"content": []map[string]any{{"type": "text", "text": "private answer"}}}})
		emit(map[string]any{"type": "result", "subtype": "success", "uuid": "final-native", "result_index": 0,
			"user_message_uuid": frame["uuid"], "terminal_reason": "completed", "result": "private answer",
			"is_error": false, "usage": usage})
	}
}

func TestThreadTurnAccountSwitchUsesPrivateOldSession(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	first := chatTurn(0, "original public input")
	old, err := lifecycle.Prepare(t.Context(), req, first, chatPorts(req.ChatID, first, true))
	if err != nil {
		t.Fatal(err)
	}
	if result := old.Run(t.Context()); result.StorageError != nil || result.Outcome == nil {
		t.Fatalf("first turn = %+v", result)
	}

	req.Connection.Account.Provider = "claude"
	req.Connection.Account.AccountID = "other-account"
	req.Connection.Account.Subject = "other-subject"
	req.Summarize = true
	configureThreadFixture(&req, trace)
	second := chatTurn(1, "new provider input")
	prepareCtx, cancelPrepare := context.WithCancel(t.Context())
	next, err := lifecycle.Prepare(prepareCtx, req, second, chatPorts(req.ChatID, second, false))
	if err != nil {
		cancelPrepare()
		t.Fatal(err)
	}
	cancelPrepare() // Request-scoped preparation hands off to Execution's own context.
	got := next.Run(t.Context())
	if got.StorageError != nil || got.Outcome == nil || got.OperationError != nil || got.Binding.Session.Subject != "other-subject" {
		t.Fatalf("replacement = %+v", got)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	var hidden, submitted bool
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "Prepare a concise private handoff") {
			hidden = strings.Contains(line, `"method":"turn/start"`) && strings.Contains(line, `"sandboxPolicy":{"type":"readOnly","networkAccess":false}`)
		}
		if strings.Contains(line, "new provider input") {
			submitted = strings.Contains(line, "private switch summary") && !strings.Contains(line, "original public input")
		}
	}
	if !hidden || !submitted {
		t.Fatalf("private old-session handoff or new input missing:\n%s", data)
	}
	messages, err := store.ReadMessages(t.Context(), agent.MessageQuery{ChatID: req.ChatID, Limit: 12})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range messages.Messages {
		if strings.Contains(msg.Text, "private switch summary") || strings.Contains(msg.Text, "Prepare a concise private handoff") {
			t.Fatalf("private material entered public Chat: %+v", msg)
		}
	}
}

func TestThreadTurnPartialAcquisitionClosesSameOwnerBeforeRetry(t *testing.T) {
	lifecycle, _, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	input := chatTurn(0, "future prompt")
	ports := chatPorts(req.ChatID, input, true)
	opens, releases := 0, 0
	ports.AcquireResources = func(context.Context, agent.ResourceSelection) (agent.ResourceLease, error) {
		opens++
		return agent.ResourceLease{Close: func(context.Context) error {
			releases++
			if releases == 1 {
				return status_error.RuntimeShutdownFailed
			}
			return nil
		}}, status_error.RuntimeConfigurationInvalid
	}
	_, err := lifecycle.Prepare(t.Context(), req, input, ports)
	if !errors.Is(err, status_error.RuntimeConfigurationInvalid) || !errors.Is(err, status_error.RuntimeShutdownFailed) {
		t.Fatalf("partial prepare = %v", err)
	}
	_, err = lifecycle.Prepare(t.Context(), req, input, ports)
	if !errors.Is(err, status_error.RuntimeUnavailable) || opens != 1 {
		t.Fatalf("substitute owner attempted = %v opens=%d", err, opens)
	}
	if err := lifecycle.CloseThread(t.Context(), req.ChatID); err != nil || releases != 2 {
		t.Fatalf("same owner release retry = %v releases=%d", err, releases)
	}
}

func TestThreadTurnReaderCommitsAutonomousOutcomeWithoutWaiter(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	input := chatTurn(0, "activate-autonomous")
	turn, err := lifecycle.Prepare(t.Context(), req, input, chatPorts(req.ChatID, input, true))
	if err != nil {
		t.Fatal(err)
	}
	result := turn.Run(t.Context())
	if result.OperationError != nil || result.StorageError != nil || result.Outcome == nil {
		t.Fatalf("foreground outcome = %+v", result)
	}
	indexPath, err := store.IndexPath(req.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		paths, err := filepath.Glob(filepath.Join(filepath.Dir(indexPath), "native-conversations", "*.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, "independent-native-result") && strings.Contains(line, "autonomous answer") &&
					strings.Contains(line, `"origin":"provider"`) && strings.Contains(line, `"usage":`) {
					message, err := store.ReadMessage(t.Context(), req.ChatID, input.AssistantMessageID)
					if err != nil || message.Text != "native answer" {
						t.Fatalf("autonomous output contaminated host reply: %+v %v", message, err)
					}
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("unobserved autonomous outcome not durable")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestThreadTurnReplacementSeedsFiveCompleteTurns(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	for i := range 6 {
		input := chatTurn(i, fmt.Sprintf("previous-user-%d", i))
		turn, err := lifecycle.Prepare(t.Context(), req, input, chatPorts(req.ChatID, input, i == 0))
		if err != nil {
			t.Fatal(err)
		}
		if got := turn.Run(t.Context()); got.StorageError != nil || got.Outcome == nil || got.OperationError != nil {
			t.Fatalf("history seed %d = %+v", i, got)
		}
		version, err := store.ReadVersion(t.Context(), req.ChatID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Commit(t.Context(), agent.ConversationBatch{
			ChatID: req.ChatID, Key: "done-" + input.ExecutionID, ExpectedVersion: version,
			Changes: []agent.ConversationChange{{Fact: &agent.ChatFact{
				Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: input.ExecutionID, MessageID: input.AssistantMessageID,
			}}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := lifecycle.CloseThread(t.Context(), req.ChatID); err != nil {
		t.Fatal(err)
	}
	req.Connection.Account.AuthGeneration++
	req.Summarize = true // No old live owner remains, so the bounded fallback applies.
	input := chatTurn(6, "replacement-current-user")
	turn, err := lifecycle.Prepare(t.Context(), req, input, chatPorts(req.ChatID, input, false))
	if err != nil {
		t.Fatal(err)
	}
	if got := turn.Run(t.Context()); got.StorageError != nil || got.Outcome == nil || got.OperationError != nil {
		t.Fatalf("replacement = %+v", got)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "replacement-current-user") {
			if strings.Contains(line, "previous-user-0") || strings.Count(line, "previous-user-") != 5 ||
				strings.Count(line, "native answer") != 5 || !strings.Contains(line, "Recent conversation history") {
				t.Fatalf("wrong history window: %s", line)
			}
			return
		}
	}
	t.Fatalf("replacement was not submitted: %s", data)
}

func TestThreadTurnSteerAdmitsSecondInputOnSameNativeResult(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first := chatTurn(0, "hold-native")
	ports := chatPorts(req.ChatID, first, true)
	observed := make(chan struct{}, 1)
	ports.Observe = func(_ context.Context, scope agent.AgentScope, event agent.AgentEvent) error {
		if event.Kind == "text" && scope.NativeInvocationID != "" && scope.HostInvocationID == first.HostInvocationID {
			select {
			case observed <- struct{}{}:
			default:
			}
		}
		return nil
	}
	turn, err := lifecycle.Prepare(ctx, req, first, ports)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan agent.TurnResult, 1)
	go func() { done <- turn.Run(ctx) }()
	select {
	case <-observed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	steer := chatTurn(1, "only one steering input")
	denied := chatPorts(req.ChatID, steer, false)
	denied.Admit = func(context.Context, agent.TurnAdmission) (agent.ConversationBatch, error) {
		return agent.ConversationBatch{}, status_error.AgentOperationForbidden
	}
	receipt, err := turn.Steer(ctx, steer, denied)
	if !errors.Is(err, status_error.AgentOperationForbidden) || receipt.Receipt == nil || receipt.Receipt.Submission != "not_sent" {
		t.Fatalf("steering gate denial = %+v %v", receipt, err)
	}
	rejectedInput := receipt.Receipt.Input.InputID
	receipt, err = turn.Steer(ctx, steer, chatPorts(req.ChatID, steer, false))
	if err != nil || receipt.Outcome != "injected" || receipt.Receipt == nil || receipt.Receipt.Submission != "sent" || receipt.Receipt.Admission != "confirmed" {
		t.Fatalf("steering receipt = %+v %v", receipt, err)
	}
	var result agent.TurnResult
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if result.Outcome == nil || result.StorageError != nil || result.OperationError != nil ||
		!slices.Equal(result.Outcome.Turn.ConsumedInputIDs, []string{result.Input.InputID, receipt.Receipt.Input.InputID}) {
		t.Fatalf("shared outcome = %+v", result)
	}
	key := agent.NativeResultKey{Session: result.Binding.Session, InvocationID: result.Outcome.Turn.InvocationID}
	refs := []agent.HostInputRef{{ChatID: req.ChatID, HostInvocationID: first.HostInvocationID}, {ChatID: req.ChatID, HostInvocationID: steer.HostInvocationID}}
	if err := store.ConfirmNativeResult(ctx, key, refs); err != nil {
		t.Fatal(err)
	}
	if _, err := turn.Steer(ctx, chatTurn(2, "stale steering"), chatPorts(req.ChatID, steer, false)); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("old turn issued another steer: %v", err)
	}
	data, err := os.ReadFile(trace)
	if err != nil || strings.Count(string(data), `"method":"turn/steer"`) != 1 ||
		strings.Contains(string(data), rejectedInput) || strings.Contains(string(data), "stale steering") {
		t.Fatalf("steering bytes = %s %v", data, err)
	}
}

func TestThreadTurnCursorUsesExactAdapterConsumptionWithoutNativeInputID(t *testing.T) {
	lifecycle, store, req, _ := newTurnFixture(t, "cursor")
	input := chatTurn(0, "cursor prompt")
	turn, err := lifecycle.Prepare(t.Context(), req, input, chatPorts(req.ChatID, input, true))
	if err != nil {
		t.Fatal(err)
	}
	result := turn.Run(t.Context())
	if result.Outcome == nil || result.OperationError != nil || result.StorageError != nil || result.Input.NativeInputID != nil ||
		!slices.Equal(result.Outcome.Turn.ConsumedInputIDs, []string{result.Input.InputID}) {
		t.Fatalf("Cursor result = %+v", result)
	}
	key := agent.NativeResultKey{Session: result.Binding.Session, InvocationID: result.Outcome.Turn.InvocationID}
	if err := store.ConfirmNativeResult(t.Context(), key, []agent.HostInputRef{{ChatID: req.ChatID, HostInvocationID: input.HostInvocationID}}); err != nil {
		t.Fatal(err)
	}
}

func TestThreadTurnCompactUsesExistingNativeOwner(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	input := chatTurn(0, "before compaction")
	turn, err := lifecycle.Prepare(ctx, req, input, chatPorts(req.ChatID, input, true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Compact(ctx, req.ChatID); !errors.Is(err, status_error.RuntimeUnavailable) {
		t.Fatalf("compaction bypassed prepared owner: %v", err)
	}
	run := turn.Run(ctx)
	if run.Outcome == nil || run.StorageError != nil || run.OperationError != nil {
		t.Fatalf("initial run = %+v", run)
	}
	compacted, err := lifecycle.Compact(ctx, req.ChatID)
	if err != nil || compacted.Turn == nil || compacted.Turn.Status != acpmeta.NativeTurnCompleted {
		t.Fatalf("compacted = %+v %v", compacted, err)
	}
	key := agent.NativeResultKey{Session: run.Binding.Session, InvocationID: compacted.Turn.InvocationID}
	if err := store.ConfirmNativeResult(ctx, key, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(trace)
	if err != nil || strings.Count(string(data), `"method":"thread/start"`) != 1 ||
		strings.Count(string(data), `"method":"turn/start"`) != 1 || strings.Count(string(data), `"method":"thread/compact/start"`) != 1 {
		t.Fatalf("compaction did not use existing owner: %s %v", data, err)
	}
}

func TestThreadTurnPreservesOrderedNativeMessageSemantics(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "codex")
	configureThreadFixture(&req, trace)
	input := chatTurn(0, "rich-parts")
	turn, err := lifecycle.Prepare(t.Context(), req, input, chatPorts(req.ChatID, input, true))
	if err != nil {
		t.Fatal(err)
	}
	result := turn.Run(t.Context())
	if result.StorageError != nil || result.OperationError != nil || result.Outcome == nil {
		t.Fatalf("rich result = %+v", result)
	}
	message, err := store.ReadMessage(t.Context(), req.ChatID, input.AssistantMessageID)
	if err != nil {
		t.Fatal(err)
	}
	want := []constants.RuntimeEventType{
		constants.RuntimeEventTypeCommentaryDelta, constants.RuntimeEventTypeThinkingDelta,
		constants.RuntimeEventTypeToolUpdate, constants.RuntimeEventTypeTextDelta,
	}
	var types []constants.RuntimeEventType
	for _, part := range message.Parts {
		types = append(types, part.Type)
	}
	if !slices.Equal(types, want) || message.Parts[0].Text != "checking sources" ||
		message.Parts[1].Text != "reasoning text" || !strings.Contains(string(message.Parts[2].Data), "tool output") ||
		!strings.Contains(string(message.Parts[2].Data), "completed") || message.Text != "native answer" {
		t.Fatalf("durable ordered parts = %+v", message)
	}
}

func TestThreadTurnDetachedTaskRequiresExplicitNativeToolSelection(t *testing.T) {
	lifecycle, store, req, trace := newTurnFixture(t, "claude")
	configureThreadFixture(&req, trace)
	chatID := req.ChatID
	req.ChatID = ""
	task := agent.TaskRequest{
		Thread: req, Input: agent.TurnRequest{Text: "detached task"},
		Ports: agent.TurnPorts{Approve: func(context.Context, agent.AgentScope, agent.AgentApproval) (string, error) {
			return "", nil
		}},
	}
	if _, err := lifecycle.ExecuteTask(t.Context(), task); !errors.Is(err, status_error.RuntimePolicyUnsupported) {
		t.Fatalf("task with inherited tools = %v", err)
	}
	task.Thread.Connection.Launch.Claude.Tools = []string{"Read"}
	result, err := lifecycle.ExecuteTask(t.Context(), task)
	if err != nil || result.Outcome == nil || result.Outcome.Turn.Usage == nil {
		t.Fatalf("detached allowlisted task = %+v %v", result, err)
	}
	data, err := os.ReadFile(trace)
	if err != nil || !strings.Contains(string(data), "--tools=Read") || !strings.Contains(string(data), "detached task") {
		t.Fatalf("native tool grant = %s %v", data, err)
	}
	if v, err := store.ReadVersion(t.Context(), chatID); err != nil || v != 0 {
		t.Fatalf("detached task created a public Chat: %d %v", v, err)
	}
}
