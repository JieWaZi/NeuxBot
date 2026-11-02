package constants

//go:generate toolx gen enum RuntimeCapability

// RuntimeCapability 是 Runtime 探测得到的能力标识。
type RuntimeCapability string

const (
	// RuntimeCapabilityUnspecified 表示未指定 Runtime 能力。
	RuntimeCapabilityUnspecified RuntimeCapability = ""
	// RuntimeCapabilityStructuredOutput 表示 Runtime 支持要求的结构化输出格式。
	RuntimeCapabilityStructuredOutput RuntimeCapability = "structured_output"
	// RuntimeCapabilityResumeSession 表示 Runtime 支持恢复原生 Session。
	RuntimeCapabilityResumeSession RuntimeCapability = "resume_session"
	// RuntimeCapabilityDynamicModels 表示本次探测取得了动态模型列表。
	RuntimeCapabilityDynamicModels RuntimeCapability = "dynamic_models"
	// RuntimeCapabilityStreamingUserMessages 表示活跃调用可以继续接收用户消息。
	RuntimeCapabilityStreamingUserMessages RuntimeCapability = "streaming_user_messages"
	// RuntimeCapabilityAddDirectory 表示 Runtime 支持声明附加工作目录。
	RuntimeCapabilityAddDirectory RuntimeCapability = "add_directory"
	// RuntimeCapabilityPartialMessages 表示 Runtime 支持更细粒度的流式消息。
	RuntimeCapabilityPartialMessages RuntimeCapability = "partial_messages"
	// RuntimeCapabilityImagePath 表示 Runtime 支持通过本地路径原生提交图片。
	RuntimeCapabilityImagePath RuntimeCapability = "image_path"
	// RuntimeCapabilitySkillDirectory 表示 Runtime 支持从调用方指定目录原生加载技能。
	RuntimeCapabilitySkillDirectory RuntimeCapability = "skill_directory"
	// RuntimeCapabilityMCPStreamableHTTP 表示 ACP Agent 声明支持 Streamable HTTP MCP。
	RuntimeCapabilityMCPStreamableHTTP RuntimeCapability = "mcp_streamable_http"
	// RuntimeCapabilityMCPSSE 表示 ACP Agent 声明支持旧版 SSE MCP。
	RuntimeCapabilityMCPSSE RuntimeCapability = "mcp_sse"
)
