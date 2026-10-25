package integration_knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 集成作业分页事实
type PublishedJobList struct {
	// 本主体作业
	Items []PublishedJob `json:"items"`
	// 实际总数
	Total int64 `json:"total"`
	// 起始位置
	Offset int `json:"offset"`
	// 返回数量
	Limit int `json:"limit"`
}

// 列出本主体知识作业
type PublishedListJobs struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"id" validate:"required"`
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0,max=10000000"`
	// 返回数量
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (*PublishedListJobs) Path() string { return "/knowledge-bases/:id/integration-jobs" }
func (o *PublishedListJobs) Output(ctx *gin.Context) (interface{}, error) {
	if o.Limit == 0 {
		o.Limit = 50
	}
	if o.Offset < 0 || o.Limit < 1 || o.Limit > 100 {
		return nil, status_error.KnowledgeInvalid
	}
	d := dependencies(ctx)
	identity, _ := middleware.IntegrationPrincipal(ctx.Request.Context())
	rows, total, err := d.engine.ListJobsForActor(ctx.Request.Context(), o.LibraryID, identity.KeyID, o.Offset, o.Limit)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	out := PublishedJobList{Items: []PublishedJob{}, Total: total, Offset: o.Offset, Limit: o.Limit}
	for _, row := range rows {
		out.Items = append(out.Items, jobDTO(row))
	}
	return out, nil
}
