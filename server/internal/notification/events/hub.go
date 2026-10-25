// Package events 提供应用事件的有界路由与提交后发布边界，不保存业务历史。
package events

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

const (
	// Protocol 表示当前全局事件传输协议。
	Protocol = "ally.events.v1"
	// MaximumSubscriptions 限制单连接资源订阅数量。
	MaximumSubscriptions = 128
	// MaximumFrameBytes 限制单个业务事件的序列化大小。
	MaximumFrameBytes = 128 << 10
	// MaximumControlBytes 限制客户端控制帧大小。
	MaximumControlBytes = 16 << 10
	// BufferSize 是单消费者允许积压的事件上限。
	BufferSize = 256
)

// Resource 是受限资源定位，不接受任意地址或可执行命令。
type Resource struct {
	// Kind 区分全局、对话、团队对话、工作流运行等资源。
	Kind string `json:"kind"`
	// ID 是资源的原身份，全局资源可省略。
	ID string `json:"id,omitempty"`
	// ParentID 是资源的原所属身份，用于归属校验。
	ParentID string `json:"parentId,omitempty"`
}

// Event 是已完成安全投影的变化；连接序号由传输层发送时赋予。
type Event struct {
	// Topic 是编译期声明的事件主题。
	Topic string `json:"topic"`
	// Type 是主题内的明确事件种类。
	Type string `json:"type"`
	// Resource 定位原领域资源。
	Resource Resource `json:"resource"`
	// EventID 标识本次真实发布，不能替代原领域游标。
	EventID string `json:"eventId"`
	// SourceCursor 是原领域持久化恢复游标。
	SourceCursor string `json:"sourceCursor,omitempty"`
	// Transient 标识不推进持久游标的安全实时快照。
	Transient bool `json:"transient,omitempty"`
	// Payload 是已脱敏并有大小上限的主题载荷。
	Payload json.RawMessage `json:"payload"`
}

// Publisher 是生产者使用的窄发布端口。
type Publisher interface {
	Publish(Event)
}

// Subscription 拥有一个有界事件队列，队列关闭要求消费者重新对账。
type Subscription struct {
	// Events 交付匹配的实时变化，不保存可编辑的业务副本。
	Events <-chan Event
	// Close 幂等释放订阅资源。
	Close func()
}

// listener 保存单消费者的过滤条件与传输队列。
type listener struct {
	// topic 为空时接收内部所有主题，否则精确匹配。
	topic string
	// resource 为空时接收该主题所有资源，否则精确匹配。
	resource Resource
	// queue 是有界输出队列，发布者不会等待慢消费者。
	queue chan Event
}

// Hub 只负责事件路由、缓冲与生命周期。
type Hub struct {
	// mutex 串行注册、发布与释放，避免关闭和发送竞态。
	mutex sync.Mutex
	// listeners 以本地递增身份保存消费者。
	listeners map[uint64]listener
	// nextID 为本次进程内订阅生成身份。
	nextID uint64
	// closed 阻止退出后重新订阅或发布。
	closed bool
	// epoch 是当前 Core 生命周期身份。
	epoch string
	// done 在 Core 退出时唤醒已升级的 WS 会话。
	done chan struct{}
}

// New 构造空路由器，不启动后台流程。
func New() *Hub {
	return &Hub{listeners: make(map[uint64]listener), epoch: util.New(), done: make(chan struct{})}
}

// Epoch 返回不能跨 Core 重启比较的生命周期身份。
func (h *Hub) Epoch() string { return h.epoch }

// Done 用于关闭 HTTP Shutdown 无法回收的升级连接。
func (h *Hub) Done() <-chan struct{} { return h.done }

// Subscribe 在查询初始事实之前注册监听，以衔接查询期间的真实变化。
func (h *Hub) Subscribe(topic string, resource Resource) Subscription {
	h.mutex.Lock()
	h.nextID++
	id := h.nextID
	queue := make(chan Event, BufferSize)
	if h.closed {
		close(queue)
	} else {
		h.listeners[id] = listener{topic: topic, resource: resource, queue: queue}
	}
	h.mutex.Unlock()
	var once sync.Once
	return Subscription{Events: queue, Close: func() {
		once.Do(func() {
			h.mutex.Lock()
			defer h.mutex.Unlock()
			if current, exists := h.listeners[id]; exists {
				delete(h.listeners, id)
				close(current.queue)
			}
		})
	}}
}

// Publish 不阻塞提交；慢消费者队列关闭后必须从权威事实恢复。
func (h *Hub) Publish(event Event) {
	if len(event.Payload) > MaximumFrameBytes {
		return
	}
	if event.EventID == "" {
		event.EventID = util.New()
	}
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.closed {
		return
	}
	for id, current := range h.listeners {
		if current.topic != "" && current.topic != event.Topic {
			continue
		}
		if current.resource.Kind != "" && current.resource.Kind != "global" && event.Resource.Kind != "global" && current.resource != event.Resource {
			continue
		}
		select {
		case current.queue <- event:
		default:
			delete(h.listeners, id)
			close(current.queue)
		}
	}
}

// Emit 序列化安全载荷并发布；序列化错误由调用方处理。
func Emit(publisher Publisher, topic, kind string, resource Resource, payload any) error {
	if publisher == nil {
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	publisher.Publish(Event{Topic: topic, Type: kind, Resource: resource, Payload: encoded})
	return nil
}

// Close 释放全部监听，不取消任何原业务执行。
func (h *Hub) Close() {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.done)
	for id, current := range h.listeners {
		delete(h.listeners, id)
		close(current.queue)
	}
}

// commitBuffer 保留本次事务成功后才能发布的回调。
type commitBuffer struct {
	// parent 是尚未提交的外层事务缓冲。
	parent *commitBuffer
	// callbacks 是本次尝试产生的提交后行为。
	callbacks []func()
}

// commitKey 隔离事务内的发布回调。
type commitKey struct{}

// CommitScope 建立独立尝试缓冲；成功的嵌套尝试仅合并到外层。
func CommitScope(ctx context.Context, run func(context.Context) error) error {
	parent, _ := ctx.Value(commitKey{}).(*commitBuffer)
	buffer := &commitBuffer{parent: parent}
	if err := run(context.WithValue(ctx, commitKey{}, buffer)); err != nil {
		return err
	}
	if parent != nil {
		parent.callbacks = append(parent.callbacks, buffer.callbacks...)
		return nil
	}
	for _, callback := range buffer.callbacks {
		callback()
	}
	return nil
}

// AfterCommit 仅用于已经生效的独立写入或事务内登记，禁止提前登记未执行操作。
func AfterCommit(ctx context.Context, callback func()) {
	if buffer, ok := ctx.Value(commitKey{}).(*commitBuffer); ok {
		buffer.callbacks = append(buffer.callbacks, callback)
		return
	}
	callback()
}

// HoldCommitEvents 延迟已提交的公开变化，适用于 Registry 释放等提交后的必要交接。
func HoldCommitEvents(ctx context.Context) (context.Context, func()) {
	buffer := &commitBuffer{}
	var once sync.Once
	return context.WithValue(ctx, commitKey{}, buffer), func() {
		once.Do(func() {
			for _, callback := range buffer.callbacks {
				callback()
			}
		})
	}
}
