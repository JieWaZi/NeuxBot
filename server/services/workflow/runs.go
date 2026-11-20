package workflow

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// StartRunCommand 是用户、子运行与自动任务共用的接纳命令。
type StartRunCommand struct {
	// localConsumer 随本地伙伴的首次运行接纳一同提交使用权限。
	localConsumer *models.WorkflowConsumerLease
	// ChatSource 是服务端确认的聊天来源，运行与该关系一同提交。
	ChatSource *ChatRunSource `json:"-"`
	// ReplaySourceRunID 让完整重跑沿用已经冻结的原输入文件，但不复用节点结果。
	ReplaySourceRunID string `json:"-"`
	// ExpectedSourceVersion 是重新执行时核对的来源运行版本。
	ExpectedSourceVersion int64 `json:"-"`
	// AutomaticRunID 是服务端传入的原自动运行，不能从用户或模型正文绑定。
	AutomaticRunID string `json:"-"`
	// InputFileOwnerKeyID 由调用入口填写，原件同时绑定任务和集成主体。
	InputFileOwnerKeyID string `json:"-"`
	// stagedInputs 是已核验 CLI 文件范围并复制的候选原件。
	stagedInputs *workflowfiles.StagedInputs
	// WorkerConfigurations 仅由重新运行入口携带原节点配置，外部输入不能自行填写。
	WorkerConfigurations map[string]models.WorkflowExecutionDefaults `json:"-"`
	// InputFileTaskID 是自动任务入口绑定的上传归属，外部请求不能自行填写。
	InputFileTaskID string `json:"-"`
	// ConsumerLeaseID 是服务端绑定的单回合使用者身份，不接纳模型自填。
	ConsumerLeaseID string `json:"-"`
	// ApprovedScope 是业务入口已经验证的工作范围，HTTP 和模型不能自行扩大。
	ApprovedScope *models.WorkflowScopeBinding `json:"-"`
	// additionalWorkingDirectories 是来源聊天批准的其他根目录。
	additionalWorkingDirectories []string
	// TriggerSource 是服务端明确写入的接纳来源。
	TriggerSource string `json:"-"`
	// RetrySourceRunID 关联用户明确重新运行的源运行。
	RetrySourceRunID string `json:"-"`
	// DefinitionID 是运行目标。
	DefinitionID string `json:"-"`
	// RequestID 是同一触发发生的稳定身份。
	RequestID string `json:"requestId"`
	// RevisionID 是可选固定版本，空值冻结接纳时当前版。
	RevisionID string `json:"revisionId"`
	// Args 是用户提交的原始 JSON。
	Args any `json:"args"`
	// Defaults 是用户选择的智能体配置。
	Defaults models.WorkflowExecutionDefaults `json:"defaults"`
	// ParentRunID 是服务端填写的来源运行。
	ParentRunID string `json:"-"`
	// AwaitChildResult 表示创建该子运行的 workflow 节点会直接读取结果。
	AwaitChildResult bool `json:"-"`
	// Joined 表示跟随父运行取消。
	Joined bool `json:"-"`
	// Depth 是服务端填写的调用深度。
	Depth int `json:"-"`
}

