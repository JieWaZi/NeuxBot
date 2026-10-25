package workflows

import (
	"mime/multipart"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 上传工作流文件
type UploadWorkflowFile struct {
	// MethodPost 声明文件上传使用 POST 请求。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 上传文件
	File *multipart.FileHeader `in:"multipart" name:"file" validate:"required"`
	// 文件名称
	Name string `in:"form" name:"name" validate:"max=255"`
}

// Path 返回受 Core 管理认证保护的文件上传地址。
func (*UploadWorkflowFile) Path() string { return "/:id/files" }

// Output 返回后续参数表单运行所需的受管文件引用。
func (o *UploadWorkflowFile) Output(ctx *gin.Context) (interface{}, error) {
	if ctx.Request.MultipartForm != nil {
		defer ctx.Request.MultipartForm.RemoveAll()
	}
	if o.File.Size > constants.WorkflowFileMaximumBytes {
		return nil, ginx.WithStack(status_error.WorkflowBudgetExceeded.WithField("budget", "file_bytes"))
	}
	file, err := o.File.Open()
	if err != nil {
		logx.Errorf("open workflow upload failed: workflow_id=%s error=%v", o.ID, err)
		return nil, ginx.WithStack(status_error.WorkflowFileStorageError)
	}
	defer file.Close()
	name := o.Name
	if name == "" {
		name = o.File.Filename
	}
	result, err := global.WorkflowService.UploadFile(ctx, o.ID, "", name, file)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
