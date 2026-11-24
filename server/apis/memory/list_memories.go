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
	// 核心记忆
	Core *bool `in:"query" name:"core"`
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

// Validate 校验筛选与分页。
func (l *ListMemories) Validate(*gin.Context) error {
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
	if _, err := partnerMemoryContext(ctx, l.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	filter := internalmemory.ListFilter{PartnerID: l.WakerID, Core: l.Core, Query: l.Query, UpdatedAtStart: l.UpdatedAtStart, UpdatedAtEnd: l.UpdatedAtEnd, Limit: l.PageSize, Offset: (l.PageNum - 1) * l.PageSize}
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
