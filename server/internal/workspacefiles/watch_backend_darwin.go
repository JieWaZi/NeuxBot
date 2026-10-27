package workspacefiles

import (
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/fswatcher/fswatcher"
)

// newResourceWatcher 使用 FSEvents，避免 kqueue 为目录内每个文件打开描述符。
// purego 后端兼容桌面 sidecar 的 CGO_ENABLED=0 构建。
func newResourceWatcher() (*resourceWatcher, error) {
	watcher, err := fswatcher.NewWatcher()
	if err != nil {
		return nil, err
	}
	events := make(chan fsnotify.Event)
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		defer close(events)
		for {
			select {
			case <-stop:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				var operation fsnotify.Op
				if event.Op.Has(fswatcher.Create) {
					operation |= fsnotify.Create
				}
				if event.Op.Has(fswatcher.Write) {
					operation |= fsnotify.Write
				}
				if event.Op.Has(fswatcher.Remove) {
					operation |= fsnotify.Remove
				}
				if event.Op.Has(fswatcher.Rename) {
					operation |= fsnotify.Rename
				}
				if event.Op.Has(fswatcher.Chmod) {
					operation |= fsnotify.Chmod
				}
				select {
				case events <- fsnotify.Event{Name: event.Name, Op: operation}:
				case <-stop:
					return
				}
			}
		}
	}()
	return &resourceWatcher{
		Events: events,
		Errors: watcher.Errors,
		Add: func(path string) error {
			return watcher.Add(path, fswatcher.All)
		},
		AddRecursive: func(path string) error {
			return watcher.AddRecursive(path, fswatcher.All)
		},
		Close: func() error {
			once.Do(func() { close(stop) })
			err := watcher.Close()
			<-done
			return err
		},
	}, nil
}
