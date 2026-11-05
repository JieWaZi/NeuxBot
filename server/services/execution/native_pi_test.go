package execution

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	usageservice "github.com/JieWaZi/NeuxBot/server/services/usage"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

const (
	piFirstQuestion  = "pi-first"
	piSecondQuestion = "pi-second"
	piHeldQuestion   = "pi-hold"
	piResumeQuestion = "pi-current-after-restart"
	piFixturePartial = "Pi fixture partial"
)

func TestNativeProductPiContinueCancelAndRecover(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	fixture := newNativeProductFixture(t, ctx, constants.RuntimeIDPi, "TestNativeExecutionPiChild", constants.PermissionLevelDefault)
	core, chats, store, closeRuntime := fixture.open()
	defer func() { closeRuntime() }()
	options := WorkOptions{
		RuntimeID: constants.RuntimeIDPi, Model: "test/fixture-model", Reasoning: "high",
		PermissionLevel: constants.PermissionLevelDefault, SessionPolicy: constants.RuntimeSessionPolicyContinue,
	}
	start := func(target ChatTarget, text string) AcceptedExecution {
		t.Helper()
		intent := WorkIntent{Chat: target, Input: WorkInput{Actor: "user", Content: text}, Options: options}
		if target.Create != nil {
			intent.ClientRequestID = "native-pi-fixture"
		}
		work, err := core.Start(ctx, intent)
		if err != nil {
			trace, _ := os.ReadFile(fixture.trace)
			t.Fatalf("Pi start %q: %v\n%s", text, err, trace)
		}
		return work
	}
	verify := func(work AcceptedExecution, status constants.ExecutionStatus, input int64) agent.NativeResultKey {
		t.Helper()
		waitNativeProductStatus(t, ctx, store, work, status)
		if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
			t.Fatal(err)
		}
		admitted, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: work.Chat.ID, HostInvocationID: work.Execution.ID})
		if err != nil || consumed == nil || !filepath.IsAbs(admitted.Session.SessionID) {
			t.Fatalf("Pi durable consumption: %+v %+v %v", admitted, consumed, err)
		}
		result, err := store.ReadNativeResultMetadata(ctx, consumed.Result)
		if err != nil || result.Attribution == nil || result.Attribution.AccountID != agent.SystemAccountID ||
			!slices.Equal(result.Turn.ConsumedInputIDs, []string{admitted.AdapterInputID}) {
			t.Fatalf("Pi native result: %+v %v", result, err)
		}
		usage := result.Turn.Usage
		if status == constants.ExecutionStatusCanceled {
			if result.Turn.Status != acpmeta.NativeTurnCancelled || usage != nil {
				t.Fatalf("Pi cancellation evidence: %+v", result.Turn)
			}
		} else if result.Turn.Status != acpmeta.NativeTurnCompleted || usage == nil || usage.InputTokens == nil ||
			*usage.InputTokens != input || !usage.Complete || usage.Coverage != "assistant_messages" {
			t.Fatalf("Pi invocation usage: %+v", result.Turn)
		}
		return consumed.Result
	}

	first := start(ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, piFirstQuestion)
	firstResult := verify(first, constants.ExecutionStatusCompleted, 3)
	target := ChatTarget{ChatID: first.Chat.ID}
	options.Reasoning = "low"
	second := start(target, piSecondQuestion)
	if result := verify(second, constants.ExecutionStatusCompleted, 7); result.Session != firstResult.Session || result == firstResult || fixture.accounts.Load() != 1 {
		t.Fatalf("Pi hot session replaced or invocation reused: %+v", result)
	}
	held := start(target, piHeldQuestion)
	waitNativeProductStream(t, ctx, chats, held, piFixturePartial)
	if err := core.CancelExecutionAndWait(ctx, held.Chat.ID, held.Execution.ID); err != nil {
		t.Fatal(err)
	}
	if result := verify(held, constants.ExecutionStatusCanceled, 0); result.Session != firstResult.Session {
		t.Fatalf("Pi cancellation targeted another session: %+v", result)
	}

	closeRuntime()
	core, chats, store, closeRuntime = fixture.open()
	resumed := start(target, piResumeQuestion)
	if result := verify(resumed, constants.ExecutionStatusCompleted, 11); result.Session != firstResult.Session || fixture.accounts.Load() != 2 {
		t.Fatalf("Pi cold resume changed native identity: %+v leases=%d", result, fixture.accounts.Load())
	}
	history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{piFirstQuestion, piSecondQuestion, piHeldQuestion, piResumeQuestion, "reply to " + piFirstQuestion, "reply to " + piSecondQuestion, "reply to " + piResumeQuestion} {
		count := 0
		for _, message := range history.Resources {
			if message.Content == text {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("Pi public history contains %d copies of %q", count, text)
		}
	}
	report, err := usageservice.NewService(store, fixture.usage).Summary(ctx, usageservice.Filter{
		From: time.Now().Add(-time.Minute).UnixMilli(), To: time.Now().Add(time.Minute).UnixMilli(),
		Timezone: "UTC", ChatID: first.Chat.ID, AccountID: agent.SystemAccountID, Source: "chat",
	}, "")
	if err != nil || report.Metrics.Invocations != 4 || report.Metrics.MissingUsageInvocations != 1 ||
		report.Metrics.KnownTokens.Input == nil || *report.Metrics.KnownTokens.Input != 21 ||
		report.Metrics.KnownTokens.Output == nil || *report.Metrics.KnownTokens.Output != 6 {
		t.Fatalf("Pi canonical token projection: %+v %v", report.Metrics, err)
	}
	assertNativePiProductWire(t, fixture.trace, firstResult.Session.SessionID)
}

