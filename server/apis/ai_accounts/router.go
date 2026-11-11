// Package ai_accounts 提供受管 AI 账号的基本管理接口。
package ai_accounts

import "github.com/shrewx/ginx"

// Router 是设置账号管理的资源路由。
var Router = ginx.NewRouter(ginx.Group("/ai-accounts"))

func init() {
	Router.Register(&ListAIAccounts{})
	Router.Register(&RefreshAIAccountModels{})
	Router.Register(&GetAIAccountCredential{})
	Router.Register(&LogoutAIAccount{})
	Router.Register(&PreviewAIAccountModels{})
	Router.Register(&CreateAIAccount{})
	Router.Register(&UpdateAIAccount{})
	Router.Register(&DeleteAIAccount{})
	Router.Register(&StartAIAccountLogin{})
	Router.Register(&GetAIAccountLogin{})
	Router.Register(&CancelAIAccountLogin{})
}
