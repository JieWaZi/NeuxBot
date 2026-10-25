package knowledge

import (
	"encoding/base64"
	"github.com/JieWaZi/NeuxBot/server/global"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 读取原件字节
type GetRaw struct {
	ginx.MethodGet
	// 知识库标识
	LibraryID string `in:"path" name:"libraryId" validate:"required"`
	// 来源版本
	SourceID string `in:"path" name:"sourceId" validate:"required"`
}

// 原件字节响应
type RawSource struct {
	// 文件名
	Filename string `json:"filename"`
	// 媒体类型
	MIME string `json:"mime"`
	// 原始字节
	Bytes string `json:"bytes"`
}

func (o *GetRaw) Path() string { return "/libraries/:libraryId/sources/:sourceId/raw" }
func (o *GetRaw) Output(ctx *gin.Context) (interface{}, error) {
	if err := engineAvailable(ctx); err != nil {
		return nil, err
	}
	s, err := global.KnowledgeEngine.GetSource(ctx, o.LibraryID, o.SourceID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	raw, err := global.KnowledgeEngine.ReadRaw(ctx, o.LibraryID, o.SourceID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return RawSource{Filename: s.Filename, MIME: s.MIME, Bytes: base64.StdEncoding.EncodeToString(raw)}, nil
}
