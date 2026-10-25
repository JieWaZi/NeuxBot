package constants

//go:generate toolx gen enum IMPlatform

// IMPlatform 定义 IM 集成的稳定业务值。
type IMPlatform string

const (
	// IMPlatformUnspecified 表示未选择平台。
	IMPlatformUnspecified IMPlatform = ""
	// IMPlatformWeixin 表示个人微信 iLink 机器人。
	IMPlatformWeixin IMPlatform = "weixin"
	// IMPlatformWecom 表示企业微信智能机器人。
	IMPlatformWecom IMPlatform = "wecom"
	// IMPlatformFeishu 表示飞书企业机器人。
	IMPlatformFeishu IMPlatform = "feishu"
)
