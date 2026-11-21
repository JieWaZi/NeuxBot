package workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"

	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

var workflowAuthoringOperations = []string{
	"workflow.authoring.prepare", "workflow.source.get", "workflow.source.validate", "workflow.source.save",
	"workflow.sdk.get", "workflow.resources.list", "workflow.annotations.list", "workflow.evidence.list",
}

var workflowRunOperations = []string{
	"workflow.definitions.list", "workflow.definitions.describe", "workflow.runs.start",
	"workflow.runs.get", "workflow.runs.cancel", "workflow.runs.wait",
}

// AgentBindings 只使用 Core 固定的对话、执行和业务身份。
func (s *Service) AgentBindings(scope workflowtool.Scope) []capability.Binding {
	if scope.Kind == constants.WorkflowCapabilityScopeResult {
		return s.agentBindings(scope, []string{"workflow.results.schema", "workflow.results.submit"})
	}
	return nil
}

func (s *Service) consumerAgentBindings(lease models.WorkflowConsumerLease) []capability.Binding {
	scope := workflowtool.Scope{Key: lease.ID, Kind: constants.WorkflowCapabilityScopeConsumer}
	ids := []string{"workflow.runs.get", "workflow.runs.wait", "workflow.runs.cancel"}
	if lease.Selection != nil {
		ids = append(ids, "workflow.definitions.list", "workflow.definitions.describe", "workflow.runs.start")
	} else {
		ids = append(ids, "workflow.runs.list", "workflow.runs.repeat")
	}
	return s.agentBindings(scope, ids)
}

// consumerReceiptOwner 保证不同聊天回合的相同命令 requestId 不能串用接纳回执。
func consumerReceiptOwner(lease models.WorkflowConsumerLease) string {
	return "workflow.consumer:" + lease.ChatID + ":" + lease.ID
}

func (s *Service) agentBindings(scope workflowtool.Scope, ids []string) []capability.Binding {
	bindings := make([]capability.Binding, 0, len(ids))
	for _, id := range ids {
		op, _ := capability.Find(id)
		binding := capability.Binding{Operation: id}
		binding.Call = func(ctx context.Context, request capability.Request) (capability.Result, error) {
			if scope.LocalChatID != "" && (id == "workflow.definitions.list" || id == "workflow.definitions.describe") {
				return s.readLocalWorkflowDefinitions(ctx, scope, request)
			}
			if op.Effect != constants.AgentCLIEffectRead {
				access, err := s.authorizeWorkflowTool(ctx, scope)
				if err != nil {
					return capability.Result{}, err
				}
				if id == "workflow.runs.start" {
					definitionID, _ := request.Params["definitionId"].(string)
					revisionID, _ := request.Params["revisionId"].(string)
					if scope.LocalChatID != "" {
						allowed, err := s.localWorkflowRevision(scope, definitionID, revisionID)
						if err != nil {
							return capability.Result{}, err
						}
						if revisionID != allowed {
							return capability.Result{}, status_error.WorkflowRevisionConflict
						}
					} else {
						if access.selection == nil || definitionID != access.selection.ID {
							return capability.Result{}, status_error.AgentOperationForbidden
						}
						if revisionID != access.selection.RevisionID {
							return capability.Result{}, status_error.WorkflowRevisionConflict
						}
					}
					return s.startAgentRun(ctx, scope, access.owner, request)
				}
				result, err := capability.Execute(ctx, s.commands, access.owner, request, func(ctx context.Context) error {
					input := workflowAgentInput(request)
					input.RequestID = capability.ID(access.owner, id, request.RequestID)
					_, err := access.call(ctx, input)
					return err
				})
				if err != nil {
					return result, err
				}
				return s.workflowAgentReceipt(scope, id, result), nil
			}
			if id == "workflow.runs.wait" {
				if s.events == nil {
					return capability.Result{}, status_error.AgentConnectionUnavailable
				}
				input := workflowAgentInput(request)
				return capability.Observe(ctx, request.Params, func() events.Subscription {
					return s.events.Subscribe("workflow", events.Resource{Kind: "workflow", ID: input.RunID})
				}, func(ctx context.Context) (any, bool, error) {
					value, err := s.handleTool(ctx, scope, input)
					if err != nil {
						return nil, false, err
					}
					details := unwrapWorkflow(value)
					run, _ := details["run"].(map[string]any)
					status, _ := run["status"].(constants.WorkflowRunStatus)
					return details, workflowstate.TerminalRun(status), nil
				})
			}
			input := workflowAgentInput(request)
			value, err := s.handleTool(ctx, scope, input)
			if err != nil {
				return capability.Result{}, err
			}
			value = unwrapWorkflow(value)
			if id == "workflow.definitions.describe" {
				items, _ := value["items"].([]map[string]any)
				for _, item := range items {
					if item["id"] == request.Params["definitionId"] {
						if revision, _ := request.Params["revisionId"].(string); revision != "" && item["revisionId"] != revision {
							return capability.Result{}, status_error.WorkflowRevisionConflict
						}
						return capability.Result{Data: item}, nil
					}
				}
				return capability.Result{}, status_error.WorkflowNotFound
			}
			if id == "workflow.definitions.list" {
				items, _ := value["items"].([]map[string]any)
				page, err := capability.Page(items, request.Params)
				return capability.Result{Data: page}, err
			}
			if id == "workflow.annotations.list" {
				encoded, _ := json.Marshal(value["items"])
				var items []any
				_ = json.Unmarshal(encoded, &items)
				page, err := capability.Page(items, request.Params)
				return capability.Result{Data: page}, err
			}
			return capability.Result{Data: value}, nil
		}
		if op.Effect != constants.AgentCLIEffectRead {
			binding.Receipt = func(ctx context.Context, requestID string) (capability.Result, error) {
				access, err := s.authorizeWorkflowTool(ctx, scope)
				if err != nil {
					return capability.Result{}, err
				}
				result, err := capability.Lookup(ctx, s.commands, access.owner, id, requestID)
				if err != nil {
					return result, err
				}
				return s.workflowAgentReceipt(scope, id, result), nil
			}
		}
		bindings = append(bindings, binding)
	}
	return bindings
}

