package mcp

import (
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 启停连接器
type SetWakerConnectorEnabled struct {
	// MethodPut 声明以 PUT 更新伙伴的连接器启停关系。
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
	// 请求体
	Body SetConnectorEnabledRequest `in:"body"`
}

// 启停请求
type SetConnectorEnabledRequest struct {
	// 启用状态
	Enabled bool `json:"enabled"`
}

// Path 返回伙伴连接器启停接口路径。
func (s *SetWakerConnectorEnabled) Path() string {
	return "/:wakerId/connectors/:connectorId/enabled"
}

// Output 保存伙伴连接器启停关系。
func (s *SetWakerConnectorEnabled) Output(ctx *gin.Context) (interface{}, error) {
	wakerID := strings.TrimSpace(s.WakerID)
	connectorID := strings.TrimSpace(s.ConnectorID)
	if _, err := visibleConnector(
		ctx,
		wakerID,
		connectorID,
	); err != nil {
		return nil, ginx.WithStack(err)
	}
	now := time.Now().UnixMilli()
	if err := global.MCPConnectorControllerManager.UpsertWakerConnector(
		ctx,
		models.WakerMCPConnector{
			ID:             util.New(),
			WakerID:        wakerID,
			MCPConnectorID: connectorID,
			Enabled:        s.Body.Enabled,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	); err != nil {
		return nil, ginx.WithStack(err)
	}
	notifyResourceChanged(ctx, s.ConnectorID)
	return ginx.CommonSuccessResponse(), nil
}
