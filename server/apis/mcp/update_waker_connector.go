package mcp

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 更新连接器
type UpdateWakerConnector struct {
	ginx.MethodPut
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 连接器标识
	ConnectorID string `in:"path" name:"connectorId" validate:"required"`
	// 请求体
	Body ConnectorInput `in:"body"`
}

// Path 返回编辑伙伴连接器接口路径。
func (u *UpdateWakerConnector) Path() string {
	return "/:wakerId/connectors/:connectorId"
}

// Output 更新共享连接器。
func (u *UpdateWakerConnector) Output(ctx *gin.Context) (interface{}, error) {
	return updateConnector(ctx, u)
}

// updateConnector 复用相同资源用例，空伙伴范围表示全局库。
func updateConnector(ctx *gin.Context, u *UpdateWakerConnector) (ConnectorResponse, error) {
	updated, err := connectorService().Update(ctx, strings.TrimSpace(u.WakerID), strings.TrimSpace(u.ConnectorID), connectorDefinition(u.Body))
	if err != nil {
		return ConnectorResponse{}, ginx.WithStack(err)
	}
	return connectorResponse(ctx, updated)
}
