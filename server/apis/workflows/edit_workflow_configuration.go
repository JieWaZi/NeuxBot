package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// EditWorkflowConfiguration 保存用户直接修改的工作流配置。
type EditWorkflowConfiguration struct {
	// MethodPut 声明此配置命令使用 HTTP PUT。
	ginx.MethodPut
	// ID 是路由中的工作流稳定身份。
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// Body 包含打开表单时的版本及实际改动。
	Body EditConfigurationCommand `in:"body"`
}

func (*EditWorkflowConfiguration) Path() string { return "/:id/configuration" }
func (o *EditWorkflowConfiguration) Output(ctx *gin.Context) (interface{}, error) {
	o.Body.DefinitionID = o.ID
	receipt, diagnostics, err := editConfiguration(ctx, dependencies(), o.Body)
	if err != nil {
		if len(diagnostics) > 0 {
			return nil, ginx.WithStack(status_error.WorkflowSourceInvalid.WithParams(map[string]interface{}{"diagnostics": diagnostics}))
		}
		return nil, ginx.WithStack(err)
	}
	return receipt, nil
}

// 配置修改请求
type EditConfigurationCommand struct {
	// 工作流标识
	DefinitionID string `json:"-"`
	// 请求标识
	RequestID string `json:"requestId"`
	// 当前版本
	ExpectedRevisionID string `json:"expectedRevisionId"`
	// 配置修改
	Edit workflowdsl.SourceEdit `json:"edit"`
}

// editConfiguration 与 Agent 保存共用编译、资源校验、幂等回执和版本 CAS。
func editConfiguration(ctx context.Context, d *workflowDependencies, command EditConfigurationCommand) (workflowdsl.SaveReceipt, []workflowdsl.Diagnostic, error) {
	if command.ExpectedRevisionID == "" || command.RequestID == "" {
		return workflowdsl.SaveReceipt{}, nil, status_error.BadRequest
	}
	revision, err := d.Store.GetRevision(ctx, command.DefinitionID, command.ExpectedRevisionID)
	if err != nil {
		return workflowdsl.SaveReceipt{}, nil, err
	}
	source, err := workflowdsl.EditSource(revision.Source, command.Edit)
	if err != nil {
		return workflowdsl.SaveReceipt{}, nil, err
	}
	return d.SaveSource(ctx, command.DefinitionID, command.RequestID, command.ExpectedRevisionID, source, "")
}
