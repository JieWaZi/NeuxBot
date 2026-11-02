package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// nativeCodexObservation 保留真实CLI的模型请求和最终结果，不检查其原生配置与工具清单。
type nativeCodexObservation struct {
	requests []map[string]any // requests 保存同一回合的模型请求，工具循环不是宿主重放。
	result   turnResult       // result 是真实Runtime与公开ACP连接返回的结果。
}

// TestNativeCodexToolThenFinal 验证真实CLI执行工具后继续模型回合，仅返回最终回答。
func TestNativeCodexToolThenFinal(t *testing.T) {
	got := runNativeCodexFixture(t, "tool_then_final")
	if len(got.requests) != 2 {
		t.Fatalf("model requests = %d, want tool loop then final", len(got.requests))
	}
	if !got.result.Effects.ToolActivity {
		t.Error("native tool activity was not recorded")
	}
	toolOutputFound := false
	items, _ := got.requests[1]["input"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["type"] != "function_call_output" || item["call_id"] != "call_fixture" {
			continue
		}
		encoded, err := json.Marshal(item["output"])
		if err != nil {
			t.Fatal(err)
		}
		toolOutputFound = strings.Contains(string(encoded), "TOOL_FIXTURE_OK")
	}
	if !toolOutputFound {
		t.Error("executed tool output did not reach the follow-up model request")
	}
	if got.result.Status != native.StatusCompleted || got.result.Fault != nil || got.result.CleanupFault != nil || got.result.Output.Text != "FINAL_FIXTURE_OK" || got.result.Output.Reliability != ReliabilityVerifiedPhase {
		t.Fatalf("tool loop did not yield final-only result: %+v", got.result)
	}
}

// TestNativeCodexSessionResume 验证两轮热续聊及关闭后的原生检查点恢复，不重建用户历史。
func TestNativeCodexSessionResume(t *testing.T) {
	got := runNativeCodexFixture(t, "session_resume")
	if len(got.requests) != 3 || got.result.Status != native.StatusCompleted || got.result.Output.Text != "FINAL_FIXTURE_OK" || got.result.Output.Reliability != ReliabilityVerifiedPhase {
		t.Fatalf("native resumed session result=%+v requests=%d", got.result, len(got.requests))
	}
	markers := []string{"EXACT_USER_FIXTURE", "SECOND_USER_FIXTURE", "THIRD_USER_FIXTURE"}
	for index, request := range got.requests {
		if request["instructions"] != "EXPLICIT_SYSTEM_FIXTURE" && !nativeCodexExactMessage(request["input"], "developer", "EXPLICIT_SYSTEM_FIXTURE") {
			t.Fatalf("native turn %d changed static instructions", index+1)
		}
		for markerIndex, text := range markers {
			count := 0
			items, _ := request["input"].([]any)
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item["role"] != "user" {
					continue
				}
				content, _ := item["content"].([]any)
				for _, rawPart := range content {
					part, _ := rawPart.(map[string]any)
					if part["type"] == "input_text" && part["text"] == text {
						count++
					}
				}
			}
			if markerIndex == index && count != 1 || markerIndex < index && count > 1 || markerIndex > index && count != 0 {
				t.Fatalf("native turn %d user marker %d count=%d", index+1, markerIndex+1, count)
			}
		}
	}
	t.Log("native codex three turns each contain exactly one new input, no duplicated prior inputs, unchanged static instructions")
}

// TestNativeCodexCancellation 仅在真实CLI到达虚构Provider后取消，不交付未完成正文。
func TestNativeCodexCancellation(t *testing.T) {
	got := runNativeCodexFixture(t, "cancel")
	if got.result.Status != native.StatusCancelled || got.result.Output.Text != "" || got.result.Output.Status == OutputAvailable || got.result.CleanupFault != nil {
		t.Fatalf("native cancellation = %+v", got.result)
	}
}

// TestNativeCodexFinalEvidenceGate 只接受原生明确最终阶段，缺失或非最终阶段不产生答案。
func TestNativeCodexFinalEvidenceGate(t *testing.T) {
	for _, phase := range []string{"final_answer", "late_final", "commentary", "partial_answer", ""} {
		name := phase
		if name == "" {
			name = "missing_phase"
		}
		t.Run(name, func(t *testing.T) {
			got := runNativeCodexFixture(t, phase)
			if got.result.Status != native.StatusCompleted || got.result.Fault != nil || got.result.CleanupFault != nil {
				t.Fatalf("native execution failed: %+v", got.result)
			}
			if phase == "final_answer" || phase == "late_final" {
				if got.result.Output.Status != OutputAvailable || got.result.Output.Text != "FINAL_FIXTURE_OK" || got.result.Output.Reliability != ReliabilityVerifiedPhase {
					t.Fatalf("explicit native final evidence unavailable: %+v", got.result.Output)
				}
			} else if got.result.Output.Status != OutputUnavailable || got.result.Output.Text != "" {
				t.Fatalf("non-final native text promoted to answer: %+v", got.result.Output)
			}
		})
	}
}

