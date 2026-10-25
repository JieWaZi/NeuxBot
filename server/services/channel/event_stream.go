package channel

import (
	"context"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	committedevents "github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
)

const (
	channelEventWakeupBufferSize = 1
	channelEventOutputBufferSize = 64
)

// EventSubscription 衔接 Team 持久事件历史与后续进程内实时唤醒。
type EventSubscription struct {
	// Backlog 是建立订阅时游标之后已经提交的持久事件。
	Backlog []models.ChannelEvent
	// Events 按数据库游标顺序交付订阅建立后提交的持久事件。
	Events <-chan models.ChannelEvent
	// Close 注销当前订阅；重复调用保持幂等。
	Close func()
}

// channelEventBrokerThread 保存一个 Team 对话的订阅者和共享交互投影 watcher。
type channelEventBrokerThread struct {
	// subscribers 是等待当前 Team 新持久事件的进程内订阅者。
	subscribers map[uint64]chan struct{}
	// projectionCancel 停止该 Team 唯一的成员 Chat 交互投影 watcher。
	projectionCancel context.CancelFunc
}

// channelEventBroker 只发布“已有新提交”的合并唤醒，权威事件始终从数据库游标读取。
type channelEventBroker struct {
	// mutex 保护 Team 订阅组和递增订阅标识。
	mutex sync.Mutex
	// threads 按 Team 对话标识保存当前进程内订阅组。
	threads map[string]*channelEventBrokerThread
	// nextSubscriberID 为进程内订阅生成递增标识。
	nextSubscriberID uint64
}

// newChannelEventBroker 创建空的 Team 持久事件唤醒中心。
func newChannelEventBroker() *channelEventBroker {
	return &channelEventBroker{threads: make(map[string]*channelEventBrokerThread)}
}

// add 在读取持久 backlog 前注册订阅者，并指出是否需要启动共享交互投影。
func (b *channelEventBroker) add(
	threadID string,
) (uint64, <-chan struct{}, *channelEventBrokerThread, bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	group, exists := b.threads[threadID]
	if !exists {
		group = &channelEventBrokerThread{subscribers: make(map[uint64]chan struct{})}
		b.threads[threadID] = group
	}
	b.nextSubscriberID++
	subscriberID := b.nextSubscriberID
	wakeups := make(chan struct{}, channelEventWakeupBufferSize)
	group.subscribers[subscriberID] = wakeups
	return subscriberID, wakeups, group, !exists
}

// attachProjection 把共享交互投影的取消函数绑定到仍然有效的订阅组。
func (b *channelEventBroker) attachProjection(
	threadID string,
	group *channelEventBrokerThread,
	cancel context.CancelFunc,
) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.threads[threadID] != group {
		return false
	}
	group.projectionCancel = cancel
	return true
}

// publish 合并通知当前 Team 的全部订阅者重新读取数据库游标。
func (b *channelEventBroker) publish(threadID string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	group := b.threads[threadID]
	if group == nil {
		return
	}
	for _, wakeups := range group.subscribers {
		select {
		case wakeups <- struct{}{}:
		default:
			// 已有未消费唤醒即可覆盖后续事件；消费者每次都会把数据库窗口完整排空。
		}
	}
}

// remove 注销订阅者，并在最后一个订阅者离开时停止共享交互投影。
func (b *channelEventBroker) remove(threadID string, subscriberID uint64) {
	var projectionCancel context.CancelFunc
	b.mutex.Lock()
	group := b.threads[threadID]
	if group != nil {
		if wakeups, exists := group.subscribers[subscriberID]; exists {
			close(wakeups)
			delete(group.subscribers, subscriberID)
		}
		if len(group.subscribers) == 0 {
			delete(b.threads, threadID)
			projectionCancel = group.projectionCancel
		}
	}
	b.mutex.Unlock()
	if projectionCancel != nil {
		projectionCancel()
	}
}

