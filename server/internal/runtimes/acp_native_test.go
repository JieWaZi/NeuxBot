package runtimes

import (
	"context"
	"encoding/json"
	acp "github.com/JieWaZi/acp-go/sdk"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNativeCLIDiscoveryReadsActualCatalogWithoutPrompt(t *testing.T) {
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
			t.Fatal("capability detection must not submit a model Prompt")
			return nil, acp.NewMethodNotFound(method)
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
