package execution

import (
	"context"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"

	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

// activeExecution 保存进程内对话执行的取消入口和退出通知。
type activeExecution struct {
	// executionID 是当前占用对话的执行标识。
	executionID string
	// cancel 结束 Runtime 运行使用的 Context。
	cancel context.CancelFunc
	// done 在执行完成日志和查询索引处理结束后关闭。
	done chan struct{}
}

// executionSettlement 记录同一后台所有者的文件冻结与最终退出屏障，不复制业务状态。
type executionSettlement struct {
	// frozen 在结束侧冻结或明确放弃采集后关闭；nil 表示已关闭。
	frozen chan struct{}
	// done 在差异计算、发布和后台资源清理全部退出后关闭。
	done chan struct{}
}

// executionRegistry 统一维护 Chat 执行的进程生命周期和单执行约束。
//
// 注册表只管理进程内占用关系，不替代 JSONL 中的权威执行状态。调用方必须先取得
// reservation，再提交消息和执行事实；提交失败时释放 reservation，提交成功后由
// launch 保证后台流程退出时释放。
type executionRegistry struct {
	// mutex 保护生命周期状态和活跃执行集合。
	mutex sync.Mutex
	// active 按对话标识保存当前唯一活跃执行。
	active map[string]activeExecution
	// settling 保存已接纳后台工作的技术屏障，冻结后允许下一轮读取基线和写盘。
	settling map[string]map[string]*executionSettlement
	// lifecycleContext 控制全部后台执行。
	lifecycleContext context.Context
	// lifecycleCancel 取消全部后台执行。
	lifecycleCancel context.CancelFunc
	// waitGroup 等待预留、原生执行与终态之后的文件收尾全部退出。
	waitGroup sync.WaitGroup
	// started 表示注册表已经绑定进程根 Context。
	started bool
}

// newExecutionRegistry 创建尚未绑定进程生命周期的执行注册表。
func newExecutionRegistry() *executionRegistry {
	return &executionRegistry{active: make(map[string]activeExecution), settling: make(map[string]map[string]*executionSettlement)}
}

// start 绑定进程根 Context；重复调用保持幂等。
func (r *executionRegistry) start(ctx context.Context) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.started {
		return
	}
	r.lifecycleContext, r.lifecycleCancel = context.WithCancel(ctx)
	r.started = true
}

// close 取消全部执行并等待后台流程完成。
func (r *executionRegistry) close() {
	r.mutex.Lock()
	if !r.started {
		r.mutex.Unlock()
		return
	}
	cancel := r.lifecycleCancel
	r.started = false
	r.mutex.Unlock()

	cancel()
	r.waitGroup.Wait()
}

