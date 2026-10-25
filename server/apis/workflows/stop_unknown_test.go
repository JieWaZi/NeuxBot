package workflows

import (
	"context"
	"sync"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
)

// uncertainWorker 模拟已接纳但派发响应丢失，停止后提供真实终态的外部边界。
type uncertainWorker struct {
	// mutex 保护后台 Runner 与 API 命令共享的执行观察事实。
	mutex sync.Mutex
	// inspectUnavailable 控制暂时无法确认实际执行的窗口。
	inspectUnavailable bool
	// stopped 记录是否已经实际等待停止。
	stopped bool
	// dispatches 用于确认未知结果没有重发。
	dispatches int
	// missingExecution 模拟接纳被拒绝，权威对话从未创建。
	missingExecution bool
	// dispatchRejected 模拟启动校验被明确拒绝但首次查询不可用。
	dispatchRejected bool
}

func (*uncertainWorker) ResolveAdmission(_ context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults, _ bool) (models.WorkflowExecutionDefaults, error) {
	return defaults, nil
}

func (w *uncertainWorker) Dispatch(context.Context, workflow.WorkerDispatch) (workflow.WorkerReceipt, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.dispatches++
	if w.missingExecution || w.dispatchRejected {
		return workflow.WorkerReceipt{}, status_error.BadRequest
	}
	return workflow.WorkerReceipt{}, status_error.WorkflowEffectFailed
}

func (w *uncertainWorker) Inspect(_ context.Context, effect models.WorkflowEffect) (workflow.WorkerReceipt, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.missingExecution {
		return workflow.WorkerReceipt{}, status_error.ChatNotFound.WithParams(map[string]interface{}{"ID": "missing-chat"})
	}
	if w.inspectUnavailable {
		return workflow.WorkerReceipt{}, status_error.WorkflowEffectFailed
	}
	status := constants.ExecutionStatusRunning
	if w.stopped {
		status = constants.ExecutionStatusCanceled
	}
	return workflow.WorkerReceipt{ChatID: "actual-chat", ExecutionID: effect.ExecutionID, Status: status}, nil
}

func TestRejectedWorkerWithParameterizedMissingChatFailsWithoutReconciliation(t *testing.T) {
	service, _ := workflowTestService(t)
	worker := &uncertainWorker{missingExecution: true}
	service.Worker = worker
	service.Service = workflow.New(service.Config)
	definition := savedTestWorkflow(t, service, `export const meta={sdk:'ally.workflow/1',title:'启动拒绝',description:'没有外部效果',nodeResultPageOmissions:{worker:'只返回启动拒绝测试的控制值'},phases:[{id:'work',title:'执行',detail:'检查失败',canExit:true}]};return await worker('整理',{id:'worker',label:'整理',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});`)
	run, err := service.StartRun(context.Background(), workflow.StartRunCommand{
		DefinitionID: definition.ID, RequestID: "start",
		Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	state := waitWorkflow(t, service, run.ID, func(state controllers.WorkflowRunState) bool {
		return workflowstate.TerminalRun(state.Run.Status)
	})
	if state.Run.Status != constants.WorkflowRunStatusFailed || state.Effects[0].Status != constants.WorkflowStepStatusFailed {
		t.Fatalf("rejected admission asked user to reconcile: %s / %s", state.Run.Status, state.Effects[0].Status)
	}
}
func (w *uncertainWorker) Cancel(context.Context, models.WorkflowEffect) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.stopped = true
	return nil
}

func (w *uncertainWorker) allowInspection() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.inspectUnavailable = false
}

func (w *uncertainWorker) facts() (bool, int) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.stopped, w.dispatches
}

func TestLostWorkerReceiptFailsWithoutUserReconciliationOrResend(t *testing.T) {
	service, _ := workflowTestService(t)
	worker := &uncertainWorker{inspectUnavailable: true}
	service.Worker = worker
	service.Service = workflow.New(service.Config)
	definition := savedTestWorkflow(t, service, `export const meta={sdk:'ally.workflow/1',title:'未知执行',description:'失败后优化',nodeResultPageOmissions:{worker:'只返回未知执行恢复测试的控制值'},phases:[{id:'work',title:'执行',detail:'执行',canExit:true}]};return await worker('work',{id:'worker',label:'执行',phase:'work',resolve:{kind:'waker',wakerId:'fixture'},schema:{type:'string'}});`)
	ctx := context.Background()
	run, err := service.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	state := waitWorkflow(t, service, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusFailed && !service.State.EffectActive(state.Effects[0].ID)
	})
	effect := state.Effects[0]
	if effect.Status != constants.WorkflowStepStatusUnknown || state.Instances[0].Status != constants.WorkflowStepStatusFailed || state.Run.Failure == nil || state.Run.CompletedAt == nil {
		t.Fatal("uncertainty was not retained as failure evidence", state)
	}
	bindings := service.State.AgentBindings(effect.ID, effect.AuthToken, constants.WorkflowCapabilityScopeResult)
	var callErr error
	for _, binding := range bindings {
		if binding.Operation == "workflow.results.submit" {
			_, callErr = binding.Call(ctx, agentcapability.Request{Operation: binding.Operation, RequestID: "late", Params: map[string]any{"data": "late"}})
		}
	}
	if workflowstate.ErrorKey(callErr) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("late result changed failed run", callErr)
	}

	if _, err := retryRun(ctx, workflowAPIDependencies(service), RetryRunCommand{RunID: run.ID, RequestID: "unsafe", ExpectedVersion: state.Run.Version}); workflowstate.ErrorKey(err) != status_error.WorkflowStateConflict.Key() {
		t.Fatal("uncertain external operation was blindly retried", err)
	}
	_, dispatches := worker.facts()
	if dispatches != 1 {
		t.Fatal("worker resent", dispatches)
	}
}

