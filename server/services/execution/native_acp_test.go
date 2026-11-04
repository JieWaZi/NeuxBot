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
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	usageservice "github.com/JieWaZi/NeuxBot/server/services/usage"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
)

const (
	acpFirstQuestion   = "acp-first-question"
	acpSecondQuestion  = "acp-second-question"
	acpHeldQuestion    = "acp-hold-for-cancel"
	acpResumeQuestion  = "acp-current-after-restart"
	acpAutoQuestion    = "acp-auto-permission"
	acpFullQuestion    = "acp-full-permission"
	acpUnsupportedFast = "acp-unsupported-fast"
	acpFixtureResponse = "ACP fixture response"
	acpFixturePartial  = "ACP fixture partial"
)

func TestNativeProductWorkBuddyQoderContinueCancelAndRecover(t *testing.T) {
	for _, provider := range []string{constants.RuntimeIDWorkBuddy, constants.RuntimeIDQoder, constants.RuntimeIDQoderCN} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			t.Setenv("NEUX_EXECUTION_FIXTURE_PROVIDER", provider)
			fixture := newNativeProductFixture(t, ctx, provider, "TestNativeExecutionACPChild", constants.PermissionLevelDefault)
			core, chats, store, closeRuntime := fixture.open()
			defer func() { closeRuntime() }()
			options := WorkOptions{RuntimeID: provider, Model: "fixture-model", Reasoning: "high",
				PermissionLevel: constants.PermissionLevelDefault, SessionPolicy: constants.RuntimeSessionPolicyContinue}
			start := func(target ChatTarget, text string) AcceptedExecution {
				t.Helper()
				intent := WorkIntent{Chat: target, Input: WorkInput{Actor: "user", Content: text}, Options: options}
				if target.Create != nil {
					intent.ClientRequestID = "native-create-" + text
				}
				work, err := core.Start(ctx, intent)
				if err != nil {
					trace, _ := os.ReadFile(fixture.trace)
					t.Fatalf("start %q: %v\n%s", text, err, trace)
				}
				if work.Execution.Fast != nil || work.Execution.Reasoning != options.Reasoning || work.Execution.PermissionLevel != options.PermissionLevel {
					t.Fatalf("execution lost its selected policy: %+v", work.Execution)
				}
				return work
			}
			verify := func(work AcceptedExecution, status constants.ExecutionStatus, reportedOutput int64) agent.NativeResultKey {
				t.Helper()
				waitNativeProductStatus(t, ctx, store, work, status)
				if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
					t.Fatal(err)
				}
				input, consumed, err := store.ReadHostInputConsumption(ctx, agent.HostInputRef{ChatID: work.Chat.ID, HostInvocationID: work.Execution.ID})
				if err != nil || consumed == nil || input.Session.SessionID == "" {
					t.Fatalf("durable consumption: %+v %+v %v", input, consumed, err)
				}
				result, err := store.ReadNativeResultMetadata(ctx, consumed.Result)
				if err != nil || result.Attribution == nil || result.Attribution.AccountID != agent.SystemAccountID ||
					result.Attribution.WakerID != "native-waker" || result.Attribution.ChatID != work.Chat.ID || result.Attribution.Source != "chat" {
					t.Fatalf("canonical native attribution: %+v %v", result, err)
				}
				nativeStatus := acpmeta.NativeTurnCompleted
				if status == constants.ExecutionStatusCanceled {
					nativeStatus = acpmeta.NativeTurnCancelled
				}
				if result.Turn.Status != nativeStatus || !slices.Equal(result.Turn.ConsumedInputIDs, []string{input.AdapterInputID}) {
					t.Fatalf("native status or exact input receipt: %+v", result.Turn)
				}
				if provider == constants.RuntimeIDWorkBuddy && result.Turn.NativeTurnID == nil {
					t.Fatalf("CodeBuddy native turn identity was lost: %+v", result.Turn)
				}
				usage := result.Turn.Usage
				if provider == constants.RuntimeIDWorkBuddy || status == constants.ExecutionStatusCanceled {
					if usage != nil {
						t.Fatalf("provider supplied no billable counters: %+v", usage)
					}
				} else if usage == nil || usage.InputTokens != nil || usage.OutputTokens != nil || usage.Complete ||
					len(usage.Observations) != 1 || usage.Observations[0].Coverage != "unknown" ||
					usage.Observations[0].Tokens.InputTokens != nil || usage.Observations[0].Tokens.OutputTokens == nil ||
					*usage.Observations[0].Tokens.OutputTokens != reportedOutput ||
					usage.Observations[0].InputTotalTokens == nil || *usage.Observations[0].InputTotalTokens != reportedOutput*2 ||
					usage.Observations[0].TotalTokens == nil || *usage.Observations[0].TotalTokens != reportedOutput*3 {
					t.Fatalf("Qoder sample with unknown scope must be retained without billing: %+v", usage)
				}
				return consumed.Result
			}

			first := start(ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, acpFirstQuestion)
			firstResult := verify(first, constants.ExecutionStatusCompleted, 4)
			target := ChatTarget{ChatID: first.Chat.ID}

			options.Reasoning = "low"
			second := start(target, acpSecondQuestion)
			secondResult := verify(second, constants.ExecutionStatusCompleted, 9)
			if secondResult.Session != firstResult.Session || secondResult == firstResult || fixture.accounts.Load() != 1 {
				t.Fatalf("hot continuation did not reuse one native session: %+v %+v leases=%d", firstResult, secondResult, fixture.accounts.Load())
			}
			history, err := chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
			if err != nil || len(history.Resources) != 4 || history.Resources[0].Content != acpFirstQuestion ||
				history.Resources[1].Content != acpFixtureResponse || history.Resources[2].Content != acpSecondQuestion ||
				history.Resources[3].Content != acpFixtureResponse {
				t.Fatalf("public history: %+v %v", history, err)
			}

			fast := true
			unsupported := options
			unsupported.Fast = &fast
			_, err = core.Start(ctx, WorkIntent{Chat: target, Options: unsupported,
				Input: WorkInput{Actor: "user", Content: acpUnsupportedFast}})
			if !errors.Is(err, status_error.RuntimePolicyUnsupported) {
				t.Fatalf("unsupported Fast request = %v", err)
			}

			held := start(target, acpHeldQuestion)
			waitNativeProductStream(t, ctx, chats, held, acpFixturePartial)
			if err := core.CancelExecutionAndWait(ctx, held.Chat.ID, held.Execution.ID); err != nil {
				t.Fatal(err)
			}
			if canceled := verify(held, constants.ExecutionStatusCanceled, 0); canceled.Session != firstResult.Session {
				t.Fatalf("cancel targeted a different native session: %+v", canceled)
			}

			closeRuntime()
			core, chats, store, closeRuntime = fixture.open()
			resumed := start(target, acpResumeQuestion)
			if recovered := verify(resumed, constants.ExecutionStatusCompleted, 12); recovered.Session != firstResult.Session || fixture.accounts.Load() != 2 {
				t.Fatalf("cold recovery lost the original session: %+v leases=%d", recovered, fixture.accounts.Load())
			}
			metrics, err := usageservice.NewService(store, fixture.usage).Summary(ctx, usageservice.Filter{
				From: time.Now().Add(-time.Minute).UnixMilli(), To: time.Now().Add(time.Minute).UnixMilli(),
				Timezone: "UTC", ChatID: first.Chat.ID, Source: "chat", AccountID: agent.SystemAccountID,
			}, "")
			if err != nil || metrics.Metrics.Invocations != 4 || metrics.Metrics.KnownTokens.Input != nil {
				t.Fatalf("usage projection did not retain one native record per execution: %+v %v", metrics, err)
			}
			if metrics.Metrics.KnownTokens.Output != nil || metrics.Metrics.MissingUsageInvocations != 4 {
				t.Fatalf("unattributed native samples became billable counters: %+v", metrics.Metrics)
			}
			assertACPNativeProductWire(t, fixture.trace, firstResult.Session.SessionID)
			history, err = chats.ReadMessageHistory(ctx, first.Chat.ID, 0, 12)
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{acpFirstQuestion, acpSecondQuestion, acpResumeQuestion} {
				matches := 0
				for _, message := range history.Resources {
					if message.Content == text {
						matches++
					}
				}
				if matches != 1 {
					t.Fatalf("restored public history contains %d copies of %q: %+v", matches, text, history)
				}
			}

			options.Reasoning = ""
			for _, policy := range []struct {
				// level 是新聊天显式选择的产品权限。
				level constants.PermissionLevel
				// prompt 让原生夹具核对这一轮实际使用的权限。
				prompt string
			}{
				{constants.PermissionLevelAuto, acpAutoQuestion},
				{constants.PermissionLevelFullAccess, acpFullQuestion},
			} {
				options.PermissionLevel = policy.level
				work := start(ChatTarget{Create: &CreateChatSpec{WakerID: "native-waker"}}, policy.prompt)
				waitNativeProductStatus(t, ctx, store, work, constants.ExecutionStatusCompleted)
				if err := core.CancelExecutionAndWait(ctx, work.Chat.ID, work.Execution.ID); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func assertACPNativeProductWire(t *testing.T, tracePath, sessionID string) {
	t.Helper()
	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(trace), acpUnsupportedFast) {
		t.Fatalf("unsupported Fast request reached the native prompt: %s", trace)
	}
	var prompts []string
	var permissionModes []string
	var opened, recovered, cancelled int
	for _, line := range strings.Split(string(trace), "\n") {
		if line == "" {
			continue
		}
		var frame struct {
			// Method 是夹具收到的原生 ACP 请求方法。
			Method string `json:"method"`
			// Params 是未投影的原生请求参数。
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		switch frame.Method {
		case "session/new":
			opened++
		case "session/load", "session/resume":
			recovered++
		case "session/cancel":
			cancelled++
		case "session/prompt":
			prompts = append(prompts, line)
		case "session/set_mode":
			permissionModes = append(permissionModes, strings.Trim(string(frame.Params["modeId"]), `"`))
		case "session/set_config_option":
			if string(frame.Params["configId"]) == `"mode"` {
				permissionModes = append(permissionModes, strings.Trim(string(frame.Params["value"]), `"`))
			}
			if string(frame.Params["configId"]) == `"reasoning_effort"` && string(frame.Params["value"]) == `"fixture-model"` {
				t.Fatalf("model ID was submitted as a reasoning effort: %s", line)
			}
		}
		if rawID := frame.Params["sessionId"]; len(rawID) > 0 && string(rawID) != fmt.Sprintf("%q", sessionID) {
			t.Fatalf("native request addressed the wrong session: %s", line)
		}
	}
	if opened != 1 || recovered != 1 || cancelled != 1 || len(prompts) != 4 {
		t.Fatalf("native lifecycle: new=%d recover=%d cancel=%d prompt=%d\n%s", opened, recovered, cancelled, len(prompts), trace)
	}
	for _, mode := range permissionModes {
		if mode != "default" {
			t.Fatalf("default permission unexpectedly changed on the wire: %v\n%s", permissionModes, trace)
		}
	}
	inputs := []string{acpFirstQuestion, acpSecondQuestion, acpHeldQuestion, acpResumeQuestion}
	for i, prompt := range prompts {
		for j, text := range inputs {
			if strings.Contains(prompt, text) != (i == j) {
				t.Fatalf("prompt did not send only its current user input: %s", prompt)
			}
		}
		if strings.Contains(prompt, acpFixtureResponse) || strings.Contains(prompt, "Recent conversation history") {
			t.Fatalf("prompt replayed previous turns: %s", prompt)
		}
	}
}

func TestNativeExecutionACPChild(t *testing.T) {
	provider := os.Getenv("NEUX_EXECUTION_FIXTURE_PROVIDER")
	if provider == "" {
		return
	}
	workBuddy := provider == constants.RuntimeIDWorkBuddy
	if slices.Contains(os.Args, "--version") {
		if workBuddy {
			fmt.Println("2.151.0")
		} else {
			fmt.Println("1.1.53")
		}
		os.Exit(0)
	}
	trace, err := os.OpenFile(os.Getenv("NEUX_EXECUTION_FIXTURE_TRACE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	serveNativeExecutionACP(bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout), trace, provider)
	_ = trace.Close()
	os.Exit(0)
}

func serveNativeExecutionACP(in *bufio.Scanner, out *json.Encoder, trace *os.File, provider string) {
	workBuddy := provider == constants.RuntimeIDWorkBuddy
	var session string
	model, effort, mode := "fixture-default", "low", "default"
	effortCategory, full := "model", "yolo"
	if workBuddy {
		effortCategory, full = "thought_level", "bypassPermissions"
	} else if provider == constants.RuntimeIDQoderCN {
		full = "bypassPermissions"
	}
	selectOption := func(id, category, current string, values ...string) map[string]any {
		choices := make([]map[string]string, 0, len(values))
		for _, value := range values {
			choices = append(choices, map[string]string{"value": value, "name": value})
		}
		return map[string]any{"id": id, "name": id, "category": category, "type": "select", "currentValue": current, "options": choices}
	}
	settings := func() []any {
		return []any{selectOption("reasoning_effort", effortCategory, effort, "low", "high"),
			selectOption("model", "model", model, "fixture-default", "fixture-model"),
			selectOption("mode", "mode", mode, "default", "auto", full)}
	}
	modes := func() map[string]any {
		return map[string]any{"currentModeId": mode, "availableModes": []map[string]string{
			{"id": "default", "name": "Default"}, {"id": "auto", "name": "Auto"}, {"id": full, "name": "Full"},
		}}
	}
	reply := func(id, result any) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	notify := func(kind, text string) {
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
			"sessionId": session, "update": map[string]any{"sessionUpdate": kind,
				"content": map[string]any{"type": "text", "text": text}},
		}})
	}
	finish := func(id any, reason string, serial, tokens int) {
		result := map[string]any{"stopReason": reason, "userMessageId": fmt.Sprintf("fixture-user-%d", serial)}
		if workBuddy {
			result["_meta"] = map[string]any{"codebuddy.ai/conversationRequestId": fmt.Sprintf("019abbbbbbbb700080000000%08x", serial)}
		} else if tokens > 0 {
			result["usage"] = map[string]any{"inputTokens": tokens * 2, "outputTokens": tokens, "totalTokens": tokens * 3}
			result["_meta"] = map[string]any{"quota": map[string]any{"token_count": map[string]any{"input_tokens": tokens * 2, "output_tokens": tokens}}}
		}
		reply(id, result)
	}
	var held any
	for in.Scan() {
		fmt.Fprintln(trace, in.Text())
		var frame struct {
			// ID 是需要回复的 JSON-RPC 请求标识。
			ID json.RawMessage `json:"id"`
			// Method 是夹具执行的原生 ACP 操作。
			Method string `json:"method"`
			// Params 是该操作的原生参数。
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &frame) != nil {
			os.Exit(3)
		}
		value := func(key string) string {
			var result string
			_ = json.Unmarshal(frame.Params[key], &result)
			return result
		}
		id := frame.ID
		switch frame.Method {
		case "initialize":
			capabilities := map[string]any{"close": map[string]any{}}
			if !workBuddy {
				capabilities["resume"] = map[string]any{}
			}
			reply(id, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
				"loadSession": true, "sessionCapabilities": capabilities,
			}})
		case "session/new":
			session = "acp-fixture-" + filepath.Base(filepath.Dir(value("cwd")))
			reply(id, map[string]any{"sessionId": session, "configOptions": settings(), "modes": modes()})
		case "session/load", "session/resume":
			session = "acp-fixture-" + filepath.Base(filepath.Dir(value("cwd")))
			if value("sessionId") != session || workBuddy != (frame.Method == "session/load") {
				os.Exit(4)
			}
			if workBuddy {
				notify("user_message_chunk", acpFirstQuestion)
			}
			reply(id, map[string]any{"configOptions": settings(), "modes": modes()})
		case "session/set_config_option":
			switch value("configId") {
			case "model":
				model = value("value")
			case "reasoning_effort":
				effort = value("value")
			case "mode":
				if !workBuddy {
					os.Exit(4)
				}
				mode = value("value")
			default:
				os.Exit(4)
			}
			reply(id, map[string]any{"configOptions": settings(), "modes": modes()})
		case "session/set_mode":
			if workBuddy {
				os.Exit(4)
			}
			mode = value("modeId")
			reply(id, map[string]any{})
		case "session/prompt":
			if value("sessionId") != session || model != "fixture-model" {
				os.Exit(4)
			}
			line := in.Text()
			switch {
			case strings.Contains(line, acpFirstQuestion) && mode == "default" && effort == "high":
				notify("agent_message_chunk", acpFixtureResponse)
				finish(id, "end_turn", 1, 4)
			case strings.Contains(line, acpSecondQuestion) && mode == "default" && effort == "low":
				notify("agent_message_chunk", acpFixtureResponse)
				finish(id, "end_turn", 2, 9)
			case strings.Contains(line, acpHeldQuestion) && mode == "default":
				held = id
				notify("agent_message_chunk", acpFixturePartial)
			case strings.Contains(line, acpResumeQuestion) && mode == "default":
				notify("agent_message_chunk", acpFixtureResponse)
				finish(id, "end_turn", 4, 12)
			case strings.Contains(line, acpAutoQuestion) && mode == "auto" && effort == "low":
				notify("agent_message_chunk", acpFixtureResponse)
				finish(id, "end_turn", 5, 4)
			case strings.Contains(line, acpFullQuestion) && mode == full && effort == "low":
				notify("agent_message_chunk", acpFixtureResponse)
				finish(id, "end_turn", 6, 4)
			default:
				os.Exit(4)
			}
		case "session/cancel":
			if held == nil || value("sessionId") != session {
				os.Exit(4)
			}
			finish(held, "cancelled", 3, 0)
			held = nil
		case "session/close":
			reply(id, map[string]any{})
		default:
			if len(id) > 0 {
				os.Exit(4)
			}
		}
	}
}