// runNativeCodexFixture 使用实际CLI和本地虚构Provider，输出只包含测试标记。
func runNativeCodexFixture(t *testing.T, phase string) nativeCodexObservation {
	t.Helper()
	binary := os.Getenv("NEUX_RUNTIME_CODEX_CLI")
	if binary == "" {
		t.Skip("set NEUX_RUNTIME_CODEX_CLI to an absolute CLI path for explicit native acceptance")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("native acceptance requires an absolute CLI path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	home, nativeHome, workspace := filepath.Join(root, "home"), filepath.Join(root, "native"), filepath.Join(root, "workspace")
	for _, directory := range []string{home, workspace} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	requests := make(chan map[string]any, 4)
	var requestCount atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected fixture route: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("native request did not use fixture credential")
		}
		var payload map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		select {
		case requests <- payload:
		default:
			t.Error("unexpected repeated model requests")
		}
		if phase == "cancel" {
			cancel()
			<-r.Context().Done()
			return
		}
		if phase == "tool_then_final" && requestCount.Add(1) == 1 {
			name := ""
			definitions, _ := payload["tools"].([]any)
			for _, raw := range definitions {
				tool, _ := raw.(map[string]any)
				candidate, _ := tool["name"].(string)
				if candidate == "shell_command" || candidate == "exec_command" {
					name = candidate
					break
				}
			}
			if name == "" {
				t.Error("native fixture did not advertise a shell tool")
				writeNativeCodexResponse(w, "final_answer")
				return
			}
			writeNativeCodexToolResponse(w, name)
		} else {
			responsePhase := phase
			if phase == "tool_then_final" || phase == "session_resume" {
				responsePhase = "final_answer"
			}
			writeNativeCodexResponse(w, responsePhase)
		}
	}))
	defer provider.Close()
	catalog := `{"models":[{"slug":"fixture-model","display_name":"fixture-model","description":"fixture","base_instructions":"Fixture model instructions.","visibility":"list","priority":1,"supported_in_api":true,"context_window":32768,"max_context_window":32768,"effective_context_window_percent":95,"input_modalities":["text"],"supports_reasoning_summaries":false,"supports_parallel_tool_calls":false,"support_verbosity":false,"shell_type":"shell_command","experimental_supported_tools":[],"default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low","description":"low"}],"default_reasoning_summary":"none","truncation_policy":{"mode":"bytes","limit":10000}}]}`
	write(filepath.Join(nativeHome, "models.json"), catalog)
	write(filepath.Join(nativeHome, "instructions.md"), "EXPLICIT_SYSTEM_FIXTURE")
	config := "model = \"fixture-model\"\nmodel_provider = \"fixture\"\nmodel_catalog_json = " + strconv.Quote(filepath.Join(nativeHome, "models.json")) + "\n[model_providers.fixture]\nname = \"fixture\"\nwire_api = \"responses\"\nbase_url = " + strconv.Quote(provider.URL+"/v1") + "\nenv_key = \"NEUX_FIXTURE_API_KEY\"\n"
	write(filepath.Join(nativeHome, "config.toml"), config)
	environment := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home, "CODEX_HOME=" + nativeHome, "TMPDIR=" + root, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"), "XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"), "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"), "XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"), "NEUX_FIXTURE_API_KEY=fixture-key", "NO_PROXY=127.0.0.1,localhost", "HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1"}
	// acp-go进程继承调用者cwd；测试启动器把所有子命令固定在临时目录，隔离真实项目配置。
	launcher := filepath.Join(root, "codex-fixture")
	write(launcher, "#!/bin/sh\ncd \"$NEUX_FIXTURE_CWD\" || exit 70\nexec \"$NEUX_FIXTURE_CODEX\" \"$@\"\n")
	if err := os.Chmod(launcher, 0700); err != nil {
		t.Fatal(err)
	}
	environment = append(environment, "NEUX_FIXTURE_CWD="+workspace, "NEUX_FIXTURE_CODEX="+binary)
	version := exec.CommandContext(ctx, launcher, "--version")
	version.Env, version.Dir = environment, workspace
	versionOutput, err := version.CombinedOutput()
	if err != nil {
		t.Fatalf("native CLI version probe failed: %v", err)
	}
	t.Logf("native CLI: %s", strings.TrimSpace(string(versionOutput)))
	// 仅给fixture选择假Provider和明确系统输入；保留原生工具及配置加载。
	args := []string{"-c", "model_instructions_file=" + strconv.Quote(filepath.Join(nativeHome, "instructions.md"))}
	if phase == "final_answer" {
		management, err := native.OpenManagement(ctx, func(ctx context.Context) (sdk.Agent, error) {
			return native.NewAgent(ctx, native.AgentConfig{RuntimeID: "codex", Executable: launcher, PrefixArgs: args, Environment: environment, WorkingDirectory: workspace, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		}, native.ManagementConfig{})
		if err != nil {
			if management != nil {
				_ = management.Close(context.Background())
			}
			t.Fatal(err)
		}
		probe, err := management.Probe(ctx, native.ProbeRequest{Workspace: native.WorkspaceScope{Root: workspace}, Model: "fixture-model"})
		closeErr := management.Close(context.Background())
		if err != nil || closeErr != nil || !probe.SessionReady || len(requests) != 0 {
			t.Fatalf("native Codex management session_ready=%v prompts=%d error=%v close_error=%v", probe.SessionReady, len(requests), err, closeErr)
		}
		t.Logf("native codex management initialized=true session_ready=true models=%d prompts=0", len(probe.Models))
	}
	factory := func(ctx context.Context, request native.OpenRequest) (sdk.Agent, error) {
		return native.NewAgent(ctx, native.AgentConfig{RuntimeID: "codex", Executable: launcher, PrefixArgs: args, Environment: environment, WorkingDirectory: request.Workspace.Root, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}
	rt := newNativeIntegrationManager(t, factory, nil)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := rt.Close(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	rt.Start(ctx)
	declaration := nativeIntegrationPlan(workspace)
	declaration.Policy.Model = "fixture-model"
	input := TurnInput{Message: Message{Text: "EXACT_USER_FIXTURE"}}
	directive := SessionDirective{Mode: SessionModeNew}
	actual := SessionResources{Target: RuntimeTarget{RuntimeID: "codex", Isolated: true, CloseAfterRun: phase != "session_resume"}}
	var result turnResult
	if phase == "session_resume" {
		first := runNativeIntegration(t, rt, ctx, declaration, directive, input, actual, resourcePreparation{}, nil, true)
		if first.Status != native.StatusCompleted || !first.NativeSession.Resumable || first.NativeSession.Handle == "" || first.Fault != nil || first.CleanupFault != nil {
			t.Fatalf("native first turn has no resumable native identity: %+v", first)
		}
		input.Message.Text = "SECOND_USER_FIXTURE"
		directive = SessionDirective{Mode: SessionModeResume, ID: first.NativeSession.Handle}
		second := runNativeIntegration(t, rt, ctx, declaration, directive, input, actual, resourcePreparation{}, nil, true)
		if second.Status != native.StatusCompleted || !second.NativeSession.Resumable || second.NativeSession.Handle != first.NativeSession.Handle || second.Invocation.SessionID != first.Invocation.SessionID || second.Invocation.Revision != 2 || second.Fault != nil || second.CleanupFault != nil {
			t.Fatalf("native hot continuation failed: %+v", second)
		}
		if err := rt.CloseWorkspace(ctx, workspace); err != nil {
			t.Fatal(err)
		}
		input.Message.Text = "THIRD_USER_FIXTURE"
		directive = SessionDirective{Mode: SessionModeResume, ID: second.NativeSession.Handle}
		result = runNativeIntegration(t, rt, ctx, declaration, directive, input, actual, resourcePreparation{}, nil, true)
		if !result.NativeSession.Resumable || result.NativeSession.Handle != first.NativeSession.Handle || result.Invocation.SessionID == first.Invocation.SessionID || result.Invocation.Revision != 1 || result.Fault != nil || result.CleanupFault != nil {
			t.Fatalf("native cold continuation failed: %+v", result)
		}
		t.Log("native codex hot revisions=1,2 closed=true cold_revision=1 native_handle_preserved=true")
	} else {
		result = runNativeIntegration(t, rt, ctx, declaration, directive, input, actual, resourcePreparation{}, nil, true)
	}
	select {
	case payload := <-requests:
		observed := []map[string]any{payload}
		for len(requests) > 0 {
			observed = append(observed, <-requests)
		}
		expected := 1
		if phase == "tool_then_final" {
			expected = 2
		}
		if phase == "session_resume" {
			expected = 3
		}
		if len(observed) != expected {
			t.Errorf("model requests = %d, want %d", len(observed), expected)
		}
		if payload["instructions"] != "EXPLICIT_SYSTEM_FIXTURE" && !nativeCodexExactMessage(payload["input"], "developer", "EXPLICIT_SYSTEM_FIXTURE") {
			t.Error("task instructions were not delivered")
		}
		if !nativeCodexExactMessage(payload["input"], "user", "EXACT_USER_FIXTURE") {
			t.Error("native input lost user text")
		}
		return nativeCodexObservation{requests: observed, result: result}
	default:
		t.Fatalf("native CLI did not reach fixture: %+v", result)
	}
	return nativeCodexObservation{}
}

// writeNativeCodexToolResponse 提供一次无外部副作用的shell调用，随后由CLI继续请求最终结果。
func writeNativeCodexToolResponse(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "text/event-stream")
	args := map[string]any{"command": "printf TOOL_FIXTURE_OK"}
	if name == "exec_command" {
		args = map[string]any{"cmd": "printf TOOL_FIXTURE_OK"}
	}
	arguments, _ := json.Marshal(args)
	commentary := map[string]any{"id": "msg_process", "type": "message", "role": "assistant", "phase": "commentary", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "INTERMEDIATE_FIXTURE", "annotations": []any{}}}}
	tool := map[string]any{"id": "fc_fixture", "type": "function_call", "call_id": "call_fixture", "name": name, "arguments": string(arguments), "status": "completed"}
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_tool", "object": "response", "status": "in_progress", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": commentary},
		{"type": "response.output_item.done", "output_index": 0, "item": commentary},
		{"type": "response.output_item.added", "output_index": 1, "item": tool},
		{"type": "response.output_item.done", "output_index": 1, "item": tool},
		{"type": "response.completed", "response": map[string]any{"id": "resp_tool", "object": "response", "status": "completed", "output": []any{commentary, tool}, "usage": map[string]any{"input_tokens": 2, "output_tokens": 3, "total_tokens": 5}}},
	}
	for sequence, event := range events {
		event["sequence_number"] = sequence
		encoded, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], encoded)
	}
}

