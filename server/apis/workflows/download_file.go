package workflows

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 下载工作流文件
type DownloadWorkflowFile struct {
	// MethodGet 声明附件下载使用 GET 请求。
	ginx.MethodGet
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required,max=32"`
	// 产物标识
	FileID string `in:"query" name:"fileId" validate:"omitempty,len=32"`
	// 节点实例
	InstanceID string `in:"query" name:"instanceId" validate:"omitempty,len=32"`
	// 执行尝试
	AttemptID string `in:"query" name:"attemptId" validate:"omitempty,len=32"`
	// 文件路径
	Candidate string `in:"query" name:"path" validate:"max=4096"`
}

// Path 返回受 Core 管理认证保护的下载地址。
func (*DownloadWorkflowFile) Path() string { return "/:id/runs/:runId/file" }

// Validate 保证只指定已声明产物身份或一个待检查的路径。
func (o *DownloadWorkflowFile) Validate(*gin.Context) error {
	if (o.InstanceID == "") != (o.AttemptID == "") || (o.AttemptID != "" && o.FileID == "") || (o.FileID == "") == (o.Candidate == "") {
		return status_error.BadRequest
	}
	return nil
}

// Output 在冻结范围校验后返回所有文件类型的下载附件。
func (o *DownloadWorkflowFile) Output(ctx *gin.Context) (interface{}, error) {
	var file *os.File
	var name string
	var err error
	if o.AttemptID != "" {
		run, effect, _, schema, lookupErr := nodeResultFacts(ctx, o.ID, o.RunID, o.InstanceID, o.AttemptID)
		if lookupErr != nil {
			return nil, ginx.WithStack(lookupErr)
		}
		if effect.Status != constants.WorkflowStepStatusSucceeded || effect.Result == nil {
			return nil, ginx.WithStack(status_error.WorkflowFileNotFound)
		}
		file, name, err = workflow.OpenDeclaredOutputFile(run, schema, effect.Result.Data, o.FileID)
	} else if o.FileID != "" {
		file, name, err = global.WorkflowService.OpenOutputFile(ctx, o.ID, o.RunID, o.FileID)
	} else {
		file, err = openArtifact(ctx, dependencies(), o.ID, o.RunID, o.Candidate)
		name = filepath.Base(o.Candidate)
	}
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, constants.WorkflowFileMaximumBytes+1))
	if err != nil {
		logx.Errorf("read workflow download failed: run_id=%s error=%v", o.RunID, err)
		return nil, ginx.WithStack(status_error.WorkflowFileStorageError)
	}
	if int64(len(data)) > constants.WorkflowFileMaximumBytes {
		return nil, ginx.WithStack(status_error.WorkflowBudgetExceeded.WithField("budget", "file_bytes"))
	}
	attachment := ginx.NewAttachment(name, "application/octet-stream")
	_, _ = attachment.Write(data)
	return attachment, nil
}

// openArtifact 为管理端的预览产物提供不限扩展名的真实下载。
func openArtifact(ctx context.Context, d *workflowDependencies, definitionID, runID, path string) (*os.File, error) {
	run, err := d.Runs.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.DefinitionID != definitionID {
		return nil, status_error.WorkflowFileNotFound
	}
	return workflowfiles.OpenScopeFile(run.ScopeBinding.WorkingDirectory, path)
}
