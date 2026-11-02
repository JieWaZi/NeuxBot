package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	native "github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	"github.com/JieWaZi/acp-go/pkg/nativeacp"
	acp "github.com/JieWaZi/acp-go/sdk"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"github.com/shrewx/ginx/pkg/statuserror"
	"github.com/sirupsen/logrus"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestClaudeACPDetectOptionsUsesAdapterConfigIDs 验证 Claude model/effort 目录来自 ACP。
func TestClaudeACPDetectOptionsUsesAdapterConfigIDs(t *testing.T) {
	t.Parallel()

	models := []acp.SessionConfigSelectOption{
		{Value: "sonnet", Name: "Sonnet"},
		{Value: "opus", Name: "Opus"},
	}
	sonnetOptions := testClaudeACPConfigOptions(
		"sonnet",
		models,
		[]acp.SessionConfigSelectOption{{Value: "medium", Name: "Medium"}},
	)
	opusOptions := testClaudeACPConfigOptions(
		"opus",
		models,
		[]acp.SessionConfigSelectOption{{Value: "high", Name: "High"}},
	)
	fakeAgent := &fakeCodexACPAgent{
		sessionID:       "claude-option-session",
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		configOptions:   sonnetOptions,
		configOptionsByModel: map[acp.SessionConfigValueId][]acp.SessionConfigOption{
			"opus": opusOptions,
		},
	}
	backend := newACPBackend()
	replaceACPAgentFactory(backend, RuntimeIDClaude, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return fakeAgent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })

	catalog, err := backend.DetectOptions(context.Background(), acpOptionsRequest{
		RuntimeID: RuntimeIDClaude,
		Prepared: preparedRuntime{
			definition:  runtimeDefinition{id: RuntimeIDClaude, baseRuntimeID: RuntimeIDClaude},
			detection:   DetectionResult{ResolvedBinary: "/fake/claude"},
			environment: environmentSnapshot{},
		},
		WorkingDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DetectOptions() error = %v", err)
	}
	if len(catalog.Models) != 2 || catalog.Models[0].ID != "sonnet" ||
		len(catalog.Models[0].ReasoningOptions) != 1 ||
		catalog.Models[0].ReasoningOptions[0].ID != "medium" ||
		len(catalog.Models[1].ReasoningOptions) != 1 ||
		catalog.Models[1].ReasoningOptions[0].ID != "high" {
		t.Fatalf("catalog = %#v", catalog)
	}
}

// testClaudeACPConfigOptions 创建 model 与当前模型 effort 配置。
func testClaudeACPConfigOptions(
	currentModel string,
	models []acp.SessionConfigSelectOption,
	reasoning []acp.SessionConfigSelectOption,
) []acp.SessionConfigOption {
	modelValues := acp.SessionConfigSelectOptionsUngrouped(models)
	reasoningValues := acp.SessionConfigSelectOptionsUngrouped(reasoning)
	return []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{
			Id: acp.SessionConfigId("model"), Name: "Model",
			CurrentValue: acp.SessionConfigValueId(currentModel),
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &modelValues},
			Type:         "select",
		}},
		{Select: &acp.SessionConfigOptionSelect{
			Id: acp.SessionConfigId("effort"), Name: "Effort",
			CurrentValue: reasoningValues[0].Value,
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &reasoningValues},
			Type:         "select",
		}},
	}
}

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
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	result := sdk.Detect(ctx, DetectRequest{RuntimeID: RuntimeIDGemini, WorkingDirectory: workspace, Model: "gemini-3.8-flash", BaseEnvironment: accountconfig.Environment(os.Environ()), LaunchOverride: LaunchOverride{Binary: node, PrefixArgs: []string{cli}, Environment: environment}})
	if result.State != DetectionStateReady || (result.Version != "" && result.Version != "0.62.0") {
		t.Fatalf("official managed detection: %+v", result)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == DiagnosticCodeACPOptionsUnavailable {
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

// acpTestLogger 在 SDK goroutine 启动前取一次底层实例，避免绕过 logx 锁并发取 Logger。
var acpTestLogger = ginxlogx.Instance()

// acpLogCapture 并发安全地收集 SDK 后台收尾与当前测试的日志。
type acpLogCapture struct {
	// mutex 保护日志缓冲区的读写。
	mutex sync.Mutex
	// buffer 保存项目日志器格式化后的内容。
	buffer bytes.Buffer
}

// Write 接收日志输出并防止 SDK 收尾与断言并发访问缓冲区。
func (c *acpLogCapture) Write(data []byte) (int, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.buffer.Write(data)
}

func (c *acpLogCapture) entries(t *testing.T) []map[string]any {
	t.Helper()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var entries []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(c.buffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("invalid captured log: %s: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func captureACPLogs(t *testing.T) *acpLogCapture {
	t.Helper()
	logger := acpTestLogger
	output, formatter, level := logger.Out, logger.Formatter, logger.GetLevel()
	capture := &acpLogCapture{}
	logger.SetOutput(capture)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logger.SetOutput(output)
		logger.SetFormatter(formatter)
		logger.SetLevel(level)
	})
	return capture
}

func TestACPLogBridgeSuppressesRoutineClosureAndKeepsFailures(t *testing.T) {
	capture := captureACPLogs(t)
	AdapterLogger(context.Background())
	for _, cause := range []string{"peer connection closed", "connection closed", "io: read/write on closed pipe", "read |0: file already closed"} {
		slog.Info("connection closed", "cause", cause)
	}
	slog.Info("connection closed", "cause", "read: connection reset by peer")
	slog.Error("failed to queue notification; closing connection", "err", "notification queue overflow")
	slog.Error("connection closed", "cause", "peer connection closed")
	entries := capture.entries(t)
	if len(entries) != 3 || entries[0]["level"] != "warning" || entries[1]["level"] != "error" || entries[2]["level"] != "error" {
		t.Fatalf("closure filtering lost errors or retained routine logs: %+v", entries)
	}
}

func TestACPNativeStderrKeepsLevelsFragmentsAndIdentity(t *testing.T) {
	capture := captureACPLogs(t)
	ctx := acpAgentLogContext(context.Background(), acpAdapterConfig{RuntimeID: "profile", DetectionID: "probe-one"}, defaultACPRuntimeBindings()["gemini"])
	logger := AdapterLogger(ctx)
	logger.Warn("native ACP stderr", "stderr", "{\"level\":\"in")
	logger.Warn("native ACP stderr", "stderr", "fo\",\"msg\":\"ready\"}\n{\"level\":\"error\",\"msg\":\"failed\"}\n[DEBUG] details\n[STARTUP] Phase 'cli_startup' was started but never ended. Skipping metrics.\n")
	logger.Warn("native ACP stderr", "stderr", `{"level":"info","error":"no provider configured"}`+"\nunknown diagnostic")
	logger.Warn("native ACP stderr", "stderr", "\n")
	logger.Warn("native ACP stderr", "stderr", "\n")
	entries := capture.entries(t)
	want := []string{"info", "error", "warning", "warning"}
	if len(entries) != len(want) {
		t.Fatalf("fragmented lines, debug filtering or line boundaries incorrect: %+v", entries)
	}
	for i, entry := range entries {
		message := entry["msg"].(string)
		if entry["level"] != want[i] {
			t.Fatalf("entry %d has level %v, want %s: %s", i, entry["level"], want[i], message)
		}
		for _, field := range []string{`runtime_id="profile"`, `base_runtime_id="gemini"`, `operation="detect"`, `detection_id="probe-one"`, `agent_id="`} {
			if !strings.Contains(message, field) {
				t.Fatalf("missing diagnostic identity %q: %s", field, message)
			}
		}
	}
}

func TestACPNativeStderrRecognizesExplicitTextLevels(t *testing.T) {
	for line, want := range map[string]slog.Level{
		"[INFO] ready":                                     slog.LevelInfo,
		"WARN provider unavailable":                        slog.LevelWarn,
		"ERROR request failed":                             slog.LevelError,
		"2026/10/15 17:30:52 INFO initialized":             slog.LevelInfo,
		"time=2026-10-15 level=debug msg=details":          slog.LevelDebug,
		`{"severity":"FATAL","msg":"failed"}`:              slog.LevelError,
		"Traceback (most recent call last)":                slog.LevelWarn,
		"ValueError: invalid option level=debug":           slog.LevelWarn,
		`time=2026-10-15 msg="invalid option level=debug"`: slog.LevelWarn,
	} {
		if got := acpNativeStderrLevel(line); got != want {
			t.Errorf("%q level = %v, want %v", line, got, want)
		}
	}
}

func TestACPDetectionLogsNativeProcessIdentity(t *testing.T) {
	capture := captureACPLogs(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	profile := ProfileSpec{ID: "logging-fixture", BaseRuntimeID: RuntimeIDWorkBuddy, BinaryCandidates: []string{binary}, PrefixArgs: []string{"-test.run=^TestAllyNativeACPProcess$", "--"}, Environment: map[string]string{"ALLY_NATIVE_ACP_TEST": RuntimeIDWorkBuddy, "ALLY_NATIVE_ACP_STDERR": "[INFO] native fixture ready\n[ERROR] native fixture diagnostic\n"}}
	sdk, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{profile}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer sdk.Close(ctx)
	for range 2 {
		if result := sdk.Detect(ctx, DetectRequest{RuntimeID: profile.ID, BaseEnvironment: os.Environ()}); result.State != DetectionStateReady {
			t.Fatalf("detection failed: %+v", result)
		}
	}
	ids := map[string]bool{}
	nativeDiagnostics := 0
	for _, entry := range capture.entries(t) {
		message := entry["msg"].(string)
		if strings.Contains(message, "native fixture") {
			nativeDiagnostics++
			if strings.Contains(message, "ready") && entry["level"] != "info" || strings.Contains(message, "diagnostic") && entry["level"] != "error" {
				t.Fatalf("real child diagnostic lost native level: %+v", entry)
			}
			for _, field := range []string{`runtime_id="logging-fixture"`, `base_runtime_id="workbuddy"`, `operation="detect"`, `detection_id="`, `agent_id="`} {
				if !strings.Contains(message, field) {
					t.Fatalf("real child diagnostic lost %s: %s", field, message)
				}
			}
			_, suffix, _ := strings.Cut(message, `detection_id="`)
			id, _, _ := strings.Cut(suffix, `"`)
			ids[id] = true
		}
	}
	if len(ids) != 2 || nativeDiagnostics != 4 {
		t.Fatalf("independent probes did not receive distinct identities: %+v", ids)
	}
}

func TestACPDetectionFailureKeepsProbeIdentity(t *testing.T) {
	capture := captureACPLogs(t)
	backend := newACPBackend()
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		return nil, status_error.RuntimeUnavailable
	})
	_, err := backend.DetectOptions(context.Background(), acpOptionsRequest{RuntimeID: "failed-profile", Prepared: preparedRuntime{definition: runtimeDefinition{baseRuntimeID: RuntimeIDCodex}}})
	if err == nil {
		t.Fatal("fixture did not exercise a detection failure")
	}
	entries := capture.entries(t)
	if len(entries) != 1 || entries[0]["level"] != "warning" {
		t.Fatalf("detection failure was hidden: %+v", entries)
	}
	message := entries[0]["msg"].(string)
	for _, field := range []string{"ACP option detection failed", `runtime_id="failed-profile"`, `operation="detect"`, `detection_id="`, `agent_id="`, `error=`} {
		if !strings.Contains(message, field) {
			t.Fatalf("detection failure lost %s: %s", field, message)
		}
	}
}

func TestACPDetectionDeadlineKeepsProbeIdentity(t *testing.T) {
	capture := captureACPLogs(t)
	backend := newACPBackend()
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(ctx context.Context, _ acpAdapterConfig) (acp.Agent, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := backend.DetectOptions(ctx, acpOptionsRequest{RuntimeID: "deadline-profile", Prepared: preparedRuntime{definition: runtimeDefinition{baseRuntimeID: RuntimeIDCodex}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fixture did not exercise a detection deadline: %v", err)
	}
	entries := capture.entries(t)
	if len(entries) != 1 || entries[0]["level"] != "warning" {
		t.Fatalf("detection deadline was hidden: %+v", entries)
	}
	message := entries[0]["msg"].(string)
	for _, field := range []string{"ACP option detection failed", `runtime_id="deadline-profile"`, `detection_id="`, "context deadline exceeded"} {
		if !strings.Contains(message, field) {
			t.Fatalf("detection deadline lost %s: %s", field, message)
		}
	}
}

func TestACPLogsRedactBoundPluginAuthenticationValues(t *testing.T) {
	capture := captureACPLogs(t)
	token := "oauth-sentinel-private-174"
	ctx := acpAgentLogContext(context.Background(), acpAdapterConfig{RuntimeID: "codex", Environment: []string{"ALLY_PLUGIN_AUTH_ABC=Bearer " + token}}, defaultACPRuntimeBindings()["codex"])
	logger := AdapterLogger(ctx)
	logger.Warn("Codex app-server stderr", "stderr", "Authorization: Bearer "+token)
	logger.With("env", "ALLY_PLUGIN_AUTH_ABC=Bearer "+token).WithGroup("launch").Error("failure "+token, "error", errors.New("rejected "+token))
	for _, entry := range capture.entries(t) {
		data, _ := json.Marshal(entry)
		if bytes.Contains(data, []byte(token)) {
			t.Fatal("plugin authorization appeared in diagnostic log")
		}
	}
}

// acpAgentFactory 仅供管理协议替身消费冻结的启动事实。
type acpAgentFactory func(context.Context, acpAdapterConfig) (acp.Agent, error)

// fakeCodexACPAgent 提供无模型调用的管理协议替身，供探测与分叉测试复用。
type fakeCodexACPAgent struct {
	// mutex 保护请求计数与关闭事实。
	mutex sync.Mutex
	// runtimeVersion 是真实 CLI 版本元数据替身。
	runtimeVersion string
	// newCalls 记录临时会话创建次数。
	newCalls int
	// promptCalls 检查管理操作没有发起模型回合。
	promptCalls int
	// closeSessionCalls 记录会话资源释放次数。
	closeSessionCalls int
	// configCalls 记录目录模型切换次数。
	configCalls int
	// selectedConfigs 保存已请求的配置值。
	selectedConfigs map[acp.SessionConfigId]acp.SessionConfigValueId
	// configOptions 是临时会话初始配置目录。
	configOptions []acp.SessionConfigOption
	// configOptionsByModel 保存每个模型的实际目录回执。
	configOptionsByModel map[acp.SessionConfigValueId][]acp.SessionConfigOption
	// sessionID 覆盖临时会话身份。
	sessionID acp.SessionId
	// newSessionError 模拟认证或创建故障。
	newSessionError error
	// closed 记录原生管理适配器已释放。
	closed bool
}

// installFakeCodexACPAgent 替换 SDK 的 Codex Agent 工厂并在测试结束时释放资源。
func installFakeCodexACPAgent(
	t *testing.T,
	sdk *Catalog,
	agent *fakeCodexACPAgent,
) {
	t.Helper()
	backend, ok := sdk.acp.(*acpBackend)
	if !ok {
		t.Fatalf("ACP backend = %T", sdk.acp)
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return agent, nil
	})
	t.Cleanup(func() {
		if err := sdk.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

// replaceACPAgentFactory 只替换目标测试 Backend 的一个 Runtime Agent 工厂。
func replaceACPAgentFactory(
	backend *acpBackend,
	baseRuntimeID string,
	factory acpAgentFactory,
) {
	previous := backend.openAgent
	backend.openAgent = func(ctx context.Context, prepared *native.LaunchLease, config acpAdapterConfig) (acp.Agent, error) {
		if config.BaseRuntimeID == baseRuntimeID {
			return factory(ctx, config)
		}
		return previous(ctx, prepared, config)
	}
}

// testCodexACPConfigOptions 创建 model 与当前模型 reasoning_effort 配置。
func testCodexACPConfigOptions(
	currentModel string,
	models []acp.SessionConfigSelectOption,
	reasoning []acp.SessionConfigSelectOption,
) []acp.SessionConfigOption {
	modelValues := acp.SessionConfigSelectOptionsUngrouped(models)
	options := []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("model"),
		Name:         "Model",
		CurrentValue: acp.SessionConfigValueId(currentModel),
		Options: acp.SessionConfigSelectOptions{
			Ungrouped: &modelValues,
		},
		Type: "select",
	}}}
	if reasoning == nil {
		return options
	}
	reasoningValues := acp.SessionConfigSelectOptionsUngrouped(reasoning)
	options = append(options, acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("reasoning_effort"),
		Name:         "Reasoning effort",
		CurrentValue: reasoningValues[0].Value,
		Options: acp.SessionConfigSelectOptions{
			Ungrouped: &reasoningValues,
		},
		Type: "select",
	}})
	return options
}

// testCodexACPAgentWithCatalog 创建声明两个模型及不同推理强度的 fake Agent。
func testCodexACPAgentWithCatalog() *fakeCodexACPAgent {
	models := []acp.SessionConfigSelectOption{
		{Value: "gpt-fixture", Name: "GPT Fixture"},
		{Value: "gpt-fast", Name: "GPT Fast"},
	}
	fixtureOptions := testCodexACPConfigOptions(
		"gpt-fixture",
		models,
		[]acp.SessionConfigSelectOption{
			{Value: "medium", Name: "Medium"},
			{Value: "high", Name: "High"},
		},
	)
	fastOptions := testCodexACPConfigOptions(
		"gpt-fast",
		models,
		[]acp.SessionConfigSelectOption{{Value: "low", Name: "Low"}},
	)
	return &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		configOptions:   fixtureOptions,
		configOptionsByModel: map[acp.SessionConfigValueId][]acp.SessionConfigOption{
			"gpt-fixture": fixtureOptions,
			"gpt-fast":    fastOptions,
		},
	}
}

