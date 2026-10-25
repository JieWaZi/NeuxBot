package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 恢复知识任务
type ResumeJob struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 任务标识
	JobID string `in:"path" name:"jobId" validate:"required"`
	// 请求体
	Body ResumeJobInput `in:"body"`
}

// 恢复参数
type ResumeJobInput struct {
	// 当前版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"required,min=1"`
}

func (o *ResumeJob) Path() string { return "/libraries/:libraryId/jobs/:jobId/resume" }
func (o *ResumeJob) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	job, err := global.KnowledgeEngine.GetJob(ctx, o.JobID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if job.LibraryID != o.LibraryID {
		return nil, status_error.KnowledgeNotFound
	}
	if job.Version != o.Body.ExpectedVersion {
		return nil, status_error.KnowledgeConflict
	}
	if job.State != "queued" && job.State != "processing" && job.State != "cancel_requested" {
		return nil, status_error.KnowledgeConflict
	}
	if err := global.KnowledgeService.Resume(ctx, job.ID); err != nil {
		return nil, err
	}
	// 返回原任务真实投影，恢复调度本身不制造核心写回执或执行完成事实。
	return jobHostDTO(ctx, job)
}
