package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
)

// TestOfficialGeminiManagedDetection 使用固定官方 CLI 和本地目录端点验证受管信任及精确模型能力。
func TestOfficialGeminiManagedDetection(t *testing.T) {
	cli := os.Getenv("ALLY_GEMINI_OFFICIAL_CLI")
	if cli == "" {
		t.Skip("requires pinned official Gemini 0.62 CLI")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	versionCtx, cancelVersion := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelVersion()
	version, err := exec.CommandContext(versionCtx, node, cli, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "0.62.0" {
		t.Fatalf("official version: %s %v", version, err)
	}
	t.Logf("same official entry --version: %s", strings.TrimSpace(string(version)))
	var requestMutex sync.Mutex
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "local-fixture-key" {
			t.Errorf("missing managed credential on %s", r.URL.Path)
		}
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if strings.Contains(r.URL.Path, ":countTokens") {
				_, _ = w.Write([]byte(`{"totalTokens":10}`))
				return
			}
			data, _ := json.Marshal(body)
			requestMutex.Lock()
			prompts = append(prompts, string(data))
			requestMutex.Unlock()
			response := `{"candidates":[{"content":{"role":"model","parts":[{"text":"Local fixture answer"}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
			if r.URL.Query().Get("alt") == "sse" {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", response)
			} else {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			}
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1beta/") || strings.Contains(r.URL.Path, "/v1beta/v1beta/") {
			t.Errorf("incorrect SDK endpoint: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "models/gemini-3.8-flash", "supportedGenerationMethods": []string{"generateContent"}}}})
	}))
	defer server.Close()
	workspace := t.TempDir()
	environment, err := accountconfig.Prepare(workspace, accountconfig.Spec{RuntimeID: constants.RuntimeIDGemini, ID: "official-fixture", Revision: 1, AuthMode: constants.AccountAuthModeAPIKey, Protocol: constants.AccountProtocolGoogleGenerativeAI, BaseURL: server.URL + "/v1beta", APIKey: "local-fixture-key", Model: "gemini-3.8-flash", SelectedModels: []string{"gemini-3.8-flash"}})
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	result := sdk.Detect(ctx, runtimecontract.DetectRequest{RuntimeID: runtimecontract.RuntimeIDGemini, WorkingDirectory: workspace, Model: "gemini-3.8-flash", BaseEnvironment: accountconfig.Environment(os.Environ()), LaunchOverride: runtimecontract.LaunchOverride{Binary: node, PrefixArgs: []string{cli}, Environment: environment}})
	if result.State != runtimecontract.DetectionStateReady || (result.Version != "" && result.Version != "0.62.0") {
		t.Fatalf("official managed detection: %+v", result)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == runtimecontract.DiagnosticCodeACPOptionsUnavailable {
			t.Fatalf("official managed catalog unavailable: %+v", diagnostic)
		}
	}
	t.Logf("managed ready/catalog native version metadata: %q", result.Version)
	selected := false
	for _, model := range result.Models {
		if model.ID == "gemini-3.8-flash" {
			for _, option := range model.ReasoningOptions {
				if option.ID == "low" || option.ID == "LOW" {
					selected = true
				}
			}
			if !selected {
				t.Fatalf("missing source-backed level: %+v", model)
			}
		}
	}
	if !selected {
		t.Fatal("selected native model missing")
	}

}