// cloneACPConfigOptions 为 fake Agent 返回非 nil 的 ACP 配置切片。
func cloneACPConfigOptions(options []acp.SessionConfigOption) []acp.SessionConfigOption {
	return append([]acp.SessionConfigOption{}, options...)
}

// Initialize 返回 Codex 测试所需的 Session 能力。
func (agent *fakeCodexACPAgent) Initialize(
	context.Context,
	acp.InitializeRequest,
) (acp.InitializeResponse, error) {
	version := agent.runtimeVersion
	if version == "" {
		version = "0.149.1"
	}
	sessionCapabilities := acp.SessionCapabilities{
		AdditionalDirectories: &acp.SessionAdditionalDirectoriesCapabilities{},
		Close:                 &acp.SessionCloseCapabilities{},
		Resume:                &acp.SessionResumeCapabilities{},
	}
	return acp.InitializeResponse{
		ProtocolVersion: acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{
			LoadSession: true,
			McpCapabilities: acp.McpCapabilities{
				Http: true,
				Sse:  true,
			},
			PromptCapabilities: acp.PromptCapabilities{
				Image: true,
			},
			SessionCapabilities: sessionCapabilities,
		},
		AgentInfo: &acp.Implementation{
			Name:    "ally-test-acp-agent",
			Version: "fake-agent 1.2.3",
			Meta: map[string]any{
				"runtime": map[string]any{"version": version},
			},
		},
		Meta: map[string]any{
			"steering": map[string]any{"supported": true},
		},
	}, nil
}

// NewSession 返回固定的新 Session。
func (agent *fakeCodexACPAgent) NewSession(
	_ context.Context,
	_ acp.NewSessionRequest,
) (acp.NewSessionResponse, error) {
	agent.mutex.Lock()
	agent.newCalls++
	sessionID := agent.sessionID
	if sessionID == "" {
		sessionID = "codex-acp-session"
	}
	newSessionError := agent.newSessionError
	agent.mutex.Unlock()
	if newSessionError != nil {
		return acp.NewSessionResponse{}, newSessionError
	}
	return acp.NewSessionResponse{
		SessionId:     sessionID,
		ConfigOptions: cloneACPConfigOptions(agent.configOptions),
	}, nil
}

// SetSessionConfigOption 记录模型和推理配置。
func (agent *fakeCodexACPAgent) SetSessionConfigOption(
	_ context.Context,
	request acp.SetSessionConfigOptionRequest,
) (acp.SetSessionConfigOptionResponse, error) {
	options := cloneACPConfigOptions(agent.configOptions)
	if request.ValueId != nil {
		agent.mutex.Lock()
		agent.configCalls++
		agent.selectedConfigs[request.ValueId.ConfigId] = request.ValueId.Value
		if request.ValueId.ConfigId == acp.SessionConfigId("model") {
			if configured, exists := agent.configOptionsByModel[request.ValueId.Value]; exists {
				options = cloneACPConfigOptions(configured)
			}
		}
		agent.mutex.Unlock()
	}
	return acp.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
}

// Cancel 满足 Agent；测试 Prompt 不阻塞。
func (*fakeCodexACPAgent) Cancel(context.Context, acp.CancelNotification) error {
	return nil
}

// CloseSession 满足 Agent 的 Session 资源释放接口。
func (agent *fakeCodexACPAgent) CloseSession(
	context.Context,
	acp.CloseSessionRequest,
) (acp.CloseSessionResponse, error) {
	agent.mutex.Lock()
	agent.closeSessionCalls++
	agent.mutex.Unlock()
	return acp.CloseSessionResponse{}, nil
}

// ListSessions 返回未公开能力错误。
func (*fakeCodexACPAgent) ListSessions(
	context.Context,
	acp.ListSessionsRequest,
) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionList)
}

// Authenticate 返回未使用的空响应。
func (*fakeCodexACPAgent) Authenticate(
	context.Context,
	acp.AuthenticateRequest,
) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}

// Logout 返回未使用的空响应。
func (*fakeCodexACPAgent) Logout(
	context.Context,
	acp.LogoutRequest,
) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}

// Close 记录 acpserver 已释放 Adapter。
func (agent *fakeCodexACPAgent) Close(context.Context) error {
	agent.mutex.Lock()
	agent.closed = true
	agent.mutex.Unlock()
	return nil
}

// Prompt 使任何意外模型调用可被测试观察并立即失败。
func (agent *fakeCodexACPAgent) Prompt(context.Context, acp.PromptRequest) (acp.PromptResponse, error) {
	agent.mutex.Lock()
	agent.promptCalls++
	agent.mutex.Unlock()
	return acp.PromptResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionPrompt)
}

