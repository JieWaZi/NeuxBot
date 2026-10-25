package automatic_tasks

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// GlobalRouter 提供所有伙伴和团队共用的任务列表与详情入口。
var GlobalRouter = ginx.NewRouter(ginx.Group("/automatic-tasks"))

func init() {
	GlobalRouter.Register(&ListGlobalAutomaticTasks{})
	GlobalRouter.Register(&GetGlobalAutomaticTask{})
	GlobalRouter.Register(&CreateWorkflowAutomaticTask{})
	GlobalRouter.Register(&UpdateWorkflowAutomaticTask{})
	GlobalRouter.Register(&PreviewGlobalAutomaticSchedule{})
}

// CreateWorkflowAutomaticTask 由工作流接纳绑定内部执行伙伴，客户端无须知道其身份。
type CreateWorkflowAutomaticTask struct {
	// MethodPost 声明新建任务的请求方法。
	ginx.MethodPost
	// Body 是包含工作流目标的完整触发配置。
	Body models.AutomaticTaskConfiguration `in:"body"`
}

func (*CreateWorkflowAutomaticTask) Path() string { return "" }
func (o *CreateWorkflowAutomaticTask) Output(ctx *gin.Context) (interface{}, error) {
	if o.Body.TargetKind != constants.AutomaticTaskTargetKindWorkflow || o.Body.WorkflowID == "" {
		return nil, status_error.BadRequest
	}
	result, err := global.ScheduleService.SaveConfiguration(ctx, "", "", 0, o.Body, true)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

// UpdateWorkflowAutomaticTask 从全局任务身份编辑工作流触发器。
type UpdateWorkflowAutomaticTask struct {
	// MethodPut 声明完整配置修改的请求方法。
	ginx.MethodPut
	// TaskID 是需要编辑的任务身份。
	TaskID string `in:"path" name:"taskId" validate:"required,max=32"`
	// Body 携带用户打开表单时的配置版本与新配置。
	Body UpdateAutomaticTaskRequest `in:"body"`
}

func (*UpdateWorkflowAutomaticTask) Path() string { return "/:taskId" }
func (o *UpdateWorkflowAutomaticTask) Output(ctx *gin.Context) (interface{}, error) {
	if o.Body.TargetKind != constants.AutomaticTaskTargetKindWorkflow || o.Body.WorkflowID == "" {
		return nil, status_error.BadRequest
	}
	task, err := global.ScheduleService.Get(ctx, "", o.TaskID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if task.TargetKind != constants.AutomaticTaskTargetKindWorkflow {
		return nil, status_error.BadRequest
	}
	result, err := global.ScheduleService.SaveConfiguration(ctx, "", o.TaskID, o.Body.Revision, o.Body.AutomaticTaskConfiguration, true)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

// PreviewGlobalAutomaticSchedule 预览尚未绑定执行者的定时规则。
type PreviewGlobalAutomaticSchedule struct {
	// MethodPost 声明只计算预览的请求方法。
	ginx.MethodPost
	// Body 是用户编辑中的定时规则。
	Body models.AutomaticTaskSchedule `in:"body"`
}

func (*PreviewGlobalAutomaticSchedule) Path() string { return "/schedule-preview" }
func (o *PreviewGlobalAutomaticSchedule) Output(*gin.Context) (interface{}, error) {
	return previewSchedule(o.Body)
}

// ListGlobalAutomaticTasks 返回全局任务列表，执行者类型由显式筛选决定。
type ListGlobalAutomaticTasks struct {
	// MethodGet 声明该操作只读取任务投影。
	ginx.MethodGet
	// Query 是任务名称或指令的搜索词。
	Query string `in:"query" name:"query"`
	// ExecutorKind 是伙伴、团队或工作流类型筛选，空值表示全部。
	ExecutorKind string `in:"query" name:"executorKind" validate:"omitempty,oneof=waker team workflow"`
	// Trigger 是定时或 API 筛选，空值表示全部。
	Trigger constants.AutomaticTaskTrigger `in:"query" name:"trigger" validate:"omitempty,oneof=schedule api"`
	// Enabled 是可选启停状态筛选。
	Enabled *bool `in:"query" name:"enabled"`
	// PageNum 是从一开始的页码。
	PageNum int `in:"query" name:"pageNum"`
	// PageSize 是单页返回数量。
	PageSize int `in:"query" name:"pageSize"`
}

// Path 返回全局集合路径。
func (*ListGlobalAutomaticTasks) Path() string { return "" }

// Validate 校验分页范围并填入默认值。
func (o *ListGlobalAutomaticTasks) Validate(*gin.Context) error {
	return pagination(&o.PageNum, &o.PageSize)
}

// Output 返回服务端全局分页结果与全部任务统计。
func (o *ListGlobalAutomaticTasks) Output(ctx *gin.Context) (interface{}, error) {
	resources, total, counts, err := global.ScheduleService.List(ctx, controllers.AutomaticTaskFilter{Query: o.Query, ExecutorKind: o.ExecutorKind, Trigger: o.Trigger, Enabled: o.Enabled, Limit: o.PageSize, Offset: (o.PageNum - 1) * o.PageSize})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListAutomaticTasksResponse{Resources: resources, TotalSize: total, Counts: counts, PageNum: o.PageNum, PageSize: o.PageSize}, nil
}

// GetGlobalAutomaticTask 从全局入口按任务身份读取详情。
type GetGlobalAutomaticTask struct {
	// MethodGet 声明该操作只读取一个任务投影。
	ginx.MethodGet
	// TaskID 是任务稳定标识。
	TaskID string `in:"path" name:"taskId" validate:"required,max=32"`
}

// Path 返回全局详情路径。
func (*GetGlobalAutomaticTask) Path() string { return "/:taskId" }

// Output 返回任务所属伙伴或团队和运行概览。
func (o *GetGlobalAutomaticTask) Output(ctx *gin.Context) (interface{}, error) {
	result, err := global.ScheduleService.Get(ctx, "", o.TaskID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