// StartRun 在派发任何能力前原子固定版本、输入、根帧和接纳回执。
func (s *Service) StartRun(ctx context.Context, command StartRunCommand) (models.WorkflowRun, error) {
	if s.runs == nil {
		return models.WorkflowRun{}, status_error.WorkflowCapabilityUnsupported
	}
	if command.RequestID == "" || len(command.RequestID) > 128 || command.Depth > constants.WorkflowMaxChildDepth ||
		(command.RetrySourceRunID != "" && command.ReplaySourceRunID != "") || (command.ParentRunID != "" && command.ChatSource != nil) {
		return models.WorkflowRun{}, status_error.BadRequest
	}
	if origin := command.ChatSource; origin != nil && !origin.Workbench && !s.chatTurns.beginStart(origin.ChatID, origin.ExecutionID) {
		return models.WorkflowRun{}, status_error.WorkflowStateConflict
	}
	encodedArgs, err := json.Marshal(command.Args)
	if err != nil || len(encodedArgs) > constants.WorkflowOutputLimitBytes {
		return models.WorkflowRun{}, status_error.WorkflowBudgetExceeded.WithField("budget", "input_bytes")
	}
	payload := []any{command, command.DefinitionID, command.TriggerSource, command.RetrySourceRunID, command.ParentRunID, command.Joined, command.Depth, command.ConsumerLeaseID, command.ApprovedScope, command.InputFileTaskID, command.InputFileOwnerKeyID, command.AutomaticRunID, command.WorkerConfigurations}
	if command.ChatSource != nil || command.ReplaySourceRunID != "" {
		payload = append(payload, command.ChatSource, command.ReplaySourceRunID)
	}
	if len(command.additionalWorkingDirectories) != 0 {
		payload = append(payload, command.additionalWorkingDirectories)
	}
	if command.stagedInputs != nil {
		digests := make(map[string]string, len(command.stagedInputs.Files))
		for pointer, input := range command.stagedInputs.Files {
			digests[pointer] = input.Digest
		}
		payload = append(payload, digests)
	}
	digest := workflowstate.Digest(payload)
	// 幂等重放不重新访问已经冻结的目录或账号。
	if receipt, found, err := s.store.FindReceipt(ctx, command.DefinitionID, command.RequestID); err != nil {
		return models.WorkflowRun{}, err
	} else if found {
		if receipt.Kind != "run" || receipt.PayloadDigest != digest {
			return models.WorkflowRun{}, status_error.WorkflowRequestConflict
		}
		id, _ := receipt.Result["runId"].(string)
		return s.runs.GetRun(ctx, id)
	}
	definition, err := s.store.GetDefinition(ctx, command.DefinitionID)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	revisionID := command.RevisionID
	if revisionID == "" {
		revisionID = definition.CurrentRevisionID
	}
	if revisionID == "" {
		return models.WorkflowRun{}, status_error.WorkflowStateConflict
	}
	preparedRevision, err := s.store.GetRevision(ctx, definition.ID, revisionID)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	if err := verifyRevision(preparedRevision); err != nil {
		return models.WorkflowRun{}, err
	}
	if _, err := validateSourcePolicies(preparedRevision.Manifest); err != nil {
		return models.WorkflowRun{}, err
	}
	if err := s.validateIMDependencies(ctx, preparedRevision); err != nil {
		return models.WorkflowRun{}, err
	}
	preparedID := workflowRunID(definition.ID, command.RequestID)
	workspace, err := workflowfiles.PrepareWorkspace(s.dataDirectory, definition.ID, preparedID)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	var parent, previous *models.WorkflowRun
	limitWorkerPermissions := command.ConsumerLeaseID != ""
	if sourceID := cmp.Or(command.ParentRunID, command.RetrySourceRunID, command.ReplaySourceRunID); sourceID != "" {
		source, err := s.runs.GetRun(ctx, sourceID)
		if err != nil {
			return models.WorkflowRun{}, err
		}
		if sourceID == command.ParentRunID {
			parent = &source
		} else {
			previous = &source
			command.ApprovedScope = &source.ScopeBinding
		}
		command.additionalWorkingDirectories = source.AdditionalWorkingDirectories
		// 重试与子运行继续使用已冻结的调用者权限，聊天展示和停止归属独立处理。
		if source.WorkerPermissionsLimited {
			limitWorkerPermissions = true
			command.Defaults = constrainWorkflowDefaults(command.Defaults, source.Defaults)
		}
	}
	configurations := make(map[string]models.WorkflowExecutionDefaults)
	scope, err := s.prepareAdmission(ctx, definition, preparedRevision, command, parent, limitWorkerPermissions, configurations)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	pythonPath := ""
	needsPython := len(preparedRevision.Bundle.Scripts) > 0
	for _, node := range preparedRevision.Manifest.Nodes {
		if node.Kind == "functionTask" && node.Options["language"] == "python" {
			needsPython = true
		}
	}
	if needsPython {
		pythonPath, err = s.resolvePython(ctx)
		if err != nil {
			return models.WorkflowRun{}, err
		}
	}
	args := command.Args
	if args == nil {
		args = map[string]any{}
	}
	args = workflowdsl.ApplyDefaults(preparedRevision.Manifest.Meta.InputSchema, args)
	if err := workflowdsl.ValidateJSON(preparedRevision.Manifest.Meta.InputSchema, args); err != nil {
		return models.WorkflowRun{}, err
	}
	stage := command.stagedInputs
	if stage == nil && len(workflowdsl.FileValues(preparedRevision.Manifest.Meta.InputSchema, args)) > 0 {
		stage, err = workflowfiles.StageInputs(workspace, preparedID)
		if err != nil {
			return models.WorkflowRun{}, err
		}
		defer stage.Close()
		if err := s.prepareInputFiles(ctx, command, preparedRevision, preparedID, workspace, scope, args, stage, previous, parent); err != nil {
			return models.WorkflowRun{}, err
		}
	}
	var inputDigests map[string]string
	if stage != nil {
		inputDigests = make(map[string]string, len(stage.Files))
		args, err = workflowdsl.MapFiles(preparedRevision.Manifest.Meta.InputSchema, args, func(pointer, _ string) (string, error) {
			input, ok := stage.Files[pointer]
			if !ok {
				return "", status_error.WorkflowFileInvalid.WithField("field", pointer)
			}
			inputDigests[pointer] = input.Digest
			return input.Path, nil
		})
		if err != nil {
			return models.WorkflowRun{}, err
		}
	}
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	if receipt, found, err := s.store.FindReceipt(ctx, command.DefinitionID, command.RequestID); err != nil {
		return models.WorkflowRun{}, err
	} else if found {
		if receipt.Kind != "run" || receipt.PayloadDigest != digest {
			return models.WorkflowRun{}, status_error.WorkflowRequestConflict
		}
		id, _ := receipt.Result["runId"].(string)
		return s.runs.GetRun(ctx, id)
	}
	if stage != nil {
		if err := stage.Publish(); err != nil {
			return models.WorkflowRun{}, err
		}
	}
	var run models.WorkflowRun
	err = s.transactions.Transaction(ctx, func(ctx context.Context) error {
		if command.ChatSource != nil {
			if err := s.validateChatRunAdmission(ctx, command); err != nil {
				return err
			}
		}
		definition, err := s.store.GetDefinition(ctx, command.DefinitionID)
		if err != nil {
			return err
		}
		revisionID := command.RevisionID
		if revisionID == "" {
			revisionID = definition.CurrentRevisionID
		}
		if revisionID == "" {
			return status_error.WorkflowStateConflict
		}
		if revisionID != preparedRevision.ID {
			return status_error.WorkflowRevisionConflict
		}
		if err := s.validateReexecution(ctx, command, revisionID); err != nil {
			return err
		}
		revision, err := s.store.GetRevision(ctx, definition.ID, revisionID)
		if err != nil {
			return err
		}
		if err := s.validateIMDependencies(ctx, revision); err != nil {
			return err
		}
		encodedArgs, err := json.Marshal(args)
		if err != nil || len(encodedArgs) > constants.WorkflowOutputLimitBytes {
			return status_error.WorkflowBudgetExceeded.WithField("budget", "input_bytes")
		}
		// 缺少智能体配置明确拒绝，不能用任意账号或伙伴替换创作引用。
		for _, node := range revision.Manifest.Nodes {
			if node.Kind != "worker" {
				continue
			}
			resolve, _ := node.Options["resolve"].(map[string]any)
			runtimeID, _ := resolve["runtimeId"].(string)
			if runtimeID == "" && command.Defaults.RuntimeID == "" {
				return status_error.WorkflowResourceInvalid.WithField("field", "defaults.runtimeId")
			}
		}
		if command.Defaults.PermissionLevel == "" {
			command.Defaults.PermissionLevel = constants.PermissionLevelDefault
		}
		if !constants.SelectablePermissionLevel(command.Defaults.PermissionLevel) {
			return status_error.BadRequest
		}
		now := time.Now().UnixMilli()
		id := preparedID
		rootRunID := id
		var captureFilesForChatParent bool
		if command.ParentRunID != "" {
			parent, err := s.runs.GetRun(ctx, command.ParentRunID)
			if err != nil {
				return err
			}
			if workflowstate.TerminalRun(parent.Status) || parent.Status == constants.WorkflowRunStatusCancelling {
				return status_error.WorkflowStateConflict
			}
			if err := s.validateChildChain(ctx, parent, revision); err != nil {
				return err
			}
			rootRunID = parent.RootRunID
			if rootRunID == "" {
				rootRunID = parent.ID
			}
			count, err := s.runs.CountTreeRuns(ctx, rootRunID)
			if err != nil {
				return err
			}
			if count > constants.WorkflowMaxChildren {
				return status_error.WorkflowBudgetExceeded.WithField("budget", "child_count")
			}
			captureFilesForChatParent, err = s.childFilesReachChat(ctx, command, parent, revision)
			if err != nil {
				return err
			}
		}
		fiberID := util.New()
		timeout := revision.Manifest.Meta.Limits.TimeoutMillis
		if timeout <= 0 {
			timeout = constants.WorkflowDefaultTimeoutMillis
		}
		source := command.TriggerSource
		if source == "" && command.RetrySourceRunID != "" {
			source = "retry"
		}
		if source == "" {
			source = "manual"
		}
		var parentID, retryID any
		if command.ParentRunID != "" {
			parentID = command.ParentRunID
		}
		if command.RetrySourceRunID != "" {
			retryID = command.RetrySourceRunID
		}
		flowContext := map[string]any{"runId": id, "workflowId": definition.ID, "revisionId": revision.ID, "runStartedAt": now, "workflowName": revision.Manifest.Meta.Title, "parentRunId": parentID, "retrySourceRunId": retryID, "triggerSource": source, "scope": map[string]any{"projectId": scope.ProjectID, "workingDirectory": scope.WorkingDirectory}}
		workspaceJSON, _ := json.Marshal(workspace)
		workspaceValue := map[string]any{}
		_ = json.Unmarshal(workspaceJSON, &workspaceValue)
		flowContext["workspace"] = workspaceValue
		if pythonPath != "" {
			flowContext["pythonExecutable"] = pythonPath
		}
		run = models.WorkflowRun{AutomaticRunID: command.AutomaticRunID, ID: id, RootRunID: rootRunID, DefinitionID: definition.ID, RevisionID: revision.ID, RevisionOrdinal: revision.Ordinal, Status: constants.WorkflowRunStatusQueued, Args: args, InputFileDigests: inputDigests, Defaults: command.Defaults, WorkerConfigurations: configurations, Limits: revision.Manifest.Meta.Limits, ScopeBinding: scope, Context: flowContext, RootFiberID: fiberID, ParentRunID: command.ParentRunID, Joined: command.Joined, Depth: command.Depth, Version: 1, StartedAt: now, DeadlineAt: now + timeout}
		run.WorkerPermissionsLimited = limitWorkerPermissions
		run.AdditionalWorkingDirectories = command.additionalWorkingDirectories
		run.CaptureFilesForChatParent = captureFilesForChatParent
		if origin := command.ChatSource; origin != nil {
			run.SourceChatID = origin.ChatID
			run.SourceExecutionID = origin.ExecutionID
			run.SourceMessageID = origin.MessageID
			run.SourceThreadID = origin.ThreadID
			run.SourceWakerID = origin.WakerID
			run.SourceName = origin.Name
			run.SourceRunID = origin.RunID
			run.SourceWorkbench = origin.Workbench
		}
		fiber := models.WorkflowFiber{ID: fiberID, RunID: id, Path: "root", Status: constants.WorkflowStepStatusReady, State: workflowdsl.NewMachine(revision.Bundle, args, flowContext, now)}
		if err = s.runs.CreateRun(ctx, run, fiber); err != nil {
			return err
		}
		if err = s.store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: definition.ID, RequestID: command.RequestID, Kind: "run", PayloadDigest: digest, Result: map[string]any{"runId": id}}); err != nil {
			return err
		}
		if err := capability.Commit(ctx, map[string]any{"runId": id, "definitionId": definition.ID, "revisionId": revision.ID}); err != nil {
			return err
		}
		_, err = s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definition.ID, EventID: util.New(), Type: constants.WorkflowRunCreatedEvent, EntityID: id, Payload: map[string]any{"runId": id, "revisionId": revision.ID}})
		if err == nil && run.SourceChatID != "" {
			chatID, executionID := run.SourceChatID, run.SourceExecutionID
			if run.SourceWorkbench {
				executionID = ""
			}
			events.AfterCommit(ctx, func() { s.chatTurns.remember(chatID, executionID) })
		}
		return err
	})
	if err != nil && stage != nil {
		stage.Rollback()
	}
	return run, err
}

