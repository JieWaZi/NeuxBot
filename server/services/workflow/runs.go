package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowfiles "github.com/JieWaZi/NeuxBot/server/internal/workflow/files"
	workflowfunction "github.com/JieWaZi/NeuxBot/server/internal/workflow/function"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// StartRunCommand 是用户、子运行与自动任务共用的接纳命令。
type StartRunCommand struct {
	// AutomaticRunID 是服务端传入的原自动运行，不能从用户或模型正文绑定。
	AutomaticRunID string `json:"-"`
	// InputFileOwnerKeyID 由调用入口填写，原件同时绑定任务和集成主体。
	InputFileOwnerKeyID string `json:"-"`
	// InputFileRoots 只由 CLI 认证层提供，允许读取的原文件将在接纳前冻结。
	InputFileRoots []string `json:"-"`
	// WorkerConfigurations 仅由重新运行入口携带原节点配置，外部输入不能自行填写。
	WorkerConfigurations map[string]models.WorkflowExecutionDefaults `json:"-"`
	// InputFileTaskID 是自动任务入口绑定的上传归属，外部请求不能自行填写。
	InputFileTaskID string `json:"-"`
	// ConsumerLeaseID 是服务端绑定的单回合使用者身份，不接纳模型自填。
	ConsumerLeaseID string `json:"-"`
	// ApprovedScope 是业务入口已经验证的工作范围，HTTP 和模型不能自行扩大。
	ApprovedScope *models.WorkflowScopeBinding `json:"-"`
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
	if command.RequestID == "" || len(command.RequestID) > 128 || command.Depth > constants.WorkflowMaxChildDepth {
		return models.WorkflowRun{}, status_error.BadRequest
	}
	encodedArgs, err := json.Marshal(command.Args)
	if err != nil || len(encodedArgs) > constants.WorkflowOutputLimitBytes {
		return models.WorkflowRun{}, status_error.WorkflowBudgetExceeded.WithField("budget", "input_bytes")
	}
	digest := workflowstate.Digest([]any{command, command.DefinitionID, command.TriggerSource, command.RetrySourceRunID, command.ParentRunID, command.Joined, command.Depth, command.ConsumerLeaseID, command.ApprovedScope, command.InputFileTaskID, command.InputFileOwnerKeyID, command.AutomaticRunID, command.WorkerConfigurations})
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
	if err := s.validateIMDependencies(ctx, preparedRevision); err != nil {
		return models.WorkflowRun{}, err
	}
	preparedID := requestidempotency.Identity{ID: command.RequestID}.ResourceID("workflow-run", definition.ID)
	configurations := make(map[string]models.WorkflowExecutionDefaults)
	scope, err := s.prepareAdmission(ctx, definition, preparedRevision, command, preparedID, configurations)
	if err != nil {
		return models.WorkflowRun{}, err
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
	args := command.Args
	if args == nil {
		args = map[string]any{}
	}
	args = workflowdsl.ApplyDefaults(preparedRevision.Manifest.Meta.InputSchema, args)
	if err := workflowdsl.ValidateJSON(preparedRevision.Manifest.Meta.InputSchema, args); err != nil {
		return models.WorkflowRun{}, err
	}
	preparedArgs, err := s.prepareInputFiles(ctx, command, preparedRevision, preparedID, scope, args)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	var run models.WorkflowRun
	err = s.transactions.Transaction(ctx, func(ctx context.Context) error {
		receipt, found, err := s.store.FindReceipt(ctx, command.DefinitionID, command.RequestID)
		if err != nil {
			return err
		}
		if found {
			if receipt.Kind != "run" || receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			id, _ := receipt.Result["runId"].(string)
			run, err = s.runs.GetRun(ctx, id)
			return err
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
		revision, err := s.store.GetRevision(ctx, definition.ID, revisionID)
		if err != nil {
			return err
		}
		if err := s.validateIMDependencies(ctx, revision); err != nil {
			return err
		}
		args := preparedArgs
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
		}
		fiberID := util.New()
		timeout := revision.Manifest.Meta.Limits.TimeoutMillis
		if timeout <= 0 {
			timeout = constants.WorkflowDefaultTimeoutMillis
		}
		source := command.TriggerSource
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
		if command.ConsumerLeaseID != "" {
			flowContext["consumerLeaseId"] = command.ConsumerLeaseID
		}
		run = models.WorkflowRun{AutomaticRunID: command.AutomaticRunID, ID: id, RootRunID: rootRunID, DefinitionID: definition.ID, RevisionID: revision.ID, RevisionOrdinal: revision.Ordinal, Status: constants.WorkflowRunStatusQueued, Args: args, Defaults: command.Defaults, WorkerConfigurations: configurations, Limits: revision.Manifest.Meta.Limits, ScopeBinding: scope, Context: flowContext, RootFiberID: fiberID, ParentRunID: command.ParentRunID, Joined: command.Joined, Depth: command.Depth, Version: 1, StartedAt: now, DeadlineAt: now + timeout}
		fiber := models.WorkflowFiber{ID: fiberID, RunID: id, Path: "root", Status: constants.WorkflowStepStatusReady, State: workflowdsl.NewMachine(revision.Bundle, args, flowContext, now)}
		if err = s.runs.CreateRun(ctx, run, fiber); err != nil {
			return err
		}
		if err = s.store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: definition.ID, RequestID: command.RequestID, Kind: "run", PayloadDigest: digest, Result: map[string]any{"runId": id}}); err != nil {
			return err
		}
		if err := agentcommand.Commit(ctx, map[string]any{"runId": id, "definitionId": definition.ID, "revisionId": revision.ID}); err != nil {
			return err
		}
		_, err = s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definition.ID, EventID: util.New(), Type: constants.WorkflowRunCreatedEvent, EntityID: id, Payload: map[string]any{"runId": id, "revisionId": revision.ID}})
		return err
	})
	return run, err
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
		state, err := s.state.OwnedRun(ctx, id)
		if err != nil {
			return err
		}
		if !workflowstate.TerminalRun(state.Run.Status) {
			state.Run.Status = constants.WorkflowRunStatusCancelling
			if err := s.state.CommitRun(ctx, state); err != nil {
				return err
			}
		}
		return agentcommand.Commit(ctx, map[string]any{"runId": id, "definitionId": state.Run.DefinitionID, "cancellationRequested": true})
	})
}

