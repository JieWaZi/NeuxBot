package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentcap "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

// BindLocalWorkflowTurn 为本地交互回合准备共同工具和默认目标。实际调用前不创建运行记录。
func (s *Service) BindLocalWorkflowTurn(ctx context.Context, chat models.Chat, executionID string, selected *workinput.WorkflowReference, creationSessionID string) (execution.WorkflowTurnBinding, error) {
	var bound execution.WorkflowTurnBinding
	scope := workflowtool.Scope{Key: creationSessionID, ExecutionID: executionID, LocalChatID: chat.ID, SavedRevisions: &sync.Map{}}
	if creationSessionID != "" {
		session, err := s.creationSessions.GetCreationSession(ctx, creationSessionID)
		if err != nil {
			return bound, err
		}
		if session.ActiveExecutionID != executionID || (session.ChatID != "" && session.ChatID != chat.ID) {
			return bound, status_error.WorkflowAuthoringSuperseded
		}
		scope.DefaultWorkflowID = session.DefinitionID
	} else if selected != nil {
		reference, err := s.FreezeWorkflowSelection(ctx, selected)
		if err != nil {
			return bound, err
		}
		scope.DefaultWorkflowID = reference.ID
		scope.Selection = reference
		bound.Reference = reference
	}
	ids := append(append([]string{}, workflowAuthoringOperations...), workflowRunOperations...)
	ids = append(ids, "workflow.runs.list", "workflow.runs.repeat")
	bound.Capability = &execution.ToolCapability{ID: "workflow.local", Name: "工作流", Bindings: s.agentBindings(scope, ids)}
	bound.Instruction = "Follow the user's request to read, edit, or run a workflow. Use workflow.definitions.list to find a named workflow, or workflow.runs.list to locate a prior run in this chat. For a repair, read the run evidence when relevant and read the current source before saving. Use the saved revision for any subsequent run the user requests."
	if scope.DefaultWorkflowID != "" {
		bound.Instruction += " The default workflow for this turn is " + scope.DefaultWorkflowID + "."
	}
	if scope.Selection != nil {
		bound.Instruction += " Read and run the selected revision " + scope.Selection.RevisionID + " unless the user asks you to run a revision you explicitly save in this turn."
	}
	return bound, nil
}

func (s *Service) readWorkflowExecution(ctx context.Context, chatID, executionID string) (models.Execution, models.Chat, error) {
	if s.chats == nil {
		return models.Execution{}, models.Chat{}, status_error.WorkflowCapabilityUnsupported
	}
	actual, err := s.chats.ReadExecution(ctx, chatID, executionID)
	if err != nil {
		return actual, models.Chat{}, err
	}
	summary, err := s.chats.GetChatSummary(ctx, chatID)
	if err != nil {
		return actual, models.Chat{}, err
	}
	if actual.CompletedAt != nil || actual.Status != constants.ExecutionStatusPending && actual.Status != constants.ExecutionStatusRunning ||
		summary.Chat.ActiveExecutionID != executionID {
		return actual, summary.Chat, status_error.WorkflowStateConflict
	}
	return actual, summary.Chat, nil
}

func workflowExecutionDefaults(actual models.Execution) models.WorkflowExecutionDefaults {
	return models.WorkflowExecutionDefaults{RuntimeID: actual.RuntimeID, AccountID: actual.AccountID, Model: actual.Model, Reasoning: actual.Reasoning, Fast: actual.Fast, PermissionLevel: actual.PermissionLevel, WorkspaceReadOnly: actual.WorkspaceReadOnly}
}

func (s *Service) authorizeLocalWorkflowTool(ctx context.Context, scope workflowtool.Scope) (workflowToolAccess, error) {
	actual, chat, err := s.readWorkflowExecution(ctx, scope.LocalChatID, scope.ExecutionID)
	if err != nil {
		return workflowToolAccess{}, err
	}
	return workflowToolAccess{
		owner: "workflow.local:" + chat.ID + ":" + actual.ID,
		call: func(ctx context.Context, input workflowtool.Input) (map[string]any, error) {
			return s.handleLocalWorkflowTool(ctx, scope, actual, chat, input)
		},
	}, nil
}

