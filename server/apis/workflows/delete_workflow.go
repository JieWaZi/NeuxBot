package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 删除工作流
type DeleteWorkflow struct {
	// 通过嵌入契约声明该接口使用 HTTP DELETE 方法。
	ginx.MethodDelete
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 删除依据
	Body DeleteWorkflowBody `in:"body"`
}

// 删除工作流依据
type DeleteWorkflowBody struct {
	// 当前版本
	ExpectedRevisionID string `json:"expectedRevisionId" validate:"max=32"`
}

func (*DeleteWorkflow) Path() string { return "/:id" }
func (o *DeleteWorkflow) Output(ctx *gin.Context) (interface{}, error) {
	if err := deleteDefinition(ctx, dependencies(), o.ID, o.Body.ExpectedRevisionID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return true, nil
}

// deleteDefinition 原子验证依赖并删除资源入口，既有运行和不可变证据完整保留。
func deleteDefinition(ctx context.Context, d *workflowDependencies, id, expectedRevisionID string) error {
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	return d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		definition, err := d.Store.GetDefinition(ctx, id)
		if workflowstate.ErrorKey(err) == status_error.WorkflowNotFound.Key() {
			return nil
		}
		if err != nil {
			return err
		}
		if definition.CurrentRevisionID != expectedRevisionID {
			return status_error.WorkflowRevisionConflict
		}
		if err := d.Store.DeleteDefinition(ctx, id, expectedRevisionID); err != nil {
			return err
		}
		_, err = d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: id, EventID: util.New(), Type: "workflow.definition.deleted", EntityID: id, Payload: map[string]any{"definitionId": id}})
		return err
	})
}
