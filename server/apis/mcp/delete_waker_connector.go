package mcp

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
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
	item, err := visibleConnector(
		ctx,
		strings.TrimSpace(d.WakerID),
		strings.TrimSpace(d.ConnectorID),
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if item.Connector.Scope == constants.MCPConnectorScopeBuiltIn {
		return nil, ginx.WithStack(status_error.BuiltInMCPConnectorCannotDelete)
	}
	if err := global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			if item.Connector.OwnerWakerID == "" && d.WakerID != "" {
				return global.MCPConnectorControllerManager.DeleteWakerConnector(transactionContext, d.WakerID, item.Connector.ID)
			}
			if d.WakerID == "" {
				counts, err := global.MCPConnectorControllerManager.InstalledCounts(transactionContext, []string{item.Connector.ID})
				if err != nil {
					return err
				}
				if !d.Confirmed || counts[item.Connector.ID] != d.ExpectedAffectedCount {
					return status_error.PluginConfigurationConflict
				}
			}
			return global.MCPConnectorControllerManager.DeleteConnector(
				transactionContext,
				item.Connector.ID,
			)
		},
	); err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, d.ConnectorID)
	return ginx.CommonSuccessResponse(), nil
}
