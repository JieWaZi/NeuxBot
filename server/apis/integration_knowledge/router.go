package integration_knowledge

import (
	"context"
	published "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"reflect"

	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Router 是独立发布合同的生成源，不注册到桌面 RootRouter。
var Router = ginx.NewRouter(ginx.Group("/api/v1"))

// publishedRoute 将同一 Operator 同时用于实际路由、授权登记和合同生成。
type publishedRoute struct {
	// operator 是唯一业务 Operator 原型。
	operator ginx.HandleOperator
	// policy 是该操作的明确授权策略。
	policy middleware.APIKeyRoutePolicy
}

var publishedRoutes []publishedRoute

func register(operator ginx.HandleOperator) {
	Router.Register(operator)
	read := middleware.APIKeyRoutePolicy{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceParam: "id", Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}
	switch operator.(type) {
	case *PublishedListLibraries:
		read.Collection = true
	case *PublishedGetSource, *PublishedGetRaw, *PublishedReparseSource:
		read.ResourceParam = ""
		read.ResolveResource = resolveSourceLibrary
	case *PublishedListPageDirectory, *PublishedListPages, *PublishedGetPage, *PublishedGetPageByID, *PublishedListRevisions, *PublishedListThemes, *PublishedSearch, *PublishedGetGraph, *PublishedCreatePage, *PublishedWritePage, *PublishedCreateTheme, *PublishedWriteTheme, *PublishedDeleteTheme, *PublishedMaintain:
		read.ResourceParam = "kb_id"
	}
	switch operator.(type) {
	case *PublishedImportSource, *PublishedCreatePage, *PublishedWritePage, *PublishedCreateTheme, *PublishedWriteTheme, *PublishedDeleteTheme:
		read.Capabilities = append(read.Capabilities, constants.IntegrationCapabilityIngest)
	case *PublishedOrganize, *PublishedReparseSource, *PublishedMaintain:
		read.Capabilities = append(read.Capabilities, constants.IntegrationCapabilityOrganize)
	}
	publishedRoutes = append(publishedRoutes, publishedRoute{operator: operator, policy: read})
}

func init() {
	register(&PublishedGetLibrary{})
	register(&PublishedListLibraries{})
	register(&PublishedGetCapabilities{})
	register(&PublishedGetReceipt{})
	register(&PublishedGetJob{})
	register(&PublishedListJobs{})
	register(&PublishedListSources{})
	register(&PublishedListPages{})
	register(&PublishedListPageDirectory{})
	register(&PublishedGetPage{})
	register(&PublishedGetPageByID{})
	register(&PublishedListRevisions{})
	register(&PublishedListThemes{})
	register(&PublishedSearch{})
	register(&PublishedHybridSearch{})
	register(&PublishedHybridSearchGET{})
	register(&PublishedGetGraph{})
	register(&PublishedGetSource{})
	register(&PublishedGetRaw{})
	register(&PublishedOrganize{})
	register(&PublishedReparseSource{})
	register(&PublishedImportSource{})
	register(&PublishedMaintain{})
	register(&PublishedCreatePage{})
	register(&PublishedWritePage{})
	register(&PublishedCreateTheme{})
	register(&PublishedWriteTheme{})
	register(&PublishedDeleteTheme{})
}

// apiDependencies 只保存宿主已有服务的引用，不拥有第二套知识或凭据。
type apiDependencies struct {
	// engine 是唯一权威知识核心。
	engine *core.Engine
	// knowledge 是现有独立模型作业服务。
	knowledge *knowledgeservice.Service
	// auth 是知识与自动任务共用的集成授权源。
	auth *integrationservice.Service
	// options 是可恢复写入和提交后事件的既有宿主端口。
	options Options
}

// Options 装配公开入口必要的宿主端口，不复制知识或授权状态。
type Options struct {
	// References 是现有知识操作登记与冻结配置存储。
	References controllers.KnowledgeController
	// Events 复用当前 Core 的提交后变化事件。
	Events *events.Hub
}

// dependenciesKey 只允许宿主装配请求依赖。
type dependenciesKey struct{}

func dependencies(ctx *gin.Context) *apiDependencies {
	return ctx.Request.Context().Value(dependenciesKey{}).(*apiDependencies)
}

// NewHandler 装配明确发布的 Operator，不引用桌面管理路由。
func NewHandler(engine *core.Engine, knowledge *knowledgeservice.Service, auth *integrationservice.Service, options ...Options) *gin.Engine {
	deps := &apiDependencies{engine: engine, knowledge: knowledge, auth: auth}
	if len(options) > 0 {
		deps.options = options[0]
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(ctx *gin.Context) {
		if engine == nil || auth == nil {
			middleware.AbortIntegration(ctx, status_error.KnowledgeUnsupported)
			return
		}
		ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), dependenciesKey{}, deps))
		ctx.Next()
	})
	gate := middleware.NewAPIKeyRouteAuthorizer()
	router.Use(gate.Middleware(auth))
	for _, route := range publishedRoutes {
		path := "/api/v1" + route.operator.Path()
		gate.Register(route.operator.Method(), path, route.policy)
		router.Handle(route.operator.Method(), path, middleware.IntegrationOperatorHandler(route.operator))
	}
	return router
}

// PublishedRoutes 从运行时同一登记导出公开合同，避免第二套路由清单。
func PublishedRoutes() []published.Route {
	out := make([]published.Route, 0, len(publishedRoutes))
	for _, route := range publishedRoutes {
		op := route.operator
		out = append(out, published.Route{Method: op.Method(), Path: "/api/v1" + op.Path(), OperationID: reflect.TypeOf(op).Elem().Name()})
	}
	return out
}
