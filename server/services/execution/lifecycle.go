package execution

import (
	"context"
	"sync"
	"time"

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
	// submissionID 允许界面在收到执行回执前精确取消原提交。
	submissionID string
	// cancel 结束 Runtime 运行使用的 Context。
	cancel context.CancelFunc
}

// executionSettlement 记录同一后台所有者的文件冻结与最终退出屏障，不复制业务状态。
type executionSettlement struct {
	// cancel 在后台工作退出后释放执行上下文。
	cancel context.CancelFunc
	// frozen 在结束侧冻结或明确放弃采集后关闭；nil 表示已关闭。
	frozen chan struct{}
	// done 在差异计算、发布和后台资源清理全部退出后关闭。
	done chan struct{}
}

type submissionStopKey struct {
	// chatID 绑定准备中的输入归属。
	chatID string
	// submissionID 与尚未写入 Chat 的请求身份一致。
	submissionID string
}

// submissionStopLifetime 覆盖在途发送与停止的传输乱序，过期后释放未到达的请求。
const submissionStopLifetime = time.Minute

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
	// pendingStops 暂存比对应占位先到的停止，只在本进程调用窗口内有效。
	pendingStops map[submissionStopKey]*time.Timer
	// lifecycleContext 控制全部后台执行。
	lifecycleContext context.Context
	// lifecycleCancel 取消全部后台执行。
	lifecycleCancel context.CancelFunc
	// waitGroup 等待预留、原生执行与终态之后的文件收尾全部退出。
	waitGroup sync.WaitGroup
	// started 表示注册表已经绑定进程根 Context。
	started bool
	// cancelAssociated 撤销由当前执行接纳的工作流，内部清理与关闭不调用。
	cancelAssociated func(context.Context, string, string) error
}

// newExecutionRegistry 创建尚未绑定进程生命周期的执行注册表。
func newExecutionRegistry() *executionRegistry {
	return &executionRegistry{
		active: make(map[string]activeExecution), settling: make(map[string]map[string]*executionSettlement),
		pendingStops: make(map[submissionStopKey]*time.Timer),
	}
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
	for _, timer := range r.pendingStops {
		timer.Stop()
	}
	clear(r.pendingStops)
	r.mutex.Unlock()

	cancel()
	r.waitGroup.Wait()
}

