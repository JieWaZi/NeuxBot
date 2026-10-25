package knowledge

import (
	"github.com/JieWaZi/neux-knowledge/core"
)

// SourcePurgeReview 仅暴露清理后需要人工核查的来源身份，不透传自由元数据。
type SourcePurgeReview struct {
	// 需要核查
	NeedsReview bool `json:"needsReview"`
	// 不可用来源
	UnavailableSourceIDs []string `json:"unavailableSourceIds"`
	// 不可用版本
	UnavailableVersionIDs []string `json:"unavailableVersionIds"`
	// 清理操作
	PurgeIDs []string `json:"purgeIds"`
}

// SourcePurgeReviewFromMetadata 白名单投影本地与自有远端的相同核心事实。
func SourcePurgeReviewFromMetadata(metadata core.JSON) *SourcePurgeReview {
	value := core.ParseSourcePurgeReview(metadata)
	if value == nil {
		return nil
	}
	return &SourcePurgeReview{NeedsReview: value.NeedsReview, UnavailableSourceIDs: value.UnavailableSourceIDs, UnavailableVersionIDs: value.UnavailableVersionIDs, PurgeIDs: value.PurgeIDs}
}
