package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// pluginUsageRuntime 只替换原生执行，在提交时观察真实插件身份租约上的工作引用。
type pluginUsageRuntime struct {
	// leasedChatRuntime 保留同一会话持有的真实插件资源。
	leasedChatRuntime
	// authorizer 是插件安装模块拥有的真实共享身份。
	authorizer *authorization.Authorizer
	// observed 保存每次提交时的工作引用，终态后由测试读取。
	observed [][]authorization.Usage
	// acquisitions 统计真实资源物化次数。
	acquisitions int
}

// pluginUsagePreparation 只观察接纳层显式交还真实资源的次数。
type pluginUsagePreparation struct {
	// PreparedChatRun 保留消费者的阶段与所有权。
	runtimecontract.PreparedChatRun
	// owner 保存当前测试的身份与提交观察值。
	owner *pluginUsageRuntime
}

func (p *pluginUsagePreparation) Bind(ctx context.Context, resources runtimecontract.SessionResources) error {
	p.owner.acquisitions++
	return p.PreparedChatRun.Bind(ctx, resources)
}
func (p *pluginUsagePreparation) Run(ctx context.Context, turn runtimecontract.TurnInput, sink runtimecontract.EventSink) runtimecontract.Result {
	p.owner.observed = append(p.owner.observed, p.owner.authorizer.Usages())
	return p.PreparedChatRun.Run(ctx, turn, sink)
}

// PrepareSession 保留同一原生消费者，只在提交边界观察已有身份事实。
func (r *pluginUsageRuntime) PrepareSession(ctx context.Context, plan runtimecontract.SessionPlan, session runtimecontract.SessionDirective) (runtimecontract.PreparedChatRun, error) {
	prepared, err := r.leasedChatRuntime.PrepareSession(ctx, plan, session)
	if err != nil {
		return nil, err
	}
	return &pluginUsagePreparation{PreparedChatRun: prepared, owner: r}, nil
}

// TestChatHotPluginUsageTracksCurrentExecutionAndClearsAtIdle 防止热会话继续展示首轮执行或把闲置资源当作活动工作。
func TestChatHotPluginUsageTracksCurrentExecutionAndClearsAtIdle(t *testing.T) {
	plugins, state, _ := authenticatedPluginSourceFixture(t, authenticationFixtureShape{Canonical: true})
	state.Installations[0].WakerID = "waker-1"
	authorizer, _, err := plugins.pluginInstallations.Bridge(state.Versions[0])
	if err != nil {
		t.Fatal(err)
	}
	runtime := &pluginUsageRuntime{authorizer: authorizer}
	runtime.result.Session = runtimecontract.SessionResult{OutputID: "native-session", State: runtimecontract.SessionStateCreated}
	manager := newMessageTestService(t, t.TempDir(), &runtime.accountRuntime, messageTestOptions{skillDataDirectory: plugins.skillDataDirectory, plugins: installedPluginFixture{states: []models.PluginState{state}}})
	core := manager.execution.(*service)
	core.runtime, core.pluginInstallations = runtime, plugins.pluginInstallations
	t.Cleanup(func() { _ = runtime.CloseWorkspace(context.Background(), "") })
	intent := WorkIntent{ClientRequestID: "plugin-usage", Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: t.TempDir()}}, Input: WorkInput{Actor: "user", Content: "first"}, Options: WorkOptions{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto}}
	first, err := core.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if uses := authorizer.Usages(); len(uses) != 0 {
		t.Fatalf("idle consumer still reports finished execution: %+v", uses)
	}
	intent.ClientRequestID, intent.Chat, intent.Input.Content = "", ChatTarget{ChatID: first.Chat.ID}, "second"
	second, err := core.Start(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, second.Chat.ID)
	if uses := authorizer.Usages(); len(uses) != 0 || runtime.acquisitions != 1 || runtime.release == nil {
		t.Fatalf("idle usage=%+v acquisitions=%d retained=%v", uses, runtime.acquisitions, runtime.release != nil)
	}
	if len(runtime.observed) != 2 || len(runtime.observed[0]) != 1 || len(runtime.observed[1]) != 1 || runtime.observed[0][0].ExecutionID != first.Execution.ID || runtime.observed[1][0].ExecutionID != second.Execution.ID || runtime.observed[1][0].ChatID != first.Chat.ID {
		t.Fatalf("current execution usage was not refreshed: %+v", runtime.observed)
	}
}

// usageTerminalJournal 只替代日志存储边界，在可接纳终态发布时观察真实共享身份。
type usageTerminalJournal struct {
	// terminalJournal 记录终态，未使用的日志端口维持失败即暴露的测试行为。
	terminalJournal
	// authorizer 拥有同一热消费者的真实使用引用。
	authorizer *authorization.Authorizer
	// idleAtPublish 表示终态公开前旧轮使用引用已经清空。
	idleAtPublish bool
}

