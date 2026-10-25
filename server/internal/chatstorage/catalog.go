package chatstorage

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// LogAddress 保存一份 Chat 权威日志及其派生文件的稳定地址。
type LogAddress struct {
	// ChatID 是日志所属 Chat 的不可变标识。
	ChatID string
	// ThreadID 非空时表示日志属于团队对话聚合目录。
	ThreadID string
	// LogPath 是 JSONL 权威日志文件路径。
	LogPath string
	// IndexPath 是 JSONL 文件位置索引路径。
	IndexPath string
}

// Catalog 维护 Chat ID 到普通或团队聚合日志地址的唯一映射。
type Catalog struct {
	// layout 提供全部规范聚合路径。
	layout *Layout
	// mutex 保护运行期注册、删除和启动扫描结果。
	mutex sync.RWMutex
	// addresses 按 Chat ID 保存唯一日志地址。
	addresses map[string]LogAddress
}

// NewCatalog 扫描当前布局并拒绝重复 Chat ID 或非法聚合目录。
func NewCatalog(layout *Layout) (*Catalog, error) {
	if layout == nil {
		return nil, status_error.BadRequest
	}
	catalog := &Catalog{
		layout:    layout,
		addresses: make(map[string]LogAddress),
	}
	if err := catalog.discover(); err != nil {
		return nil, err
	}
	return catalog, nil
}

// Address 返回 Chat 当前注册地址；未创建的 Chat 默认落入普通 Chat 聚合目录。
func (c *Catalog) Address(chatID string) (LogAddress, error) {
	if !safeSegment(chatID) {
		return LogAddress{}, status_error.ChatIDInvalid
	}
	c.mutex.RLock()
	address, exists := c.addresses[chatID]
	c.mutex.RUnlock()
	if exists {
		return address, nil
	}
	return c.register(chatID, "")
}

// RegisterTeamChat 在创建首条事实前把成员 Chat 绑定到团队聚合目录。
func (c *Catalog) RegisterTeamChat(threadID string, chatID string) (LogAddress, error) {
	if !safeSegment(threadID) || !safeSegment(chatID) {
		return LogAddress{}, status_error.BadRequest
	}
	return c.register(chatID, threadID)
}

// Remove 删除一份 Chat 的运行期地址；文件删除由日志 Store 负责。
func (c *Catalog) Remove(chatID string) {
	c.mutex.Lock()
	delete(c.addresses, chatID)
	c.mutex.Unlock()
}

// ListChatIDs 返回当前发现或注册的全部 Chat 标识。
func (c *Catalog) ListChatIDs() []string {
	c.mutex.RLock()
	ids := make([]string, 0, len(c.addresses))
	for chatID := range c.addresses {
		ids = append(ids, chatID)
	}
	c.mutex.RUnlock()
	sort.Strings(ids)
	return ids
}

// ListThreadChatIDs 返回一个团队对话聚合目录中的全部成员 Chat 标识。
func (c *Catalog) ListThreadChatIDs(threadID string) ([]string, error) {
	if !safeSegment(threadID) {
		return nil, status_error.BadRequest
	}
	if err := c.Refresh(); err != nil {
		return nil, err
	}
	c.mutex.RLock()
	ids := make([]string, 0)
	for chatID, address := range c.addresses {
		if address.ThreadID == threadID {
			ids = append(ids, chatID)
		}
	}
	c.mutex.RUnlock()
	sort.Strings(ids)
	return ids, nil
}

// Refresh 重新发现运行期由其他 Store 或崩溃恢复留下的日志地址。
func (c *Catalog) Refresh() error {
	return c.discover()
}

func (c *Catalog) register(chatID string, threadID string) (LogAddress, error) {
	address, err := c.address(chatID, threadID)
	if err != nil {
		return LogAddress{}, err
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if current, exists := c.addresses[chatID]; exists {
		if current.ThreadID != threadID || current.LogPath != address.LogPath {
			return LogAddress{}, status_error.ChatLogCorrupted
		}
		return current, nil
	}
	c.addresses[chatID] = address
	return address, nil
}

func (c *Catalog) address(chatID string, threadID string) (LogAddress, error) {
	var directory string
	var err error
	if threadID == "" {
		directory, err = c.layout.ChatLogDirectory(chatID)
	} else {
		directory, err = c.layout.TeamLogDirectory(threadID)
	}
	if err != nil {
		return LogAddress{}, err
	}
	return LogAddress{
		ChatID:    chatID,
		ThreadID:  threadID,
		LogPath:   filepath.Join(directory, chatID+".jsonl"),
		IndexPath: filepath.Join(directory, chatID+".idx"),
	}, nil
}

func (c *Catalog) discover() error {
	chatRoot := filepath.Join(c.layout.Root(), chatDirectoryName)
	if err := c.discoverAggregateRoot(chatRoot, false); err != nil {
		return err
	}
	teamRoot := filepath.Join(c.layout.Root(), teamDirectoryName)
	return c.discoverAggregateRoot(teamRoot, true)
}

func (c *Catalog) discoverAggregateRoot(root string, team bool) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		logx.Errorf("scan chat storage root failed: root=%s error=%v", root, err)
		return status_error.ChatLogError
	}
	for _, entry := range entries {
		if !entry.IsDir() || !safeSegment(entry.Name()) {
			continue
		}
		threadID := ""
		logRoot := filepath.Join(root, entry.Name(), logDirectoryName)
		if team {
			threadID = entry.Name()
		}
		logs, readErr := os.ReadDir(logRoot)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			logx.Errorf("scan aggregate chat logs failed: root=%s error=%v", logRoot, readErr)
			return status_error.ChatLogError
		}
		for _, logEntry := range logs {
			if logEntry.IsDir() || filepath.Ext(logEntry.Name()) != ".jsonl" {
				continue
			}
			chatID := strings.TrimSuffix(logEntry.Name(), ".jsonl")
			if !safeSegment(chatID) || (!team && chatID != entry.Name()) {
				continue
			}
			if _, err := c.register(chatID, threadID); err != nil {
				return err
			}
		}
	}
	return nil
}
