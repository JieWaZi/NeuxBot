package acp_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/agent-runtime/contract"
	agentruntime "github.com/JieWaZi/NeuxBot/agent-runtime/runtime"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

func selectionOption(category sdk.SessionConfigOptionCategory, id, current string, values ...string) sdk.SessionConfigOption {
	choices := make(sdk.SessionConfigSelectOptionsUngrouped, 0, len(values))
	for _, value := range values {
		choices = append(choices, sdk.SessionConfigSelectOption{Value: sdk.SessionConfigValueId(value), Name: value})
	}
	return sdk.SessionConfigOption{Select: &sdk.SessionConfigOptionSelect{Id: sdk.SessionConfigId(id), Name: id, Category: &category, CurrentValue: sdk.SessionConfigValueId(current), Options: sdk.SessionConfigSelectOptions{Ungrouped: &choices}}}
}

// TestGenerationConfigurationUsesConfirmedNativeChoices 防止工具授权冒充原生权限或推理降级被虚报为关闭。
func TestGenerationConfigurationUsesConfirmedNativeChoices(t *testing.T) {
	for _, sample := range []struct {
		name     string   // name 标识当前推理情景。
		values   []string // values 是模型实际公布的推理档位。
		explicit string   // explicit 是用户明确覆盖的档位。
		want     string   // want 是确认后应生效的档位。
		disabled bool     // disabled 表示该档位真正关闭推理。
		fallback string   // fallback 是无法关闭时的真实降级说明。
	}{
		{name: "native off", values: []string{"medium", "off", "low"}, want: "off", disabled: true},
		{name: "lowest supported", values: []string{"high", "medium", "low"}, want: "low", fallback: "thinking_off_unsupported: selected low"},
		{name: "explicit wins", values: []string{"off", "high"}, explicit: "high", want: "high"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{
				selectionOption(sdk.SessionConfigOptionCategoryMode, "native-mode", "agent", "agent", "agent-full-access"),
				selectionOption(sdk.SessionConfigOptionCategoryModel, "model", "default", "selected"),
				selectionOption(sdk.SessionConfigOptionCategoryThoughtLevel, "reasoning", "medium", sample.values...),
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			var selected []string
			a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				selected = append(selected, string(r.ValueId.ConfigId)+"="+string(r.ValueId.Value))
				for _, option := range options {
					if option.Select.Id == r.ValueId.ConfigId {
						option.Select.CurrentValue = r.ValueId.Value
					}
				}
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newDriver(t, a)
			r := openRequest(t)
			no := false
			r.Profile = contract.Profile{Model: "selected", NativeMode: "agent-full-access", Thinking: &no, Reasoning: sample.explicit}
			s, err := d.Open(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			result := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "answer"}}, &recordingSink{})
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := []string{"native-mode=agent-full-access", "model=selected", "reasoning=" + sample.want}
			if !reflect.DeepEqual(selected, want) {
				t.Fatalf("native configuration = %#v, want %#v", selected, want)
			}
			c := result.Configuration
			if c.ActualMode != "agent-full-access" || c.ActualModel != "selected" || c.ActualReasoning != sample.want || c.ThinkingDisabled != sample.disabled || c.ReasoningFallback != sample.fallback {
				t.Fatalf("unconfirmed diagnostics: %+v", c)
			}
		})
	}
}

// TestNativeModeMustBeAdvertisedAndConfirmed 防止 full-access 请求被静默忽略或在回执不符时继续调用模型。
func TestNativeModeMustBeAdvertisedAndConfirmed(t *testing.T) {
	for _, advertised := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "unconfirmed"}[advertised], func(t *testing.T) {
			a := newFixtureAgent()
			options := []sdk.SessionConfigOption{}
			if advertised {
				options = append(options, selectionOption(sdk.SessionConfigOptionCategoryMode, "mode", "agent", "agent-full-access"))
			}
			a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
				return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
			}
			a.configure = func(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
				return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
			}
			d := newDriver(t, a)
			r := openRequest(t)
			r.Profile.NativeMode = "agent-full-access"
			s, err := d.Open(context.Background(), r)
			if s != nil {
				_ = s.Close(context.Background())
			}
			code := contract.FaultUnsupported
			if advertised {
				code = contract.FaultProtocol
			}
			var f *contract.Fault
			if !errors.As(err, &f) || f.Code != code {
				t.Fatalf("mode selection error = %v, want %s", err, code)
			}
		})
	}
}

