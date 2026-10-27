//go:build !darwin

package workspacefiles

import "github.com/fsnotify/fsnotify"

func newResourceWatcher() (*resourceWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &resourceWatcher{
		Events: watcher.Events,
		Errors: watcher.Errors,
		Add:    watcher.Add,
		Close:  watcher.Close,
	}, nil
}
