package workspace

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 释放文件观察
type UnwatchResource struct {
	// MethodDelete 释放观察登记与底层句柄。
	ginx.MethodDelete
	// 观察标识
	SubscriptionID string `in:"query" name:"subscriptionId" validate:"required"`
}

func (*UnwatchResource) Path() string { return "/watch" }
func (op *UnwatchResource) Output(ctx *gin.Context) (interface{}, error) {
	removeWatch(middleware.AuthSubject(ctx), op.SubscriptionID)
	return ginx.CommonSuccessResponse(), nil
}
