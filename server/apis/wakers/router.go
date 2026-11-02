// Package wakers 提供伙伴 CRUD 接口。
package wakers

import "github.com/shrewx/ginx"

// Router 是伙伴接口路由。
var Router = ginx.NewRouter(ginx.Group("/wakers"))

func init() {
	Router.Register(&ListWakers{})
	Router.Register(&CreateWaker{})
	Router.Register(&GetWaker{})
	Router.Register(&GetWakerWorkRecord{})
	Router.Register(&ListWakerChats{})
	Router.Register(&GetWakerRecentProject{})
	Router.Register(&UpdateWaker{})
	Router.Register(&DeleteWaker{})
	Router.Register(&ListWakerDocuments{})
	Router.Register(&GetWakerDocument{})
	Router.Register(&UpdateWakerDocument{})
	Router.Register(&ExportWakerProfile{})
}