// reserve 在写入任何权威事实前占用 Chat 的唯一执行位置。
func (r *executionRegistry) reserve(
	chatID string,
	executionID string,
) (context.Context, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !r.started {
		return nil, status_error.ChatServiceNotStarted
	}
	if _, exists := r.active[chatID]; exists {
		return nil, status_error.ChatHasActiveExecution
	}
	executionContext, cancel := context.WithCancel(r.lifecycleContext)
	// 从预留成功开始计入生命周期，而不是等到 goroutine 启动后再 Add。这样 Close
	// 与发送准备或权威提交并发时，不会在 Wait 已返回后才出现新的后台执行。
	r.waitGroup.Add(1)
	if r.settling[chatID] == nil {
		r.settling[chatID] = make(map[string]*executionSettlement)
	}
	r.settling[chatID][executionID] = &executionSettlement{frozen: make(chan struct{}), done: make(chan struct{})}
	r.active[chatID] = activeExecution{
		executionID: executionID,
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	return executionContext, nil
}

// launch 启动已经提交权威事实的执行，并保证退出时释放占用。
func (r *executionRegistry) launch(
	chatID string,
	executionID string,
	executionContext context.Context,
	run func(context.Context),
) {
	go func() {
		defer r.release(chatID, executionID)
		run(executionContext)
	}()
}

// release 释放指定后台工作及其收尾所有权，旧流程不能删除新执行占用。
func (r *executionRegistry) release(chatID string, executionID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if active, exists := r.active[chatID]; exists && active.executionID == executionID {
		delete(r.active, chatID)
		close(active.done)
	}
	if settlement, exists := r.settling[chatID][executionID]; exists {
		delete(r.settling[chatID], executionID)
		if len(r.settling[chatID]) == 0 {
			delete(r.settling, chatID)
		}
		if settlement.frozen != nil {
			close(settlement.frozen)
		}
		close(settlement.done)
		r.waitGroup.Done()
	}
}

// waitForFileSnapshots 只等待前序后台所有者冻结结束侧，不等待差异计算或成果发布。
func (r *executionRegistry) waitForFileSnapshots(ctx, executionContext context.Context, chatID, executionID string) error {
	r.mutex.Lock()
	pending := make([]<-chan struct{}, 0, len(r.settling[chatID]))
	for id, settlement := range r.settling[chatID] {
		if id != executionID && settlement.frozen != nil {
			pending = append(pending, settlement.frozen)
		}
	}
	r.mutex.Unlock()
	for _, frozen := range pending {
		select {
		case <-frozen:
		case <-ctx.Done():
			return ctx.Err()
		case <-executionContext.Done():
			return executionContext.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return executionContext.Err()
}

// freezeFileSnapshots 解除同一所有者的工作树屏障，后续收尾只能消费不可变快照。
func (r *executionRegistry) freezeFileSnapshots(chatID, executionID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if settlement := r.settling[chatID][executionID]; settlement != nil && settlement.frozen != nil {
		close(settlement.frozen)
		settlement.frozen = nil
	}
}

// complete 在同一临界区内释放执行占用并发布终态，避免订阅者先于下一轮入口观察到完成。
func (r *executionRegistry) complete(
	chatID string,
	executionID string,
	publish func(),
) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	active, exists := r.active[chatID]
	matched := exists && active.executionID == executionID
	if matched {
		delete(r.active, chatID)
	}
	if publish != nil {
		publish()
	}
	if matched {
		close(active.done)
	}
}

// cancel 取消 Chat 当前活跃执行；没有活跃执行时保持幂等。
func (r *executionRegistry) cancel(chatID string) {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	r.mutex.Unlock()
	if exists && active.cancel != nil {
		active.cancel()
	}
}

// cancelAndWait 取消活跃执行，并等待该 Chat 已发布终态的文件收尾退出。
func (r *executionRegistry) cancelAndWait(ctx context.Context, chatID string) error {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	pending := make([]<-chan struct{}, 0, len(r.settling[chatID]))
	for _, settlement := range r.settling[chatID] {
		pending = append(pending, settlement.done)
	}
	r.mutex.Unlock()
	if exists && active.cancel != nil {
		active.cancel()
	}
	for _, done := range pending {
		select {
		case <-done:
		case <-ctx.Done():
			return status_error.ChatHasActiveExecution
		}
	}
	return nil
}

// cancelExecutionAndWait 捕获精确执行并等待其文件收尾，旧请求不能取消新工作。
func (r *executionRegistry) cancelExecutionAndWait(ctx context.Context, chatID, executionID string) error {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	var done <-chan struct{}
	if settlement := r.settling[chatID][executionID]; settlement != nil {
		done = settlement.done
	}
	r.mutex.Unlock()
	if exists && active.executionID == executionID && active.cancel != nil {
		active.cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return status_error.ChatHasActiveExecution
		}
	}
	return nil
}

// executionStartResult is the sending caller’s durable admission receipt.
type executionStartResult struct {
	// chat 是启动成功后已进入权威日志的对话索引。
	chat models.Chat
	// execution 是启动成功后已进入 running 状态的执行索引。
	execution models.Execution
	// err reports failure before the admission could be projected.
	err error
}

// finishExecution 先提交执行终态，再以尽力而为方式追平 SQLite 查询索引。
func (m *service) finishExecution(
	chat models.Chat,
	execution models.Execution,
	finished eventlog.ExecutionFinishedData,
) {
	eventType := constants.ChatEventTypeExecutionCompleted
	if finished.Status == constants.ExecutionStatusFailed {
		eventType = constants.ChatEventTypeExecutionFailed
	} else if finished.Status == constants.ExecutionStatusCanceled {
		eventType = constants.ChatEventTypeExecutionCanceled
	}
	terminalData := marshalData(finished)
	commit, err := m.journal.CommitTerminal(context.Background(), chatservice.TerminalFact{
		Execution: execution,
		Type:      eventType,
		Data:      terminalData,
		Status:    finished.Status,
		ErrorCode: finished.ErrorCode,
	})
	if err != nil {
		logx.Errorf("commit execution terminal: chat_id=%s execution_id=%s error=%v", chat.ID, execution.ID, err)
		return
	}
	m.executions.complete(chat.ID, execution.ID, func() {
		m.journal.PublishTerminal(commit)
	})
}
