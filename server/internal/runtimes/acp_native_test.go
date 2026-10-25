package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// TestUnifiedPermissionAuditProjection 验证所有 Runtime 的统一工具投影保留自动审查事实。
func TestUnifiedPermissionAuditProjection(t *testing.T) {
	for id, binding := range defaultACPRuntimeBindings() {
		t.Run(id, func(t *testing.T) {
			metadata := map[string]any{"acp-go/permission-review": map[string]any{"mode": "auto", "outcome": "ask", "source": "acp-go", "reason": "review_unavailable"}}
			payload, err := acpToolPayload(acp.SessionToolCallUpdate{ToolCallId: "reviewed-tool", Meta: metadata}, binding)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(payload["_meta"], metadata) {
				t.Fatalf("permission review audit lost during projection: %#v", payload)
			}
		})
	}
}

// TestNativeCLIDiscoveryModelSelectionAndResume 验证公开适配器使用真实子进程连接选择模型和恢复历史。
func TestNativeCLIDiscoveryModelSelectionAndResume(t *testing.T) {
	for _, fixtureID := range []string{RuntimeIDKimi, RuntimeIDPi, RuntimeIDOpenCode, RuntimeIDGrok, RuntimeIDWorkBuddy, RuntimeIDQoder, RuntimeIDQoderCN, "kimi-legacy"} {
		id := strings.TrimSuffix(fixtureID, "-legacy")
		t.Run(fixtureID, func(t *testing.T) {
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "model-selected")
			profile := ProfileSpec{ID: id + "-fixture", BaseRuntimeID: id, BinaryCandidates: []string{binary}, PrefixArgs: []string{"-test.run=^TestAllyNativeACPProcess$", "--"}, Environment: map[string]string{"ALLY_NATIVE_ACP_TEST": fixtureID, "ALLY_NATIVE_ACP_MARKER": marker}}
			if id == RuntimeIDWorkBuddy || id == RuntimeIDQoder || id == RuntimeIDQoderCN {
				profile.Environment["ALLY_NATIVE_ACP_PREFIX_RECORD"] = filepath.Join(t.TempDir(), "static-prefix")
			}
			if id == RuntimeIDPi {
				profile.Environment["PI_CODING_AGENT_DIR"] = t.TempDir()
				module := filepath.Join(t.TempDir(), "bridge.mjs")
				if err := os.WriteFile(module, []byte("export const createMcpAdapter = () => {};"), 0600); err != nil {
					t.Fatal(err)
				}
				profile.Environment["PI_MCP_MODULE_PATH"] = module
			}
			profile.Environment["KIMI_SHARE_DIR"] = t.TempDir()
			sdk, err := New(Config{Profiles: []ProfileSpec{profile}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			defer sdk.Close(ctx)
			detection := sdk.Detect(ctx, DetectRequest{RuntimeID: profile.ID, BaseEnvironment: os.Environ()})
			if detection.State != DetectionStateReady || len(detection.Models) != 2 {
				t.Fatalf("detection: %+v", detection)
			}
			if detection.Version != "test-native-1" {
				t.Fatalf("CLI version not exposed to menu: %q", detection.Version)
			}
			if id == RuntimeIDWorkBuddy {
				if len(detection.Models[1].ReasoningOptions) == 0 {
					t.Fatal("WorkBuddy did not probe concrete model thinking")
				}
			} else if len(detection.Models[1].ReasoningOptions) != 0 {
				t.Fatal("discovery inferred reasoning options for an unselected model")
			}
			if fixtureID == "kimi-legacy" && !reflect.DeepEqual(detection.Models[0].PermissionLevels, []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}) {
				t.Fatal("Python Kimi isolation did not expose its supported permission levels")
			}
			if id == RuntimeIDWorkBuddy {
				if selected, err := os.ReadFile(marker); err != nil || string(selected) != "provider/model-b" {
					t.Fatalf("WorkBuddy temporary model probe: %q, %v", selected, err)
				}
			} else if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("discovery changed CLI model/config: %v", err)
			}
			level := PermissionLevelDefault
			if id == RuntimeIDPi || fixtureID == "kimi-legacy" {
				level = PermissionLevelFullAccess
			}
			reasoning := "low"
			if fixtureID == "kimi-legacy" {
				reasoning = ""
			}
			request := RunRequest{Target: RuntimeTarget{RuntimeID: profile.ID}, Detection: detection, Workspace: Workspace{Path: t.TempDir(), Managed: true}, Message: Message{Text: "hello"}, Session: SessionDirective{Mode: SessionModeNew}, Policy: RunPolicy{Model: "provider/model-b", Reasoning: reasoning, Execution: ExecutionPolicy{PermissionLevel: level}}}
			request.History = []HistoryMessage{{Role: "user", Content: "历史问题"}, {Role: "assistant", Content: "历史回答"}}
			request.Connectors = []Connector{{ID: "control", Name: "Control fixture", RuntimeKey: "ally_control", Transport: ConnectorTransportStreamableHTTP, URL: "http://127.0.0.1:9/mcp", Headers: map[string]string{"Authorization": "Bearer first"}}}
			skillDirectory := t.TempDir()
			if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte("---\nname: review\ndescription: Review fixture\n---\nReview marker"), 0600); err != nil {
				t.Fatal(err)
			}
			request.Skills = []Skill{{Name: "review", Path: skillDirectory, Selected: true}}
			request.Instructions = []Instruction{{Name: "fixture", Content: "Instruction marker"}}
			sink := &recordingSink{}
			result := sdk.Run(ctx, request, sink)
			if result.Failure != nil || sink.textOutput() != "provider/model-b:low" {
				t.Fatalf("run: %+v output=%q", result, sink.textOutput())
			}
			if id == RuntimeIDPi {
				var finalUsage UsageData
				for _, event := range sink.events {
					if event.Type == EventTypeUsage {
						var value UsageData
						if json.Unmarshal(event.Data, &value) == nil && value.Turn != nil {
							finalUsage = value
						}
					}
				}
				if finalUsage.Turn == nil || finalUsage.Context == nil || finalUsage.Turn.InputTokens != 7 || finalUsage.Turn.OutputTokens != 5 || finalUsage.Turn.CacheReadTokens != 3 || finalUsage.Turn.CacheCreationTokens != 2 || finalUsage.Context.UsedTokens != 123 || finalUsage.Context.WindowTokens != 100000 {
					t.Fatalf("Pi usage lost before NeuxBot: %+v", finalUsage)
				}
			}
			request.Session = SessionDirective{Mode: SessionModeResume, ID: result.Session.OutputID}
			request.Message.Text = "恢复后的当前问题"
			request.Connectors[0].Headers["Authorization"] = "Bearer refreshed"
			sink = &recordingSink{}
			if result = sdk.Run(ctx, request, sink); result.Failure != nil {
				t.Fatalf("reuse: %+v", result)
			}
			// 对会话实际声明的当前模型验证推理选择。
			if fixtureID != "kimi-legacy" {
				request.Policy.Model = "provider/model-a"
				request.Policy.Reasoning = "high"
				sink = &recordingSink{}
				if result = sdk.Run(ctx, request, sink); result.Failure != nil || sink.textOutput() != "provider/model-a:high" {
					t.Fatalf("reasoning: %+v output=%q", result, sink.textOutput())
				}
			}
			// 重启 SDK 后只声明 load 的上游必须通过 load 恢复，不能调用未声明的 resume。
			if err := sdk.Close(ctx); err != nil {
				t.Fatal(err)
			}
			resumed, err := New(Config{Profiles: []ProfileSpec{profile}})
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.Close(ctx)
			if result = resumed.Run(ctx, request, &recordingSink{}); result.Failure != nil {
				t.Fatalf("load: %+v", result)
			}
			if !LaunchOverrideArgumentsAllowed(id, nil) || LaunchOverrideArgumentsAllowed(id, []string{"--model=override"}) {
				t.Fatal("invalid launch override policy")
			}
			if id == RuntimeIDWorkBuddy || id == RuntimeIDQoder || id == RuntimeIDQoderCN {
				if _, err := os.Stat(profile.Environment["ALLY_NATIVE_ACP_PREFIX_RECORD"]); err != nil {
					t.Fatalf("static prefix was never observed: %v", err)
				}
			}
		})
	}
}

