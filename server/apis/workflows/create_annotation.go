package workflows

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 添加修改批注
type CreateWorkflowAnnotation struct {
	// 通过嵌入契约声明该接口使用 HTTP POST 方法。
	ginx.MethodPost
	// 工作流标识
	ID string `in:"path" name:"id" validate:"required,max=32"`
	// 请求体
	Body WorkflowAnnotationRequest `in:"body"`
}

// 批注请求
type WorkflowAnnotationRequest struct {
	// 请求身份
	RequestID string `json:"requestId" validate:"required,max=128"`
	// 基准版本
	BaseRevisionID string `json:"baseRevisionId" validate:"required,max=32"`
	// 目标节点
	NodeID string `json:"nodeId" validate:"required,max=64"`
	// 修改要求
	Text string `json:"text" validate:"required,max=10000"`
}

func (*CreateWorkflowAnnotation) Path() string { return "/:id/annotations" }
func (o *CreateWorkflowAnnotation) Output(ctx *gin.Context) (interface{}, error) {
	annotation, err := createAnnotation(ctx, dependencies(), CreateAnnotationCommand{DefinitionID: o.ID, RequestID: o.Body.RequestID, BaseRevisionID: o.Body.BaseRevisionID, NodeID: o.Body.NodeID, Text: o.Body.Text})
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	// 返回身份供用户选择将批注加入本轮创作要求。
	return annotation, nil
}

// 创建批注请求
type CreateAnnotationCommand struct {
	// 工作流标识
	DefinitionID string `json:"-"`
	// 请求标识
	RequestID string `json:"requestId"`
	// 基准版本
	BaseRevisionID string `json:"baseRevisionId"`
	// 目标节点
	NodeID string `json:"nodeId"`
	// 修改要求
	Text string `json:"text"`
}

// createAnnotation 将批注、幂等回执和事件原子保存，供创作下一轮读取。
func createAnnotation(ctx context.Context, d *workflowDependencies, command CreateAnnotationCommand) (models.WorkflowAnnotation, error) {
	command.Text = strings.TrimSpace(command.Text)
	if command.RequestID == "" || len(command.RequestID) > 128 || command.Text == "" || utf8.RuneCountInString(command.Text) > 10000 || d.CreationSessions == nil {
		return models.WorkflowAnnotation{}, status_error.BadRequest
	}
	digest := workflowstate.Digest(command)
	d.State.Commands.Lock()
	defer d.State.Commands.Unlock()
	var annotation models.WorkflowAnnotation
	err := d.Transactions.Transaction(ctx, func(ctx context.Context) error {
		receipt, found, err := d.Store.FindReceipt(ctx, command.DefinitionID, command.RequestID)
		if err != nil {
			return err
		}
		if found {
			if receipt.Kind != "annotation" || receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			encoded, err := json.Marshal(receipt.Result)
			if err != nil || json.Unmarshal(encoded, &annotation) != nil {
				return status_error.WorkflowStateInvalid
			}
			return nil
		}
		revision, err := d.Store.GetRevision(ctx, command.DefinitionID, command.BaseRevisionID)
		if err != nil {
			return err
		}
		found = false
		for _, node := range revision.Manifest.Nodes {
			if node.ID == command.NodeID {
				found = true
			}
		}
		for _, phase := range revision.Manifest.Meta.Phases {
			if phase.ID == command.NodeID {
				found = true
			}
		}
		if !found {
			return status_error.WorkflowResourceInvalid.WithField("field", "nodeId")
		}
		id := requestidempotency.Identity{ID: command.RequestID}.ResourceID("workflow-annotation", command.DefinitionID)
		annotation = models.WorkflowAnnotation{ID: id, DefinitionID: command.DefinitionID, BaseRevisionID: command.BaseRevisionID, NodeID: command.NodeID, Text: command.Text, CreatedAt: time.Now().UnixMilli()}
		if err := d.CreationSessions.CreateAnnotation(ctx, annotation); err != nil {
			return err
		}
		encoded, err := json.Marshal(annotation)
		if err != nil {
			return status_error.WorkflowStateInvalid
		}
		var result map[string]any
		if json.Unmarshal(encoded, &result) != nil {
			return status_error.WorkflowStateInvalid
		}
		if err := d.Store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: command.DefinitionID, RequestID: command.RequestID, Kind: "annotation", PayloadDigest: digest, Result: result}); err != nil {
			return err
		}
		_, err = d.Store.AppendEvent(ctx, models.WorkflowEvent{StreamID: command.DefinitionID, EventID: id, Type: constants.WorkflowAnnotationChangedEvent, EntityID: id, Payload: map[string]any{"annotationId": id, "baseRevisionId": command.BaseRevisionID, "nodeId": command.NodeID}})
		return err
	})
	return annotation, err
}
