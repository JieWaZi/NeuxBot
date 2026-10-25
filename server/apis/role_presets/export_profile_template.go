package role_presets

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 导出模板
type ExportProfileTemplate struct {
	ginx.MethodGet
}

// Path 返回空白档案模板导出接口路径。
func (e *ExportProfileTemplate) Path() string { return "/profile-template" }

// Output 返回只包含三份标准空白档案的 ZIP。
func (e *ExportProfileTemplate) Output(*gin.Context) (interface{}, error) {
	content, err := profile.CreateZIP(map[constants.ProfileDocumentKind]string{
		constants.ProfileDocumentKindIdentity: "## 擅长领域\n",
		constants.ProfileDocumentKindPersona:  "## 工作风格\n",
		constants.ProfileDocumentKindPlaybook: "## 工作方式\n",
	})
	if err != nil {
		return nil, ginx.WithStack(status_error.ProfileArchiveGenerationFailed)
	}
	attachment := ginx.NewAttachment("ally-waker-profile-template.zip", "application/zip")
	_, _ = attachment.Write(content)
	return attachment, nil
}
