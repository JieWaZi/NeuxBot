package automatic_tasks

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/apis/middleware"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
	"io"
	"net/http"
)

// integrationFileUploader 由既有工作流服务提供按主体保存输入的能力。
type integrationFileUploader interface {
	// UploadFileForKey 保存任务加 Key 归属的原件，不接受本机文件路径。
	UploadFileForKey(context.Context, string, string, string, string, io.Reader) (workflowfiles.Upload, error)
}

// 上传公开任务输入文件
type PublishedTaskUpload struct {
	ginx.MethodPost
	// 任务标识
	TaskID string `in:"path" name:"id" validate:"required"`
}

func (*PublishedTaskUpload) Path() string { return "/automatic-tasks/:id/files" }
func (o *PublishedTaskUpload) Output(ctx *gin.Context) (interface{}, error) {
	d := taskDependencies(ctx)
	task, err := d.API.service.IntegrationTask(ctx.Request.Context(), o.TaskID, true)
	if err != nil {
		return nil, err
	}
	uploader, ok := d.API.config.Workflows.(integrationFileUploader)
	if !ok || task.WorkflowID == "" {
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, constants.WorkflowFileMaximumBytes+(1<<20))
	if ctx.Request.ParseMultipartForm(1<<20) != nil {
		return nil, status_error.WorkflowFileInvalid
	}
	defer ctx.Request.MultipartForm.RemoveAll()
	form := ctx.Request.MultipartForm
	if len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
		return nil, status_error.WorkflowFileInvalid
	}
	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		return nil, status_error.WorkflowFileInvalid
	}
	defer file.Close()
	identity, _ := middleware.IntegrationPrincipal(ctx.Request.Context())
	result, err := uploader.UploadFileForKey(ctx.Request.Context(), task.WorkflowID, task.ID, identity.KeyID, header.Filename, file)
	if err != nil {
		return nil, err
	}
	if _, err = d.Auth.Authorize(ctx.Request.Context(), identity, constants.IntegrationResourceKindAutomaticTask, task.ID, []constants.IntegrationCapability{constants.IntegrationCapabilityTaskInvoke}); err != nil {
		return nil, err
	}
	ctx.JSON(http.StatusCreated, result)
	return result, nil
}

// 下载本主体任务产出
type PublishedTaskDownload struct {
	ginx.MethodGet
	// 任务标识
	TaskID string `in:"path" name:"id" validate:"required"`
	// 运行标识
	RunID string `in:"path" name:"runId" validate:"required"`
	// 产出标识
	FileID string `in:"path" name:"fileId" validate:"required"`
}

func (*PublishedTaskDownload) Path() string { return "/automatic-tasks/:id/runs/:runId/files/:fileId" }
func (o *PublishedTaskDownload) Output(ctx *gin.Context) (interface{}, error) {
	d := taskDependencies(ctx)
	run, err := ownedIntegrationRun(ctx, o.TaskID, o.RunID)
	if err != nil {
		return nil, err
	}
	if run.Configuration.WorkflowID == "" || d.API.config.Workflows == nil {
		return nil, status_error.AutomaticRunNotFound
	}
	file, name, err := d.API.config.Workflows.OpenOutputFile(ctx.Request.Context(), run.Configuration.WorkflowID, run.WorkflowRunID, o.FileID)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	serveWorkflowFile(ctx.Writer, ctx.Request, file, name)
	return nil, nil
}