// SetSessionMode 不参与目录读取，保留标准接口的明确拒绝。
func (*fakeCodexACPAgent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionSetMode)
}

var _ acp.Agent = (*fakeCodexACPAgent)(nil)

// ResumeSession 拒绝管理能力之外的恢复请求，防止探测重用真实会话。
func (*fakeCodexACPAgent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, acp.NewMethodNotFound(acp.AgentMethodSessionResume)
}

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
			sdk, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{profile}})
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

// TestNewNativeRuntimeRegistry 验证运行时可发现且交付真实指令与技能目录，不伪造模型目录。
func TestNewNativeRuntimeRegistry(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opencode", "gemini", "grok"} {
		definition, ok := definitions[id]
		if !ok {
			t.Errorf("runtime missing: %s", id)
			continue
		}
		instruction := "AGENTS.md"
		if id == "gemini" {
			instruction = "GEMINI.md"
		}
		if definition.instructionFileName != instruction || definition.skillDirectoryName != "."+id || len(definition.models) != 0 {
			t.Errorf("invented native files/models: %+v", definition)
		}
		binding, ok := defaultACPRuntimeBindings()[id]
		if !ok || !binding.descriptor.NativeProcess {
			t.Errorf("unsafe lifecycle binding: %s", id)
		}
	}
}

// TestNewNativeVersionDoesNotUseBridgeBuild 防止版本探测超时后把适配器构建号当作 CLI 版本。
func TestNewNativeVersionDoesNotUseBridgeBuild(t *testing.T) {
	for _, id := range []string{"opencode", "gemini", "grok"} {
		catalog := acpInitializeCatalog(defaultACPRuntimeBindings()[id], acp.InitializeResponse{AgentInfo: &acp.Implementation{Name: id, Version: "bridge-build"}})
		if catalog.Version != "" {
			t.Errorf("%s exposed bridge version as CLI: %s", id, catalog.Version)
		}
		actual := acpInitializeCatalog(defaultACPRuntimeBindings()[id], acp.InitializeResponse{AgentInfo: &acp.Implementation{Name: id, Version: "bridge-build", Meta: map[string]any{"runtime": map[string]any{"version": "native-1.2.3"}}}})
		if actual.Version != "native-1.2.3" {
			t.Errorf("native version lost: %s", actual.Version)
		}
	}
}

// TestExistingNativeVersionFallback 保留原有原生绑定的握手版本兼容行为。
func TestExistingNativeVersionFallback(t *testing.T) {
	catalog := acpInitializeCatalog(defaultACPRuntimeBindings()["kimi"], acp.InitializeResponse{AgentInfo: &acp.Implementation{Version: "legacy-cli"}})
	if catalog.Version != "legacy-cli" {
		t.Fatalf("legacy version lost: %s", catalog.Version)
	}
}

func TestACPCatalogOptionsPreservesEmptyCatalog(t *testing.T) {
	models, reasoning := acpCatalogOptions(acpOptionCatalog{})
	if len(models) != 0 {
		t.Fatalf("models = %#v, want none", models)
	}
	if len(reasoning) != 0 {
		t.Fatalf("reasoning = %#v, want none", reasoning)
	}
}

// TestNativeACPModelThinkingCatalog 验证原生目录按模型传递思考选项，且探测不切换模型。
func TestNativeACPModelThinkingCatalog(t *testing.T) {
	thought := func(values ...string) []acp.SessionConfigOption {
		options := acp.SessionConfigSelectOptionsUngrouped{}
		for _, value := range values {
			options = append(options, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(value), Name: value})
		}
		return []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: options[0].Value, Options: acp.SessionConfigSelectOptions{Ungrouped: &options}}}}
	}
	models := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "composer", Name: "Composer", Meta: map[string]any{"acp-go/model-config-options": []acp.SessionConfigOption{}}},
		{Value: "claude", Name: "Claude", Meta: map[string]any{"acp-go/model-config-options": thought("off", "medium", "high")}},
		{Value: "gpt", Name: "GPT", Meta: map[string]any{"acp-go/model-config-options": thought("low", "high")}},
	}
	agent := &catalogDecoder{binding: defaultACPRuntimeBindings()["cursor"]}
	catalog, err := agent.detectSessionOptions(context.Background(), "catalog", []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "model", CurrentValue: "composer", Options: acp.SessionConfigSelectOptions{Ungrouped: &models}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultModel != "composer" {
		t.Fatalf("DefaultModel = %q, want composer", catalog.DefaultModel)
	}
	for index, want := range [][]string{nil, {"off", "medium", "high"}, {"low", "high"}} {
		got := catalog.Models[index].ReasoningOptions
		if len(got) != len(want) {
			t.Fatalf("%s thinking = %+v, want %v", catalog.Models[index].ID, got, want)
		}
		for n := range want {
			if got[n].ID != want[n] {
				t.Fatalf("thinking = %+v", got)
			}
		}
	}
}

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

// TestQoderCNRegistryAndIsolation 验证中国版与国际版分别保存命令和用户配置根，共用项目技能目录，共用同一权限契约。
func TestQoderCNRegistryAndIsolation(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[RuntimeIDQoderCN]
	if definition.name != "Qoder CN" || definition.instructionFileName != "AGENTS.md" || definition.skillDirectoryName != ".qoder" || definition.configHomeEnvironmentVariable != "QODERCN_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"qodercn", "qoder-cn", "qoderclicn"}) || len(definition.models) != 0 {
		t.Fatalf("incorrect CN definition: %+v", definition)
	}
	if !slices.Contains(BaseRuntimeIDs(), RuntimeIDQoderCN) {
		t.Fatal("connector matrix omitted Qoder CN")
	}
	binding := defaultACPRuntimeBindings()[RuntimeIDQoderCN]
	if binding.descriptor.ID != RuntimeIDQoderCN || binding.descriptor.Name != "Qoder CN" || !binding.descriptor.NativeProcess {
		t.Fatal("CN identity or process isolation lost")
	}
	for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
		if supported, _ := ConnectorCompatibility(RuntimeIDQoderCN, transport); !supported {
			t.Fatalf("missing transport %s", transport)
		}
	}
	for flag := range protectedPrefixArguments[RuntimeIDQoder] {
		if LaunchOverrideArgumentsAllowed(RuntimeIDQoderCN, []string{flag + "=override"}) {
			t.Fatalf("unsafe CN override accepted: %s", flag)
		}
	}
}

// TestQoderNativeInstallDiscovery 验证 PATH 缺省时也能解析官方原生路径，两个地区与显式路径覆盖互不混用。
func TestQoderNativeInstallDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	for _, id := range []string{RuntimeIDQoder, RuntimeIDQoderCN} {
		t.Run(id, func(t *testing.T) {
			home := t.TempDir()
			sdk, err := NewCatalog(CatalogConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close(context.Background())
			definition := sdk.definitions[id]
			binary := filepath.Join(home, definition.userBinaryCandidates[0])
			if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '1.2.3\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			request := DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
			resolved, err := sdk.ResolveExecutable(request)
			if err != nil || resolved != binary {
				t.Fatalf("native executable = %s, %v", resolved, err)
			}
			installations := sdk.DiscoverInstallations(context.Background(), request, nil)
			found := false
			for _, installation := range installations {
				if installation.Path == binary && installation.Available && installation.Version == "1.2.3" {
					found = true
				}
			}
			if !found {
				t.Fatalf("native installation omitted: %+v", installations)
			}
			// 国际版与中国版之间不做透明登录或可执行文件替换。
			other := RuntimeIDQoder
			if id == other {
				other = RuntimeIDQoderCN
			}
			request.RuntimeID = other
			if _, err := sdk.ResolveExecutable(request); err == nil {
				t.Fatal("cross-region executable fallback")
			}
			request.RuntimeID = id
			request.LaunchOverride.Binary = filepath.Join(home, "missing-explicit-binary")
			if _, err := sdk.ResolveExecutable(request); err == nil {
				t.Fatal("explicit binary silently fell back to native installation")
			}
		})
	}
}

// TestQoderDesktopDispatcherIsNotNativeInstall 防止仅有桌面版 entry 分发脚本时误报安装了原生 CLI。
func TestQoderDesktopDispatcherIsNotNativeInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	for _, id := range []string{RuntimeIDQoder, RuntimeIDQoderCN} {
		home := t.TempDir()
		sdk, err := NewCatalog(CatalogConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer sdk.Close(context.Background())
		definition := sdk.definitions[id]
		entry := filepath.Join(home, "."+id, "entry", definition.binaryCandidates[0])
		if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entry, []byte("#!/bin/sh\nexit 127\n"), 0700); err != nil {
			t.Fatal(err)
		}
		request := DetectRequest{RuntimeID: id, HomeDirectory: home, BaseEnvironment: []string{"PATH="}}
		if _, err := sdk.ResolveExecutable(request); err == nil {
			t.Fatal("desktop dispatcher treated as installed CLI")
		}
	}
}

// TestQoderRegistryAndPermissionCatalog 验证原生目录、传输和实际模式目录。
func TestQoderRegistryAndPermissionCatalog(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[RuntimeIDQoder]
	if definition.name != "Qoder" || definition.instructionFileName != "AGENTS.md" || definition.skillDirectoryName != ".qoder" || definition.configHomeEnvironmentVariable != "QODER_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"qoder", "qodercli"}) || len(definition.models) != 0 {
		t.Fatalf("native definition: %+v", definition)
	}
	binding := defaultACPRuntimeBindings()[RuntimeIDQoder]
	if !binding.descriptor.NativeProcess {
		t.Fatal("missing native factory")
	}
	for _, bypass := range []string{constants.RuntimeQoderBypassModeID, constants.RuntimeQoderYoloModeID} {
		modes := []acp.SessionMode{{Id: "default"}, {Id: acp.SessionModeId(bypass)}}
		if got := binding.permissionLevels(nil, modes); !reflect.DeepEqual(got, []PermissionLevel{PermissionLevelDefault, PermissionLevelFullAccess}) {
			t.Fatalf("invented permissions: %v", got)
		}
	}
	modes := []acp.SessionMode{{Id: "default"}, {Id: "auto"}, {Id: "yolo"}}
	if got := binding.permissionLevels(nil, modes); !reflect.DeepEqual(got, []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}) {
		t.Fatalf("auto mode lost: %v", got)
	}
	for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
		if supported, _ := ConnectorCompatibility(RuntimeIDQoder, transport); !supported {
			t.Fatalf("missing transport: %s", transport)
		}
	}
	for flag := range protectedPrefixArguments[RuntimeIDQoder] {
		if LaunchOverrideArgumentsAllowed(RuntimeIDQoder, []string{flag}) || LaunchOverrideArgumentsAllowed(RuntimeIDQoder, []string{flag + "=override"}) {
			t.Fatalf("unsafe override: %s", flag)
		}
	}
}

