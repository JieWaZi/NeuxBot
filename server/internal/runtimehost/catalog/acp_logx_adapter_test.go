package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
	ginxlogx "github.com/shrewx/ginx/pkg/logx"
	"github.com/sirupsen/logrus"
)

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
	acpAdapterLogger(context.Background())
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
	ctx := acpAgentLogContext(context.Background(), acpAdapterConfig{RuntimeID: "profile", DetectionID: "probe-one"}, geminiACPRuntimeBinding())
	logger := acpAdapterLogger(ctx)
	logger.Warn("native ACP stderr", "stderr", "{\"level\":\"in")
	logger.Warn("native ACP stderr", "stderr", "fo\",\"msg\":\"ready\"}\n{\"level\":\"error\",\"msg\":\"failed\"}\n[DEBUG] details\n[STARTUP] Phase 'cli_startup' was started but never ended. Skipping metrics.\n")
	logger.Warn("native ACP stderr", "stderr", `{"level":"info","error":"no provider configured"}`+"\nunknown diagnostic")
	flushACPStderr(ctx)
	flushACPStderr(ctx)
	entries := capture.entries(t)
	want := []string{"info", "error", "warning", "warning"}
	if len(entries) != len(want) {
		t.Fatalf("fragmented lines, debug filtering or tail flush incorrect: %+v", entries)
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
	profile := runtimecontract.ProfileSpec{ID: "logging-fixture", BaseRuntimeID: runtimecontract.RuntimeIDWorkBuddy, BinaryCandidates: []string{binary}, PrefixArgs: []string{"-test.run=^TestAllyNativeACPProcess$", "--"}, Environment: map[string]string{"ALLY_NATIVE_ACP_TEST": runtimecontract.RuntimeIDWorkBuddy, "ALLY_NATIVE_ACP_STDERR": "[INFO] native fixture ready\n[ERROR] native fixture diagnostic\n"}}
	sdk, err := New(Config{Profiles: []runtimecontract.ProfileSpec{profile}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer sdk.Close(ctx)
	for range 2 {
		if result := sdk.Detect(ctx, runtimecontract.DetectRequest{RuntimeID: profile.ID, BaseEnvironment: os.Environ()}); result.State != runtimecontract.DetectionStateReady {
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
	replaceACPAgentFactory(backend, runtimecontract.RuntimeIDCodex, func(context.Context, acpAdapterConfig) (acp.Agent, error) {
		return nil, status_error.RuntimeUnavailable
	})
	_, err := backend.DetectOptions(context.Background(), acpOptionsRequest{RuntimeID: "failed-profile", Prepared: preparedRuntime{definition: runtimeDefinition{baseRuntimeID: runtimecontract.RuntimeIDCodex}}})
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
	replaceACPAgentFactory(backend, runtimecontract.RuntimeIDCodex, func(ctx context.Context, _ acpAdapterConfig) (acp.Agent, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := backend.DetectOptions(ctx, acpOptionsRequest{RuntimeID: "deadline-profile", Prepared: preparedRuntime{definition: runtimeDefinition{baseRuntimeID: runtimecontract.RuntimeIDCodex}}})
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
	ctx := acpAgentLogContext(context.Background(), acpAdapterConfig{RuntimeID: "codex", Environment: []string{"ALLY_PLUGIN_AUTH_ABC=Bearer " + token}}, codexACPRuntimeBinding())
	logger := acpAdapterLogger(ctx)
	logger.Warn("Codex app-server stderr", "stderr", "Authorization: Bearer "+token)
	logger.With("env", "ALLY_PLUGIN_AUTH_ABC=Bearer "+token).WithGroup("launch").Error("failure "+token, "error", errors.New("rejected "+token))
	for _, entry := range capture.entries(t) {
		data, _ := json.Marshal(entry)
		if bytes.Contains(data, []byte(token)) {
			t.Fatal("plugin authorization appeared in diagnostic log")
		}
	}
	snapshot := environmentSnapshot{values: map[string]string{"ALLY_PLUGIN_AUTH_ABC": "Bearer " + token}}
	digest := fingerprintEnvironment(snapshot)
	if strings.Contains(digest, token) || len(digest) != 64 {
		t.Fatal("environment fingerprint exposed value")
	}
}
