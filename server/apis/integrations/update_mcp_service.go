package integrations

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
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

// Output 只保存 MCP 子功能偏好，不改变发布服务总开关和地址。
func (o *UpdateMCPService) Output(ctx *gin.Context) (interface{}, error) {
	if global.IntegrationAPIListener == nil {
		return nil, status_error.IntegrationListenerError
	}
	if err := global.IntegrationAPIListener.SetMCPEnabled(ctx, o.Body.Enabled); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
