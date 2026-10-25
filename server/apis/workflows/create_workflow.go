package workflows

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinwaker"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 创建工作流
type CreateWorkflow struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 请求体
	Body CreateDefinitionCommand `in:"body"`
}

func (*CreateWorkflow) Path() string { return "" }
func (o *CreateWorkflow) Output(ctx *gin.Context) (interface{}, error) {
	definition, err := createDefinition(ctx, dependencies(), o.Body)
	if err != nil {
		return nil, ginx.WithStack(err)
	} /* 创建后前端需要稳定身份进入空 Scene。 */
	return definition, nil
}

// 创建工作流请求
type CreateDefinitionCommand struct {
	// 请求标识
	RequestID string `json:"requestId"`
	// 草稿标题
	Title string `json:"-"`
	// 创作伙伴
	CreationWakerID string `json:"-"`
	// 执行范围
	ScopeBinding models.WorkflowScopeBinding `json:"-"`
}

// createDefinition 建立空定义；同内容同请求不产生第二个工作流。
func createDefinition(ctx context.Context, d *workflowDependencies, command CreateDefinitionCommand) (models.WorkflowDefinition, error) {
	command.Title = strings.TrimSpace(command.Title)
	if command.RequestID == "" || len(command.RequestID) > 128 || len(command.Title) > 120 {
		return models.WorkflowDefinition{}, status_error.BadRequest
	}
	digest := workflowstate.Digest([]any{command.RequestID, command.Title, command.CreationWakerID, command.ScopeBinding})
	if receipt, found, err := d.Store.FindReceipt(ctx, "workflow.create", command.RequestID); err != nil {
		return models.WorkflowDefinition{}, err
	} else if found {
		if receipt.Kind != "create" || receipt.PayloadDigest != digest {
			return models.WorkflowDefinition{}, status_error.WorkflowRequestConflict
		}
		id, _ := receipt.Result["definitionId"].(string)
		return d.Store.GetDefinition(ctx, id)
	}
	if command.ScopeBinding.ProjectID != "" {
		if d.Projects == nil {
			return models.WorkflowDefinition{}, status_error.WorkflowResourceInvalid
		}
		project, err := d.Projects.Get(ctx, command.ScopeBinding.ProjectID)
		if err != nil {
			return models.WorkflowDefinition{}, err
		}
		if project.WakerID != command.CreationWakerID || command.ScopeBinding.WorkingDirectory != "" {
			return models.WorkflowDefinition{}, status_error.WorkflowResourceInvalid
		}
	}
	if command.ScopeBinding.WorkingDirectory != "" {
		directory, err := workflowfiles.EnsureScopeDirectory(command.ScopeBinding.WorkingDirectory)
		if err != nil {
			return models.WorkflowDefinition{}, err
		}
		command.ScopeBinding.WorkingDirectory = directory
	}
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	var definition models.WorkflowDefinition
	err := d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		receipt, found, err := d.Store.FindReceipt(ctx, "workflow.create", command.RequestID)
		if err != nil {
			return err
		}
		if found {
			if receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			id, _ := receipt.Result["definitionId"].(string)
			definition, err = d.Store.GetDefinition(ctx, id)
			return err
		}
		creationWakerID := command.CreationWakerID
		if creationWakerID == "" && d.Wakers != nil {
			creationWakerID = builtinrolepreset.PersonalAssistantWakerID
			preset, _ := builtinrolepreset.DefinitionByWakerID(creationWakerID)
			if err := builtinwaker.EnsureDefinition(ctx, d.Wakers, preset); err != nil {
				return err
			}
		}
		definition = models.WorkflowDefinition{ID: util.New(), Title: command.Title, CreationWakerID: creationWakerID, ScopeBinding: command.ScopeBinding}
		if err := d.Store.CreateDefinition(ctx, definition); err != nil {
			return err
		}
		if _, err := d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definition.ID, EventID: util.New(), Type: "workflow.definition.created", EntityID: definition.ID, Payload: map[string]any{"definitionId": definition.ID}}); err != nil {
			return err
		}
		return d.Store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: "workflow.create", RequestID: command.RequestID, Kind: "create", PayloadDigest: digest, Result: map[string]any{"definitionId": definition.ID}})
	})
	if err != nil {
		return definition, err
	}
	return d.Store.GetDefinition(ctx, definition.ID)
}
