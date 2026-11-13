// Package work_board 提供只读工作聚合及结果查看、主动清空确认接口。
package work_board

import "github.com/shrewx/ginx"

// Router 是全局任务看板路由。
var Router = ginx.NewRouter(ginx.Group("/work-board"))

func init() {
	Router.Register(&ListWorkBoard{})
	Router.Register(&ListWorkBoardExecutors{})
	Router.Register(&GetWorkBoardSummary{})
	Router.Register(&GetWorkBoardViewTargets{})
	Router.Register(&MarkWorkBoardViewed{})
	Router.Register(&GetWorkBoardLiveTarget{})
}