// TestWorkBuddyFreeModelCatalog 验证免费标记只来自当前零积分声明，不影响模型选择或其他智能体。
func TestWorkBuddyFreeModelCatalog(t *testing.T) {
	tests := []struct {
		// option 是原生 ACP 返回的模型选项。
		option acp.SessionConfigSelectOption
		// want 是 WorkBuddy 对外提供的模型展示名称。
		want string
	}{
		{acp.SessionConfigSelectOption{Value: "hy3", Name: "Hy3", Description: acp.Ptr("x0.00 credits")}, "Hy3 (free)"},
		{acp.SessionConfigSelectOption{Value: "hy4-preview", Name: "Hy4 preview", Description: acp.Ptr("x0.29 credits")}, "Hy4 preview"},
		{acp.SessionConfigSelectOption{Value: "unknown", Name: "Unknown"}, "Unknown"},
		{acp.SessionConfigSelectOption{Value: "night-only", Name: "Night only", Description: acp.Ptr("x0.00 credits at night")}, "Night only"},
		{acp.SessionConfigSelectOption{Value: "unsupported", Name: "Unsupported", Description: acp.Ptr("0.00")}, "Unsupported"},
		{acp.SessionConfigSelectOption{Value: "already-free", Name: "Already (free)", Description: acp.Ptr("x0.00 credits")}, "Already (free)"},
		{acp.SessionConfigSelectOption{Value: "unnamed", Description: acp.Ptr("x0.00 credits")}, "unnamed (free)"},
	}
	models := make(acp.SessionConfigSelectOptionsUngrouped, 0, len(tests))
	for _, test := range tests {
		models = append(models, test.option)
	}
	options := []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{
		Id: "model", CurrentValue: "hy3", Options: acp.SessionConfigSelectOptions{Ungrouped: &models},
	}}}
	for _, binding := range []acpRuntimeBinding{defaultACPRuntimeBindings()["workbuddy"], defaultACPRuntimeBindings()["cursor"]} {
		t.Run(binding.descriptor.ID, func(t *testing.T) {
			agent := &catalogDecoder{binding: binding}
			catalog, err := agent.detectSessionOptions(context.Background(), "free-catalog", options, nil)
			if err != nil {
				t.Fatal(err)
			}
			if catalog.DefaultModel != "hy3" || len(catalog.Models) != len(tests) {
				t.Fatalf("incorrect catalog: %+v", catalog)
			}
			for index, test := range tests {
				want := test.want
				if binding.descriptor.ID != RuntimeIDWorkBuddy {
					want = test.option.Name
					if want == "" {
						want = string(test.option.Value)
					}
				}
				if got := catalog.Models[index]; got.ID != string(test.option.Value) || got.Name != want {
					t.Fatalf("model %q = %+v, want name %q and original ID", test.option.Value, got, want)
				}
			}
		})
	}
	if models[0].Name != "Hy3" {
		t.Fatal("model name annotation mutated the upstream option")
	}
	models[0].Description = acp.Ptr("x0.29 credits")
	agent := &catalogDecoder{binding: defaultACPRuntimeBindings()["workbuddy"]}
	catalog, err := agent.detectSessionOptions(context.Background(), "refreshed-catalog", options, nil)
	if err != nil || catalog.Models[0].Name != "Hy3" {
		t.Fatalf("stale free label after catalog refresh: %+v, %v", catalog, err)
	}
}

// TestWorkBuddyConcreteModelCatalog 验证目录隐藏自动路由与思考开关，保留实际强度及模型专属目录。
func TestWorkBuddyConcreteModelCatalog(t *testing.T) {
	levels := acp.SessionConfigSelectOptionsUngrouped{}
	for _, id := range []string{"minimal", "low", "medium", "high", "xhigh", "max", "enabled", "disabled"} {
		levels = append(levels, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(id), Name: id})
	}
	glmLevels := acp.SessionConfigSelectOptionsUngrouped{{Value: "high", Name: "High"}, {Value: "enabled", Name: "On (default)"}}
	models := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "fast-model", Name: "快速"},
		{Value: "step-5-preview", Name: "Step-5-Preview", Description: acp.Ptr("x0.00 credits")},
		{Value: "balanced-model", Name: "均衡"},
		{Value: "glm-5.3", Name: "GLM-5.3", Meta: map[string]any{"acp-go/model-config-options": []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &glmLevels}}}}}},
		{Value: "deep-model", Name: "极致"},
		{Value: "unknown-model", Name: "Unknown"},
	}
	options := []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{Id: "model", CurrentValue: "step-5-preview", Options: acp.SessionConfigSelectOptions{Ungrouped: &models}}},
		{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &levels}}},
	}
	agent := &catalogDecoder{binding: defaultACPRuntimeBindings()["workbuddy"]}
	catalog, err := agent.detectSessionOptions(context.Background(), "concrete-catalog", options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultModel != "step-5-preview" || len(catalog.Models) != 3 {
		t.Fatalf("incorrect concrete catalog: %+v", catalog)
	}
	model, reasoning := NormalizeDetectedDefaults(DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "step-5-preview", "enabled")
	if model != "step-5-preview" || reasoning != "minimal" {
		t.Fatalf("hidden toggle remained selected: model=%q reasoning=%q", model, reasoning)
	}
	wantLevels := []ReasoningOption{{ID: "minimal", Name: "minimal"}, {ID: "low", Name: "low"}, {ID: "medium", Name: "medium"}, {ID: "high", Name: "high"}, {ID: "xhigh", Name: "xhigh"}, {ID: "max", Name: "max"}}
	if catalog.Models[0].ID != "step-5-preview" || catalog.Models[0].Name != "Step-5-Preview (free)" || len(catalog.ReasoningOptions) != 0 || !reflect.DeepEqual(catalog.Models[0].ReasoningOptions, wantLevels) {
		t.Fatalf("concrete levels changed: %+v", catalog)
	}
	if catalog.Models[1].ID != "glm-5.3" || !reflect.DeepEqual(catalog.Models[1].ReasoningOptions, []ReasoningOption{{ID: "high", Name: "High"}}) || catalog.Models[2].ID != "unknown-model" || catalog.Models[2].ReasoningOptions != nil {
		t.Fatalf("model-specific options lost or inferred: %+v", catalog.Models)
	}
	if len(levels) != 8 || len(models) != 6 || len(glmLevels) != 2 {
		t.Fatal("catalog filtering mutated native options")
	}
	other := &catalogDecoder{binding: defaultACPRuntimeBindings()["cursor"]}
	unchanged, err := other.detectSessionOptions(context.Background(), "other-catalog", options, nil)
	if err != nil || len(unchanged.Models) != 6 || len(unchanged.ReasoningOptions) != 8 || unchanged.DefaultReasoning != "enabled" {
		t.Fatalf("WorkBuddy product scope leaked to another runtime: %+v, %v", unchanged, err)
	}
}

// TestWorkBuddyCatalogDefaultReasoning 验证有效默认值保持不变，隐藏项不再成为默认或跨模型能力。
func TestWorkBuddyCatalogDefaultReasoning(t *testing.T) {
	levels := []ReasoningOption{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}, {ID: "enabled", Name: "On (default)"}}
	for _, selected := range []string{"high", "enabled"} {
		catalog := workBuddyCatalogOptions(acpOptionCatalog{DefaultModel: "glm-5.3", DefaultReasoning: selected, Models: []ModelOption{{ID: "glm-5.3", ReasoningOptions: levels}}})
		want := "high"
		if selected == "enabled" {
			want = "low"
		}
		_, reasoning := NormalizeDetectedDefaults(DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "glm-5.3", "enabled")
		if reasoning != want || len(catalog.Models[0].ReasoningOptions) != 2 {
			t.Fatalf("default %q: %+v", selected, catalog)
		}
	}
	catalog := workBuddyCatalogOptions(acpOptionCatalog{DefaultModel: "fast-model", DefaultReasoning: "high", ReasoningOptions: levels, Models: []ModelOption{{ID: "fast-model", ReasoningOptions: levels}, {ID: "glm-5.3"}}})
	if catalog.DefaultModel != "" || catalog.DefaultReasoning != "" || catalog.ReasoningOptions != nil || catalog.Models[0].ReasoningOptions != nil {
		t.Fatalf("hidden model options leaked to the replacement model: %+v", catalog)
	}
	catalog = workBuddyCatalogOptions(acpOptionCatalog{DefaultModel: "hy3", DefaultReasoning: "enabled", Models: []ModelOption{{ID: "hy3", ReasoningOptions: []ReasoningOption{{ID: "enabled"}, {ID: "disabled"}}}}})
	_, reasoning := NormalizeDetectedDefaults(DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "hy3", "enabled")
	if reasoning != "" || len(catalog.Models[0].ReasoningOptions) != 0 || len(catalog.ReasoningOptions) != 0 {
		t.Fatalf("invented concrete thinking levels for a toggle-only model: %+v", catalog)
	}
}

// TestWorkBuddyDetectsConcreteModelThinking 验证默认模型被隐藏或仅有开关时，仍逐模型取得真实强度。
func TestWorkBuddyDetectsConcreteModelThinking(t *testing.T) {
	for _, test := range []struct {
		// name 标识默认模型或错误回执的回归场景。
		name string
		// current 是临时会话初始生效的模型。
		current string
		// confirm 控制模型 setter 是否确认本次目标模型。
		confirm bool
	}{{"hidden default", "fast-model", true}, {"toggle-only default", "hy3", true}, {"concrete default", "step-5-preview", true}, {"mismatched receipt", "fast-model", false}} {
		t.Run(test.name, func(t *testing.T) {
			current := test.current
			glmLevels := acp.SessionConfigSelectOptionsUngrouped{{Value: "high", Name: "High"}, {Value: "enabled", Name: "On (default)"}}
			models := acp.SessionConfigSelectOptionsUngrouped{
				{Value: "fast-model", Name: "快速"},
				{Value: "hy3", Name: "Hy3"},
				{Value: "balanced-model", Name: "均衡"},
				{Value: "step-5-preview", Name: "Step-5-Preview", Description: acp.Ptr("x0.00 credits")},
				{Value: "deep-model", Name: "极致"},
				{Value: "glm-5.3", Name: "GLM-5.3", Meta: map[string]any{"acp-go/model-config-options": []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &glmLevels}}}}}},
			}
			config := func(model string, levels ...string) []acp.SessionConfigOption {
				values := acp.SessionConfigSelectOptionsUngrouped{}
				for _, level := range levels {
					values = append(values, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(level), Name: level})
				}
				return []acp.SessionConfigOption{
					{Select: &acp.SessionConfigOptionSelect{Id: "model", CurrentValue: acp.SessionConfigValueId(model), Options: acp.SessionConfigSelectOptions{Ungrouped: &models}}},
					{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &values}}},
				}
			}
			configs := map[acp.SessionConfigValueId][]acp.SessionConfigOption{
				"fast-model":     config("fast-model", "medium", "enabled"),
				"hy3":            config("hy3", "enabled", "disabled"),
				"step-5-preview": config("step-5-preview", "minimal", "low", "medium", "high", "xhigh", "max", "enabled"),
			}
			if !test.confirm {
				configs["step-5-preview"] = configs["hy3"]
			}
			fixture := &fakeCodexACPAgent{sessionID: "thinking-catalog", selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId), configOptions: configs[acp.SessionConfigValueId(current)], configOptionsByModel: configs}
			backend := newACPBackend()
			t.Cleanup(func() { _ = backend.Close(context.Background()) })
			replaceACPAgentFactory(backend, RuntimeIDWorkBuddy, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return fixture, nil })
			catalog, err := backend.DetectOptions(context.Background(), acpOptionsRequest{
				RuntimeID: RuntimeIDWorkBuddy,
				Prepared: preparedRuntime{
					definition:  runtimeDefinition{id: RuntimeIDWorkBuddy, baseRuntimeID: RuntimeIDWorkBuddy},
					detection:   DetectionResult{ResolvedBinary: "/fake/codebuddy"},
					environment: environmentSnapshot{},
				},
				WorkingDirectory: t.TempDir(),
			})
			if !test.confirm {
				if err == nil || fixture.promptCalls != 0 || fixture.closeSessionCalls != 1 {
					t.Fatalf("unconfirmed model accepted or detection session leaked: err=%v prompts=%d close=%d", err, fixture.promptCalls, fixture.closeSessionCalls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.Models) != 3 || catalog.Models[0].ID != "hy3" || len(catalog.Models[0].ReasoningOptions) != 0 || catalog.Models[1].ID != "step-5-preview" || catalog.Models[1].Name != "Step-5-Preview (free)" || len(catalog.Models[1].ReasoningOptions) != 6 || catalog.Models[1].ReasoningOptions[5].ID != "max" || !reflect.DeepEqual(catalog.Models[2].ReasoningOptions, []ReasoningOption{{ID: "high", Name: "High"}}) {
				t.Fatalf("concrete model thinking lost or borrowed: %+v", catalog)
			}
			if len(catalog.ReasoningOptions) != 0 {
				t.Fatalf("global thinking would leak to toggle-only models: %+v", catalog.ReasoningOptions)
			}
			_, reasoning := NormalizeDetectedDefaults(DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "step-5-preview", "enabled")
			if reasoning != "minimal" {
				t.Fatalf("filtered default strength = %q, want minimal", reasoning)
			}
			wantSwitches := 1
			if current == "fast-model" {
				wantSwitches = 2
			}
			if fixture.configCalls != wantSwitches || fixture.promptCalls != 0 || fixture.newCalls != 1 || fixture.closeSessionCalls != 1 {
				t.Fatalf("temporary detection lifecycle: switches=%d prompts=%d new=%d close=%d", fixture.configCalls, fixture.promptCalls, fixture.newCalls, fixture.closeSessionCalls)
			}
		})
	}
}

