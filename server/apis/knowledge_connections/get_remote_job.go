package knowledge_connections

import (
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取远端任务
type GetRemoteJob struct {
	ginx.MethodGet
	// 连接标识
	ConnectionID string `in:"path" name:"connectionId" validate:"required"`
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 任务标识
	JobID string `in:"path" name:"jobId" validate:"required"`
}

func (o *GetRemoteJob) Path() string { return "/:connectionId/libraries/:libraryId/jobs/:jobId" }
func (o *GetRemoteJob) Output(ctx *gin.Context) (interface{}, error) {
	c, err := client(ctx, o.ConnectionID, o.LibraryID)
	if err != nil {
		return nil, err
	}
	if c.custom == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	v, e := c.custom.GetJob(ctx, o.JobID)
	if e != nil {
		return nil, remoteError(e)
	}
	return knowledge.JobDetail{Job: customJob(v)}, nil
}
