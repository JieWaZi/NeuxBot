// Package resource_categories 提供用户资源分类选项查询。
package resource_categories

import "github.com/shrewx/ginx"

// Router 管理插件、技能及连接器共用的分类协议。
var Router = ginx.NewRouter(ginx.Group("/resource-categories"))

func init() { Router.Register(&ListResourceCategories{}) }
