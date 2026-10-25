package integrations

import (
	"github.com/JieWaZi/NeuxAlly/server/constants/status_error"
	"github.com/JieWaZi/NeuxAlly/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// MCP 服务设置请求
type MCPServiceInput struct {
	// 启用状态
	Enabled bool `json:"enabled"`
}

// 修改 MCP 服务设置
type UpdateMCPService struct {
	ginx.MethodPut
	// 请求体
	Body MCPServiceInput `in:"body"`
}

// Path 返回独立 MCP 开关的本机管理入口。
func (*UpdateMCPService) Path() string { return "/mcp" }

// Output 只切换 MCP 接纳，不改变同一发布服务的 API 开关和地址。
func (o *UpdateMCPService) Output(ctx *gin.Context) (interface{}, error) {
	if global.IntegrationAPIListener == nil {
		return nil, status_error.IntegrationListenerError
	}
	if err := global.IntegrationAPIListener.SetMCPEnabled(ctx, o.Body.Enabled); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
