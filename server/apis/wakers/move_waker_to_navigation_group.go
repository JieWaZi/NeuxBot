package wakers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 移动伙伴分组请求
type MoveWakerToNavigationGroupRequest struct {
	// 分组名称
	Name *string `json:"name" validate:"required"`
}

// 移动伙伴分组
type MoveWakerToNavigationGroup struct {
	// MethodPut 声明移动伙伴分组接口的 HTTP 方法。
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body MoveWakerToNavigationGroupRequest `in:"body"`
}

// Path 返回移动伙伴导航分组的路径。
func (*MoveWakerToNavigationGroup) Path() string { return "/:wakerId/group" }

// Output 将伙伴移动到指定导航分组，清除名称时设为未分组。
// [BadRequest][40000000001][请求参数不合法]
// [WakerNotFound][40400000001][伙伴不存在]
// [DatabaseOperationCancelled][40800001001][数据库操作已取消]
// [DatabaseError][50000000002][数据库操作失败]
// [DatabaseBusy][50300001001][数据库繁忙]
func (m *MoveWakerToNavigationGroup) Output(ctx *gin.Context) (interface{}, error) {
	name := util.NormalizeName(*m.Body.Name)
	err := global.TransactionControllerManager.Transaction(ctx.Request.Context(), func(tx context.Context) error {
		waker, err := global.WakerControllerManager.Get(tx, m.WakerID)
		if err != nil {
			return err
		}
		return global.WakerControllerManager.MoveToNavigationGroup(tx, waker, name)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
