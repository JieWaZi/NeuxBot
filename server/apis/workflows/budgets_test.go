package workflows

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// holdingWorker 控制真实业务派发边界的外部等待，不替代 SQLite Runner 或并发接纳。
type holdingWorker struct {
	// mutex 保护来自多个 Run 的实际同时派发观察值。
	mutex sync.Mutex
	// active 是尚未结束的外部调用数量。
	active int
	// maximum 是测试期间观测到的最大同时调用数量。
	maximum int
	// changed 通知等待方真实调用数发生改变。
	changed chan struct{}
}

func (*holdingWorker) ResolveAdmission(_ context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults, _ bool) (models.WorkflowExecutionDefaults, error) {
	return defaults, nil
}

func (w *holdingWorker) Dispatch(ctx context.Context, input workflow.WorkerDispatch) (workflow.WorkerReceipt, error) {
	w.mutex.Lock()
	w.active++
	w.maximum = max(w.maximum, w.active)
	w.mutex.Unlock()
	select {
	case w.changed <- struct{}{}:
	default:
	}
	defer func() {
		w.mutex.Lock()
		w.active--
		w.mutex.Unlock()
		select {
		case w.changed <- struct{}{}:
		default:
		}
	}()
	<-ctx.Done()
	return workflow.WorkerReceipt{ExecutionID: input.Effect.ExecutionID, Status: constants.ExecutionStatusCanceled}, status_error.WorkflowCancelled
}
func (w *holdingWorker) Inspect(context.Context, models.WorkflowEffect) (workflow.WorkerReceipt, error) {
	return workflow.WorkerReceipt{Status: constants.ExecutionStatusRunning}, nil
}
func (w *holdingWorker) Cancel(context.Context, models.WorkflowEffect) error { return nil }

func waitActiveWorkers(t *testing.T, worker *holdingWorker, count int) {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	for {
		worker.mutex.Lock()
		active := worker.active
		worker.mutex.Unlock()
		if active == count {
			return
		}
		select {
		case <-worker.changed:
		case <-deadline.C:
			t.Fatalf("active calls %d, expected %d", active, count)
		}
	}
}

func holdingSource(concurrency int) string {
	limit := "1"
	if concurrency == 8 {
		limit = "8"
	}
	return `export const meta={sdk:'ally.workflow/1',title:'并发预算',description:'真实派发边界',nodeResultPageOmissions:{held:'只返回并发预算测试的时序标记'},limits:{concurrency:` + limit + `},phases:[{id:'work',title:'工作',detail:'限制同时派发',canExit:true}]};const values=await parallel(args.items.map(item=>async()=>await worker(String(item),{id:'held',label:'受控等待',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}})),{id:'batch',concurrency:8});return values;`
}

func TestDeclaredRunConcurrencyAndGlobalSlotsLimitActualDispatch(t *testing.T) {
	for _, concurrency := range []int{1, 8} {
		t.Run(map[int]string{1: "run-limit", 8: "global-limit"}[concurrency], func(t *testing.T) {
			s, _ := workflowTestService(t)
			worker := &holdingWorker{changed: make(chan struct{}, 64)}
			s.Worker = worker
			s.Service = workflow.New(s.Config)
			definition := savedTestWorkflow(t, s, holdingSource(concurrency))
			items := []any{1, 2, 3, 4, 5, 6, 7, 8}
			runCount, expected := 1, 1
			if concurrency == 8 {
				runCount, expected = 3, constants.WorkflowGlobalConcurrency
			}
			for index := 0; index < runCount; index++ {
				if _, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: strings.Repeat("r", index+1), Args: map[string]any{"items": items}, Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			waitActiveWorkers(t, worker, expected)
			// 额外调度周期仍须保持调用槽上限，不能把 Inspect goroutine 数误当 Runtime 调用数。
			time.Sleep(350 * time.Millisecond)
			worker.mutex.Lock()
			active, maximum := worker.active, worker.maximum
			worker.mutex.Unlock()
			if active != expected || maximum != expected {
				t.Fatalf("actual dispatch exceeded budget: active=%d maximum=%d expected=%d", active, maximum, expected)
			}
		})
	}
}

func TestLowerLoopAndInstanceBudgetsBlockFurtherEffects(t *testing.T) {
	for _, test := range []struct {
		// name 标识被验证的独立预算。
		name string
		// source 是提交给实际编译器和 SQLite Runner 的源程序。
		source string
	}{
		{"loop-limit", `export const meta={sdk:'ally.workflow/1',title:'循环预算',description:'限制循环',limits:{maxLoopSteps:2},phases:[{id:'one',title:'one',detail:'one',canExit:true}]};let n=0;for(let i=0;i<3;i++){n++;}return n;`},
		{"instance-limit", `export const meta={sdk:'ally.workflow/1',title:'实例预算',description:'限制实例',limits:{maxInstances:1},phases:[{id:'one',title:'one',detail:'one',canExit:true}]};for(let i=0;i<2;i++){await askUser('输入',{id:'answer',label:'输入',phase:'one'});}return true;`},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := workflowTestService(t)
			definition := savedTestWorkflow(t, s, test.source)
			run, err := s.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start"})
			if err != nil {
				t.Fatal(err)
			}
			runTicks(t, s, run.ID, 3)
			state, err := s.Runs.LoadRunState(context.Background(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Humans) > 0 {
				if err := answerHuman(context.Background(), workflowAPIDependencies(s), AnswerHumanCommand{RunID: run.ID, HumanID: state.Humans[0].ID, RequestID: "answer", Value: "continue"}); err != nil {
					t.Fatal(err)
				}
			}
			result := runTicks(t, s, run.ID, 5)
			if result.Status != constants.WorkflowRunStatusFailed {
				t.Fatalf("lower declared budget ignored: %+v", result)
			}
			state, _ = s.Runs.LoadRunState(context.Background(), run.ID)
			if len(state.Humans) > 1 {
				t.Fatal("instance budget created a second human request")
			}
			if len(state.Humans) == 1 {
				instance := state.Instances[0]
				if instance.Status != constants.WorkflowStepStatusSucceeded || instance.Output == nil || instance.Failure != nil || instance.CompletedAt == nil {
					t.Fatalf("completed first step lost its receipt after budget failure: %+v", instance)
				}
				failure, _ := state.Run.Failure.JSON()
				if failure.(map[string]any)["budget"] != "instances" {
					t.Fatalf("instance budget failure omitted its cause: %#v", failure)
				}
			}
		})
	}
}
