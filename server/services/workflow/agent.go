package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
)

// AgentBindings 只签发当前服务端场景具有的操作，不接受调用方声明场景或身份。
func (s *Service) AgentBindings(key, token string, kind constants.WorkflowCapabilityScope) []agentcapability.Binding {
	scope := workflowtool.Scope{Key: key, Token: token, Kind: kind}
	ids := []string{}
	switch kind {
	case constants.WorkflowCapabilityScopeCreation:
		ids = []string{"workflow.authoring.prepare", "workflow.source.get", "workflow.source.validate", "workflow.source.save", "workflow.sdk.get", "workflow.resources.list", "workflow.annotations.list", "workflow.evidence.list"}
	case constants.WorkflowCapabilityScopeConsumer:
		ids = []string{"workflow.definitions.list", "workflow.definitions.describe", "workflow.runs.start", "workflow.runs.get", "workflow.runs.cancel", "workflow.runs.wait"}
	case constants.WorkflowCapabilityScopeResult:
		ids = []string{"workflow.results.schema", "workflow.results.submit"}
	}
	bindings := make([]agentcapability.Binding, 0, len(ids))
	for _, id := range ids {
		binding := agentcapability.Binding{Operation: id}
		binding.Call = func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			owner, err := s.agentOwner(ctx, scope)
			if err != nil {
				return agentcapability.Result{}, err
			}
			if kind == constants.WorkflowCapabilityScopeConsumer && id == "workflow.runs.start" {
				lease, err := s.consumers.GetConsumer(ctx, key)
				if err != nil {
					return agentcapability.Result{}, err
				}
				definitionID, _ := request.Params["definitionId"].(string)
				if !slices.Contains(lease.DefinitionIDs, definitionID) {
					return agentcapability.Result{}, status_error.AgentOperationForbidden
				}
			}
			op, _ := contract.Find(id)
			if op.Effect != constants.AgentCLIEffectRead {
				if id == "workflow.runs.start" {
					return s.startAgentRun(ctx, scope, owner, request)
				}
				result, err := agentcommand.Execute(ctx, s.commands, owner, request, func(ctx context.Context) error {
					input := workflowAgentInput(request)
					input.RequestID = agentcommand.ID(owner, id, request.RequestID)
					_, err := s.handleTool(ctx, scope, input)
					return err
				})
				return workflowAgentReceipt(id, result), err
			}
			if id == "workflow.runs.wait" {
				if s.events == nil {
					return agentcapability.Result{}, status_error.AgentConnectionUnavailable
				}
				input := workflowAgentInput(request)
				return agentcapability.Observe(ctx, request.Params, func() events.Subscription {
					return s.events.Subscribe("workflow", events.Resource{Kind: "workflow", ID: input.RunID})
				}, func(ctx context.Context) (any, bool, error) {
					value, err := s.handleConsumerTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionRunGet, RunID: input.RunID})
					if err != nil {
						return nil, false, err
					}
					run, err := s.runs.GetRun(ctx, input.RunID)
					return unwrapWorkflow(value), workflowstate.TerminalRun(run.Status), err
				})
			}
			input := workflowAgentInput(request)
			value, err := s.handleTool(ctx, scope, input)
			if err != nil {
				return agentcapability.Result{}, err
			}
			value = unwrapWorkflow(value)
			if id == "workflow.definitions.describe" {
				items, _ := value["items"].([]map[string]any)
				for _, item := range items {
					if item["id"] == request.Params["definitionId"] {
						return agentcapability.Result{Data: item}, nil
					}
				}
				return agentcapability.Result{}, status_error.WorkflowNotFound
			}
			if id == "workflow.definitions.list" {
				items, _ := value["items"].([]map[string]any)
				page, err := agentcapability.Page(items, request.Params)
				return agentcapability.Result{Data: page}, err
			}
			if id == "workflow.annotations.list" {
				encoded, _ := json.Marshal(value["items"])
				var items []any
				_ = json.Unmarshal(encoded, &items)
				page, err := agentcapability.Page(items, request.Params)
				return agentcapability.Result{Data: page}, err
			}
			return agentcapability.Result{Data: value}, nil
		}
		op, _ := contract.Find(id)
		if op.Effect != constants.AgentCLIEffectRead {
			binding.Receipt = func(ctx context.Context, requestID string) (agentcapability.Result, error) {
				owner, err := s.agentOwner(ctx, scope)
				if err != nil {
					return agentcapability.Result{}, err
				}
				result, err := agentcommand.Lookup(ctx, s.commands, owner, id, requestID)
				if err == nil && kind == constants.WorkflowCapabilityScopeConsumer {
					data, _ := result.Data.(map[string]any)
					if definition, ok := data["definitionId"].(string); ok {
						lease, getErr := s.consumers.GetConsumer(ctx, key)
						if getErr != nil {
							return agentcapability.Result{}, getErr
						}
						if !slices.Contains(lease.DefinitionIDs, definition) {
							return agentcapability.Result{}, status_error.AgentOperationForbidden
						}
					}
				}
				return workflowAgentReceipt(id, result), err
			}
		}
		bindings = append(bindings, binding)
	}
	return bindings
}