// writeNativeCodexResponse 发送完整Responses事件，phase只来自本次明确测试输入。
func writeNativeCodexResponse(w http.ResponseWriter, phase string) {
	w.Header().Set("Content-Type", "text/event-stream")
	part := map[string]any{"type": "output_text", "text": "FINAL_FIXTURE_OK", "annotations": []any{}}
	message := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{part}}
	started := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
	if phase == "late_final" {
		message["phase"] = "final_answer"
		started["phase"] = "commentary"
	} else if phase != "" {
		message["phase"] = phase
		started["phase"] = phase
	}
	response := map[string]any{"id": "resp_fixture", "object": "response", "created_at": 1792573200, "model": "fixture-model", "status": "completed", "output": []any{message}, "usage": map[string]any{"input_tokens": 2, "output_tokens": 3, "total_tokens": 5}}
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_fixture", "object": "response", "status": "in_progress", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": started},
		{"type": "response.content_part.added", "item_id": "msg_fixture", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}},
		{"type": "response.output_text.delta", "item_id": "msg_fixture", "output_index": 0, "content_index": 0, "delta": "FINAL_FIXTURE_OK"},
		{"type": "response.output_text.done", "item_id": "msg_fixture", "output_index": 0, "content_index": 0, "text": "FINAL_FIXTURE_OK"},
		{"type": "response.content_part.done", "item_id": "msg_fixture", "output_index": 0, "content_index": 0, "part": part},
		{"type": "response.output_item.done", "output_index": 0, "item": message},
		{"type": "response.completed", "response": response},
	}
	for sequence, event := range events {
		event["sequence_number"] = sequence
		encoded, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], encoded)
	}
}

