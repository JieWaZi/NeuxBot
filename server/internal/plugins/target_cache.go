package plugins

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gofrs/flock"
)

const maxIdleTargetPreparations = 128

// targetPreparation 只缓存本进程从已核验来源计算出的派生摘要，不信任磁盘自报摘要。
type targetPreparation struct {
	// gate 合并同一份完整生成输入的并发准备。
	gate chan struct{}
	// root 是最近一次完整发布的不可变目录。
	root string
	// digest 是本进程实际生成并验证的字节摘要，每次命中仍复验目录。
	digest string
	// users 统计当前持有者及等待者，均退出后才能淘汰。
	users int
	// idle 指向空闲队列中的键；持有或等待期间为空。
	idle *list.Element
}

var (
	// 公开打包入口复用有界的可信派生摘要，连接私有目标不进入此队列。
	targetPreparationsMu   sync.Mutex
	targetPreparations     = make(map[[sha256.Size]byte]*targetPreparation)
	idleTargetPreparations list.List
)

func lockTargetPreparation(ctx context.Context, parent, id, source, cli string, pkg Package, authentication []AuthenticationBinding) (*targetPreparation, func(), error) {
	parent, err := plannedPluginDirectory(parent)
	if err != nil {
		return nil, nil, err
	}
	input, err := json.Marshal([]any{parent, id, source, cli, pkg, authentication})
	if err != nil {
		return nil, nil, status_error.PluginContentInvalid
	}
	sum := sha256.Sum256(input)
	targetPreparationsMu.Lock()
	prepared := targetPreparations[sum]
	if prepared == nil {
		prepared = &targetPreparation{gate: make(chan struct{}, 1)}
		targetPreparations[sum] = prepared
	}
	if prepared.idle != nil {
		idleTargetPreparations.Remove(prepared.idle)
		prepared.idle = nil
	}
	prepared.users++
	targetPreparationsMu.Unlock()
	releaseUser := func() {
		targetPreparationsMu.Lock()
		defer targetPreparationsMu.Unlock()
		prepared.users--
		if prepared.users == 0 {
			prepared.idle = idleTargetPreparations.PushBack(sum)
			if idleTargetPreparations.Len() > maxIdleTargetPreparations {
				oldest := idleTargetPreparations.Remove(idleTargetPreparations.Front()).([sha256.Size]byte)
				delete(targetPreparations, oldest)
			}
		}
	}
	select {
	case prepared.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-prepared.gate
			releaseUser()
			return nil, nil, err
		}
		return prepared, sync.OnceFunc(func() {
			<-prepared.gate
			releaseUser()
		}), nil
	case <-ctx.Done():
		releaseUser()
		return nil, nil, ctx.Err()
	}
}

// publishTarget 以跨进程键锁发布不可变目录。修复使用新根，已返回目录始终保留。
func publishTarget(ctx context.Context, parent, stage, id, source, cli, digest string) (string, error) {
	if actual, err := ContentDigest(stage); err != nil {
		return "", err
	} else if actual != digest {
		return "", status_error.PluginVersionConflict
	}
	keyHash := sha256.Sum256([]byte(id + "\x00" + source + "\x00" + cli + "\x00" + digest))
	key := hex.EncodeToString(keyHash[:])
	locks := filepath.Join(parent, ".locks")
	if err := os.MkdirAll(locks, 0700); err != nil {
		return "", storageFailure(err)
	}
	if info, err := os.Lstat(locks); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", status_error.PluginContentInvalid
	}
	lockPath := filepath.Join(locks, key+".lock")
	if info, err := os.Lstat(lockPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", status_error.PluginContentInvalid
	} else if err != nil && !os.IsNotExist(err) {
		return "", storageFailure(err)
	}
	lease := flock.New(lockPath)
	locked, err := lease.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", storageFailure(err)
	}
	if !locked {
		return "", ctx.Err()
	}
	defer lease.Close()
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", storageFailure(err)
	}
	candidates := []string{}
	for _, entry := range entries {
		if entry.Name() == key || strings.HasPrefix(entry.Name(), key+"-repair-") {
			if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				candidates = append(candidates, entry.Name())
			}
		}
	}
	sort.Strings(candidates)
	for _, name := range candidates {
		path := filepath.Join(parent, name)
		if actual, err := ContentDigest(path); err == nil && actual == digest {
			return path, nil
		}
	}
	path := filepath.Join(parent, key)
	if _, err := os.Lstat(path); err == nil {
		// 不删除损坏旧根。全新repair目录发布失败时，旧位置及原字节仍可检查和回收。
		reserved, err := os.MkdirTemp(parent, key+"-repair-")
		if err != nil {
			return "", storageFailure(err)
		}
		if err := os.Remove(reserved); err != nil {
			return "", storageFailure(err)
		}
		path = reserved
	} else if !os.IsNotExist(err) {
		return "", storageFailure(err)
	}
	if err := os.Rename(stage, path); err != nil {
		return "", storageFailure(err)
	}
	return path, nil
}
