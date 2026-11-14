package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// ContentStore 只跟踪当前进程内容使用期间，不复制数据库中的资源事实。
type ContentStore struct {
	// mutex 串行化资源准备、接纳和删除后的回收检查。
	mutex sync.Mutex
	// root 是当前应用数据根目录。
	root string
	// references 每次从数据库读取仍保留的内容和数据路径。
	references func() ([]string, error)
	// active 保存进行中执行的目录使用计数。
	active map[string]int
	// pending 记录回收时仍被租用的路径，最后一位使用者退出时重新核验。
	pending map[string]bool
}

// NewContentStore 创建没有后台任务的内容生命周期边界。
func NewContentStore(root string, references func() ([]string, error)) *ContentStore {
	return &ContentStore{root: root, references: references, active: map[string]int{}, pending: map[string]bool{}}
}

// Guard 保护暂存复制与资源提交窗口，避免尚未入库的内容被回收。
func (s *ContentStore) Guard() func() { s.mutex.Lock(); return s.mutex.Unlock }

// Acquire 保留内容直至本轮执行退出，已删除内容不能重新接纳。
func (s *ContentStore) Acquire(paths ...string) (func(), error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	resolved := []string{}
	for _, path := range paths {
		value, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, status_error.PluginNotReady
		}
		allowed := false
		for _, kind := range []string{"content", "data"} {
			base, err := s.managedRoot(kind)
			if err != nil {
				continue
			}
			relative, err := filepath.Rel(base, value)
			if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				allowed = true
			}
		}
		if !allowed {
			return nil, status_error.PluginContentInvalid
		}
		resolved = append(resolved, value)
	}
	for _, path := range resolved {
		s.active[path]++
	}
	return sync.OnceFunc(func() {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		var ready []string
		for _, path := range resolved {
			s.active[path]--
			if s.active[path] == 0 {
				delete(s.active, path)
				if s.pending[path] {
					ready = append(ready, path)
				}
			}
		}
		if len(ready) == 0 {
			return
		}
		live, err := s.livePaths()
		if err == nil {
			err = s.collectPaths(ready, live)
		}
		if err != nil {
			logx.Warnf("collect released plugin content failed: %v", err)
		}
	}), nil
}

// Collect 只清理当前数据根下已无数据库引用和活跃使用的直接资源目录。
func (s *ContentStore) Collect() error {
	return s.CollectAfter(nil)
}

// CollectAfter 在同一边界内退休业务元数据，并根据最终引用回收文件。
func (s *ContentStore) CollectAfter(retire func([]string) error) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if retire != nil {
		root, err := filepath.EvalSymlinks(s.root)
		if err != nil {
			return storageFailure(err)
		}
		active := make([]string, 0, len(s.active))
		for path := range s.active {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return status_error.PluginStorageError
			}
			active = append(active, filepath.ToSlash(relative))
		}
		if err := retire(active); err != nil {
			return err
		}
	}
	live, err := s.livePaths()
	if err != nil {
		return err
	}
	for _, kind := range []string{"content", "data"} {
		directory, err := s.managedRoot(kind)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return status_error.PluginStorageError
		}
		paths := make([]string, 0, len(entries))
		for _, entry := range entries {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
		if err := s.collectPaths(paths, live); err != nil {
			return err
		}
	}
	return nil
}

func (s *ContentStore) livePaths() (map[string]bool, error) {
	live := map[string]bool{}
	if s.references == nil {
		return live, nil
	}
	paths, err := s.references()
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.root, path)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, status_error.PluginStorageError
		}
		live[resolved] = true
	}
	return live, nil
}

func (s *ContentStore) collectPaths(paths []string, live map[string]bool) error {
	for _, path := range paths {
		resolved, err := filepath.EvalSymlinks(path)
		if os.IsNotExist(err) {
			delete(s.pending, path)
			continue
		}
		if err != nil || resolved != path {
			return status_error.PluginStorageError
		}
		if live[path] {
			delete(s.pending, path)
			continue
		}
		if s.active[path] > 0 {
			s.pending[path] = true
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return status_error.PluginStorageError
		}
		delete(s.pending, path)
	}
	return nil
}

// CleanTargetStaging 在 Core 接纳任何工作前清除上次运行留下的派生目标和锁。
func (s *ContentStore) CleanTargetStaging() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	root, err := s.managedRoot("targets")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return storageFailure(err)
	}
	return nil
}

// managedRoot 禁止回收和接纳穿过受管子目录的符号链接。
func (s *ContentStore) managedRoot(kind string) (string, error) {
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(root, "plugins", kind)
	actual, err := filepath.EvalSymlinks(expected)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(expected, actual)
	if err != nil || relative != "." {
		return "", status_error.PluginContentInvalid
	}
	return actual, nil
}
