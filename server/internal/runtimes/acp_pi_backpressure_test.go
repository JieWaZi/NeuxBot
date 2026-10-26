package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	acppi "github.com/JieWaZi/acp-go/pkg/pi"
	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestACPPiPressureProcess 只模拟 Pi 官方逐行 RPC，不运行真实 Pi 或模型。
func TestACPPiPressureProcess(t *testing.T) {
	if os.Getenv("NEUX_TEST_PI_PRESSURE") != "1" {
		return
	}
	if os.Args[len(os.Args)-1] == "--version" {
		fmt.Println("0.84.1")
		os.Exit(0)
	}
	cwd, _ := os.Getwd()
	sessionFile := filepath.Join(cwd, "pi-pressure.jsonl")
	header, _ := json.Marshal(map[string]any{"type": "session", "id": "pi-pressure", "cwd": cwd})
	if os.WriteFile(sessionFile, append(header, '\n'), 0600) != nil {
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	write := func(value any) {
		if encoder.Encode(value) != nil {
			os.Exit(0)
		}
	}
	write(map[string]any{"type": "extension_ui_request", "method": "setStatus", "statusKey": "acp-go.extension-ready", "statusText": "ready"})
	decoder := json.NewDecoder(os.Stdin)
	for {
		var request map[string]any
		if decoder.Decode(&request) != nil {
			os.Exit(0)
		}
		result := any(map[string]any{})
		switch request["type"] {
		case "get_state":
			result = map[string]any{"sessionId": "pi-pressure", "sessionFile": sessionFile, "model": map[string]any{"provider": "fixture", "id": "model"}, "thinkingLevel": "high"}
		case "get_available_models":
			result = map[string]any{"models": []any{map[string]any{"provider": "fixture", "id": "model"}}}
		case "get_available_thinking_levels":
			result = map[string]any{"levels": []string{"off", "high"}}
		case "get_commands":
			result = map[string]any{"commands": []any{}}
		case "prompt":
			for i := 0; i < 2200; i++ {
				write(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": fmt.Sprintf("chunk-%04d;", i)}})
			}
			write(map[string]any{"type": "agent_end"})
			write(map[string]any{"type": "agent_settled"})
		case "abort":
			write(map[string]any{"type": "agent_settled"})
		}
		write(map[string]any{"type": "response", "id": request["id"], "success": true, "data": result})
	}
}

func piPressureBackend(t *testing.T) *acpBackend {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	module := filepath.Join(root, "bridge.mjs")
	if err = os.WriteFile(module, []byte("export const createMcpAdapter = () => {};"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := newACPBackend()
	replaceACPAgentFactory(backend, RuntimeIDPi, func(ctx context.Context, config acpAdapterConfig) (acp.Agent, error) {
		return acppi.NewAgent(ctx, acppi.Config{PiPath: binary, PrefixArgs: []string{"-test.run=^TestACPPiPressureProcess$", "--"}, Environment: append(os.Environ(), "NEUX_TEST_PI_PRESSURE=1", "PI_CODING_AGENT_DIR="+root), WorkingDirectory: config.WorkingDirectory, Logger: acpAdapterLogger(ctx), MCPModulePath: module})
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })
	return backend
}
func piPressureRequest(t *testing.T, id string, sink EventSink) acpRunRequest {
	t.Helper()
	req := testCodexACPRunRequest(t, id)
	req.Prepared.definition.id = RuntimeIDPi
	req.Prepared.definition.baseRuntimeID = RuntimeIDPi
	req.Request.Target.RuntimeID = RuntimeIDPi
	req.Prepared.defaults.EventDelivery = 5 * time.Second
	req.Sink = sink
	return req
}

func TestACPPiStdioBackpressuresWithinDeliveryBudget(t *testing.T) {
	backend := piPressureBackend(t)
	sink := &nativeOrderedTextSink{burstPauseSink: burstPauseSink{delay: 1500 * time.Millisecond}}
	request := piPressureRequest(t, "pi-pressure", sink)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	begin := time.Now()
	result := backend.Run(ctx, request)
	if result.Failure != nil || sink.count != 2200 {
		t.Fatalf("normal Pi stdio failed within 5s delivery budget: elapsed=%v delivered=%d/2200 failure=%+v", time.Since(begin), sink.count, result.Failure)
	}
	t.Logf("Pi stdio delivered=%d elapsed=%v", sink.count, time.Since(begin))
}
