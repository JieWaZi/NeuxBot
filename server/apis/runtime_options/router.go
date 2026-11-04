// Package runtime_options 提供聊天 Runtime 探测接口。
package runtime_options

import "github.com/shrewx/ginx"

// Router 是 Runtime 选项接口路由。
var Router = ginx.NewRouter(ginx.Group("/runtimes"))

func init() {
	Router.Register(&ListRuntimes{})
	Router.Register(&GetRuntimeModelCatalog{})
	Router.Register(&RefreshRuntimes{})
	Router.Register(&RefreshRuntime{})
	Router.Register(&UpdateRuntimeEnvironment{})
}
