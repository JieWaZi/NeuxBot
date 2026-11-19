package localweb

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/gofrs/flock"
)

// LockDataDirectory 对同一数据根取得跨进程锁，避免独立 Web 与桌面同时拥有 Core。
func LockDataDirectory(directory string) (func() error, error) {
	return LockStorageRoots(directory, directory)
}

// LockStorageRoots 分别取得两个受管根的跨进程锁，同一真实目录只锁一次。
func LockStorageRoots(dataDirectory, managedDirectory string) (func() error, error) {
	directories := []string{dataDirectory, managedDirectory}
	identities := make([]os.FileInfo, 0, len(directories))
	roots := make([]string, 0, len(directories))
	for _, directory := range directories {
		root, err := filepath.EvalSymlinks(directory)
		if err != nil {
			logx.Errorf("resolve core storage lock directory %q: %v", directory, err)
			return nil, status_error.CoreDataDirectoryError
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			logx.Errorf("inspect core storage lock directory %q: %v", root, err)
			return nil, status_error.CoreDataDirectoryError
		}
		if len(identities) == 0 || !os.SameFile(info, identities[0]) {
			identities = append(identities, info)
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)

	locks := make([]*flock.Flock, 0, len(roots))
	release := sync.OnceValue(func() error {
		var result error
		for i := len(locks) - 1; i >= 0; i-- {
			if err := locks[i].Close(); err != nil {
				logx.Errorf("release core storage lock %q: %v", locks[i].Path(), err)
				result = errors.Join(result, status_error.CoreShutdownFailed)
			}
		}
		return result
	})
	for _, root := range roots {
		lock := flock.New(filepath.Join(root, "core.lock"))
		locks = append(locks, lock)
		ok, err := lock.TryLock()
		if err != nil {
			_ = release()
			logx.Errorf("lock core storage root %q: %v", root, err)
			return nil, status_error.CoreDataDirectoryError
		}
		if !ok {
			_ = release()
			logx.Warnf("core storage root already in use: %q", root)
			return nil, status_error.CoreStorageInUse
		}
	}
	return release, nil
}
