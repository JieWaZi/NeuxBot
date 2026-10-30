package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func savedTestWorkflow(t *testing.T, s *Service, source string) models.WorkflowDefinition {
	t.Helper()
	ctx := context.Background()
	definition, err := fixtureDefinition(ctx, s.store, definitionFixture{RequestID: "create-" + strings.ReplaceAll(t.Name(), "/", "-"), Title: "测试流程"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, diagnostics, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: definition.ID, RequestID: "save", Source: source, SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatalf("compile: %v %+v", err, diagnostics)
	}
	definition.CurrentRevisionID = receipt.RevisionID
	return definition
}
func runTicks(t *testing.T, s *Service, id string, count int) models.WorkflowRun {
	t.Helper()
	for index := 0; index < count; index++ {
		if err := s.Tick(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	value, err := s.runs.GetRun(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestRunAdmissionFreezesVersionDefaultsAndComputesF0(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, workflowTestSource(t))
	command := StartRunCommand{DefinitionID: definition.ID, RequestID: "run", Args: map[string]any{"text": "NeuxBot"}}
	run, err := s.StartRun(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.StartRun(context.Background(), command)
	if err != nil || replay.ID != run.ID {
		t.Fatal("run replay changed identity", err)
	}
	command.Args = map[string]any{"text": "Other"}
	if _, err := s.StartRun(context.Background(), command); !errors.Is(err, status_error.WorkflowRequestConflict) {
		t.Fatal("same occurrence accepted different input", err)
	}
	saved, _, err := s.SaveSource(context.Background(), SaveSourceCommand{DefinitionID: definition.ID, RequestID: "new-version", ExpectedRevisionID: definition.CurrentRevisionID, Source: strings.Replace(workflowTestSource(t), "parts.join(' ')", "parts.join('-')", 1), SDKVersion: constants.WorkflowSDKVersion})
	if err != nil {
		t.Fatal(err)
	}
	result := runTicks(t, s, run.ID, 8)
	if result.Status != constants.WorkflowRunStatusSucceeded {
		t.Fatalf("run: %+v", result)
	}
	object, ok := result.Result.(map[string]any)
	if !ok || object["message"] != "NEUXBOT NEUXBOT!" || object["length"] != float64(16) {
		t.Fatalf("actual result: %#v", result.Result)
	}
	if result.RevisionID == saved.RevisionID {
		t.Fatal("running program changed after creation saved")
	}
}

const humanSource = `export const meta={sdk:'ally.workflow/1',title:'人工关卡',description:'恢复测试',phases:[{id:'approval',title:'确认',detail:'显式批准',canExit:true}]};
const answer=await askUser('是否批准？',{id:'approve',label:'批准结果',phase:'approval',options:[{value:'approve',label:'批准'},{value:'reject',label:'拒绝'}],defaultValue:'reject',allowSkip:false});
return {approved:answer.source==='user' && answer.value==='approve',source:answer.source};`

// controlledWorker 用真实运行仓储控制智能体接纳与终态，避免依赖外部模型延迟。
type controlledWorker struct {
	// prompts 保存真实派发顺序，检验管道没有全阶段栅栏。
	prompts []string
	// hasDeadline 记录派发或查询是否被宿主附加截止时间。
	hasDeadline bool
	// cancellations 记录实际取消端口的调用次数。
	cancellations int
	// inspections 记录对已派发执行的权威查询次数。
	inspections int
}

func (*controlledWorker) ResolveAdmission(_ context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults, _ bool) (models.WorkflowExecutionDefaults, error) {
	return workerConfiguration(options, defaults), nil
}

func (w *controlledWorker) Dispatch(ctx context.Context, input WorkerDispatch) (WorkerReceipt, error) {
	_, w.hasDeadline = ctx.Deadline()
	prompt, _ := input.Effect.Boundary.Arguments[0].Data.(string)
	w.prompts = append(w.prompts, prompt)
	receipt := WorkerReceipt{ChatID: "chat-" + input.Effect.ID, ExecutionID: input.Effect.ExecutionID, Status: constants.ExecutionStatusCompleted, FinalPresent: true, Text: prompt + "-result"}
	if prompt == "B" {
		receipt.Status = constants.ExecutionStatusRunning
	}
	if prompt == "C" {
		receipt.Status = constants.ExecutionStatusFailed
		receipt.ErrorCode = "ENV_FAILED"
	}
	return receipt, nil
}
func (w *controlledWorker) Inspect(ctx context.Context, effect models.WorkflowEffect) (WorkerReceipt, error) {
	_, w.hasDeadline = ctx.Deadline()
	w.inspections++
	return WorkerReceipt{ChatID: effect.ChatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusRunning}, nil
}
func (w *controlledWorker) Cancel(context.Context, models.WorkflowEffect) error {
	w.cancellations++
	return nil
}

func TestWorkerWithoutNodeDeadlineKeepsRunCancellation(t *testing.T) {
	for _, name := range []string{"no-deadline", "expired-stored-deadline", "run-deadline"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, _ := workflowTestService(t)
			worker := &controlledWorker{}
			s.worker = worker
			source := `export const meta={sdk:'ally.workflow/1',title:'伙伴期限',description:'无独立期限',nodeResultPageOmissions:{task:'只返回期限协议测试的控制值'},phases:[{id:'work',title:'执行',detail:'提交结果',canExit:true}]};return await worker('B',{id:'task',label:'结果',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime'},schema:{type:'string'}});`
			if name == "run-deadline" {
				source = strings.Replace(source, "description:'无独立期限',", "description:'无独立期限',limits:{timeoutMs:2000},", 1)
			}
			definition := savedTestWorkflow(t, s, source)
			run, err := s.StartRun(ctx, StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 2)
			state, err := s.runs.LoadRunState(ctx, run.ID)
			if err != nil || len(state.Effects) != 1 {
				t.Fatalf("worker was not prepared: %v %+v", err, state)
			}
			effect := state.Effects[0]
			if effect.DeadlineAt != 0 {
				t.Fatal("worker admission created a deadline")
			}
			if name == "expired-stored-deadline" {
				if err := s.state.MutateEffect(ctx, run.ID, effect.ID, func(_ context.Context, _ *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
					current.DeadlineAt = 1
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			scope := workflowtool.Scope{Key: effect.ID, Token: effect.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}
			query := workflowtool.Input{Action: constants.WorkflowActionQuery, Type: constants.WorkflowQueryResultSchema}
			if _, err := s.handleTool(ctx, scope, query); err != nil {
				t.Fatal("result schema rejected an active worker", err)
			}
			if _, err := s.handleTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: "verified"}); err != nil {
				t.Fatal("result submission rejected an active worker", err)
			}
			dispatchPrepared(t, s, run.ID)
			effect, err = s.runs.GetEffect(ctx, effect.ID)
			if err != nil || effect.Status != constants.WorkflowStepStatusRunning || worker.hasDeadline || worker.cancellations != 0 {
				t.Fatalf("worker inherited a node timeout: %+v %v %+v", effect, err, worker)
			}
			run, err = s.runs.GetRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			s.dispatchEffect(ctx, run, effect)
			if worker.hasDeadline || worker.cancellations != 0 || worker.inspections != 1 {
				t.Fatal("running worker inspection inherited a node deadline")
			}
			if name == "run-deadline" {
				// 总期限在接纳时冻结，等待真实时钟到期，不通过状态提交伪改不可变预算。
				time.Sleep(time.Until(time.UnixMilli(run.DeadlineAt)))
				if _, err := s.handleTool(ctx, scope, query); !errors.Is(err, status_error.WorkflowStateConflict) {
					t.Fatal("expired run retained result query before runner tick", err)
				}
				if _, err := s.handleTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: "verified"}); !errors.Is(err, status_error.WorkflowStateConflict) {
					t.Fatal("expired run retained result submission before runner tick", err)
				}
				runTicks(t, s, run.ID, 1)
			} else if err := s.CancelRun(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.handleTool(ctx, scope, query); !errors.Is(err, status_error.WorkflowStateConflict) {
				t.Fatal("cancelled run retained result capability", err)
			}
			run, err = s.runs.GetRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			s.dispatchEffect(ctx, run, effect)
			if worker.cancellations != 1 {
				t.Fatal("removing the deadline disabled explicit cancellation")
			}
			expected := constants.WorkflowRunStatusCancelled
			if name == "run-deadline" {
				expected = constants.WorkflowRunStatusFailed
			}
			if result := runTicks(t, s, run.ID, 2); result.Status != expected {
				t.Fatalf("worker cancellation did not finish: %+v", result)
			}
		})
	}
}

func dispatchPrepared(t *testing.T, s *Service, id string) {
	t.Helper()
	state, err := s.runs.LoadRunState(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, effect := range state.Effects {
		if effect.Status == constants.WorkflowStepStatusPrepared && effect.Boundary.Primitive == "worker" {
			s.dispatchEffect(context.Background(), state.Run, effect)
		}
	}
}
func TestPipelineFastItemAdvancesBeforeSlowFirstStageAndCollectsByInput(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/1',title:'逐项管道',description:'快慢失败',nodeResultPageOmissions:{stage1:'只返回管道时序测试的第一阶段标记',stage2:'只返回管道时序测试的第二阶段标记'},phases:[{id:'work',title:'执行',detail:'逐项推进',canExit:true}]};
const results=await pipeline(['A','B','C'],[
 async (previous,original,index)=> await worker(original,{id:'stage1',label:'检查',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime'}}),
 async (previous,original,index)=> await worker(original+'-next',{id:'stage2',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime'}})
],{id:'items',failureMode:'collect-null',concurrency:3});return results;`
	s, _ := workflowTestService(t)
	worker := &controlledWorker{}
	s.worker = worker
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 8; index++ {
		runTicks(t, s, run.ID, 1)
		dispatchPrepared(t, s, run.ID)
	}
	if !strings.Contains(strings.Join(worker.prompts, ","), "A-next") {
		t.Fatalf("A waited behind B: %v", worker.prompts)
	}
	state, err := s.runs.LoadRunState(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Groups) != 1 || !state.Groups[0].Finished[0] || state.Groups[0].Finished[1] || !state.Groups[0].Finished[2] {
		t.Fatalf("pipeline states: %+v", state.Groups)
	}
	for _, effect := range state.Effects {
		if effect.Boundary.Arguments[0].Data == "B" {
			s.finishEffect(context.Background(), effect, WorkerReceipt{ChatID: effect.ChatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusCompleted, FinalPresent: true, Text: "B-result"}, nil)
		}
	}
	for index := 0; index < 8; index++ {
		runTicks(t, s, run.ID, 1)
		dispatchPrepared(t, s, run.ID)
	}
	result := runTicks(t, s, run.ID, 2)
	items, _ := result.Result.([]any)
	if result.Status != constants.WorkflowRunStatusSucceeded || len(items) != 3 || items[0] != "A-next-result" || items[1] != "B-next-result" || items[2] != nil {
		t.Fatalf("ordered result: %+v", result)
	}
}

func TestStructuredCandidateCannotReplaceExecutionSuccessAndCannotBeOverwritten(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/1',title:'结构化',description:'双重成功条件',nodeResultPageOmissions:{task:'只返回结构化提交协议测试的布尔控制值'},phases:[{id:'work',title:'执行',detail:'提交结果',canExit:true}]};return await worker('B',{id:'task',label:'结果',phase:'work',resolve:{kind:'waker',wakerId:'fixture',runtimeId:'fixture-runtime'},schema:{type:'object',required:['ok'],properties:{ok:{type:'boolean'}}}});`
	s, _ := workflowTestService(t)
	s.worker = &controlledWorker{}
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 1)
	dispatchPrepared(t, s, run.ID)
	state, err := s.runs.LoadRunState(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	effect := state.Effects[0]
	scope := workflowtool.Scope{Key: effect.ID, Token: effect.AuthToken, Kind: constants.WorkflowCapabilityScopeResult}
	if _, err = s.handleTool(context.Background(), scope, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: map[string]any{"ok": true}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.handleTool(context.Background(), scope, workflowtool.Input{Action: constants.WorkflowActionResultSubmit, Data: map[string]any{"ok": false}}); !errors.Is(err, status_error.WorkflowRequestConflict) {
		t.Fatal("candidate overwritten", err)
	}
	result := runTicks(t, s, run.ID, 2)
	if result.Status != constants.WorkflowRunStatusWaiting {
		t.Fatal("candidate released before execution success")
	}
	s.finishEffect(context.Background(), effect, WorkerReceipt{ChatID: effect.ChatID, ExecutionID: effect.ExecutionID, Status: constants.ExecutionStatusFailed, ErrorCode: "MODEL_FAILED"}, nil)
	result = runTicks(t, s, run.ID, 3)
	if result.Status != constants.WorkflowRunStatusFailed {
		t.Fatalf("submitted then failed counted as success: %+v", result)
	}
}

// oneTickRunStore 计数真实运行聚合读取，并在第二次扫描前停止受管调度。
type oneTickRunStore struct {
	// WorkflowRunController 保留真实 SQLite 仓储和事务行为。
	controllers.WorkflowRunController
	// loads 记录完整运行聚合加载次数。
	loads int
	// scans 记录业务调度扫描次数。
	scans int
	// cancel 结束本用例的调度生命周期。
	cancel context.CancelFunc
}

func (c *oneTickRunStore) LoadRunState(ctx context.Context, id string) (controllers.WorkflowRunState, error) {
	c.loads++
	return c.WorkflowRunController.LoadRunState(ctx, id)
}
func (c *oneTickRunStore) ListRunnable(ctx context.Context, limit int) ([]models.WorkflowRun, error) {
	c.scans++
	if c.scans > 1 {
		c.cancel()
		return nil, nil
	}
	return c.WorkflowRunController.ListRunnable(ctx, limit)
}
func TestRunLoopLoadsWaitingAggregateOncePerTick(t *testing.T) {
	s, _ := workflowTestService(t)
	definition := savedTestWorkflow(t, s, humanSource)
	run, err := s.StartRun(context.Background(), StartRunCommand{DefinitionID: definition.ID, RequestID: "waiting", Args: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	runTicks(t, s, run.ID, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	store := &oneTickRunStore{WorkflowRunController: s.runs, cancel: cancel}
	owner := s.state.Owner
	s.runs = store
	s.state = workflowstate.New(s.store, store, s.transactions)
	s.state.Owner = owner
	lifecycle := &runnerLifecycle{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	s.runLoop(lifecycle)
	if store.loads != 1 {
		t.Fatalf("waiting tick loaded full aggregate %d times, expected one authoritative load", store.loads)
	}
}
