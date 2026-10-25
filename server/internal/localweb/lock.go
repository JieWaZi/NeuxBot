package localweb

import (
	"fmt"
	"github.com/gofrs/flock"
	"path/filepath"
)

// LockDataDirectory 对同一数据根取得跨进程锁，避免独立 Web 与桌面同时拥有 Core。
func LockDataDirectory(directory string) (func() error, error) {
	lock := flock.New(filepath.Join(directory, "core.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		_ = lock.Close()
		return nil, fmt.Errorf("数据目录已由其他 Core 使用：%s", directory)
	}
	return lock.Close, nil
}