func workflowRunID(definitionID, requestID string) string {
	return requestidempotency.Identity{ID: requestID}.ResourceID("workflow-run", definitionID)
}

func keepsFinalChatFiles(run models.WorkflowRun) bool {
	return run.CaptureFilesForChatParent || run.SourceChatID != "" && !run.SourceWorkbench
}

// childFilesReachChat 只给可返回结果的子链保留文件。异步启动要求子流程声明文件且父流程可显式等待。
func (s *Service) childFilesReachChat(ctx context.Context, command StartRunCommand, parent models.WorkflowRun, revision models.WorkflowRevision) (bool, error) {
	if !keepsFinalChatFiles(parent) {
		return false, nil
	}
	if command.AwaitChildResult {
		return true, nil
	}
	if !workflowdsl.HasDeclaredFileFields(revision.Manifest.Meta.OutputSchema) {
		return false, nil
	}
	parentRevision, err := s.store.GetRevision(ctx, parent.DefinitionID, parent.RevisionID)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(parentRevision.Manifest.Nodes, func(node workflowdsl.Node) bool { return node.Kind == "joinWorkflow" }), nil
}

func (s *Service) validateIMDependencies(ctx context.Context, revision models.WorkflowRevision) error {
	return s.validateRevisionIMTargets(ctx, revision, map[string]bool{})
}

