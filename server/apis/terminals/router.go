// Package terminals 装配绑定 Chat 冻结目录及认证主体的交互终端。
package terminals

import "github.com/shrewx/ginx"

// Router 复用根管理认证，终端字节采用独立流。
var Router = ginx.NewRouter(ginx.Group("/terminals"))

func init() {
	Router.Register(&StartTerminal{})
	Router.Register(&CloseTerminal{})
	Router.Register(&StreamTerminal{})
}