// workBuddyFixtureAgent 给共用测试 Agent 补充 WorkBuddy 实际声明的模式目录。
type workBuddyFixtureAgent struct {
	// fakeCodexACPAgent 复用标准 ACP 的请求记录与响应，不引入私有协议。
	*fakeCodexACPAgent
}

// NewSession 保留标准测试行为并返回原生权限菜单。
func (agent *workBuddyFixtureAgent) NewSession(ctx context.Context, request acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	response, err := agent.fakeCodexACPAgent.NewSession(ctx, request)
	response.Modes = &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default", Name: "Default"}, {Id: "auto", Name: "Auto"}, {Id: "bypassPermissions", Name: "Bypass"}}}
	return response, err
}

// TestWorkBuddyRegistryAndPermissionCatalog 只验证仍被探测及安装使用的声明。
func TestWorkBuddyRegistryAndPermissionCatalog(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[RuntimeIDWorkBuddy]
	if definition.name != "WorkBuddy" || definition.instructionFileName != "CODEBUDDY.md" || definition.skillDirectoryName != ".codebuddy" || definition.configHomeEnvironmentVariable != "CODEBUDDY_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"codebuddy", "cbc"}) || len(definition.models) != 0 {
		t.Fatalf("native definition: %+v", definition)
	}
	binding := defaultACPRuntimeBindings()[RuntimeIDWorkBuddy]
	if !binding.descriptor.NativeProcess {
		t.Fatal("missing native factory")
	}
	modes := []acp.SessionMode{{Id: "default"}, {Id: "auto"}, {Id: "bypassPermissions"}, {Id: "plan"}}
	if got := binding.permissionLevels(nil, modes); !reflect.DeepEqual(got, []PermissionLevel{PermissionLevelDefault, PermissionLevelAuto, PermissionLevelFullAccess}) {
		t.Fatalf("native permission catalog: %v", got)
	}
	for _, transport := range []ConnectorTransport{ConnectorTransportStdio, ConnectorTransportStreamableHTTP, ConnectorTransportSSE} {
		if ok, _ := ConnectorCompatibility(RuntimeIDWorkBuddy, transport); !ok {
			t.Fatalf("missing transport: %s", transport)
		}
	}
	for _, flag := range []string{"--acp-transport=http", "--permission-mode=fullAccess", "--resume=old", "--model=other", "--mcp-config=other", "--system-prompt=other", "--multitask"} {
		if LaunchOverrideArgumentsAllowed(RuntimeIDWorkBuddy, []string{flag}) {
			t.Fatalf("managed argument override: %s", flag)
		}
	}
}

// TestConnectorCompatibilityMatchesUpstreamTransports 锁住各 Runtime 的 upstream MCP 边界。
func TestConnectorCompatibilityMatchesUpstreamTransports(t *testing.T) {
	t.Parallel()
	tests := []struct {
		// name 是能力组合名称。
		name string
		// runtimeID 是被检查的基础 Runtime。
		runtimeID string
		// transport 是目标 MCP transport。
		transport ConnectorTransport
		// expected 表示该 transport 是否受支持。
		expected bool
	}{
		{name: "codex stdio", runtimeID: RuntimeIDCodex, transport: ConnectorTransportStdio, expected: true},
		{name: "codex http", runtimeID: RuntimeIDCodex, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "codex sse", runtimeID: RuntimeIDCodex, transport: ConnectorTransportSSE, expected: false},
		{name: "claude stdio", runtimeID: RuntimeIDClaude, transport: ConnectorTransportStdio, expected: true},
		{name: "claude http", runtimeID: RuntimeIDClaude, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "claude sse", runtimeID: RuntimeIDClaude, transport: ConnectorTransportSSE, expected: true},
		{name: "kimi stdio", runtimeID: RuntimeIDKimi, transport: ConnectorTransportStdio, expected: true},
		{name: "kimi http", runtimeID: RuntimeIDKimi, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "kimi sse", runtimeID: RuntimeIDKimi, transport: ConnectorTransportSSE, expected: false},
		{name: "cursor stdio", runtimeID: RuntimeIDCursor, transport: ConnectorTransportStdio, expected: true},
		{name: "cursor http", runtimeID: RuntimeIDCursor, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "cursor sse", runtimeID: RuntimeIDCursor, transport: ConnectorTransportSSE, expected: false},
		{name: "pi stdio", runtimeID: RuntimeIDPi, transport: ConnectorTransportStdio, expected: true},
		{name: "pi http", runtimeID: RuntimeIDPi, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "pi sse", runtimeID: RuntimeIDPi, transport: ConnectorTransportSSE, expected: true},
		{name: "opencode stdio", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportStdio, expected: true},
		{name: "opencode http", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "opencode sse", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportSSE, expected: true},
		{name: "gemini stdio", runtimeID: RuntimeIDGemini, transport: ConnectorTransportStdio, expected: true},
		{name: "gemini http", runtimeID: RuntimeIDGemini, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "gemini sse", runtimeID: RuntimeIDGemini, transport: ConnectorTransportSSE, expected: true},
		{name: "grok stdio", runtimeID: RuntimeIDGrok, transport: ConnectorTransportStdio, expected: true},
		{name: "grok http", runtimeID: RuntimeIDGrok, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "grok sse", runtimeID: RuntimeIDGrok, transport: ConnectorTransportSSE, expected: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, reason := ConnectorCompatibility(test.runtimeID, test.transport)
			if actual != test.expected || (actual && reason != "") || (!actual && reason == "") {
				t.Fatalf("compatibility = %v/%q, want %v", actual, reason, test.expected)
			}
		})
	}
}

func TestDetectedConnectorTransportSupportedUsesCapabilitySnapshot(t *testing.T) {
	detection := DetectionResult{
		State: DetectionStateReady,
		Capabilities: CapabilitySet{
			CapabilityMCPStreamableHTTP: true,
		},
	}
	if !DetectedConnectorTransportSupported(detection, ConnectorTransportStdio) ||
		!DetectedConnectorTransportSupported(detection, ConnectorTransportStreamableHTTP) {
		t.Fatal("ready runtime lost declared MCP transports")
	}
	if DetectedConnectorTransportSupported(detection, ConnectorTransportSSE) {
		t.Fatal("runtime gained undeclared SSE transport")
	}
}

func TestResolveExecutableRejectsUnregisteredRuntimeWithStableError(t *testing.T) {
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sdk.ResolveExecutable(DetectRequest{RuntimeID: "unregistered-runtime"})
	var stable statuserror.CommonError
	if !errors.As(err, &stable) || stable.Code() != status_error.RuntimeNotRegistered.Code() {
		t.Fatalf("unregistered runtime error = %v, want RuntimeNotRegistered", err)
	}
}

