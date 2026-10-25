// Package apis 装配 NeuxBot Core 的 ginx HTTP 路由。
package apis

import (
	"github.com/JieWaZi/NeuxBot/server/apis/agent"
	"github.com/JieWaZi/NeuxBot/server/apis/ai_accounts"
	"github.com/JieWaZi/NeuxBot/server/apis/automatic_tasks"
	"github.com/JieWaZi/NeuxBot/server/apis/channels"
	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	eventapi "github.com/JieWaZi/NeuxBot/server/apis/events"
	imapi "github.com/JieWaZi/NeuxBot/server/apis/im"
	"github.com/JieWaZi/NeuxBot/server/apis/integrations"
	knowledgeapi "github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge_connections"
	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	memoryapi "github.com/JieWaZi/NeuxBot/server/apis/memory"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/apis/notifications"
	"github.com/JieWaZi/NeuxBot/server/apis/plugins"
	"github.com/JieWaZi/NeuxBot/server/apis/projects"
	"github.com/JieWaZi/NeuxBot/server/apis/role_presets"
	"github.com/JieWaZi/NeuxBot/server/apis/runtime_options"
	"github.com/JieWaZi/NeuxBot/server/apis/skills"
	"github.com/JieWaZi/NeuxBot/server/apis/system"
	"github.com/JieWaZi/NeuxBot/server/apis/terminals"
	"github.com/JieWaZi/NeuxBot/server/apis/uploads"
	"github.com/JieWaZi/NeuxBot/server/apis/wakers"
	"github.com/JieWaZi/NeuxBot/server/apis/websession"
	"github.com/JieWaZi/NeuxBot/server/apis/work_board"
	"github.com/JieWaZi/NeuxBot/server/apis/workflows"
	"github.com/JieWaZi/NeuxBot/server/apis/workspace"
	"github.com/shrewx/ginx"
)

// RootRouter 是版本化业务接口的根路由。
var RootRouter = ginx.NewRouter()

// desktopRouter 将桌面管理接口与执行能力的认证命名空间分开。
var desktopRouter = ginx.NewRouter(ginx.Group("/api/v1"))

func init() {
	desktopRouter.Register(&middleware.LocalTokenAuth{})
	desktopRouter.Register(system.Router)
	desktopRouter.Register(terminals.Router)
	desktopRouter.Register(uploads.Router)
	desktopRouter.Register(eventapi.Router)
	desktopRouter.Register(memoryapi.Router)
	desktopRouter.Register(knowledgeapi.Router)
	desktopRouter.Register(knowledge_connections.Router)
	desktopRouter.Register(integrations.Router)
	desktopRouter.Register(wakers.Router)
	desktopRouter.Register(automatic_tasks.Router)
	desktopRouter.Register(automatic_tasks.GlobalRouter)
	desktopRouter.Register(projects.Router)
	desktopRouter.Register(role_presets.Router)
	desktopRouter.Register(chats.Router)
	desktopRouter.Register(channels.Router)
	desktopRouter.Register(runtime_options.Router)
	desktopRouter.Register(ai_accounts.Router)
	desktopRouter.Register(skills.Router)
	desktopRouter.Register(skills.LibraryRouter)
	desktopRouter.Register(mcp.Router)
	desktopRouter.Register(mcp.LibraryRouter)
	desktopRouter.Register(plugins.Router)
	desktopRouter.Register(plugins.WakerRouter)
	desktopRouter.Register(imapi.Router)
	desktopRouter.Register(workflows.Router)
	desktopRouter.Register(workspace.Router)
	desktopRouter.Register(work_board.Router)
	desktopRouter.Register(notifications.Router)
	RootRouter.Register(desktopRouter)
	RootRouter.Register(agent.Router)
	RootRouter.Register(websession.Router)
}
