package workspacefiles

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
)

// ChangeEvent 表示一个资源观察订阅的状态或变化。
type ChangeEvent struct {
	// SubscriptionID 是当前观察实例的身份。
	SubscriptionID string `json:"subscriptionId"`
	// Sequence 是该实例内递增的交付序号。
	Sequence uint64 `json:"sequence"`
	// State 区分就绪、变化、恢复和失效。
	State string `json:"state"`
	// Resource 是文件快照；目录或已删除文件为空。
	Resource *Resource `json:"resource"`
}

// resourceWatcher 隔离系统监听后端；macOS 使用递归事件流，其他平台保留目录观察。
type resourceWatcher struct {
	// Events 交付统一路径及操作类型。
	Events <-chan fsnotify.Event
	// Errors 交付后端无法继续可靠观察的错误。
	Errors <-chan error
	// Add 注册单个目录。
	Add func(string) error
	// AddRecursive 非空时表示后端支持原生递归，无需逐目录注册。
	AddRecursive func(string) error
	// Close 停止后端并等待资源释放，可重复调用。
	Close func() error
}

// ValidateScope 验证句柄仍归当前认证主体所有。
func (m *Manager) ValidateScope(subject, id string) error { _, err := m.scope(subject, id); return err }

// physicalWatchKey 按真实路径和观察语义复用物理监听，与 Chat 身份无关。
type physicalWatchKey struct {
	// target 是已经通过授权校验的真实路径。
	target string
	// directory 区分目录失效与单文件版本观察。
	directory bool
}

// physicalWatch 只广播失效信号，各消费者仍独立校验授权并读取快照。
type physicalWatch struct {
	// listeners 由 Manager.watchMu 保护，同一消费者最多积压一个信号。
	listeners map[chan string]struct{}
	// state 保存后端的最新可靠性状态。
	state string
	// stop 由最后一个消费者关闭。
	stop chan struct{}
	// done 在底层句柄和转发协程释放后关闭。
	done chan struct{}
}

func configureResourceWatch(allowed, target string, directory bool) (*resourceWatcher, func(string) error, error) {
	watcher, err := newResourceWatcher()
	if err != nil {
		return nil, nil, fileError("create-watch", err)
	}
	watched := map[string]bool{}
	add := func(name string) error {
		if watched[name] {
			return nil
		}
		if len(watched) >= 10000 {
			return status_error.WorkspaceResourceTooLarge
		}
		if !within(allowed, name) {
			return status_error.WorkspacePathInvalid
		}
		if err := watcher.Add(name); err != nil {
			return fileError("add-watch", err)
		}
		watched[name] = true
		return nil
	}
	var addTree func(string) error
	addTree = func(name string) error {
		return filepath.WalkDir(name, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fileError("walk-watch", walkErr)
			}
			if !entry.IsDir() {
				return nil
			}
			if entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if accountPath(current) {
				return filepath.SkipDir
			}
			return add(current)
		})
	}
	if within(allowed, filepath.Dir(target)) {
		if err = add(filepath.Dir(target)); err != nil {
			watcher.Close()
			return nil, nil, err
		}
	}
	if directory {
		if watcher.AddRecursive != nil {
			err = watcher.AddRecursive(target)
			if err != nil {
				err = fileError("add-watch-tree", err)
			}
		} else {
			err = addTree(target)
		}
		if err != nil {
			watcher.Close()
			return nil, nil, err
		}
	}
	return watcher, addTree, nil
}

func (m *Manager) subscribePhysicalWatch(allowed, target string, directory bool) (<-chan string, func(), error) {
	key := physicalWatchKey{target: target, directory: directory}
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	if m.watches == nil {
		m.watches = make(map[physicalWatchKey]*physicalWatch)
	}
	source := m.watches[key]
	if source == nil {
		watcher, addTree, err := configureResourceWatch(allowed, target, directory)
		if err != nil {
			return nil, nil, err
		}
		source = &physicalWatch{listeners: make(map[chan string]struct{}), stop: make(chan struct{}), done: make(chan struct{})}
		m.watches[key] = source
		go m.runPhysicalWatch(key, source, watcher, addTree)
	}
	signals := make(chan string, 1)
	source.listeners[signals] = struct{}{}
	if source.state != "" {
		signals <- source.state
	}
	var once sync.Once
	return signals, func() {
		once.Do(func() {
			m.watchMu.Lock()
			delete(source.listeners, signals)
			last := len(source.listeners) == 0
			if last {
				delete(m.watches, key)
				close(source.stop)
			}
			m.watchMu.Unlock()
			if last {
				<-source.done
			}
		})
	}, nil
}

