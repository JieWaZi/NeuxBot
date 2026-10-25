package mcp

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建连接器
type CreateWakerConnector struct {
	ginx.MethodPost
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 请求体
	Body ConnectorInput `in:"body"`
}

// Path 返回新建伙伴连接器接口路径。
func (c *CreateWakerConnector) Path() string { return "/:wakerId/connectors" }

// Output 新建当前伙伴自有连接器并安装给自身。
func (c *CreateWakerConnector) Output(ctx *gin.Context) (interface{}, error) {
	return createConnector(ctx, c)
}

// createConnector 复用相同资源用例，空伙伴范围表示全局库。
func createConnector(ctx *gin.Context, c *CreateWakerConnector) (ConnectorResponse, error) {
	wakerID := strings.TrimSpace(c.WakerID)
	if wakerID != "" {
		if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
			return ConnectorResponse{}, ginx.WithStack(err)
		}
	}
	connector, err := newPublicConnector(c.Body)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	connector.OwnerWakerID = wakerID
	if wakerID != "" {
		connector.Scope = constants.MCPConnectorScopePrivate
	}
	err = global.TransactionControllerManager.Transaction(
		ctx,
		func(transactionContext context.Context) error {
			if err := global.MCPConnectorControllerManager.Create(
				transactionContext,
				connector,
			); err != nil {
				return err
			}
			if wakerID == "" {
				return nil
			}
			return global.MCPConnectorControllerManager.UpsertWakerConnector(
				transactionContext,
				models.WakerMCPConnector{
					ID:             util.New(),
					WakerID:        wakerID,
					MCPConnectorID: connector.ID,
					Enabled:        true,
					CreatedAt:      connector.CreatedAt,
					UpdatedAt:      connector.UpdatedAt,
				},
			)
		},
	)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, connector.ID)
	item, err := visibleConnector(ctx, wakerID, connector.ID)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	result, err := connectorResponse(ctx, item)
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	return result, nil
}