func TestDetectUsesACPWithoutExecutingRuntimeProbeCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}
	marker := filepath.Join(t.TempDir(), "executed")
	executable := filepath.Join(t.TempDir(), "runtime-fixture")
	script := "#!/bin/sh\n: > \"" + marker + "\"\nexit 99\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatalf("write Runtime fixture: %v", err)
	}
	sdk, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{{
		ID:               "codex-detection-fixture",
		BaseRuntimeID:    RuntimeIDCodex,
		BinaryCandidates: []string{executable},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	installFakeCodexACPAgent(t, sdk, testCodexACPAgentWithCatalog())
	resolved, err := sdk.ResolveExecutable(DetectRequest{
		RuntimeID:     "codex-detection-fixture",
		HomeDirectory: t.TempDir(),
	})
	if err != nil || resolved != executable {
		t.Fatalf("ResolveExecutable() = %q, %v", resolved, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ResolveExecutable invoked the CLI: %v", err)
	}

	result := sdk.Detect(context.Background(), DetectRequest{
		RuntimeID:     "codex-detection-fixture",
		HomeDirectory: t.TempDir(),
	})
	if result.State != DetectionStateReady || result.Auth != AuthStatusUnknown {
		t.Fatalf("Detection = %+v", result)
	}
	if result.Version != "0.149.1" ||
		!result.Capabilities.Has(CapabilityResumeSession) ||
		!result.Capabilities.Has(CapabilityMCPStreamableHTTP) ||
		!result.Capabilities.Has(CapabilityDynamicModels) {
		t.Fatalf("ACP evidence = version %q, capabilities %+v", result.Version, result.Capabilities)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Runtime probe executable was invoked: %v", err)
	}
}

func TestDetectRefreshesVersionAfterCLIUpdate(t *testing.T) {
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{{
		ID:               "codex-refresh-fixture",
		BaseRuntimeID:    RuntimeIDCodex,
		BinaryCandidates: []string{executable},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	backend := sdk.acp.(*acpBackend)
	version := "a"
	created := 0
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		created++
		agent := testCodexACPAgentWithCatalog()
		agent.runtimeVersion = version
		return agent, nil
	})
	t.Cleanup(func() { _ = sdk.Close(context.Background()) })
	request := DetectRequest{RuntimeID: "codex-refresh-fixture", HomeDirectory: t.TempDir()}
	first := sdk.Detect(context.Background(), request)
	if first.Version != "a" || first.State != DetectionStateReady {
		t.Fatalf("first detection = %+v", first)
	}
	version = "b"
	second := sdk.Detect(context.Background(), request)
	if second.Version != "b" || second.State != DetectionStateReady || created != 2 {
		t.Fatalf("refresh detection = %+v, agents created = %d; want version b from a fresh agent", second, created)
	}
}

func TestOpenCodeDiscoversBothInstallationsAndHonorsExplicitPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executables")
	}
	home := t.TempDir()
	userBinary := filepath.Join(home, ".opencode", "bin", "opencode")
	oldBinary := filepath.Join(t.TempDir(), "opencode")
	for _, binary := range []string{userBinary, oldBinary} {
		if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	request := DetectRequest{
		RuntimeID:       RuntimeIDOpenCode,
		HomeDirectory:   home,
		BaseEnvironment: []string{"PATH=" + filepath.Dir(oldBinary)},
	}
	resolved, err := sdk.ResolveExecutable(request)
	if err != nil || resolved != oldBinary {
		t.Fatalf("default OpenCode binary = %q, %v; want %q", resolved, err, oldBinary)
	}
	installations := sdk.DiscoverInstallations(context.Background(), request, nil)
	paths := make(map[string]bool, len(installations))
	for _, installation := range installations {
		paths[installation.Path] = installation.Available
	}
	if !paths[oldBinary] || !paths[userBinary] {
		t.Fatalf("OpenCode installations = %+v; want both paths", installations)
	}
	request.LaunchOverride.Binary = oldBinary
	resolved, err = sdk.ResolveExecutable(request)
	if err != nil || resolved != oldBinary {
		t.Fatalf("explicit OpenCode binary = %q, %v; want %q", resolved, err, oldBinary)
	}
}

func TestACPInitializeCatalogDoesNotUseAgentImplementationVersion(t *testing.T) {
	catalog := acpInitializeCatalog(acpRuntimeBinding{}, acp.InitializeResponse{
		AgentInfo: &acp.Implementation{Version: "development"},
	})

	if catalog.Version != "" {
		t.Fatalf("缺少 Runtime CLI 元数据时版本为 %q，期望为空", catalog.Version)
	}
}

func TestDetectKeepsInstalledRuntimesReadyWhenAuthenticationIsMissing(t *testing.T) {
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, runtimeID := range []string{RuntimeIDCodex, RuntimeIDClaude, RuntimeIDKimi, RuntimeIDCursor, RuntimeIDPi} {
		t.Run(runtimeID, func(t *testing.T) {
			profileID := runtimeID + "-auth-fixture"
			sdk, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{{
				ID:               profileID,
				BaseRuntimeID:    runtimeID,
				BinaryCandidates: []string{executable},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			agent := testCodexACPAgentWithCatalog()
			agent.newSessionError = acp.NewAuthRequired(nil)
			backend := sdk.acp.(*acpBackend)
			replaceACPAgentFactory(backend, runtimeID, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
				return agent, nil
			})
			t.Cleanup(func() { _ = sdk.Close(context.Background()) })

			result := sdk.Detect(context.Background(), DetectRequest{
				RuntimeID:     profileID,
				HomeDirectory: t.TempDir(),
			})
			if result.State != DetectionStateReady || result.Auth != AuthStatusMissing || result.ResolvedBinary != executable || result.Version != "0.149.1" {
				t.Fatalf("Detection = %+v", result)
			}
		})
	}
}

// TestCommonExecutableDirectoriesCoversDesktopToolchains 验证桌面进程补齐跨平台通用安装目录。
func TestCommonExecutableDirectoriesCoversDesktopToolchains(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "tester")
	environment, err := buildEnvironment([]string{
		"MISE_DATA_DIR=" + filepath.Join(home, "mise-data"),
		"ASDF_DATA_DIR=" + filepath.Join(home, "asdf-data"),
		"PNPM_HOME=" + filepath.Join(home, "pnpm-home"),
		"GOBIN=" + filepath.Join(home, "go-bin"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		// name 是测试平台名称。
		name string
		// goos 是待投影的 Go 平台标识。
		goos string
		// expected 是该平台必须包含的通用目录。
		expected []string
	}{
		{
			name: "macOS",
			goos: "darwin",
			expected: []string{
				filepath.Join(home, "mise-data", "shims"),
				filepath.Join(home, "asdf-data", "shims"),
				filepath.Join(home, "pnpm-home"),
				filepath.Join(home, "go-bin"),
				filepath.Join(home, ".bun", "bin"),
				filepath.Join(home, ".cargo", "bin"),
				"/opt/homebrew/bin",
				"/usr/local/bin",
			},
		},
		{
			name: "Linux",
			goos: "linux",
			expected: []string{
				filepath.Join(home, ".local", "bin"),
				filepath.Join(home, ".volta", "bin"),
				"/home/linuxbrew/.linuxbrew/bin",
				"/snap/bin",
			},
		},
		{
			name: "Windows",
			goos: "windows",
			expected: []string{
				filepath.Join(home, "AppData", "Roaming", "npm"),
				filepath.Join(home, "scoop", "shims"),
				filepath.Join(home, ".bun", "bin"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directories := commonExecutableDirectories(test.goos, home, environment)
			for _, expected := range test.expected {
				if !slices.Contains(directories, expected) {
					t.Errorf("directories missing %q: %v", expected, directories)
				}
			}
			for _, directory := range directories {
				if directory == filepath.Join(home, ".kimi-code", "bin") {
					t.Fatal("common discovery must not contain a Kimi-specific directory")
				}
			}
		})
	}
}

// TestPreparedEnvironmentResolvesCLIFromCommonShim 验证通用 shim 目录真实参与可执行文件解析。
func TestPreparedEnvironmentResolvesCLIFromCommonShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows executable suffix is covered by platform-specific tests")
	}
	home := t.TempDir()
	shimDirectory := filepath.Join(home, ".local", "share", "mise", "shims")
	if err := os.MkdirAll(shimDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(shimDirectory, "sample-cli")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	environment, err := buildPreparedEnvironment([]string{"PATH="}, nil, home)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveExecutable([]string{"sample-cli"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != executable {
		t.Fatalf("resolved executable = %q, want %q", resolved, executable)
	}
}

// forkManagementAgent 记录原生分叉与管理连接的收尾，不提供模型回合。
type forkManagementAgent struct {
	// fakeCodexACPAgent 提供配置目录与资源回收的标准协议替身。
	*fakeCodexACPAgent
	// mode 是本次初始化确认的分叉精度。
	mode string
	// requests 保存原样穿过 ACP 边界的分叉请求。
	requests []acp.UnstableForkSessionRequest
	// forkError 模拟原生分叉已进入协议后返回的错误。
	forkError error
}

// Initialize 仅公开已确认的分叉精度。
func (agent *forkManagementAgent) Initialize(ctx context.Context, request acp.InitializeRequest) (acp.InitializeResponse, error) {
	result, err := agent.fakeCodexACPAgent.Initialize(ctx, request)
	result.Meta = acpmeta.ForkMetadata(agent.mode)
	result.AgentCapabilities.SessionCapabilities.Fork = acpmeta.ForkCapability(agent.mode)
	return result, err
}

// UnstableForkSession 保留来源、目标与对账元数据。
func (agent *forkManagementAgent) UnstableForkSession(_ context.Context, request acp.UnstableForkSessionRequest) (acp.UnstableForkSessionResponse, error) {
	agent.requests = append(agent.requests, request)
	return acp.UnstableForkSessionResponse{SessionId: "fork-child"}, agent.forkError
}

func TestForkSessionPreservesNativeCredentialConfigurationError(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		name := "fork"
		if reconcile {
			name = "reconcile"
		}
		t.Run(name, func(t *testing.T) {
			sdk, err := NewCatalog(CatalogConfig{})
			if err != nil {
				t.Fatal(err)
			}
			defer sdk.Close(context.Background())
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			if err = os.Mkdir(filepath.Join(home, "auth.json"), 0o700); err != nil {
				t.Fatal(err)
			}
			_, err = sdk.ForkSession(context.Background(), ForkRequest{
				SourceSessionID: "source", SourceWorkspace: t.TempDir(), ReconcileOnly: reconcile,
				Target: RuntimeTarget{RuntimeID: RuntimeIDCodex, ConfigHome: home}, Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary),
				Workspace: native.WorkspaceScope{Root: t.TempDir()}, PermissionLevel: PermissionLevelDefault,
			})
			if !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
				t.Fatalf("pre-dispatch credential failure mapped to %v, want RuntimeConfigurationInvalid", err)
			}
		})
	}
}

func TestCatalogClosePreservesNativeCredentialConfigurationError(t *testing.T) {
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	failure := &native.Fault{Code: native.FaultConfigurationInvalid, Stage: "credential", Message: "native credential configuration is invalid"}
	fail := true
	defer func() {
		fail = false
		_ = sdk.Close(context.Background())
	}()
	if err = sdk.acp.ReleaseResources(context.Background(), func(context.Context) error {
		if fail {
			return failure
		}
		return nil
	}); !errors.Is(err, failure) {
		t.Fatalf("failed cleanup was not retained: %v", err)
	}
	if err = sdk.Close(context.Background()); !errors.Is(err, status_error.RuntimeConfigurationInvalid) {
		t.Fatalf("catalog close leaked technical configuration fault: %v", err)
	}
}

// TestForkSessionOwnsIsolatedManagementConnection 验证分叉不依赖旧执行缓存且连接逐次释放。
func TestForkSessionOwnsIsolatedManagementConnection(t *testing.T) {
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, target, extra := t.TempDir(), t.TempDir(), t.TempDir()
	request := ForkRequest{ReceiptDirectory: t.TempDir(), SourceSessionID: "source", SourceWorkspace: source, Position: "opaque-turn",
		Target:    RuntimeTarget{Isolated: true, RuntimeID: RuntimeIDCodex, NativePluginDirectories: []string{"native-plugin"}, CodexPluginConfiguration: map[string]string{"plugins.fixture": "enabled"}},
		Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary), Workspace: native.WorkspaceScope{Root: target, Directories: []string{extra}}, ManagedWorkspace: true,
		PermissionLevel: PermissionLevelAuto, MCPServers: []acp.McpServer{{Http: &acp.McpServerHttpInline{Name: "ally_fixture", Url: "http://127.0.0.1:1/mcp", Headers: []acp.HttpHeader{{Name: "Authorization", Value: "fixture"}, {Name: "Z-Header", Value: "z"}}}}},
	}

	var agents []*forkManagementAgent
	var configs []acpAdapterConfig
	replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		agent := &forkManagementAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog(), mode: acpmeta.ForkTurn}
		agents = append(agents, agent)
		configs = append(configs, config)
		return agent, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, reconcile := range []bool{false, true} {
		request.ReconcileOnly = reconcile
		result, err := sdk.ForkSession(ctx, request)
		if err != nil || result.SessionID != "fork-child" || result.Mode != acpmeta.ForkTurn {
			t.Fatalf("fork = %+v, %v", result, err)
		}
		agent := agents[len(agents)-1]
		if !agent.closed || agent.promptCalls != 0 || agent.newCalls != 0 || agent.closeSessionCalls != 1 || len(agent.requests) != 1 {
			t.Fatalf("management lifecycle: %+v", agent.fakeCodexACPAgent)
		}
		got := agent.requests[0]
		if got.SessionId != "source" || got.Cwd != target || !reflect.DeepEqual(got.AdditionalDirectories, []string{extra}) || got.Meta["forkPosition"] != "opaque-turn" || got.Meta["forkReceiptDirectory"] != request.ReceiptDirectory || got.Meta["sourceCwd"] != source || got.Meta["reconcileOnly"] != reconcile {
			t.Fatalf("fork input: %+v", got)
		}
		servers, err := acpmeta.ForkMCPServers(got.McpServers)
		if err != nil || len(servers) != 1 || servers[0].Http == nil || servers[0].Http.Name != "ally_fixture" || len(servers[0].Http.Headers) != 2 || servers[0].Http.Headers[0].Name != "Authorization" {
			t.Fatalf("MCP scope/order: %+v, %v", servers, err)
		}
	}
	if len(agents) != 2 || configs[0].WorkingDirectory != source || !reflect.DeepEqual(configs[0].NativePluginDirectories, []string{"native-plugin"}) || configs[0].CodexPluginConfiguration["plugins.fixture"] != "enabled" {
		t.Fatalf("management launch facts: %+v", configs)
	}
	if err := sdk.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.ForkSession(ctx, request); !errors.Is(err, status_error.RuntimeUnavailable) || len(agents) != 2 {
		t.Fatalf("closed SDK accepted fork: %v agents=%d", err, len(agents))
	}
}

