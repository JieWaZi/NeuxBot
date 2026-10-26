package model

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

// TestRealWikiMaintenanceReadsSkillAndCallsCLI 核验真实原生会话能读取技能并执行只读知识命令，不访问生产知识内容。
func TestRealWikiMaintenanceReadsSkillAndCallsCLI(t *testing.T) {
	if os.Getenv("ALLY_REAL_WIKI_MAINTENANCE") != "1" {
		t.Skip("requires explicit real-model Wiki maintenance transport check")
	}
	binary := os.Getenv(constants.AgentCLIBinaryEnvironment)
	if binary == "" {
		t.Fatal("the packaged allycli path is required")
	}
	runtimeID := os.Getenv("ALLY_WIKI_RUNTIME")
	if runtimeID == "" {
		runtimeID = runtimes.RuntimeIDWorkBuddy
	}
	model := os.Getenv("ALLY_WIKI_MODEL")
	if model == "" && runtimeID == runtimes.RuntimeIDWorkBuddy {
		model = "deepseek-v4.1-flash"
	}
	reasoning := os.Getenv("ALLY_WIKI_REASONING")
	sdk, err := runtimes.New(runtimes.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	detection := sdk.Detect(ctx, runtimes.DetectRequest{RuntimeID: runtimeID, BaseEnvironment: os.Environ()})
	if detection.State != runtimes.DetectionStateReady {
		t.Fatalf("%s detection failed: %+v", runtimeID, detection.Diagnostics)
	}
	registry := agentcapability.New(nil)
	defer registry.Close()
	endpoint := httptest.NewServer(registry)
	defer endpoint.Close()
	var calls atomic.Int32
	runner := NewMaintenanceRunner(MaintenanceConfig{
		Runtime: maintenanceRuntimeFunc(func(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
			t.Logf("native policy: runtime=%s permission=%s workspaceReadOnly=%t", runtimeID, request.Policy.Execution.PermissionLevel, request.Policy.Execution.WorkspaceReadOnly)
			result := sdk.Run(ctx, request, sink)
			if result.Failure != nil {
				t.Logf("native terminal: runtime=%s version=%s code=%s stage=%s reason=%.300s", runtimeID, detection.Version, result.Failure.Code, result.Failure.Stage, result.Failure.DisplayMessage)
			} else {
				t.Logf("native terminal: runtime=%s version=%s stop=%s protocolHealthy=%t", runtimeID, detection.Version, result.StopReason, result.ProtocolHealthy)
			}
			return result
		}), Registry: registry, Binary: binary,
		Endpoint: endpoint.URL + constants.AgentCLIPath,
		Model:    model, Reasoning: reasoning,
		Prepare: func(context.Context) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
			return runtimes.RuntimeTarget{RuntimeID: runtimeID, Binary: detection.ResolvedBinary}, detection, nil, nil
		},
	})
	input := MaintenanceRequest{
		JobID:        "readonly-maintenance-transport-check",
		Instructions: "This is a read-only transport verification, with one synthetic library named transport-check. Only knowledge.audit.get is authorized. Do not read any other local files, access other services, modify files, or attempt any writes.",
		Message:      "Read the selected ally SKILL.md using the Read tool. Then use the exact supplied allycli executable to run capabilities and knowledge audit get --library-id transport-check. Do not plan a Wiki edit or call ExitPlanMode. Report the marker returned by the audit. This checks software transport only, not real knowledge contents.",
		Validate:     func(context.Context) error { return nil },
		Bindings: []agentcapability.Binding{{Operation: "knowledge.audit.get", Call: func(_ context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			if request.Params["libraryId"] != "transport-check" {
				t.Errorf("unexpected synthetic scope: %+v", request.Params)
			}
			calls.Add(1)
			return agentcapability.Result{Data: map[string]any{"marker": "WIKI_TRANSPORT_OK", "issues": []any{}}}, nil
		}}},
	}
	report, err := runner.Run(ctx, input)
	if err != nil || calls.Load() < 1 || !strings.Contains(report, "WIKI_TRANSPORT_OK") {
		t.Fatalf("synthetic maintenance transport failed: calls=%d err=%v report=%q", calls.Load(), err, report)
	}
	t.Logf("real %s skill + CLI transport passed, version=%s calls=%d", runtimeID, detection.Version, calls.Load())
}