func (s *Service) validateRevisionIMTargets(ctx context.Context, revision models.WorkflowRevision, visited map[string]bool) error {
	if visited[revision.ID] {
		return nil
	}
	visited[revision.ID] = true
	dependencies, err := workflowdsl.IMTargetDependencies(revision.Manifest, revision.Bundle)
	if err != nil {
		return err
	}
	for _, dependency := range dependencies {
		if s.imValidate == nil {
			return status_error.WorkflowCapabilityUnsupported.WithField("capability", "im_target_validation")
		}
		if err := s.imValidate(ctx, dependency.TargetID); err != nil {
			return err
		}
	}
	for _, child := range workflowdsl.ChildWorkflowDependencies(revision.Manifest, revision.Bundle) {
		definition, err := s.store.GetDefinition(ctx, child.DefinitionID)
		if err != nil {
			return err
		}
		revisionID := child.RevisionID
		if revisionID == "" {
			revisionID = definition.CurrentRevisionID
		}
		childRevision, err := s.store.GetRevision(ctx, definition.ID, revisionID)
		if err != nil {
			return err
		}
		if err := s.validateRevisionIMTargets(ctx, childRevision, visited); err != nil {
			return err
		}
	}
	return nil
}

// CancelRun 提交停止意图，Runner 在真实外部执行收敛后形成 cancelled。
func (s *Service) CancelRun(ctx context.Context, id string) error {
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	return s.transactions.Transaction(ctx, func(ctx context.Context) error {
		run, err := s.requestRunCancellation(ctx, id)
		if err != nil {
			return err
		}
		return capability.Commit(ctx, map[string]any{"runId": id, "definitionId": run.DefinitionID, "cancellationRequested": true})
	})
}

