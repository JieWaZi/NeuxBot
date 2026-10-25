package plugins

import "github.com/shrewx/ginx"

// Router 提供插件库与共享资源管理入口。
var Router = ginx.NewRouter(ginx.Group("/plugins"))

// WakerRouter 提供伙伴真实插件安装入口。
var WakerRouter = ginx.NewRouter(ginx.Group("/wakers"))

func init() {
	Router.Register(&ListPluginCatalog{})
	Router.Register(&RefreshPluginCatalog{})
	Router.Register(&GetCatalogPlugin{})
	Router.Register(&GetCatalogPluginReadme{})
	Router.Register(&GetPluginReadme{})
	Router.Register(&GetCatalogPluginSkill{})
	Router.Register(&PrepareCatalogPlugin{})
	Router.Register(&ListPlugins{})
	Router.Register(&GetPlugin{})
	Router.Register(&GetPluginSkill{})
	Router.Register(&GetPluginConnector{})
	Router.Register(&TestPluginConnector{})
	Router.Register(&GetCatalogPluginConnector{})
	Router.Register(&InstallPlugin{})
	Router.Register(&UpdatePluginConfiguration{})
	Router.Register(&UpdatePlugin{})
	Router.Register(&DeletePlugin{})
	Router.Register(&GetPluginAuthorization{})
	Router.Register(&ConfigurePluginAuthorization{})
	Router.Register(&StartPluginAuthorization{})
	Router.Register(&CancelPluginAuthorization{})
	Router.Register(&LogoutPluginAuthorization{})
	WakerRouter.Register(&ListWakerPlugins{})
	WakerRouter.Register(&SetWakerPluginEnabled{})
	WakerRouter.Register(&UninstallWakerPlugin{})
}
