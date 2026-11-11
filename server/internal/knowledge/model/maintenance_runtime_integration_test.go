package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
)

// TestMaintenanceRuntimeIntegratesScopedTask 通过正式 ProviderRuntime 核验审批、领域调用和清理顺序。
func TestMaintenanceRuntimeIntegratesScopedTask(t *testing.T) {
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	endpoint := httptest.NewServer(registry)
	t.Cleanup(endpoint.Close)
	store, err := conversation.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	trace := filepath.Join(t.TempDir(), "bridge-path")
	var calls atomic.Int32
	var releases atomic.Int32
	var workspace, connectionPath string
	provider := agent.NewProviderRuntime(agent.ProviderRuntimeOptions{AcquireAccount: func(_ context.Context, request agent.AccountRequest) (agent.AccountLease, error) {
		workspace = request.CWD
		return agent.AccountLease{Close: func(context.Context) error {
			releases.Add(1)
			value, err := os.ReadFile(trace)
			if err != nil {
				t.Error(err)
				return err
			}
			connectionPath = string(value)
			connection, err := capability.ReadConnection(connectionPath)
			if err != nil {
				t.Error("连接文件先于账号租约释放", err)
				return err
			}
			if response := maintenanceCapabilities(registry, connection); response.Code != http.StatusUnauthorized {
				t.Errorf("账号释放时维护授权尚未撤销: %d", response.Code)
			}
			return nil
		}}, nil
	}})
	t.Cleanup(func() { _ = provider.Close(context.Background()) })
	host := agent.NewThreadLifecycle(provider, store)
	t.Cleanup(func() { _ = host.Close(context.Background()) })

	input := maintenanceInput()
	input.Bindings[0].Call = func(_ context.Context, request capability.Request) (capability.Result, error) {
		calls.Add(1)
		if request.Params["libraryId"] != "library" {
			t.Errorf("维护丢失单库作用域: %+v", request)
		}
		return capability.Result{Data: map[string]bool{"checked": true}}, nil
	}
	runner := NewMaintenanceRunner(MaintenanceConfig{
		Runtime: host, Registry: registry, Binary: os.Args[0], Endpoint: endpoint.URL + constants.AgentCLIPath,
		Connection: agent.ProviderConnectionRequest{
			Account: agent.AccountRequest{Provider: constants.RuntimeIDClaude, AccountID: "test-account", Subject: "test-subject"},
			Launch: agent.ProviderLaunch{Executable: os.Args[0], PrefixArgs: []string{"-test.run=^TestMaintenanceNativeFixture$", "--"},
				Environment: map[string]string{"NEUX_MAINTENANCE_FIXTURE_TRACE": trace, "GORACE": "atexit_sleep_ms=0"}},
		},
	})
	report, err := runner.Run(t.Context(), input)
	if err != nil || report != "verified maintenance report" || calls.Load() != 1 || releases.Load() != 1 {
		debug, _ := os.ReadFile(trace + ".debug")
		t.Fatalf("报告=%q 调用=%d 账号释放=%d 错误=%v 记录=%s", report, calls.Load(), releases.Load(), err, debug)
	}
	for _, path := range []string{workspace, connectionPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("任务结束后仍有临时资源 %s: %v", path, err)
		}
	}
}

