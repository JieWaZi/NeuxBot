package projects

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 项目列表
type ListProjects struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 搜索内容
	Search string `in:"query" name:"search"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// 项目列表响应
type ListProjectsResponse struct {
	// 资源列表
	Resources []ProjectResource `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

// Path 返回项目列表接口路径。
func (l *ListProjects) Path() string { return "" }

// Validate 校验分页范围并补充默认值。
func (l *ListProjects) Validate(*gin.Context) error {
	if l.PageNum < 0 || l.PageSize < 0 || l.PageSize > 100 {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.PageNum == 0 {
		l.PageNum = 1
	}
	if l.PageSize == 0 {
		l.PageSize = 50
	}
	return nil
}

// Output 返回伙伴项目及目录当前可用状态。
func (l *ListProjects) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, l.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	projects, err := global.ProjectControllerManager.ListByWaker(
		ctx,
		l.WakerID,
		l.Search,
		l.PageSize,
		(l.PageNum-1)*l.PageSize,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources := make([]ProjectResource, 0, len(projects))
	for _, project := range projects {
		directories, sourceErr := global.ProjectControllerManager.ListDirectories(ctx, project.ID)
		if sourceErr != nil {
			return nil, ginx.WithStack(sourceErr)
		}
		resources = append(resources, projectResource(project, directories))
	}
	total, err := global.ProjectControllerManager.CountByWaker(ctx, l.WakerID, l.Search)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ListProjectsResponse{
		Resources: resources,
		PageNum:   l.PageNum,
		PageSize:  l.PageSize,
		TotalSize: total,
	}, nil
}
