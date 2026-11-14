package catalog

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

const (
	artifactRetryWindow   = 5 * time.Second
	artifactIdleAge       = 7 * 24 * time.Hour
	archiveDiskBudget     = 512 * 1024 * 1024
	detailsDiskBudget     = 64 * 1024 * 1024
	maxIdleArtifactStates = 128
)

// artifactLoadState 合并同一份工件的核验和下载，提高故障重试的稳定性。
type artifactLoadState struct {
	// gate 串行化文件核验和原子发布。
	gate chan struct{}
	// failedURL 绑定失败的分发地址。
	failedURL string
	// failedAt 记录最近失败时间。
	failedAt time.Time
	// failure 是短冷却期内复用的稳定错误。
	failure error
	// refs 统计等待者、下载和正在使用的文件路径。
	refs int
	// idleAt 记录最后一个使用者退出时间。
	idleAt time.Time
}

// artifactCache 保护下载协调状态及可回收资产的读取租约。
type artifactCache struct {
	// mu 序列化文件回收与租约接纳。
	mu sync.Mutex
	// states 仅保留活跃状态及有界的短时失败。
	states map[string]*artifactLoadState
}

var artifactStates = artifactCache{states: make(map[string]*artifactLoadState)}

func (c *artifactCache) retain(key string) (*artifactLoadState, func()) {
	c.mu.Lock()
	state := c.states[key]
	if state == nil {
		state = &artifactLoadState{gate: make(chan struct{}, 1)}
		c.states[key] = state
	}
	state.refs++
	c.mu.Unlock()
	return state, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		state.refs--
		if state.refs != 0 {
			return
		}
		now := time.Now()
		if state.failure == nil || now.Sub(state.failedAt) >= artifactRetryWindow {
			delete(c.states, key)
		} else {
			state.idleAt = now
		}
		var oldest string
		var oldestTime time.Time
		idleCount := 0
		for key, candidate := range c.states {
			if candidate.refs != 0 {
				continue
			}
			if now.Sub(candidate.idleAt) >= artifactRetryWindow {
				delete(c.states, key)
				continue
			}
			idleCount++
			if oldest == "" || candidate.idleAt.Before(oldestTime) {
				oldest, oldestTime = key, candidate.idleAt
			}
		}
		if idleCount > maxIdleArtifactStates {
			delete(c.states, oldest)
		}
	}
}

func (c *artifactCache) pin(path string) func() {
	_, release := c.retain(path)
	return release
}

func (c *artifactCache) acquire(ctx context.Context, key string) (*artifactLoadState, func(), error) {
	state, release := c.retain(key)
	select {
	case state.gate <- struct{}{}:
		return state, func() { <-state.gate; release() }, nil
	case <-ctx.Done():
		release()
		return nil, nil, status_error.PluginDistributionUnavailable
	}
}

func (state *artifactLoadState) failed(address string) error {
	if state.failedURL == address && time.Since(state.failedAt) < artifactRetryWindow {
		return state.failure
	}
	return nil
}

func (state *artifactLoadState) fail(address string, err error) error {
	state.failedURL, state.failedAt, state.failure = address, time.Now(), err
	return err
}

type artifactFile struct {
	// path 是受管的摘要文件路径。
	path string
	// size 是本地工件字节数。
	size int64
	// written 是工件原子发布的时间。
	written time.Time
}

func pruneArtifacts(root string, entries []Entry, now time.Time) error {
	protected := map[string]bool{}
	for _, entry := range entries {
		protected[archivePath(root, entry.ArchiveSHA256)] = true
		protected[filepath.Join(root, "details", entry.DetailsSHA256+".json")] = true
	}
	if err := artifactStates.prune(filepath.Join(root, "archives"), ".zip", archiveDiskBudget, protected, now); err != nil {
		return err
	}
	return artifactStates.prune(filepath.Join(root, "details"), ".json", detailsDiskBudget, protected, now)
}

func (c *artifactCache) prune(directory, suffix string, budget int64, protected map[string]bool, now time.Time) error {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return status_error.PluginStorageError
	}
	files, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var candidates []artifactFile
	var total int64
	for _, file := range files {
		name := file.Name()
		digest, ok := strings.CutSuffix(name, suffix)
		if !ok || len(digest) != 64 || !file.Type().IsRegular() {
			continue
		}
		if _, err := hex.DecodeString(digest); err != nil {
			continue
		}
		info, err := file.Info()
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		path := filepath.Join(directory, name)
		total += info.Size()
		if !protected[path] {
			candidates = append(candidates, artifactFile{path: path, size: info.Size(), written: info.ModTime()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].written.Before(candidates[j].written) })
	for _, candidate := range candidates {
		if total <= budget && now.Sub(candidate.written) < artifactIdleAge {
			break
		}
		c.mu.Lock()
		state := c.states[candidate.path]
		var err error
		if state == nil || state.refs == 0 {
			if info, statErr := os.Lstat(candidate.path); statErr == nil && info.Mode().IsRegular() &&
				info.Size() == candidate.size && info.ModTime().Equal(candidate.written) {
				err = os.Remove(candidate.path)
				if err == nil {
					total -= candidate.size
				}
			}
		}
		c.mu.Unlock()
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
