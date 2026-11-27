// Package main_conversations 提供伙伴与团队共同使用的主会话入口。
package main_conversations

import "github.com/shrewx/ginx"

// Router 注册本机授权下的主会话命令和查询。
var Router = ginx.NewRouter(ginx.Group("/main-conversations"))

func init() {
	Router.Register(&GetMainConversation{})
	Router.Register(&GetMainConversationReading{})
	Router.Register(&MarkMainConversationRead{})
	Router.Register(&MarkMainConversationUnread{})
	Router.Register(&SendMainConversationMessage{})
	Router.Register(&DeleteMainConversationMessage{})
	Router.Register(&GetMainConversationSource{})
	Router.Register(&SendMainConversationSourceMessage{})
	Router.Register(&ResolveMainConversationSourceApproval{})
	Router.Register(&ResolveMainConversationSourceElicitation{})
}
