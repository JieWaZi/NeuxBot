package knowledge

import (
	"context"
	"errors"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除知识库请求
type DeleteLibraryInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 当前版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"required,min=1"`
}

// 永久删除知识库
type DeleteLibrary struct {
	ginx.MethodDelete
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body DeleteLibraryInput `in:"body"`
}

// Path 返回知识库删除操作的路由。
func (o *DeleteLibrary) Path() string { return "/libraries/:libraryId" }

// Output 执行已确认的删除操作并保留原请求回执。
func (o *DeleteLibrary) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	// Gin Context 的取消转发依赖引擎配置，等待管线必须直接继承 HTTP 请求生命周期。
	deletionContext := context.Context(ctx)
	if ctx.Request != nil {
		deletionContext = ctx.Request.Context()
	}
	r, err := executeWrite(ctx, o.Body.RequestID, "library.delete", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return deleteLibraryStoppingOrganize(deletionContext, core.DeleteLibraryInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion})
	})
	if err != nil {
		return nil, err
	}
	// 保留原操作锚点供重试，清理已失效的模型配置和知识成果引用。
	if err := deleteLibraryHostReferences(ctx, o.LibraryID); err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}

// deleteLibraryHostReferences 仅清理知识领域引用，普通聊天由其原生命周期拥有。
func deleteLibraryHostReferences(ctx context.Context, library string) error {
	run := func(tx context.Context) error {
		return global.KnowledgeControllerManager.DeleteLibraryReferences(tx, "local", "local", library)
	}
	if global.IntegrationService != nil {
		return global.IntegrationService.WithAuthorizationWrite(ctx, run)
	}
	return run(ctx)
}

// deleteLibraryStoppingOrganize 先持久取消屏障并收束模型，再沿核心清单删除；等待不占发布锁。
func deleteLibraryStoppingOrganize(ctx context.Context, input core.DeleteLibraryInput) (*core.Receipt, error) {
	for {
		if ctx.Err() != nil {
			return nil, status_error.KnowledgeCancelled
		}
		retryCancellation := false
		var result *core.Receipt
		var job *core.Job
		run := func() error {
			// 原删除已提交时仍调用同一核心操作，核对请求摘要并重放原回执。
			if _, err := global.KnowledgeEngine.GetReceipt(ctx, input.RequestID); err == nil {
				result, err = global.KnowledgeEngine.DeleteLibrary(ctx, input)
				return err
			} else {
				var failure *core.Error
				if !errors.As(err, &failure) || failure.Code != core.ErrNotFound {
					return err
				}
			}
			library, err := global.KnowledgeEngine.GetLibrary(ctx, input.LibraryID)
			if err != nil {
				return err
			}
			// 失效确认不能先取消用户后来接纳的工作。
			if library.Version != input.ExpectedVersion {
				return status_error.KnowledgeConflict
			}
			job, err = global.KnowledgeEngine.GetActiveJob(ctx, input.LibraryID)
			if err != nil {
				return err
			}
			if job == nil {
				result, err = global.KnowledgeEngine.DeleteLibrary(ctx, input)
				return err
			}
			_, err = global.KnowledgeEngine.CancelOrganize(ctx, core.JobActionInput{
				RequestID: fmt.Sprintf("library-delete/%s/%s/%d", input.RequestID, job.ID, job.Version),
				JobID:     job.ID, ExpectedVersion: job.Version,
			})
			// 队列激活和模型阶段登记不持有宿主发布锁；版本竞争后按最新任务重试取消。
			var failure *core.Error
			retryCancellation = errors.As(err, &failure) && failure.Code == core.ErrVersionConflict
			return err
		}
		var err error
		if global.KnowledgeService != nil {
			err = global.KnowledgeService.WithPolicyWrite(run)
		} else {
			err = run()
		}
		if retryCancellation {
			continue
		}
		if err != nil || result != nil {
			return result, err
		}
		if global.KnowledgeService == nil {
			current, err := global.KnowledgeEngine.GetJob(ctx, job.ID)
			if err != nil {
				return nil, err
			}
			if current.State == "cancel_requested" {
				// 无宿主句柄可取消的外部运行仍由核心拒绝删除，不能忙等。
				return global.KnowledgeEngine.DeleteLibrary(ctx, input)
			}
			continue
		}
		if err := global.KnowledgeService.StopOrganizeAttempt(ctx, job.ID); err != nil {
			return nil, err
		}
		// 再次进入核心取消可恢复已退出管线留下的运行阶段，并依次清空整个任务队列。
	}
}
