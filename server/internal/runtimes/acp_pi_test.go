package runtimes

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"testing"
)

// runAllyPiRPCFixture 提供 Pi 能力探测使用的原生 RPC，不允许模型回合。
func runAllyPiRPCFixture(t *testing.T) {
	file := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "sessions", "fixture.jsonl")
	model, reasoning := "model-a", "low"
	write := func(value any) {
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	write(map[string]any{"type": "extension_ui_request", "method": "setStatus", "statusKey": "acp-go.extension-ready", "statusText": "ready"})
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command map[string]any
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			t.Fatal("invalid Pi RPC")
		}
		result := any(map[string]any{})
		switch command["type"] {
		case "get_state":
			result = map[string]any{"sessionId": "pi-native-session", "sessionFile": file, "model": map[string]any{"provider": "provider", "id": model}, "thinkingLevel": reasoning}
		case "get_available_models":
			result = map[string]any{"models": []any{map[string]string{"provider": "provider", "id": "model-a", "name": "Model A"}, map[string]string{"provider": "provider", "id": "model-b", "name": "Model B"}}}
		case "get_available_thinking_levels":
			result = map[string]any{"levels": []string{"low", "high"}}
		case "get_commands":
			result = map[string]any{"commands": []any{}}
		case "get_messages":
			result = map[string]any{"messages": []any{}}
		case "set_model":
			model = command["modelId"].(string)
			_ = os.WriteFile(os.Getenv("ALLY_NATIVE_ACP_MARKER"), []byte(model), 0600)
		case "set_thinking_level":
			reasoning = command["level"].(string)
		case "prompt":
			t.Fatal("capability detection must not submit a model Prompt")
		case "abort":
		default:
			t.Fatalf("not a supported Pi RPC command: %v", command)
		}
		write(map[string]any{"type": "response", "id": command["id"], "command": command["type"], "success": true, "data": result})
	}
	os.Exit(0)
}
