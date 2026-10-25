package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// realACPRuntimeCase 保存一个显式启用的真实 Runtime smoke 配置。
type realACPRuntimeCase struct {
	// name 是子测试展示名称。
	name string
	// runtimeID 是目标内置 Runtime 标识。
	runtimeID string
	// pathEnvironment 是可执行文件覆盖环境变量。
	pathEnvironment string
	// enabledEnvironment 是真实 smoke 显式开关。
	enabledEnvironment string
}

// cancelOnRuntimeActivitySink 在真实 Runtime 已开始产生活动后取消调用。
type cancelOnRuntimeActivitySink struct {
	// recordingSink 保存取消前已交付的事件。
	recordingSink
	// cancel 终止真实 Runtime.Run Context。
	cancel context.CancelFunc
	// cancelOnce 保证只取消一次。
	cancelOnce sync.Once
}

// Accept 先记录事件，再在首个工具或模型输出事件到达时触发取消。
func (sink *cancelOnRuntimeActivitySink) Accept(ctx context.Context, event Event) error {
	if err := sink.recordingSink.Accept(ctx, event); err != nil {
		return err
	}
	if event.Type == EventTypeToolUse || event.Type == EventTypeTextDelta ||
		event.Type == EventTypeCommentaryDelta || event.Type == EventTypeThinkingDelta {
		sink.cancelOnce.Do(sink.cancel)
	}
	return nil
}

// TestRealCodexACPBackendSmoke 显式验证内嵌 acp-go、真实 Codex 与 Runtime Event 链路。
func TestRealCodexACPBackendSmoke(t *testing.T) {
	if os.Getenv("ALLY_REAL_CODEX_ACP") != "1" {
		t.Skip("set ALLY_REAL_CODEX_ACP=1 to run a billable Codex ACP smoke test")
	}
	runRealACPExactSmoke(t, RuntimeIDCodex, "CODEX_PATH", "ACP_REAL_CODEX_OK")
}

// TestRealClaudeACPBackendSmoke 显式验证固定 upstream 对应的真实 Claude ACP 链路。
func TestRealClaudeACPBackendSmoke(t *testing.T) {
	if os.Getenv("ALLY_REAL_CLAUDE_ACP") != "1" {
		t.Skip("set ALLY_REAL_CLAUDE_ACP=1 to run a billable Claude ACP smoke test")
	}
	runRealACPExactSmoke(t, RuntimeIDClaude, "CLAUDE_PATH", "ACP_REAL_CLAUDE_OK")
}

// TestRealACPCapabilitySmoke 显式验证 resume、cancel、File Change、Skill 与 STDIO MCP。
func TestRealACPCapabilitySmoke(t *testing.T) {
	tests := enabledRealACPRuntimes()
	if len(tests) == 0 {
		t.Skip("set ALLY_REAL_CODEX_ACP=1 and/or ALLY_REAL_CLAUDE_ACP=1 to run billable capability smoke tests")
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			runRealACPCapabilitySmoke(t, test)
		})
	}
}

// runRealACPExactSmoke 要求真实 Runtime 只返回精确标记且不调用工具。
func runRealACPExactSmoke(
	t *testing.T,
	runtimeID string,
	pathEnvironment string,
	marker string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, cancelClose := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancelClose()
		if closeErr := sdk.Close(closeContext); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	sink := &recordingSink{}
	homeDirectory, _ := os.UserHomeDir()
	detection := sdk.Detect(ctx, DetectRequest{
		RuntimeID:       runtimeID,
		BaseEnvironment: os.Environ(),
		HomeDirectory:   homeDirectory,
		LaunchOverride: LaunchOverride{
			Binary: os.Getenv(pathEnvironment),
		},
	})
	if detection.State != DetectionStateReady {
		t.Fatalf("Detection = %+v", detection)
	}
	result := sdk.Run(ctx, RunRequest{
		Target: RuntimeTarget{
			RuntimeID: runtimeID,
			Binary:    os.Getenv(pathEnvironment),
		},
		Detection: detection,
		Workspace: Workspace{
			Path:    t.TempDir(),
			Managed: true,
		},
		Message: Message{
			Text: "Reply with exactly " + marker + " and nothing else. Do not use tools.",
		},
		Session: SessionDirective{Mode: SessionModeNew},
		Policy: RunPolicy{Execution: ExecutionPolicy{
			PermissionLevel: PermissionLevelAuto,
		}},
	}, sink)
	if result.Failure != nil {
		t.Fatalf("Run() failure = %+v", result.Failure)
	}
	if !result.ProtocolHealthy || result.Session.OutputID == "" ||
		result.Session.State != SessionStateCreated {
		t.Fatalf("Result = %#v", result)
	}
	if output := strings.TrimSpace(sink.textOutput()); output != marker {
		t.Fatalf("text output = %q, want %q", output, marker)
	}
	if containsEventType(sink.eventTypes(), EventTypeToolUse) {
		t.Fatalf("event types = %#v, want no tools", sink.eventTypes())
	}
	backend, ok := sdk.acp.(*acpBackend)
	if !ok {
		t.Fatalf("%s ACP backend is not configured", runtimeID)
	}
	backend.mutex.Lock()
	agentCount := len(backend.entries)
	backend.mutex.Unlock()
	if agentCount != 1 {
		t.Fatalf("ACP Agent count = %d, want 1", agentCount)
	}
}

