// Package websession 提供本机 Web 的会话建立、读取和退出入口。
package websession

import "github.com/shrewx/ginx"

// Router 与 Bearer 管理 API 共用 Core，会话建立入口校验本机来源。
var Router = ginx.NewRouter(ginx.Group("/api/web"))

func init() {
	Router.Register(&CreateSession{})
	Router.Register(&GetSession{})
	Router.Register(&DeleteSession{})
}
