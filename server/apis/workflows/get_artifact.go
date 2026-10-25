package workflows

import (
	"encoding/base64"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 检查运行文件
type GetWorkflowArtifact struct {
	// 通过嵌入契约声明该接口使用 HTTP GET 方法。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 候选路径
	Candidate string `in:"query" name:"path" validate:"required,max=4096"`
	// 先前摘要
	ExpectedDigest string `in:"query" name:"expectedDigest" validate:"omitempty,len=64"`
}

// Path 返回运行文件预览接口路径。
func (*GetWorkflowArtifact) Path() string { return "/:id/runs/:runId/artifact" }

// Output 校验运行归属并组装冻结范围内文件的预览。
func (o *GetWorkflowArtifact) Output(ctx *gin.Context) (interface{}, error) {
	run, err := global.WorkflowRunControllerManager.GetRun(ctx, o.RunID)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	if run.DefinitionID != o.ID {
		return nil, ginx.WithStack(status_error.WorkflowNotFound)
	}
	preview, err := workflowfiles.ReadPreview(ctx, run.ScopeBinding.WorkingDirectory, o.Candidate)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	response := WorkflowArtifactResponse{Candidate: o.Candidate, CheckedAt: time.Now().UnixMilli(), Reason: preview.Reason}
	if preview.Reason != "" {
		return response, nil
	}
	response.Verified = true
	response.RelativePath = preview.RelativePath
	response.MediaType = preview.MediaType
	response.Size = preview.Size
	response.Digest = preview.Digest
	response.Changed = o.ExpectedDigest != "" && o.ExpectedDigest != preview.Digest
	if preview.Text {
		value := string(preview.Content)
		response.Text = &value
	} else {
		response.DataURL = "data:" + preview.MediaType + ";base64," + base64.StdEncoding.EncodeToString(preview.Content)
	}
	return response, nil
}

// 工作流文件预览
type WorkflowArtifactResponse struct {
	// 候选路径
	Candidate string `json:"candidate"`
	// 校验通过
	Verified bool `json:"verified"`
	// 失败原因
	Reason string `json:"reason,omitempty"`
	// 相对路径
	RelativePath string `json:"relativePath,omitempty"`
	// 媒体类型
	MediaType string `json:"mediaType,omitempty"`
	// 文件大小
	Size int64 `json:"size"`
	// 内容摘要
	Digest string `json:"digest,omitempty"`
	// 检查时间
	CheckedAt int64 `json:"checkedAt"`
	// 内容变化
	Changed bool `json:"changed"`
	// 文本内容
	Text *string `json:"text,omitempty"`
	// 编码内容
	DataURL string `json:"dataUrl,omitempty"`
}