// closeAll 关闭全部实时订阅，并停止所有共享交互投影；持久事件不受影响。
func (b *channelEventBroker) closeAll() {
	b.mutex.Lock()
	cancels := make([]context.CancelFunc, 0, len(b.threads))
	for threadID, group := range b.threads {
		for subscriberID, wakeups := range group.subscribers {
			close(wakeups)
			delete(group.subscribers, subscriberID)
		}
		if group.projectionCancel != nil {
			cancels = append(cancels, group.projectionCancel)
		}
		delete(b.threads, threadID)
	}
	b.mutex.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// channelEventTransactionBuffer 暂存当前数据库事务成功后才能对外唤醒的事件。
type channelEventTransactionBuffer struct {
	// events 是事务回调已经写入、等待提交确认的 Team 持久事件。
	events []models.ChannelEvent
}

// channelEventTransactionContextKey 隔离 Team 事务内的待发布事件集合。
type channelEventTransactionContextKey struct{}

// transaction 在数据库事务提交后统一发布其中写入的 Team 事件。
func (s *service) transaction(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	var buffer *channelEventTransactionBuffer
	if err := s.transactions.Transaction(ctx, func(transactionContext context.Context) error {
		// busy 回滚后重新执行回调，待发布事件也必须从本次事务重新收集。
		buffer = &channelEventTransactionBuffer{}
		return fn(context.WithValue(transactionContext, channelEventTransactionContextKey{}, buffer))
	}); err != nil {
		return err
	}
	for _, event := range buffer.events {
		committedevents.AfterCommit(ctx, func() { s.publishCommittedEvent(event) })
	}
	return nil
}

// appendPreparedEvent 持久化完整 Team 事件，并只在提交可见后通知实时订阅者。
func (s *service) appendPreparedEvent(
	ctx context.Context,
	event models.ChannelEvent,
) (models.ChannelEvent, error) {
	persisted, err := s.threads.AppendEvent(ctx, event)
	if err != nil {
		return models.ChannelEvent{}, err
	}
	if buffer, ok := ctx.Value(channelEventTransactionContextKey{}).(*channelEventTransactionBuffer); ok {
		buffer.events = append(buffer.events, persisted)
		return persisted, nil
	}
	committedevents.AfterCommit(ctx, func() { s.publishCommittedEvent(persisted) })
	return persisted, nil
}

// publishCommittedEvent 唤醒指定 Team 的实时订阅者；没有订阅者时不保留内存副本。
func (s *service) publishCommittedEvent(event models.ChannelEvent) {
	if s.events != nil {
		s.events.publish(event.ThreadID)
	}
}

// SubscribeEvents 先完成持久交互补偿，再衔接 Team backlog 和实时 broker。
func (s *service) SubscribeEvents(
	ctx context.Context,
	threadID string,
	after string,
) (EventSubscription, error) {
	threadID = strings.TrimSpace(threadID)
	after = strings.TrimSpace(after)
	if threadID == "" || s.events == nil {
		return EventSubscription{}, status_error.BadRequest
	}
	if err := s.SyncInteractionEvents(ctx, threadID); err != nil {
		return EventSubscription{}, err
	}
	members, err := s.threads.ListMembers(ctx, threadID)
	if err != nil {
		return EventSubscription{}, err
	}
	subscriberID, wakeups, group, startProjection := s.events.add(threadID)
	if startProjection {
		if err := s.startInteractionProjection(threadID, group, members); err != nil {
			s.events.remove(threadID, subscriberID)
			return EventSubscription{}, err
		}
	}
	backlog, err := s.threads.ListEvents(
		ctx,
		threadID,
		after,
		constants.ChannelEventDefaultLimit,
	)
	if err != nil {
		s.events.remove(threadID, subscriberID)
		return EventSubscription{}, err
	}
	streamContext, cancelStream := context.WithCancel(ctx)
	stream := make(chan models.ChannelEvent, channelEventOutputBufferSize)
	streamAfter := after
	if len(backlog) > 0 {
		streamAfter = backlog[len(backlog)-1].ID
	}
	go s.forwardChannelEvents(streamContext, threadID, streamAfter, wakeups, stream)
	if len(backlog) == constants.ChannelEventDefaultLimit {
		s.events.publish(threadID)
	}
	var once sync.Once
	return EventSubscription{
		Backlog: backlog,
		Events:  stream,
		Close: func() {
			once.Do(func() {
				cancelStream()
				s.events.remove(threadID, subscriberID)
			})
		},
	}, nil
}

// forwardChannelEvents 在 broker 唤醒后从权威数据库按游标排空新增事件。
func (s *service) forwardChannelEvents(
	ctx context.Context,
	threadID string,
	after string,
	wakeups <-chan struct{},
	stream chan<- models.ChannelEvent,
) {
	defer close(stream)
	for {
		select {
		case <-ctx.Done():
			return
		case _, open := <-wakeups:
			if !open {
				return
			}
		}
		for {
			events, err := s.threads.ListEvents(
				ctx,
				threadID,
				after,
				constants.ChannelEventDefaultLimit,
			)
			if err != nil {
				logx.Errorf(
					"read channel event subscription failed: operation=forward_channel_events thread_id=%s after=%s error=%v",
					threadID,
					after,
					err,
				)
				return
			}
			for _, event := range events {
				select {
				case <-ctx.Done():
					return
				case stream <- event:
					after = event.ID
				}
			}
			if len(events) < constants.ChannelEventDefaultLimit {
				break
			}
		}
	}
}

// startInteractionProjection 为一个有订阅者的 Team 启动唯一成员 Chat 事件观察组。
func (s *service) startInteractionProjection(
	threadID string,
	group *channelEventBrokerThread,
	members []models.ChannelThreadMember,
) error {
	watcherID := "channel-interactions:" + threadID
	watcherContext, registered := s.registerWatcher(watcherID)
	if !registered {
		return status_error.ChatEventStreamUnavailable
	}
	projectionContext, cancelProjection := context.WithCancel(watcherContext)
	if !s.events.attachProjection(threadID, group, cancelProjection) {
		cancelProjection()
		s.unregisterWatcher(watcherID)
		return status_error.ChatEventStreamUnavailable
	}
	go func() {
		defer s.unregisterWatcher(watcherID)
		s.runInteractionProjection(projectionContext, threadID, members)
	}()
	return nil
}

// runInteractionProjection 并行观察固定成员 Chat，并共享同一个 Team broker 生命周期。
func (s *service) runInteractionProjection(
	ctx context.Context,
	threadID string,
	members []models.ChannelThreadMember,
) {
	var watchers sync.WaitGroup
	watchers.Add(len(members))
	for _, member := range members {
		member := member
		go func() {
			defer watchers.Done()
			s.watchMemberInteractionEvents(ctx, threadID, member)
		}()
	}
	watchers.Wait()
}

// watchMemberInteractionEvents 以 Chat 持久游标补历史并观察后续公开交互事件。
func (s *service) watchMemberInteractionEvents(
	ctx context.Context,
	threadID string,
	member models.ChannelThreadMember,
) {
	policy := s.effectiveWatcherPolicy()
	retryDelay := policy.initialDelay
	for {
		after := s.interactionCursor(member.ChatID)
		subscription, err := s.chats.SubscribeEvents(ctx, member.ChatID, after)
		if err != nil {
			logx.Errorf(
				"subscribe team interaction events failed: operation=watch_member_interactions thread_id=%s chat_id=%s waker_id=%s after=%d error=%v",
				threadID,
				member.ChatID,
				member.WakerID,
				after,
				err,
			)
			if !waitAttemptWatcherRetry(ctx, retryDelay) {
				return
			}
			retryDelay = nextAttemptWatcherRetryDelay(retryDelay, policy.maximumDelay)
			continue
		}
		resubscribe := false
		for _, event := range subscription.Backlog {
			if observeErr := s.observeInteractionEvent(ctx, threadID, member, event); observeErr != nil {
				logTeamInteractionProjectionError(threadID, member, event, observeErr)
				resubscribe = true
				break
			}
		}
		for !resubscribe {
			select {
			case <-ctx.Done():
				subscription.Close()
				return
			case event, open := <-subscription.Events:
				if !open {
					resubscribe = true
					continue
				}
				if observeErr := s.observeInteractionEvent(ctx, threadID, member, event); observeErr != nil {
					logTeamInteractionProjectionError(threadID, member, event, observeErr)
					resubscribe = true
					continue
				}
				retryDelay = policy.initialDelay
			}
		}
		subscription.Close()
		if !waitAttemptWatcherRetry(ctx, retryDelay) {
			return
		}
		retryDelay = nextAttemptWatcherRetryDelay(retryDelay, policy.maximumDelay)
	}
}

// interactionCursor 返回一个成员 Chat 已经完成 Team 投影的最大持久序号。
func (s *service) interactionCursor(chatID string) int64 {
	s.interactionMutex.Lock()
	defer s.interactionMutex.Unlock()
	return s.interactionCursors[chatID]
}

// setInteractionCursor 提交一个成员 Chat 已完成 Team 投影的最大持久序号。
func (s *service) setInteractionCursor(chatID string, sequence int64) {
	s.interactionMutex.Lock()
	s.interactionCursors[chatID] = sequence
	s.interactionMutex.Unlock()
}

// lockInteractionProjection 只串行同一分片内的成员 Chat，避免全局锁包围数据库和 Chat I/O。
func (s *service) lockInteractionProjection(chatID string) func() {
	var hash uint32 = 2166136261
	for index := 0; index < len(chatID); index++ {
		hash ^= uint32(chatID[index])
		hash *= 16777619
	}
	shard := &s.interactionShards[hash%channelInteractionProjectionShardCount]
	shard.Lock()
	return shard.Unlock
}

// observeInteractionEvent 按来源 Chat 游标幂等推进交互投影。
func (s *service) observeInteractionEvent(
	ctx context.Context,
	threadID string,
	member models.ChannelThreadMember,
	event chat.PublicEvent,
) error {
	if event.Transient || event.Sequence <= 0 {
		return nil
	}
	unlock := s.lockInteractionProjection(member.ChatID)
	defer unlock()
	if event.Sequence <= s.interactionCursor(member.ChatID) {
		return nil
	}
	if !isProjectedInteractionEvent(event.Type) {
		s.setInteractionCursor(member.ChatID, event.Sequence)
		return nil
	}
	thread, err := s.threads.Get(ctx, threadID)
	if err != nil {
		return err
	}
	if err := s.mirrorInteractionEvent(ctx, thread, member, event); err != nil {
		return err
	}
	s.setInteractionCursor(member.ChatID, event.Sequence)
	return nil
}

// isProjectedInteractionEvent 判断 Chat 公开事实是否需要复制到 Team 事件流。
func isProjectedInteractionEvent(eventType constants.ChatEventType) bool {
	switch eventType {
	case constants.ChatEventTypeApprovalRequested,
		constants.ChatEventTypeApprovalResolved,
		constants.ChatEventTypeElicitationRequested,
		constants.ChatEventTypeElicitationResolved:
		return true
	default:
		return false
	}
}

// logTeamInteractionProjectionError 记录交互投影失败的完整来源游标。
func logTeamInteractionProjectionError(
	threadID string,
	member models.ChannelThreadMember,
	event chat.PublicEvent,
	err error,
) {
	logx.Errorf(
		"project team interaction event failed: operation=watch_member_interactions thread_id=%s chat_id=%s waker_id=%s execution_id=%s sequence=%d event_type=%s error=%v",
		threadID,
		member.ChatID,
		member.WakerID,
		event.ExecutionID,
		event.Sequence,
		event.Type,
		err,
	)
}
