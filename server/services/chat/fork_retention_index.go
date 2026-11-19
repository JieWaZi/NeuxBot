package chat

import (
	"sync"

	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
)

type pendingForkIndex struct {
	// mu 保护恢复完成标志和正在变化的分叉引用。
	mu sync.RWMutex
	// ready 表示启动恢复已核对全部持久分叉操作。
	ready bool
	// byDirectory 保存未完成操作的双方身份，用于幂等维护投影。
	byDirectory map[string][2]string
	// byChat 统计每个 Chat 的未完成源或目标引用。
	byChat map[string]int
}

func (index *pendingForkIndex) replace(operations map[string][2]string) {
	index.mu.Lock()
	defer index.mu.Unlock()
	index.byDirectory = operations
	index.byChat = make(map[string]int, len(operations))
	for _, pair := range operations {
		for _, id := range pair {
			index.byChat[id]++
		}
	}
	index.ready = true
}

func (index *pendingForkIndex) update(directory string, operation chatstorage.ForkOperation) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if !index.ready {
		return
	}
	old, exists := index.byDirectory[directory]
	if operation.Published || operation.Cancelled {
		if exists {
			delete(index.byDirectory, directory)
			for _, id := range old {
				index.byChat[id]--
				if index.byChat[id] == 0 {
					delete(index.byChat, id)
				}
			}
		}
	} else if !exists {
		pair := [2]string{operation.ChatID, operation.SourceChatID}
		index.byDirectory[directory] = pair
		for _, id := range pair {
			index.byChat[id]++
		}
	}
}

func (index *pendingForkIndex) chatIDs() ([]string, bool) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	ids := make([]string, 0, len(index.byChat))
	for id := range index.byChat {
		ids = append(ids, id)
	}
	return ids, index.ready
}

func (index *pendingForkIndex) contains(chatID string) (bool, bool) {
	index.mu.RLock()
	defer index.mu.RUnlock()
	return index.byChat[chatID] > 0, index.ready
}

func (m *service) writeForkOperation(directory string, operation chatstorage.ForkOperation) error {
	terminal := operation.Published || operation.Cancelled
	if !terminal {
		// Rename 后目录同步失败时操作也可能存在，写入前先保守保护双方。
		m.pendingForks.update(directory, operation)
	}
	if err := chatstorage.WriteForkOperation(directory, operation); err != nil {
		return err
	}
	if terminal {
		m.pendingForks.update(directory, operation)
	}
	return nil
}
