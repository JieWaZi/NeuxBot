package integration

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// coordinatedTasks 只为会改变知识权限的任务写入增加同一提交屏障。
type coordinatedTasks struct {
	// AutomaticTaskController 保留原任务事实、接纳和查询实现。
	controllers.AutomaticTaskController
	// service 是共享的集成授权协调者。
	service *Service
}

// CoordinateTasks 在组合根包装已有仓库，任务、工作流及聚合删除都不能绕过同一撤权屏障。
func (s *Service) CoordinateTasks(tasks controllers.AutomaticTaskController) controllers.AutomaticTaskController {
	return &coordinatedTasks{AutomaticTaskController: tasks, service: s}
}
func (c *coordinatedTasks) UpdateConfiguration(ctx context.Context, task models.AutomaticTask, version int64) error {
	return c.service.WithAuthorizationWrite(ctx, func(ctx context.Context) error {
		return c.AutomaticTaskController.UpdateConfiguration(ctx, task, version)
	})
}
func (c *coordinatedTasks) SetDeleting(ctx context.Context, id string, value bool) error {
	return c.service.WithAuthorizationWrite(ctx, func(ctx context.Context) error { return c.AutomaticTaskController.SetDeleting(ctx, id, value) })
}
func (c *coordinatedTasks) Delete(ctx context.Context, id string) error {
	return c.service.WithAuthorizationWrite(ctx, func(ctx context.Context) error { return c.AutomaticTaskController.Delete(ctx, id) })
}
func (c *coordinatedTasks) DeleteByWaker(ctx context.Context, id string) error {
	return c.service.WithAuthorizationWrite(ctx, func(ctx context.Context) error { return c.AutomaticTaskController.DeleteByWaker(ctx, id) })
}
