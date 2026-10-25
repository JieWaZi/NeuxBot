package runtimes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	acp "github.com/coder/acp-go-sdk"
)

// countingACPRunner 记录发送链路是否错误触发 ACP 配置探测。
type countingACPRunner struct {
	// detectCalls 记录 DetectOptions 被调用的次数。
	detectCalls int
	// runCalls 记录 Run 被调用的次数。
	runCalls int
	// lastRequest 保存实际交给 ACP 的 cwd 与输入，检验指令通过原生文件加载。
	lastRequest acpRunRequest
}

// readyDetectionForRun 构造测试发送链路使用的已持久化可用快照。
func readyDetectionForRun(
	t *testing.T,
	sdk *SDK,
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

// DetectOptions 返回可用目录并记录调用。
func (runner *countingACPRunner) DetectOptions(
	context.Context,
	acpOptionsRequest,
) (acpOptionCatalog, error) {
	runner.detectCalls++
	return acpOptionCatalog{
		AgentConnected: true,
		SessionReady:   true,
		Capabilities:   CapabilitySet{},
	}, nil
}

// Run 记录调用并返回成功的最小 Runtime 结果。
func (runner *countingACPRunner) Run(_ context.Context, request acpRunRequest) Result {
	runner.runCalls++
	runner.lastRequest = request
	return Result{ProtocolHealthy: true}
}

// Close 满足 ACP Runner 生命周期接口。
func (*countingACPRunner) Close(context.Context) error {
	return nil
}

func TestRunUsesPersistedDetectionWithoutDetectingAgain(t *testing.T) {
	t.Parallel()
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	sdk, err := New(Config{Profiles: []ProfileSpec{{
		ID:               "codex-persisted-detection-fixture",
		BaseRuntimeID:    RuntimeIDCodex,
		BinaryCandidates: []string{testBinary},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runner := &countingACPRunner{}
	sdk.acp = runner
	result := sdk.Run(context.Background(), RunRequest{
		Target: RuntimeTarget{RuntimeID: "codex-persisted-detection-fixture"},
		Detection: DetectionResult{
			Runtime: RuntimeSummary{
				ID:                  "codex-persisted-detection-fixture",
				BaseRuntimeID:       RuntimeIDCodex,
				InstructionFileName: "AGENTS.md",
				SkillDirectoryName:  ".agents/skills",
			},
			State:          DetectionStateReady,
			ResolvedBinary: testBinary,
			Capabilities:   CapabilitySet{},
		},
		Workspace: Workspace{Path: t.TempDir(), Managed: true},
		Message:   Message{Text: "hello"},
		Session:   SessionDirective{Mode: SessionModeNew},
		Policy:    RunPolicy{Execution: defaultExecutionPolicy()},
	}, &recordingSink{})
	if result.Failure != nil {
		t.Fatalf("Run() failure = %+v", result.Failure)
	}
	if runner.detectCalls != 0 || runner.runCalls != 1 {
		t.Fatalf("detectCalls=%d runCalls=%d, want 0 and 1", runner.detectCalls, runner.runCalls)
	}
}

func TestRunRejectsMissingPersistedDetectionWithoutDetectingAgain(t *testing.T) {
	t.Parallel()
	sdk, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runner := &countingACPRunner{}
	sdk.acp = runner
	result := sdk.Run(context.Background(), RunRequest{
		Target:    RuntimeTarget{RuntimeID: RuntimeIDCodex},
		Workspace: Workspace{Path: t.TempDir(), Managed: true},
		Message:   Message{Text: "hello"},
		Session:   SessionDirective{Mode: SessionModeNew},
		Policy:    RunPolicy{Execution: defaultExecutionPolicy()},
	}, &recordingSink{})
	if result.Failure == nil || result.Failure.Code != FailureCodeRuntimeUnavailable {
		t.Fatalf("Run() failure = %+v, want runtime unavailable", result.Failure)
	}
	if runner.detectCalls != 0 || runner.runCalls != 0 {
		t.Fatalf("detectCalls=%d runCalls=%d, want 0 and 0", runner.detectCalls, runner.runCalls)
	}
}

func TestRunUsesManagedChatInstructionsAndAuthorizedProjectLinks(t *testing.T) {
	t.Parallel()
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	sdk, err := New(Config{Profiles: []ProfileSpec{{
		ID:               "codex-shared-context-fixture",
		BaseRuntimeID:    RuntimeIDCodex,
		BinaryCandidates: []string{testBinary},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sharedDirectory, workspaceDirectory := t.TempDir(), t.TempDir()
	projects := filepath.Join(workspaceDirectory, "projects")
	if err := os.Mkdir(projects, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedDirectory, filepath.Join(projects, "approved")); err != nil {
		t.Fatal(err)
	}
	runner := &countingACPRunner{}
	sdk.acp = runner
	result := sdk.Run(context.Background(), RunRequest{
		Target:    RuntimeTarget{RuntimeID: "codex-shared-context-fixture"},
		Detection: readyDetectionForRun(t, sdk, "codex-shared-context-fixture", testBinary),
		Workspace: Workspace{
			Path:                          workspaceDirectory,
			Managed:                       true,
			AdditionalWritableDirectories: []string{sharedDirectory},
			ProjectDirectories: []WorkspaceProjectDirectory{{
				Path:    "projects/approved",
				Default: true,
			}},
		},
		Instructions: []Instruction{{Name: "IDENTITY.md", Content: "独立 Worker 私有集成上下文"}},
		Message:      Message{Text: "read approved project"},
		Session:      SessionDirective{Mode: SessionModeNew},
		Policy:       RunPolicy{Execution: defaultExecutionPolicy()},
	}, &recordingSink{})
	if result.Failure != nil || runner.runCalls != 1 {
		t.Fatalf("failure = %+v, run calls = %d", result.Failure, runner.runCalls)
	}
	if runner.lastRequest.Prompt != "read approved project" {
		t.Fatalf("ACP input includes managed instructions instead of only the business message: %q", runner.lastRequest.Prompt)
	}
	if _, err := os.Stat(filepath.Join(sharedDirectory, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("shared cwd unexpectedly contains managed instructions: %v", err)
	}
	instructions, err := os.ReadFile(filepath.Join(workspaceDirectory, "AGENTS.md"))
	if err != nil || !strings.Contains(string(instructions), "独立 Worker 私有集成上下文") {
		t.Fatalf("private instructions = %q, error = %v", instructions, err)
	}
}

func TestRunDeliversInvocationStartedBeforeProtocolEvents(t *testing.T) {
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	sdk, err := New(Config{Profiles: []ProfileSpec{{
		ID:               "codex-run-fixture",
		BaseRuntimeID:    RuntimeIDCodex,
		BinaryCandidates: []string{testBinary},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	backend, ok := sdk.acp.(*acpBackend)
	if !ok {
		t.Fatalf("ACP backend = %T", sdk.acp)
	}
	fakeAgent := &fakeCodexACPAgent{
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
	}
	replaceACPAgentFactory(backend, RuntimeIDCodex, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return fakeAgent, nil
	})
	t.Cleanup(func() {
		closeContext, cancelClose := context.WithTimeout(context.Background(), time.Second)
		defer cancelClose()
		if err := sdk.Close(closeContext); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	sink := &recordingSink{}
	workspacePath := t.TempDir()
	result := sdk.Run(context.Background(), RunRequest{
		Target: RuntimeTarget{RuntimeID: "codex-run-fixture"},
		Detection: readyDetectionForRun(
			t,
			sdk,
			"codex-run-fixture",
			testBinary,
		),
		Workspace: Workspace{
			Path:    workspacePath,
			Managed: true,
		},
		Message: Message{Text: "hello"},
		Instructions: []Instruction{{
			Name:    "IDENTITY.md",
			Content: "当前伙伴档案",
		}},
		Session: SessionDirective{Mode: SessionModeNew},
		Policy: RunPolicy{
			Execution: defaultExecutionPolicy(),
		},
	}, sink)
	if result.Failure != nil {
		t.Fatalf("Run() failure = %+v", result.Failure)
	}
	types := sink.eventTypes()
	if len(types) == 0 || types[0] != EventTypeInvocationStarted {
		t.Fatalf("event types = %#v, want invocation_started first", types)
	}
	for _, privateType := range []EventType{
		EventTypeStatus,
		EventTypeSession,
		EventTypeError,
		EventTypeDiagnostic,
		EventTypeRaw,
	} {
		if containsEventType(types, privateType) {
			t.Fatalf("public event types = %#v, contain private %s", types, privateType)
		}
	}
	instructions, err := os.ReadFile(filepath.Join(workspacePath, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read Runtime instructions: %v", err)
	}
	if !strings.Contains(string(instructions), "当前伙伴档案") {
		t.Fatalf("Runtime instructions = %q", instructions)
	}
	fakeAgent.mutex.Lock()
	defer fakeAgent.mutex.Unlock()
	if fakeAgent.newSessionRequest.Cwd != workspacePath {
		t.Fatalf("ACP cwd = %q, want the directory containing AGENTS.md", fakeAgent.newSessionRequest.Cwd)
	}
	if len(fakeAgent.promptRequest.Prompt) != 1 || fakeAgent.promptRequest.Prompt[0].Text == nil || fakeAgent.promptRequest.Prompt[0].Text.Text != "hello" {
		t.Fatalf("ACP input includes managed instructions: %+v", fakeAgent.promptRequest.Prompt)
	}
}

func TestRunRejectsIncompleteStructuredRequestBeforeStarting(t *testing.T) {
	sdk, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}
	result := sdk.Run(context.Background(), RunRequest{
		Target:    RuntimeTarget{RuntimeID: RuntimeIDCodex},
		Workspace: Workspace{Path: t.TempDir(), Managed: true},
		Session:   SessionDirective{Mode: SessionModeNew},
	}, sink)
	if result.Failure == nil || result.Failure.Code != FailureCodeInvalidRequest {
		t.Fatalf("Run() failure = %+v, want invalid request", result.Failure)
	}
	if types := sink.eventTypes(); len(types) != 0 {
		t.Fatalf("event types = %#v, want no accepted events", types)
	}
}

func TestRunRejectsMissingPermissionLevel(t *testing.T) {
	sdk, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}
	result := sdk.Run(context.Background(), RunRequest{
		Target:    RuntimeTarget{RuntimeID: RuntimeIDCodex},
		Workspace: Workspace{Path: t.TempDir(), Managed: true},
		Message:   Message{Text: "hello"},
		Session:   SessionDirective{Mode: SessionModeNew},
		Policy:    RunPolicy{Execution: ExecutionPolicy{}},
	}, sink)
	if result.Failure == nil || result.Failure.Code != FailureCodeInvalidRequest {
		t.Fatalf("Run() failure = %+v, want invalid request", result.Failure)
	}
	if types := sink.eventTypes(); len(types) != 0 {
		t.Fatalf("event types = %#v, want no accepted events", types)
	}
}

func TestRunDoesNotDeliverStartedWhenRuntimeIsUnavailable(t *testing.T) {
	sdk, err := New(Config{Profiles: []ProfileSpec{{
		ID:               "codex-missing-fixture",
		BaseRuntimeID:    RuntimeIDCodex,
		BinaryCandidates: []string{filepath.Join(t.TempDir(), "missing-codex")},
	}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	sink := &recordingSink{}
	result := sdk.Run(context.Background(), RunRequest{
		Target: RuntimeTarget{RuntimeID: "codex-missing-fixture"},
		Detection: readyDetectionForRun(
			t,
			sdk,
			"codex-missing-fixture",
			filepath.Join(t.TempDir(), "missing-codex"),
		),
		Workspace: Workspace{
			Path:    t.TempDir(),
			Managed: true,
		},
		Message: Message{Text: "hello"},
		Session: SessionDirective{Mode: SessionModeNew},
		Policy: RunPolicy{
			Execution: defaultExecutionPolicy(),
		},
	}, sink)
	if result.Failure == nil || result.Failure.Code != FailureCodeRuntimeUnavailable {
		t.Fatalf("Run() failure = %+v, want runtime unavailable", result.Failure)
	}
	if types := sink.eventTypes(); len(types) != 0 {
		t.Fatalf("event types = %#v, want no accepted events", types)
	}
}

// TestRunSeparatesExpiredDeadlineFromUserCancellation 防止超时被展示成用户主动取消。
func TestRunSeparatesExpiredDeadlineFromUserCancellation(t *testing.T) {
	sdk := &SDK{}
	expired, stopExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stopExpired()
	timedOut := sdk.Run(expired, RunRequest{}, &recordingSink{})
	if timedOut.Failure == nil || timedOut.Failure.Code != constants.RuntimeFailureCodeRequestTimeout {
		t.Fatalf("expired request failure = %#v", timedOut.Failure)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	result := sdk.Run(canceled, RunRequest{}, &recordingSink{})
	if result.Failure == nil || result.Failure.Code != FailureCodeContextCanceled {
		t.Fatalf("canceled request failure = %#v", result.Failure)
	}
}