func TestLostDispatchResponseReconcilesAlreadyAcceptedExecution(t *testing.T) {
	service, _ := workflowTestService(t)
	worker := &uncertainWorker{}
	service.Worker = worker
	service.Service = workflow.New(service.Config)
	definition := savedTestWorkflow(t, service, `export const meta={sdk:'ally.workflow/1',title:'对账',description:'绑定原身份',nodeResultPageOmissions:{worker:'只返回执行身份对账测试的控制值'},phases:[{id:'work',title:'执行',detail:'恢复',canExit:true}]};return await worker('work',{id:'worker',label:'执行',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}});`)
	run, err := service.StartRun(context.Background(), workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "start", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	state := waitWorkflow(t, service, run.ID, func(state controllers.WorkflowRunState) bool {
		return len(state.Effects) == 1 && len(state.Instances) == 1 && state.Effects[0].Status == constants.WorkflowStepStatusRunning && state.Instances[0].ChatID == "actual-chat"
	})
	_, dispatches := worker.facts()
	if state.Effects[0].Status != constants.WorkflowStepStatusRunning || state.Instances[0].ChatID != "actual-chat" || dispatches != 1 {
		t.Fatal("accepted execution not recovered", state.Effects[0].Status)
	}
}

// failingParallelWorker 模拟一个步骤丢失结果，另一个真实派发仍在执行。
type failingParallelWorker struct {
	// holding 保存并行在途执行的计数与取消等待。
	holding *holdingWorker
	// uncertain 模拟无法确认结果的外部边界。
	uncertain *uncertainWorker
}

func (w *failingParallelWorker) ResolveAdmission(ctx context.Context, options map[string]any, defaults models.WorkflowExecutionDefaults, resolved bool) (models.WorkflowExecutionDefaults, error) {
	return w.holding.ResolveAdmission(ctx, options, defaults, resolved)
}
func (w *failingParallelWorker) Dispatch(ctx context.Context, input workflow.WorkerDispatch) (workflow.WorkerReceipt, error) {
	if input.Effect.Boundary.NodeID == "broken" {
		select {
		case <-w.holding.changed:
		case <-ctx.Done():
			return workflow.WorkerReceipt{}, ctx.Err()
		}
		return w.uncertain.Dispatch(ctx, input)
	}
	return w.holding.Dispatch(ctx, input)
}
func (w *failingParallelWorker) Inspect(ctx context.Context, effect models.WorkflowEffect) (workflow.WorkerReceipt, error) {
	if effect.Boundary.NodeID == "broken" {
		return w.uncertain.Inspect(ctx, effect)
	}
	return w.holding.Inspect(ctx, effect)
}
func (w *failingParallelWorker) Cancel(ctx context.Context, effect models.WorkflowEffect) error {
	if effect.Boundary.NodeID == "broken" {
		return w.uncertain.Cancel(ctx, effect)
	}
	return w.holding.Cancel(ctx, effect)
}

func TestUncertainResultAutomaticallyCancelsParallelWorker(t *testing.T) {
	ctx := context.Background()
	s, _ := workflowTestService(t)
	worker := &failingParallelWorker{holding: &holdingWorker{changed: make(chan struct{}, 8)}, uncertain: &uncertainWorker{inspectUnavailable: true}}
	s.Worker = worker
	s.Service = workflow.New(s.Config)
	source := `export const meta={sdk:'ally.workflow/1',title:'并行失败',description:'自动停止其他步骤',nodeResultPageOmissions:{held:'只返回在途步骤停止测试的控制值',broken:'只模拟断联失败，不交付可读结果'},phases:[{id:'work',title:'执行',detail:'并行',canExit:true}]};return await parallel([async()=>await worker('hold',{id:'held',label:'在途',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}}),async()=>await worker('fail',{id:'broken',label:'断联',phase:'work',resolve:{kind:'waker',wakerId:'fixture'}})],{id:'both',concurrency:2});`
	definition := savedTestWorkflow(t, s, source)
	run, err := s.StartRun(ctx, workflow.StartRunCommand{DefinitionID: definition.ID, RequestID: "parallel", Defaults: models.WorkflowExecutionDefaults{RuntimeID: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	state := waitWorkflow(t, s, run.ID, func(state controllers.WorkflowRunState) bool {
		return state.Run.Status == constants.WorkflowRunStatusFailed
	})
	worker.holding.mutex.Lock()
	active := worker.holding.active
	worker.holding.mutex.Unlock()
	if active != 0 || state.Run.CompletedAt == nil {
		t.Fatalf("failure left parallel worker active: %d %+v", active, state.Run)
	}
	for _, instance := range state.Instances {
		if instance.CompletedAt == nil {
			t.Fatal("peer did not converge", instance)
		}
	}
}
