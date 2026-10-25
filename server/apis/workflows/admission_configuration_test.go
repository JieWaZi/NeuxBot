package workflows

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// mutableAdmissionEnvironment 只替代外部目录配置，运行状态仍由真实 SQLite 与 Core 持有。
type mutableAdmissionEnvironment struct {
	// mutex 保护测试中用户变更默认配置的并发读取。
	mutex sync.Mutex
	// initialReasoning 是接纳时的推理默认值，允许合法空值。
	initialReasoning string
	// changed 表示用户已在人工等待期间改过默认配置。
	changed bool
}

func (e *mutableAdmissionEnvironment) Get(ctx context.Context, id string) (models.RuntimeEnvironment, error) {
	environment, err := (fixtureEnvironment{}).Get(ctx, id)
	e.mutex.Lock()
	defer e.mutex.Unlock()
	environment.DefaultReasoning = e.initialReasoning
	environment.Detection.Models[0].ReasoningOptions = []runtimes.ReasoningOption{{ID: "low"}, {ID: "high"}}
	override := environment.Detection.Models[0]
	override.ID, override.Name = "override", "override"
	environment.Detection.Models = append(environment.Detection.Models, override)
	if e.changed {
		environment.DefaultAccountID = "new-default-account"
		environment.DefaultModel = "override"
		environment.DefaultReasoning = "high"
	}
	return environment, err
}

// observedWorkerConfiguration 保存 Runtime 实际收到的非秘密身份及模型选择。
type observedWorkerConfiguration struct {
	// account 是实际账号身份，原生登录为空。
	account string
	// model 是实际派发模型。
	model string
	// reasoning 是实际派发推理强度。
	reasoning string
	// permission 是 Runtime 实际收到的执行权限等级。
	permission runtimes.PermissionLevel
}

// admissionConfigurationRuntime 仅替代模型协议边界，完整 JSONL 与结果对账仍经过 Core。
type admissionConfigurationRuntime struct {
	// observations 保存每个 worker 的实际派发配置。
	observations chan observedWorkerConfiguration
	// final 是本次真实 final 的内容，nil 表示模型没有产生 final。
	final *string
}

func (*admissionConfigurationRuntime) ForkSession(context.Context, runtimes.ForkSessionRequest) (runtimes.ForkSessionResult, error) {
	return runtimes.ForkSessionResult{}, status_error.WorkflowCapabilityUnsupported
}
func (*admissionConfigurationRuntime) Runtimes() []runtimes.RuntimeSummary {
	return (&fixtureRuntime{}).Runtimes()
}
func (r *admissionConfigurationRuntime) Run(ctx context.Context, request runtimes.RunRequest, sink runtimes.EventSink) runtimes.Result {
	r.observations <- observedWorkerConfiguration{
		account: request.Target.AccountID, model: request.Policy.Model,
		reasoning: request.Policy.Reasoning, permission: request.Policy.Execution.PermissionLevel,
	}
	invocation := util.New()
	started, _ := json.Marshal(runtimes.InvocationStartedData{InvocationID: invocation})
	if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeInvocationStarted, Data: started}); err != nil {
		return runtimes.Result{ExitCode: 1}
	}
	if r.final != nil {
		text, _ := json.Marshal(map[string]any{"text": *r.final})
		if err := sink.Accept(ctx, runtimes.Event{Type: runtimes.EventTypeTextDelta, Data: text}); err != nil {
			return runtimes.Result{ExitCode: 1}
		}
	}
	return runtimes.Result{Facts: runtimes.InvocationFacts{InvocationID: invocation, RuntimeID: "codex", BaseRuntimeID: "codex"}, ProtocolHealthy: true}
}

func TestWorkerAdmissionFreezesDefaultsAcrossHumanWaitAndRestart(t *testing.T) {
	// 外部模型由替身处理，测试不依赖本机 CLI 或用户原生配置。
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	for _, reasoning := range []string{"low", ""} {
		name := reasoning
		if name == "" {
			name = "empty native default"
		}
		t.Run(name, func(t *testing.T) { testWorkerAdmissionFrozenConfiguration(t, reasoning) })
	}
}

