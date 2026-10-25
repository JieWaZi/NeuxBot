package im

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取连接删除影响。
type GetIMConnectionDependencies struct {
	// MethodGet 声明只读依赖查询使用 HTTP GET。
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
}

// Path 返回删除影响的只读查询地址。
func (*GetIMConnectionDependencies) Path() string { return "/connections/:connectionId/dependencies" }

// Output 读取当前真实引用，不执行工作流或发送消息。
func (o *GetIMConnectionDependencies) Output(ctx *gin.Context) (interface{}, error) {
	impact, err := global.IMControllerManager.ConnectionDependencies(ctx, o.ConnectionID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return impact, nil
}
