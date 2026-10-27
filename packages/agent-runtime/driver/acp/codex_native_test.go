package acp_test

import (
	"context"
	"encoding/json"
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
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	driver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
)

// nativeCodexObservation 保留真实CLI请求和实际内核结果，不使用账号或项目内容。
type nativeCodexObservation struct {
	payload             map[string]any  // payload 是本地Provider实际收到的模型请求。
	result              contract.Result // result 是真实Runtime与公开ACP连接返回的结果。
	inheritedMCPStarted bool            // inheritedMCPStarted 表示继承配置中的MCP命令实际执行。
}

// TestNativeCodexConfigurationIsolation 检查候选启动配置下的真实请求，不能单独证明禁工具。
func TestNativeCodexConfigurationIsolation(t *testing.T) {
	got := runNativeCodexFixture(t, "final_answer")
	if got.inheritedMCPStarted {
		t.Error("native CLI executed MCP from inherited project configuration")
	}
	encoded, err := json.Marshal(got.payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"GLOBAL_POISON", "PROJECT_POISON", "SKILL_POISON", "MCP_POISON", "CATALOG_POISON"} {
		if strings.Contains(string(encoded), marker) {
			t.Errorf("native configuration leaked %s", marker)
		}
	}
	if got.payload["instructions"] != "EXPLICIT_SYSTEM_FIXTURE" && !nativeCodexExactMessage(got.payload["input"], "developer", "EXPLICIT_SYSTEM_FIXTURE") {
		t.Errorf("explicit instructions changed: %v", got.payload["instructions"])
	}
	if !nativeCodexExactMessage(got.payload["input"], "user", "EXACT_USER_FIXTURE") {
		t.Error("native input lost user text")
	}
	if got.result.Status != contract.StatusCompleted || got.result.Fault != nil || got.result.CleanupFault != nil {
		t.Fatalf("native execution failed: %+v", got.result)
	}
}

// TestNativeCodexDenyAllGate 是生产切换门槛；声明任何工具都失败，事后取消不能使它通过。
func TestNativeCodexDenyAllGate(t *testing.T) {
	got := runNativeCodexFixture(t, "final_answer")
	tools, ok := got.payload["tools"].([]any)
	if !ok {
		t.Fatalf("provider request has no verifiable tools array: %T", got.payload["tools"])
	}
	var names []string
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("invalid tool definition: %T", raw)
		}
		name, _ := tool["name"].(string)
		if name == "" {
			name, _ = tool["type"].(string)
		}
		names = append(names, name)
	}
	if len(tools) != 0 {
		t.Fatalf("deny_all not satisfied; native tool declarations: %v", names)
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
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".codex", "config.toml"), "developer_instructions = \"GLOBAL_POISON\"\n")
	write(filepath.Join(home, ".codex", "AGENTS.md"), "GLOBAL_POISON")
	write(filepath.Join(home, ".agents", "skills", "poison", "SKILL.md"), "---\nname: poison\ndescription: SKILL_POISON\n---\nSKILL_POISON")
	write(filepath.Join(workspace, "AGENTS.md"), "PROJECT_POISON")
	mcpMarker := filepath.Join(root, "mcp-started")
	mcpArgs, err := json.Marshal([]string{"-c", "printf MCP_POISON > \"$1\"", "fixture", mcpMarker})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(workspace, ".codex", "config.toml"), "developer_instructions = \"PROJECT_POISON\"\n[mcp_servers.poison]\ncommand = \"/bin/sh\"\nargs = "+string(mcpArgs)+"\n")
	requests := make(chan map[string]any, 4)
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
		writeNativeCodexResponse(w, phase)
	}))
	defer provider.Close()
	catalog := `{"models":[{"slug":"fixture-model","display_name":"fixture-model","description":"fixture","base_instructions":"CATALOG_POISON","visibility":"list","priority":1,"supported_in_api":true,"context_window":32768,"max_context_window":32768,"effective_context_window_percent":95,"input_modalities":["text"],"supports_reasoning_summaries":false,"supports_parallel_tool_calls":false,"support_verbosity":false,"shell_type":"shell_command","experimental_supported_tools":[],"default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low","description":"low"}],"default_reasoning_summary":"none","truncation_policy":{"mode":"bytes","limit":10000}}]}`
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
	args := []string{"-c", "project_doc_max_bytes=0", "-c", "skills.bundled.enabled=false", "-c", "skills.include_instructions=false", "-c", `developer_instructions=""`, "-c", `web_search="disabled"`, "-c", "model_instructions_file=" + strconv.Quote(filepath.Join(nativeHome, "instructions.md"))}
	for _, feature := range []string{"shell_tool", "hooks", "plugins", "apps", "memories", "multi_agent", "browser_use", "computer_use", "image_generation", "view_image", "skill_search", "goals", "default_mode_request_user_input", "collaboration_modes"} {
		args = append(args, "-c", "features."+feature+"=false")
	}
	d, err := driver.NewCodex(driver.CodexConfig{CodexPath: launcher, PrefixArgs: args, Environment: environment, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{d}, Grants: contract.Grants{Directories: []string{workspace}, AllowAllTools: true}})
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
	// 显式允许观察候选配置的原生行为；此调用绝不声明已经支持deny_all或生产隔离。
	result := rt.Invoke(ctx, contract.ExecutionSpec{Profile: contract.Profile{DriverID: d.ID(), Model: "fixture-model"}, Input: contract.Input{Text: "EXACT_USER_FIXTURE"}, Workspace: contract.WorkspaceScope{Root: workspace}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}})
	select {
	case payload := <-requests:
		if len(requests) != 0 {
			t.Error("native prompt unexpectedly repeated the model request")
		}
		_, markerErr := os.Stat(mcpMarker)
		return nativeCodexObservation{payload: payload, result: result, inheritedMCPStarted: markerErr == nil}
	default:
		t.Fatalf("native CLI did not reach fixture: %+v", result)
	}
	return nativeCodexObservation{}
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