// runRealACPCapabilitySmoke 运行一个 Runtime 的完整 opt-in 发布门。
func runRealACPCapabilitySmoke(t *testing.T, test realACPRuntimeCase) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	sdk, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelClose()
		if closeErr := sdk.Close(closeContext); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	binary := os.Getenv(test.pathEnvironment)
	workspace := t.TempDir()
	homeDirectory, _ := os.UserHomeDir()
	detection := sdk.Detect(ctx, DetectRequest{
		RuntimeID:       test.runtimeID,
		BaseEnvironment: os.Environ(),
		HomeDirectory:   homeDirectory,
		LaunchOverride:  LaunchOverride{Binary: binary},
	})
	if detection.State != DetectionStateReady {
		t.Fatalf("Detection = %+v", detection)
	}

	resumeToken := "ACP_" + strings.ToUpper(test.runtimeID) + "_RESUME_OK"
	firstSink := &recordingSink{}
	first := sdk.Run(ctx, realACPRunRequest(
		test.runtimeID,
		binary,
		detection,
		workspace,
		"Remember the exact token "+resumeToken+" for the next turn. Reply with exactly ACP_RESUME_SEEDED and nothing else. Do not use tools.",
	), firstSink)
	if first.Failure != nil || first.Session.OutputID == "" ||
		strings.TrimSpace(firstSink.textOutput()) != "ACP_RESUME_SEEDED" {
		t.Fatalf("resume seed result = %#v, output = %q", first, firstSink.textOutput())
	}
	resumeRequest := realACPRunRequest(
		test.runtimeID,
		binary,
		detection,
		workspace,
		"Reply with exactly the token I asked you to remember in the previous turn and nothing else. Do not use tools.",
	)
	resumeRequest.Session = SessionDirective{Mode: SessionModeResume, ID: first.Session.OutputID}
	resumeSink := &recordingSink{}
	resumed := sdk.Run(ctx, resumeRequest, resumeSink)
	if resumed.Failure != nil || resumed.Session.State != SessionStateResumed ||
		resumed.Session.OutputID != first.Session.OutputID ||
		strings.TrimSpace(resumeSink.textOutput()) != resumeToken {
		t.Fatalf("resume result = %#v, output = %q", resumed, resumeSink.textOutput())
	}

	cancelContext, cancelPrompt := context.WithCancel(ctx)
	cancelSink := &cancelOnRuntimeActivitySink{cancel: cancelPrompt}
	cancelled := sdk.Run(cancelContext, realACPRunRequest(
		test.runtimeID,
		binary,
		detection,
		t.TempDir(),
		"Use the shell tool to run sleep 120. Do not reply before the command completes.",
	), cancelSink)
	if cancelled.Failure == nil || cancelled.Failure.Code != FailureCodeContextCanceled ||
		cancelled.Session.OutputID == "" {
		t.Fatalf("cancel result = %#v", cancelled)
	}

	capabilityWorkspace := t.TempDir()
	skillDirectory := createRealACPSmokeSkill(t, test.runtimeID)
	mcpServerPath := createRealACPSmokeMCPServer(t)
	capabilityMarker := "ACP_" + strings.ToUpper(test.runtimeID) + "_CAPABILITY_OK"
	capabilityRequest := realACPRunRequest(
		test.runtimeID,
		binary,
		detection,
		capabilityWorkspace,
		"Follow the selected skill exactly and finish with its required marker.",
	)
	capabilityRequest.Skills = []Skill{{
		Name: "ally-acp-smoke", Path: skillDirectory, Selected: true,
	}}
	capabilityRequest.Connectors = []Connector{{
		ID: "ally-smoke", Name: "NeuxBot Smoke MCP", RuntimeKey: "ally_smoke",
		Selected: true, Transport: ConnectorTransportStdio,
		Command: "go", Arguments: []string{"run", mcpServerPath},
	}}
	capabilitySink := &recordingSink{}
	capability := sdk.Run(ctx, capabilityRequest, capabilitySink)
	if capability.Failure != nil || strings.TrimSpace(capabilitySink.textOutput()) != capabilityMarker {
		t.Fatalf("capability result = %#v, output = %q", capability, capabilitySink.textOutput())
	}
	fileContent, err := os.ReadFile(filepath.Join(capabilityWorkspace, "acp-smoke-output.txt"))
	if err != nil || strings.TrimSpace(string(fileContent)) != "ACP_FILE_SMOKE_OK" {
		t.Fatalf("file smoke content = %q, err = %v", fileContent, err)
	}
	toolNames := realACPToolNames(t, capabilitySink)
	for _, expected := range []string{
		"mcp__ally_smoke__ally_smoke_echo",
		"file_modify",
	} {
		if !toolNames[expected] {
			t.Fatalf("tool names = %#v, missing %q", toolNames, expected)
		}
	}
	// 显式 Skill 通过 Runtime 原生 Slash Command 执行；固定 Claude upstream
	// 会展开命令正文，但不会为命令本身合成 Skill ToolCall。上面的文件、MCP
	// 与最终标记共同证明 Skill 已执行；模型主动调用 Skill 工具另由确定性测试覆盖。

	nextSink := &recordingSink{}
	next := sdk.Run(ctx, realACPRunRequest(
		test.runtimeID,
		binary,
		detection,
		t.TempDir(),
		"Reply with exactly ACP_AFTER_CANCEL_OK and nothing else. Do not use tools.",
	), nextSink)
	if next.Failure != nil || strings.TrimSpace(nextSink.textOutput()) != "ACP_AFTER_CANCEL_OK" {
		t.Fatalf("post-cancel result = %#v, output = %q", next, nextSink.textOutput())
	}
}