// nativeCodexExactMessage 检查真实模型消息角色和原文，不接受宿主用XML伪造角色。
func nativeCodexExactMessage(input any, role, text string) bool {
	items, _ := input.([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["role"] != role {
			continue
		}
		content, _ := item["content"].([]any)
		for _, rawPart := range content {
			part, _ := rawPart.(map[string]any)
			if part["type"] == "input_text" && part["text"] == text {
				return true
			}
		}
	}
	return false
}

// TestNativeCLIResult 使用真实二进制和本地模型服务验证公开工厂到最终结果的完整链路。
// 每类由绝对路径环境变量单独启用，普通测试不发现、不启动用户安装的CLI。
func TestNativeCLIResult(t *testing.T) {
	for _, name := range []string{"claude", "gemini", "opencode", "kimi", "pi"} {
		t.Run(name, func(t *testing.T) {
			fixture := newNativeCLIFixture(t, name, false)
			got := fixture.invoke(t, context.Background())
			t.Logf("native result: %+v", got)
			if got.Fault != nil {
				t.Logf("native fault cause: %v", got.Fault.Cause)
			}
			fixture.assertProviderReached(t)
			if got.Status != native.StatusCompleted || got.Fault != nil || got.CleanupFault != nil || got.Output.Status != OutputAvailable || got.Output.Text != "FINAL_FIXTURE_OK" || got.Output.Reliability != ReliabilityVerifiedCompletion {
				t.Fatalf("native final result = %+v; diagnostics: %s", got, fixture.diagnostics.String())
			}
		})
	}
}

// TestNativeCLIClaudeThinkingResult 捕获真实 Claude 思考与正文的消息边界被错误拒绝的回归。
func TestNativeCLIClaudeThinkingResult(t *testing.T) {
	fixture := newNativeCLIFixture(t, "claude", false)
	fixture.thinking = true
	got := fixture.invoke(t, context.Background())
	fixture.assertProviderReached(t)
	if got.Status != native.StatusCompleted || got.Fault != nil || got.CleanupFault != nil || got.Output.Status != OutputAvailable || got.Output.Text != "FINAL_FIXTURE_OK" || got.Output.Reliability != ReliabilityVerifiedCompletion {
		t.Fatalf("thinking and answer did not complete: %+v; diagnostics: %s", got, fixture.diagnostics.String())
	}
}

// TestNativeCLIUnconfiguredStartup 记录未登录临时配置的真实启动边界，不宣称模型联通。
func TestNativeCLIUnconfiguredStartup(t *testing.T) {
	for _, name := range []string{"workbuddy", "qoder", "cursor"} {
		t.Run(name, func(t *testing.T) {
			fixture := newNativeCLIFixture(t, name, false)
			got := fixture.invoke(t, context.Background())
			if got.Status != native.StatusFailed || got.Fault == nil || got.Fault.Code != native.FaultPreparation || got.Fault.Stage != "new_session" || got.Effects.PromptAccepted || got.Output.Text != "" || got.CleanupFault != nil {
				t.Fatalf("unexpected unconfigured native startup result: %+v", got)
			}
			assertNativeCLIStartupBarrier(t, name, got.Fault.Cause)
			fixture.mutex.Lock()
			requests := len(fixture.requests)
			fixture.mutex.Unlock()
			if requests != 0 {
				t.Fatalf("unconfigured native unexpectedly reached model provider %d times", requests)
			}
			t.Logf("native %s stopped at %s before Prompt: %v", name, got.Fault.Stage, got.Fault.Cause)
		})
	}
}

// TestNativeCLICancellation 在真实模型请求尚未完成时取消，不把未完成调用作为最终结果。
func TestNativeCLICancellation(t *testing.T) {
	for _, name := range []string{"claude", "gemini", "opencode", "kimi", "pi"} {
		t.Run(name, func(t *testing.T) {
			fixture := newNativeCLIFixture(t, name, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				select {
				case <-fixture.reached:
					cancel()
				case <-ctx.Done():
				}
			}()
			got := fixture.invoke(t, ctx)
			fixture.assertProviderReached(t)
			if got.Status != native.StatusCancelled || got.Output.Text != "" || got.Output.Status == OutputAvailable || got.CleanupFault != nil {
				t.Fatalf("native cancellation = %+v; diagnostics: %s", got, fixture.diagnostics.String())
			}
		})
	}
}

// TestNativeCLIManagementProbe 验证真实管理连接经过统一工厂，读取目录期间不发送模型Prompt。
func TestNativeCLIManagementProbe(t *testing.T) {
	for _, name := range []string{"claude", "gemini", "opencode", "kimi", "pi", "workbuddy", "qoder", "cursor"} {
		t.Run(name, func(t *testing.T) {
			f := newNativeCLIFixture(t, name, false)
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			management, err := native.OpenManagement(ctx, f.newAgent, native.ManagementConfig{})
			if management != nil {
				t.Cleanup(func() {
					closeCtx, closeCancel := context.WithTimeout(context.Background(), 6*time.Second)
					defer closeCancel()
					if closeErr := management.Close(closeCtx); closeErr != nil {
						t.Error(closeErr)
					}
				})
			}
			if err != nil {
				t.Fatalf("native management initialization failed: %v; diagnostics: %s", err, f.diagnostics.String())
			}
			probe, err := management.Probe(ctx, native.ProbeRequest{Workspace: native.WorkspaceScope{Root: f.workspace}, Model: f.model, AllowEmptyModelCatalog: true})
			f.mutex.Lock()
			requests := len(f.requests)
			f.mutex.Unlock()
			if requests != 0 {
				t.Fatalf("management probe sent %d model prompts", requests)
			}
			if name == "workbuddy" || name == "qoder" || name == "cursor" {
				if ctx.Err() != nil || probe.SessionReady {
					t.Fatalf("unexpected unconfigured native probe session_ready=%v context_error=%v error=%v", probe.SessionReady, ctx.Err(), err)
				}
				assertNativeCLIStartupBarrier(t, name, err)
				t.Logf("native %s management initialized=true session_ready=false stopped before Prompt: %v", name, err)
				return
			}
			if err != nil || !probe.SessionReady {
				t.Fatalf("native management initialized=true session_ready=%v error=%v; diagnostics: %s", probe.SessionReady, err, f.diagnostics.String())
			}
			t.Logf("native %s management initialized=true session_ready=true model=%s models=%d prompts=0", name, f.model, len(probe.Models))
		})
	}
}

// assertNativeCLIStartupBarrier 只接受已经确认的原生门槛，拒绝取消、超时与其他协议错误。
func assertNativeCLIStartupBarrier(t *testing.T, name string, err error) {
	t.Helper()
	var requestError *sdk.RequestError
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &requestError) {
		t.Fatalf("native %s did not return the expected session barrier: %v", name, err)
	}
	want := sdk.NewAuthRequired(nil)
	switch name {
	case "workbuddy":
	case "qoder":
		want.Message = "Authentication required: Authentication is required."
	case "cursor":
		want = sdk.NewInternalError(nil)
		data, _ := requestError.Data.(map[string]any)
		if data["message"] != "Failed to initialize session services" {
			t.Fatalf("native cursor returned an unexpected session error: %v", err)
		}
	default:
		t.Fatalf("native %s has no accepted unconfigured session barrier", name)
	}
	if requestError.Code != want.Code || requestError.Message != want.Message {
		t.Fatalf("native %s returned an unexpected session barrier: %v", name, err)
	}
}

