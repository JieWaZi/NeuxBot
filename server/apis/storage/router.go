// Package storage 提供本机存储设置和确认后的清理入口。
package storage

import "github.com/shrewx/ginx"

// Router 注册仅限本机认证的存储操作。
var Router = ginx.NewRouter(ginx.Group("/storage"))

func init() {
	Router.Register(&GetStorageUsage{})
	Router.Register(&ListStorageEntities{})
	Router.Register(&GetStoragePolicy{})
	Router.Register(&UpdateStoragePolicy{})
	Router.Register(&GetStorageMaintenance{})
	Router.Register(&PreviewStorageCleanup{})
	Router.Register(&CleanupStorage{})
	Router.Register(&ListExpiredChats{})
	Router.Register(&PreviewExpiredChats{})
	Router.Register(&CleanupExpiredChats{})
	Router.Register(&UpdateChatProtection{})
}