func (s *Service) requestRunCancellation(ctx context.Context, id string) (models.WorkflowRun, error) {
	state, err := s.state.OwnedRun(ctx, id)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	if !workflowstate.TerminalRun(state.Run.Status) && state.Run.Status != constants.WorkflowRunStatusCancelling {
		state.Run.Status = constants.WorkflowRunStatusCancelling
		if err := s.state.CommitRun(ctx, state); err != nil {
			return models.WorkflowRun{}, err
		}
	}
	return state.Run, nil
}

func (s *Service) prepareAdmission(ctx context.Context, definition models.WorkflowDefinition, revision models.WorkflowRevision, command StartRunCommand, parent *models.WorkflowRun, limitWorkerPermissions bool, configurations map[string]models.WorkflowExecutionDefaults) (models.WorkflowScopeBinding, error) {
	scope := definition.ScopeBinding
	if command.ApprovedScope != nil {
		scope = *command.ApprovedScope
	}
	if parent != nil {
		if command.Depth != parent.Depth+1 || workflowstate.TerminalRun(parent.Status) || parent.Status == constants.WorkflowRunStatusCancelling {
			return scope, status_error.WorkflowStateConflict
		}
		scope = parent.ScopeBinding
	}
	if scope.ProjectID != "" && scope.WorkingDirectory == "" {
		if s.projects == nil {
			return scope, status_error.WorkflowResourceInvalid
		}
		project, err := s.projects.Get(ctx, scope.ProjectID)
		if err != nil {
			return scope, err
		}
		if project.WakerID != definition.CreationWakerID {
			return scope, status_error.WorkflowResourceInvalid
		}
		directories, err := s.projects.ListDirectories(ctx, project.ID)
		if err != nil {
			return scope, err
		}
		for _, directory := range directories {
			if directory.IsDefault {
				scope.WorkingDirectory = directory.Source.Path
				break
			}
		}
		if scope.WorkingDirectory == "" {
			return scope, status_error.WorkflowResourceInvalid
		}
	}
	if scope.WorkingDirectory == "" && s.dataDirectory != "" {
		var err error
		scope.WorkingDirectory, err = workflowfiles.PrepareManagedWorkspace(s.dataDirectory, definition.ID)
		if err != nil {
			return scope, err
		}
	}
	directory, err := workflowfiles.EnsureScopeDirectory(scope.WorkingDirectory)
	if err != nil {
		return scope, err
	}
	if (command.ApprovedScope != nil || parent != nil) && directory != scope.WorkingDirectory {
		return scope, status_error.WorkflowResourceInvalid.WithField("field", "workingDirectory")
	}
	scope.WorkingDirectory = directory
	defaults := command.Defaults
	if defaults.PermissionLevel == "" {
		defaults.PermissionLevel = constants.PermissionLevelDefault
	}
	if limitWorkerPermissions {
		// 已接纳的调用者使用系统身份；保留其冻结选择供同一身份的节点继承。
		defaults.RuntimeID = strings.TrimSpace(defaults.RuntimeID)
		defaults.AccountID = strings.TrimSpace(defaults.AccountID)
		if defaults.AccountID == "" {
			defaults.AccountID = constants.AiAccountSystemID
		}
	}
	for _, node := range revision.Manifest.Nodes {
		if node.Kind == "worker" {
			resolve, _ := node.Options["resolve"].(map[string]any)
			wakerID, _ := resolve["wakerId"].(string)
			if s.wakers != nil {
				if _, err := s.wakers.Get(ctx, wakerID); err != nil {
					return scope, err
				}
			}
			if s.worker == nil {
				return scope, status_error.WorkflowCapabilityUnsupported
			}
			var nodeDefaults models.WorkflowExecutionDefaults
			selectionResolved := command.WorkerConfigurations != nil
			if selectionResolved {
				var found bool
				nodeDefaults, found = command.WorkerConfigurations[node.ID]
				if !found {
					return scope, status_error.WorkflowStateInvalid
				}
			} else {
				var inherited bool
				nodeDefaults, inherited = workerConfiguration(node.Options, defaults)
				selectionResolved = limitWorkerPermissions && inherited
			}
			if limitWorkerPermissions && defaults.WorkspaceReadOnly {
				nodeDefaults.WorkspaceReadOnly = true
			}
			resolved, err := s.worker.ResolveAdmission(ctx, node.Options, nodeDefaults, selectionResolved)
			if err != nil {
				return scope, err
			}
			if _, err := workerRetryDiagnostic(node, &resolved); err != nil {
				return scope, err
			}
			// 从既有执行调用工作流时，节点配置不能扩大调用者冻结的权限。
			if limitWorkerPermissions && workflowPermissions(resolved.PermissionLevel) > workflowPermissions(defaults.PermissionLevel) {
				return scope, status_error.WorkflowCapabilityUnsupported.WithField("capability", "worker_permission_escalation")
			}
			configurations[node.ID] = resolved
		}
		if node.Kind == "functionTask" && node.Options["language"] == "python" {
			if node.Options["executionMode"] != "trusted_local" || defaults.WorkspaceReadOnly {
				return scope, status_error.WorkflowCapabilityUnsupported.WithField("capability", "python_pure_isolation")
			}

		}
	}
	return scope, nil
}

