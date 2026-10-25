// Package automatic_tasks 提供伙伴范围内的自动任务管理接口。
package automatic_tasks

import "github.com/shrewx/ginx"

// Router 是受 Core 本地管理令牌保护的伙伴任务路由。
var Router = ginx.NewRouter(ginx.Group("/wakers/:wakerId/automatic-tasks"))

func init() {
	Router.Register(&ListAutomaticTasks{})
	Router.Register(&ListAutomaticTaskAttention{})
	Router.Register(&CreateAutomaticTask{})
	Router.Register(&GetAutomaticTask{})
	Router.Register(&UpdateAutomaticTask{})
	Router.Register(&SetAutomaticTaskEnabled{})
	Router.Register(&DeleteAutomaticTask{})
	Router.Register(&TriggerAutomaticTask{})
	Router.Register(&ListAutomaticRuns{})
	Router.Register(&PreviewAutomaticSchedule{})
}
