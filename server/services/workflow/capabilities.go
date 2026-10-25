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
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowtool "github.com/JieWaZi/NeuxBot/server/internal/workflow/capability"
	workflowcatalog "github.com/JieWaZi/NeuxBot/server/internal/workflow/catalog"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func (s *Service) handleCreationTool(ctx context.Context, scope workflowtool.Scope, input workflowtool.Input) (map[string]any, error) {
	if s.creationSessions == nil {
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	session, err := s.creationSessions.GetCreationSession(ctx, scope.Key)
	if err != nil {
		return nil, err
	}
	if err := s.validateCreationExecution(ctx, session, scope.Token); err != nil {
		return nil, err
	}
	if input.SDKVersion != "" && input.SDKVersion != constants.WorkflowSDKVersion {
		return nil, status_error.WorkflowCapabilityUnsupported.WithField("capability", "sdk_version")
	}
	switch input.Action {
	case constants.WorkflowActionQuery:
		switch input.Type {
		case constants.WorkflowQueryCapabilities:
			return workflowtool.Capabilities(scope.Kind), nil
		case constants.WorkflowQueryAuthoring:
			return s.prepareAuthoring(ctx, scope, session, input)
		case constants.WorkflowQuerySDK:
			content, err := workflowdsl.SDKTopic(input.Topic)
			return map[string]any{"version": constants.WorkflowSDKVersion, "topic": input.Topic, "content": content}, err
		case constants.WorkflowQueryExamples:
			source, err := workflowdsl.SDKExample(input.Topic)
			return map[string]any{"source": source, "guidance": "Replace all example resource IDs with actual query resources results; validate complete source before saving."}, err
		case constants.WorkflowQueryResources:
			return s.resourcePage(ctx, session.DefinitionID, input.Kind, input.Cursor)
		}
		return nil, status_error.WorkflowCapabilityUnsupported
	case constants.WorkflowActionSourceGet:
		definition, err := s.store.GetDefinition(ctx, session.DefinitionID)
		if err != nil {
			return nil, err
		}
		source := ""
		digest := ""
		revisionID := input.RevisionID
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
		return map[string]any{"workflowId": definition.ID, "revisionId": revisionID, "currentRevisionId": definition.CurrentRevisionID, "source": source, "sourceDigest": digest, "sdkVersion": constants.WorkflowSDKVersion}, nil
	case constants.WorkflowActionSourceValidate:
		compiled, diagnostics, err := workflowdsl.CompileForAuthoring(input.Source)
		if err == nil {
			err = s.validateSourcePolicies(ctx, compiled.Manifest)
		}
		result := map[string]any{"valid": err == nil, "diagnostics": diagnostics}
		if err != nil {
			result["error"] = err.Error()
		}
		if err == nil {
			result["summary"] = map[string]any{
				"title": compiled.Manifest.Meta.Title, "nodes": len(compiled.Manifest.Nodes),
				"phases": len(compiled.Manifest.Meta.Phases), "sourceDigest": compiled.SourceDigest,
			}
			if input.IncludeManifest {
				result["manifest"] = compiled.Manifest
			}
		}
		return result, nil
	case constants.WorkflowActionSourceUpsert:
		receipt, diagnostics, err := s.SaveSource(ctx, SaveSourceCommand{DefinitionID: session.DefinitionID, RequestID: input.RequestID, ExpectedRevisionID: input.ExpectedRevisionID, Source: input.Source, SDKVersion: input.SDKVersion, CreationSessionID: session.ID, CreationExecutionID: session.ActiveExecutionID, CreationToken: scope.Token, ResolvedAnnotationIDs: input.ResolvedAnnotationIDs})
		if err != nil {
			if diagnostics == nil {
				diagnostics = []workflowdsl.Diagnostic{}
			}
			return nil, agentcapability.RejectWithDetails(err, "Source was not saved; repair the diagnostics or resolve the revision conflict.", map[string]any{
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
		return map[string]any{"run": run, "instances": instances, "actionCalls": calls, "humanRequests": humans, "operations": operations, "totalInstances": total, "nextCursor": nextOffset(offset, len(instances), int(total))}, nil
	default:
		return nil, status_error.WorkflowCapabilityUnsupported
	}
}

// prepareAuthoring 在现有创作租约内组合资料，不创建工作、不修改源码或扩大资源权限。
func (s *Service) prepareAuthoring(
	ctx context.Context,
	scope workflowtool.Scope,
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
	if slices.Contains(input.Primitives, "action") || slices.Contains(input.Features, "im") {
		resourceKinds = append(resourceKinds, "builtinActions")
	}
	if input.Language == "python" {
		resourceKinds = append(resourceKinds, "functionLanguages")
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
	source, err := s.handleCreationTool(ctx, scope, workflowtool.Input{Action: constants.WorkflowActionSourceGet})
	if err != nil {
		return nil, err
	}
	annotations, err := s.creationSessions.ListFilteredAnnotations(ctx, session.DefinitionID, "pending", "")
	if err != nil {
		return nil, err
	}
	annotationPage, err := agentcapability.Page(annotations, map[string]any{})
	if err != nil {
		return nil, err
	}
	resources := map[string]any{}
	for _, kind := range resourceKinds {
		page, err := s.resourceItemsPage(ctx, session.DefinitionID, kind, "")
		if err != nil {
			return nil, err
		}
		resources[kind] = page
	}
	operations := []contract.Operation{}
	for _, id := range []string{"workflow.source.save", "workflow.source.validate"} {
		op, _ := contract.Find(id)
		operations = append(operations, op)
	}
	return map[string]any{
		"workflow": source, "annotations": annotationPage, "topics": topics, "guideIds": guides,
		"content": content.String(), "resources": resources, "operations": operations,
	}, nil
}

// BindConsumer 签发用户选择的单回合能力，并只向中立 Core 绑定受限 CLI 能力。
func (s *Service) BindConsumer(ctx context.Context, intent execution.WorkIntent, definitionIDs []string) (execution.WorkIntent, error) {
	if len(definitionIDs) == 0 {
		return intent, nil
	}
	if len(definitionIDs) > 100 || s.consumers == nil || s.state.AgentBindings == nil || s.chats == nil || s.executions == nil {
		return intent, status_error.WorkflowCapabilityUnsupported
	}
	seen := map[string]bool{}
	for _, id := range definitionIDs {
		if id == "" || len(id) > 32 || seen[id] {
			return intent, status_error.BadRequest
		}
		seen[id] = true
		definition, err := s.store.GetDefinition(ctx, id)
		if err != nil {
			return intent, err
		}
		if definition.CurrentRevisionID == "" {
			return intent, status_error.WorkflowStateConflict.WithField("workflowId", id)
		}
	}
	if err := s.executions.ValidateConfiguration(ctx, execution.ExecutionConfiguration{RuntimeID: intent.Options.RuntimeID, AccountID: intent.Options.AccountID, Model: intent.Options.Model, Reasoning: intent.Options.Reasoning, PermissionLevel: intent.Options.PermissionLevel}); err != nil {
		return intent, err
	}
	chatID := intent.Chat.ChatID
	if intent.Chat.Create != nil {
		if intent.ClientRequestID == "" {
			return intent, status_error.BadRequest
		}
		identity := requestidempotency.Identity{ID: intent.ClientRequestID}
		chatID = identity.ResourceID("chat", intent.Chat.Create.WakerID)
		if intent.ExecutionID == "" {
			intent.ExecutionID = identity.ResourceID("workflow-use-execution", intent.Chat.Create.WakerID)
		}
	}
	if chatID == "" {
		return intent, status_error.BadRequest
	}
	if intent.ExecutionID == "" {
		intent.ExecutionID = util.New()
	}
	ids := slices.Clone(definitionIDs)
	slices.Sort(ids)
	// 尚未由 Core 补齐的可选枚举不能直接 MarshalText；摘要显式编码入口的原始选择。
	options := intent.Options
	fingerprint, err := requestidempotency.New(intent.ExecutionID, []any{chatID, intent.ClientRequestID, intent.Chat.Create, intent.Input, options.RuntimeID, options.AccountID, options.Model, options.Reasoning, string(options.PermissionLevel), options.WorkspaceReadOnly, string(options.SessionPolicy), options.HistoryTurns, string(options.HistoryMode), options.SummaryModel, options.SummaryReasoning, ids})
	if err != nil {
		return intent, err
	}
	lease := models.WorkflowConsumerLease{ID: intent.ExecutionID, ChatID: chatID, DefinitionIDs: ids, PayloadDigest: fingerprint.Hash, AuthToken: util.New() + util.New(), DeadlineAt: time.Now().UnixMilli() + constants.WorkflowDefaultTimeoutMillis}
	// 插入只保留首次事实；同一客户端首发重试不能改变白名单或凭据。
	if err := s.consumers.CreateConsumer(ctx, lease); err != nil {
		return intent, err
	}
	current, err := s.consumers.GetConsumer(ctx, lease.ID)
	if err != nil {
		return intent, err
	}
	if current.PayloadDigest != lease.PayloadDigest || current.ChatID != chatID {
		return intent, status_error.WorkflowRequestConflict
	}
	intent.Options.ToolCapabilities = append(intent.Options.ToolCapabilities, execution.ToolCapability{
		ID: "workflow.consumer", Name: "工作流使用", Bindings: s.AgentBindings(current.ID, current.AuthToken, constants.WorkflowCapabilityScopeConsumer),
	})
	return intent, nil
}

func (s *Service) handleConsumerTool(ctx context.Context, scope workflowtool.Scope, input workflowtool.Input) (map[string]any, error) {
	if s.consumers == nil || s.chats == nil {
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	lease, err := s.consumers.GetConsumer(ctx, scope.Key)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(lease.AuthToken), []byte(scope.Token)) != 1 || lease.DeadlineAt <= time.Now().UnixMilli() {
		return nil, status_error.WorkflowStateConflict
	}
	actual, err := s.chats.ReadExecution(ctx, lease.ChatID, lease.ID)
	if err != nil {
		return nil, err
	}
	if actual.CompletedAt != nil || actual.Status != constants.ExecutionStatusPending && actual.Status != constants.ExecutionStatusRunning {
		return nil, status_error.WorkflowStateConflict
	}
	summary, err := s.chats.GetChatSummary(ctx, lease.ChatID)
	if err != nil {
		return nil, err
	}
	if summary.Chat.ActiveExecutionID != lease.ID {
		return nil, status_error.WorkflowStateConflict
	}
	if input.Action == constants.WorkflowActionQuery && input.Type == constants.WorkflowQueryCapabilities {
		return workflowtool.Capabilities(scope.Kind), nil
	}
	if input.Action == constants.WorkflowActionQuery && input.Type == constants.WorkflowQueryWorkflows {
		items := []map[string]any{}
		query := strings.ToLower(strings.TrimSpace(input.Search))
		for _, id := range lease.DefinitionIDs {
			definition, err := s.store.GetDefinition(ctx, id)
			if err != nil {
				return nil, err
			}
			if query != "" && !strings.Contains(strings.ToLower(definition.Title+" "+definition.Description+" "+definition.WhenToUse), query) {
				continue
			}
			item := map[string]any{"id": id, "name": definition.Title, "description": definition.Description, "whenToUse": definition.WhenToUse, "revisionId": definition.CurrentRevisionID, "runnable": definition.CurrentRevisionID != "", "outputSchema": map[string]any{}}
			if definition.CurrentRevisionID != "" {
				revision, err := s.store.GetRevision(ctx, id, definition.CurrentRevisionID)
				if err != nil {
					return nil, err
				}
				item["inputSchema"] = revision.Manifest.Meta.InputSchema
				item["outputSchema"] = revision.Manifest.Meta.OutputSchema
			}
			items = append(items, item)
		}
		return map[string]any{"ok": true, "data": map[string]any{"items": items}}, nil
	}
	if input.Action == constants.WorkflowActionRunStart {
		if !slices.Contains(lease.DefinitionIDs, input.WorkflowID) || input.RequestID == "" || len(input.RequestID) > 128 {
			return nil, status_error.WorkflowStateConflict
		}
		if len(summary.Chat.AdditionalWorkingDirectories) == 0 {
			return nil, status_error.WorkflowResourceInvalid.WithField("field", "workingDirectory")
		}
		approved := models.WorkflowScopeBinding{ProjectID: summary.Chat.ProjectID, WorkingDirectory: summary.Chat.AdditionalWorkingDirectories[0]}
		requestID := requestidempotency.Identity{ID: input.RequestID}.ResourceID("workflow-use-run", lease.ID)
		roots := agentcapability.FileRoots(ctx)
		if input.InputFileRoots != nil {
			roots = input.InputFileRoots
		}
		run, err := s.StartRun(ctx, StartRunCommand{AutomaticRunID: actual.AutomaticRunID, InputFileRoots: roots, DefinitionID: input.WorkflowID, RevisionID: input.RevisionID, RequestID: requestID, Args: input.Args, TriggerSource: "agent", ConsumerLeaseID: lease.ID, ApprovedScope: &approved, Defaults: models.WorkflowExecutionDefaults{RuntimeID: actual.RuntimeID, AccountID: actual.AccountID, Model: actual.Model, Reasoning: actual.Reasoning, PermissionLevel: actual.PermissionLevel, WorkspaceReadOnly: actual.WorkspaceReadOnly}})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "data": run}, nil
	}
	if input.Action != constants.WorkflowActionRunGet && input.Action != constants.WorkflowActionRunCancel {
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	run, err := s.runs.GetRun(ctx, input.RunID)
	if err != nil {
		return nil, err
	}
	originID, _ := run.Context["consumerLeaseId"].(string)
	origin, originErr := s.consumers.GetConsumer(ctx, originID)
	if originErr != nil || origin.ChatID != lease.ChatID || !slices.Contains(lease.DefinitionIDs, run.DefinitionID) {
		return nil, status_error.WorkflowNotFound
	}
	if input.Action == constants.WorkflowActionRunCancel {
		if input.RequestID == "" || len(input.RequestID) > 128 {
			return nil, status_error.BadRequest
		}
		if err := s.CancelRun(ctx, run.ID); err != nil {
			return nil, err
		}
		run, err = s.runs.GetRun(ctx, run.ID)
		return map[string]any{"ok": true, "data": run}, err
	}
	humans, err := s.runs.ListHumanRequests(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	operations, err := s.runs.ListOperations(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "data": map[string]any{"run": run, "humanRequests": humans, "operationRequests": operations}}, nil
}

// Resources 返回创作可引用的真实目录，凭据仅输出可选择的引用身份。
func (s *Service) resourcesCatalog(ctx context.Context, definitionID string) (map[string]any, error) {
	definition, err := s.store.GetDefinition(ctx, definitionID)
	if err != nil {
		return nil, err
	}
	resources := map[string]any{"builtinActions": []map[string]any{{"id": constants.IMBuiltinConnectorID, "name": "IM 交付", "toolName": constants.IMBuiltinToolName, "description": "工作流内置交付操作，经 Runner 持久效果边界直接调用，不需要安装 MCP 连接器。"}}, "sdkVersion": constants.WorkflowSDKVersion, "toolPolicyCapabilities": workflowToolPolicyCapabilities(), "functionLanguages": []map[string]any{{"language": "javascript", "available": true, "executionModes": []string{"pure"}}, {"language": "python", "available": s.pythonExecutable != "", "executionModes": []string{"trusted_local"}}}}
	if s.wakers != nil {
		wakers, err := s.wakers.List(ctx, "", 100, 0)
		if err != nil {
			return nil, err
		}
		items := []map[string]any{}
		for _, waker := range wakers {
			item := map[string]any{"id": waker.ID, "name": waker.Name}
			if s.connectors != nil {
				bound, err := s.connectors.ListEnabled(ctx, waker.ID)
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
		resources["wakers"] = items
	}
	workflows, err := s.store.ListDefinitions(ctx, "", 0, "", 100)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, target := range workflows {
		items = append(items, map[string]any{"id": target.ID, "title": target.Title, "currentRevisionId": target.CurrentRevisionID, "runnable": target.CurrentRevisionID != ""})
	}
	resources["workflows"] = items
	if s.projects != nil {
		projects, err := s.projects.ListByWaker(ctx, definition.CreationWakerID, "", 100, 0)
		if err != nil {
			return nil, err
		}
		items := []map[string]any{}
		for _, project := range projects {
			items = append(items, map[string]any{"id": project.ID, "name": project.Name})
		}
		resources["projects"] = items
	}
	if s.connectors != nil {
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
		resources["connectors"] = items
	}
	if s.resources != nil {
		additional, err := s.resources(ctx)
		if err != nil {
			return nil, err
		}
		for key, value := range additional {
			resources[key] = value
		}
	}
	return resources, nil
}

// workflowToolPolicyCapabilities 只声明执行层实际支持并强制实施的策略。
func workflowToolPolicyCapabilities() map[string]any {
	return map[string]any{"connectors": []string{"inherit", "allowlist"}, "nativeTools": []string{"inherit"}}
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
		page, err = s.resourceItemsPage(ctx, definitionID, kind, cursor)
		if err != nil {
			return nil, err
		}
	}
	page["toolPolicyCapabilities"] = workflowToolPolicyCapabilities()
	return page, nil
}

func (s *Service) resourceItemsPage(ctx context.Context, definitionID, kind, cursor string) (map[string]any, error) {
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
		values, err := s.store.ListDefinitions(ctx, "", position.UpdatedAt, position.ID, limit+1)
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
	if kind == "wakers" && s.wakers != nil {
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
	if kind == "projects" && s.projects != nil {
		definition, err := s.store.GetDefinition(ctx, definitionID)
		if err != nil {
			return nil, err
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
	resources, err := s.resourcesCatalog(ctx, definitionID)
	if err != nil {
		return nil, err
	}
	if value, found := resources[kind]; found {
		encoded, err := json.Marshal(value)
		if err != nil || json.Unmarshal(encoded, &items) != nil {
			return nil, status_error.WorkflowStateInvalid
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, _ := items[i].(map[string]any)
		right, _ := items[j].(map[string]any)
		a, _ := left["id"].(string)
		b, _ := right["id"].(string)
		return a < b
	})
	if offset >= len(items) {
		items = []any{}
	} else {
		end := offset + limit
		if end > len(items) {
			end = len(items)
		}
		next = nextOffset(offset, end-offset, len(items))
		items = items[offset:end]
	}
	return map[string]any{"kind": kind, "items": items, "nextCursor": next}, nil
}

func (s *Service) handleTool(ctx context.Context, scope workflowtool.Scope, input workflowtool.Input) (map[string]any, error) {
	if !workflowtool.Allows(scope.Kind, input) {
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	switch scope.Kind {
	case constants.WorkflowCapabilityScopeConsumer:
		return s.handleConsumerTool(ctx, scope, input)
	case constants.WorkflowCapabilityScopeCreation:
		return s.handleCreationTool(ctx, scope, input)
	case constants.WorkflowCapabilityScopeResult:
	default:
		return nil, status_error.WorkflowCapabilityUnsupported
	}
	effect, err := s.runs.GetEffect(ctx, scope.Key)
	if err != nil {
		return nil, err
	}
	digest := workflowdsl.Digest([]byte(scope.Token))
	if subtle.ConstantTimeCompare([]byte(digest), []byte(effect.TokenDigest)) != 1 {
		return nil, status_error.WorkflowStateConflict
	}
	if input.Action == constants.WorkflowActionQuery {
		run, err := s.runs.GetRun(ctx, effect.RunID)
		if err != nil {
			return nil, err
		}
		if effect.Boundary.Primitive != "worker" || run.DeadlineAt <= time.Now().UnixMilli() || workflowstate.TerminalRun(run.Status) || run.Status == constants.WorkflowRunStatusCancelling || workflowstate.StepTerminal(effect.Status) || effect.Status == constants.WorkflowStepStatusUnknown || effect.SkipRequested {
			return nil, status_error.WorkflowStateConflict
		}
		schema, _ := workflowstate.BoundaryOptions(effect.Boundary)["schema"].(map[string]any)
		if len(schema) == 0 {
			return nil, status_error.WorkflowStateConflict
		}
		if input.Type == constants.WorkflowQueryCapabilities {
			return workflowtool.Capabilities(scope.Kind), nil
		}
		return map[string]any{"schema": schema, "nodeId": effect.Boundary.NodeID, "guidance": "Complete actual work, then call allycli workflow results submit with data matching this schema. A final reply is not a structured result. Submission remains a candidate until this execution succeeds."}, nil
	}
	encoded, err := json.Marshal(input.Data)
	if err != nil || len(encoded) > constants.WorkflowOutputLimitBytes {
		return nil, status_error.WorkflowBudgetExceeded
	}
	err = s.state.MutateEffect(ctx, effect.RunID, effect.ID, func(ctx context.Context, state *controllers.WorkflowRunState, current *models.WorkflowEffect) error {
		if current.AttemptGeneration != effect.AttemptGeneration || current.ExecutionID != effect.ExecutionID || current.Boundary.Primitive != "worker" {
			return status_error.WorkflowStateConflict
		}
		if state.Run.DeadlineAt <= time.Now().UnixMilli() || workflowstate.TerminalRun(state.Run.Status) || state.Run.Status == constants.WorkflowRunStatusCancelling || workflowstate.StepTerminal(current.Status) || current.Status == constants.WorkflowStepStatusUnknown || current.SkipRequested {
			return status_error.WorkflowStateConflict
		}
		schema, _ := workflowstate.BoundaryOptions(current.Boundary)["schema"].(map[string]any)
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
			return agentcommand.Commit(ctx, map[string]any{"submitted": true, "executionId": current.ExecutionID, "attemptGeneration": current.AttemptGeneration})
		}
		value := workflowdsl.JSONValue(input.Data)
		current.Candidate = &value
		return agentcommand.Commit(ctx, map[string]any{"submitted": true, "executionId": current.ExecutionID, "attemptGeneration": current.AttemptGeneration})
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"submitted": true, "executionId": effect.ExecutionID, "attemptGeneration": effect.AttemptGeneration}, nil
}

// validateCreationExecution 让创作授权跟随当前执行；等待用户回答不会消耗独立期限。
// 源码保存事务复用此校验，阻止编译期间已结束或被替换的执行迟到写入。
func (s *Service) validateCreationExecution(ctx context.Context, session models.WorkflowCreationSession, token string) error {
	if session.ActiveExecutionID == "" || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(session.AuthToken)) != 1 {
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
