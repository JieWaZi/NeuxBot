package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 恢复知识库删除清理
type RecoverLibraryDeletion struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body DeleteLibraryInput `in:"body"`
}

// Path 返回知识库删除操作的路由。
func (o *RecoverLibraryDeletion) Path() string { return "/libraries/:libraryId/deletion/recover" }

// Output 执行已确认的删除操作并保留原请求回执。
func (o *RecoverLibraryDeletion) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	input := core.RecoverLibraryDeletionInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, ExpectedVersion: o.Body.ExpectedVersion}
	r, err := executeWrite(ctx, o.Body.RequestID, "library.delete.recover", o.LibraryID, input, func() (*core.Receipt, error) {
		var result *core.Receipt
		run := func() error {
			var err error
			result, err = global.KnowledgeEngine.RecoverLibraryDeletion(ctx, input)
			return err
		}
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
	if err := deleteLibraryHostReferences(ctx, o.LibraryID); err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}