// TestMaintenanceNativeFixture 模拟 Claude 原生协议，保留真实审批和授权调用。
func TestMaintenanceNativeFixture(t *testing.T) {
	trace := os.Getenv("NEUX_MAINTENANCE_FIXTURE_TRACE")
	if trace == "" {
		return
	}
	if slices.Contains(os.Args, "--version") {
		fmt.Println("2.1.309 (Claude Code)")
		os.Exit(0)
	}
	var session, path string
	for i, arg := range os.Args {
		if strings.HasPrefix(arg, "--session-id=") {
			session = strings.TrimPrefix(arg, "--session-id=")
		}
		if arg == "--mcp-config" && i+1 < len(os.Args) {
			var cfg struct {
				// Servers 是原生进程收到的严格连接器配置。
				Servers map[string]struct {
					// Args 指向服务端拥有的本次连接文件。
					Args []string `json:"args"`
				} `json:"mcpServers"`
			}
			if json.Unmarshal([]byte(os.Args[i+1]), &cfg) != nil || len(cfg.Servers) != 1 {
				os.Exit(2)
			}
			if args := cfg.Servers["ally_capabilities"].Args; len(args) == 4 {
				path = args[3]
			}
		}
	}
	if session == "" || path == "" || !slices.Contains(os.Args, "--tools=") || !slices.Contains(os.Args, "--strict-mcp-config") {
		os.Exit(2)
	}
	if os.WriteFile(trace, []byte(path), 0600) != nil {
		os.Exit(2)
	}
	cwd, _ := os.Getwd()
	out := json.NewEncoder(os.Stdout)
	emit := func(frame map[string]any) {
		frame["session_id"] = session
		_ = out.Encode(frame)
	}
	var user string
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var frame map[string]any
		if json.Unmarshal(in.Bytes(), &frame) != nil {
			os.Exit(2)
		}
		switch frame["type"] {
		case "control_request":
			request, _ := frame["request"].(map[string]any)
			response := map[string]any{}
			switch request["subtype"] {
			case "initialize":
				emit(map[string]any{"type": "system", "subtype": "init", "cwd": cwd, "model": "sonnet", "permissionMode": "default", "claude_code_version": "2.1.309"})
				response["models"] = []map[string]string{{"value": "sonnet", "displayName": "Sonnet"}}
			case "get_settings":
				response["applied"] = map[string]string{"model": "sonnet"}
			case "mcp_status":
				response["mcpServers"] = []map[string]string{{"name": "ally_capabilities", "status": "connected"}}
			}
			_ = out.Encode(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": frame["request_id"], "response": response,
			}})
		case "user":
			user, _ = frame["uuid"].(string)
			emit(map[string]any{"type": "assistant", "user_message_uuid": user, "parent_tool_use_id": nil,
				"message": map[string]any{"id": "wiki-tool-turn", "model": "sonnet",
					"content": []map[string]any{{"type": "tool_use", "id": "knowledge-tool", "name": "mcp__ally_capabilities__knowledge_audit_get",
						"input": map[string]string{"libraryId": "library"}}}}})
			emit(map[string]any{"type": "control_request", "request_id": "approved-wiki", "request": map[string]any{
				"subtype": "can_use_tool", "tool_name": "mcp__ally_capabilities__knowledge_audit_get", "tool_use_id": "knowledge-tool",
				"input": map[string]string{"libraryId": "library"},
			}})
		case "control_response":
			_ = os.WriteFile(trace+".debug", []byte(in.Text()), 0600)
			if user == "" || !strings.Contains(in.Text(), `"behavior":"allow"`) {
				os.Exit(2)
			}
			if err := fixtureCallKnowledge(path); err != nil {
				os.Exit(2)
			}
			usage := map[string]int{"input_tokens": 2, "output_tokens": 1, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}
			emit(map[string]any{"type": "assistant", "user_message_uuid": user, "parent_tool_use_id": nil,
				"message": map[string]any{"id": "wiki-report", "model": "sonnet", "usage": usage,
					"content": []map[string]string{{"type": "text", "text": "verified maintenance report"}}}})
			emit(map[string]any{"type": "result", "subtype": "success", "uuid": "wiki-terminal", "result_index": 0,
				"user_message_uuid": user, "terminal_reason": "completed", "result": "verified maintenance report", "usage": usage})
		}
	}
	os.Exit(0)
}

func fixtureCallKnowledge(path string) error {
	connection, err := capability.ReadConnection(path)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, connection.Endpoint+"/commands", strings.NewReader(`{"operation":"knowledge.audit.get","params":{"libraryId":"library"}}`))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+connection.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var payload capability.Response
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&payload) != nil || !payload.OK {
		return status_error.KnowledgeForbidden
	}
	return nil
}
