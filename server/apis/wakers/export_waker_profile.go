package wakers

import (
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 导出伙伴档案
type ExportWakerProfile struct {
	ginx.MethodGet
	// 伙伴标识
	WakerID string `in:"path" name:"wakerId" validate:"required"`
}

// Path 返回伙伴档案导出接口路径。
func (e *ExportWakerProfile) Path() string {
	return "/:wakerId/profile/export"
}

// Output 返回只包含三份伙伴档案的 ZIP。
func (e *ExportWakerProfile) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WakerControllerManager.Get(ctx, e.WakerID); err != nil {
		return nil, ginx.WithStack(err)
	}
	documents, err := global.ProfileDocumentControllerManager.ListWakerDocuments(
		ctx,
		e.WakerID,
	)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	contents, err := builtinrolepreset.ResolveWakerContents(e.WakerID, documents)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	content, err := profile.CreateZIP(contents)
	if err != nil {
		return nil, ginx.WithStack(status_error.ProfileArchiveGenerationFailed)
	}
	attachment := ginx.NewAttachment("waker-profile.zip", "application/zip")
	_, _ = attachment.Write(content)
	return attachment, nil
}
