package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取知识任务列表
type ListJobs struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 分页参数
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

// 知识任务列表
type JobList struct {
	// 任务
	Items []Job `json:"items"`
	// 总数
	Total int64 `json:"total"`
	// 当前占用知识库的任务
	ActiveJob *Job `json:"activeJob"`
}

func (o *ListJobs) Path() string { return "/libraries/:libraryId/jobs" }
func (o *ListJobs) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	offset, limit := pagination(Pagination{Offset: o.Offset, Limit: o.Limit})
	rows, total, err := global.KnowledgeEngine.ListJobs(ctx, o.LibraryID, offset, limit)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	active, err := global.KnowledgeEngine.GetActiveJob(ctx, o.LibraryID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	items := []Job{}
	var activeJob *Job
	for _, v := range rows {
		item, err := jobHostDTO(ctx, v)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if active != nil && v.ID == active.ID {
			activeJob = &item
		}
	}
	if active != nil && activeJob == nil {
		item, err := jobHostDTO(ctx, active)
		if err != nil {
			return nil, err
		}
		activeJob = &item
	}
	return JobList{Items: items, Total: total, ActiveJob: activeJob}, nil
}
