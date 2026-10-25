package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
)

// TestOfficialCodexTextWireIsolation 使用真实 CLI 和本地假服务核对最终模型请求，不调用真实模型或账号。
func TestOfficialCodexTextWireIsolation(t *testing.T) {
	binary := os.Getenv("ALLY_CODEX_OFFICIAL_CLI")
	if binary == "" {
		t.Skip("set ALLY_CODEX_OFFICIAL_CLI to verify the official CLI against a loopback fixture")
	}
	for _, withSystem := range []bool{true, false} {
		t.Run(fmt.Sprintf("system=%t", withSystem), func(t *testing.T) {
			requests := make(chan map[string]any, 8)
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
					t.Errorf("unexpected native provider request: %s %s", request.Method, request.URL.Path)
					http.NotFound(writer, request)
					return
				}
				if request.Header.Get("Authorization") != "Bearer local-fixture-key" {
					t.Error("native request did not use the selected fixture account")
				}
				var payload map[string]any
				if err := json.NewDecoder(io.LimitReader(request.Body, 4<<20)).Decode(&payload); err != nil {
					t.Errorf("decode native request: %v", err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				select {
				case requests <- payload:
				default:
					t.Error("native model retried beyond the fixture request bound")
				}
				writeCodexTextFixtureResponse(writer)
			}))
			defer provider.Close()
			source := t.TempDir()
			environment, err := accountconfig.Prepare(source, accountconfig.Spec{
				ID: "native-wire-fixture", RuntimeID: RuntimeIDCodex,
				AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolOpenAIResponses,
				BaseURL: provider.URL + "/v1", APIKey: "local-fixture-key", Model: "gpt-6-luna",
				SelectedModels: []string{"gpt-6-luna"},
			})
			if err != nil {
				t.Fatal(err)
			}
			environment["NO_PROXY"] = "127.0.0.1,localhost"
			home := environment["CODEX_HOME"]
			configPath := filepath.Join(home, "config.toml")
			config, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			// 在连接配置之前加全局指令；MCP 配置若被加载会尝试启动不存在的标记命令。
			config = append([]byte("developer_instructions = \"GLOBAL_DEVELOPER_MARKER\"\n"), config...)
			config = append(config, []byte("\n[mcp_servers.private]\ncommand = \"MCP_POISON_MARKER\"\n")...)
			if err = os.WriteFile(configPath, config, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte("GLOBAL_AGENTS_MARKER"), 0600); err != nil {
				t.Fatal(err)
			}
			catalogPath := filepath.Join(home, "models.json")
			catalog, err := os.ReadFile(catalogPath)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err = json.Unmarshal(catalog, &document); err != nil {
				t.Fatal(err)
			}
			for _, raw := range document["models"].([]any) {
				model := raw.(map[string]any)
				model["base_instructions"] = "CATALOG_POISON_MARKER"
				model["model_messages"] = map[string]any{"instructions_template": "CATALOG_POISON_MARKER"}
			}
			catalog, err = json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(catalogPath, catalog, 0600); err != nil {
				t.Fatal(err)
			}
			sdk, err := New(Config{})
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close(context.Background())
			messages := []TextMessage{{Role: "user", Content: "EXACT_USER_MARKER"}}
			if withSystem {
				messages = append([]TextMessage{{Role: "system", Content: "ORIGINAL_SYSTEM_MARKER"}}, messages...)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			result := sdk.GenerateText(ctx, TextRequest{
				Target: RuntimeTarget{RuntimeID: RuntimeIDCodex, AccountID: "native-wire-fixture", Isolated: true, Environment: environment, RefreshCredentials: func(context.Context, string) (codexoauth.Tokens, error) {
					t.Error("API Key 请求不应刷新 OAuth")
					return codexoauth.Tokens{}, status_error.RuntimeAuthRequired
				}},
				Detection: readyTextDetectionForRun(t, sdk, RuntimeIDCodex, binary), Model: "gpt-6-luna", Messages: messages,
			})
			if result.Failure != nil || result.Text != "NATIVE_FIXTURE_OK" {
				t.Fatalf("official CLI result: %+v", result)
			}
			select {
			case payload := <-requests:
				instructions, _ := payload["instructions"].(string)
				wire, _ := json.Marshal(payload)
				if withSystem && instructions != "ORIGINAL_SYSTEM_MARKER" && !codexWireHasExactMessage(payload["input"], "developer", "ORIGINAL_SYSTEM_MARKER") {
					t.Fatal("original system rules did not reach the official native instruction channel")
				}
				for _, poison := range []string{"GLOBAL_DEVELOPER_MARKER", "GLOBAL_AGENTS_MARKER", "CATALOG_POISON_MARKER", "MCP_POISON_MARKER", "skills_instructions", "You are Codex", "message role="} {
					if strings.Contains(string(wire), poison) {
						t.Errorf("native request contains forbidden instruction marker %q", poison)
					}
				}
				if !codexWireHasExactMessage(payload["input"], "user", "EXACT_USER_MARKER") {
					t.Fatal("native request did not preserve the original user message")
				}
				if payload["model"] != "gpt-6-luna" {
					t.Fatalf("native model = %v", payload["model"])
				}
			default:
				t.Fatal("official CLI did not reach the loopback provider")
			}
		})
	}
}

// codexWireHasExactMessage 只认真实 Responses 消息角色，不接受宿主在 XML 中模拟的角色。
func codexWireHasExactMessage(input any, role, expected string) bool {
	items, _ := input.([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["role"] != role {
			continue
		}
		content, _ := item["content"].([]any)
		for _, rawPart := range content {
			part, _ := rawPart.(map[string]any)
			if part["type"] == "input_text" && part["text"] == expected {
				return true
			}
		}
	}
	return false
}

// writeCodexTextFixtureResponse 返回完整官方 Responses 流终态，防止把仅收到文本当作调用成功。
func writeCodexTextFixtureResponse(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/event-stream")
	text := "NATIVE_FIXTURE_OK"
	part := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
	message := map[string]any{"id": "msg_native_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{part}}
	response := map[string]any{"id": "resp_native_fixture", "object": "response", "created_at": 1792573200, "model": "gpt-6-luna", "status": "completed", "output": []any{message}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
	events := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": response["id"], "object": "response", "status": "in_progress", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": message["id"], "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}},
		{"type": "response.content_part.added", "item_id": message["id"], "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}},
		{"type": "response.output_text.delta", "item_id": message["id"], "output_index": 0, "content_index": 0, "delta": text},
		{"type": "response.output_text.done", "item_id": message["id"], "output_index": 0, "content_index": 0, "text": text},
		{"type": "response.content_part.done", "item_id": message["id"], "output_index": 0, "content_index": 0, "part": part},
		{"type": "response.output_item.done", "output_index": 0, "item": message},
		{"type": "response.completed", "response": response},
	}
	for sequence, event := range events {
		event["sequence_number"] = sequence
		encoded, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event["type"], encoded)
	}
}
