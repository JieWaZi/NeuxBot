package system

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取本机入口设置
type GetAutomaticTaskAPI struct {
	// 请求方法
	ginx.MethodGet
}

// Path 返回本机任务入口设置路径。
func (*GetAutomaticTaskAPI) Path() string { return "/automatic-tasks-api" }

// Output 返回不包含任何任务密钥的设备设置。
func (*GetAutomaticTaskAPI) Output(*gin.Context) (interface{}, error) {
	return global.ScheduleService.GetAPIStatus(), nil
}
