package contract

// UsageData 是 Runtime SDK 归一化后交给 Host 的完整用量数据。
type UsageData struct {
	// Turn 保存当前 Turn 的累计 Token 用量；仅报告上下文时为空。
	Turn *TurnUsageData `json:"turn,omitempty"`
	// Context 保存当前原生 Session 的上下文占用快照；Runtime 不支持时为空。
	Context *ContextUsageData `json:"context,omitempty"`
}

// TurnUsageData 保存一次 Turn 内按统一语义归一化的 Token 用量。
type TurnUsageData struct {
	// InputTokens 是扣除缓存读取后的非缓存输入 Token 数。
	InputTokens int64 `json:"input_tokens,omitempty"`
	// CacheReadTokens 是由 Runtime 缓存命中的输入 Token 数。
	CacheReadTokens int64 `json:"cache_read_tokens,omitempty"`
	// CacheCreationTokens 是本次 Turn 写入 Runtime 缓存的输入 Token 数。
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// OutputTokens 是 Runtime 报告的输出 Token 数。
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// ReasoningOutputTokens 是 Runtime 单独报告的推理输出 Token 数。
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens,omitempty"`
}

// ContextUsageData 保存当前原生 Session 的上下文占用快照。
type ContextUsageData struct {
	// UsedTokens 是当前原生 Session 已占用的上下文 Token 数。
	UsedTokens int64 `json:"used_tokens,omitempty"`
	// WindowTokens 是当前模型可用的上下文窗口上限。
	WindowTokens int64 `json:"window_tokens,omitempty"`
}
