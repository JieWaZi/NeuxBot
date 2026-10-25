package workflows

import (
	"context"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// DeleteWorkflowRevision 移除历史版本入口，当前版本不能删除。
type DeleteWorkflowRevision struct {
	// MethodDelete 声明 HTTP DELETE 方法。
	ginx.MethodDelete
	// ID 是所属工作流身份。
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// RevisionID 是需要删除的历史版本身份。
	RevisionID string `in:"path" name:"revisionId" validate:"required,max=32"`
}

func (*DeleteWorkflowRevision) Path() string { return "/:id/revisions/:revisionId" }
func (o *DeleteWorkflowRevision) Output(ctx *gin.Context) (interface{}, error) {
	if err := deleteRevision(ctx, dependencies(), o.ID, o.RevisionID); err != nil {
		return nil, ginx.WithStack(err)
	}
	return true, nil
}

// deleteRevision 删除历史入口，不改变已保存的程序和已固定该版本的运行。
func deleteRevision(ctx context.Context, d *workflowDependencies, id, revisionID string) error {
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	return d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		definition, err := d.Store.GetDefinition(ctx, id)
		if err != nil {
			return err
		}
		if definition.CurrentRevisionID == revisionID {
			return status_error.WorkflowStateConflict.WithField("reason", "current_revision")
		}
		revision, err := d.Store.GetRevision(ctx, id, revisionID)
		if err != nil {
			return err
		}
		if revision.DeletedAt != nil {
			return nil
		}
		if err := d.Store.DeleteRevision(ctx, id, revisionID, time.Now().UnixMilli()); err != nil {
			return err
		}
		_, err = d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: id, EventID: util.New(), Type: "workflow.revision.deleted", EntityID: revisionID, Payload: map[string]any{"revisionId": revisionID}})
		return err
	})
}
