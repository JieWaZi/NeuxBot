package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 恢复工作流版本
type RestoreWorkflowRevision struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body RestoreWorkflowRevisionRequest `in:"body"`
}

// 版本恢复请求
type RestoreWorkflowRevisionRequest struct {
	// 请求标识
	RequestID string `json:"requestId" validate:"required,max=128"`
	// 当前版本
	ExpectedRevisionID string `json:"expectedRevisionId" validate:"required,max=32"`
	// 恢复版本
	TargetRevisionID string `json:"targetRevisionId" validate:"required,max=32"`
}

func (*RestoreWorkflowRevision) Path() string { return "/:id/restore" }
func (o *RestoreWorkflowRevision) Output(ctx *gin.Context) (interface{}, error) {
	receipt, diagnostics, err := restoreSource(ctx, dependencies(), o.ID, o.Body.TargetRevisionID, o.Body.ExpectedRevisionID, o.Body.RequestID)
	if err != nil {
		if len(diagnostics) > 0 {
			return nil, ginx.WithStack(status_error.WorkflowSourceInvalid.WithParams(map[string]interface{}{"diagnostics": diagnostics}))
		}
		return nil, ginx.WithStack(err)
	} /* 前端需要新版本身份退出旧版查看。 */
	return receipt, nil
}

// restoreSource 重新编译旧版并新增版本，不修改历史或运行中的版本。
func restoreSource(ctx context.Context, d *workflowDependencies, definitionID, targetRevisionID, expectedRevisionID, requestID string) (workflowdsl.SaveReceipt, []workflowdsl.Diagnostic, error) {
	revision, err := d.Store.GetRevision(ctx, definitionID, targetRevisionID)
	if err != nil {
		return workflowdsl.SaveReceipt{}, nil, err
	}
	return d.SaveSource(ctx, definitionID, requestID, expectedRevisionID, revision.Source, revision.ID)
}
