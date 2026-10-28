package acp_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	driver "github.com/JieWaZi/NeuxBot/agent-runtime/driver/acp"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// skillResourceFixture 提供宿主已物化的技能目录，记录真实租约归还。
type skillResourceFixture struct {
	bindings []contract.ResourceBinding // bindings 是宿主固定的技能目录视图。
	closes   atomic.Int32               // closes 统计依赖使用结束后的归还次数。
}

// Acquire 将当前宿主资源租约交给真实执行内核。
func (f *skillResourceFixture) Acquire(context.Context, contract.ResourceRequest) (contract.ResourceLease, error) {
	return f, nil
}

// Resources 返回由当前租约保护的技能目录。
func (f *skillResourceFixture) Resources() []contract.ResourceBinding { return f.bindings }

// Close 记录执行内核归还资源的事实。
func (f *skillResourceFixture) Close(context.Context) error { f.closes.Add(1); return nil }

// TestRuntimeACPContributionsReachToolCycleOnce 捕获贡献未交付、工具循环重复注入或过程正文冒充最终输出的错误。
func TestRuntimeACPContributionsReachToolCycleOnce(t *testing.T) {
	for _, policy := range []driver.CompletionPolicy{driver.CompletionNativePhase, driver.CompletionLastAssistant} {
		t.Run(string(policy), func(t *testing.T) {
			agent := newFixtureAgent()
			skillDir := filepath.Join(t.TempDir(), "installed ally")
			resources := &skillResourceFixture{bindings: []contract.ResourceBinding{{ID: "ally-skill", Path: skillDir}}}
			var prompts atomic.Int32
			agent.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				prompts.Add(1)
				if len(p.Prompt) != 1 || p.Prompt[0].Text == nil {
					t.Errorf("expected one initial text block: %+v", p.Prompt)
					return sdk.PromptResponse{}, sdk.NewInvalidParams(nil)
				}
				text := p.Prompt[0].Text.Text
				for _, once := range []string{"MAINTENANCE_INSTRUCTION", filepath.Join(skillDir, "SKILL.md"), "MAINTENANCE_TASK"} {
					if strings.Count(text, once) != 1 {
						t.Errorf("initial context must contain %q exactly once: %q", once, text)
					}
				}
				if !strings.Contains(text, "ally") || !strings.Contains(text, "2026.10") {
					t.Errorf("skill identity missing: %q", text)
				}
				id := "progress"
				updates := []sdk.SessionUpdate{
					{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("tool preparation")}},
					sdk.StartToolCall("knowledge-call", "Update Wiki", sdk.WithStartStatus(sdk.ToolCallStatusInProgress)),
					sdk.UpdateToolCall("knowledge-call", sdk.WithUpdateStatus(sdk.ToolCallStatusCompleted)),
				}
				for _, update := range updates {
					if err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: update}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				id = "final"
				var meta map[string]any
				if policy == driver.CompletionNativePhase {
					meta = map[string]any{"acp-go/message": map[string]any{"phase": "final", "complete": true, "source": "native_phase"}}
				}
				err := agent.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Content: sdk.TextBlock("Wiki maintenance completed"), Meta: meta}}})
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
			}
			agent.closeNative = func(context.Context) error {
				if resources.closes.Load() != 0 {
					return errors.New("skill lease released before native consumer stopped")
				}
				return nil
			}
			d, err := driver.New(driver.Config{ID: "contributions", CompletionPolicy: policy, Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) { return agent, nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := d.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			root := t.TempDir()
			runtime, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{d}, Resources: resources, Grants: contract.Grants{Directories: []string{root}, AllowAllTools: true}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := runtime.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			if err := runtime.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			result := runtime.Invoke(context.Background(), contract.ExecutionSpec{Profile: contract.Profile{DriverID: d.ID()}, Workspace: contract.WorkspaceScope{Root: root}, Input: contract.Input{Text: "MAINTENANCE_TASK"}, Contributions: contract.Contributions{Instructions: []contract.Instruction{{Name: "maintenance", Version: "v1", Text: "MAINTENANCE_INSTRUCTION"}}, Skills: []contract.Skill{{Name: "ally", Version: "2026.10", ResourceID: "ally-skill"}}}, Policies: contract.Policies{Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}, NativeConfig: contract.NativeConfigInherit, Output: contract.OutputStrict}})
			if result.Status != contract.StatusCompleted || result.Fault != nil || result.CleanupFault != nil || result.Output.Status != contract.OutputAvailable || result.Output.Text != "Wiki maintenance completed" || !result.Effects.ToolActivity {
				t.Fatalf("tool cycle lost final output: %+v", result)
			}
			if prompts.Load() != 1 || agent.closes.Load() != 1 || resources.closes.Load() != 1 {
				t.Fatalf("replayed prompt or incorrect cleanup: prompts=%d native=%d resources=%d", prompts.Load(), agent.closes.Load(), resources.closes.Load())
			}
		})
	}
}