func (s *Service) startAgentRun(ctx context.Context, scope workflowtool.Scope, owner string, request capability.Request) (capability.Result, error) {
	commandID := capability.ID(owner, request.Operation, request.RequestID)
	executionID := scope.Key
	if scope.LocalChatID != "" {
		executionID = scope.ExecutionID
	}
	prepared, staged, err := s.prepareAgentRunInput(ctx, request, consumerRunRequestID(executionID, commandID))
	if err != nil {
		return capability.Result{}, err
	}
	if staged != nil {
		defer staged.Close()
	}
	return capability.Execute(ctx, s.commands, owner, prepared, func(ctx context.Context) error {
		input := workflowAgentInput(prepared)
		input.StagedInputs = staged
		input.RequestID = commandID
		// 文件准备期间执行可能已经结束，接纳前核对当前执行范围。
		_, err := s.handleTool(ctx, scope, input)
		return err
	})
}

func (s *Service) prepareAgentRunInput(ctx context.Context, request capability.Request, runRequestID string) (_ capability.Request, _ *workflowfiles.StagedInputs, err error) {
	input := workflowAgentInput(request)
	if input.RevisionID == "" {
		return request, nil, status_error.AgentInputInvalid.WithField("field", "revisionId")
	}
	revision, err := s.store.GetRevision(ctx, input.WorkflowID, input.RevisionID)
	if err != nil {
		return request, nil, err
	}
	args := workflowdsl.ApplyDefaults(revision.Manifest.Meta.InputSchema, input.Args)
	if err := workflowdsl.ValidateJSON(revision.Manifest.Meta.InputSchema, args); err != nil {
		return request, nil, err
	}
	params := make(map[string]any, len(request.Params))
	for key, value := range request.Params {
		params[key] = value
	}
	request.Params = params
	request.InputDigests = make(map[string]string)
	roots, allowedFiles := capability.FileRoots(ctx), capability.FilePaths(ctx)
	managedSpace := capability.ManagedSpace(ctx)
	runID := workflowRunID(input.WorkflowID, runRequestID)
	var stage *workflowfiles.StagedInputs
	defer func() {
		if err != nil && stage != nil {
			stage.Close()
		}
	}()
	canonicalArgs, err := workflowdsl.MapFiles(revision.Manifest.Meta.InputSchema, args, func(pointer, reference string) (string, error) {
		if strings.HasPrefix(reference, constants.WorkflowFileReferencePrefix) {
			return "", status_error.WorkflowFileInvalid.WithField("field", pointer)
		}
		if !filepath.IsAbs(reference) {
			if !filepath.IsAbs(request.CWD) {
				return "", status_error.WorkflowFileInvalid
			}
			reference = filepath.Join(request.CWD, reference)
		}
		reference = filepath.Clean(reference)
		if stage == nil {
			workspace, err := workflowfiles.PrepareWorkspace(s.dataDirectory, input.WorkflowID, "")
			if err != nil {
				return "", err
			}
			stage, err = workflowfiles.StageInputs(workspace, runID)
			if err != nil {
				return "", err
			}
		}
		frozen, err := workflowfiles.FreezeLocalInput(ctx, roots, allowedFiles, reference, stage.Directory, runID, pointer, managedSpace)
		if err != nil {
			return "", err
		}
		if err := stage.Record(pointer, frozen); err != nil {
			return "", err
		}
		request.InputDigests[pointer] = frozen.Digest
		return reference, nil
	})
	if err != nil {
		return request, nil, err
	}
	request.Params["args"], request.Params["revisionId"] = canonicalArgs, revision.ID
	return request, stage, nil
}