func (m *Manager) runPhysicalWatch(key physicalWatchKey, source *physicalWatch, watcher *resourceWatcher, addTree func(string) error) {
	defer close(source.done)
	defer watcher.Close()
	publish := func(state string) {
		m.watchMu.Lock()
		defer m.watchMu.Unlock()
		source.state = state
		for listener := range source.listeners {
			// 这里只传递“需重读”的信号，覆盖旧信号不会丢失文件事实。
			select {
			case <-listener:
			default:
			}
			listener <- state
		}
	}
	for {
		select {
		case <-source.stop:
			return
		case _, ok := <-watcher.Errors:
			if !ok {
				publish("failed")
				return
			}
			publish("recovering")
		case event, ok := <-watcher.Events:
			if !ok {
				publish("failed")
				return
			}
			if !key.directory && event.Name != key.target {
				continue
			}
			if key.directory && !within(key.target, event.Name) {
				continue
			}
			if accountPath(event.Name) || (key.directory && strings.Contains(filepath.ToSlash(event.Name)+"/", "/node_modules/")) {
				continue
			}
			if event.Has(fsnotify.Create) && key.directory && watcher.AddRecursive == nil {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err = addTree(event.Name); err != nil {
						publish("failed")
						return
					}
				}
			}
			publish("changed")
		}
	}
}

// Watch 使用文件系统事件观察冻结范围，调用方必须在订阅结束时调用返回的关闭函数。
func (m *Manager) Watch(ctx context.Context, subject, id, relative, root string, directory bool) (ChangeEvent, <-chan ChangeEvent, func(), error) {
	scope, err := m.scope(subject, id)
	if err != nil {
		return ChangeEvent{}, nil, nil, err
	}
	allowed, target, err := m.resolve(subject, id, root, relative, true)
	if err != nil {
		return ChangeEvent{}, nil, nil, err
	}

	snapshot := func() (*Resource, error) {
		if err := m.ValidateScope(subject, id); err != nil {
			return nil, err
		}
		if directory {
			_, _, err := m.resolve(subject, id, root, relative, false)
			return nil, err
		}
		resource, _, err := m.Read(subject, id, root, relative)
		if err == status_error.WorkspaceResourceNotFound {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &resource, nil
	}
	initial, err := snapshot()
	if err != nil {
		return ChangeEvent{}, nil, nil, err
	}
	signals, release, err := m.subscribePhysicalWatch(allowed, target, directory)
	if err != nil {
		return ChangeEvent{}, nil, nil, err
	}
	ready := ChangeEvent{SubscriptionID: uuid.NewString(), Sequence: 1, State: "ready", Resource: initial}
	// 快照按消费速度交付；期间的变化在物理监听处合并，不伪造丢帧或恢复错误。
	events := make(chan ChangeEvent)
	done := make(chan struct{})
	owned, cancel := context.WithDeadline(ctx, scope.expires)
	var once sync.Once
	closeWatch := func() { once.Do(cancel); <-done }
	go func() {
		defer close(done)
		defer close(events)
		defer cancel()
		defer release()
		sequence := uint64(1)
		publish := func(state string, resource *Resource) {
			sequence++
			event := ChangeEvent{SubscriptionID: ready.SubscriptionID, Sequence: sequence, State: state, Resource: resource}
			select {
			case events <- event:
			case <-owned.Done():
			}
		}
		for {
			select {
			case <-owned.Done():
				return
			case state := <-signals:
				if state != "changed" {
					publish(state, nil)
					if state == "failed" {
						return
					}
					continue
				}
				resource, err := snapshot()
				if err == status_error.WorkspaceResourceConflict {
					// 文件仍在写入，保留订阅，由后续文件事件重新取得完整快照。
					publish("recovering", nil)
					continue
				}
				if err != nil {
					publish("failed", nil)
					return
				}
				publish("changed", resource)
			}
		}
	}()
	return ready, events, closeWatch, nil
}