// enabledRealACPRuntimes 返回调用方显式启用的真实 Runtime。
func enabledRealACPRuntimes() []realACPRuntimeCase {
	all := []realACPRuntimeCase{
		{name: "codex", runtimeID: RuntimeIDCodex, pathEnvironment: "CODEX_PATH", enabledEnvironment: "ALLY_REAL_CODEX_ACP"},
		{name: "claude", runtimeID: RuntimeIDClaude, pathEnvironment: "CLAUDE_PATH", enabledEnvironment: "ALLY_REAL_CLAUDE_ACP"},
	}
	result := make([]realACPRuntimeCase, 0, len(all))
	for _, test := range all {
		if os.Getenv(test.enabledEnvironment) == "1" {
			result = append(result, test)
		}
	}
	return result
}

// realACPRunRequest 创建真实 Runtime smoke 的最小公开请求。
func realACPRunRequest(
	runtimeID string,
	binary string,
	detection DetectionResult,
	workspace string,
	text string,
) RunRequest {
	return RunRequest{
		Target:    RuntimeTarget{RuntimeID: runtimeID, Binary: binary},
		Detection: detection,
		Workspace: Workspace{Path: workspace, Managed: true},
		Message:   Message{Text: text},
		Session:   SessionDirective{Mode: SessionModeNew},
		Policy: RunPolicy{Execution: ExecutionPolicy{
			PermissionLevel: PermissionLevelAuto,
		}},
	}
}

// createRealACPSmokeSkill 创建同时要求 Skill、MCP 与文件变更的临时技能。
func createRealACPSmokeSkill(t *testing.T, runtimeID string) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "ally-acp-smoke")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := "ACP_" + strings.ToUpper(runtimeID) + "_CAPABILITY_OK"
	content := fmt.Sprintf(`---
name: ally-acp-smoke
description: NeuxBot ACP release smoke capability.
---

When this skill is explicitly invoked, do all of the following:

1. Call the MCP tool named ally_smoke_echo from server ally_smoke exactly once. Its argument value must be ACP_MCP_SMOKE_OK.
2. Use the native file edit or write tool to create acp-smoke-output.txt in the current working directory with exact content ACP_FILE_SMOKE_OK.
3. After both operations succeed, reply with exactly %s and nothing else.
`, marker)
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

// createRealACPSmokeMCPServer 创建离线、确定性的最小 STDIO MCP Server 源码。
func createRealACPSmokeMCPServer(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
	content := `package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		id, hasID := request["id"]
		if !hasID {
			continue
		}
		method, _ := request["method"].(string)
		var result any
		switch method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "ally-smoke", "version": "1.0.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "ally_smoke_echo",
				"description": "Return a deterministic ACP smoke marker.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{"value": map[string]any{"type": "string"}},
					"required": []string{"value"},
				},
			}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]any{
				"type": "text", "text": "ACP_MCP_SMOKE_OK",
			}}}
		default:
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"error": map[string]any{"code": -32601, "message": "method not found"},
			})
			continue
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// realACPToolNames 返回真实 smoke 中全部规范化工具名。
func realACPToolNames(t *testing.T, sink *recordingSink) map[string]bool {
	t.Helper()
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	names := make(map[string]bool)
	for _, event := range sink.events {
		if event.Type != EventTypeToolUse && event.Type != EventTypeToolResult {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if name, _ := payload["name"].(string); name != "" {
			names[name] = true
		}
	}
	return names
}