// TestPromptUsagePreservesUnknownAndCacheComponents 防止未知用量变成零值或缓存被丢失及重复计入。
func TestPromptUsagePreservesUnknownAndCacheComponents(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "known"}[known], func(t *testing.T) {
			a := newFixtureAgent()
			a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 17}}}); err != nil {
					return sdk.PromptResponse{}, err
				}
				r := sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}
				if known {
					read, write := 7, 2
					r.Usage = &sdk.Usage{InputTokens: 11, OutputTokens: 5, CachedReadTokens: &read, CachedWriteTokens: &write}
				}
				return r, nil
			}
			d := newDriver(t, a)
			s, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			r := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "answer"}}, &recordingSink{})
			_ = s.Close(context.Background())
			if r.Usage.Known != known || !r.Usage.ContextKnown || r.Usage.ContextTokens != 17 {
				t.Fatalf("usage certainty = %+v", r.Usage)
			}
			if known && (r.Usage.InputTokens != 11 || r.Usage.OutputTokens != 5 || r.Usage.CachedReadTokens != 7 || r.Usage.CachedWriteTokens != 2) {
				t.Fatalf("cache usage changed = %+v", r.Usage)
			}
		})
	}
}

// TestPromptInactivityIsDistinctFromCancellation 防止静默原生调用无限占用资源或超时被误报为用户取消。
func TestPromptInactivityIsDistinctFromCancellation(t *testing.T) {
	a := newFixtureAgent()
	a.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := s.Prompt(ctx, contract.TurnRequest{Input: contract.Input{Text: "answer"}, Limits: contract.Limits{InactivityTimeout: 20 * time.Millisecond}}, &recordingSink{})
	_ = s.Close(context.Background())
	if r.Status != contract.StatusFailed || r.Fault == nil || r.Fault.Code != contract.FaultInactivityTimeout {
		t.Fatalf("inactivity result = %+v", r)
	}
}

// TestNativeActivityAndApprovalPauseInactivity 防止原生更新未刷新计时器或审批等待被无活动监视器中断。
func TestNativeActivityAndApprovalPauseInactivity(t *testing.T) {
	for _, approval := range []bool{false, true} {
		t.Run(map[bool]string{false: "native progress", true: "approval wait"}[approval], func(t *testing.T) {
			a := newFixtureAgent()
			a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
				if approval {
					_, err := a.connection.Load().RequestPermission(ctx, sdk.RequestPermissionRequest{SessionId: p.SessionId, ToolCall: sdk.ToolCallUpdate{ToolCallId: "t", Title: sdk.Ptr("Read")}, Options: []sdk.PermissionOption{{OptionId: "allow", Kind: sdk.PermissionOptionKindAllowOnce, Name: "Allow"}}})
					return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, err
				}
				for range 5 {
					select {
					case <-time.After(15 * time.Millisecond):
					case <-ctx.Done():
						return sdk.PromptResponse{}, ctx.Err()
					}
					if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{Plan: &sdk.SessionUpdatePlan{Entries: []sdk.PlanEntry{}}}}); err != nil {
						return sdk.PromptResponse{}, err
					}
				}
				return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
			}
			d := newDriver(t, a)
			s, err := d.Open(context.Background(), openRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			sink := &recordingSink{interact: func(ctx context.Context, r contract.InteractionRequest) (contract.InteractionResponse, error) {
				select {
				case <-time.After(80 * time.Millisecond):
					return contract.InteractionResponse{Invocation: r.Invocation, ID: r.ID, Allowed: true}, nil
				case <-ctx.Done():
					return contract.InteractionResponse{}, ctx.Err()
				}
			}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			r := s.Prompt(ctx, contract.TurnRequest{Input: contract.Input{Text: "answer"}, Limits: contract.Limits{InactivityTimeout: 35 * time.Millisecond}}, sink)
			_ = s.Close(context.Background())
			if r.Status != contract.StatusCompleted || r.Fault != nil {
				t.Fatalf("healthy prompt interrupted = %+v", r)
			}
		})
	}
}