func (j *usageTerminalJournal) MarkExecutionStarted(_ context.Context, execution models.Execution, _ string) (models.Execution, error) {
	return execution, nil
}
func (j *usageTerminalJournal) AppendExecutionFacts(context.Context, string, []eventlog.AppendInput) ([]eventlog.Record, error) {
	return nil, nil
}
func (j *usageTerminalJournal) PublishTerminal(chatservice.TerminalCommit) {
	j.idleAtPublish = len(j.authorizer.Usages()) == 0
}

// usageHandoffPreparation 让测试只在 Run 外层的真实准备收尾点模拟下一轮接纳。
type usageHandoffPreparation struct {
	// synchronousPreparation 提供已绑定热消费者及明确的关闭观察端口。
	*synchronousPreparation
}

func (p *usageHandoffPreparation) Run(ctx context.Context, turn runtimecontract.TurnInput, sink runtimecontract.EventSink) runtimecontract.Result {
	p.started = true
	return p.run(ctx, recordedRun{SessionPlan: p.plan, TurnInput: turn, Session: p.session}, sink)
}

// TestPluginUsageClearsBeforeTerminalAndNextAdmission 确定性重现旧轮退出清理覆盖新轮使用引用的问题。
func TestPluginUsageClearsBeforeTerminalAndNextAdmission(t *testing.T) {
	cases := []struct {
		// name 标识当前执行终态或原生接纳分支。
		name string
		// accepted 表示 Runtime 是否发出真实启动确认。
		accepted bool
		// failure 是当前分支的固定运行结果。
		failure *runtimecontract.Failure
		// status 是权威终态应当保留的执行状态。
		status constants.ExecutionStatus
	}{
		{name: "completed", accepted: true, status: constants.ExecutionStatusCompleted},
		{name: "failed", accepted: true, failure: &runtimecontract.Failure{Code: runtimediagnostic.FailureCodeRuntimeUnavailable}, status: constants.ExecutionStatusFailed},
		{name: "canceled", accepted: true, failure: &runtimecontract.Failure{Code: runtimediagnostic.FailureCodeContextCanceled}, status: constants.ExecutionStatusCanceled},
		{name: "rejected", failure: &runtimecontract.Failure{Code: runtimediagnostic.FailureCodeRuntimeUnavailable}, status: constants.ExecutionStatusFailed},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			const chatID, firstID, secondID = "usage-chat", "first-execution", "second-execution"
			authorizer := authorization.New(authorization.Config{})
			update, release, err := authorizer.AcquireUsage(authorization.Usage{ChatID: chatID, ExecutionID: firstID})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			registry := newExecutionRegistry()
			registry.start(context.Background())
			defer registry.close()
			defer registry.release(chatID, secondID)
			if _, err := registry.reserve(chatID, firstID); err != nil {
				t.Fatal(err)
			}
			journal := &usageTerminalJournal{authorizer: authorizer}
			core := &service{journal: journal, executions: registry, elicitations: newElicitationCoordinator(journal)}
			cleanupCount := 0
			var admissionError error
			prepared := &usageHandoffPreparation{synchronousPreparation: &synchronousPreparation{
				run: func(ctx context.Context, _ recordedRun, sink runtimecontract.EventSink) runtimecontract.Result {
					if test.accepted {
						if err := sink.Accept(ctx, runtimecontract.Event{Type: runtimecontract.EventTypeInvocationStarted, Data: marshalData(runtimecontract.InvocationStartedData{InvocationID: "invocation"})}); err != nil {
							t.Fatal(err)
						}
					}
					return runtimecontract.Result{Failure: test.failure}
				},
				close: func(context.Context) error {
					// 此关闭入口在 finishExecution 返回后执行，此时新轮确实可以取得同一 Chat。
					_, admissionError = registry.reserve(chatID, secondID)
					if admissionError == nil {
						update(authorization.Usage{ChatID: chatID, ExecutionID: secondID})
					}
					return nil
				},
			}}
			core.runExecution(context.Background(), messageExecution{
				pluginUsageRelease: func() { cleanupCount++; update() }, preparedRun: prepared,
				state: messageChat{chat: models.Chat{ID: chatID}}, execution: models.Execution{ID: firstID, ChatID: chatID},
				files: &preparedMessageFiles{}, precommitted: true,
			})
			if !journal.idleAtPublish {
				t.Error("terminal was published while the completed execution still owned plugin usage")
			}
			if admissionError != nil || journal.terminal.Status != test.status || cleanupCount != 1 {
				t.Fatalf("handoff admission=%v terminal=%s cleanup=%d", admissionError, journal.terminal.Status, cleanupCount)
			}
			if uses := authorizer.Usages(); len(uses) != 1 || uses[0].ExecutionID != secondID {
				t.Fatalf("old execution cleanup erased next admission usage: %+v", uses)
			}
			if err := authorizer.Logout(context.Background()); !errors.Is(err, status_error.PluginBusy) {
				t.Fatalf("turn cleanup released the hot identity lease: %v", err)
			}
		})
	}
}