// reserve 在写入任何权威事实前占用 Chat 的唯一执行位置。
func (r *executionRegistry) reserve(
	chatID string,
	executionID string,
	submissionID string,
) (context.Context, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !r.started {
		return nil, status_error.ChatServiceNotStarted
	}
	key := submissionStopKey{chatID: chatID, submissionID: submissionID}
	if r.pendingStops[key] != nil {
		return nil, status_error.OperationCanceled
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
	r.settling[chatID][executionID] = &executionSettlement{cancel: cancel, frozen: make(chan struct{}), done: make(chan struct{})}
	r.active[chatID] = activeExecution{
		executionID:  executionID,
		submissionID: submissionID,
		cancel:       cancel,
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
	}
	if settlement, exists := r.settling[chatID][executionID]; exists {
		settlement.cancel()
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

// IsExecutionActive 检查指定执行是否仍持有会话占位。
func (m *service) IsExecutionActive(chatID, executionID string) bool {
	r := m.executions
	r.mutex.Lock()
	defer r.mutex.Unlock()
	active, exists := r.active[chatID]
	return exists && active.executionID == executionID
}

// OccupiedChatIDs 读取现有占用与文件收尾登记，不影响正在执行的工作。
func (m *service) OccupiedChatIDs() []string {
	r := m.executions
	r.mutex.Lock()
	defer r.mutex.Unlock()
	ids := make([]string, 0, len(r.settling))
	for id := range r.settling {
		ids = append(ids, id)
	}
	return ids
}

// complete 在同一临界区内释放执行占用并发布终态，避免订阅者先于下一轮入口观察到完成。
func (r *executionRegistry) complete(
	chatID string,
	executionID string,
	publish func(),
) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if active, exists := r.active[chatID]; exists && active.executionID == executionID {
		delete(r.active, chatID)
	}
	if publish != nil {
		publish()
	}
}

// cancel 取消 Chat 当前活跃执行；没有活跃执行时保持幂等。
func (r *executionRegistry) cancel(chatID string) string {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	r.mutex.Unlock()
	if exists {
		active.cancel()
	}
	return active.executionID
}

// stopSubmission 在占位锁内记录尚未到达的停止，或返回本次占位的精确执行。
func (r *executionRegistry) stopSubmission(chatID, submissionID string) string {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	active := r.active[chatID]
	key := submissionStopKey{chatID: chatID, submissionID: submissionID}
	if r.started && r.pendingStops[key] == nil {
		var expiry *time.Timer
		expiry = time.AfterFunc(submissionStopLifetime, func() {
			r.mutex.Lock()
			defer r.mutex.Unlock()
			if r.pendingStops[key] == expiry {
				delete(r.pendingStops, key)
			}
		})
		r.pendingStops[key] = expiry
	}
	if active.submissionID == submissionID {
		return active.executionID
	}
	return ""
}

// submissionStopped 保留原提交停止事实，供持久队列在接纳边界核对。
func (r *executionRegistry) submissionStopped(chatID, submissionID string) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.pendingStops[submissionStopKey{chatID: chatID, submissionID: submissionID}] != nil
}

// cancelNativeExecution 取消指定执行，即使 Chat 已由其他执行接管。
func (r *executionRegistry) cancelNativeExecution(chatID, executionID string) {
	r.mutex.Lock()
	settlement := r.settling[chatID][executionID]
	r.mutex.Unlock()
	if settlement != nil {
		settlement.cancel()
	}
}

// cancelAndWait 取消活跃执行，并等待该 Chat 的所有现有执行收尾退出。
func (r *executionRegistry) cancelAndWait(ctx context.Context, chatID string) error {
	active, pending := r.stopNative([]string{chatID})
	var associatedErr error
	if len(active) != 0 {
		associatedErr = r.cancelAssociatedWork(ctx, chatID, active[0].executionID)
	}
	if err := waitForExecutionSettlements(ctx, pending); err != nil {
		return err
	}
	return associatedErr
}

// stopNative 捕获并取消已占位的执行，等待只使用本次捕获的收尾屏障。
func (r *executionRegistry) stopNative(chatIDs []string) ([]activeExecution, []<-chan struct{}) {
	r.mutex.Lock()
	var active []activeExecution
	var pending []<-chan struct{}
	for _, chatID := range chatIDs {
		if execution, exists := r.active[chatID]; exists {
			active = append(active, execution)
		}
		for _, settlement := range r.settling[chatID] {
			pending = append(pending, settlement.done)
		}
	}
	r.mutex.Unlock()
	for _, execution := range active {
		execution.cancel()
	}
	return active, pending
}

func waitForExecutionSettlements(ctx context.Context, pending []<-chan struct{}) error {
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
func (r *executionRegistry) cancelExecutionAndWait(ctx context.Context, chatID, executionID string, withAssociatedWork bool) error {
	r.mutex.Lock()
	active, exists := r.active[chatID]
	var done <-chan struct{}
	if settlement := r.settling[chatID][executionID]; settlement != nil {
		done = settlement.done
	}
	r.mutex.Unlock()
	if exists && active.executionID == executionID {
		active.cancel()
	}
	var associatedErr error
	if withAssociatedWork {
		associatedErr = r.cancelAssociatedWork(ctx, chatID, executionID)
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return status_error.ChatHasActiveExecution
		}
	}
	return associatedErr
}

func (r *executionRegistry) cancelAssociatedWork(ctx context.Context, chatID, executionID string) error {
	if r.cancelAssociated == nil || executionID == "" {
		return nil
	}
	return r.cancelAssociated(ctx, chatID, executionID)
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
