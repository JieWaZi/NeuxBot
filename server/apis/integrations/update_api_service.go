package integrations

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// API 服务设置请求
type APIServiceInput struct {
	// 启用状态
	Enabled bool `json:"enabled"`
	// 监听地址
	IP string `json:"ip" validate:"required,ip"`
	// 监听端口
	Port int `json:"port" validate:"min=1,max=65535"`
	// 访问地址
	PublicBaseURL string `json:"publicBaseUrl"`
}

// 修改 API 服务设置
type UpdateAPIService struct {
	ginx.MethodPut
	// 请求体
	Body APIServiceInput `in:"body"`
}

// Path 返回唯一发布服务设置入口。
func (*UpdateAPIService) Path() string { return "/service" }

// Output 切换失败保留旧监听，停用不停止定时调度。
func (o *UpdateAPIService) Output(ctx *gin.Context) (interface{}, error) {
	if global.IntegrationAPIListener == nil {
		return nil, status_error.IntegrationListenerError
	}
	if err := global.IntegrationAPIListener.Configure(ctx, o.Body.IP, o.Body.Port, o.Body.Enabled, o.Body.PublicBaseURL); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
