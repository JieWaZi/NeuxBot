package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// ConfirmSourcePurge 只接纳用户明确选择的原件清理操作。
type ConfirmSourcePurge struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`

	// 请求体
	Body ConfirmSourcePurgeInput `in:"body"`
}

func (o *ConfirmSourcePurge) Path() string { return "/libraries/:libraryId/source-purges/confirm" }
func (o *ConfirmSourcePurge) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	r, err := executeWrite(ctx, o.Body.RequestID, "source.purge.confirm", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		var result *core.Receipt
		run := func() error {
			var err error
			result, err = global.KnowledgeEngine.ConfirmSourcePurge(ctx, core.ConfirmSourcePurgeInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, SourceID: o.Body.SourceID, PreviewDigest: o.Body.PreviewDigest, Confirmed: o.Body.Confirmed})
			return err
		}
		// 本机发布与清理共用排他策略锁；不持有主库事务跨核心调用。
		var err error
		if global.KnowledgeService != nil {
			err = global.KnowledgeService.WithPolicyWrite(run)
		} else {
			err = run()
		}
		return result, err
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}