// nativeCLIFixture 只拥有临时配置、假凭据和本地服务，不读取用户账号或仓库配置。
type nativeCLIFixture struct {
	name        string           // name 是接受验证的原生CLI家族。
	binary      string           // binary 是用户显式启用的绝对可执行路径。
	root        string           // root 是所有测试状态的临时根目录。
	workspace   string           // workspace 是没有宿主项目配置的原生cwd。
	environment []string         // environment 是完全替换继承环境的测试环境。
	model       string           // model 是本地假Provider中的原生模型标识。
	reached     chan struct{}    // reached 在首个模型请求到达本地服务时关闭。
	diagnostics nativeCLILog     // diagnostics 保存公开Adapter诊断，不写真实账号内容。
	mutex       sync.Mutex       // mutex 保护模型请求记录。
	requests    []map[string]any // requests 记录实际CLI向本地服务发出的请求。
	thinking    bool             // thinking 要求本地模型先发送思考块，再发送普通正文。
}

// nativeCLILog 使CLI的并发诊断可以在失败时安全读取。
type nativeCLILog struct {
	mutex sync.Mutex      // mutex 保护缓冲区。
	text  strings.Builder // text 保存本次临时进程的诊断。
}

func (l *nativeCLILog) Write(p []byte) (int, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.text.Write(p)
}
func (l *nativeCLILog) String() string {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.text.String()
}