func testWorkerAdmissionFrozenConfiguration(t *testing.T, reasoning string) {
	t.Helper()
	ctx := context.Background()
	final := "done"
	runtime := &admissionConfigurationRuntime{observations: make(chan observedWorkerConfiguration, 2), final: &final}
	environment := &mutableAdmissionEnvironment{initialReasoning: reasoning}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, environment)
	source := `export const meta={sdk:'ally.workflow/1',title:'配置冻结',description:'等待后恢复',nodeResultPageOmissions:{first:'只返回默认配置冻结测试的控制值',second:'只返回显式配置冻结测试的控制值'},phases:[{id:'work',title:'执行',detail:'冻结默认配置',canExit:true}]};const answer=await askUser('继续？',{id:'wait',label:'等待',phase:'work'});const first=await worker('第一项',{id:'first',label:'继承默认',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex'}});const second=await worker('第二项',{id:'second',label:'显式模型',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex',model:'override',permissionLevel:'full-access'}});if(answer.value==='fail'){throw 'RETRY_TEST';}return [first,second];`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	state := awaitFixtureState(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) == 1 })
	first, second := state.Run.WorkerConfigurations["first"], state.Run.WorkerConfigurations["second"]
	inherited := first.AccountID == constants.AiAccountSystemID && first.Model == "fixture"
	overridden := second.Model == "override" && second.PermissionLevel == constants.PermissionLevelFullAccess
	if !inherited || !overridden {
		t.Fatalf("effective node configurations were not persisted: %+v", state.Run.WorkerConfigurations)
	}
	s.Close()
	environment.mutex.Lock()
	environment.changed = true
	environment.mutex.Unlock()
	restored := workflowTestNew(workflow.Config{Store: s.Store, Runs: s.Runs, Transactions: s.Transactions, Worker: s.Worker, Chats: s.Chats, Executions: s.Executions, Wakers: s.Wakers, Projects: s.Projects, DataDirectory: s.DataDirectory})
	if err := restored.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	if err := answerHuman(ctx, workflowAPIDependencies(restored), AnswerHumanCommand{RunID: run.ID, HumanID: state.Humans[0].ID, RequestID: "answer", Value: "fail"}); err != nil {
		t.Fatal(err)
	}
	state = awaitFixtureState(t, restored, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusFailed
	})
	for index, model := range []string{"fixture", "override"} {
		permission := runtimes.PermissionLevelDefault
		if index == 1 {
			permission = runtimes.PermissionLevelFullAccess
		}
		var actual observedWorkerConfiguration
		select {
		case actual = <-runtime.observations:
		case <-time.After(5 * time.Second):
			t.Fatalf("worker %d was not dispatched: status=%s failure=%+v", index, state.Run.Status, state.Run.Failure)
		}
		configurationMatches := actual.model == model && actual.reasoning == reasoning && actual.permission == permission
		if actual.account != "" || !configurationMatches {
			t.Fatalf("worker %d followed changed global defaults: %+v", index, actual)
		}
	}
	retried, err := retryRun(ctx, workflowAPIDependencies(restored), RetryRunCommand{RunID: run.ID, RequestID: "retry", ExpectedVersion: state.Run.Version})
	if err != nil {
		t.Fatal(err)
	}
	retryState := awaitFixtureState(t, restored, retried.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) == 1 })
	if err := answerHuman(ctx, workflowAPIDependencies(restored), AnswerHumanCommand{RunID: retried.ID, HumanID: retryState.Humans[0].ID, RequestID: "retry-answer", Value: "continue"}); err != nil {
		t.Fatal(err)
	}
	awaitFixtureState(t, restored, retried.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusSucceeded
	})
	for index, model := range []string{"fixture", "override"} {
		permission := runtimes.PermissionLevelDefault
		if index == 1 {
			permission = runtimes.PermissionLevelFullAccess
		}
		var actual observedWorkerConfiguration
		select {
		case actual = <-runtime.observations:
		case <-time.After(5 * time.Second):
			t.Fatalf("worker %d was not dispatched: status=%s failure=%+v", index, state.Run.Status, state.Run.Failure)
		}
		configurationMatches := actual.model == model && actual.reasoning == reasoning && actual.permission == permission
		if actual.account != "" || !configurationMatches {
			t.Fatalf("retry changed the original resolved configuration: %+v", actual)
		}
	}
}

func TestWorkerNodePermissionCannotEscalateConsumer(t *testing.T) {
	final := "done"
	runtime := &admissionConfigurationRuntime{
		observations: make(chan observedWorkerConfiguration, 1), final: &final,
	}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
	source := `export const meta={sdk:'ally.workflow/1',title:'节点权限',description:'调用权限边界',nodeResultPageOmissions:{task:'只返回权限边界测试的控制值'},phases:[{id:'work',title:'执行',detail:'验证权限',canExit:true}]};return await worker('整理关注项',{id:'task',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex',permissionLevel:'full-access'}});`
	definition := savedTestWorkflow(t, s, source)
	_, err := s.StartRun(context.Background(), workflow.StartRunCommand{
		DefinitionID: definition.ID, RequestID: "consumer", ConsumerLeaseID: "caller",
		Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault},
	})
	if workflowstate.ErrorKey(err) != status_error.WorkflowCapabilityUnsupported.Key() {
		t.Fatalf("node permission escaped caller configuration: %v", err)
	}
	if len(runtime.observations) != 0 {
		t.Fatal("rejected node was dispatched")
	}
}

func TestPlainWorkerRequiresFinalAndPreservesExplicitEmptyResult(t *testing.T) {
	for _, present := range []bool{false, true} {
		name := "missing final"
		if present {
			name = "explicit empty final"
		}
		t.Run(name, func(t *testing.T) {
			var final *string
			if present {
				// 正文存在但规范化后为空，不应被误判为缺少 final。
				text := " \n "
				final = &text
			}
			runtime := &admissionConfigurationRuntime{observations: make(chan observedWorkerConfiguration, 1), final: final}
			s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
			source := `export const meta={sdk:'ally.workflow/1',title:'最终结果',description:'真实执行结果',nodeResultPageOmissions:{task:'只返回最终消息协议测试的控制值'},phases:[{id:'work',title:'执行',detail:'检查 final',canExit:true}]};return await worker('完成任务',{id:'task',label:'任务',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex'}});`
			definition := savedTestWorkflow(t, s, source)
			run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex"}})
			if err != nil {
				t.Fatal(err)
			}
			state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool { return workflowstate.TerminalRun(state.Run.Status) })
			if present {
				if state.Run.Status != constants.WorkflowRunStatusSucceeded || state.Run.Result != "" {
					t.Fatalf("explicit empty final was lost: status=%s result=%#v", state.Run.Status, state.Run.Result)
				}
			} else if state.Run.Status != constants.WorkflowRunStatusFailed || len(state.Effects) != 1 || state.Effects[0].Failure == nil || state.Effects[0].Failure.Data.(map[string]any)["code"] != "RESULT_MISSING" {
				t.Fatalf("worker without final became successful: status=%s result=%#v", state.Run.Status, state.Run.Result)
			}
		})
	}
}
