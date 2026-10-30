package acp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/internal/plan"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	driver "github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/core/driver/acp/native"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// nativeCodexObservation 保留真实CLI的模型请求和最终结果，不检查其原生配置与工具清单。
type nativeCodexObservation struct {
	requests []map[string]any // requests 保存同一回合的模型请求，工具循环不是宿主重放。
	result   contract.Result  // result 是真实Runtime与公开ACP连接返回的结果。
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
	if got.result.Status != contract.StatusCompleted || got.result.Fault != nil || got.result.CleanupFault != nil || got.result.Output.Text != "FINAL_FIXTURE_OK" || got.result.Output.Reliability != contract.ReliabilityVerifiedPhase {
		t.Fatalf("tool loop did not yield final-only result: %+v", got.result)
	}
}

// TestNativeCodexSessionResume 验证两轮热续聊及关闭后的原生检查点恢复，不重建用户历史。
func TestNativeCodexSessionResume(t *testing.T) {
	got := runNativeCodexFixture(t, "session_resume")
	if len(got.requests) != 3 || got.result.Status != contract.StatusCompleted || got.result.Output.Text != "FINAL_FIXTURE_OK" || got.result.Output.Reliability != contract.ReliabilityVerifiedPhase {
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
	if got.result.Status != contract.StatusCancelled || got.result.Output.Text != "" || got.result.Output.Status == contract.OutputAvailable || got.result.CleanupFault != nil {
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
			if got.result.Status != contract.StatusCompleted || got.result.Fault != nil || got.result.CleanupFault != nil {
				t.Fatalf("native execution failed: %+v", got.result)
			}
			if phase == "final_answer" || phase == "late_final" {
				if got.result.Output.Status != contract.OutputAvailable || got.result.Output.Text != "FINAL_FIXTURE_OK" || got.result.Output.Reliability != contract.ReliabilityVerifiedPhase {
					t.Fatalf("explicit native final evidence unavailable: %+v", got.result.Output)
				}
			} else if got.result.Output.Status != contract.OutputUnavailable || got.result.Output.Text != "" {
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
		management, err := driver.OpenManagement(ctx, func(ctx context.Context) (sdk.Agent, error) {
			return native.New(ctx, native.Config{RuntimeID: "codex", Executable: launcher, PrefixArgs: args, Environment: environment, WorkingDirectory: workspace, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		}, driver.ManagementConfig{})
		if err != nil {
			if management != nil {
				_ = management.Close(context.Background())
			}
			t.Fatal(err)
		}
		probe, err := management.Probe(ctx, driver.ProbeRequest{Workspace: contract.WorkspaceScope{Root: workspace}, Model: "fixture-model"})
		closeErr := management.Close(context.Background())
		if err != nil || closeErr != nil || !probe.SessionReady || len(requests) != 0 {
			t.Fatalf("native Codex management session_ready=%v prompts=%d error=%v close_error=%v", probe.SessionReady, len(requests), err, closeErr)
		}
		t.Logf("native codex management initialized=true session_ready=true models=%d prompts=0", len(probe.Models))
	}
	d, err := driver.New(driver.Config{ID: "codex", Factory: func(ctx context.Context, request contract.OpenRequest) (sdk.Agent, error) {
		return native.New(ctx, native.Config{RuntimeID: "codex", Executable: launcher, PrefixArgs: args, Environment: environment, WorkingDirectory: request.Workspace.Root, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := newRuntimeFixture(runtimeFixtureConfig{Drivers: []contract.Driver{d}, Grants: contract.Grants{Directories: []string{workspace}, AllowAllTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := rt.Close(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// 输出收集策略与工具、原生配置策略独立；真实工具循环保持启用。
	spec := plan.Spec{Profile: contract.Profile{DriverID: d.ID(), Model: "fixture-model", NativeMode: "agent-full-access"}, Input: contract.Input{Text: "EXACT_USER_FIXTURE"}, Workspace: contract.WorkspaceScope{Root: workspace}, Policies: plan.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}}
	var result contract.Result
	if phase == "session_resume" {
		first := runRuntime(rt, ctx, spec, contract.RunOptions{})
		if first.Status != contract.StatusCompleted || !first.NativeSession.Resumable || first.NativeSession.Handle == "" || first.Fault != nil || first.CleanupFault != nil {
			t.Fatalf("native first turn has no resumable native identity: %+v", first)
		}
		spec.Input.Text = "SECOND_USER_FIXTURE"
		spec.Session = contract.SessionDirective{Mode: contract.SessionResume, NativeHandle: first.NativeSession.Handle}
		second := runRuntime(rt, ctx, spec, contract.RunOptions{})
		if second.Status != contract.StatusCompleted || !second.NativeSession.Resumable || second.NativeSession.Handle != first.NativeSession.Handle || second.Invocation.SessionID != first.Invocation.SessionID || second.Invocation.Revision != 2 || second.Fault != nil || second.CleanupFault != nil {
			t.Fatalf("native hot continuation failed: %+v", second)
		}
		if err := rt.CloseIdle(ctx, "fixture"); err != nil {
			t.Fatal(err)
		}
		spec.Input.Text = "THIRD_USER_FIXTURE"
		spec.Session = contract.SessionDirective{Mode: contract.SessionResume, NativeHandle: second.NativeSession.Handle}
		result = runRuntime(rt, ctx, spec, contract.RunOptions{})
		if !result.NativeSession.Resumable || result.NativeSession.Handle != first.NativeSession.Handle || result.Invocation.SessionID == first.Invocation.SessionID || result.Invocation.Revision != 1 || result.Fault != nil || result.CleanupFault != nil {
			t.Fatalf("native cold continuation failed: %+v", result)
		}
		t.Log("native codex hot revisions=1,2 closed=true cold_revision=1 native_handle_preserved=true")
	} else {
		result = invokeRuntime(rt, ctx, spec)
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