// newNativeCLIFixture 仅生成Provider、认证与模型路由配置，保留原生工具和配置加载。
func newNativeCLIFixture(t *testing.T, name string, hold bool) *nativeCLIFixture {
	t.Helper()
	binary := os.Getenv("NEUX_RUNTIME_" + strings.ToUpper(name) + "_CLI")
	if binary == "" {
		t.Skip("set NEUX_RUNTIME_" + strings.ToUpper(name) + "_CLI for explicit native acceptance")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("native acceptance requires an absolute CLI path")
	}
	root := t.TempDir()
	f := &nativeCLIFixture{name: name, binary: binary, root: root, workspace: filepath.Join(root, "workspace"), model: "fixture-model", reached: make(chan struct{})}
	home := filepath.Join(root, "home")
	for _, path := range []string{home, f.workspace} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.environment = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home, "TMPDIR=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_DATA_HOME=" + filepath.Join(root, "data"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "XDG_STATE_HOME=" + filepath.Join(root, "state"), "NO_PROXY=127.0.0.1,localhost", "no_proxy=127.0.0.1,localhost", "HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "ALL_PROXY=http://127.0.0.1:1"}
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if strings.Contains(r.URL.Path, "count_tokens") || strings.Contains(r.URL.Path, "countTokens") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"input_tokens":2,"totalTokens":2}`)
			return
		}
		if !strings.Contains(r.URL.Path, "messages") && !strings.Contains(r.URL.Path, "generateContent") && !strings.Contains(r.URL.Path, "streamGenerateContent") {
			t.Errorf("unexpected native route %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		f.mutex.Lock()
		f.requests = append(f.requests, payload)
		f.mutex.Unlock()
		once.Do(func() { close(f.reached) })
		if hold {
			<-r.Context().Done()
			return
		}
		if name == "gemini" {
			writeNativeGoogleResponse(w, r.URL.Query().Get("alt") == "sse")
			return
		}
		writeNativeAnthropicResponse(w, payload["model"], f.thinking)
	}))
	t.Cleanup(provider.Close)
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	add := func(key, value string) { f.environment = append(f.environment, key+"="+value) }
	switch name {
	case "claude":
		f.model = "claude-sonnet-4-5-20250929"
		add("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
		add("ANTHROPIC_API_KEY", "fixture-key")
		add("ANTHROPIC_BASE_URL", provider.URL)
		add("ANTHROPIC_MODEL", f.model)
		add("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	case "gemini":
		f.model = "gemini-3.8-flash"
		add("GEMINI_CLI_HOME", home)
		add("GEMINI_API_KEY", "fixture-key")
		add("GOOGLE_GEMINI_BASE_URL", provider.URL)
		add("GOOGLE_GENAI_API_VERSION", "v1beta")
		write(filepath.Join(home, ".gemini", "settings.json"), `{"security":{"auth":{"selectedType":"gemini-api-key"}},"model":{"name":"gemini-3.8-flash"}}`)
		resolved, _ := filepath.EvalSymlinks(f.workspace)
		trust, _ := json.Marshal(map[string]string{resolved: "TRUST_FOLDER"})
		write(filepath.Join(home, ".gemini", "trustedFolders.json"), string(trust))
	case "opencode":
		f.model = "fixture/fixture-model"
		configuration, _ := json.Marshal(map[string]any{"model": f.model, "enabled_providers": []string{"fixture"}, "provider": map[string]any{"fixture": map[string]any{"npm": "@ai-sdk/anthropic", "name": "Fixture", "options": map[string]string{"baseURL": provider.URL + "/v1", "apiKey": "fixture-key"}, "models": map[string]any{"fixture-model": map[string]string{"name": "Fixture"}}, "whitelist": []string{"fixture-model"}}}})
		path := filepath.Join(root, "opencode.json")
		write(path, string(configuration))
		add("OPENCODE_CONFIG", path)
		add("OPENCODE_CONFIG_DIR", filepath.Join(root, "opencode"))
	case "kimi":
		path := filepath.Join(root, "kimi")
		add("KIMI_SHARE_DIR", path)
		add("KIMI_CODE_HOME", path)
		write(filepath.Join(path, "config.toml"), "default_model = \"fixture-model\"\n[providers.fixture]\ntype = \"anthropic\"\nbase_url = "+strconv.Quote(provider.URL)+"\napi_key = \"fixture-key\"\n[models.fixture-model]\nprovider = \"fixture\"\nmodel = \"fixture-model\"\nmax_context_size = 32768\n")
	case "pi":
		path := filepath.Join(root, "pi")
		add("PI_CODING_AGENT_DIR", path)
		add("ACP_GO_PI_PREFERRED_PROVIDER", "fixture")
		bridge := os.Getenv("NEUX_RUNTIME_PI_BRIDGE")
		if bridge == "" || !filepath.IsAbs(bridge) {
			t.Fatal("native Pi acceptance requires an absolute NEUX_RUNTIME_PI_BRIDGE")
		}
		add("PI_MCP_MODULE_PATH", bridge)
		configuration, _ := json.Marshal(map[string]any{"providers": map[string]any{"fixture": map[string]any{"baseUrl": provider.URL, "api": "anthropic-messages", "apiKey": "fixture-key", "models": []map[string]string{{"id": f.model}}}}})
		write(filepath.Join(path, "models.json"), string(configuration))
	case "workbuddy":
		add("CODEBUDDY_CONFIG_DIR", filepath.Join(root, "codebuddy"))
	case "qoder":
		add("QODER_CONFIG_DIR", filepath.Join(root, "qoder"))
	case "cursor":
		add("CURSOR_CONFIG_DIR", filepath.Join(root, "cursor"))
		add("CURSOR_API_KEY", "fixture-key")
	}
	// 部分原生版本探测不会设置cwd；启动器也把这类子命令固定在临时工作区。
	launcher := filepath.Join(root, "native-cli")
	write(launcher, "#!/bin/sh\ncd \"$NEUX_FIXTURE_CWD\" || exit 70\nexec \"$NEUX_FIXTURE_BINARY\" \"$@\"\n")
	if err := os.Chmod(launcher, 0700); err != nil {
		t.Fatal(err)
	}
	add("NEUX_FIXTURE_CWD", f.workspace)
	add("NEUX_FIXTURE_BINARY", binary)
	f.binary = launcher
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, launcher, "--version")
	command.Env = f.environment
	command.Dir = f.workspace
	version, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native version failed: %v: %s", err, version)
	}
	t.Logf("native %s: %s", name, strings.TrimSpace(string(version)))
	return f
}

// invoke 通过产品Runtime和公开Adapter运行一次输入，不安装指令、技能或MCP。
func (f *nativeCLIFixture) invoke(t *testing.T, parent context.Context) turnResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	manager := NewManager(ManagerConfig{
		Launch: func(RuntimeTarget, DetectionResult) (LaunchFacts, error) {
			return LaunchFacts{BaseRuntimeID: f.name, Executable: "fixture"}, nil
		},
		Open: func(ctx context.Context, config native.ConnectionConfig, request native.OpenRequest) (NativeConnection, error) {
			config.Factory = func(ctx context.Context, _ native.OpenRequest) (sdk.Agent, error) { return f.newAgent(ctx) }
			config.CompletionPolicy = native.CompletionLastAssistant
			return native.Open(ctx, config, request)
		},
	})
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer closeCancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Error(err)
		}
	})
	manager.Start(ctx)
	declaration := nativeIntegrationPlan(f.workspace)
	declaration.Target = DeclareTarget(RuntimeTarget{RuntimeID: f.name, Isolated: true})
	declaration.Detection.Runtime = RuntimeSummary{ID: f.name, BaseRuntimeID: f.name}
	declaration.Policy.Model = f.model
	return runNativeIntegration(t, manager, ctx, declaration, SessionDirective{Mode: SessionModeNew}, TurnInput{Message: Message{Text: "EXACT_USER_FIXTURE"}}, SessionResources{Target: RuntimeTarget{RuntimeID: f.name, Isolated: true, CloseAfterRun: true}}, resourcePreparation{}, nil, true)
}

// newAgent 与执行和管理入口共享唯一原生工厂，保留各CLI状态目录与Pi桥接配置。
func (f *nativeCLIFixture) newAgent(ctx context.Context) (sdk.Agent, error) {
	return native.NewAgent(ctx, native.AgentConfig{RuntimeID: f.name, Executable: f.binary, Environment: f.environment, WorkingDirectory: f.workspace, StateDirectory: filepath.Join(f.root, f.name+"-state"), PermissionMode: "full-access", Logger: slog.New(slog.NewTextHandler(&f.diagnostics, nil))})
}

// assertProviderReached 确认真正的模型请求含原始用户输入，并只使用虚构Provider。
func (f *nativeCLIFixture) assertProviderReached(t *testing.T) {
	t.Helper()
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if len(f.requests) == 0 {
		t.Fatalf("native %s did not reach loopback model provider; diagnostics: %s", f.name, f.diagnostics.String())
	}
	found := false
	for _, r := range f.requests {
		encoded, _ := json.Marshal(r)
		if strings.Contains(string(encoded), "EXACT_USER_FIXTURE") {
			found = true
		}
	}
	if !found {
		t.Error("native model request lost the original user input")
	}
	t.Logf("native %s model requests: %d", f.name, len(f.requests))
}

// writeNativeAnthropicResponse 发送无工具调用的完整原生Messages流。
func writeNativeAnthropicResponse(w http.ResponseWriter, model any, thinking bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 2, "output_tokens": 0}}},
	}
	index := 0
	if thinking {
		events = append(events,
			map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "thinking", "thinking": "", "signature": ""}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "thinking_delta", "thinking": "fixture reasoning"}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "signature_delta", "signature": "fixture-signature"}},
			map[string]any{"type": "content_block_stop", "index": 0},
		)
		index++
	}
	events = append(events,
		map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]string{"type": "text", "text": ""}},
		map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]string{"type": "text_delta", "text": "FINAL_FIXTURE_OK"}},
		map[string]any{"type": "content_block_stop", "index": index},
		map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 3}},
		map[string]any{"type": "message_stop"},
	)
	for _, event := range events {
		encoded, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], encoded)
	}
}

// writeNativeGoogleResponse 发送官方Google SDK可消费的完整生成事件。
func writeNativeGoogleResponse(w http.ResponseWriter, stream bool) {
	response := map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]string{"text": "FINAL_FIXTURE_OK"}}}, "finishReason": "STOP", "index": 0}}, "usageMetadata": map[string]int{"promptTokenCount": 2, "candidatesTokenCount": 3, "totalTokenCount": 5}, "modelVersion": "gemini-3.8-flash"}
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	encoded, _ := json.Marshal(response)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}
