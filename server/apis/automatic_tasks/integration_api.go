package automatic_tasks

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	published "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"net/http"
	"reflect"
	"strings"
)

// IntegrationRouter 只作为公开任务合同源，不注册到桌面管理 Router。
var IntegrationRouter = ginx.NewRouter(ginx.Group("/api/v1"))

// integrationTaskRoute 使生成、实际路由与授权使用同一登记。
type integrationTaskRoute struct {
	// Operator 是业务操作原型。
	Operator ginx.HandleOperator
	// Capability 是该操作必须具备的任务能力。
	Capability constants.IntegrationCapability
}

var integrationTaskRoutes []integrationTaskRoute

func registerIntegrationTask(operator ginx.HandleOperator) {
	capability := constants.IntegrationCapabilityTaskResults
	switch operator.(type) {
	case *PublishedTriggerTask, *PublishedTaskUpload:
		capability = constants.IntegrationCapabilityTaskInvoke
	}
	IntegrationRouter.Register(operator)
	integrationTaskRoutes = append(integrationTaskRoutes, integrationTaskRoute{Operator: operator, Capability: capability})
}

func init() {
	registerIntegrationTask(&PublishedTriggerTask{})
	registerIntegrationTask(&PublishedTaskState{})
	registerIntegrationTask(&PublishedTaskResult{})
	registerIntegrationTask(&PublishedTaskUpload{})
	registerIntegrationTask(&PublishedTaskDownload{})
}

// integrationTaskDependencies 只持有既有任务服务和统一授权源。
type integrationTaskDependencies struct {
	// API 复用原任务查询和工作流结果用例。
	API *taskAPI
	// Auth 是唯一集成 Key 认证与提交协调源。
	Auth *integrationservice.Service
}
type integrationTaskKey struct{}

func taskDependencies(ctx *gin.Context) *integrationTaskDependencies {
	return ctx.Request.Context().Value(integrationTaskKey{}).(*integrationTaskDependencies)
}

// NewIntegrationHandler 使用集成 Key 认证，绝不伪造旧任务 Token。
func NewIntegrationHandler(service *schedule.Service, config schedule.Config, auth *integrationservice.Service) http.Handler {
	router := gin.New()
	router.Use(gin.Recovery())
	dependencies := &integrationTaskDependencies{API: &taskAPI{service: service, config: config}, Auth: auth}
	router.Use(func(ctx *gin.Context) {
		if service == nil || auth == nil {
			middleware.AbortIntegration(ctx, status_error.AutomaticTaskServiceInvalid)
			return
		}
		ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), integrationTaskKey{}, dependencies))
		ctx.Next()
	})
	gate := middleware.NewAPIKeyRouteAuthorizer()
	router.Use(gate.Middleware(auth))
	for _, route := range integrationTaskRoutes {
		path := "/api/v1" + route.Operator.Path()
		gate.Register(route.Operator.Method(), path, middleware.APIKeyRoutePolicy{ResourceKind: constants.IntegrationResourceKindAutomaticTask, ResourceParam: "id", Capabilities: []constants.IntegrationCapability{route.Capability}})
		router.Handle(route.Operator.Method(), path, middleware.IntegrationOperatorHandler(route.Operator))
	}
	return router
}

// 集成任务调用输入
type PublishedTaskInput struct {
	// 附加要求
	Input *string `json:"input,omitempty" validate:"omitempty,max=10000"`
	// 工作流参数
	Args map[string]any `json:"args,omitempty"`
}

// 调用公开自动任务
type PublishedTriggerTask struct {
	ginx.MethodPost
	// 任务标识
	TaskID string `in:"path" name:"id" validate:"required"`
	// 本次输入
	Body PublishedTaskInput `in:"body"`
}