// UploadFile 为工作流手动运行和自动任务 API 保存受管原件。
func (s *Service) UploadFile(ctx context.Context, definitionID, taskID, name string, input io.Reader) (workflowfiles.Upload, error) {
	if _, err := s.store.GetDefinition(ctx, definitionID); err != nil {
		return workflowfiles.Upload{}, err
	}
	return workflowfiles.Save(ctx, s.dataDirectory, definitionID, taskID, name, input)
}

func (s *Service) prepareInputFiles(ctx context.Context, command StartRunCommand, revision models.WorkflowRevision, runID string, workspace workflowfiles.Workspace, scope models.WorkflowScopeBinding, args any, stage *workflowfiles.StagedInputs, previous, parent *models.WorkflowRun) error {
	schema := revision.Manifest.Meta.InputSchema
	var sourceWorkspace workflowfiles.Workspace
	var sourceInputs map[string]string
	if previous != nil {
		if previous.DefinitionID != revision.DefinitionID || previous.RevisionID != revision.ID {
			return status_error.WorkflowStateConflict
		}
		var err error
		sourceWorkspace, err = runWorkspace(*previous)
		if err != nil {
			return err
		}
		sourceInputs = workflowdsl.FileValues(schema, previous.Args)
	}
	roots := append([]string{scope.WorkingDirectory, workspace.RootDirectory}, command.additionalWorkingDirectories...)
	ownerWorkspace := workspace
	if parent != nil {
		var err error
		ownerWorkspace, err = runWorkspace(*parent)
		if err != nil {
			return err
		}
		roots = append(roots, ownerWorkspace.RootDirectory)
	}
	_, err := workflowdsl.MapFiles(schema, args, func(pointer, reference string) (string, error) {
		var frozen workflowfiles.LocalInput
		var err error
		switch {
		case previous != nil:
			digest := previous.InputFileDigests[pointer]
			if reference != sourceInputs[pointer] || digest == "" {
				return "", status_error.WorkflowFileInvalid.WithField("field", pointer)
			}
			frozen, err = workflowfiles.ReplayFrozenInput(ctx, sourceWorkspace, previous.ID, workflowfiles.Workspace{RootDirectory: stage.Directory}, runID, pointer, reference, digest)
			if errors.Is(err, status_error.WorkflowFileInvalid) {
				return "", status_error.WorkflowFileInvalid.WithField("field", pointer)
			}
		case strings.HasPrefix(reference, constants.WorkflowFileReferencePrefix):
			if command.ChatSource != nil {
				return "", status_error.WorkflowFileInvalid.WithField("field", pointer)
			}
			frozen, err = workflowfiles.Materialize(ctx, s.dataDirectory, command.DefinitionID, command.InputFileTaskID, runID, stage.Directory, reference, command.InputFileOwnerKeyID)
		default:
			reference = ownerWorkspace.ResolveRunReference(reference)
			frozen, err = workflowfiles.FreezeLocalInput(ctx, roots, nil, reference, stage.Directory, runID, pointer, "")
		}
		if err != nil {
			return "", err
		}
		return reference, stage.Record(pointer, frozen)
	})
	return err
}

