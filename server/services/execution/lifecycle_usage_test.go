package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/native"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/acp-go/pkg/acpmeta"
	sdk "github.com/JieWaZi/acp-go/sdk"
)

// terminalUsageAgent 只替换原生进程；Manager、Session、ACP SDK 管道和 Execution 收尾均使用真实实现。
type terminalUsageAgent struct {
	// Agent 保留未声明即失败的原生可选接口。
	sdk.Agent
	// connection 是 SDK 注入的唯一管道连接。
	connection atomic.Pointer[sdk.AgentSideConnection]
	// ready 同步连接注入与初始化。
	ready chan struct{}
	// partial 决定最终回执前是否已观察到较旧的消费与上下文。
	partial bool
	// terminal 是原生最终停止原因。
	terminal sdk.StopReason
	// prompts 记录本轮原生 Prompt 次数。
	prompts atomic.Int32
}

func (a *terminalUsageAgent) SetAgentConnection(connection *sdk.AgentSideConnection) {
	a.connection.Store(connection)
	close(a.ready)
}
func (a *terminalUsageAgent) Initialize(ctx context.Context, _ sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return sdk.InitializeResponse{}, ctx.Err()
	}
	return sdk.InitializeResponse{ProtocolVersion: sdk.ProtocolVersionNumber, AgentCapabilities: sdk.AgentCapabilities{McpCapabilities: sdk.McpCapabilities{Http: true}}}, nil
}
func terminalUsageOptions() []sdk.SessionConfigOption {
	var options []sdk.SessionConfigOption
	for _, entry := range []struct {
		// category 是原生声明的配置种类。
		category sdk.SessionConfigOptionCategory
		// value 是本地 fixture 支持的唯一配置值。
		value string
	}{{sdk.SessionConfigOptionCategoryModel, "gpt-5"}, {sdk.SessionConfigOptionCategoryThoughtLevel, "high"}} {
		choices := sdk.SessionConfigSelectOptionsUngrouped{{Value: sdk.SessionConfigValueId(entry.value), Name: entry.value}}
		options = append(options, sdk.SessionConfigOption{Select: &sdk.SessionConfigOptionSelect{Id: sdk.SessionConfigId(entry.category), Name: string(entry.category), Category: sdk.Ptr(entry.category), CurrentValue: sdk.SessionConfigValueId(entry.value), Options: sdk.SessionConfigSelectOptions{Ungrouped: &choices}}})
	}
	return options
}
func (a *terminalUsageAgent) NewSession(context.Context, sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	return sdk.NewSessionResponse{SessionId: "native-session", ConfigOptions: terminalUsageOptions(), Modes: &sdk.SessionModeState{CurrentModeId: "agent-full-access", AvailableModes: []sdk.SessionMode{{Id: "agent-full-access", Name: "Full access"}}}}, nil
}
func (a *terminalUsageAgent) SetSessionMode(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, nil
}
func (a *terminalUsageAgent) SetSessionConfigOption(context.Context, sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	return sdk.SetSessionConfigOptionResponse{ConfigOptions: terminalUsageOptions()}, nil
}
func (a *terminalUsageAgent) Close(context.Context) error { return nil }
func (a *terminalUsageAgent) Prompt(ctx context.Context, request sdk.PromptRequest) (sdk.PromptResponse, error) {
	a.prompts.Add(1)
	if a.partial {
		partial := acpmeta.TurnUsage{InputTokens: sdk.Ptr(int64(100)), OutputTokens: sdk.Ptr(int64(10)), Source: "fixture.partial", Coverage: "observed_requests", ContextKnown: true}
		if err := a.connection.Load().SessionUpdate(ctx, sdk.SessionNotification{SessionId: request.SessionId, Update: sdk.SessionUpdate{UsageUpdate: &sdk.SessionUsageUpdate{Used: 900, Size: 64000, Meta: acpmeta.WithTurnUsage(nil, partial)}}}); err != nil {
			return sdk.PromptResponse{}, err
		}
	}
	input, output := int64(100), int64(10)
	if a.partial {
		input, output = 250, 30
	}
	reported := acpmeta.TurnUsage{InputTokens: &input, OutputTokens: &output, CacheReadTokens: sdk.Ptr(int64(0)), Complete: true, Source: "fixture.final", Coverage: "native_turn", Model: "fixture-model"}
	return sdk.PromptResponse{StopReason: a.terminal, Meta: acpmeta.WithTurnUsage(nil, reported)}, nil
}

