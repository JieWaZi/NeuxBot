package knowledge

import (
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 获取知识任务详情
type GetJob struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 任务标识
	JobID string `in:"path" name:"jobId" validate:"required"`
}

// 知识任务详情
type JobDetail struct {
	// 任务
	Job Job `json:"job"`
	// 模型选择
	Configuration TaskConfiguration `json:"configuration"`
	// 整理报告
	Report string `json:"report,omitempty"`
}

func (o *GetJob) Path() string { return "/libraries/:libraryId/jobs/:jobId" }
func (o *GetJob) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	j, err := global.KnowledgeEngine.GetJob(ctx, o.JobID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if j.LibraryID != o.LibraryID {
		return nil, status_error.KnowledgeNotFound
	}
	job, err := jobHostDTO(ctx, j)
	if err != nil {
		return nil, err
	}
	out := JobDetail{Job: job}
	if core.IsMaintenanceJob(j) && j.State == "completed" {
		stages, err := global.KnowledgeEngine.ListStages(ctx, j.ID)
		if err != nil {
			return nil, bridge.MapError(err)
		}
		out.Report = bridge.MaintenanceReportForAttempt(j, stages)
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, j.RequestID)
	if err != nil {
		if errors.Is(err, status_error.KnowledgeNotFound) {
			return out, nil
		}
		return nil, err
	}
	configuration, err := global.KnowledgeControllerManager.GetTaskConfiguration(ctx, link.ConfigurationID)
	if err != nil {
		if errors.Is(err, status_error.KnowledgeNotFound) {
			return out, nil
		}
		return nil, err
	}
	out.Configuration = taskConfigurationDTO(configuration)

	return out, nil
}
