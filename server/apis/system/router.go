// Package system 提供 Core 健康和版本信息。
package system

import "github.com/shrewx/ginx"

// Router 是系统接口路由。
var Router = ginx.NewRouter(ginx.Group("/system"))

func init() {
	Router.Register(&GetHealth{})
	Router.Register(&GetVersion{})
	Router.Register(&SetAutomaticTaskSuspended{})
}
