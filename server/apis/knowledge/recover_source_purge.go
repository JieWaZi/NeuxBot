package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// RecoverSourcePurge 只接纳用户明确选择的原件清理操作。
type RecoverSourcePurge struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 清理标识
	PurgeID string `in:"path" name:"purgeId" validate:"required"`
	// 请求体
	Body RecoverSourcePurgeInput `in:"body"`
}

func (o *RecoverSourcePurge) Path() string {
	return "/libraries/:libraryId/source-purges/:purgeId/recover"
}
func (o *RecoverSourcePurge) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	r, err := executeWrite(ctx, o.Body.RequestID, "source.purge.recover", o.LibraryID, core.RecoverSourcePurgeInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, PurgeID: o.PurgeID, ExpectedPolicyVersion: o.Body.ExpectedPolicyVersion}, func() (*core.Receipt, error) {
		var result *core.Receipt
		run := func() error {
			var err error
			result, err = global.KnowledgeEngine.RecoverSourcePurge(ctx, core.RecoverSourcePurgeInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, PurgeID: o.PurgeID, ExpectedPolicyVersion: o.Body.ExpectedPolicyVersion})
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