func (s *Service) startAgentRun(ctx context.Context, scope workflowtool.Scope, owner string, request agentcapability.Request) (agentcapability.Result, error) {
	for attempt := 0; ; attempt++ {
		prepared, err := s.prepareAgentRunInput(ctx, owner, request)
		if err != nil {
			return agentcapability.Result{}, err
		}
		defer prepared.close()
		result, callErr := agentcommand.Execute(ctx, s.commands, owner, prepared.request, func(ctx context.Context) error {
			input := workflowAgentInput(prepared.request)
			input.Args, input.InputFileRoots = prepared.args, prepared.roots
			input.RequestID = agentcommand.ID(owner, request.Operation, request.RequestID)
			_, err := s.handleTool(ctx, scope, input)
			return err
		})
		revisionID, _ := request.Params["revisionId"].(string)
		if !errors.Is(callErr, status_error.AgentCommandRequestConflict) || revisionID != "" || attempt != 0 {
			return result, callErr
		}
		// 并发首提可能先按另一个 Head 准备；只按已经胜出的原回执重核一次。
		accepted, err := agentcommand.Lookup(ctx, s.commands, owner, request.Operation, request.RequestID)
		data, _ := accepted.Data.(map[string]any)
		if err != nil || data["revisionId"] == prepared.request.Params["revisionId"] {
			return result, callErr
		}
	}
}

// agentRunInput 持有单次接纳前的冻结原件，不让临时路径进入幂等摘要。
type agentRunInput struct {
	// request 保留规范路径参数及同一次复制得到的文件摘要。
	request agentcapability.Request
	// args 只向领域接纳传递受管冻结路径。
	args any
	// roots 只授权领域读取本次临时原件。
	roots []string
	// directory 在调用结束或重放后整体释放。
	directory string
}

func (p *agentRunInput) close() {
	if p.directory != "" {
		if err := os.RemoveAll(p.directory); err != nil {
			logx.Errorf("remove frozen agent inputs failed: error=%v", err)
		}
	}
}

