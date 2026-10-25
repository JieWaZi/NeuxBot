package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalmemory "github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 时间线列表
type ListTimeline struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
	// 记忆范围
	Scope constants.MemoryScope `in:"query" name:"scope"`
	// 项目标识
	ProjectID string `in:"query" name:"projectId"`
	// 搜索关键字
	Query string `in:"query" name:"query"`
	// 页码
	PageNum int `in:"query" name:"pageNum"`
	// 每页数量
	PageSize int `in:"query" name:"pageSize"`
}

// 时间线列表响应
type ListTimelineResponse struct {
	// 资源列表
	Resources []TimelineResource `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

func (l *ListTimeline) Path() string { return "/wakers/:wakerId/timeline" }

// Validate 校验范围与分页。
func (l *ListTimeline) Validate(*gin.Context) error {
	if l.Scope == constants.MemoryScopeUnspecified {
		l.Scope = constants.MemoryScopePartner
	}
	if l.PageNum < 0 || l.PageSize < 0 || l.PageSize > constants.MemoryManagementMaxPageSize {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.PageNum == 0 {
		l.PageNum = 1
	}
	if l.PageSize == 0 {
		l.PageSize = constants.MemoryManagementDefaultPageSize
	}
	return nil
}

// Output 返回记忆变更时间线分页。
func (l *ListTimeline) Output(ctx *gin.Context) (interface{}, error) {
	memoryContext, err := visibleScope(ctx, l.WakerID, l.Scope, l.ProjectID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	ownerID := scopeID(l.Scope, memoryContext)
	page, err := global.MemoryService.ListTimeline(ctx, internalmemory.ListFilter{Scope: l.Scope, ScopeID: ownerID, Query: l.Query, Limit: l.PageSize, Offset: (l.PageNum - 1) * l.PageSize})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources := make([]TimelineResource, 0, len(page.Items))
	for _, item := range page.Items {
		resources = append(resources, timelineResource(item))
	}
	return ListTimelineResponse{Resources: resources, PageNum: l.PageNum, PageSize: l.PageSize, TotalSize: page.Total}, nil
}