func (*PublishedTriggerTask) Path() string { return "/automatic-tasks/:id/runs" }
func (o *PublishedTriggerTask) Output(ctx *gin.Context) (interface{}, error) {
	d := taskDependencies(ctx)
	identity, ok := middleware.IntegrationPrincipal(ctx.Request.Context())
	if !ok {
		return nil, status_error.IntegrationUnauthorized
	}
	key, err := requestidempotency.New(ctx.GetHeader("Idempotency-Key"), nil)
	if err != nil {
		return nil, err
	}
	locked, release := d.API.service.AcquireMutation(ctx.Request.Context())
	defer release()
	task, err := d.API.service.IntegrationTask(locked, o.TaskID, true)
	if err != nil {
		return nil, err
	}
	if (task.WorkflowID != "" && o.Body.Input != nil) || (task.WorkflowID == "" && o.Body.Args != nil) {
		return nil, status_error.BadRequest
	}
	auth, err := d.Auth.Authorize(locked, identity, constants.IntegrationResourceKindAutomaticTask, task.ID, []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke})
	if err != nil {
		return nil, err
	}
	auth.RemoteAddr = ctx.Request.RemoteAddr
	auth.ForwardedFor = strings.Join(ctx.Request.Header.Values("X-Forwarded-For"), ",")
	input := ""
	if o.Body.Input != nil {
		input = *o.Body.Input
	}
	invocation := &models.IntegrationInvocation{Identity: identity, TaskID: task.ID, TaskConfigurationVersion: task.ConfigurationVersion, RemoteAddr: auth.RemoteAddr, ForwardedFor: auth.ForwardedFor}
	view, err := d.API.service.Admit(locked, schedule.AdmissionCommand{Task: task, IntegrationInvocation: invocation, AcquireAuthorization: func(ctx context.Context) (func(), error) { return d.Auth.AcquireGuard(ctx, auth) }, Source: constants.AutomaticRunSourceAPI, OccurrenceKey: "integration:" + key.ResourceID("automatic-task", identity.KeyID, task.ID), RequestID: key.ID, Input: input, Args: o.Body.Args, NextRunAt: task.NextRunAt})
	if err != nil {
		return nil, err
	}
	result := publicRun(view)
	ctx.JSON(http.StatusAccepted, result)
	return result, nil
}

// 读取公开任务运行状态
type PublishedTaskState struct {
	ginx.MethodGet
	// 任务标识
	TaskID string `in:"path" name:"id" validate:"required"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required"`
}

func (*PublishedTaskState) Path() string { return "/automatic-tasks/:id/runs/:runId" }
func (o *PublishedTaskState) Output(ctx *gin.Context) (interface{}, error) {
	d := taskDependencies(ctx)
	run, err := ownedIntegrationRun(ctx, o.TaskID, o.RunID)
	if err != nil {
		return nil, err
	}
	return d.API.readRunState(ctx.Request.Context(), run)
}

// 读取公开任务结构化结果
type PublishedTaskResult struct {
	ginx.MethodGet
	// 任务标识
	TaskID string `in:"path" name:"id" validate:"required"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required"`
}

func (*PublishedTaskResult) Path() string { return "/automatic-tasks/:id/runs/:runId/result" }
func (o *PublishedTaskResult) Output(ctx *gin.Context) (interface{}, error) {
	d := taskDependencies(ctx)
	run, err := ownedIntegrationRun(ctx, o.TaskID, o.RunID)
	if err != nil {
		return nil, err
	}
	if run.Configuration.WorkflowID == "" || d.API.config.Workflows == nil {
		return nil, status_error.AutomaticRunNotFound
	}
	return d.API.workflowResult(ctx.Request.Context(), run)
}
func ownedIntegrationRun(ctx *gin.Context, taskID, runID string) (models.AutomaticRun, error) {
	d := taskDependencies(ctx)
	if _, err := d.API.service.IntegrationTask(ctx.Request.Context(), taskID, false); err != nil {
		return models.AutomaticRun{}, err
	}
	run, err := d.API.config.Tasks.GetRun(ctx.Request.Context(), runID)
	if err != nil {
		return run, err
	}
	identity, _ := middleware.IntegrationPrincipal(ctx.Request.Context())
	if run.TaskID != taskID || run.IntegrationInvocation == nil || run.IntegrationInvocation.Identity.KeyID != identity.KeyID {
		return models.AutomaticRun{}, status_error.AutomaticRunNotFound
	}
	if err := d.API.service.ReconcileRun(ctx.Request.Context(), &run); err != nil {
		return run, err
	}
	return run, nil
}

// PublishedRoutes 从运行时同一登记导出公开合同，避免第二套路由清单。
func PublishedRoutes() []published.Route {
	out := make([]published.Route, 0, len(integrationTaskRoutes))
	for _, route := range integrationTaskRoutes {
		op := route.Operator
		out = append(out, published.Route{Method: op.Method(), Path: "/api/v1" + op.Path(), OperationID: reflect.TypeOf(op).Elem().Name()})
	}
	return out
}