func (s *Service) handleLocalWorkflowTool(ctx context.Context, scope workflowtool.Scope, actual models.Execution, chat models.Chat, input workflowtool.Input) (map[string]any, error) {
	lease := models.WorkflowConsumerLease{ID: actual.ID, ChatID: chat.ID}
	switch input.Action {
	case constants.WorkflowActionRunList:
		return s.handleConsumerTool(ctx, lease, actual, chat, input)
	case constants.WorkflowActionRunStart:
		revision, err := s.store.GetRevision(ctx, input.WorkflowID, input.RevisionID)
		if err != nil {
			return nil, err
		}
		lease.Selection = &workinput.WorkflowReference{ID: input.WorkflowID, RevisionID: revision.ID, Name: revision.Manifest.Meta.Title}
		return s.startConsumerRun(ctx, lease, actual, chat, input, true)
	case constants.WorkflowActionRunRepeat, constants.WorkflowActionRunCancel:
		if input.Action == constants.WorkflowActionRunRepeat && scope.Selection != nil {
			return nil, status_error.AgentOperationForbidden
		}
		run, err := s.visibleConsumerRun(ctx, lease, input.RunID)
		if err != nil {
			return nil, err
		}
		return s.handleConsumerRun(ctx, lease, run, actual, chat, input, true)
	case constants.WorkflowActionRunGet:
		run, err := s.runs.GetRun(ctx, input.RunID)
		if err != nil {
			return nil, err
		}
		if run.HistoryDeletedAt != nil {
			return nil, status_error.WorkflowNotFound
		}
		// 源码修复可按明确的 Run ID 读取本地运行证据。
		return s.handleConsumerRun(ctx, lease, run, actual, chat, input, true)
	}

	targetID := input.WorkflowID
	if input.Action == constants.WorkflowActionRunsGet {
		run, err := s.runs.GetRun(ctx, input.RunID)
		if err != nil {
			return nil, err
		}
		if run.HistoryDeletedAt != nil {
			return nil, status_error.WorkflowNotFound
		}
		targetID = run.DefinitionID
	}
	if targetID == "" {
		targetID = scope.DefaultWorkflowID
	}
	authoring := input.Action == constants.WorkflowActionQuery && input.Type == constants.WorkflowQueryAuthoring
	sourceTarget := authoring || input.Action == constants.WorkflowActionSourceGet || input.Action == constants.WorkflowActionSourceUpsert
	if sourceTarget {
		if targetID == "" {
			return nil, status_error.AgentInputInvalid.WithField("field", "workflowId")
		}
		if scope.DefaultWorkflowID != "" && targetID != scope.DefaultWorkflowID {
			return nil, status_error.WorkflowStateConflict
		}
	}

	session := models.WorkflowCreationSession{DefinitionID: targetID, ChatID: chat.ID, ActiveExecutionID: actual.ID, Defaults: workflowExecutionDefaults(actual)}
	if scope.Key != "" && targetID == scope.DefaultWorkflowID {
		stored, err := s.creationSessions.GetCreationSession(ctx, scope.Key)
		if err != nil {
			return nil, err
		}
		if err := s.validateCreationExecution(ctx, stored, scope.ExecutionID); err != nil {
			return nil, err
		}
		session = stored
	}
	if targetID == "" && input.Action == constants.WorkflowActionAnnotationsList {
		return nil, status_error.AgentInputInvalid.WithField("field", "workflowId")
	}
	return s.handleCreationTool(ctx, session, input)
}

