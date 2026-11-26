package wakers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴导航分组列表
type ListWakerNavigationGroups struct {
	// MethodGet 声明列出分组接口的 HTTP 方法。
	ginx.MethodGet
}

// 伙伴导航分组响应
type ListWakerNavigationGroupsResponse struct {
	// 分组列表
	Groups []models.WakerNavigationGroupMembers `json:"groups" validate:"required"`
	// 未分组伙伴
	UngroupedWakerIDs []string `json:"ungroupedWakerIds" validate:"required"`
}

// Path 返回伙伴导航分组列表的路径。
func (*ListWakerNavigationGroups) Path() string { return "/groups" }

// Output 在同一数据库快照中读取伙伴导航分组与成员关系。
// [DatabaseOperationCancelled][40800001001][数据库操作已取消]
// [DatabaseError][50000000002][数据库操作失败]
// [DatabaseBusy][50300001001][数据库繁忙]
func (*ListWakerNavigationGroups) Output(ctx *gin.Context) (interface{}, error) {
	var result ListWakerNavigationGroupsResponse
	err := global.TransactionControllerManager.Transaction(ctx.Request.Context(), func(tx context.Context) error {
		var err error
		result.Groups, result.UngroupedWakerIDs, err = global.WakerControllerManager.ListNavigationGroups(tx)
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
