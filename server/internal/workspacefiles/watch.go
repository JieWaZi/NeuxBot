package workspacefiles

import (
	"context"
	"os"
	"path/filepath"
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

// ValidateScope 验证句柄仍归当前认证主体所有。
func (m *Manager) ValidateScope(subject, id string) error { _, err := m.scope(subject, id); return err }

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
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return ChangeEvent{}, nil, nil, fileError("create-watch", err)
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
			return ChangeEvent{}, nil, nil, err
		}
	}
	if directory {
		if err = addTree(target); err != nil {
			watcher.Close()
			return ChangeEvent{}, nil, nil, err
		}
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
		watcher.Close()
		return ChangeEvent{}, nil, nil, err
	}
	ready := ChangeEvent{SubscriptionID: uuid.NewString(), Sequence: 1, State: "ready", Resource: initial}
	events := make(chan ChangeEvent, 1)
	done := make(chan struct{})
	owned, cancel := context.WithDeadline(ctx, scope.expires)
	var once sync.Once
	closeWatch := func() { once.Do(func() { cancel(); watcher.Close() }); <-done }
	go func() {
		defer close(done)
		defer close(events)
		defer cancel()
		defer watcher.Close()
		sequence := uint64(1)
		publish := func(state string, resource *Resource) {
			sequence++
			event := ChangeEvent{SubscriptionID: ready.SubscriptionID, Sequence: sequence, State: state, Resource: resource}
			select {
			case events <- event:
			default:
				select {
				case <-events:
				default:
				}
				event.State = "recovering"
				select {
				case events <- event:
				case <-owned.Done():
				}
			}
		}
		for {
			select {
			case <-owned.Done():
				return
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
				publish("recovering", nil)
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !directory && event.Name != target {
					continue
				}
				if directory && !within(target, event.Name) && event.Name != target {
					continue
				}
				if event.Has(fsnotify.Create) && directory {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if err = addTree(event.Name); err != nil {
							publish("failed", nil)
							return
						}
					}
				}
				resource, err := snapshot()
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
