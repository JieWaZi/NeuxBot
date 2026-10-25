package workflows

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 校验工作流源码
type ValidateWorkflowSource struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body ValidateWorkflowSourceRequest `in:"body"`
}

// 源码校验请求
type ValidateWorkflowSourceRequest struct {
	// 完整源码
	Source string `json:"source" validate:"required,max=1048576"`
	// 契约版本
	SDKVersion string `json:"sdkVersion" validate:"required,max=80"`
}

// 源码校验结果
type WorkflowValidationResponse struct {
	// 校验通过
	Valid bool `json:"valid"`
	// 编译诊断
	Diagnostics []workflowdsl.Diagnostic `json:"diagnostics"`
	// 源码投影
	Manifest *workflowdsl.Manifest `json:"manifest"`
}

func (*ValidateWorkflowSource) Path() string { return "/:id/validate" }
func (o *ValidateWorkflowSource) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.WorkflowControllerManager.GetDefinition(ctx, o.ID); err != nil {
		return nil, ginx.WithStack(err)
	}
	if o.Body.SDKVersion != constants.WorkflowSDKVersion {
		return nil, ginx.WithStack(status_error.WorkflowSourceInvalid)
	}
	compiled, diagnostics, err := workflowdsl.Compile(o.Body.Source)
	response := WorkflowValidationResponse{Valid: err == nil, Diagnostics: diagnostics}
	if err == nil {
		response.Manifest = &compiled.Manifest
	}
	return response, nil
}
