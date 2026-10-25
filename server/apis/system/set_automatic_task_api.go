package system

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// API 监听设置
type SetAutomaticTaskAPIRequest struct {
	// 监听地址
	IP string `json:"ip" validate:"required,ip"`
	// 监听端口
	Port int `json:"port" validate:"required,min=1,max=65535"`
}

// 修改任务 API 设置
type SetAutomaticTaskAPI struct {
	// 请求方法
	ginx.MethodPut
	// 请求体
	Body SetAutomaticTaskAPIRequest `in:"body"`
}

// Path 返回本机任务入口设置路径。
func (*SetAutomaticTaskAPI) Path() string { return "/automatic-tasks-api" }

// Output 在监听成功后持久保存地址和端口，失败保留原配置。
func (o *SetAutomaticTaskAPI) Output(ctx *gin.Context) (interface{}, error) {
	if err := global.ScheduleService.SetAPIAddress(ctx, o.Body.IP, o.Body.Port); err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
