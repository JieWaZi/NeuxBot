package im

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// DeleteIMConnection 删除平台连接及其 IM 关联，保留已产生的内部对话。
type DeleteIMConnection struct {
	// MethodDelete 声明连接删除使用 HTTP DELETE。
	ginx.MethodDelete
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 配置版本
	Revision int64 `in:"query" name:"revision" validate:"min=1"`
	// 依赖确认摘要
	DependencyDigest string `in:"query" name:"dependencyDigest" validate:"max=64"`
}

func (*DeleteIMConnection) Path() string { return "/connections/:connectionId" }
func (o *DeleteIMConnection) Output(ctx *gin.Context) (interface{}, error) {
	global.WorkflowState.Commands.Lock()
	defer global.WorkflowState.Commands.Unlock()
	err := global.IMService.WithConnectionRemoval(ctx, o.ConnectionID, func(tx context.Context) error {
		return global.IMControllerManager.DeleteConnection(tx, o.ConnectionID, o.Revision, o.DependencyDigest)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
