package chatstorage

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// operations 串行化同一受管 Chat 的发送准备、分叉和删除入口。
var operations = struct {
	// Mutex 保护所有正在进行的短期 Chat 操作。
	sync.Mutex
	// busy 按数据根目录和资源身份保存排他操作。
	busy map[string]bool
	// admissions 统计短期伙伴接纳与会话引用提交，不表示原生执行状态。
	admissions map[string]int
}{busy: map[string]bool{}, admissions: map[string]int{}}

// AcquireOperation 尝试独占一组 Chat；不等待，避免与执行收尾形成锁环。
func AcquireOperation(root string, chatIDs ...string) (func(), error) {
	operations.Lock()
	defer operations.Unlock()
	keys := make(map[string]bool, len(chatIDs))
	for _, id := range chatIDs {
		if id == "" {
			continue
		}
		if !safeSegment(id) {
			return nil, status_error.BadRequest
		}
		key := filepath.Clean(root) + "\x00" + id
		if operations.busy[key] || operations.admissions[key] > 0 {
			return nil, status_error.ChatHasActiveExecution
		}
		keys[key] = true
	}
	for key := range keys {
		operations.busy[key] = true
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			operations.Lock()
			defer operations.Unlock()
			for key := range keys {
				delete(operations.busy, key)
			}
		})
	}, nil
}

type wakerDeletionKey struct{}

type wakerDeletionClaim struct {
	// root 是操作门规范化后的受管根。
	root string
	// wakerID 是获准延续删除阶段的伙伴身份。
	wakerID string
	// active 防止已释放的上下文再次越过伙伴操作门。
	active atomic.Bool
}

// AcquireWakerDeletion 独占伙伴删除操作，允许内层删除阶段复用同一有效声明。
func AcquireWakerDeletion(ctx context.Context, root, wakerID string) (context.Context, func(), error) {
	root, wakerID = filepath.Clean(root), strings.TrimSpace(wakerID)
	if !safeSegment(wakerID) {
		return ctx, nil, status_error.BadRequest
	}
	if claim, _ := ctx.Value(wakerDeletionKey{}).(*wakerDeletionClaim); claim != nil &&
		claim.root == root && claim.wakerID == wakerID && claim.active.Load() {
		return ctx, func() {}, nil
	}
	release, err := AcquireOperation(root, "waker:"+wakerID)
	if err != nil {
		return ctx, nil, err
	}
	claim := &wakerDeletionClaim{root: root, wakerID: wakerID}
	claim.active.Store(true)
	return context.WithValue(ctx, wakerDeletionKey{}, claim), func() {
		claim.active.Store(false)
		release()
	}, nil
}

// AcquireAdmission 同时登记伙伴共享接纳和 Chat 排他准备，与伙伴删除共用同一操作门。
func AcquireAdmission(root, wakerID string, chatIDs ...string) (func(), error) {
	if !safeSegment(wakerID) {
		return nil, status_error.BadRequest
	}
	operations.Lock()
	defer operations.Unlock()
	prefix := filepath.Clean(root) + "\x00"
	wakerKey := prefix + "waker:" + wakerID
	if operations.busy[wakerKey] {
		return nil, status_error.ChatHasActiveExecution
	}
	keys := make(map[string]bool, len(chatIDs))
	for _, id := range chatIDs {
		if id == "" {
			continue
		}
		if !safeSegment(id) {
			return nil, status_error.BadRequest
		}
		key := prefix + id
		if operations.busy[key] {
			return nil, status_error.ChatHasActiveExecution
		}
		keys[key] = true
	}
	operations.admissions[wakerKey]++
	for key := range keys {
		operations.busy[key] = true
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			operations.Lock()
			defer operations.Unlock()
			for key := range keys {
				delete(operations.busy, key)
			}
			operations.admissions[wakerKey]--
			if operations.admissions[wakerKey] == 0 {
				delete(operations.admissions, wakerKey)
			}
		})
	}, nil
}

// AcquireReferences 在同一操作门内登记领域引用写入。并行引用可共享目标，排他删除须等所有登记者释放。
func AcquireReferences(root string, chatIDs ...string) (func(), error) {
	operations.Lock()
	defer operations.Unlock()
	keys := make(map[string]bool, len(chatIDs))
	for _, id := range chatIDs {
		if id == "" {
			continue
		}
		if !safeSegment(id) {
			return nil, status_error.BadRequest
		}
		key := filepath.Clean(root) + "\x00" + id
		if operations.busy[key] {
			return nil, status_error.ChatHasActiveExecution
		}
		keys[key] = true
	}
	for key := range keys {
		operations.admissions[key]++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			operations.Lock()
			defer operations.Unlock()
			for key := range keys {
				operations.admissions[key]--
				if operations.admissions[key] == 0 {
					delete(operations.admissions, key)
				}
			}
		})
	}, nil
}

// BusyChatIDs 返回当前根中不能获取排他删除门的会话身份快照。
func BusyChatIDs(root string) []string {
	operations.Lock()
	defer operations.Unlock()
	var ids []string
	prefix := filepath.Clean(root) + "\x00"
	add := func(key string) {
		if id, ok := strings.CutPrefix(key, prefix); ok && !strings.HasPrefix(id, "waker:") {
			ids = append(ids, id)
		}
	}
	for key := range operations.busy {
		add(key)
	}
	for key := range operations.admissions {
		add(key)
	}
	return ids
}