// TestNegativePromptUsageFailsInsteadOfReportingCost 防止无效上游统计进入宿主计费与诊断。
func TestNegativePromptUsageFailsInsteadOfReportingCost(t *testing.T) {
	a := newFixtureAgent()
	a.prompt = func(context.Context, sdk.PromptRequest) (sdk.PromptResponse, error) {
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{InputTokens: -1}}, nil
	}
	d := newDriver(t, a)
	s, err := d.Open(context.Background(), openRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	r := s.Prompt(context.Background(), contract.TurnRequest{Input: contract.Input{Text: "answer"}}, &recordingSink{})
	_ = s.Close(context.Background())
	if r.Fault == nil || r.Fault.Code != contract.FaultProtocol {
		t.Fatalf("invalid usage accepted: %+v", r)
	}
}

// TestInvokePreservesConfirmedConfigurationAndUsage 防止内核丢失原生确认的选择或把上下文统计混入最终生成用量。
func TestInvokePreservesConfirmedConfigurationAndUsage(t *testing.T) {
	a := newFixtureAgent()
	options := []sdk.SessionConfigOption{selectionOption(sdk.SessionConfigOptionCategoryMode, "mode", "agent", "agent-full-access")}
	a.newSession = func(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
		return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: options}, nil
	}
	a.configure = func(_ context.Context, r sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
		options[0].Select.CurrentValue = r.ValueId.Value
		return sdk.SetSessionConfigOptionResponse{ConfigOptions: options}, nil
	}
	a.prompt = func(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
		id := "final"
		err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: p.SessionId, Update: sdk.SessionUpdate{AgentMessageChunk: &sdk.SessionUpdateAgentMessageChunk{MessageId: &id, Meta: map[string]any{"acp-go/message": map[string]any{"phase": "final", "source": "native_phase", "complete": true}}, Content: sdk.TextBlock("done")}}})
		read := 7
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn, Usage: &sdk.Usage{InputTokens: 11, OutputTokens: 5, CachedReadTokens: &read}}, err
	}
	d := newDriver(t, a)
	root := t.TempDir()
	r, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{d}, Grants: contract.Grants{Directories: []string{root}, AllowAllTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := r.Invoke(context.Background(), contract.ExecutionSpec{Profile: contract.Profile{DriverID: d.ID(), NativeMode: "agent-full-access"}, Input: contract.Input{Text: "answer"}, Workspace: contract.WorkspaceScope{Root: root}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}})
	if result.Status != contract.StatusCompleted || result.Output.Text != "done" || result.Configuration.ActualMode != "agent-full-access" || !result.Usage.Known || result.Usage.InputTokens != 11 || result.Usage.CachedReadTokens != 7 || result.Usage.ContextKnown {
		t.Fatalf("lost native facts: %+v", result)
	}
}

// TestInvokeForwardsInactivityBudget 防止内核仅规范化无活动预算却没有传给原生调用。
func TestInvokeForwardsInactivityBudget(t *testing.T) {
	a := newFixtureAgent()
	a.prompt = func(ctx context.Context, _ sdk.PromptRequest) (sdk.PromptResponse, error) {
		<-ctx.Done()
		return sdk.PromptResponse{}, ctx.Err()
	}
	d := newDriver(t, a)
	root := t.TempDir()
	r, err := agentruntime.New(agentruntime.Config{Drivers: []contract.Driver{d}, Grants: contract.Grants{Directories: []string{root}, AllowAllTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	result := r.Invoke(ctx, contract.ExecutionSpec{Profile: contract.Profile{DriverID: d.ID()}, Input: contract.Input{Text: "answer"}, Workspace: contract.WorkspaceScope{Root: root}, Policies: contract.Policies{NativeConfig: contract.NativeConfigInherit, Tools: contract.ToolPolicy{Mode: contract.ToolsAllowAll}}, Limits: contract.Limits{InactivityTimeout: 20 * time.Millisecond}})
	if result.Fault == nil || result.Fault.Code != contract.FaultInactivityTimeout || result.Status != contract.StatusFailed || !result.Effects.PromptAccepted || !result.Effects.OutcomeUnknown {
		t.Fatalf("wrong inactivity outcome: %+v", result)
	}
}