func TestNativePermissionModesAreNotInterchangeable(t *testing.T) {
	modes := []acp.SessionMode{{Id: "agent"}, {Id: "plan"}, {Id: "ask"}, {Id: "default"}, {Id: "auto"}, {Id: "yolo"}, {Id: "high"}}
	for _, test := range []struct {
		// id 是本例的内置 Runtime 标识。
		id string
		// expected 是该 Runtime 的真实产品权限集合。
		expected []PermissionLevel
	}{{RuntimeIDKimi, []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}}, {RuntimeIDCursor, []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}}, {RuntimeIDPi, []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}}} {
		got := defaultACPRuntimeBindings()[test.id].permissionLevels(nil, modes)
		if !reflect.DeepEqual(got, test.expected) {
			t.Fatalf("%s: %v", test.id, got)
		}
	}
}

func TestNativeMCPCompatibilityUsesDetectedTransports(t *testing.T) {
	for _, id := range []string{RuntimeIDKimi, RuntimeIDCursor, RuntimeIDPi} {
		for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
			supported, _ := ConnectorCompatibility(id, transport)
			expected := transport != ConnectorTransportSSE || id == RuntimeIDPi
			if supported != expected {
				t.Fatalf("%s compatibility for %s = %v, want %v", id, transport, supported, expected)
			}
			if !DetectedConnectorTransportSupported(DetectionResult{Runtime: RuntimeSummary{BaseRuntimeID: id}, State: DetectionStateReady, Capabilities: CapabilitySet{CapabilityMCPStreamableHTTP: true, CapabilityMCPSSE: true}}, transport) {
				t.Fatalf("%s detection rejects supported %s", id, transport)
			}
		}
		if DetectedConnectorTransportSupported(DetectionResult{Runtime: RuntimeSummary{BaseRuntimeID: id}, State: DetectionStateReady}, ConnectorTransportSSE) {
			t.Fatalf("%s must not infer an undeclared SSE capability", id)
		}
	}
}

