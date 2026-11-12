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
	// LogPath 是 JSONL 权威日志文件路径。
	LogPath string
}

// Catalog 只维护 Chat ID 到 Conversation 日志地址的映射，团队归属读取原创建事实。
type Catalog struct {
	// layout 提供全部规范聚合路径。
	layout *Layout
	// mutex 保护运行期注册、删除和启动扫描结果。
	mutex sync.RWMutex
	// addresses 按 Chat ID 保存唯一日志地址。
	addresses map[string]LogAddress
}

// NewCatalog 只扫描当前 Conversation 布局并拒绝非法身份。
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

// Address 返回由 Chat 身份唯一确定的日志地址，不为查询新增目录项。
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
	return c.address(chatID)
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

// Refresh 重新发现运行期由其他 Store 或崩溃恢复留下的日志地址。
func (c *Catalog) Refresh() error {
	return c.discover()
}

// Remember 将已经落盘的 Chat 日志地址加入运行期目录。
func (c *Catalog) Remember(address LogAddress) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.addresses[address.ChatID] = address
}

func (c *Catalog) address(chatID string) (LogAddress, error) {
	directory, err := c.layout.ChatLogDirectory(chatID)
	if err != nil {
		return LogAddress{}, err
	}
	return LogAddress{
		ChatID: chatID,

		LogPath: filepath.Join(directory, chatID+".jsonl"),
	}, nil
}

func (c *Catalog) discover() error {
	return c.discoverAggregateRoot(filepath.Join(c.layout.Root(), chatDirectoryName))
}

func (c *Catalog) discoverAggregateRoot(root string) error {
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
		logRoot := filepath.Join(root, entry.Name(), logDirectoryName)
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
			if !safeSegment(chatID) || chatID != entry.Name() {
				continue
			}
			address, err := c.address(chatID)
			if err != nil {
				return err
			}
			c.Remember(address)
		}
	}
	return nil
}
