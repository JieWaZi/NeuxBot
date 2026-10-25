// Package authorization 管理插件提供者共享身份的生命周期与安全投影。
package authorization

import "github.com/JieWaZi/NeuxBot/server/constants"

// Account 是可显示的共享身份摘要，不保存或返回登录 token。
type Account struct {
	// 核验时间
	CheckedAt int64 `json:"checkedAt,omitempty"`
	// Configured 表示官方 profile 已完成应用配置。
	Configured bool `json:"configured"`
	// Available 表示最近核验的用户身份与所需权限可用。
	Available bool `json:"available"`
	// AppID 是当前应用身份。
	AppID string `json:"appId,omitempty"`
	// ID 是操作者公开标识。
	ID string `json:"id,omitempty"`
	// Name 是操作者显示名称。
	Name string `json:"name,omitempty"`
	// Scopes 是官方返回的实际权限摘要。
	Scopes []string `json:"scopes"`
}

// 授权提醒
type Notice struct {
	// 提示文案
	Messages map[string]string `json:"messages"`
}

// Session 是本机授权等待的安全投影。
type Session struct {
	// ChallengeID 是本次唯一挑战身份。
	ChallengeID string `json:"challengeId,omitempty"`
	// Actions 是本次协议明确允许的通用交互。
	Actions []Action `json:"actions,omitempty"`
	// 授权提醒
	Notice *Notice `json:"notice,omitempty"`
	// DeadlineAt 是本机等待上限，不表示上游有效期。
	DeadlineAt int64 `json:"deadlineAt,omitempty"`
	// ID 是本次等待身份。
	ID string `json:"id"`
	// State 是经过验证的授权阶段。
	State constants.PluginAuthorizationState `json:"state"`
	// VerificationURL 是官方原样返回的短时确认链接。
	VerificationURL string `json:"verificationUrl,omitempty"`
	// UserCode 是需用户在官方页面输入的一次性验证码。
	UserCode string `json:"userCode,omitempty"`
	// ExpiresAt 是本机等待上限的 Unix 毫秒值。
	ExpiresAt int64 `json:"expiresAt"`
	// Account 是最终确认的共享身份。
	Account Account `json:"account"`
	// ErrorCode 是不含敏感输出的稳定错误标识。
	ErrorCode string `json:"errorCode,omitempty"`
}

// Usage 是一个正在使用共享身份的工作引用，不保存输入或凭据。
type Usage struct {
	// ChatID 是关联对话标识。
	ChatID string `json:"chatId"`
	// ExecutionID 是本轮执行标识。
	ExecutionID string `json:"executionId"`
	// WakerID 是工作所属伙伴。
	WakerID string `json:"wakerId"`
	// Title 是接纳时的工作标题。
	Title string `json:"title"`
}

// Action 是本次挑战提供的临时资料，不从插件名称推导。
type Action struct {
	// Type 是允许的交互动作。
	Type string `json:"type"`
	// URL 是实际授权页面。
	URL string `json:"url,omitempty"`
	// Value 是协议明确提供的二维码或验证码。
	Value string `json:"value,omitempty"`
}
