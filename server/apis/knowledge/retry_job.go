package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 重试知识任务
type RetryJob struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 任务标识
	JobID string `in:"path" name:"jobId" validate:"required"`
	// 请求体
	Body RetryJobInput `in:"body"`
}

// 重试任务请求
type RetryJobInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=1"`
}

func (o *RetryJob) Path() string { return "/libraries/:libraryId/jobs/:jobId/retry" }
func (o *RetryJob) Output(ctx *gin.Context) (interface{}, error) {
	if global.KnowledgeService == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	j, err := global.KnowledgeEngine.GetJob(ctx, o.JobID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	if j.LibraryID != o.LibraryID {
		return nil, status_error.KnowledgeNotFound
	}
	r, err := global.KnowledgeService.Retry(bridge.LocalUserActivity(ctx), o.Body.RequestID, o.JobID, o.Body.ExpectedVersion)
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}