// TestACPInitialContextSnapshotIsDeterministic 捕获工厂或调用方改写贡献影响已打开会话、资源顺序影响提示词的错误。
func TestACPInitialContextSnapshotIsDeterministic(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "ally")
	var previous string
	for _, reverse := range []bool{false, true} {
		agent := newFixtureAgent()
		request := openRequest(t)
		request.Contributions = contract.Contributions{Instructions: []contract.Instruction{{Name: "maintenance", Version: "v1", Text: "fixed instructions"}}, Skills: []contract.Skill{{Name: "ally", Version: "v2", ResourceID: "ally"}, {Name: "optional", ResourceID: "optional", Optional: true}}}
		request.Resources = []contract.ResourceBinding{{ID: "ally", Path: skillDir}, {ID: "optional", Path: filepath.Join(skillDir, "optional")}}
		if reverse {
			request.Resources[0], request.Resources[1] = request.Resources[1], request.Resources[0]
		}
		var prompt string
		agent.prompt = func(_ context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
			prompt = p.Prompt[0].Text.Text
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
		}
		d, err := driver.New(driver.Config{ID: "snapshot", Factory: func(_ context.Context, r contract.OpenRequest) (sdk.Agent, error) {
			r.Contributions.Instructions[0].Text = "factory mutated instructions"
			r.Contributions.Skills[0].Name = "factory mutated skill"
			r.Resources[0].Path = "/factory-mutated"
			return agent, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.Close(context.Background()) })
		s, err := d.Open(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		request.Contributions.Instructions[0].Text = "caller mutated instructions"
		request.Contributions.Skills[0].Name = "caller mutated skill"
		request.Resources[0].Path = "/caller-mutated"
		result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "task body"}}, &recordingSink{})
		if result.Status != contract.StatusCompleted || strings.Contains(prompt, "mutated") || strings.Count(prompt, "fixed instructions") != 1 || strings.Count(prompt, filepath.Join(skillDir, "SKILL.md")) != 1 || !strings.HasSuffix(prompt, "task body") {
			t.Fatalf("context snapshot changed: result=%+v prompt=%q", result, prompt)
		}
		if reverse && prompt != previous {
			t.Fatalf("resource binding order changed prompt:\n%q\n%q", previous, prompt)
		}
		previous = prompt
	}
}

// TestACPEmptyContributionsKeepPromptBytes 捕获纯生成被加入静态包装或空可选技能说明的错误。
func TestACPEmptyContributionsKeepPromptBytes(t *testing.T) {
	for _, optional := range []bool{false, true} {
		agent := newFixtureAgent()
		want := "  original task\n中文\n\t"
		agent.prompt = func(_ context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
			if len(p.Prompt) != 1 || p.Prompt[0].Text == nil || p.Prompt[0].Text.Text != want {
				t.Errorf("prompt bytes changed: %+v", p.Prompt)
			}
			return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
		}
		d := newDriver(t, agent)
		request := openRequest(t)
		if optional {
			request.Contributions.Skills = []contract.Skill{{Name: "missing optional", ResourceID: "missing", Optional: true}}
		}
		s, err := d.Open(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: want}}, &recordingSink{})
		if result.Status != contract.StatusCompleted {
			t.Fatal(result)
		}
	}
}

// TestACPContributionValidationPrecedesNativeLaunch 捕获缺失、冲突、相对或超限资源被静默使用的错误。
func TestACPContributionValidationPrecedesNativeLaunch(t *testing.T) {
	for _, sample := range []struct {
		name   string                      // name 标识无效贡献场景。
		change func(*contract.OpenRequest) // change 只构造当前边界的无效请求。
		code   contract.FaultCode          // code 是预期错误类别。
	}{
		{name: "missing binding", change: func(r *contract.OpenRequest) { r.Resources = nil }, code: contract.FaultPreparation},
		{name: "relative path", change: func(r *contract.OpenRequest) { r.Resources[0].Path = "relative/ally" }, code: contract.FaultInvalidSpec},
		{name: "empty path", change: func(r *contract.OpenRequest) { r.Resources[0].Path = "" }, code: contract.FaultInvalidSpec},
		{name: "conflicting binding", change: func(r *contract.OpenRequest) {
			r.Resources = append(r.Resources, contract.ResourceBinding{ID: "ally", Path: "/different"})
		}, code: contract.FaultPreparation},
		{name: "unrequested binding", change: func(r *contract.OpenRequest) {
			r.Resources = append(r.Resources, contract.ResourceBinding{ID: "unrequested", Path: "/different"})
		}, code: contract.FaultUnsupported},
		{name: "missing identity", change: func(r *contract.OpenRequest) { r.Contributions.Skills[0].ResourceID = "" }, code: contract.FaultInvalidSpec},
		{name: "oversized instruction", change: func(r *contract.OpenRequest) {
			r.Contributions.Instructions = []contract.Instruction{{Name: "too-large", Text: strings.Repeat("x", (1<<20)+1)}}
		}, code: contract.FaultInvalidSpec},
		{name: "oversized aggregate", change: func(r *contract.OpenRequest) {
			r.Contributions.Instructions = []contract.Instruction{{Name: "first", Text: strings.Repeat("x", 600<<10)}, {Name: "second", Text: strings.Repeat("y", 600<<10)}}
		}, code: contract.FaultInvalidSpec},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var prepared atomic.Bool
			d, err := driver.New(driver.Config{ID: "validation", Factory: func(context.Context, contract.OpenRequest) (sdk.Agent, error) {
				prepared.Store(true)
				return newFixtureAgent(), nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Close(context.Background()) })
			request := openRequest(t)
			request.Contributions.Skills = []contract.Skill{{Name: "ally", ResourceID: "ally"}}
			request.Resources = []contract.ResourceBinding{{ID: "ally", Path: filepath.Join(t.TempDir(), "installed")}}
			sample.change(&request)
			_, err = d.Open(context.Background(), request)
			var f *contract.Fault
			if !errors.As(err, &f) || f.Code != sample.code || prepared.Load() {
				t.Fatalf("invalid contribution reached native launch: error=%v prepared=%v", err, prepared.Load())
			}
		})
	}
}
