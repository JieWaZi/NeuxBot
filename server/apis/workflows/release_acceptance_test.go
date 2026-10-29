package workflows

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// releaseFixtureRuntime 只控制模型回执与快慢顺序，真实 Core、结果 MCP 和本地文件仍由生产路径处理。
type releaseFixtureRuntime struct {
	// fixtureRuntime 复用真实 Core 端口需要的配置目录与并发调用记录。
	*fixtureRuntime
	// releaseB 允许测试在 A 已验证后恢复 B 的分析。
	releaseB chan struct{}
	// emptyBlocking 使 B 提供一个无条目但尚未解决的真实阻塞判定。
	emptyBlocking bool
}

func (r *releaseFixtureRuntime) Run(ctx context.Context, request runtimecontract.RunRequest, sink runtimecontract.EventSink) runtimecontract.Result {
	invocation := util.New()
	started, _ := json.Marshal(runtimecontract.InvocationStartedData{InvocationID: invocation})
	failed := func(err error) runtimecontract.Result {
		return runtimecontract.Result{ExitCode: 1, Failure: &runtimecontract.Failure{Code: runtimecontract.FailureCodeProcessExited, Stage: "runtime", Message: err.Error(), Transience: runtimecontract.FailureTransiencePermanent}}
	}
	if err := sink.Accept(ctx, runtimecontract.Event{Type: runtimecontract.EventTypeInvocationStarted, Data: started}); err != nil {
		return failed(err)
	}
	contract, err := fixtureCLI(ctx, request, []string{"workflow", "results", "schema"}, nil)
	if err != nil {
		return failed(err)
	}
	node, _ := contract["nodeId"].(string)
	prompt := request.Message.Text
	environment := ""
	for _, name := range []string{"A", "B", "C"} {
		if strings.Contains(prompt, "环境="+name+"；") || strings.Contains(prompt, `"environment":"`+name+`"`) {
			environment = name
			break
		}
	}
	r.mutex.Lock()
	r.calls[node+":"+environment]++
	r.workspaces = append(r.workspaces, request.Workspace)
	r.mutex.Unlock()
	var data any
	switch node {
	case "check-materials":
		data = map[string]any{"iteration": "", "releaseDate": "", "severityConvention": "", "priorityConvention": "", "summary": "四项缺失", "missingFields": []string{"iteration", "releaseDate", "severityConvention", "priorityConvention"}}
	case "coverage-environment", "risk-environment":
		if environment == "B" {
			select {
			case <-r.releaseB:
			case <-ctx.Done():
				return failed(ctx.Err())
			}
		}
		if environment == "C" {
			return failed(status_error.WorkflowEffectFailed)
		}
		if !strings.Contains(prompt, `"iteration":"Sprint 12"`) || !strings.Contains(prompt, `"severityConvention":"P0-P3"`) {
			return failed(status_error.WorkflowStateInvalid.WithField("reason", "structured material supplement was not propagated"))
		}
		if node == "coverage-environment" {
			data = map[string]any{"total": 1, "coveredCount": 1, "highPriorityGaps": []any{}, "summary": "核心需求覆盖"}
		} else {
			items := []any{map[string]any{"title": environment + "-风险", "severity": "high", "impact": "发布链路", "reason": "仍未验证"}}
			if environment == "B" && r.emptyBlocking {
				items = []any{}
			}
			data = map[string]any{"total": 1, "highRiskItems": items, "blocking": environment == "B" && r.emptyBlocking, "summary": "阻塞原因仍需人工确认"}
		}
	case "recommend-item", "revise-item":
		data = map[string]any{"action": "本地验收后再考虑放行", "residualRisk": "high", "note": "只是建议，尚未执行；独立验证文件"}
	case "execute-item":
		path := filepath.Join(request.Workspace.AdditionalWritableDirectories[0], "acceptance", environment+"-defect-0.json")
		if !strings.Contains(prompt, path) {
			return failed(status_error.WorkflowStateInvalid.WithField("path", "execution prompt omitted the shared artifact path"))
		}
		payload := map[string]any{"environment": environment, "itemId": environment + "-defect-0", "action": "本地验收后再考虑放行", "accepted": true}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return failed(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return failed(err)
		}
		if err := os.WriteFile(path, encoded, 0600); err != nil {
			return failed(err)
		}
		data = map[string]any{"written": true, "artifactPath": path}
	case "verify-item":
		path := filepath.Join(request.Workspace.AdditionalWritableDirectories[0], "acceptance", environment+"-defect-0.json")
		if !strings.Contains(prompt, path) {
			return failed(status_error.WorkflowStateInvalid.WithField("path", "verification prompt omitted the shared artifact path"))
		}
		encoded, err := os.ReadFile(path)
		if err != nil {
			return failed(err)
		}
		var payload map[string]any
		if json.Unmarshal(encoded, &payload) != nil || payload["environment"] != environment || payload["accepted"] != true {
			return failed(status_error.WorkflowEffectFailed)
		}
		data = map[string]any{"exists": true, "matches": true, "verified": true}
	case "release-report":
		data = map[string]any{"decision": "批准发布", "markdown": "这份报告故意和真实否决结果冲突"}
	case "revise-report-facts":
		data = map[string]any{"decision": "不予发布", "markdown": "A 仅完成本地验收；B 未获批准；C 分析失败。本次不予发布。"}
	default:
		return failed(status_error.WorkflowStateInvalid.WithField("node", node))
	}
	if _, err := fixtureCLI(ctx, request, []string{"workflow", "results", "submit", "--request-id", "result"}, map[string]any{"data": data}); err != nil {
		return failed(err)
	}

	return runtimecontract.Result{Facts: runtimecontract.InvocationFacts{InvocationID: invocation, RuntimeID: "codex", BaseRuntimeID: "codex"}, ProtocolHealthy: true}
}

