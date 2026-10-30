package wakers

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 伙伴最近可用项目
type GetWakerRecentProject struct {
	// MethodGet 声明此查询只读取草稿可采用的近期项目。
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
}

// 伙伴最近可用项目响应
type GetWakerRecentProjectResponse struct {
	// 项目标识
	ProjectID string `json:"projectId"`
}

// Path 返回伙伴最近可用项目接口路径。
func (*GetWakerRecentProject) Path() string { return "/:wakerId/recent-project" }

// Output 返回伙伴最近可用的项目标识。
func (g *GetWakerRecentProject) Output(ctx *gin.Context) (interface{}, error) {
	id, err := global.ChatService.RecentAvailableProject(ctx, g.WakerID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return GetWakerRecentProjectResponse{ProjectID: id}, nil
}