func (s *Service) readLocalWorkflowDefinitions(ctx context.Context, scope workflowtool.Scope, request agentcap.Request) (agentcap.Result, error) {
	if request.Operation == "workflow.definitions.describe" {
		id, _ := request.Params["definitionId"].(string)
		revisionID, _ := request.Params["revisionId"].(string)
		revisionID, err := s.localWorkflowRevision(scope, id, revisionID)
		if err != nil {
			return agentcap.Result{}, err
		}
		item, err := s.describeWorkflowDefinition(ctx, id, revisionID)
		return agentcap.Result{Data: item}, err
	}
	if scope.Selection != nil {
		item, err := s.describeWorkflowDefinition(ctx, scope.Selection.ID, scope.Selection.RevisionID)
		if err != nil {
			return agentcap.Result{}, err
		}
		items := []map[string]any{}
		query, _ := request.Params["search"].(string)
		if matchesWorkflowDefinition(item, query) {
			items = append(items, item)
		}
		page, err := agentcap.Page(items, request.Params)
		return agentcap.Result{Data: page}, err
	}
	var query struct {
		// Search 按工作流名称或说明匹配。
		Search string `json:"search"`
		// Cursor 指定上一页末项。
		Cursor string `json:"cursor"`
		// PageNum 为首次查询跳过指定页。
		PageNum int `json:"pageNum"`
		// PageSize 限定单页返回数量。
		PageSize int `json:"pageSize"`
	}
	if err := agentcap.DecodeParams(request.Params, &query); err != nil {
		return agentcap.Result{}, err
	}
	size := query.PageSize
	if size == 0 {
		size = 50
	}
	position := workflowResourceCursor{}
	if query.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(query.Cursor)
		if err != nil || json.Unmarshal(data, &position) != nil || position.UpdatedAt <= 0 || position.ID == "" || query.PageNum != 0 {
			return agentcap.Result{}, status_error.AgentInputInvalid
		}
	}
	page := max(1, query.PageNum)
	if size < 1 || size > 100 || page > 1_000_000 {
		return agentcap.Result{}, status_error.AgentInputInvalid
	}
	values, err := s.store.ListDefinitions(ctx, query.Search, position.UpdatedAt, position.ID, size, (page-1)*size, nil)
	if err != nil {
		return agentcap.Result{}, err
	}
	next := ""
	if len(values) == size {
		last := values[len(values)-1]
		data, _ := json.Marshal(workflowResourceCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(data)
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		items = append(items, localWorkflowDefinition(value, value.CurrentRevisionID))
	}
	return agentcap.Result{Data: map[string]any{"items": items, "nextCursor": next, "pageNum": page, "pageSize": size}}, nil
}

func (s *Service) localWorkflowRevision(scope workflowtool.Scope, id, revisionID string) (string, error) {
	if scope.Selection == nil {
		return revisionID, nil
	}
	if id != scope.Selection.ID {
		return "", status_error.AgentOperationForbidden
	}
	if revisionID == "" || revisionID == scope.Selection.RevisionID {
		return scope.Selection.RevisionID, nil
	}
	if scope.SavedRevisions != nil {
		if _, saved := scope.SavedRevisions.Load(revisionID); saved {
			return revisionID, nil
		}
	}
	return "", status_error.WorkflowRevisionConflict
}

func (s *Service) describeWorkflowDefinition(ctx context.Context, id, revisionID string) (map[string]any, error) {
	definition, err := s.store.GetDefinition(ctx, id)
	if err != nil {
		return nil, err
	}
	if revisionID == "" {
		revisionID = definition.CurrentRevisionID
	}
	item := localWorkflowDefinition(definition, revisionID)
	if revisionID == "" {
		return item, nil
	}
	revision, err := s.store.GetRevision(ctx, id, revisionID)
	if err != nil {
		return nil, err
	}
	meta := revision.Manifest.Meta
	item["name"], item["description"], item["whenToUse"] = meta.Title, meta.Description, meta.WhenToUse
	item["inputSchema"], item["outputSchema"] = meta.InputSchema, meta.OutputSchema
	return item, nil
}

func matchesWorkflowDefinition(item map[string]any, search string) bool {
	name, _ := item["name"].(string)
	description, _ := item["description"].(string)
	whenToUse, _ := item["whenToUse"].(string)
	return strings.Contains(strings.ToLower(name+" "+description+" "+whenToUse), strings.ToLower(strings.TrimSpace(search)))
}

func localWorkflowDefinition(definition models.WorkflowDefinition, revisionID string) map[string]any {
	return map[string]any{"id": definition.ID, "name": definition.Title, "description": definition.Description, "whenToUse": definition.WhenToUse, "revisionId": revisionID, "runnable": revisionID != ""}
}
