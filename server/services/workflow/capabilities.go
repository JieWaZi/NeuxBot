package workflow

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"

	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowcatalog "github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func (s *Service) handleCreationTool(ctx context.Context, session models.WorkflowCreationSession, input workflowtool.Input) (map[string]any, error) {
	if input.SDK != "" && input.SDK != constants.WorkflowSDKIdentifier {
		return nil, status_error.WorkflowCapabilityUnsupported.WithField("capability", "sdk_identity")
	}
	switch input.Action {
	case constants.WorkflowActionQuery:
		switch input.Type {
		case constants.WorkflowQueryAuthoring:
			return s.prepareAuthoring(ctx, session, input)
		case constants.WorkflowQuerySDK:
			content, err := workflowdsl.SDKTopic(input.Topic)
			return map[string]any{"sdk": constants.WorkflowSDKIdentifier, "topic": input.Topic, "content": content}, err
		case constants.WorkflowQueryExamples:
			source, err := workflowdsl.SDKExample(input.Topic)
			return map[string]any{"source": source, "guidance": agent.WorkflowExampleGuidance}, err
		case constants.WorkflowQueryResources:
			return s.resourcePage(ctx, session.DefinitionID, input.Kind, input.Cursor)
		}
		return nil, status_error.WorkflowCapabilityUnsupported
	case constants.WorkflowActionSourceGet:
		return s.readWorkflowAuthoringSource(ctx, session.DefinitionID, input.RevisionID)
	case constants.WorkflowActionSourceValidate:
		compiled, diagnostics, err := CompileSource(input.Source)
		if err == nil {
			var configurationDiagnostics []workflowdsl.Diagnostic
			configurationDiagnostics, err = validateWorkerRetryPolicies(compiled.Manifest, &session.Defaults)
			diagnostics = append(diagnostics, configurationDiagnostics...)
		}
		result := map[string]any{"valid": err == nil, "diagnostics": diagnostics}
		if err != nil {
			result["error"] = err.Error()
			result["errorCode"] = workflowstate.ErrorKey(err)
		}
		if err == nil {
			result["summary"] = map[string]any{
				"title": compiled.Manifest.Meta.Title, "nodes": len(compiled.Manifest.Nodes),
				"phases": len(compiled.Manifest.Meta.Phases), "sourceDigest": compiled.SourceDigest,
			}
			if input.IncludeManifest {
				result["manifest"] = workflowdsl.ExportCanvas(compiled.Manifest)
			}
		}
		return result, nil
	case constants.WorkflowActionSourceUpsert:
		if input.WorkflowID != "" && input.WorkflowID != session.DefinitionID {
			return nil, status_error.WorkflowStateConflict
		}
		patches := make([]SourcePatch, len(input.Patches))
		for i, p := range input.Patches {
			patches[i] = SourcePatch{Search: p.Search, Replace: p.Replace}
		}
		receipt, diagnostics, err := s.SaveSource(ctx, SaveSourceCommand{Patches: patches, BaseSourceDigest: input.BaseSourceDigest, DefinitionID: session.DefinitionID, RequestID: input.RequestID, ExpectedRevisionID: input.ExpectedRevisionID, Source: input.Source, SDK: input.SDK, CreationSessionID: session.ID, ExecutionID: session.ActiveExecutionID, ExecutionChatID: session.ChatID, ResolvedAnnotationIDs: input.ResolvedAnnotationIDs})
		if err != nil {
			if diagnostics == nil {
				diagnostics = []workflowdsl.Diagnostic{}
			}
			return nil, capability.RejectWithDetails(err, "Source was not saved; repair the diagnostics or resolve the revision conflict.", map[string]any{
				"diagnostics": diagnostics,
			})
		}
		encoded, _ := json.Marshal(receipt)
		result := map[string]any{}
		_ = json.Unmarshal(encoded, &result)
		result["diagnostics"] = diagnostics
		return result, nil
	case constants.WorkflowActionAnnotationsList:
		if input.Status != "" && input.Status != "pending" && input.Status != "resolved" {
			return nil, status_error.BadRequest
		}
		items, err := s.creationSessions.ListFilteredAnnotations(ctx, session.DefinitionID, input.Status, input.BaseRevisionID)
		return map[string]any{"items": items}, err
	case constants.WorkflowActionRunsGet:
		run, err := s.runs.GetRun(ctx, input.RunID)
		if err != nil {
			return nil, err
		}
		if run.DefinitionID != session.DefinitionID {
			return nil, status_error.WorkflowNotFound
		}
		offset, err := pageOffset(input.Cursor)
		if err != nil {
			return nil, err
		}
		instances, total, err := s.runs.ListInstances(ctx, run.ID, 200, offset)
		if err != nil {
			return nil, err
		}
		humans, err := s.runs.ListHumanRequests(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		operations, err := s.runs.ListOperations(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		calls := []models.WorkflowActionCall{}
		for _, instance := range instances {
			if instance.EffectID == "" {
				continue
			}
			items, err := s.runs.ListActionCalls(ctx, run.ID, instance.EffectID)
			if err != nil {
				return nil, err
			}
			calls = append(calls, items...)
		}
		publicRun := map[string]any{"rootRunId": run.RootRunID, "id": run.ID, "definitionId": run.DefinitionID, "revisionId": run.RevisionID, "revisionOrdinal": run.RevisionOrdinal, "status": run.Status, "args": run.Args, "defaults": run.Defaults, "limits": run.Limits, "scopeBinding": run.ScopeBinding, "parentRunId": run.ParentRunID, "joined": run.Joined, "depth": run.Depth, "result": run.Result, "version": run.Version, "startedAt": run.StartedAt, "deadlineAt": run.DeadlineAt, "createdAt": run.CreatedAt, "updatedAt": run.UpdatedAt, "triggerSource": run.Context["triggerSource"]}
		if run.Failure != nil {
			publicRun["failure"] = run.Failure
		}
		if run.CompletedAt != nil {
			publicRun["completedAt"] = run.CompletedAt
		}
		return map[string]any{"run": publicRun, "instances": instances, "actionCalls": calls, "humanRequests": humans, "operations": operations, "totalInstances": total, "nextCursor": nextOffset(offset, len(instances), int(total))}, nil
	default:
		return nil, status_error.WorkflowCapabilityUnsupported
	}
}

// readWorkflowAuthoringSource 读取已授权工作流的源码与工作区。
func (s *Service) readWorkflowAuthoringSource(ctx context.Context, definitionID, revisionID string) (map[string]any, error) {
	definition, err := s.store.GetDefinition(ctx, definitionID)
	if err != nil {
		return nil, err
	}
	source := ""
	digest := ""
	if revisionID == "" {
		revisionID = definition.CurrentRevisionID
	}
	if revisionID != "" {
		revision, err := s.store.GetRevision(ctx, definition.ID, revisionID)
		if err != nil {
			return nil, err
		}
		source = revision.Source
		digest = revision.SourceDigest
	}
	workspace, err := workflowfiles.PrepareWorkspace(s.dataDirectory, definition.ID, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"workflowId": definition.ID, "revisionId": revisionID, "currentRevisionId": definition.CurrentRevisionID, "source": source, "sourceDigest": digest, "sdk": constants.WorkflowSDKIdentifier, "workspace": workspace}, nil
}

// prepareAuthoring 在现有创作租约内组合资料，不创建工作、不修改源码或扩大资源权限。
func (s *Service) prepareAuthoring(
	ctx context.Context,
	session models.WorkflowCreationSession,
	input workflowtool.Input,
) (map[string]any, error) {
	for _, primitive := range input.Primitives {
		if !slices.Contains(workflowcatalog.Primitives(), primitive) {
			return nil, status_error.AgentInputInvalid
		}
	}
	for _, feature := range input.Features {
		if !slices.Contains(workflowcatalog.Features(), feature) {
			return nil, status_error.AgentInputInvalid
		}
	}
	topics := workflowcatalog.AuthoringTopics(input.Primitives, input.Features)
	sdk, err := workflowdsl.SDKBundle(topics)
	if err != nil {
		return nil, err
	}
	guides := []string{"workflow.authoring"}
	resourceKinds := []string{}
	if slices.Contains(input.Primitives, "worker") {
		resourceKinds = append(resourceKinds, "wakers", "runtimes", "accounts")
	}
	needsActions := slices.Contains(input.Primitives, "action") || slices.Contains(input.Features, "im")
	if needsActions {
		resourceKinds = append(resourceKinds, "builtinActions")
	}
	if input.Language == "python" {
		resourceKinds = append(resourceKinds, "functionLanguages")
	}
	if input.Language == "python" && slices.Contains(input.Primitives, "action") {
		resourceKinds = append(resourceKinds, "scriptLanguages")
	}
	for _, primitive := range input.Primitives {
		if workflowcatalog.PrimitiveTopics()[primitive] == "workflow" {
			resourceKinds = append(resourceKinds, "workflows")
			break
		}
	}
	if slices.Contains(input.Features, "im") {
		resourceKinds = append(resourceKinds, "imTargets")
	}
	if slices.Contains(input.Features, "files") && slices.Contains(input.Primitives, "worker") {
		guides = append(guides, "workflow.output-delivery")
	}
	if slices.Contains(input.Features, "api-retrieval") {
		guides = append(guides, "workflow.api-retrieval")
	}
	var content strings.Builder
	for _, id := range guides {
		guide, err := builtin.Guide(id)
		if err != nil {
			return nil, err
		}
		content.WriteString(guide + "\n\n")
	}
	content.WriteString(sdk)
	source, err := s.readWorkflowAuthoringSource(ctx, session.DefinitionID, "")
	if err != nil {
		return nil, err
	}
	annotations, err := s.creationSessions.ListFilteredAnnotations(ctx, session.DefinitionID, "pending", "")
	if err != nil {
		return nil, err
	}
	annotationPage, err := capability.Page(annotations, map[string]any{})
	if err != nil {
		return nil, err
	}
	resources := map[string]any{}
	resolvePython := sync.OnceValues(func() (string, error) { return s.resolvePython(ctx) })
	for _, kind := range resourceKinds {
		page, err := s.resourceItemsPage(ctx, session.DefinitionID, kind, "", resolvePython)
		if err != nil {
			return nil, err
		}
		resources[kind] = page
	}
	operations := []capability.Operation{}
	for _, id := range []string{"workflow.source.save", "workflow.source.validate"} {
		op, _ := capability.Find(id)
		operations = append(operations, op)
	}
	result := map[string]any{
		"workflow": source, "annotations": annotationPage, "topics": topics, "guideIds": guides,
		"content": content.String(), "resources": resources, "operations": operations,
	}
	if slices.Contains(input.Primitives, "worker") {
		policies := map[string]any{}
		for _, safety := range []string{"pure", "idempotent"} {
			policy := workerRetryPolicy(safety, &session.Defaults)
			capability := map[string]any{"supported": policy == 0}
			if policy != 0 {
				capability["errorCode"] = policy.Key()
			}
			policies[safety] = capability
		}
		result["execution"] = map[string]any{"defaults": session.Defaults, "workerRetry": policies}
	}
	return result, nil
}

func (s *Service) persistConsumer(ctx context.Context, lease models.WorkflowConsumerLease) (models.WorkflowConsumerLease, error) {
	var err error
	if lease.ThreadID == "" {
		err = s.consumers.CreateConsumer(ctx, lease)
	} else {
		err = s.transactions.Transaction(ctx, func(tx context.Context) error {
			if err := s.ensureTeamWorkflowWritable(tx, lease.ThreadID); err != nil {
				return err
			}
			return s.consumers.CreateConsumer(tx, lease)
		})
	}
	if err != nil {
		return models.WorkflowConsumerLease{}, err
	}
	current, err := s.consumers.GetConsumer(ctx, lease.ID)
	if err != nil {
		return models.WorkflowConsumerLease{}, err
	}
	if current.PayloadDigest != lease.PayloadDigest || current.ChatID != lease.ChatID || current.StoppedAt != nil {
		return models.WorkflowConsumerLease{}, status_error.WorkflowRequestConflict
	}
	return current, nil
}

func (s *Service) handleConsumerTool(ctx context.Context, lease models.WorkflowConsumerLease, actual models.Execution, chat models.Chat, input workflowtool.Input) (map[string]any, error) {
	wrap := func(value map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true, "data": value}, nil
	}
	switch input.Action {
	case constants.WorkflowActionQuery:
		items := []map[string]any{}
		if lease.Selection != nil {
			item, err := s.describeWorkflowDefinition(ctx, lease.Selection.ID, lease.Selection.RevisionID)
			if err != nil {
				return nil, err
			}
			if matchesWorkflowDefinition(item, input.Search) {
				items = append(items, item)
			}
		}
		return wrap(map[string]any{"items": items})
	case constants.WorkflowActionRunList:
		if lease.Selection != nil {
			return nil, status_error.AgentOperationForbidden
		}
		page, err := capability.PageFrom(input.PageParams, func(offset, limit int) ([]any, int, error) {
			return s.listChatRunPage(ctx, lease.ChatID, offset, limit)
		})
		if err != nil {
			return nil, err
		}
		return wrap(page)
	case constants.WorkflowActionRunStart:
		return s.startConsumerRun(ctx, lease, actual, chat, input, false)
	}

	run, err := s.visibleConsumerRun(ctx, lease, input.RunID)
	if err != nil {
		return nil, err
	}
	return s.handleConsumerRun(ctx, lease, run, actual, chat, input, false)
}

