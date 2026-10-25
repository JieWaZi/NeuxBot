package projects

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取项目
type GetProject struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 项目标识
	ProjectID string `in:"path" name:"projectId" validate:"required"`
}

// Path 返回项目详情接口路径。
func (g *GetProject) Path() string { return "/:projectId" }

// Output 返回项目及其有序目录。
func (g *GetProject) Output(ctx *gin.Context) (interface{}, error) {
	project, err := global.ProjectControllerManager.Get(ctx, g.ProjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if project.WakerID != g.WakerID {
		return nil, ginx.WithStack(status_error.ProjectNotFound.WithParams(
			map[string]interface{}{"ProjectID": g.ProjectID},
		))
	}
	directories, err := global.ProjectControllerManager.ListDirectories(ctx, project.ID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return projectResource(project, directories), nil
}
