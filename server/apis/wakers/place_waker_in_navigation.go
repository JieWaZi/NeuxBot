package wakers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 调整伙伴导航位置请求
type PlaceWakerInNavigationRequest struct {
	// 目标伙伴
	TargetWakerID string `json:"targetWakerId" validate:"required"`
	// 相对位置
	Placement constants.WakerNavigationPlacement `json:"placement" validate:"required"`
}

// 调整伙伴导航位置
type PlaceWakerInNavigation struct {
	// MethodPut 声明调整伙伴位置接口的 HTTP 方法。
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body PlaceWakerInNavigationRequest `in:"body"`
}

// Path 返回调整伙伴导航位置的路径。
func (*PlaceWakerInNavigation) Path() string { return "/:wakerId/navigation-position" }

// Output 按目标伙伴当前所属分组调整位置。
// [BadRequest][40000000001][请求参数不合法]
// [WakerNotFound][40400000001][伙伴不存在]
// [DatabaseOperationCancelled][40800001001][数据库操作已取消]
// [DatabaseError][50000000002][数据库操作失败]
// [DatabaseBusy][50300001001][数据库繁忙]
func (p *PlaceWakerInNavigation) Output(ctx *gin.Context) (interface{}, error) {
	err := global.TransactionControllerManager.Transaction(ctx.Request.Context(), func(tx context.Context) error {
		return global.WakerControllerManager.PlaceWakerInNavigation(tx, p.WakerID, p.Body.TargetWakerID, p.Body.Placement)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