// TestForkManagementClosesOnUnsupportedOrIndeterminateResult 验证拒绝和不确定结果同样释放独占连接。
func TestForkManagementClosesOnUnsupportedOrIndeterminateResult(t *testing.T) {
	for _, test := range []struct {
		// name 是原生分叉失败边界。
		name string
		// mode 是适配器确认的精度。
		mode string
		// failure 是协议错误或取消原因。
		failure error
		// expected 是公开业务错误。
		expected error
		// calls 是应交付到适配器的分叉次数。
		calls int
	}{
		{name: "unsupported", mode: acpmeta.ForkUnsupported, expected: status_error.RuntimePolicyUnsupported, calls: 0},
		{name: "position requires turn", mode: acpmeta.ForkLatest, expected: status_error.RuntimePolicyUnsupported, calls: 0},
		{name: "cancelled after dispatch", mode: acpmeta.ForkTurn, failure: context.Canceled, expected: status_error.ChatForkIndeterminate, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			sdk, err := NewCatalog(CatalogConfig{})
			if err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			agent := &forkManagementAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog(), mode: test.mode, forkError: test.failure}
			replaceACPAgentFactory(sdk.acp.(*acpBackend), RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return agent, nil })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err = sdk.ForkSession(ctx, ForkRequest{SourceSessionID: "source", SourceWorkspace: t.TempDir(), Position: "opaque-turn", Target: RuntimeTarget{Isolated: true, RuntimeID: RuntimeIDCodex}, Detection: readyDetectionForRun(t, sdk, RuntimeIDCodex, binary), Workspace: native.WorkspaceScope{Root: t.TempDir()}, PermissionLevel: PermissionLevelDefault})
			if !errors.Is(err, test.expected) || !agent.closed || len(agent.requests) != test.calls || agent.promptCalls != 0 {
				t.Fatalf("error=%v closed=%t fork calls=%d prompts=%d", err, agent.closed, len(agent.requests), agent.promptCalls)
			}
		})
	}
}

func TestDiscoverInstallationsKeepsMultipleVersionsAndMissingHistory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	directories := []string{t.TempDir(), t.TempDir()}
	for index, directory := range directories {
		version := "1.0"
		if index == 1 {
			version = "2.0"
		}
		if err := os.WriteFile(filepath.Join(directory, "codex"), []byte("#!/bin/sh\nprintf '"+version+"\\n'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	request := DetectRequest{
		RuntimeID:       RuntimeIDCodex,
		HomeDirectory:   t.TempDir(),
		BaseEnvironment: []string{"PATH=" + directories[0] + string(filepath.ListSeparator) + directories[1]},
	}
	missing := filepath.Join(t.TempDir(), "codex")
	installations := sdk.DiscoverInstallations(context.Background(), request, []CLIInstallation{{
		Path: missing, Version: "0.9", Available: true,
	}})
	versions := make(map[string]CLIInstallation, len(installations))
	for _, installation := range installations {
		versions[installation.Path] = installation
	}
	for index, directory := range directories {
		path := filepath.Join(directory, "codex")
		want := "1.0"
		if index == 1 {
			want = "2.0"
		}
		if !versions[path].Available || versions[path].Version != want {
			t.Fatalf("installation %s = %+v, want %s", path, versions[path], want)
		}
	}
	if versions[missing].Available || versions[missing].Version != "0.9" {
		t.Fatalf("missing installation = %+v", versions[missing])
	}
}

// installationProbeFixture 复用同一测试二进制，屏障不再启动 shell、touch 或 sleep 子进程。
func installationProbeFixture(t *testing.T) (string, []string, []string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses executable aliases through POSIX symlinks")
	}
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "first"), filepath.Join(root, "second")}
	for _, path := range paths {
		if err := os.Symlink(executable, path); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"-test.run=^TestInstallationVersionProbeProcess$", "--"}
	environment := []string{"PATH=", "ALLY_INSTALLATION_PROBE_ROOT=" + root}
	return root, paths, args, environment
}

// TestInstallationVersionProbeProcess 在两个真实版本进程中各自登记启动，等待对端进入后才输出版本。
func TestInstallationVersionProbeProcess(t *testing.T) {
	root := os.Getenv("ALLY_INSTALLATION_PROBE_ROOT")
	if root == "" {
		return
	}
	index := 0
	switch filepath.Base(os.Args[0]) {
	case "first":
	case "second":
		index = 1
	default:
		os.Exit(2)
	}
	marker := filepath.Join(root, fmt.Sprintf("started-%d", index))
	if err := os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339Nano)), 0600); err != nil {
		os.Exit(2)
	}
	peer := filepath.Join(root, fmt.Sprintf("started-%d", 1-index))
	for {
		if _, err := os.Stat(peer); err == nil {
			break
		} else if !os.IsNotExist(err) {
			os.Exit(2)
		}
		// 进程内等待避免外部 sleep 的启动成本，又不使用消耗 CPU 的忙循环。
		time.Sleep(time.Millisecond)
	}
	fmt.Printf("1.%d.0\n", index)
	os.Exit(0)
}

// TestDiscoverInstallationsProbesPathsConcurrently 验证两个真实版本进程必须同时启动才能通过屏障。
func TestDiscoverInstallationsProbesPathsConcurrently(t *testing.T) {
	root, paths, args, environment := installationProbeFixture(t)
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	definition := sdk.definitions[RuntimeIDCodex]
	definition.binaryCandidates = paths
	definition.prefixArgs = args
	sdk.definitions[RuntimeIDCodex] = definition
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := sdk.DiscoverInstallations(ctx, DetectRequest{RuntimeID: RuntimeIDCodex, HomeDirectory: root, BaseEnvironment: environment}, []CLIInstallation{{Path: paths[0]}, {Path: paths[1]}})
	for index := range paths {
		marker, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("started-%d", index)))
		if err != nil {
			t.Fatalf("probe %d did not enter its barrier: %v", index, err)
		}
		t.Logf("probe %d entered barrier at %s", index, marker)
	}
	if len(got) != 2 {
		t.Fatalf("installations=%+v", got)
	}
	for index, installation := range got {
		if !installation.Available || installation.Version != fmt.Sprintf("1.%d.0", index) {
			t.Fatalf("version probes failed to run concurrently: %+v", got)
		}
	}
}

// TestInstallationVersionProbeBarrierRejectsSequentialStart 证明顺序执行不能获得两个有效版本，防止屏障退化为空验证。
func TestInstallationVersionProbeBarrierRejectsSequentialStart(t *testing.T) {
	root, paths, args, environment := installationProbeFixture(t)
	args = append(args, "--version")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first := nativeacp.RuntimeVersion(ctx, paths[0], args, environment, "")
	if first != "" || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("first probe crossed barrier without peer: version=%q context=%v", first, ctx.Err())
	}
	if _, err := os.Stat(filepath.Join(root, "started-0")); err != nil {
		t.Fatalf("first probe did not enter barrier: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "started-1")); !os.IsNotExist(err) {
		t.Fatalf("second probe started before first completed: %v", err)
	}
	secondContext, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if second := nativeacp.RuntimeVersion(secondContext, paths[1], args, environment, ""); second != "1.1.0" {
		t.Fatalf("second probe could not observe the completed first probe: %q", second)
	}
}

// TestDiscoverInstallationsKeepsVersionAfterUnknownOutput 验证未知输出不覆盖已知安装版本。
func TestDiscoverInstallationsKeepsVersionAfterUnknownOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	root := t.TempDir()
	path := filepath.Join(root, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'unknown\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	definition := sdk.definitions[RuntimeIDCodex]
	definition.binaryCandidates = []string{path}
	sdk.definitions[RuntimeIDCodex] = definition
	got := sdk.DiscoverInstallations(context.Background(), DetectRequest{RuntimeID: RuntimeIDCodex, HomeDirectory: root, BaseEnvironment: []string{"PATH="}}, []CLIInstallation{{Path: path, Version: "1.2.3"}})
	if len(got) != 1 || got[0].Version != "1.2.3" {
		t.Fatalf("known installation version overwritten: %+v", got)
	}
}

// TestDiscoverInstallationsStillFindsPATHAfterExplicitSelection 验证保存明确路径后仍能枚举 PATH 中的新版本。
func TestDiscoverInstallationsStillFindsPATHAfterExplicitSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX executable scripts")
	}
	selected := filepath.Join(t.TempDir(), "chosen-codex")
	otherDirectory := t.TempDir()
	other := filepath.Join(otherDirectory, "codex")
	for _, path := range []string{selected, other} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '1.2.3\\n'\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	sdk, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	request := DetectRequest{RuntimeID: RuntimeIDCodex, HomeDirectory: t.TempDir(), BaseEnvironment: []string{"PATH=" + otherDirectory}, LaunchOverride: LaunchOverride{Binary: selected}}
	installations := sdk.DiscoverInstallations(context.Background(), request, nil)
	found := map[string]bool{}
	for _, installation := range installations {
		found[installation.Path] = installation.Available
	}
	if !found[selected] || !found[other] {
		t.Fatalf("explicit selection hid PATH install: %+v", installations)
	}
}

// readyDetectionForRun 构造测试发送链路使用的已持久化可用快照。
func readyDetectionForRun(
	t *testing.T,
	sdk *Catalog,
	runtimeID string,
	resolvedBinary string,
) DetectionResult {
	t.Helper()
	definition, exists := sdk.definition(runtimeID)
	if !exists {
		t.Fatalf("runtime %q is not registered", runtimeID)
	}
	return DetectionResult{
		Runtime: definitionSummaries(map[string]runtimeDefinition{
			definition.id: definition,
		})[0],
		State:          DetectionStateReady,
		ResolvedBinary: resolvedBinary,
		Capabilities:   CapabilitySet{},
	}
}