func TestF2WholeCoreMaterialsReworkPipelineRealFilesAndBlockingGuard(t *testing.T) {
	for _, emptyBlocking := range []bool{false, true} {
		name := "residual-risk"
		if emptyBlocking {
			name = "empty-blocking"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			runtime := &releaseFixtureRuntime{fixtureRuntime: &fixtureRuntime{calls: map[string]int{}}, releaseB: make(chan struct{}), emptyBlocking: emptyBlocking}
			s, _ := openCoreWorkflowPorts(t, directory, runtime, fixtureEnvironment{})
			runtime.service = s
			source, err := os.ReadFile("../../internal/workflow/dsl/testdata/environment-release.workflow.js")
			if err != nil {
				t.Fatal(err)
			}
			definition, err := createDefinition(ctx, workflowAPIDependencies(s), CreateDefinitionCommand{RequestID: "full-release", Title: "多环境预审", CreationWakerID: "fixture-waker", ScopeBinding: models.WorkflowScopeBinding{WorkingDirectory: directory}})
			if err != nil {
				t.Fatal(err)
			}
			if _, diagnostics, err := s.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: definition.ID, RequestID: "source", Source: string(source), SDKVersion: constants.WorkflowSDKVersion}); err != nil {
				t.Fatal(err, diagnostics)
			}
			run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "full-f2", Args: map[string]any{"requirements": "R1 核心需求", "defects": "A/B风险，C分析失败", "environments": []string{"A", "B", "C"}, "localAcceptance": true}, Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: "system", Model: "fixture", PermissionLevel: constants.PermissionLevelFullAccess}})
			if err != nil {
				t.Fatal(err)
			}
			pending := func(state controllers.WorkflowRunState, node string, environment string) models.WorkflowHumanRequest {
				for _, human := range state.Humans {
					if human.Status == constants.WorkflowStepStatusWaiting && strings.Contains(human.Prompt, environment) {
						for _, effect := range state.Effects {
							if effect.ID == human.EffectID && effect.Boundary.NodeID == node {
								return human
							}
						}
					}
				}
				return models.WorkflowHumanRequest{}
			}
			answer := func(node, environment string, value any, skip bool) {
				t.Helper()
				state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return pending(state, node, environment).ID != "" })
				human := pending(state, node, environment)
				if err := answerHuman(ctx, workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: human.ID, RequestID: util.New(), Value: value, Skip: skip}); err != nil {
					t.Fatal(err)
				}
			}
			answer("fill-materials", "", `{"iteration":"Sprint 12","releaseDate":"2026-10-10","severityConvention":"P0-P3","priorityConvention":"P1"}`, false)
			answer("approve-item", "环境=A", "rework", false)
			answer("rework-note", "", "增加独立验证要求", false)
			answer("approve-item", "环境=A", "approve", false)
			state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
				for _, effect := range state.Effects {
					if effect.Boundary.NodeID == "verify-item" && effect.Status == constants.WorkflowStepStatusSucceeded {
						return true
					}
				}
				return false
			})
			for _, effect := range state.Effects {
				prompt, _ := effect.Boundary.Arguments[0].Data.(string)
				if strings.Contains(prompt, "环境=B；") && effect.Boundary.NodeID == "risk-environment" && effect.Status == constants.WorkflowStepStatusSucceeded {
					t.Fatal("B completed before the explicit release gate")
				}
			}
			close(runtime.releaseB)
			answer("approve-item", "环境=B", nil, true)
			state = awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
			result, _ := state.Run.Result.(map[string]any)
			stats, _ := result["stats"].(map[string]any)
			contextFacts, _ := result["context"].(map[string]any)
			if state.Run.Status != constants.WorkflowRunStatusSucceeded || result["decision"] != "不予发布" || stats["executedCount"] != float64(1) || stats["verifiedCount"] != float64(1) || stats["approvedCount"] != float64(1) || stats["rejectedCount"] != float64(1) || contextFacts["iteration"] != "Sprint 12" {
				t.Fatalf("full F2 facts: %v", result)
			}
			markdown, _ := result["reportMarkdown"].(string)
			if !strings.Contains(markdown, "不予发布") || strings.Contains(markdown, "故意和真实否决结果冲突") || result["notificationDraft"] != nil {
				t.Fatal("report/notification diverged from actual rejected approval")
			}
			runtime.mutex.Lock()
			defer runtime.mutex.Unlock()
			reportRepairs := 0
			for key, count := range runtime.calls {
				if strings.HasPrefix(key, "revise-report-facts:") {
					reportRepairs += count
				}
			}
			if runtime.calls["revise-item:A"] != 1 || runtime.calls["execute-item:A"] != 1 || runtime.calls["execute-item:B"] != 0 || runtime.calls["recommend-item:C"] != 0 || reportRepairs != 1 {
				t.Fatal("unauthorized or missing actual effect", runtime.calls)
			}
		})
	}
}
