package workflows

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// consumerAdmissionEnvironment 提供当前 Runtime 默认值，调用者已接纳选择由 StartRunCommand 单独提供。
type consumerAdmissionEnvironment struct{}

// Get 返回所选原生 Runtime 的真实注册元数据与测试默认值。
func (consumerAdmissionEnvironment) Get(ctx context.Context, id string) (models.RuntimeEnvironment, error) {
	environment, err := (fixtureEnvironment{}).Get(ctx, id)
	if err != nil {
		return environment, err
	}
	catalog, err := agentapi.NewCatalog(agentapi.CatalogConfig{})
	if err != nil {
		return environment, err
	}
	for _, registered := range catalog.Runtimes() {
		if registered.ID == id {
			environment.RuntimeID, environment.BaseRuntimeID = id, id
			environment.Detection.Runtime = registered
			break
		}
	}
	environment.DefaultModel, environment.DefaultReasoning = id+"-latest", "high"
	fast := true
	environment.DefaultFast = &fast
	return environment, nil
}

func TestConsumerWorkerAdmissionPreservesOnlyMatchingCallerSelection(t *testing.T) {
	fast := true
	for _, test := range []struct {
		// name 标识实际节点覆盖分支。
		name string
		// account 是调用者已接纳的账号身份。
		account string
		// resolve 是固定在 Workflow 源码中的节点覆盖。
		resolve string
		// runtime 是节点应实际采用的 Runtime。
		runtime string
		// model 是手工确定的有效模型，空值表示保留原生默认。
		model string
		// reasoning 是手工确定的有效推理选择。
		reasoning string
		// fast 是手工确定的有效快速模式意图。
		fast *bool
	}{
		{name: "same caller", account: "system", resolve: `{"kind":"waker","wakerId":"fixture-waker","runtimeId":"codex"}`, runtime: "codex"},
		{name: "same native account alias", account: "", resolve: `{"kind":"waker","wakerId":"fixture-waker","runtimeId":"codex","accountId":"system"}`, runtime: "codex"},
		{name: "changed runtime", account: "system", resolve: `{"kind":"waker","wakerId":"fixture-waker","runtimeId":"claude"}`, runtime: "claude", model: "claude-latest", reasoning: "high", fast: &fast},
		{name: "changed account", account: "previous-managed-account", resolve: `{"kind":"waker","wakerId":"fixture-waker","runtimeId":"codex","accountId":"system"}`, runtime: "codex", model: "codex-latest", reasoning: "high", fast: &fast},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openCoreWorkflowPorts(t, t.TempDir(), &fixtureRuntime{}, consumerAdmissionEnvironment{}, true)
			source := `export const meta={sdk:'ally.workflow',title:'调用者配置',description:'接纳后保留原生默认',phases:[{id:'work',title:'执行',detail:'验证节点有效身份',canExit:true}]};await askUser('继续？',{id:'wait',label:'等待',phase:'work'});return await worker('冻结节点配置',{id:'task',label:'任务',phase:'work',resolve:` + test.resolve + `});`
			definition := savedTestWorkflow(t, s, source)
			run, err := s.StartRun(t.Context(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "consumer-run", ConsumerLeaseID: "actual-caller", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "codex", AccountID: test.account, PermissionLevel: constants.PermissionLevelDefault}})
			if err != nil {
				t.Fatal(err)
			}
			selected := run.WorkerConfigurations["task"]
			if selected.RuntimeID != test.runtime || selected.AccountID != "system" || selected.Model != test.model || selected.Reasoning != test.reasoning || !reflect.DeepEqual(selected.Fast, test.fast) {
				t.Fatalf("worker inherited the wrong caller/default configuration: %+v", selected)
			}
			retried, err := s.StartRun(t.Context(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "frozen-retry", Defaults: run.Defaults, WorkerConfigurations: run.WorkerConfigurations})
			if err != nil {
				t.Fatal(err)
			}
			retrySelection := retried.WorkerConfigurations["task"]
			if !reflect.DeepEqual(retrySelection, selected) {
				t.Fatalf("retry reinterpreted an already frozen worker configuration: %+v", retrySelection)
			}
		})
	}
}

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
	environment.Detection.Models[0].ReasoningOptions = []agentapi.ReasoningOption{{ID: "low"}, {ID: "high"}}
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
	permission agentapi.PermissionLevel
}

// admissionConfigurationRuntime 仅替代模型协议边界，完整 JSONL 与结果对账仍经过 Core。
type admissionConfigurationRuntime struct {
	// observations 保存每个 worker 的实际派发配置。
	observations chan observedWorkerConfiguration
	// final 是本次真实 final 的内容，nil 表示模型没有产生 final。
	final *string
}