func (s *Service) prepareAdmission(ctx context.Context, definition models.WorkflowDefinition, revision models.WorkflowRevision, command StartRunCommand, runID string, configurations map[string]models.WorkflowExecutionDefaults) (models.WorkflowScopeBinding, error) {
	scope := definition.ScopeBinding
	if err := s.validateSourcePolicies(ctx, revision.Manifest); err != nil {
		return scope, err
	}
	if command.ApprovedScope != nil {
		scope = *command.ApprovedScope
	}
	if command.ParentRunID != "" {
		parent, err := s.runs.GetRun(ctx, command.ParentRunID)
		if err != nil {
			return scope, err
		}
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
		scope.WorkingDirectory, err = workflowfiles.PrepareManagedWorkspace(s.dataDirectory, runID)
		if err != nil {
			return scope, err
		}
	}
	directory, err := workflowfiles.EnsureScopeDirectory(scope.WorkingDirectory)
	if err != nil {
		return scope, err
	}
	scope.WorkingDirectory = directory
	defaults := command.Defaults
	if defaults.PermissionLevel == "" {
		defaults.PermissionLevel = constants.PermissionLevelDefault
	}
	for _, node := range revision.Manifest.Nodes {
		if node.Kind == "worker" {
			if retry, _ := node.Options["retry"].(map[string]any); len(retry) > 0 {
				if retry["safety"] == "pure" && !defaults.WorkspaceReadOnly {
					return scope, status_error.WorkflowCapabilityUnsupported.WithField("capability", "worker_retry_requires_readonly")
				}
				if retry["safety"] == "idempotent" {
					// V1 Core 没有逐工具的幂等键强制端口，声明不能代替真实去重。
					return scope, status_error.WorkflowCapabilityUnsupported.WithField("capability", "worker_retry_idempotency_enforcement")
				}
			}
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
			nodeDefaults := defaults
			if command.WorkerConfigurations != nil {
				var found bool
				nodeDefaults, found = command.WorkerConfigurations[node.ID]
				if !found {
					return scope, status_error.WorkflowStateInvalid
				}
			}
			resolved, err := s.worker.ResolveAdmission(ctx, node.Options, nodeDefaults, command.WorkerConfigurations != nil)
			if err != nil {
				return scope, err
			}
			// 从既有执行调用工作流时，节点配置不能扩大调用者冻结的权限。
			if command.ConsumerLeaseID != "" {
				levels := map[constants.PermissionLevel]int{
					constants.PermissionLevelDefault:    0,
					constants.PermissionLevelAuto:       1,
					constants.PermissionLevelFullAccess: 2,
				}
				if levels[resolved.PermissionLevel] > levels[defaults.PermissionLevel] {
					return scope, status_error.WorkflowCapabilityUnsupported.WithField("capability", "worker_permission_escalation")
				}
			}
			configurations[node.ID] = resolved
		}
		if node.Kind == "functionTask" && node.Options["language"] == "python" {
			if node.Options["executionMode"] != "trusted_local" || defaults.WorkspaceReadOnly {
				return scope, status_error.WorkflowCapabilityUnsupported.WithField("capability", "python_pure_isolation")
			}
			if s.pythonExecutable == "" {
				return scope, status_error.WorkflowResourceInvalid.WithField("field", "workflowPythonExecutable")
			}
			if err := workflowfunction.PythonVersion(ctx, s.pythonExecutable); err != nil {
				return scope, err
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

func (s *Service) prepareInputFiles(ctx context.Context, command StartRunCommand, revision models.WorkflowRevision, runID string, scope models.WorkflowScopeBinding, args any) (any, error) {
	return workflowdsl.MapFiles(revision.Manifest.Meta.InputSchema, args, func(pointer, reference string) (string, error) {
		if strings.HasPrefix(reference, constants.WorkflowFileReferencePrefix) {
			return workflowfiles.Materialize(ctx, s.dataDirectory, command.DefinitionID, command.InputFileTaskID, runID, scope.WorkingDirectory, reference, command.InputFileOwnerKeyID)
		}
		if len(command.InputFileRoots) > 0 {
			frozen, err := workflowfiles.FreezeLocalInput(ctx, command.InputFileRoots, reference, scope.WorkingDirectory, runID, pointer)
			return frozen.Path, err
		}
		// 子工作流和明确的重新运行可以使用父运行已冻结且仍在批准范围内的真实文件。
		file, err := workflowfiles.OpenScopeFile(scope.WorkingDirectory, reference)
		if err != nil {
			return "", err
		}
		defer file.Close()
		path := reference
		if !filepath.IsAbs(path) {
			path = filepath.Join(scope.WorkingDirectory, path)
		}
		return path, nil
	})
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
	return InspectOutputFiles(run, revision.Manifest.Meta.OutputSchema, run.Result), nil
}

// InspectOutputFiles 检查指定冻结 Schema 和结果中的真实文件，供运行与节点产出入口复用。
func InspectOutputFiles(run models.WorkflowRun, schema map[string]any, result any) []OutputFile {
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
		file, err := workflowfiles.OpenScopeFile(run.ScopeBinding.WorkingDirectory, path)
		if err == nil {
			info, statErr := file.Stat()
			if statErr == nil {
				view.Available = true
				view.Size = info.Size()
			}
			file.Close()
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
	return OpenDeclaredOutputFile(run, revision.Manifest.Meta.OutputSchema, run.Result, fileID)
}

// OpenDeclaredOutputFile 只打开冻结结果中已声明且属于运行真实目录的文件。
func OpenDeclaredOutputFile(run models.WorkflowRun, schema map[string]any, result any, fileID string) (*os.File, string, error) {
	for pointer, path := range workflowdsl.FileValues(schema, result) {
		if outputFileID(pointer) == fileID {
			file, err := workflowfiles.OpenScopeFile(run.ScopeBinding.WorkingDirectory, path)
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
