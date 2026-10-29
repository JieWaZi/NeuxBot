package acp_test

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
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	driver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	"github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp/native"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

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
			if got.Status != contract.StatusCompleted || got.Fault != nil || got.CleanupFault != nil || got.Output.Status != contract.OutputAvailable || got.Output.Text != "FINAL_FIXTURE_OK" || got.Output.Reliability != contract.ReliabilityVerifiedCompletion {
				t.Fatalf("native final result = %+v; diagnostics: %s", got, fixture.diagnostics.String())
			}
		})
	}
}

// TestNativeCLIUnconfiguredStartup 记录未登录临时配置的真实启动边界，不宣称模型联通。
func TestNativeCLIUnconfiguredStartup(t *testing.T) {
	for _, name := range []string{"workbuddy", "qoder", "cursor"} {
		t.Run(name, func(t *testing.T) {
			fixture := newNativeCLIFixture(t, name, false)
			got := fixture.invoke(t, context.Background())
			if got.Status != contract.StatusFailed || got.Fault == nil || got.Fault.Code != contract.FaultPreparation || got.Fault.Stage != "new_session" || got.Effects.PromptAccepted || got.Output.Text != "" || got.CleanupFault != nil {
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
			if got.Status != contract.StatusCancelled || got.Output.Text != "" || got.Output.Status == contract.OutputAvailable || got.CleanupFault != nil {
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
			management, err := driver.OpenManagement(ctx, f.newAgent, driver.ManagementConfig{})
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
			probe, err := management.Probe(ctx, driver.ProbeRequest{Workspace: contract.WorkspaceScope{Root: f.workspace}, Model: f.model, AllowEmptyModelCatalog: true})
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
		writeNativeAnthropicResponse(w, payload["model"])
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
func (f *nativeCLIFixture) invoke(t *testing.T, parent context.Context) contract.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	d, err := driver.New(driver.Config{ID: f.name, CompletionPolicy: driver.CompletionLastAssistant, Factory: func(ctx context.Context, _ contract.OpenRequest) (sdk.Agent, error) {
		return f.newAgent(ctx)
	}})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{d}, Grants: contract.Grants{Directories: []string{f.workspace}, AllowAllTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer closeCancel()
		if err := rt.Close(closeCtx); err != nil {
			t.Error(err)
		}
	})
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return rt.Invoke(ctx, contract.ExecutionSpec{Profile: contract.Profile{DriverID: f.name, Model: f.model}, Input: contract.Input{Text: "EXACT_USER_FIXTURE"}, Workspace: contract.WorkspaceScope{Root: f.workspace}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}})
}

// newAgent 与执行和管理入口共享唯一原生工厂，保留各CLI状态目录与Pi桥接配置。
func (f *nativeCLIFixture) newAgent(ctx context.Context) (sdk.Agent, error) {
	return native.New(ctx, native.Config{RuntimeID: f.name, Executable: f.binary, Environment: f.environment, WorkingDirectory: f.workspace, StateDirectory: filepath.Join(f.root, f.name+"-state"), PermissionMode: "full-access", Logger: slog.New(slog.NewTextHandler(&f.diagnostics, nil))})
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
func writeNativeAnthropicResponse(w http.ResponseWriter, model any) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []map[string]any{
		{"type": "message_start", "message": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 2, "output_tokens": 0}}},
		{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "FINAL_FIXTURE_OK"}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 3}},
		{"type": "message_stop"},
	}
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
