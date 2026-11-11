package workflows

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/resourcecategory"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 修改工作流分类
type UpdateWorkflowCategories struct {
	ginx.MethodPut
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body UpdateWorkflowCategoriesRequest `in:"body"`
}

// 工作流分类请求
type UpdateWorkflowCategoriesRequest struct {
	// 分类标签
	Categories []string `json:"categories"`
}

// Path 返回独立于版本源码的目录分类入口。
func (*UpdateWorkflowCategories) Path() string { return "/:id/categories" }

// Output 将分类词库、定义标签和变更事件在同一事务提交。
func (o *UpdateWorkflowCategories) Output(ctx *gin.Context) (interface{}, error) {
	names, err := resourcecategory.Normalize(o.Body.Categories)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	err = global.TransactionControllerManager.Transaction(ctx, func(txctx context.Context) error {
		if _, err := global.WorkflowControllerManager.GetDefinition(txctx, o.ID); err != nil {
			return err
		}
		if err := global.ResourceCategoryControllerManager.Ensure(txctx, constants.ResourceCategoryTypeWorkflow, names); err != nil {
			return err
		}
		if err := global.WorkflowControllerManager.UpdateCategories(txctx, o.ID, names); err != nil {
			return err
		}
		_, err := global.WorkflowControllerManager.AppendEvent(txctx, models.WorkflowEvent{StreamID: o.ID, EventID: util.New(), Type: constants.WorkflowDefinitionUpdatedEvent, EntityID: o.ID, Payload: map[string]any{"definitionId": o.ID}})
		return err
	})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return ginx.CommonSuccessResponse(), nil
}
