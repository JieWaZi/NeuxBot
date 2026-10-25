package workflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
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
	return workerConfiguration(options, defaults), nil
}

func (w *holdingWorker) Dispatch(ctx context.Context, input WorkerDispatch) (WorkerReceipt, error) {
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
	return WorkerReceipt{ExecutionID: input.Effect.ExecutionID, Status: constants.ExecutionStatusCanceled}, status_error.WorkflowCancelled
}

func (w *holdingWorker) Inspect(context.Context, models.WorkflowEffect) (WorkerReceipt, error) {
	return WorkerReceipt{Status: constants.ExecutionStatusRunning}, nil
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