func assertNativePiProductWire(t *testing.T, trace, sessionID string) {
	t.Helper()
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	var launches, instructions, prompts []string
	var clear, abort, high, low bool
	for _, line := range strings.Split(string(data), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		switch record["type"] {
		case "launch":
			launch, _ := record["sessionPath"].(string)
			digest, _ := record["instructionsDigest"].(string)
			launches = append(launches, launch)
			instructions = append(instructions, digest)
		case "prompt":
			message, _ := record["message"].(string)
			prompts = append(prompts, message)
		case "set_thinking_level":
			high = high || record["level"] == "high"
			low = low || record["level"] == "low"
		case "clear_queue":
			clear = true
		case "abort":
			abort = clear
		}
	}
	if !slices.Equal(launches, []string{"", sessionID}) || len(instructions) != 2 || instructions[0] == "" || instructions[0] != instructions[1] ||
		!slices.Equal(prompts, []string{piFirstQuestion, piSecondQuestion, piHeldQuestion, piResumeQuestion}) || !abort || !high || !low {
		t.Fatalf("Pi RPC preparation or continuity changed: %s", data)
	}
}

func TestNativeExecutionPiChild(t *testing.T) {
	if slices.Contains(os.Args, "--version") {
		fmt.Fprintln(os.Stdout, "0.85.1")
		os.Exit(0)
	}
	if !slices.Contains(os.Args, "--mode") || !slices.Contains(os.Args, "rpc") {
		return
	}
	root, trace := os.Getenv("PI_CODING_AGENT_DIR"), os.Getenv("NEUX_EXECUTION_FIXTURE_TRACE")
	if trace == "" {
		os.Exit(11)
	}
	log, err := os.OpenFile(trace, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(12)
	}
	defer log.Close()
	traceOutput := json.NewEncoder(log)
	probe := slices.Contains(os.Args, "--no-session")
	if root == "" {
		_ = traceOutput.Encode(map[string]any{"type": "fixture_error", "reason": "missing account root"})
		os.Exit(11)
	}
	pathArg := func(flag string) string {
		index := slices.Index(os.Args, flag)
		if index >= 0 && index+1 < len(os.Args) {
			return os.Args[index+1]
		}
		return ""
	}
	sessionFile := pathArg("--session")
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(13)
	}
	if !probe && sessionFile == "" {
		dir := filepath.Join(root, "sessions")
		if os.MkdirAll(dir, 0700) != nil {
			os.Exit(14)
		}
		sessionFile = filepath.Join(dir, "fixture-native.jsonl")
		header, _ := json.Marshal(map[string]any{"type": "session", "id": "fixture-native", "cwd": cwd})
		file, err := os.OpenFile(sessionFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(15)
		}
		if _, err = file.Write(append(header, '\n')); err != nil || file.Close() != nil {
			os.Exit(16)
		}
	} else if !probe {
		if _, err := os.Stat(sessionFile); err != nil {
			os.Exit(17)
		}
	}
	if probe {
		_ = traceOutput.Encode(map[string]any{"type": "probe"})
	} else {
		instructionsPath := pathArg("--append-system-prompt")
		if instructionsPath == "" {
			instructionsPath = filepath.Join(cwd, "AGENTS.md")
		}
		instructions, err := os.ReadFile(instructionsPath)
		if err != nil || !strings.Contains(string(instructions), "Fixture instructions.") {
			_ = traceOutput.Encode(map[string]any{"type": "fixture_error", "reason": "missing instructions", "present": err == nil})
			os.Exit(18)
		}
		_ = traceOutput.Encode(map[string]any{"type": "launch", "sessionPath": pathArg("--session"), "instructionsDigest": fmt.Sprintf("%x", sha256.Sum256(instructions))})
	}

	out := json.NewEncoder(os.Stdout)
	event := func(typ string, fields map[string]any) {
		if fields == nil {
			fields = map[string]any{}
		}
		fields["type"] = typ
		if out.Encode(fields) != nil {
			os.Exit(19)
		}
	}
	if pathArg("--extension") != "" {
		event("extension_ui_request", map[string]any{"method": "setStatus", "statusKey": "acp-go.extension-ready", "statusText": "ready"})
	}
	model := map[string]any{"provider": "test", "id": "fixture-model", "name": "Fixture", "reasoning": true}
	level := "off"
	cleared := false
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var request map[string]any
		if json.Unmarshal(scan.Bytes(), &request) != nil {
			os.Exit(20)
		}
		_ = traceOutput.Encode(request)
		typ, _ := request["type"].(string)
		ack := func(data any) {
			event("response", map[string]any{"id": request["id"], "command": typ, "success": true, "data": data})
		}
		switch typ {
		case "get_state":
			ack(map[string]any{"sessionFile": sessionFile, "sessionId": "fixture-native", "model": model, "thinkingLevel": level})
		case "get_available_models":
			ack(map[string]any{"models": []any{model}})
		case "get_available_thinking_levels":
			ack(map[string]any{"levels": []string{"off", "low", "high"}})
		case "set_thinking_level":
			level, _ = request["level"].(string)
			ack(nil)
		case "set_model":
			if request["provider"] != "test" || request["modelId"] != "fixture-model" {
				os.Exit(21)
			}
			ack(model)
		case "get_session_stats":
			ack(map[string]any{"tokens": map[string]any{"input": 9999, "output": 9999}, "contextUsage": map[string]any{"tokens": 20, "contextWindow": 200}})
		case "prompt":
			text, _ := request["message"].(string)
			ack(map[string]any{"disposition": "started"})
			event("agent_start", nil)
			if text == piHeldQuestion {
				event("message_update", map[string]any{"assistantMessageEvent": map[string]any{"type": "text_delta", "delta": piFixturePartial}})
				continue
			}
			input, output := 3, 1
			if text == piSecondQuestion {
				input, output = 7, 2
			}
			if text == piResumeQuestion {
				input, output = 11, 3
			}
			event("message_end", map[string]any{"message": map[string]any{
				"role": "assistant", "provider": "test", "model": "fixture-model", "stopReason": "stop",
				"content": []any{map[string]any{"type": "text", "text": "reply to " + text}},
				"usage":   map[string]any{"input": input, "output": output, "cacheRead": 0, "cacheWrite": 0},
			}})
			event("agent_end", map[string]any{"willRetry": false})
			event("agent_settled", map[string]any{"aborted": false})
		case "clear_queue":
			cleared = true
			ack(map[string]any{"steering": []string{}, "followUp": []string{}})
		case "abort":
			if !cleared {
				os.Exit(22)
			}
			event("agent_settled", map[string]any{"aborted": true})
			ack(nil)
		default:
			event("response", map[string]any{"id": request["id"], "command": typ, "success": false, "error": "unsupported fixture command"})
		}
	}
	os.Exit(0)
}
