package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端任务列表
type ListRemoteJobs struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 分页参数
	// 起始位置
	Offset int `in:"query" name:"offset" validate:"min=0"`
	// 每页条数
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
}

func (o *ListRemoteJobs) Path() string { return "/:connectionId/libraries/:libraryId/jobs" }
func (o *ListRemoteJobs) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	offset, limit := remotePagination(knowledge.Pagination{Offset: o.Offset, Limit: o.Limit})
	v, e := c.custom.ListJobs(ctx, offset, limit)
	if e != nil {
		return nil, remoteError(e)
	}
	items := []knowledge.Job{}
	for _, x := range v.Items {
		items = append(items, customJob(x))
	}
	return knowledge.JobList{Items: items, Total: v.Total}, nil
}
