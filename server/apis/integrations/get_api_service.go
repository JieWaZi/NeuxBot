package integrations

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取 API 服务设置
type GetAPIService struct{ ginx.MethodGet }

// Path 返回统一发布服务状态入口。
func (*GetAPIService) Path() string { return "/service" }

// Output 只读取唯一监听所有者，不从任务或知识服务复制状态。
func (*GetAPIService) Output(*gin.Context) (interface{}, error) {
	if global.IntegrationAPIListener == nil {
		return nil, status_error.IntegrationListenerError
	}
	return global.IntegrationAPIListener.Settings(), nil
}
