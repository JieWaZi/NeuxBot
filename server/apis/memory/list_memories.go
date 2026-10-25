package memory

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	internalmemory "github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 记忆列表
type ListMemories struct {
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
	// 时间起点
	UpdatedAtStart int64 `in:"query" name:"updatedAtStart"`
	// 时间终点
	UpdatedAtEnd int64 `in:"query" name:"updatedAtEnd"`
}

// 记忆列表响应
type ListMemoriesResponse struct {
	// 资源列表
	Resources []MemoryResource `json:"resources"`
	// 页码
	PageNum int `json:"pageNum"`
	// 每页数量
	PageSize int `json:"pageSize"`
	// 总数
	TotalSize int64 `json:"totalSize"`
}

func (l *ListMemories) Path() string { return "/wakers/:wakerId/memories" }

// Validate 校验范围与分页。
func (l *ListMemories) Validate(*gin.Context) error {
	if l.Scope == constants.MemoryScopeUnspecified {
		l.Scope = constants.MemoryScopePartner
	}
	if l.PageNum < 0 || l.PageSize < 0 || l.PageSize > constants.MemoryManagementMaxPageSize {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.UpdatedAtStart < 0 || l.UpdatedAtEnd < 0 {
		return ginx.WithStack(status_error.BadRequest)
	}
	if l.UpdatedAtStart > 0 && l.UpdatedAtEnd > 0 && l.UpdatedAtStart > l.UpdatedAtEnd {
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

// Output 返回当前记忆分页。
func (l *ListMemories) Output(ctx *gin.Context) (interface{}, error) {
	filter := internalmemory.ListFilter{Scope: l.Scope, Query: l.Query, UpdatedAtStart: l.UpdatedAtStart, UpdatedAtEnd: l.UpdatedAtEnd, Limit: l.PageSize, Offset: (l.PageNum - 1) * l.PageSize}
	if l.Scope == constants.MemoryScopeAll {
		owners, err := wakerScopeOwners(ctx, l.WakerID, l.ProjectID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		filter.Owners = owners
	} else {
		memoryContext, err := visibleScope(ctx, l.WakerID, l.Scope, l.ProjectID)
		if err != nil {
			return nil, ginx.WithStack(err)
		}
		filter.ScopeID = scopeID(l.Scope, memoryContext)
	}
	page, err := global.MemoryService.ListMemories(ctx, filter)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources := make([]MemoryResource, 0, len(page.Items))
	for _, item := range page.Items {
		resources = append(resources, memoryResource(item))
	}
	return ListMemoriesResponse{Resources: resources, PageNum: l.PageNum, PageSize: l.PageSize, TotalSize: page.Total}, nil
}

func scopeID(scope constants.MemoryScope, memoryContext internalmemory.Context) string {
	if scope == constants.MemoryScopeProject {
		return memoryContext.ProjectID
	}
	return memoryContext.PartnerID
}

// wakerScopeOwners 返回伙伴自身范围与其拥有的全部项目范围。
func wakerScopeOwners(ctx *gin.Context, wakerID string, projectID string) ([]internalmemory.ScopeOwner, error) {
	if projectID != "" {
		return nil, status_error.BadRequest
	}
	if _, err := global.WakerControllerManager.Get(ctx, wakerID); err != nil {
		return nil, err
	}
	total, err := global.ProjectControllerManager.CountByWaker(ctx, wakerID, "")
	if err != nil {
		return nil, err
	}
	owners := []internalmemory.ScopeOwner{{Scope: constants.MemoryScopePartner, ScopeID: wakerID}}
	if total == 0 {
		return owners, nil
	}
	projects, err := global.ProjectControllerManager.ListByWaker(ctx, wakerID, "", int(total), 0)
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		owners = append(owners, internalmemory.ScopeOwner{Scope: constants.MemoryScopeProject, ScopeID: project.ID})
	}
	return owners, nil
}