func (s *Service) listChatRunPage(ctx context.Context, chatID string, offset, limit int) ([]any, int, error) {
	var runs []models.WorkflowRun
	var total int64
	err := s.transactions.Transaction(ctx, func(tx context.Context) error {
		var err error
		runs, total, err = s.runs.ListVisibleChatRunSources(tx, chatID, offset, limit)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	items := make([]any, 0, len(runs))
	for _, run := range runs {
		items = append(items, map[string]any{"runId": run.ID, "definitionId": run.DefinitionID, "revisionId": run.RevisionID,
			"name": run.SourceName, "status": run.Status, "createdAt": run.CreatedAt})
	}
	return items, int(total), nil
}

func (s *Service) startConsumerRun(ctx context.Context, lease models.WorkflowConsumerLease, actual models.Execution, chat models.Chat, input workflowtool.Input, local bool) (map[string]any, error) {
	if lease.Selection == nil || lease.Selection.ID != input.WorkflowID || input.RequestID == "" || len(input.RequestID) > 128 {
		return nil, status_error.WorkflowStateConflict
	}
	if input.RevisionID != lease.Selection.RevisionID {
		return nil, status_error.WorkflowRevisionConflict
	}
	approved, rootsFromChat, err := chatRunScope(chat)
	if err != nil {
		return nil, err
	}
	requestID := consumerRunRequestID(lease.ID, input.RequestID)
	command := StartRunCommand{
		AutomaticRunID: actual.AutomaticRunID, stagedInputs: input.StagedInputs, DefinitionID: input.WorkflowID,
		RevisionID: input.RevisionID, RequestID: requestID, Args: input.Args, TriggerSource: "agent",
		ConsumerLeaseID: lease.ID, ApprovedScope: &approved, additionalWorkingDirectories: rootsFromChat,
		Defaults: workflowExecutionDefaults(actual), ChatSource: consumerChatSource(lease, actual, chat, lease.Selection.Name, ""),
	}
	if local {
		command.localConsumer = &lease
	}
	run, err := s.StartRun(ctx, command)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "data": consumerRunDetails(run)}, nil
}

func consumerRunRequestID(executionID, requestID string) string {
	return requestidempotency.Identity{ID: requestID}.ResourceID("workflow-use-run", executionID)
}

func (s *Service) handleConsumerRun(ctx context.Context, lease models.WorkflowConsumerLease, run models.WorkflowRun, actual models.Execution, chat models.Chat, input workflowtool.Input, local bool) (map[string]any, error) {
	wrap := func(value map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true, "data": value}, nil
	}
	switch input.Action {
	case constants.WorkflowActionRunRepeat:
		if lease.Selection != nil {
			return nil, status_error.AgentOperationForbidden
		}
		run, err := s.repeatChatRun(ctx, lease, actual, chat, run, input.RequestID, local)
		if err != nil {
			return nil, err
		}
		return wrap(consumerRunDetails(run))
	case constants.WorkflowActionRunCancel:
		if input.RequestID == "" || len(input.RequestID) > 128 {
			return nil, status_error.BadRequest
		}
		if run.SourceWorkbench || run.SourceExecutionID == "" || (!local && run.SourceExecutionID != lease.ID) {
			return nil, status_error.AgentOperationForbidden
		}
		if err := s.CancelRun(ctx, run.ID); err != nil {
			return nil, err
		}
		run, err := s.runs.GetRun(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		return wrap(consumerRunDetails(run))
	case constants.WorkflowActionRunGet:
		humans, err := s.runs.ListHumanRequests(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		operations, err := s.runs.ListOperations(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		files, err := s.OutputFiles(ctx, run)
		if err != nil {
			return nil, err
		}
		details := map[string]any{"run": consumerRunDetails(run), "humanRequests": humans, "operationRequests": operations, "outputFiles": files}
		if run.Status == constants.WorkflowRunStatusCancelled || run.Status == constants.WorkflowRunStatusFailed {
			details["partialResults"] = deliveredChatPartialResults(run)
		}
		return wrap(details)
	default:
		return nil, status_error.WorkflowCapabilityUnsupported
	}
}

func (s *Service) visibleConsumerRun(ctx context.Context, lease models.WorkflowConsumerLease, runID string) (models.WorkflowRun, error) {
	run, err := s.runs.GetRun(ctx, runID)
	if err != nil {
		return run, err
	}
	if run.SourceChatID == lease.ChatID && run.HistoryDeletedAt == nil && (lease.Selection == nil || run.SourceExecutionID == lease.ID && !run.SourceWorkbench) {
		return run, nil
	}
	return models.WorkflowRun{}, status_error.WorkflowNotFound
}

func consumerRunDetails(run models.WorkflowRun) map[string]any {
	return map[string]any{
		"rootRunId": run.RootRunID, "id": run.ID, "definitionId": run.DefinitionID, "revisionId": run.RevisionID,
		"revisionOrdinal": run.RevisionOrdinal, "status": run.Status, "args": run.Args, "defaults": run.Defaults,
		"limits": run.Limits, "scopeBinding": run.ScopeBinding, "parentRunId": run.ParentRunID, "joined": run.Joined,
		"depth": run.Depth, "result": run.Result, "version": run.Version, "startedAt": run.StartedAt,
		"deadlineAt": run.DeadlineAt, "createdAt": run.CreatedAt, "updatedAt": run.UpdatedAt,
		"triggerSource": run.Context["triggerSource"], "failure": run.Failure, "completedAt": run.CompletedAt,
	}
}

// boundedResourceItems 仅加载请求的设备配置或内置能力目录。
func (s *Service) boundedResourceItems(ctx context.Context, definitionID, kind string, resolvePython func() (string, error)) ([]map[string]any, error) {
	switch kind {
	case "builtinActions":
		return []map[string]any{{
			"id": constants.IMBuiltinConnectorID, "name": "IM delivery", "toolName": constants.IMBuiltinToolName,
			"description": "The workflow runner delivers messages and files through this built-in action. No MCP connector is required.",
		}}, nil
	case "functionLanguages", "scriptLanguages":
		pythonPath, pythonErr := resolvePython()
		python := map[string]any{"language": "python", "available": pythonErr == nil, "executable": pythonPath}
		if kind == "functionLanguages" {
			python["executionModes"] = []string{"trusted_local"}
			return []map[string]any{{"language": "javascript", "available": true, "executionModes": []string{"pure"}}, python}, nil
		}
		python["protocols"] = []string{"json", "text"}
		return []map[string]any{python}, nil
	case "connectors":
		definition, err := s.store.GetDefinition(ctx, definitionID)
		if err != nil {
			return nil, err
		}
		if s.connectors == nil {
			return nil, nil
		}
		connectors, err := s.connectors.ListEnabled(ctx, definition.CreationWakerID)
		if err != nil {
			return nil, err
		}
		items := []map[string]any{}
		for _, connector := range connectors {
			refs := []map[string]any{}
			for _, secret := range connectorSecrets(connector) {
				if secret.Configured {
					refs = append(refs, map[string]any{"ref": "connector:" + connector.ID + ":secret:" + secret.ID, "name": secret.Name})
				}
			}
			items = append(items, map[string]any{"id": connector.ID, "name": connector.Name, "tools": connector.TestTools, "secretRefs": refs})
		}
		return items, nil
	case "runtimes", "accounts", "imTargets":
		if s.resources == nil {
			return nil, nil
		}
		return s.resources(ctx, kind)
	default:
		return nil, status_error.BadRequest
	}
}

// workflowToolPolicyCapabilities 只声明执行层实际支持并强制实施的策略。
func workflowToolPolicyCapabilities() map[string]any {
	return map[string]any{"connectors": []string{"inherit"}, "nativeTools": []string{"inherit"}}
}

func pageOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(cursor)
	if err != nil || value < 0 || value > 1000000 {
		return 0, status_error.BadRequest
	}
	return value, nil
}
func nextOffset(offset, length, total int) string {
	if offset+length < total {
		return strconv.Itoa(offset + length)
	}
	return ""
}

// workflowResourceCursor 固定同一时间排序中的分页位置。
type workflowResourceCursor struct {
	// UpdatedAt 是上一页末项的更新时间。
	UpdatedAt int64 `json:"updatedAt"`
	// ID 在相同毫秒内提供稳定排序。
	ID string `json:"id"`
}

func (s *Service) resourcePage(ctx context.Context, definitionID, kind, cursor string) (map[string]any, error) {
	var page map[string]any
	var err error
	if kind == "toolPolicyCapabilities" {
		if cursor != "" {
			return nil, status_error.BadRequest
		}
		if _, err = s.store.GetDefinition(ctx, definitionID); err != nil {
			return nil, err
		}
		page = map[string]any{"kind": kind, "items": []any{workflowToolPolicyCapabilities()}, "nextCursor": ""}
	} else {
		page, err = s.resourceItemsPage(ctx, definitionID, kind, cursor, func() (string, error) { return s.resolvePython(ctx) })
		if err != nil {
			return nil, err
		}
	}
	page["toolPolicyCapabilities"] = workflowToolPolicyCapabilities()
	return page, nil
}

func (s *Service) resourceItemsPage(ctx context.Context, definitionID, kind, cursor string, resolvePython func() (string, error)) (map[string]any, error) {
	if !slices.Contains(workflowcatalog.ResourceKinds(), kind) {
		return nil, status_error.BadRequest
	}
	const limit = 50
	items := []any{}
	next := ""
	if kind == "workflows" {
		position := workflowResourceCursor{}
		if cursor != "" {
			encoded, err := base64.RawURLEncoding.DecodeString(cursor)
			if err != nil || json.Unmarshal(encoded, &position) != nil || position.UpdatedAt <= 0 || position.ID == "" {
				return nil, status_error.BadRequest
			}
		}
		values, err := s.store.ListDefinitions(ctx, "", position.UpdatedAt, position.ID, limit+1, 0, nil)
		if err != nil {
			return nil, err
		}
		if len(values) > limit {
			last := values[limit-1]
			encoded, _ := json.Marshal(workflowResourceCursor{UpdatedAt: last.UpdatedAt, ID: last.ID})
			next = base64.RawURLEncoding.EncodeToString(encoded)
			values = values[:limit]
		}
		for _, value := range values {
			items = append(items, map[string]any{"id": value.ID, "title": value.Title, "description": value.Description, "currentRevisionId": value.CurrentRevisionID, "runnable": value.CurrentRevisionID != ""})
		}
		return map[string]any{"kind": kind, "items": items, "nextCursor": next}, nil
	}
	offset, err := pageOffset(cursor)
	if err != nil {
		return nil, err
	}
	if kind == "wakers" {
		if s.wakers == nil {
			return map[string]any{"kind": kind, "items": items, "nextCursor": next}, nil
		}
		values, err := s.wakers.List(ctx, "", limit+1, offset)
		if err != nil {
			return nil, err
		}
		if len(values) > limit {
			next = strconv.Itoa(offset + limit)
			values = values[:limit]
		}
		for _, value := range values {
			item := map[string]any{"id": value.ID, "name": value.Name}
			if s.connectors != nil {
				bound, err := s.connectors.ListEnabled(ctx, value.ID)
				if err != nil {
					return nil, err
				}
				connectors := []map[string]any{}
				for _, connector := range bound {
					connectors = append(connectors, map[string]any{"id": connector.ID, "name": connector.Name, "tools": connector.TestTools})
				}
				item["connectors"] = connectors
			}
			items = append(items, item)
		}
		return map[string]any{"kind": kind, "items": items, "nextCursor": next}, nil
	}
	if kind == "projects" {
		definition, err := s.store.GetDefinition(ctx, definitionID)
		if err != nil {
			return nil, err
		}
		if s.projects == nil {
			return map[string]any{"kind": kind, "items": items, "nextCursor": next}, nil
		}
		values, err := s.projects.ListByWaker(ctx, definition.CreationWakerID, "", limit+1, offset)
		if err != nil {
			return nil, err
		}
		if len(values) > limit {
			next = strconv.Itoa(offset + limit)
			values = values[:limit]
		}
		for _, value := range values {
			items = append(items, map[string]any{"id": value.ID, "name": value.Name})
		}
		return map[string]any{"kind": kind, "items": items, "nextCursor": next}, nil
	}
	// 设备配置目录和已启用连接器是有界配置集合，不暴露启动环境与凭据。
	values, err := s.boundedResourceItems(ctx, definitionID, kind, resolvePython)
	if err != nil {
		return nil, err
	}
	values = slices.Clone(values)
	sort.SliceStable(values, func(i, j int) bool {
		a, _ := values[i]["id"].(string)
		b, _ := values[j]["id"].(string)
		return a < b
	})
	if offset < len(values) {
		end := min(offset+limit, len(values))
		next = nextOffset(offset, end-offset, len(values))
		for _, value := range values[offset:end] {
			items = append(items, value)
		}
	}
	return map[string]any{"kind": kind, "items": items, "nextCursor": next}, nil
}

// workflowToolAccess 保存已核验的领域范围，供实际操作和回执共同使用。
type workflowToolAccess struct {
	// owner 是耐久命令回执的业务归属。
	owner string
	// selection 固定使用者显式选定的版本。
	selection *workinput.WorkflowReference
	// call 在本次核验的领域事实下分派实际操作。
	call func(context.Context, workflowtool.Input) (map[string]any, error)
}

func (s *Service) authorizeWorkflowTool(ctx context.Context, scope workflowtool.Scope) (workflowToolAccess, error) {
	if scope.LocalChatID != "" {
		return s.authorizeLocalWorkflowTool(ctx, scope)
	}
	switch scope.Kind {
	case constants.WorkflowCapabilityScopeConsumer:
		if s.consumers == nil {
			return workflowToolAccess{}, status_error.WorkflowCapabilityUnsupported
		}
		lease, err := s.consumers.GetConsumer(ctx, scope.Key)
		if err != nil {
			return workflowToolAccess{}, err
		}
		if lease.StoppedAt != nil {
			return workflowToolAccess{}, status_error.WorkflowStateConflict
		}
		actual, chat, err := s.readWorkflowExecution(ctx, lease.ChatID, lease.ID)
		if err != nil {
			return workflowToolAccess{}, err
		}
		return workflowToolAccess{
			owner:     consumerReceiptOwner(lease),
			selection: lease.Selection,
			call: func(ctx context.Context, input workflowtool.Input) (map[string]any, error) {
				return s.handleConsumerTool(ctx, lease, actual, chat, input)
			},
		}, nil
	case constants.WorkflowCapabilityScopeResult:
		effect, err := s.runs.GetEffect(ctx, scope.Key)
		if err != nil {
			return workflowToolAccess{}, err
		}
		digest := workflowdsl.Digest([]byte(scope.Token))
		if subtle.ConstantTimeCompare([]byte(digest), []byte(effect.TokenDigest)) != 1 {
			return workflowToolAccess{}, status_error.WorkflowStateConflict
		}
		run, err := s.runs.GetRun(ctx, effect.RunID)
		if err != nil {
			return workflowToolAccess{}, err
		}
		schema := workerResultSchema(run, effect)
		if len(schema) == 0 {
			return workflowToolAccess{}, status_error.WorkflowStateConflict
		}
		return workflowToolAccess{
			owner: "workflow.result:" + effect.ID,
			call: func(ctx context.Context, input workflowtool.Input) (map[string]any, error) {
				return s.handleResultTool(ctx, effect, schema, input)
			},
		}, nil
	default:
		return workflowToolAccess{}, status_error.WorkflowCapabilityUnsupported
	}
}

func (s *Service) handleTool(ctx context.Context, scope workflowtool.Scope, input workflowtool.Input) (map[string]any, error) {
	if scope.LocalChatID == "" && !workflowtool.Allows(scope.Kind, input) {
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	access, err := s.authorizeWorkflowTool(ctx, scope)
	if err != nil {
		return nil, err
	}
	return access.call(ctx, input)
}

func workerResultSchema(run models.WorkflowRun, effect models.WorkflowEffect) map[string]any {
	if effect.Boundary.Primitive != "worker" || run.DeadlineAt <= time.Now().UnixMilli() || workflowstate.TerminalRun(run.Status) || run.Status == constants.WorkflowRunStatusCancelling || workflowstate.StepTerminal(effect.Status) || effect.Status == constants.WorkflowStepStatusUnknown || effect.SkipRequested {
		return nil
	}
	schema, _ := workflowstate.BoundaryOptions(effect.Boundary)["schema"].(map[string]any)
	return schema
}

func (s *Service) handleResultTool(ctx context.Context, effect models.WorkflowEffect, schema map[string]any, input workflowtool.Input) (map[string]any, error) {
	if input.Action == constants.WorkflowActionQuery {
		return map[string]any{"schema": schema, "nodeId": effect.Boundary.NodeID, "guidance": agent.WorkflowResultSchemaGuidance}, nil
	}
	encoded, err := json.Marshal(input.Data)
	if err != nil || len(encoded) > constants.WorkflowOutputLimitBytes {
		return nil, status_error.WorkflowBudgetExceeded
	}
	err = s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if current.AttemptGeneration != effect.AttemptGeneration || current.ExecutionID != effect.ExecutionID {
			return status_error.WorkflowStateConflict
		}
		schema := workerResultSchema(state.Run, *current)
		if len(schema) == 0 {
			return status_error.WorkflowStateConflict
		}
		if err := workflowdsl.ValidateJSON(schema, input.Data); err != nil {
			return err
		}
		if current.Candidate != nil {
			existing, _ := current.Candidate.JSON()
			if workflowstate.Digest(existing) != workflowstate.Digest(input.Data) {
				return status_error.WorkflowRequestConflict
			}
			return capability.Commit(ctx, map[string]any{"submitted": true, "executionId": current.ExecutionID, "attemptGeneration": current.AttemptGeneration})
		}
		value := workflowdsl.JSONValue(input.Data)
		current.Candidate = &value
		return capability.Commit(ctx, map[string]any{"submitted": true, "executionId": current.ExecutionID, "attemptGeneration": current.AttemptGeneration})
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"submitted": true, "executionId": effect.ExecutionID, "attemptGeneration": effect.AttemptGeneration}, nil
}

// validateCreationExecution 让创作授权跟随当前执行；等待用户回答不会消耗独立期限。
// 源码保存事务复用此校验，阻止编译期间已结束或被替换的执行迟到写入。
func (s *Service) validateCreationExecution(ctx context.Context, session models.WorkflowCreationSession, executionID string) error {
	if executionID == "" || session.ActiveExecutionID != executionID {
		return status_error.WorkflowAuthoringSuperseded
	}
	chatID := session.ChatID
	if chatID == "" {
		chatID = requestidempotency.Identity{ID: session.ID}.ResourceID("chat", session.WakerID)
	}
	if s.chats == nil {
		return status_error.WorkflowCapabilityUnsupported
	}
	actual, err := s.chats.ReadExecution(ctx, chatID, session.ActiveExecutionID)
	if err != nil {
		return err
	}
	if workflowstate.ExecutionTerminal(actual.Status) || actual.CompletedAt != nil {
		return status_error.WorkflowAuthoringFinished
	}
	summary, err := s.chats.GetChatSummary(ctx, chatID)
	if err != nil {
		return err
	}
	if summary.Chat.ActiveExecutionID != session.ActiveExecutionID {
		return status_error.WorkflowAuthoringSuperseded
	}
	return nil
}
