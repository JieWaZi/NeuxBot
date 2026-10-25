package chatstorage

import (
	"path/filepath"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// operations 串行化同一受管 Chat 的发送准备、分叉和删除入口。
var operations = struct {
	// Mutex 保护所有正在进行的短期 Chat 操作。
	sync.Mutex
	// busy 按数据根目录和 Chat 身份隔离不同应用实例。
	busy map[string]bool
}{busy: map[string]bool{}}

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
		if operations.busy[key] {
			return nil, status_error.ChatHasActiveExecution
		}
		keys[key] = true
	}
	for key := range keys {
		operations.busy[key] = true
	}
	return func() {
		operations.Lock()
		defer operations.Unlock()
		for key := range keys {
			delete(operations.busy, key)
		}
	}, nil
}
