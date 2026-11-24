package mcp

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除连接器
type DeleteWakerConnector struct {
	ginx.MethodDelete
	// Confirmed 表示已确认全局资源删除。
	Confirmed bool `in:"query" name:"confirmed"`
	// ExpectedAffectedCount 是确认时实际安装伙伴数。
	ExpectedAffectedCount int64 `in:"query" name:"expectedAffectedCount"`
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
}

// Path 返回删除伙伴连接器接口路径。
func (d *DeleteWakerConnector) Path() string {
	return "/:wakerId/connectors/:connectorId"
}

// Output 卸载全局连接器关系，或删除当前伙伴自有连接器。
func (d *DeleteWakerConnector) Output(ctx *gin.Context) (interface{}, error) {
	return removeConnector(ctx, d)
}

// removeConnector 复用相同资源用例，空伙伴范围表示全局库。
func removeConnector(ctx *gin.Context, d *DeleteWakerConnector) (*ginx.CommonResponse, error) {
	if err := connectorService().Remove(ctx, strings.TrimSpace(d.WakerID), strings.TrimSpace(d.ConnectorID), d.Confirmed, d.ExpectedAffectedCount); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