// terminalUsageRuntime 仅观测公开结果与交付边界，不复制 Session 的消费归一化状态。
type terminalUsageRuntime struct {
	// Manager 继续拥有真实 Session、账号资源和调用生命周期。
	*agentapi.Manager
	// rejectFinal 模拟最终观察者失败。
	rejectFinal bool
	// results 在公开 Run 返回后交出同一个结果，供归档完成后检查。
	results chan agentapi.Result
	// usageEvents 记录实际尝试交付给观察者的用量事件数。
	usageEvents atomic.Int32
}
type terminalUsagePreparation struct {
	// PreparedChatRun 是真实公开 Prepare/Bind/Run 句柄。
	agentapi.PreparedChatRun
	// owner 保存当前测试的观察端口。
	owner *terminalUsageRuntime
}

func (r *terminalUsageRuntime) PrepareSession(ctx context.Context, plan agentapi.SessionPlan, session agentapi.SessionDirective) (agentapi.PreparedChatRun, error) {
	prepared, err := r.Manager.PrepareSession(ctx, plan, session)
	if err != nil {
		return nil, err
	}
	return &terminalUsagePreparation{PreparedChatRun: prepared, owner: r}, nil
}
func (p *terminalUsagePreparation) Run(ctx context.Context, turn agentapi.TurnInput, sink agentapi.EventSink) agentapi.Result {
	result := p.PreparedChatRun.Run(ctx, turn, terminalUsageSink{EventSink: sink, owner: p.owner})
	p.owner.results <- result
	return result
}

type terminalUsageSink struct {
	// EventSink 是 Execution 的真实事件接收器。
	agentapi.EventSink
	// owner 仅控制故障注入及记录事件数。
	owner *terminalUsageRuntime
}

func (s terminalUsageSink) Accept(ctx context.Context, event agentapi.Event) error {
	if event.Type == agentapi.EventTypeUsage {
		s.owner.usageEvents.Add(1)
		var usage agentapi.UsageData
		if err := json.Unmarshal(event.Data, &usage); err != nil {
			return err
		}
		if s.owner.rejectFinal && usage.Turn != nil && usage.Turn.Complete != nil && *usage.Turn.Complete {
			return errors.New("observer rejected final usage")
		}
	}
	return s.EventSink.Accept(ctx, event)
}