// TestAllyNativeACPProcess 提供真实子进程的原生 ACP 协议对照，不访问网络或用户登录。
func TestAllyNativeACPProcess(t *testing.T) {
	id := os.Getenv("ALLY_NATIVE_ACP_TEST")
	if id == "" {
		return
	}
	if os.Args[len(os.Args)-1] == "--version" {
		_, _ = os.Stdout.WriteString("test-native-1\n")
		os.Exit(0)
	}
	if diagnostic := os.Getenv("ALLY_NATIVE_ACP_STDERR"); diagnostic != "" {
		_, _ = os.Stderr.WriteString(diagnostic)
	}
	if id == RuntimeIDPi {
		runAllyPiRPCFixture(t)
		return
	}
	if (id == RuntimeIDQoder || id == RuntimeIDQoderCN) && os.Args[len(os.Args)-1] != "--acp" {
		t.Fatal("Qoder must start through --acp")
	}
	legacy := id == "kimi-legacy"
	id = strings.TrimSuffix(id, "-legacy")
	ready := make(chan struct{})
	var connection *acp.Connection
	var mutex sync.Mutex
	model, reasoning := "provider/model-a", "low"
	mcpNames := []string{}
	options := func() []map[string]any {
		return []map[string]any{{"id": "engine", "type": "select", "name": "Model", "category": "model", "currentValue": model, "options": []map[string]string{{"value": "provider/model-a", "name": "Model A"}, {"value": "provider/model-b", "name": "Model B"}}}, {"id": "thinking", "type": "select", "name": "Thinking", "category": "thought_level", "currentValue": reasoning, "options": []map[string]string{{"value": "low", "name": "Low"}, {"value": "high", "name": "High"}}}}
	}
	connection = acp.NewConnection(func(ctx context.Context, method string, data json.RawMessage) (any, *acp.RequestError) {
		<-ready
		mutex.Lock()
		defer mutex.Unlock()
		var request map[string]any
		_ = json.Unmarshal(data, &request)
		switch method {
		case "initialize":
			return map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]bool{"http": true}}, "authMethods": []any{}, "agentInfo": map[string]string{"name": id, "version": "test-native-1"}}, nil
		case "session/new", "session/load":
			var session acp.NewSessionRequest
			if json.Unmarshal(data, &session) != nil {
				return nil, acp.NewInvalidParams(nil)
			}
			mcpNames = nil
			for _, server := range session.McpServers {
				if server.Http != nil {
					mcpNames = append(mcpNames, server.Http.Name)
				}
			}
			externalServers := session.McpServers[:0]
			for _, server := range session.McpServers {
				if server.Http != nil && server.Http.Meta["acp-go/user-input"] == true {
					continue
				}
				externalServers = append(externalServers, server)
			}
			session.McpServers = externalServers
			if len(session.McpServers) > 0 {
				expected := "Bearer first"
				if method == "session/load" {
					expected = "Bearer refreshed"
				}
				server := session.McpServers[0].Http
				if server == nil || server.Name != "ally_control" || len(server.Headers) != 1 || server.Headers[0].Value != expected {
					return nil, acp.NewInvalidParams(map[string]any{"message": "stale or missing MCP configuration"})
				}
			} else if _, err := os.Stat("AGENTS.md"); err == nil {
				return nil, acp.NewInvalidParams(map[string]any{"message": "execution omitted MCP configuration"})
			}
			cwd, _ := os.Getwd()
			requestedDirectory, _ := request["cwd"].(string)
			currentInfo, currentErr := os.Stat(cwd)
			requestedInfo, requestedErr := os.Stat(requestedDirectory)
			if currentErr != nil || requestedErr != nil || !os.SameFile(currentInfo, requestedInfo) {
				return nil, acp.NewInvalidParams(map[string]any{"message": "wrong process cwd"})
			}
			modes := []map[string]string{{"id": "default", "name": "Default"}, {"id": "auto", "name": "Auto"}, {"id": "yolo", "name": "Yolo"}}
			if id == RuntimeIDWorkBuddy {
				modes = []map[string]string{{"id": "default", "name": "Default"}, {"id": "auto", "name": "Auto"}, {"id": "bypassPermissions", "name": "Bypass"}, {"id": "plan", "name": "Plan"}}
			}
			if id == RuntimeIDQoder || id == RuntimeIDQoderCN {
				modes = []map[string]string{{"id": "default", "name": "Default"}, {"id": "bypassPermissions", "name": "Bypass"}}
			}
			if id == RuntimeIDPi {
				modes = []map[string]string{{"id": "high", "name": "High thinking"}}
			}
			response := map[string]any{"sessionId": "native-session", "configOptions": options(), "modes": map[string]any{"currentModeId": modes[0]["id"], "availableModes": modes}}
			if legacy {
				delete(response, "configOptions")
				response["modes"] = map[string]any{"currentModeId": "default", "availableModes": modes[:1]}
				response["models"] = map[string]any{"currentModelId": model, "availableModels": []map[string]string{{"modelId": "provider/model-a", "name": "Model A"}, {"modelId": "provider/model-b", "name": "Model B"}}}
			}
			return response, nil
		case "session/set_model":
			if !legacy {
				return nil, acp.NewMethodNotFound(method)
			}
			model = request["modelId"].(string)
			_ = os.WriteFile(os.Getenv("ALLY_NATIVE_ACP_MARKER"), []byte(model), 0600)
			return map[string]any{}, nil
		case "session/set_config_option":
			switch request["configId"] {
			case "engine":
				model = request["value"].(string)
				_ = os.WriteFile(os.Getenv("ALLY_NATIVE_ACP_MARKER"), []byte(model), 0600)
			case "thinking":
				reasoning = request["value"].(string)
			default:
				return nil, acp.NewInvalidParams(nil)
			}
			return map[string]any{"configOptions": options()}, nil
		case "session/set_mode":
			expected := "default"
			if id == RuntimeIDPi || request["modeId"] != expected {
				return nil, acp.NewInvalidParams(nil)
			}
			return map[string]any{}, nil
		case "session/prompt":
			var prompt acp.PromptRequest
			if json.Unmarshal(data, &prompt) != nil || len(prompt.Prompt) == 0 || prompt.Prompt[0].Text == nil {
				return nil, acp.NewInvalidParams(nil)
			}
			if strings.Contains(prompt.Prompt[0].Text.Text, "恢复后的当前问题") &&
				(strings.Contains(prompt.Prompt[0].Text.Text, "Chat history:") ||
					strings.Contains(prompt.Prompt[0].Text.Text, "历史问题") ||
					strings.Contains(prompt.Prompt[0].Text.Text, "历史回答")) {
				return nil, acp.NewInvalidParams(map[string]any{"message": "resume repeated chat history"})
			}
			root, command := ".agents", "/skill:review"
			instructionFile := "AGENTS.md"
			if id == RuntimeIDWorkBuddy {
				root, command, instructionFile = ".codebuddy", "/review", "CODEBUDDY.md"
			}
			if id == RuntimeIDOpenCode || id == RuntimeIDGrok || id == RuntimeIDQoder || id == RuntimeIDQoderCN {
				root = "." + id
				command = "/review"
			}
			if id == RuntimeIDQoderCN {
				root = ".qoder"
			}
			if id == RuntimeIDPi {
				root = ".pi"
			}
			skill, skillErr := os.ReadFile(filepath.Join(root, "skills", "review", "SKILL.md"))
			instructions, instructionErr := os.ReadFile(instructionFile)
			if skillErr != nil || instructionErr != nil || !strings.Contains(string(skill), "Review marker") || !strings.Contains(string(instructions), "Instruction marker") || !strings.Contains(prompt.Prompt[0].Text.Text, command) {
				return nil, acp.NewInvalidParams(map[string]any{"message": "missing native runtime files or skill selection"})
			}
			if record := os.Getenv("ALLY_NATIVE_ACP_PREFIX_RECORD"); record != "" {
				prefix, _ := json.Marshal([]any{string(instructions), string(skill), mcpNames})
				digest := sha256.Sum256(prefix)
				previous, err := os.ReadFile(record)
				if os.IsNotExist(err) {
					err = os.WriteFile(record, digest[:], 0600)
				} else if err == nil && !reflect.DeepEqual(previous, digest[:]) {
					return nil, acp.NewInvalidParams(map[string]any{"message": "static model prefix drifted across turns or credential refresh"})
				}
				if err != nil {
					return nil, acp.NewInternalError(nil)
				}
			}
			_ = connection.SendNotification(ctx, "session/update", map[string]any{"sessionId": "native-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": model + ":" + reasoning}}})
			return map[string]any{"stopReason": "end_turn"}, nil
		case "session/close":
			return map[string]any{}, nil
		case "session/cancel":
			return nil, nil
		default:
			return nil, acp.NewMethodNotFound(method)
		}
	}, os.Stdout, os.Stdin)
	close(ready)
	<-connection.Done()
	os.Exit(0)
}
