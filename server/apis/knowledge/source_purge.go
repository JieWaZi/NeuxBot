package knowledge

// 确认来源清理请求
type ConfirmSourcePurgeInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 来源身份
	SourceID string `json:"sourceId" validate:"required"`
	// 影响摘要
	PreviewDigest string `json:"previewDigest" validate:"required"`
	// 明确确认
	Confirmed bool `json:"confirmed"`
}

// 恢复来源清理请求
type RecoverSourcePurgeInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前授权版本
	ExpectedPolicyVersion int64 `json:"expectedPolicyVersion" validate:"min=1"`
}

func init() {
	Router.Register(&PreviewSourcePurge{})
	Router.Register(&ConfirmSourcePurge{})
	Router.Register(&GetSourcePurge{})
	Router.Register(&RecoverSourcePurge{})
}
