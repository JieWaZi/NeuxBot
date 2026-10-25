package runtimes

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runAllyPiRPCFixture 验证 NeuxBot 经 Go 适配器只发送 Pi 原生 RPC，且恢复时刷新 MCP。
func runAllyPiRPCFixture(t *testing.T) {
	cwd, _ := os.Getwd()
	file := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "sessions", "fixture.jsonl")
	extension, loaded := "", false
	for i, arg := range os.Args {
		if i+1 < len(os.Args) {
			if arg == "--extension" {
				extension = os.Args[i+1]
			}
			if arg == "--session" {
				file = os.Args[i+1]
				loaded = true
			}
		}
	}
	snapshot, err := os.ReadFile(extension)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("AGENTS.md"); err == nil {
		expected := "Bearer first"
		if loaded {
			expected = "Bearer refreshed"
		}
		if !strings.Contains(string(snapshot), expected) || !strings.Contains(string(snapshot), "ally_control") {
			t.Fatal("stale or missing MCP snapshot")
		}
	}
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
			message := command["message"].(string)
			if strings.Contains(message, "恢复后的当前问题") &&
				(strings.Contains(message, "Chat history:") ||
					strings.Contains(message, "历史问题") ||
					strings.Contains(message, "历史回答")) {
				t.Fatal("Pi resume repeated chat history")
			}
			skill, skillErr := os.ReadFile(filepath.Join(".pi", "skills", "review", "SKILL.md"))
			instructions, instructionErr := os.ReadFile("AGENTS.md")
			if skillErr != nil || instructionErr != nil || !strings.Contains(string(skill), "Review marker") || !strings.Contains(string(instructions), "Instruction marker") || !strings.Contains(message, "/skill:review") {
				t.Fatal("missing Pi runtime files or selected skill")
			}
			_ = os.MkdirAll(filepath.Dir(file), 0700)
			header, _ := json.Marshal(map[string]any{"type": "session", "id": "pi-native-session", "cwd": cwd})
			_ = os.WriteFile(file, append(header, '\n'), 0600)
			write(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "provider/" + model + ":" + reasoning}})
			write(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "stopReason": "stop", "usage": map[string]any{"input": 7, "output": 5, "cacheRead": 3, "cacheWrite": 2}}})
			write(map[string]any{"type": "extension_ui_request", "method": "setStatus", "statusKey": "acp-go.context-usage", "statusText": `{"tokens":123,"contextWindow":100000}`})
			write(map[string]any{"type": "agent_settled"})
		case "abort":
		default:
			t.Fatalf("not a supported Pi RPC command: %v", command)
		}
		write(map[string]any{"type": "response", "id": command["id"], "command": command["type"], "success": true, "data": result})
	}
	os.Exit(0)
}
