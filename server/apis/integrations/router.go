package integrations

import "github.com/shrewx/ginx"

// Router 仅注册到本机管理认证组，不进入公开发布服务。
var Router = ginx.NewRouter(ginx.Group("/integrations"))

func init() {
	Router.Register(&ListAPIKeys{})
	Router.Register(&CreateAPIKey{})
	Router.Register(&UpdateAPIKey{})
	Router.Register(&DeleteAPIKey{})
	Router.Register(&GetAPIService{})
	Router.Register(&UpdateAPIService{})
	Router.Register(&UpdateMCPService{})
	Router.Register(&ListPublishableResources{})
}
