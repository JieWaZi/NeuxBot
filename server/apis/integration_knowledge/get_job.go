package integration_knowledge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 集成知识作业事实
type PublishedJob struct {
	// 作业标识
	ID string `json:"id"`
	// 知识库标识
	LibraryID string `json:"libraryId"`
	// 当前状态
	State string `json:"state"`
	// 状态版本
	Version int64 `json:"version"`
	// 当前尝试
	Attempt int `json:"attempt"`
	// 来源版本
	SourceVersionIDs []string `json:"sourceVersionIds"`
	// 取消屏障
	CancelRequested bool `json:"cancelRequested"`
	// 创建时间
	CreatedAt int64 `json:"createdAt"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
}

// 读取原主体知识作业
type PublishedGetJob struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"id" validate:"required"`
	// 作业标识
	JobID string `in:"path" name:"jobId" validate:"required"`
}

func (*PublishedGetJob) Path() string { return "/knowledge-bases/:id/integration-jobs/:jobId" }
func (o *PublishedGetJob) Output(ctx *gin.Context) (interface{}, error) {
	d := dependencies(ctx)
	if d.options.References == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	job, err := d.engine.GetJob(ctx.Request.Context(), o.JobID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if job.LibraryID != o.LibraryID {
		return nil, status_error.KnowledgeNotFound
	}
	identity, _ := middleware.IntegrationPrincipal(ctx.Request.Context())
	if err := ownsJob(ctx.Request.Context(), d, job, identity.KeyID); err != nil {
		return nil, err
	}
	return jobDTO(job), nil
}
func ownsJob(ctx context.Context, d *apiDependencies, job *core.Job, keyID string) error {
	link, err := d.options.References.GetOperation(ctx, job.RequestID)
	if err != nil {
		return status_error.KnowledgeNotFound
	}
	session, err := d.options.References.GetTaskConfiguration(ctx, link.ConfigurationID)
	if err != nil || session.RemoteSubject != keyID {
		return status_error.KnowledgeNotFound
	}
	return nil
}
func jobDTO(job *core.Job) PublishedJob {
	return PublishedJob{ID: job.ID, LibraryID: job.LibraryID, State: job.State, Version: job.Version, Attempt: job.Attempt, SourceVersionIDs: job.SourceVersionIDs, CancelRequested: job.CancelRequested, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt}
}
