package mcp

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 密钥输入
type SecretInput struct {
	// 标识
	ID string `json:"id,omitempty"`
	// 名称
	Name string `json:"name"`
	// 变量值
	Value string `json:"value,omitempty"`
	// 清除已有值
	Clear bool `json:"clear,omitempty"`
}

// 连接器输入
type ConnectorInput struct {
	// 名称
	Name string `json:"name"`
	// 描述
	Description string `json:"description"`
	// 传输方式
	Transport constants.MCPTransport `json:"transport"`
	// 启动命令
	Command string `json:"command,omitempty"`
	// 命令参数
	Arguments []string `json:"arguments"`
	// 环境变量
	Environment []SecretInput `json:"environment"`
	// 服务地址
	URL string `json:"url,omitempty"`
	// 请求头
	Headers []SecretInput `json:"headers"`
	// 超时毫秒
	TimeoutMS int64 `json:"timeoutMs"`
	// 风险确认
	TrustConfirmed bool `json:"trustConfirmed"`
}

// 密钥定义
type SecretDefinition struct {
	// 标识
	ID string `json:"id"`
	// 名称
	Name string `json:"name"`
	// 已配置
	Configured bool `json:"configured"`
}

// 连接器资源
type ConnectorResource struct {
	// 所属伙伴
	OwnerWakerID string `json:"ownerWakerId,omitempty"`
	// 标识
	ID string `json:"id"`
	// 名称
	Name string `json:"name"`
	// 描述
	Description string `json:"description"`
	// 连接器范围
	Scope constants.MCPConnectorScope `json:"scope"`
	// 挂载策略
	ActivationPolicy constants.MCPActivationPolicy `json:"activationPolicy"`
	// 传输方式
	Transport constants.MCPTransport `json:"transport"`
	// 启动命令
	Command string `json:"command,omitempty"`
	// 命令参数
	Arguments []string `json:"arguments"`
	// 环境变量
	Environment []SecretDefinition `json:"environment"`
	// 服务地址
	URL string `json:"url,omitempty"`
	// 请求头
	Headers []SecretDefinition `json:"headers"`
	// 超时毫秒
	TimeoutMS int64 `json:"timeoutMs"`
	// 确认时间
	TrustedAt int64 `json:"trustedAt"`
	// 测试状态
	TestStatus constants.MCPTestStatus `json:"testStatus"`
	// 测试时间
	TestedAt int64 `json:"testedAt"`
	// 测试耗时
	TestDurationMS int64 `json:"testDurationMs"`
	// 工具数量
	TestToolCount int `json:"testToolCount"`
	// 测试错误
	TestError string `json:"testError,omitempty"`
	// 工具列表
	TestTools []models.MCPToolSummary `json:"testTools"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
}

// 连接器响应
type ConnectorResponse struct {
	// 连接器
	Connector ConnectorResource `json:"connector"`
	// 启用状态
	Enabled bool `json:"enabled"`
	// 影响伙伴数
	AffectedWakerCount int64 `json:"affectedWakerCount"`
	// 兼容结论
	Compatibility []RuntimeCompatibility `json:"compatibility"`
}

// 运行时兼容
type RuntimeCompatibility struct {
	// 运行时标识
	RuntimeID string `json:"runtimeId"`
	// 是否支持
	Supported bool `json:"supported"`
	// 原因码
	Reason string `json:"reason,omitempty"`
}
