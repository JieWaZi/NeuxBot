package knowledge

import (
	"encoding/base64"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"

	"github.com/JieWaZi/neux-knowledge/core"
)

// 保存原件请求
type ImportSourceInput struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required"`
	// 来源标识
	SourceID string `json:"sourceId"`
	// 原件版本
	ExpectedVersion int64 `json:"expectedVersion" validate:"min=0"`
	// 文件名
	Filename string `json:"filename" validate:"required"`
	// 媒体类型
	MIME string `json:"mime"`
	// 原始字节
	Bytes string `json:"bytes" validate:"required,max=44739244"`
}

// 保存原件
type ImportSource struct {
	ginx.MethodPost
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 请求体
	Body ImportSourceInput `in:"body"`
}

func (o *ImportSource) Path() string { return "/libraries/:libraryId/sources" }
func (o *ImportSource) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(o.Body.Bytes)
	if err != nil || len(raw) > 32<<20 {
		return nil, status_error.KnowledgeInvalid
	}
	activityContext := bridge.LocalUserActivity(ctx)
	r, err := executeWrite(activityContext, o.Body.RequestID, "source.import", o.LibraryID, o.Body, func() (*core.Receipt, error) {
		return global.KnowledgeEngine.ImportSource(activityContext, core.ImportSourceInput{RequestID: o.Body.RequestID, LibraryID: o.LibraryID, SourceID: o.Body.SourceID, ExpectedVersion: o.Body.ExpectedVersion, Filename: o.Body.Filename, MIME: o.Body.MIME, Bytes: raw})
	})
	if err != nil {
		return nil, err
	}
	return receiptDTO(r), nil
}
