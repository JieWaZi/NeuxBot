// Package run_viewing 提供原运行的查看状态。
package run_viewing

import "github.com/shrewx/ginx"

// Router 按原运行身份注册结果查看接口。
var Router = ginx.NewRouter(ginx.Group("/runs"))

func init() { Router.Register(&GetRunViewing{}); Router.Register(&MarkRunViewed{}) }
