package plugins

import (
	"context"
	"slices"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// connectorDiagnostic 是一次已脱敏真实探测的进程内记录，不创建独立连接器或持久化凭据。
type connectorDiagnostic struct {
	// Connector 仅保存公开测试字段及其原始连接器展示资料。
	Connector mcp.ConnectorResource
	// Revision 固定探测使用的共享配置修订。
	Revision int64
	// Provider 只标识旧版共享授权来源。
	Provider string
	// BridgeDigest 固定本次使用的声明式身份桥接。
	BridgeDigest string
	// AccountID 固定核验后的公开账号身份。
	AccountID string
	// Scopes 固定核验后的授权范围。
	Scopes []string
}

// connectorDiagnosticStore 保护有界的最近探测结果，普通读取不会执行 MCP 或授权。
type connectorDiagnosticStore struct {
	// mu 保护探测结果的读写和有界回收。
	mu sync.Mutex
	// entries 以数据根、插件、不可变版本与组件形成唯一查看身份。
	entries map[string]connectorDiagnostic
}

var pluginConnectorDiagnostics = connectorDiagnosticStore{entries: map[string]connectorDiagnostic{}}

func connectorDiagnosticKey(plugin, version, key string) string {
	return global.Config.DataDirectory + "\x00" + plugin + "\x00" + version + "\x00" + key
}

func (s *connectorDiagnosticStore) put(key string, value connectorDiagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 诊断仅属于当前 Core，淘汰最早记录，不维护第二份安装或连接器事实。
	if len(s.entries) >= constants.PluginConnectorDiagnosticCacheLimit {
		oldest := ""
		var timestamp int64
		for candidate, entry := range s.entries {
			if oldest == "" || entry.Connector.TestedAt < timestamp {
				oldest, timestamp = candidate, entry.Connector.TestedAt
			}
		}
		delete(s.entries, oldest)
	}
	value.Connector.TestTools = slices.Clone(value.Connector.TestTools)
	value.Scopes = slices.Clone(value.Scopes)
	s.entries[key] = value
}

func (s *connectorDiagnosticStore) apply(ctx context.Context, ctxVersion models.PluginVersion, key string, result *mcp.ConnectorResponse) error {
	identity := connectorDiagnosticKey(ctxVersion.PluginID, ctxVersion.ID, key)
	s.mu.Lock()
	entry, exists := s.entries[identity]
	s.mu.Unlock()
	if !exists {
		return nil
	}
	config, err := global.PluginControllerManager.Configuration(ctx, ctxVersion.PluginID)
	if err != nil {
		return err
	}
	valid := entry.Revision == config.Revision
	if entry.Provider != "" || entry.BridgeDigest != "" {
		account := authorization.Account{}
		if entry.BridgeDigest != "" && global.PluginBridges != nil {
			account = global.PluginBridges.CachedAccount(ctxVersion.PluginID, entry.BridgeDigest)
		} else if auth := global.PluginAuthorizations[entry.Provider]; auth != nil {
			account = auth.CachedAccount()
		}
		valid = valid && account.Available && account.ID == entry.AccountID && slices.Equal(account.Scopes, entry.Scopes)
	}
	if !valid {
		s.mu.Lock()
		if current, ok := s.entries[identity]; ok && current.Connector.TestedAt == entry.Connector.TestedAt {
			delete(s.entries, identity)
		}
		s.mu.Unlock()
		return nil
	}
	result.Connector.TestStatus = entry.Connector.TestStatus
	result.Connector.TestedAt = entry.Connector.TestedAt
	result.Connector.TestDurationMS = entry.Connector.TestDurationMS
	result.Connector.TestToolCount = entry.Connector.TestToolCount
	result.Connector.TestTools = slices.Clone(entry.Connector.TestTools)
	result.Connector.TestError = entry.Connector.TestError
	result.Connector.TimeoutMS = entry.Connector.TimeoutMS
	return nil
}
