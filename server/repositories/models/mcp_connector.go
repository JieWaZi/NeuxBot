package models

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func init() {
	dbhelper.RegisterTable(&MCPConnector{})
	dbhelper.RegisterTable(&WakerMCPConnector{})
	dbhelper.RegisterTable(&MCPStorageKey{})
}

// MCPSecretDefinition 保存一个敏感配置项的非明文定义。
type MCPSecretDefinition struct {
	// ID 是配置项在连接器内稳定不变的标识。
	ID string `json:"id"`
	// Name 是环境变量名或请求头名。
	Name string `json:"name"`
	// Ciphertext 是数据库中保存的加密敏感值。
	Ciphertext string `json:"ciphertext,omitempty"`
	// Configured 表示数据库中是否已保存对应加密值。
	Configured bool `json:"configured"`
	// Value 是 Controller 解密后仅在进程内使用的敏感值。
	Value string `json:"-"`
}

// MCPToolSummary 保存连接测试发现的有界工具展示信息。
type MCPToolSummary struct {
	// Name 是 MCP 服务声明的工具名称。
	Name string `json:"name"`
	// Description 是 MCP 服务声明的工具简短说明。
	Description string `json:"description"`
}

// MCPConnector 保存一个全局共享 MCP 连接器的权威普通配置。
type MCPConnector struct {
	// ID 是 32 位无连字符 UUID，仅用于 NeuxBot 内部关联连接器。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// Name 是连接器显示名称，不参与资源身份判断。
	Name string `gorm:"column:name;type:varchar(120);not null" json:"name"`
	// RuntimeKey 是创建时生成并持久化的 Runtime 注入名称，后续不随显示名称变化。
	RuntimeKey string `gorm:"column:runtime_key;type:varchar(64);not null;uniqueIndex" json:"-"`
	// Description 是连接器的简短用途说明。
	Description string `gorm:"column:description;type:text;not null;default:''" json:"description"`
	// OwnerWakerID 是自有连接器所属伙伴，全局资源为空。
	OwnerWakerID string `gorm:"column:owner_waker_id;not null;default:'';index" json:"ownerWakerId,omitempty"`
	// Scope 是连接器的内置、全局或伙伴自有范围。
	Scope constants.MCPConnectorScope `gorm:"column:scope;type:varchar(16);not null;index" json:"scope"`
	// Transport 是连接器使用的 MCP 传输方式。
	Transport constants.MCPTransport `gorm:"column:transport;type:varchar(32);not null;index" json:"transport"`
	// Command 是 STDIO 传输启动的可执行命令。
	Command string `gorm:"column:command;type:text;not null;default:''" json:"command,omitempty"`
	// Arguments 是传给 STDIO 命令的有序参数。
	Arguments []string `gorm:"column:arguments;type:text;not null;serializer:json" json:"arguments"`
	// Environment 是 STDIO 环境变量的非明文定义。
	Environment []MCPSecretDefinition `gorm:"column:environment;type:text;not null;serializer:json" json:"environment"`
	// URL 是 Streamable HTTP 或 SSE 服务地址。
	URL string `gorm:"column:url;type:text;not null;default:''" json:"url,omitempty"`
	// Headers 是远程请求头的非明文定义。
	Headers []MCPSecretDefinition `gorm:"column:headers;type:text;not null;serializer:json" json:"headers"`
	// TimeoutMS 是 NeuxBot 连接测试使用的超时；支持时也传给 Runtime 作为启动超时。
	TimeoutMS int64 `gorm:"column:timeout_ms;not null;default:10000" json:"timeoutMs"`
	// TrustedConfigDigest 是最近一次用户确认的敏感执行配置摘要。
	TrustedConfigDigest string `gorm:"column:trusted_config_digest;type:varchar(64);not null;default:''" json:"-"`
	// TrustedAt 是最近一次用户确认连接器执行风险的 Unix 毫秒时间。
	TrustedAt int64 `gorm:"column:trusted_at;not null;default:0" json:"trustedAt"`
	// TestStatus 是当前配置最近一次连接测试的状态。
	TestStatus constants.MCPTestStatus `gorm:"column:test_status;type:varchar(16);not null;default:'untested'" json:"testStatus"`
	// TestedAt 是最近一次连接测试完成的 Unix 毫秒时间。
	TestedAt int64 `gorm:"column:tested_at;not null;default:0" json:"testedAt"`
	// TestDurationMS 是最近一次连接测试耗时的毫秒数。
	TestDurationMS int64 `gorm:"column:test_duration_ms;not null;default:0" json:"testDurationMs"`
	// TestToolCount 是最近一次成功测试发现的工具数量。
	TestToolCount int `gorm:"column:test_tool_count;not null;default:0" json:"testToolCount"`
	// TestError 是最近一次失败测试的有界错误摘要。
	TestError string `gorm:"column:test_error;type:text;not null;default:''" json:"testError,omitempty"`
	// TestTools 是最近一次成功测试发现的工具名称和描述。
	TestTools []MCPToolSummary `gorm:"column:test_tools;type:text;not null;serializer:json" json:"testTools"`
	// CreatedAt 是连接器创建时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是连接器普通配置最近更新时间的 Unix 毫秒值。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回 MCP 连接器模型使用的表名。
func (MCPConnector) TableName() string { return "mcp_connectors" }

// WakerMCPConnector 保存伙伴对共享 MCP 连接器的启停关系。
type WakerMCPConnector struct {
	// ID 是 32 位无连字符 UUID。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// WakerID 是伙伴标识。
	WakerID string `gorm:"column:waker_id;type:varchar(32);not null;uniqueIndex:idx_waker_mcp_connector" json:"wakerId"`
	// MCPConnectorID 是共享 MCP 连接器标识。
	MCPConnectorID string `gorm:"column:mcp_connector_id;type:varchar(32);not null;uniqueIndex:idx_waker_mcp_connector" json:"mcpConnectorId"`
	// Enabled 表示该伙伴是否在后续执行中使用连接器。
	Enabled bool `gorm:"column:enabled;not null;default:false" json:"enabled"`
	// CreatedAt 是关系首次建立时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
	// UpdatedAt 是关系最近更新时间的 Unix 毫秒值。
	UpdatedAt int64 `gorm:"column:updated_at;autoUpdateTime:milli" json:"updatedAt"`
}

// TableName 返回伙伴 MCP 连接器关系使用的表名。
func (WakerMCPConnector) TableName() string { return "waker_mcp_connectors" }

// MCPStorageKey 保存当前数据库加密 MCP 敏感字段使用的随机密钥。
type MCPStorageKey struct {
	// ID 是当前数据库唯一的固定密钥标识。
	ID string `gorm:"column:id;type:varchar(32);primaryKey" json:"id"`
	// EncodedKey 是使用 Base64 编码的 256 位 AES 密钥。
	EncodedKey string `gorm:"column:encoded_key;type:text;not null" json:"encodedKey"`
	// CreatedAt 是密钥首次生成时间的 Unix 毫秒值。
	CreatedAt int64 `gorm:"column:created_at;autoCreateTime:milli" json:"createdAt"`
}

// TableName 返回 MCP 数据库加密密钥模型使用的表名。
func (MCPStorageKey) TableName() string { return "mcp_storage_keys" }

// MCPConnectorWithState 是面向伙伴查询时合并连接器和启用状态的结果。
type MCPConnectorWithState struct {
	// Connector 是共享 MCP 连接器的权威普通配置。
	Connector MCPConnector `json:"connector"`
	// Enabled 表示当前伙伴是否启用该连接器。
	Enabled bool `json:"enabled"`
}
