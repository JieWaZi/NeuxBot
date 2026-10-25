package workspace

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workspacefiles"
	"sync"
	"time"
)

// watchRegistration 保存受认证的观察参数；文件事实仍由 Manager 读取。
type watchRegistration struct {
	// subject 绑定发起浏览会话。
	subject string
	// request 限制到已签发的 Chat 工作范围。
	request WatchRequest
	// created 用于回收从未建立事件订阅的登记。
	created time.Time
	// sequence 跨事件重连保持该观察的单调序号。
	sequence uint64
	// cancel 只结束当前文件观察，不影响其他资源。
	cancel context.CancelFunc
	// generation 区分当前观察与旧连接的关闭回调。
	generation uint64
	// releaseLifetime 移除会话失效回调，防止已退订记录继续占用内存。
	releaseLifetime func() bool
}

var watchRegistry = struct {
	// Mutex 串行登记、退订和观察序号分配。
	sync.Mutex
	// values 以不可猜测标识保存有限观察登记。
	values map[string]*watchRegistration
}{values: make(map[string]*watchRegistration)}

// SubscribeWatch 由已经认证的全局事件连接建立观察，不新增物理连接。
func SubscribeWatch(ctx context.Context, subject, id string) (<-chan workspacefiles.ChangeEvent, func(), error) {
	watchRegistry.Lock()
	registration := watchRegistry.values[id]
	if registration == nil || registration.subject != subject {
		watchRegistry.Unlock()
		return nil, nil, status_error.WorkspaceScopeInvalid
	}
	if registration.cancel != nil {
		registration.cancel()
	}
	child, cancel := context.WithCancel(ctx)
	registration.cancel = cancel
	registration.generation++
	generation := registration.generation
	registration.created = time.Now()
	request := registration.request
	watchRegistry.Unlock()
	ready, changes, closeWatch, err := files.Watch(child, subject, request.ScopeID, request.RelativePath, request.Root, request.Directory)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	output := make(chan workspacefiles.ChangeEvent)
	go func() {
		defer close(output)
		defer closeWatch()
		send := func(value workspacefiles.ChangeEvent) bool {
			watchRegistry.Lock()
			registration.sequence++
			value.Sequence = registration.sequence
			value.SubscriptionID = id
			watchRegistry.Unlock()
			select {
			case output <- value:
				return true
			case <-child.Done():
				return false
			}
		}
		if !send(ready) {
			return
		}
		for {
			select {
			case <-child.Done():
				return
			case value, ok := <-changes:
				if !ok || !send(value) {
					return
				}
			}
		}
	}()
	return output, func() {
		cancel()
		watchRegistry.Lock()
		if registration.generation == generation {
			registration.cancel = nil
		}
		watchRegistry.Unlock()
	}, nil
}

func registerWatch(lifetime context.Context, subject string, request WatchRequest) (string, error) {
	watchRegistry.Lock()
	defer watchRegistry.Unlock()
	now := time.Now()
	for id, value := range watchRegistry.values {
		if value.cancel == nil && now.Sub(value.created) > 10*time.Minute {
			if value.releaseLifetime != nil {
				value.releaseLifetime()
			}
			delete(watchRegistry.values, id)
		}
	}
	count := 0
	for _, value := range watchRegistry.values {
		if value.subject == subject {
			count++
		}
	}
	if count >= 128 || len(watchRegistry.values) >= 1024 {
		return "", status_error.WorkspaceStorageFailed
	}
	id := util.New()
	registration := &watchRegistration{subject: subject, request: request, created: now}
	watchRegistry.values[id] = registration
	registration.releaseLifetime = context.AfterFunc(lifetime, func() { removeWatch(subject, id) })
	return id, nil
}
func removeWatch(subject, id string) bool {
	watchRegistry.Lock()
	defer watchRegistry.Unlock()
	value := watchRegistry.values[id]
	if value == nil || value.subject != subject {
		return false
	}
	if value.cancel != nil {
		value.cancel()
	}
	if value.releaseLifetime != nil {
		value.releaseLifetime()
	}
	delete(watchRegistry.values, id)
	return true
}
