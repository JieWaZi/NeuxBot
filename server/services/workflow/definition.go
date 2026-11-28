package workflow

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinwaker"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// CreateDefinitionCommand 保存创建草稿时确定的归属和工作范围。
type CreateDefinitionCommand struct {
	// RequestID 标识可安全重放的创建请求。
	RequestID string
	// Title 是草稿的可见名称。
	Title string
	// CreationWakerID 是宿主确定的创作伙伴。
	CreationWakerID string
	// ScopeBinding 是已选定的工作范围。
	ScopeBinding models.WorkflowScopeBinding
	// ExecutionChatID 是发起创建工具的真实聊天，普通管理请求留空。
	ExecutionChatID string
	// ExecutionID 是事务内复核的当前执行。
	ExecutionID string
}

// CreateDefinition 建立空定义；同内容同请求不产生第二个工作流。
func (s *Service) CreateDefinition(ctx context.Context, command CreateDefinitionCommand) (models.WorkflowDefinition, error) {
	command.Title = strings.TrimSpace(command.Title)
	if command.RequestID == "" || len(command.RequestID) > 128 || !utf8.ValidString(command.Title) || utf8.RuneCountInString(command.Title) > 120 {
		return models.WorkflowDefinition{}, status_error.BadRequest
	}
	digest := workflowstate.Digest([]any{command.RequestID, command.Title, command.CreationWakerID, command.ScopeBinding})
	if !capability.Active(ctx) {
		if receipt, found, err := s.store.FindReceipt(ctx, "workflow.create", command.RequestID); err != nil {
			return models.WorkflowDefinition{}, err
		} else if found {
			if receipt.Kind != "create" || receipt.PayloadDigest != digest {
				return models.WorkflowDefinition{}, status_error.WorkflowRequestConflict
			}
			id, _ := receipt.Result["definitionId"].(string)
			return s.store.GetDefinition(ctx, id)
		}
	}
	if command.ScopeBinding.ProjectID != "" {
		if s.projects == nil {
			return models.WorkflowDefinition{}, status_error.WorkflowResourceInvalid
		}
		project, err := s.projects.Get(ctx, command.ScopeBinding.ProjectID)
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
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	var definition models.WorkflowDefinition
	err := s.transactions.Transaction(ctx, func(ctx context.Context) error {
		if command.ExecutionChatID != "" {
			_, chat, err := s.readWorkflowExecution(ctx, command.ExecutionChatID, command.ExecutionID)
			if err != nil {
				return err
			}
			if chat.WakerID == "" || chat.WakerID != command.CreationWakerID {
				return status_error.AgentOperationForbidden
			}
			if chat.HiddenFromWakerHistory {
				if err := s.checkMainWork(ctx, chat); err != nil {
					return err
				}
			}
		}
		receipt, found, err := s.store.FindReceipt(ctx, "workflow.create", command.RequestID)
		if err != nil {
			return err
		}
		if found {
			if receipt.Kind != "create" || receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			id, _ := receipt.Result["definitionId"].(string)
			definition, err = s.store.GetDefinition(ctx, id)
			if err != nil {
				return err
			}
			return capability.Commit(ctx, map[string]any{"definitionId": definition.ID})
		}
		creationWakerID := command.CreationWakerID
		if creationWakerID == "" && s.wakers != nil {
			creationWakerID = builtinrolepreset.PersonalAssistantWakerID
			preset, _ := builtinrolepreset.DefinitionByWakerID(creationWakerID)
			if err := builtinwaker.EnsureDefinition(ctx, s.wakers, preset); err != nil {
				return err
			}
		}
		definition = models.WorkflowDefinition{ID: util.New(), Title: command.Title, CreationWakerID: creationWakerID, ScopeBinding: command.ScopeBinding}
		if err := s.store.CreateDefinition(ctx, definition); err != nil {
			return err
		}
		if _, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definition.ID, EventID: util.New(), Type: "workflow.definition.created", EntityID: definition.ID, Payload: map[string]any{"definitionId": definition.ID}}); err != nil {
			return err
		}
		data := map[string]any{"definitionId": definition.ID}
		if err := s.store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: "workflow.create", RequestID: command.RequestID, Kind: "create", PayloadDigest: digest, Result: data}); err != nil {
			return err
		}
		return capability.Commit(ctx, data)
	})
	if err != nil {
		return definition, err
	}
	return s.store.GetDefinition(ctx, definition.ID)
}