func TestLaunchOverrideArgumentsAllowedRejectsAllyManagedArguments(t *testing.T) {
	tests := []struct {
		// name 是测试场景名称。
		name string
		// runtimeID 是参数所属的内置 Runtime。
		runtimeID string
		// arguments 是用户提交的前置参数。
		arguments []string
		// allowed 是期望的参数校验结论。
		allowed bool
	}{
		{name: "codex safe profile", runtimeID: RuntimeIDCodex, arguments: []string{"--profile", "ally"}, allowed: true},
		{name: "codex workspace", runtimeID: RuntimeIDCodex, arguments: []string{"--cd=/tmp/project"}, allowed: false},
		{name: "codex permission", runtimeID: RuntimeIDCodex, arguments: []string{"--sandbox", "read-only"}, allowed: false},
		{name: "claude safe setting", runtimeID: RuntimeIDClaude, arguments: []string{"--setting-sources", "user"}, allowed: true},
		{name: "claude protocol", runtimeID: RuntimeIDClaude, arguments: []string{"--output-format=stream-json"}, allowed: false},
		{name: "claude permission", runtimeID: RuntimeIDClaude, arguments: []string{"--permission-mode", "plan"}, allowed: false},
		{name: "unknown runtime", runtimeID: "unknown", arguments: nil, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := LaunchOverrideArgumentsAllowed(test.runtimeID, test.arguments); actual != test.allowed {
				t.Fatalf("LaunchOverrideArgumentsAllowed() = %v, want %v", actual, test.allowed)
			}
		})
	}
}

func TestLaunchDetectInputsEqualIgnoresNonLaunchFields(t *testing.T) {
	previous := LaunchDetectInput{
		Binary:     "codex",
		PrefixArgs: []string{"--profile", "ally"},
		ConfigHome: "/tmp/codex-home",
	}
	next := previous
	if !LaunchDetectInputsEqual(previous, next) {
		t.Fatal("identical launch inputs should compare equal")
	}
	next.PrefixArgs = []string{"--profile", "other"}
	if LaunchDetectInputsEqual(previous, next) {
		t.Fatal("prefix args change should require detect refresh")
	}
}

// TestPrepareLaunchPreservesProfileAndAccountFacts 防止新宿主跳过 Profile、账号覆盖和真实探测程序。
func TestPrepareLaunchPreservesProfileAndAccountFacts(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "inherited-secret")
	sdk, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{{ID: "qoder-cn-profile", BaseRuntimeID: RuntimeIDQoderCN, BinaryCandidates: []string{"profile-command"}, PrefixArgs: []string{"profile-prefix"}, Environment: map[string]string{"PROFILE_FACT": "retained"}}}})
	if err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	detection := readyDetectionForRun(t, sdk, "qoder-cn-profile", binary)
	home := filepath.Join(t.TempDir(), "home")
	facts, err := sdk.PrepareLaunch(RuntimeTarget{RuntimeID: "qoder-cn-profile", ConfigHome: home, Isolated: true, Environment: map[string]string{"ACCOUNT_FACT": "selected"}}, detection)
	if err != nil {
		t.Fatal(err)
	}
	if facts.BaseRuntimeID != RuntimeIDQoderCN || facts.Executable != binary || !slices.Equal(facts.PrefixArgs, []string{"profile-prefix"}) || !slices.Contains(facts.Environment, "PROFILE_FACT=retained") || !slices.Contains(facts.Environment, "ACCOUNT_FACT=selected") {
		t.Fatalf("launch facts lost: %+v", facts)
	}
	for _, value := range facts.Environment {
		if value == "OPENAI_API_KEY=inherited-secret" {
			t.Fatal("inherited credential retained")
		}
	}
	if !slices.Contains(facts.Environment, "QODERCN_CONFIG_DIR="+home) {
		t.Fatalf("CN config root missing: %+v", facts.Environment)
	}
}

// TestPrepareLaunchAllCLIProfilesMapConfigHomes 验证所有现有 CLI 与 Profile 保留各自配置根和启动覆盖。
func TestPrepareLaunchAllCLIProfilesMapConfigHomes(t *testing.T) {
	for _, tt := range []struct {
		base     string // base 是内置协议身份。
		variable string // variable 是该 CLI 原有配置根变量，空值表示无目录映射。
	}{{"codex", "CODEX_HOME"}, {"claude", "CLAUDE_CONFIG_DIR"}, {"kimi", ""}, {"cursor", ""}, {"pi", "PI_CODING_AGENT_DIR"}, {"opencode", "OPENCODE_CONFIG_DIR"}, {"gemini", "GEMINI_CLI_HOME"}, {"grok", "GROK_HOME"}, {"workbuddy", "CODEBUDDY_CONFIG_DIR"}, {"qoder", "QODER_CONFIG_DIR"}, {"qoder-cn", "QODERCN_CONFIG_DIR"}} {
		t.Run(tt.base, func(t *testing.T) {
			id := "profile-" + tt.base
			sdk, err := NewCatalog(CatalogConfig{Profiles: []ProfileSpec{{ID: id, BaseRuntimeID: tt.base, PrefixArgs: []string{"profile-prefix"}, Environment: map[string]string{"PROFILE_FACT": "retained"}}}})
			if err != nil {
				t.Fatal(err)
			}
			binary, _ := os.Executable()
			home := t.TempDir()
			facts, err := sdk.PrepareLaunch(RuntimeTarget{RuntimeID: id, PrefixArgs: []string{"selected-prefix"}, ConfigHome: home, Environment: map[string]string{"ACCOUNT_FACT": "selected"}}, readyDetectionForRun(t, sdk, id, binary))
			if err != nil {
				t.Fatal(err)
			}
			if facts.BaseRuntimeID != tt.base || facts.Executable != binary || !slices.Equal(facts.PrefixArgs, []string{"selected-prefix"}) || !slices.Contains(facts.Environment, "PROFILE_FACT=retained") || !slices.Contains(facts.Environment, "ACCOUNT_FACT=selected") {
				t.Fatalf("launch=%+v", facts)
			}
			if tt.variable != "" && !slices.Contains(facts.Environment, tt.variable+"="+home) {
				t.Fatalf("config home lost: %+v", facts.Environment)
			}
		})
	}
}

// TestCloseWaitsForAcceptedManagementPreparation 防止尚在工厂中的管理操作丢失资源所有权。
func TestCloseWaitsForAcceptedManagementPreparation(t *testing.T) {
	backend := newACPBackend()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	operation, cancelOperation := context.WithTimeout(context.Background(), time.Second)
	defer cancelOperation()
	directory := t.TempDir()
	replaceACPAgentFactory(backend, RuntimeIDPi, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		close(started)
		select {
		case <-release:
		case <-operation.Done():
			return nil, operation.Err()
		}
		return testCodexACPAgentWithCatalog(), nil
	})
	go func() {
		_, err := backend.DetectOptions(operation, acpOptionsRequest{RuntimeID: RuntimeIDPi, WorkingDirectory: directory, Prepared: preparedRuntime{definition: runtimeDefinition{baseRuntimeID: RuntimeIDPi}}})
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("management ended before factory: %v", err)
	case <-operation.Done():
		t.Fatal("management did not reach factory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := backend.Close(ctx)
	close(release)
	select {
	case <-done:
	case <-operation.Done():
		t.Fatal("management did not return after factory release")
	}
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close must retain an accepted preparation until its owner returns: %v", err)
	}
	if err := backend.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// closeRetryAgent 模拟原生使用者首次关闭未确认、第二次才交还依赖。
type closeRetryAgent struct {
	*fakeCodexACPAgent     // fakeCodexACPAgent 提供无模型输入的标准管理协议。
	closeCalls         int // closeCalls 记录原生关闭确认次数。
}

func (agent *closeRetryAgent) Close(context.Context) error {
	agent.closeCalls++
	if agent.closeCalls == 1 {
		return context.DeadlineExceeded
	}
	return agent.fakeCodexACPAgent.Close(context.Background())
}

// TestDetectionKeepsResourcesUntilCloseRetry 证明关闭失败不会释放账号依赖或探测目录。
func TestDetectionKeepsResourcesUntilCloseRetry(t *testing.T) {
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	agent := &closeRetryAgent{fakeCodexACPAgent: testCodexACPAgentWithCatalog()}
	directory := ""
	released := 0
	replaceACPAgentFactory(registry.acp.(*acpBackend), RuntimeIDPi, func(_ context.Context, config acpAdapterConfig) (acp.Agent, error) {
		directory = config.WorkingDirectory
		return agent, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := registry.Detect(ctx, DetectRequest{RuntimeID: RuntimeIDPi, BaseEnvironment: os.Environ(), LaunchOverride: LaunchOverride{Binary: os.Args[0]}, ReleaseResources: func(context.Context) error { released++; return nil }})
	if !result.Capabilities.Has(CapabilityPartialMessages) || released != 0 || agent.closeCalls != 1 {
		t.Fatalf("unconfirmed close released dependencies: result=%+v released=%d close=%d", result, released, agent.closeCalls)
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("probe directory disappeared before close confirmation: %v", err)
	}
	if err := registry.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if released != 1 || agent.closeCalls != 2 {
		t.Fatalf("close retry did not release exactly once: release=%d close=%d", released, agent.closeCalls)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("confirmed close retained probe directory: %v", err)
	}
	if err := registry.Close(ctx); err != nil || released != 1 {
		t.Fatalf("repeated close changed released resources: err=%v release=%d", err, released)
	}
}

// TestDetectionReleasesBeforeManagementFailure 证明未交接管理连接的拒绝路径也释放调用者资源。
func TestDetectionReleasesBeforeManagementFailure(t *testing.T) {
	registry, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	released := 0
	registry.Detect(context.Background(), DetectRequest{RuntimeID: "unregistered", ReleaseResources: func(context.Context) error { released++; return nil }})
	if released != 1 {
		t.Fatalf("rejected discovery retained caller resources: %d", released)
	}
}

// TestDetectRetainsFailedEarlyRelease 验证原生启动前失败也保留未能释放的账号租约。
func TestDetectRetainsFailedEarlyRelease(t *testing.T) {
	catalog, err := NewCatalog(CatalogConfig{})
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	result := catalog.Detect(context.Background(), DetectRequest{RuntimeID: "missing", ReleaseResources: func(context.Context) error {
		attempts++
		if attempts == 1 {
			return status_error.RuntimeShutdownFailed
		}
		return nil
	}})
	if result.State != DetectionStateUnavailable || attempts != 1 {
		t.Fatalf("state=%s release attempts=%d", result.State, attempts)
	}
	if err := catalog.Close(context.Background()); err != nil || attempts != 2 {
		t.Fatalf("retry error=%v release attempts=%d", err, attempts)
	}
}

func TestPrepareForkMCPServersRejectsDuplicateRuntimeKey(t *testing.T) {
	_, err := PrepareForkMCPServers([]Connector{
		{ID: "first", Name: "第一个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: ConnectorTransportStdio, Command: "first"},
		{ID: "second", Name: "第二个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: ConnectorTransportStdio, Command: "second"},
	})
	if err == nil || !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("duplicate runtime key error = %v", err)
	}
}