func unwrapWorkflow(value map[string]any) map[string]any {
	if data, ok := value["data"].(map[string]any); ok {
		return data
	}
	return value
}

// workflowAgentReceipt 从有效保存回执恢复所选工作流的同回合版本授权。
func (s *Service) workflowAgentReceipt(scope workflowtool.Scope, operation string, result capability.Result) capability.Result {
	if operation != "workflow.source.save" {
		return result
	}
	data, ok := result.Data.(map[string]any)
	revisionID, _ := data["revisionId"].(string)
	digest, _ := data["sourceDigest"].(string)
	if !ok || revisionID == "" || digest == "" {
		return result
	}
	if scope.Selection != nil && data["definitionId"] == scope.Selection.ID {
		if scope.SavedRevisions != nil {
			scope.SavedRevisions.Store(revisionID, struct{}{})
		}
	}
	copy := make(map[string]any, len(data)+1)
	for key, value := range data {
		copy[key] = value
	}
	copy["saved"] = true
	result.Data = copy
	return result
}

// workflowAgentInput 只映射封闭业务参数；身份与 Token 永不来自 JSON。
func workflowAgentInput(request capability.Request) workflowtool.Input {
	encoded, _ := json.Marshal(request.Params)
	var input workflowtool.Input
	_ = json.Unmarshal(encoded, &input)
	input.RequestID = request.RequestID
	switch request.Operation {
	case "workflow.authoring.prepare":
		input.Action, input.Type = constants.WorkflowActionQuery, constants.WorkflowQueryAuthoring
	case "workflow.source.get":
		input.Action = constants.WorkflowActionSourceGet
	case "workflow.source.validate":
		input.Action = constants.WorkflowActionSourceValidate
	case "workflow.source.save":
		input.Action = constants.WorkflowActionSourceUpsert
		if input.SDK == "" {
			input.SDK = constants.WorkflowSDKIdentifier
		}
	case "workflow.sdk.get":
		input.Action, input.Type = constants.WorkflowActionQuery, constants.WorkflowQuerySDK
		if input.Kind == "example" {
			input.Type = constants.WorkflowQueryExamples
		}
	case "workflow.resources.list":
		input.Action, input.Type = constants.WorkflowActionQuery, constants.WorkflowQueryResources
	case "workflow.annotations.list":
		input.Action = constants.WorkflowActionAnnotationsList
	case "workflow.definitions.list", "workflow.definitions.describe":
		input.Action, input.Type = constants.WorkflowActionQuery, constants.WorkflowQueryWorkflows
	case "workflow.runs.start":
		input.Action = constants.WorkflowActionRunStart
		input.WorkflowID, _ = request.Params["definitionId"].(string)
	case "workflow.runs.list":
		input.Action = constants.WorkflowActionRunList
		input.PageParams = request.Params
	case "workflow.runs.repeat":
		input.Action = constants.WorkflowActionRunRepeat
	case "workflow.runs.get", "workflow.runs.wait":
		input.Action = constants.WorkflowActionRunGet
	case "workflow.runs.cancel":
		input.Action = constants.WorkflowActionRunCancel
	case "workflow.evidence.list":
		input.Action = constants.WorkflowActionRunsGet
	case "workflow.results.schema":
		input.Action, input.Type = constants.WorkflowActionQuery, constants.WorkflowQueryResultSchema
	case "workflow.results.submit":
		input.Action = constants.WorkflowActionResultSubmit
	}
	return input
}
