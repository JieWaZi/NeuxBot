package memory

import "github.com/shrewx/ginx"

// Router 是内置记忆能力数据管理接口路由。
var Router = ginx.NewRouter(ginx.Group("/memory"))

func init() {
	Router.Register(&ListMemories{})
	Router.Register(&CreateMemory{})
	Router.Register(&UpdateMemory{})
	Router.Register(&SetMemoryCore{})
	Router.Register(&DeleteMemory{})
	Router.Register(&ListTimeline{})
	Router.Register(&GetMemorySettings{})
	Router.Register(&UpdateMemorySettings{})
	Router.Register(&TestMemoryEmbedding{})
	Router.Register(&GetMemoryStatus{})
	Router.Register(&ReindexMemoryIndex{})
}
