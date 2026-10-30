// Package chats 提供真实聊天和可恢复事件流接口。
package chats

import "github.com/shrewx/ginx"

// Router 是 Chat 接口路由。
var Router = ginx.NewRouter(ginx.Group("/chats"))

func init() {
	Router.Register(&ForkChat{})
	Router.Register(&CreateChat{})
	Router.Register(&SendMessage{})
	Router.Register(&RetryChatExecution{})
	Router.Register(&GetChat{})
	Router.Register(&ListChatMessages{})
	Router.Register(&GetChatMessageFileChange{})
	Router.Register(&UpdateChat{})
	Router.Register(&ListChats{})
	Router.Register(&ListChatEvents{})
	Router.Register(&CancelChatExecution{})
	Router.Register(&ResolveApproval{})
	Router.Register(&ResolveElicitation{})
	Router.Register(&DeleteChat{})
}