// runWorkspace 要求接纳时冻结的完整目录事实，并校验当前目录归属。
func runWorkspace(run models.WorkflowRun) (workflowfiles.Workspace, error) {
	workspace, err := workflowfiles.ParseWorkspace(run.Context["workspace"], run.DefinitionID, run.ID)
	if err != nil {
		return workflowfiles.Workspace{}, err
	}
	prepared, err := workflowfiles.PrepareWorkspace(filepath.Dir(filepath.Dir(workspace.RootDirectory)), run.DefinitionID, run.ID)
	if err != nil {
		return workflowfiles.Workspace{}, err
	}
	if workspace != prepared {
		return workflowfiles.Workspace{}, status_error.WorkflowStateConflict.WithField("field", "workspace")
	}
	return workspace, nil
}

// openRunFile 只读取冻结业务范围和宿主内部目录中的文件，不接受结果自报的额外授权。
func openRunFile(run models.WorkflowRun, candidate string) (*os.File, error) {
	workspace, err := workflowfiles.ParseWorkspace(run.Context["workspace"], run.DefinitionID, run.ID)
	if err != nil {
		return nil, err
	}
	candidate = workspace.ResolveRunReference(candidate)
	directories := runFileRoots(run, workspace)
	for _, directory := range directories {
		file, err := workflowfiles.OpenScopeFile(directory, candidate)
		if err == nil {
			return file, nil
		}
		if errors.Is(err, status_error.WorkflowBudgetExceeded) || errors.Is(err, status_error.WorkflowFileInvalid) {
			return nil, err
		}
	}
	return nil, status_error.WorkflowFileNotFound
}

func runFileRoots(run models.WorkflowRun, workspace workflowfiles.Workspace) []string {
	return append([]string{run.ScopeBinding.WorkingDirectory, workspace.RootDirectory}, run.AdditionalWorkingDirectories...)
}

// OutputFile 是最终输出 schema 中明确声明的可下载文件。
type OutputFile struct {
	// ID 是结果字段的稳定下载身份，不接受调用方指定本地路径。
	ID string `json:"id"`
	// Field 是结果中的 JSON Pointer。
	Field string `json:"field"`
	// Name 是下载文件名。
	Name string `json:"name"`
	// Size 是当前真实字节数。
	Size int64 `json:"size"`
	// MediaType 是下载类型，未知类型使用二进制流。
	MediaType string `json:"mediaType"`
	// Available 表示文件仍存在且属于本次运行范围。
	Available bool `json:"available"`
}

func outputFileID(pointer string) string {
	digest := sha256.Sum256([]byte(pointer))
	return hex.EncodeToString(digest[:16])
}

