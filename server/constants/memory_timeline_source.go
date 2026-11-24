package constants

//go:generate toolx gen enum MemoryTimelineSource

// MemoryTimelineSource 表示记忆时间线事件的操作来源。
type MemoryTimelineSource string

const (
	// MemoryTimelineSourceManual 表示时间线事件来自管理 API 手动操作。
	MemoryTimelineSourceManual MemoryTimelineSource = "manual"
	// MemoryTimelineSourceMCP 表示时间线事件来自执行中的 MCP 工具调用。
	MemoryTimelineSourceMCP MemoryTimelineSource = "mcp"
	// MemoryTimelineSourceReview 表示时间线事件来自自动复盘。
	MemoryTimelineSourceReview MemoryTimelineSource = "review"
)
