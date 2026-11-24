package review

import "github.com/shrewx/ginx"

// Router 提供自动记忆和技能学习的设置及聊天操作。
var Router = ginx.NewRouter(ginx.Group("/review"))

func init() {
	Router.Register(&GetReviewSettings{})
	Router.Register(&UpdateReviewSettings{})
	Router.Register(&ListWakerReviewSettings{})
	Router.Register(&GetWakerReviewSettings{})
	Router.Register(&UpdateWakerReviewSettings{})
	Router.Register(&RetryWakerReview{})
	Router.Register(&ListChatSkillSuggestions{})
	Router.Register(&ResolveChatSkillSuggestion{})
}