// OutputFiles 按不可变运行版本识别最终文件结果，普通字符串不被当作文件路径。
func (s *Service) OutputFiles(ctx context.Context, run models.WorkflowRun) ([]OutputFile, error) {
	files := []OutputFile{}
	if run.Status != constants.WorkflowRunStatusSucceeded {
		return files, nil
	}
	revision, err := s.store.GetRevision(ctx, run.DefinitionID, run.RevisionID)
	if err != nil {
		return nil, err
	}
	return s.InspectFinalOutputFiles(run, revision.Manifest.Meta.OutputSchema), nil
}

// InspectOutputFiles 检查指定冻结 Schema 和结果中的真实文件，供运行与节点产出入口复用。
func InspectOutputFiles(run models.WorkflowRun, schema map[string]any, result any) []OutputFile {
	return inspectOutputFiles(schema, result, func(_ string, path string) (int64, bool) {
		file, err := openRunFile(run, path)
		if err != nil {
			return 0, false
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return 0, false
		}
		return info.Size(), true
	})
}

// InspectFinalOutputFiles 读取已确认快照的文件元数据，用于结果列表。
func (s *Service) InspectFinalOutputFiles(run models.WorkflowRun, schema map[string]any) []OutputFile {
	if !keepsFinalChatFiles(run) {
		return InspectOutputFiles(run, schema, run.Result)
	}
	return inspectOutputFiles(schema, run.Result, func(field, _ string) (int64, bool) {
		file, size, err := s.openChatSnapshot(run, run.ChatFiles[field])
		if err != nil {
			return 0, false
		}
		file.Close()
		return size, true
	})
}

func inspectOutputFiles(schema map[string]any, result any, sizeOf func(string, string) (int64, bool)) []OutputFile {
	files := []OutputFile{}
	values := workflowdsl.FileValues(schema, result)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, pointer := range keys {
		path := values[pointer]
		view := OutputFile{ID: outputFileID(pointer), Field: pointer, Name: filepath.Base(path), MediaType: "application/octet-stream"}
		if media := mime.TypeByExtension(filepath.Ext(path)); media != "" {
			view.MediaType = media
		}
		if size, available := sizeOf(pointer, path); available {
			view.Available = true
			view.Size = size
		}
		files = append(files, view)
	}
	return files
}

// OpenOutputFile 仅允许下载本次成功运行已声明的最终文件产物。
func (s *Service) OpenOutputFile(ctx context.Context, definitionID, runID, fileID string) (*os.File, string, error) {
	run, err := s.runs.GetRun(ctx, runID)
	if err != nil {
		return nil, "", err
	}
	if run.DefinitionID != definitionID || run.Status != constants.WorkflowRunStatusSucceeded {
		return nil, "", status_error.WorkflowFileNotFound
	}
	revision, err := s.store.GetRevision(ctx, run.DefinitionID, run.RevisionID)
	if err != nil {
		return nil, "", err
	}
	if keepsFinalChatFiles(run) {
		for pointer := range workflowdsl.FileValues(revision.Manifest.Meta.OutputSchema, run.Result) {
			if outputFileID(pointer) == fileID {
				file := run.ChatFiles[pointer]
				content, err := s.openChatFile(run, file)
				return content, file.Name, err
			}
		}
		return nil, "", status_error.WorkflowFileNotFound
	}
	return OpenDeclaredOutputFile(run, revision.Manifest.Meta.OutputSchema, run.Result, fileID)
}

// OpenDeclaredOutputFile 只打开冻结结果中已声明且属于运行真实目录的文件。
func OpenDeclaredOutputFile(run models.WorkflowRun, schema map[string]any, result any, fileID string) (*os.File, string, error) {
	for pointer, path := range workflowdsl.FileValues(schema, result) {
		if outputFileID(pointer) == fileID {
			file, err := openRunFile(run, path)
			return file, filepath.Base(path), err
		}
	}
	return nil, "", status_error.WorkflowFileNotFound
}

// UploadFileForKey 由集成入口保存主体隔离的任务输入，不改变旧 Token 或本机上传归属。
func (s *Service) UploadFileForKey(ctx context.Context, definitionID, taskID, keyID, name string, input io.Reader) (workflowfiles.Upload, error) {
	if keyID == "" || taskID == "" {
		return workflowfiles.Upload{}, status_error.WorkflowFileInvalid
	}
	if _, err := s.store.GetDefinition(ctx, definitionID); err != nil {
		return workflowfiles.Upload{}, err
	}
	return workflowfiles.Save(ctx, s.dataDirectory, definitionID, taskID, name, input, keyID)
}