// TestUsageTerminalExecutionArchive 防止公开终态消费因事件预算或观察者故障在 Execution/JSONL 边界丢失。
func TestUsageTerminalExecutionArchive(t *testing.T) {
	for _, partial := range []bool{false, true} {
		for _, delivery := range []string{"ok", "events", "observer"} {
			for _, terminal := range []sdk.StopReason{sdk.StopReasonEndTurn, sdk.StopReasonCancelled, sdk.StopReason("future_reason")} {
				t.Run(fmt.Sprintf("partial=%t/%s/%s", partial, delivery, terminal), func(t *testing.T) {
					a := &terminalUsageAgent{ready: make(chan struct{}), partial: partial, terminal: terminal}
					limits := native.Limits{}
					if delivery == "events" {
						limits.MaxEvents = 1
						if partial {
							limits.MaxEvents = 2
						}
					}
					manager := agentapi.NewManager(agentapi.ManagerConfig{
						Limits:   limits,
						Runtimes: func() []agentapi.RuntimeSummary { return (&fakeRuntime{}).Runtimes() },
						Launch: func(agentapi.RuntimeTarget, agentapi.DetectionResult) (agentapi.LaunchFacts, error) {
							return agentapi.LaunchFacts{BaseRuntimeID: "codex", Executable: "fixture"}, nil
						},
						Open: func(ctx context.Context, config native.ConnectionConfig, request native.OpenRequest) (agentapi.NativeConnection, error) {
							config.Factory = func(context.Context, native.OpenRequest) (sdk.Agent, error) { return a, nil }
							return native.Open(ctx, config, request)
						},
					})
					manager.Start(context.Background())
					t.Cleanup(func() {
						if err := manager.Close(context.Background()); err != nil {
							t.Error(err)
						}
					})
					runtime := &terminalUsageRuntime{Manager: manager, rejectFinal: delivery == "observer", results: make(chan agentapi.Result, 1)}
					app := newMessageTestService(t, t.TempDir(), runtime)
					intent := retryTestIntent(t)
					intent.Options.WorkspaceReadOnly = false
					accepted := retryTestStart(t, app, intent)
					var result agentapi.Result
					select {
					case result = <-runtime.results:
					default:
						t.Fatal("public Run did not return")
					}
					if a.prompts.Load() != 1 {
						t.Fatalf("Prompt replayed: %d", a.prompts.Load())
					}
					core := app.execution.(*service)
					records, err := core.journal.ReadRecords(accepted.Chat.ID)
					if err != nil {
						t.Fatal(err)
					}
					var finished eventlog.ExecutionFinishedData
					var invocation eventlog.InvocationFinishedData
					terminalCount := 0
					for _, record := range records {
						switch record.Type {
						case constants.ChatEventTypeExecutionCompleted, constants.ChatEventTypeExecutionFailed, constants.ChatEventTypeExecutionCanceled:
							terminalCount++
							if err = json.Unmarshal(record.Data, &finished); err != nil {
								t.Fatal(err)
							}
						case constants.ChatEventTypeInvocationFinished:
							if err = json.Unmarshal(record.Data, &invocation); err != nil {
								t.Fatal(err)
							}
						}
					}
					var usage agentapi.UsageData
					if err = json.Unmarshal(finished.Usage, &usage); err != nil {
						t.Fatalf("terminal usage absent: %s error=%v", finished.Usage, err)
					}
					wantInput, wantOutput := int64(100), int64(10)
					if partial {
						wantInput, wantOutput = 250, 30
					}
					if terminalCount != 1 || usage.Turn == nil || usage.Turn.InputTokens != wantInput || usage.Turn.OutputTokens != wantOutput || usage.Turn.Source != "fixture.final" || usage.Turn.Coverage != "native_turn" || usage.Turn.Model != "fixture-model" || usage.Turn.Complete == nil || !*usage.Turn.Complete {
						t.Fatalf("terminal consumption lost: count=%d data=%s", terminalCount, finished.Usage)
					}
					if len(usage.Turn.KnownFields) != 3 || usage.Turn.KnownFields[1] != "cache_read_tokens" {
						t.Fatalf("field presence lost: %s", finished.Usage)
					}
					if partial && (usage.Context == nil || usage.Context.UsedTokens != 900 || usage.Context.WindowTokens != 64000) {
						t.Fatalf("context lost: %s", finished.Usage)
					}
					if !partial && usage.Context != nil {
						t.Fatalf("unknown context became zero: %s", finished.Usage)
					}
					if terminal == sdk.StopReasonCancelled && finished.Status != constants.ExecutionStatusCanceled {
						t.Fatalf("cancellation replaced: %+v", finished)
					}
					if terminal == sdk.StopReason("future_reason") && (finished.Status != constants.ExecutionStatusFailed || invocation.FailureCode == agentapi.FailureCodeEventDelivery) {
						t.Fatalf("native failure replaced: %+v", invocation)
					}
					if terminal == sdk.StopReasonEndTurn && delivery != "ok" && (finished.Status != constants.ExecutionStatusFailed || invocation.FailureCode != agentapi.FailureCodeEventDelivery) {
						t.Fatalf("delivery failure lost: %+v", invocation)
					}
					wantEvents := int32(0)
					if partial {
						wantEvents++
					}
					if delivery != "events" {
						wantEvents++
					}
					if runtime.usageEvents.Load() != wantEvents {
						t.Fatalf("usage bypassed event budget: %d want %d", runtime.usageEvents.Load(), wantEvents)
					}
					messages, err := messageview.ReplayMessages(records)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, message := range messages {
						if message.RunMetrics != nil {
							found = true
							if message.RunMetrics.InputTokens != wantInput || message.RunMetrics.OutputTokens != wantOutput || partial && message.RunMetrics.ContextUsedTokens != 900 {
								t.Fatalf("replay consumption changed: %+v", message.RunMetrics)
							}
						}
					}
					if !found {
						t.Fatal("replay omitted usage")
					}
					result.Usage.Turn.InputTokens = 999
					*result.Usage.Turn.Complete = false
					again, err := core.journal.ReadRecords(accepted.Chat.ID)
					if err != nil {
						t.Fatal(err)
					}
					if string(again[len(again)-1].Data) != string(records[len(records)-1].Data) {
						t.Fatal("consumer rewrote archived terminal")
					}
				})
			}
		}
	}
}
