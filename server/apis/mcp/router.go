// Package mcp 提供伙伴 MCP 管理接口。
package mcp

import "github.com/shrewx/ginx"

// Router 是伙伴 MCP 接口路由。
var Router = ginx.NewRouter(ginx.Group("/wakers"))

func init() {
	Router.Register(&ListWakerConnectors{})
	Router.Register(&CreateWakerConnector{})
	Router.Register(&GetWakerConnector{})
	Router.Register(&UpdateWakerConnector{})
	Router.Register(&DeleteWakerConnector{})
	Router.Register(&SetWakerConnectorEnabled{})
	Router.Register(&TestWakerConnector{})
}

// LibraryRouter 提供全局资源专业操作。
var LibraryRouter = ginx.NewRouter(ginx.Group("/connectors"))

func init() {
	LibraryRouter.Register(&GetLibraryConnector{})
	LibraryRouter.Register(&CreateLibraryConnector{})
	LibraryRouter.Register(&UpdateLibraryConnector{})
	LibraryRouter.Register(&TestLibraryConnector{})
	LibraryRouter.Register(&DeleteLibraryConnector{})
	LibraryRouter.Register(&ListLibraryConnectors{})
	LibraryRouter.Register(&InstallLibraryConnector{})
}
