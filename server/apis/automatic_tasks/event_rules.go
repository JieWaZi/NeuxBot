package automatic_tasks

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ListAutomaticEventSources 返回可用来源和表单字段。
type ListAutomaticEventSources struct {
	// MethodGet 声明只读来源目录。
	ginx.MethodGet
}

// Path 返回事件来源目录路径。
func (*ListAutomaticEventSources) Path() string { return "/event-sources" }

// ListAutomaticEventSourcesResponse 提供规则编辑器使用的来源目录。
type ListAutomaticEventSourcesResponse struct {
	// Resources 是当前 Core 登记的来源。
	Resources []eventsource.Definition `json:"resources"`
}

// Output 返回与保存校验共享的事件源声明。
func (*ListAutomaticEventSources) Output(*gin.Context) (interface{}, error) {
	return ListAutomaticEventSourcesResponse{Resources: eventsource.Catalog()}, nil
}

// CheckAutomaticEventRule 检查已授权来源，并接纳真实新变化。
type CheckAutomaticEventRule struct {
	// MethodPost 触发一次实际检查。
	ginx.MethodPost
	// WakerID 是该规则所属伙伴或团队负责人。
	WakerID string `in:"path" name:"wakerId" validate:"required,max=32"`
	// TaskID 是要检查的事件规则。
	TaskID string `in:"path" name:"taskId" validate:"required,max=32"`
}

// Path 返回规则检查路径。
func (*CheckAutomaticEventRule) Path() string { return "/:taskId/check-event" }

// Output 返回实际检查和最近事件。
func (o *CheckAutomaticEventRule) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.ScheduleService.CheckEventRule(ctx, o.WakerID, o.TaskID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
