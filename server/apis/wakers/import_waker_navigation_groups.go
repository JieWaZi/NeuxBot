package wakers

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 导入伙伴导航分组请求
type ImportWakerNavigationGroupsRequest struct {
	// 分组列表
	Groups []models.WakerNavigationGroupMembers `json:"groups" validate:"required,dive"`
	// 伙伴顺序
	WakerOrder []string `json:"wakerOrder,omitempty" validate:"omitempty,dive,min=1"`
}

// 导入伙伴导航分组
type ImportWakerNavigationGroups struct {
	// MethodPost 声明导入旧分组接口的 HTTP 方法。
	ginx.MethodPost
	// 请求体
	Body ImportWakerNavigationGroupsRequest `in:"body"`
}

// Path 返回导入伙伴导航分组的路径。
func (*ImportWakerNavigationGroups) Path() string { return "/groups/import" }

// Validate 规范化分组名称并拒绝没有可见名称的分组。
func (i *ImportWakerNavigationGroups) Validate(*gin.Context) error {
	for index := range i.Body.Groups {
		group := &i.Body.Groups[index]
		group.Name = util.NormalizeName(group.Name)
		if group.Name == "" {
			return ginx.WithStack(status_error.BadRequest)
		}
	}
	return nil
}

// Output 仅在 Core 尚无分组和显式顺序时导入旧导航布局。
// [BadRequest][40000000001][请求参数不合法]
// [DatabaseOperationCancelled][40800001001][数据库操作已取消]
// [DatabaseError][50000000002][数据库操作失败]
// [DatabaseBusy][50300001001][数据库繁忙]
func (i *ImportWakerNavigationGroups) Output(ctx *gin.Context) (interface{}, error) {
	err := global.TransactionControllerManager.Transaction(ctx.Request.Context(), func(tx context.Context) error {
		return global.WakerControllerManager.ImportNavigationGroups(tx, i.Body.Groups, i.Body.WakerOrder)
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