func (r *admissionConfigurationRuntime) Run(_ context.Context, request fixtureInvocation) (*string, error) {
	r.observations <- observedWorkerConfiguration{
		account: request.AccountID, model: request.Model, reasoning: request.Reasoning,
		permission: request.Permission,
	}
	return r.final, nil
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
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, environment, true)
	source := `export const meta={sdk:'ally.workflow',title:'配置冻结',description:'等待后恢复',phases:[{id:'work',title:'执行',detail:'冻结默认配置',canExit:true}]};const answer=await askUser('继续？',{id:'wait',label:'等待',phase:'work'});const first=await worker('第一项',{id:'first',label:'继承默认',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex'}});const second=await worker('第二项',{id:'second',label:'显式模型',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex',model:'override',permissionLevel:'full-access'}});if(answer.value==='fail'){throw 'RETRY_TEST';}return [first,second];`
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
	if err := answerHumanFixture(ctx, workflowAPIDependencies(restored), AnswerHumanCommand{RunID: run.ID, HumanID: state.Humans[0].ID, RequestID: "answer", Value: "fail"}); err != nil {
		t.Fatal(err)
	}
	state = awaitFixtureState(t, restored, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusFailed
	})
	for index, model := range []string{"fixture", "override"} {
		permission := agentapi.PermissionLevelDefault
		if index == 1 {
			permission = agentapi.PermissionLevelFullAccess
		}
		var actual observedWorkerConfiguration
		select {
		case actual = <-runtime.observations:
		case <-time.After(5 * time.Second):
			t.Fatalf("worker %d was not dispatched: status=%s failure=%+v", index, state.Run.Status, state.Run.Failure)
		}
		expectedReasoning := reasoning
		if index == 1 {
			// 显式选择 override 模型时使用其原生推理默认，不继承 fixture 模型的默认强度。
			expectedReasoning = ""
		}
		configurationMatches := actual.model == model && actual.reasoning == expectedReasoning && actual.permission == permission
		if actual.account != constants.AiAccountSystemID || !configurationMatches {
			t.Fatalf("worker %d followed changed global defaults: %+v", index, actual)
		}
	}
	retried, err := retryRun(ctx, workflowAPIDependencies(restored), RetryRunCommand{RunID: run.ID, RequestID: "retry", ExpectedVersion: state.Run.Version})
	if err != nil {
		t.Fatal(err)
	}
	retryState := awaitFixtureState(t, restored, retried.ID, func(state controllers.WorkflowRunState) bool { return len(state.Humans) == 1 })
	if err := answerHumanFixture(ctx, workflowAPIDependencies(restored), AnswerHumanCommand{RunID: retried.ID, HumanID: retryState.Humans[0].ID, RequestID: "retry-answer", Value: "continue"}); err != nil {
		t.Fatal(err)
	}
	awaitFixtureState(t, restored, retried.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusSucceeded
	})
	finalState, err := restored.Runs.LoadRunState(ctx, retried.ID)
	if err != nil {
		t.Fatal(err)
	}
	reused := 0
	for _, instance := range finalState.Instances {
		if len(instance.ReusedFrom) > 0 {
			reused++
		}
	}
	if reused != 2 {
		t.Fatalf("retry did not reuse matching worker results: %+v", finalState.Instances)
	}
	if len(runtime.observations) != 0 {
		t.Fatal("reused worker dispatched again")
	}
	for node, configuration := range finalState.Run.WorkerConfigurations {
		expectedReasoning := reasoning
		if node == "second" {
			expectedReasoning = ""
		}
		if configuration.Reasoning != expectedReasoning || configuration.RuntimeID != "codex" {
			t.Fatalf("retry changed frozen config %s: %+v", node, configuration)
		}
	}

}

func TestWorkerNodePermissionCannotEscalateConsumer(t *testing.T) {
	final := "done"
	runtime := &admissionConfigurationRuntime{
		observations: make(chan observedWorkerConfiguration, 1), final: &final,
	}
	s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{})
	source := `export const meta={sdk:'ally.workflow',title:'节点权限',description:'调用权限边界',phases:[{id:'work',title:'执行',detail:'验证权限',canExit:true}]};return await worker('整理关注项',{id:'task',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex',permissionLevel:'full-access'}});`
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
			s, _ := openCoreWorkflowPorts(t, t.TempDir(), runtime, fixtureEnvironment{}, true)
			source := `export const meta={sdk:'ally.workflow',title:'最终结果',description:'真实执行结果',phases:[{id:'work',title:'执行',detail:'检查 final',canExit:true}]};return await worker('完成任务',{id:'task',label:'任务',phase:'work',resolve:{kind:'waker',wakerId:'fixture-waker',runtimeId:'codex'}});`
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
