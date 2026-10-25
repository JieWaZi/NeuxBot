package eventlog

import (
	"errors"
	"os"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
)

// Store 管理应用数据目录中的 Chat JSONL。
//
// 调用方只需要理解追加、游标读取和范围读取；文件锁、崩溃尾行修复、序号校验和
// fsync 均封装在 Store 内部。每个 Chat 独立串行化，不阻塞其他日志。
type Store struct {
	// layout 统一确定普通 Chat 与团队成员 Chat 的聚合路径。
	layout *chatstorage.Layout
	// catalog 维护 Chat ID 到实际日志地址的唯一映射。
	catalog *chatstorage.Catalog
	// stateMutex 保护按 Chat 建立的 Writer 状态。
	stateMutex sync.Mutex
	// states 保存单文件串行锁和最近已确认序号。
	states map[string]*fileState
}

// fileState 保存一个 Chat 日志的进程内追加状态。
type fileState struct {
	// mutex 串行化同一 Chat 的读取、追加和删除。
	mutex sync.Mutex
	// initialized 表示 lastSequence 已经从磁盘确认。
	initialized bool
	// lastSequence 是当前文件最后一个合法事件序号。
	lastSequence int64
	// scope 是最近合法记录之后仍然活跃的执行与调用作用域。
	scope recordScope
	// scopeLogInfo 记录 scope 对应的日志文件身份和版本，文件变化后必须重新恢复作用域。
	scopeLogInfo os.FileInfo
	// indexDirty 表示派生索引需要在下次读取或启动恢复时重建。
	indexDirty bool
	// tailBytesRemoved 是最近一次尾行修复删除的不完整字节数。
	tailBytesRemoved int64
	// tailRepairedAt 是最近一次尾行修复完成的 Unix 毫秒时间。
	tailRepairedAt int64
}

// RepairInfo 保存 Store 对崩溃残留尾行执行的最近一次安全修复事实。
type RepairInfo struct {
	// TailBytesRemoved 是修复时从不完整最后一行删除的字节数。
	TailBytesRemoved int64 `json:"tailBytesRemoved"`
	// RepairedAt 是修复完成的 Unix 毫秒时间。
	RepairedAt int64 `json:"repairedAt"`
}

// NewStore 创建不启动后台任务的 Chat 日志 Store。
func NewStore(root string) (*Store, error) {
	layout, err := chatstorage.NewLayout(root)
	if err != nil {
		return nil, err
	}
	catalog, err := chatstorage.NewCatalog(layout)
	if err != nil {
		return nil, err
	}
	return &Store{
		layout:  layout,
		catalog: catalog,
		states:  make(map[string]*fileState),
	}, nil
}

// Root 返回 Store 使用的 Chat/Team 聚合数据根目录。
func (s *Store) Root() string {
	return s.layout.Root()
}

// RegisterTeamChat 在创建首条日志前把成员 Chat 注册到团队聚合目录。
func (s *Store) RegisterTeamChat(threadID string, chatID string) error {
	_, err := s.catalog.RegisterTeamChat(threadID, chatID)
	return err
}

// LogPath 返回测试和诊断使用的权威 JSONL 路径。
func (s *Store) LogPath(chatID string) (string, error) {
	address, err := s.catalog.Address(chatID)
	if err != nil {
		return "", err
	}
	return address.LogPath, nil
}

// IndexPath 返回测试和诊断使用的派生索引路径。
func (s *Store) IndexPath(chatID string) (string, error) {
	address, err := s.catalog.Address(chatID)
	if err != nil {
		return "", err
	}
	return address.IndexPath, nil
}

// RepairInfo 返回指定 Chat 最近一次安全尾行修复事实。
func (s *Store) RepairInfo(chatID string) (RepairInfo, error) {
	state, err := s.state(chatID)
	if err != nil {
		return RepairInfo{}, err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return RepairInfo{
		TailBytesRemoved: state.tailBytesRemoved,
		RepairedAt:       state.tailRepairedAt,
	}, nil
}

// ListChatIDs 返回日志目录中的全部 Chat 标识。
func (s *Store) ListChatIDs() ([]string, error) {
	if err := s.catalog.Refresh(); err != nil {
		return nil, err
	}
	return s.catalog.ListChatIDs(), nil
}

// ListThreadChatIDs 返回一个团队对话聚合目录中的全部成员 Chat 标识。
func (s *Store) ListThreadChatIDs(threadID string) ([]string, error) {
	return s.catalog.ListThreadChatIDs(threadID)
}

// Forget 在整聚合目录已经 staging 后移除 Chat 的进程内 writer 与路径注册。
func (s *Store) Forget(chatIDs []string) {
	s.stateMutex.Lock()
	for _, chatID := range chatIDs {
		delete(s.states, chatID)
		s.catalog.Remove(chatID)
	}
	s.stateMutex.Unlock()
}

// RefreshCatalog 在删除回滚或启动恢复后重新发现正式聚合目录。
func (s *Store) RefreshCatalog() error {
	return s.catalog.Refresh()
}

// Delete 删除一份 Chat 权威日志。
func (s *Store) Delete(chatID string) error {
	state, err := s.state(chatID)
	if err != nil {
		return err
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if err := os.Remove(s.path(chatID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status_error.ChatLogError
	}
	if err := os.Remove(s.indexPath(chatID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return status_error.ChatLogError
	}
	s.stateMutex.Lock()
	delete(s.states, chatID)
	s.stateMutex.Unlock()
	s.catalog.Remove(chatID)
	return nil
}

// state 返回指定日志的进程内串行化状态。
func (s *Store) state(chatID string) (*fileState, error) {
	if !validChatID(chatID) {
		return nil, status_error.ChatIDInvalid
	}
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	state := s.states[chatID]
	if state == nil {
		state = &fileState{}
		s.states[chatID] = state
	}
	return state, nil
}

// path 返回已经过标识校验的 Chat 日志路径。
func (s *Store) path(chatID string) string {
	address, _ := s.catalog.Address(chatID)
	return address.LogPath
}

// indexPath 返回已经过标识校验的 Chat 派生索引路径。
func (s *Store) indexPath(chatID string) string {
	address, _ := s.catalog.Address(chatID)
	return address.IndexPath
}

// validChatID 限制日志文件名为内部生成的 32 位小写十六进制标识。
func validChatID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if character >= '0' && character <= '9' ||
			character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return true
}
