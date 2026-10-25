package knowledge

import (
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"

	"github.com/JieWaZi/neux-knowledge/core"
)

// 归档原件请求
type ArchiveSourceInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 状态版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=1"`
	// 已归档
	Archived bool `json:"archived"`
}

// 归档原件
type ArchiveSource struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 来源版本
	SourceID string `in:"path" name:"sourceId" validate:"required"`
	// 请求体
	Body ArchiveSourceInput `in:"body"`
}

func (o *ArchiveSource) Path() string { return "/libraries/:libraryId/sources/:sourceId/archive" }
func (o *ArchiveSource) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	r, err := executeWrite(ctx, o.Body.RequestID, "source.archive", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.ArchiveSource(ctx, core.ArchiveSourceInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, SourceVersionID: o.SourceID, ExpectedVersion: o.Body.ExpectedVersion, Archived: o.Body.Archived})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}