func (s *Service) prepareAgentRunInput(ctx context.Context, owner string, request agentcapability.Request) (_ *agentRunInput, err error) {
	input := workflowAgentInput(request)
	if input.RevisionID == "" {
		accepted, err := agentcommand.Lookup(ctx, s.commands, owner, request.Operation, request.RequestID)
		if err == nil {
			data, _ := accepted.Data.(map[string]any)
			if data["definitionId"] != input.WorkflowID {
				return nil, status_error.AgentCommandRequestConflict
			}
			input.RevisionID, _ = data["revisionId"].(string)
			if input.RevisionID == "" {
				return nil, status_error.AgentCommandOutcomeUnknown
			}
		} else if !errors.Is(err, status_error.AgentReceiptNotFound) {
			return nil, err
		} else {
			definition, err := s.store.GetDefinition(ctx, input.WorkflowID)
			if err != nil {
				return nil, err
			}
			input.RevisionID = definition.CurrentRevisionID
		}
	}
	revision, err := s.store.GetRevision(ctx, input.WorkflowID, input.RevisionID)
	if err != nil {
		return nil, err
	}
	args := workflowdsl.ApplyDefaults(revision.Manifest.Meta.InputSchema, input.Args)
	if err := workflowdsl.ValidateJSON(revision.Manifest.Meta.InputSchema, args); err != nil {
		return nil, err
	}
	prepared := &agentRunInput{request: request}
	defer func() {
		if err != nil {
			prepared.close()
		}
	}()
	prepared.request.Params = make(map[string]any, len(request.Params))
	for key, value := range request.Params {
		prepared.request.Params[key] = value
	}
	prepared.request.InputDigests = map[string]string{}
	frozenPaths := map[string]string{}
	canonicalArgs, err := workflowdsl.MapFiles(revision.Manifest.Meta.InputSchema, args, func(pointer, reference string) (string, error) {
		if strings.HasPrefix(reference, constants.WorkflowFileReferencePrefix) {
			return reference, nil
		}
		if !filepath.IsAbs(reference) {
			if !filepath.IsAbs(request.CWD) {
				return "", status_error.WorkflowFileInvalid
			}
			reference = filepath.Join(request.CWD, reference)
		}
		reference = filepath.Clean(reference)
		if prepared.directory == "" {
			directory, err := os.MkdirTemp(s.dataDirectory, "workflow-input-staging-")
			if err != nil {
				logx.Errorf("prepare frozen agent inputs failed: error=%v", err)
				return "", status_error.WorkflowFileStorageError
			}
			prepared.directory = directory
			actual, err := filepath.EvalSymlinks(directory)
			if err != nil {
				logx.Errorf("resolve frozen agent inputs failed: error=%v", err)
				return "", status_error.WorkflowFileStorageError
			}
			prepared.directory, prepared.roots = actual, []string{actual}
		}
		frozen, err := workflowfiles.FreezeLocalInput(ctx, agentcapability.FileRoots(ctx), reference, prepared.directory, "input", pointer)
		if err != nil {
			return "", err
		}
		prepared.request.InputDigests[pointer], frozenPaths[pointer] = frozen.Digest, frozen.Path
		return reference, nil
	})
	if err != nil {
		return nil, err
	}
	prepared.request.Params["args"], prepared.request.Params["revisionId"] = canonicalArgs, revision.ID
	prepared.args, err = workflowdsl.MapFiles(revision.Manifest.Meta.InputSchema, canonicalArgs, func(pointer, reference string) (string, error) {
		if path, ok := frozenPaths[pointer]; ok {
			return path, nil
		}
		return reference, nil
	})
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func (s *Service) agentOwner(ctx context.Context, scope workflowtool.Scope) (string, error) {
	if _, err := s.handleTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionQuery, Type: constants.WorkflowQueryCapabilities}); err != nil {
		return "", err
	}
	switch scope.Kind {
	case constants.WorkflowCapabilityScopeCreation:
		session, err := s.creationSessions.GetCreationSession(ctx, scope.Key)
		return "workflow.authoring:" + session.DefinitionID + ":" + session.ChatID, err
	case constants.WorkflowCapabilityScopeConsumer:
		lease, err := s.consumers.GetConsumer(ctx, scope.Key)
		return "workflow.consumer:" + lease.ChatID, err
	case constants.WorkflowCapabilityScopeResult:
		return "workflow.result:" + scope.Key, nil
	}
	return "", status_error.AgentOperationForbidden
}

func unwrapWorkflow(value map[string]any) map[string]any {
	if data, ok := value["data"].(map[string]any); ok {
		return data
	}
	return value
}

// workflowAgentReceipt 只从已持久化的保存回执派生完成标志，回放与查询保持同一语义。
func workflowAgentReceipt(operation string, result agentcapability.Result) agentcapability.Result {
	if operation != "workflow.source.save" {
		return result
	}
	data, ok := result.Data.(map[string]any)
	revisionID, _ := data["revisionId"].(string)
	digest, _ := data["sourceDigest"].(string)
	if !ok || revisionID == "" || digest == "" {
		return result
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
func workflowAgentInput(request agentcapability.Request) workflowtool.Input {
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
		if input.SDKVersion == "" {
			input.SDKVersion = constants.WorkflowSDKVersion
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
