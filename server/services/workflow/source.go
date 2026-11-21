package workflow

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	capability "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// SaveSourceCommand 是源码保存和恢复共同遵守的 CAS 命令。
type SaveSourceCommand struct {
	// ExecutionID 是工具所在回合，事务内再次验证，阻止旧回合迟到保存。
	ExecutionID string `json:"-"`
	// ExecutionChatID 是从本地伙伴对话调用保存时的原 Chat。
	ExecutionChatID string `json:"-"`
	// DefinitionID 是允许写入的工作流。
	DefinitionID string `json:"-"`
	// RequestID 是同一保存内容的幂等身份。
	RequestID string `json:"requestId"`
	// ExpectedRevisionID 是保存前读取到的当前版本。
	ExpectedRevisionID string `json:"expectedRevisionId"`
	// BaseSourceDigest 是补丁对应的精确基准摘要。
	BaseSourceDigest string `json:"baseSourceDigest,omitempty"`
	// Patches 是按顺序应用的唯一文本替换。
	Patches []SourcePatch `json:"patches,omitempty"`
	// Source 是不裁剪和不格式化的完整创作源码。
	Source string `json:"source"`
	// SDK 是本次创作使用的原生契约。
	SDK string `json:"sdk"`
	// CreationSessionID 是服务端校验租约后填入的创作身份。
	CreationSessionID string `json:"-"`
	// RestoredFromRevisionID 是恢复操作的来源版本。
	RestoredFromRevisionID string `json:"-"`
	// ResolvedAnnotationIDs 是工具明确声明已经处理的批注身份。
	ResolvedAnnotationIDs []string `json:"resolvedAnnotationIds,omitempty"`
}

// SourcePatch 描述一次必须唯一匹配的精确源码替换。
type SourcePatch struct {
	// Search 是基准源码中必须恰好出现一次的字节串。
	Search string `json:"search"`
	// Replace 是替换后的完整字节串。
	Replace string `json:"replace"`
}

// SaveSource 同时提交源码、投影、程序、当前指针、回执与事件。
func (s *Service) SaveSource(ctx context.Context, command SaveSourceCommand) (workflowdsl.SaveReceipt, []workflowdsl.Diagnostic, error) {
	if command.RequestID == "" || len(command.RequestID) > 128 || command.SDK != constants.WorkflowSDKIdentifier {
		return workflowdsl.SaveReceipt{}, nil, status_error.BadRequest
	}
	digest := workflowstate.Digest(struct {
		// Expected 是创作读取的当前版本。
		Expected string
		// Source 是精确源码。
		Source string
		// SDK 是本次保存的契约身份。
		SDK string
		// Restored 是恢复来源。
		Restored string
		// Annotations 是本次明确处理的批注。
		Annotations []string
		// Base 是补丁基准摘要。
		Base string
		// Patches 是精确替换载荷。
		Patches []SourcePatch
	}{command.ExpectedRevisionID, command.Source, command.SDK, command.RestoredFromRevisionID, command.ResolvedAnnotationIDs, command.BaseSourceDigest, command.Patches})
	if len(command.Patches) > 0 {
		if command.Source != "" || command.BaseSourceDigest == "" || command.ExpectedRevisionID == "" {
			return workflowdsl.SaveReceipt{}, nil, status_error.BadRequest
		}
		base, err := s.store.GetRevision(ctx, command.DefinitionID, command.ExpectedRevisionID)
		if err != nil {
			return workflowdsl.SaveReceipt{}, nil, err
		}
		if base.SourceDigest != command.BaseSourceDigest {
			return workflowdsl.SaveReceipt{}, nil, status_error.WorkflowRevisionConflict
		}
		source := base.Source
		for _, patch := range command.Patches {
			if patch.Search == "" || strings.Count(source, patch.Search) != 1 {
				return workflowdsl.SaveReceipt{}, nil, status_error.WorkflowSourceInvalid.WithField("reason", "patch_search_not_unique")
			}
			source = strings.Replace(source, patch.Search, patch.Replace, 1)
		}
		command.Source = source
	}
	// 编译不持有数据库事务；任何诊断都不产生版本或保存成功事件。
	compiled, diagnostics, err := CompileSource(command.Source)
	if err != nil {
		return workflowdsl.SaveReceipt{}, diagnostics, err
	}
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	var saved workflowdsl.SaveReceipt
	err = s.transactions.Transaction(ctx, func(ctx context.Context) error {
		var configuration *models.WorkflowExecutionDefaults
		if command.CreationSessionID != "" {
			if s.creationSessions == nil {
				return status_error.WorkflowCapabilityUnsupported
			}
			session, err := s.creationSessions.GetCreationSession(ctx, command.CreationSessionID)
			if err != nil {
				return err
			}
			if session.DefinitionID != command.DefinitionID {
				return status_error.WorkflowAuthoringSuperseded
			}
			if err := s.validateCreationExecution(ctx, session, command.ExecutionID); err != nil {
				return err
			}
			configuration = &session.Defaults
		} else if command.ExecutionChatID != "" {
			actual, _, err := s.readWorkflowExecution(ctx, command.ExecutionChatID, command.ExecutionID)
			if err != nil {
				return err
			}
			defaults := workflowExecutionDefaults(actual)
			configuration = &defaults
		}
		receipt, found, err := s.store.FindReceipt(ctx, command.DefinitionID, command.RequestID)
		if err != nil {
			return err
		}
		if found {
			if receipt.Kind != "save" || receipt.PayloadDigest != digest {
				return status_error.WorkflowRequestConflict
			}
			encoded, _ := json.Marshal(receipt.Result)
			if json.Unmarshal(encoded, &saved) != nil {
				return status_error.WorkflowStateInvalid
			}
			saved.Replayed = true
			return nil
		}
		// 先恢复已有回执；新的保存必须满足当前创作配置，但不重写原保存事实。
		configurationDiagnostics, err := validateWorkerRetryPolicies(compiled.Manifest, configuration)
		if err != nil {
			diagnostics = configurationDiagnostics
			return err
		}
		definition, err := s.store.GetDefinition(ctx, command.DefinitionID)
		if err != nil {
			return err
		}
		if definition.CurrentRevisionID != command.ExpectedRevisionID {
			return status_error.WorkflowRevisionConflict
		}
		revision := models.WorkflowRevision{ID: util.New(), DefinitionID: definition.ID, Ordinal: definition.Ordinal + 1, Source: command.Source, SourceDigest: compiled.SourceDigest, ManifestDigest: compiled.ManifestDigest, IRDigest: compiled.IRDigest, SDK: constants.WorkflowSDKIdentifier, CompilerVersion: constants.WorkflowCompilerVersion, IRVersion: constants.WorkflowIRVersion, Manifest: compiled.Manifest, Bundle: compiled.Bundle, CreationSessionID: command.CreationSessionID, RestoredFromRevisionID: command.RestoredFromRevisionID}
		if err := s.store.CreateRevision(ctx, revision); err != nil {
			return err
		}
		definition.CurrentRevisionID = revision.ID
		definition.Ordinal = revision.Ordinal
		definition.Title = compiled.Manifest.Meta.Title
		definition.Description = compiled.Manifest.Meta.Description
		definition.WhenToUse = compiled.Manifest.Meta.WhenToUse
		if err := s.store.UpdateHead(ctx, definition, command.ExpectedRevisionID); err != nil {
			return err
		}
		saved = workflowdsl.SaveReceipt{DefinitionID: definition.ID, RevisionID: revision.ID, Ordinal: revision.Ordinal, SourceDigest: revision.SourceDigest, ManifestDigest: workflowdsl.CanvasDigest(workflowdsl.ExportCanvas(compiled.Manifest))}
		if err := s.recordSourceAnnotations(ctx, command, revision.ID); err != nil {
			return err
		}
		if err := s.saveReceipt(ctx, command, digest, saved); err != nil {
			return err
		}
		_, err = s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definition.ID, EventID: util.New(), Type: "workflow.revision.saved", EntityID: revision.ID, Payload: map[string]any{"definitionId": definition.ID, "revisionId": revision.ID, "ordinal": revision.Ordinal, "sourceDigest": revision.SourceDigest, "manifestDigest": saved.ManifestDigest}})
		return err
	})
	return saved, diagnostics, err
}

func (s *Service) recordSourceAnnotations(ctx context.Context, command SaveSourceCommand, revisionID string) error {
	if s.creationSessions == nil {
		return nil
	}
	if command.CreationSessionID != "" {
		if err := s.creationSessions.RecordCreationSave(ctx, command.CreationSessionID, revisionID); err != nil {
			return err
		}
	}
	if err := s.creationSessions.ResolveAnnotations(ctx, command.DefinitionID, command.ResolvedAnnotationIDs, revisionID); err != nil {
		return err
	}
	if len(command.ResolvedAnnotationIDs) > 0 {
		_, err := s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: command.DefinitionID, EventID: util.New(), Type: constants.WorkflowAnnotationChangedEvent, EntityID: revisionID, Payload: map[string]any{"resolvedAnnotationIds": command.ResolvedAnnotationIDs, "revisionId": revisionID}})
		return err
	}
	return nil
}

func (s *Service) saveReceipt(ctx context.Context, command SaveSourceCommand, digest string, value workflowdsl.SaveReceipt) error {
	encoded, _ := json.Marshal(value)
	var result map[string]any
	if json.Unmarshal(encoded, &result) != nil {
		return status_error.WorkflowStateInvalid
	}
	if err := s.store.CreateReceipt(ctx, models.WorkflowCommandReceipt{AggregateID: command.DefinitionID, RequestID: command.RequestID, Kind: "save", PayloadDigest: digest, Result: result}); err != nil {
		return err
	}
	return capability.Commit(ctx, result)
}

// CompileSource 为源码校验和保存提供一致的编译结果与宿主策略诊断。
func CompileSource(source string) (workflowdsl.Compilation, []workflowdsl.Diagnostic, error) {
	compiled, diagnostics, err := workflowdsl.Compile(source)
	if err != nil {
		return compiled, diagnostics, err
	}
	if diagnostic, err := validateSourcePolicies(compiled.Manifest); err != nil {
		return compiled, append(diagnostics, diagnostic), err
	}
	return compiled, diagnostics, nil
}

// validateSourcePolicies 拒绝原生执行无法强制实施的节点策略。
func validateSourcePolicies(manifest workflowdsl.Manifest) (workflowdsl.Diagnostic, error) {
	for _, node := range manifest.Nodes {
		if node.Kind != "worker" {
			continue
		}
		if field, err := validateWorkerToolPolicy(node.Options); err != nil {
			return workflowdsl.Diagnostic{
				Code: "WORKER_TOOL_POLICY_UNSUPPORTED", Severity: "error",
				Message: "Worker connector and native tool policies only support inherit.",
				NodeID:  node.ID, Field: field, Range: node.Range,
			}, err
		}
	}
	return workflowdsl.Diagnostic{}, nil
}

func verifyRevision(revision models.WorkflowRevision) error {
	if revision.SDK != constants.WorkflowSDKIdentifier || revision.IRVersion != constants.WorkflowIRVersion || revision.Bundle.Version != constants.WorkflowIRVersion {
		return status_error.WorkflowCapabilityUnsupported.WithField("capability", "workflow_revision_version")
	}
	manifest, err := json.Marshal(revision.Manifest)
	if err != nil {
		return status_error.WorkflowStateInvalid
	}
	program, err := json.Marshal(revision.Bundle)
	if err != nil {
		return status_error.WorkflowStateInvalid
	}
	if revision.SourceDigest != workflowdsl.Digest([]byte(revision.Source)) || revision.ManifestDigest != workflowdsl.Digest(manifest) || revision.IRDigest != workflowdsl.Digest(program) {
		return status_error.WorkflowStateInvalid.WithField("reason", "revision_digest_mismatch")
	}
	return workflowdsl.ValidateInstanceBudget(revision.Bundle, revision.Manifest.Meta.Limits)
}

// ValidateTargetConfiguration 解析不可变目标并校验执行配置，不要求本次参数已提供。
func (s *Service) ValidateTargetConfiguration(ctx context.Context, definitionID, revisionID string, defaults models.WorkflowExecutionDefaults) (models.WorkflowDefinition, models.WorkflowRevision, error) {
	definition, err := s.store.GetDefinition(ctx, definitionID)
	if err != nil {
		return definition, models.WorkflowRevision{}, err
	}
	if revisionID == "" {
		revisionID = definition.CurrentRevisionID
	}
	if revisionID == "" {
		return definition, models.WorkflowRevision{}, status_error.WorkflowStateConflict
	}
	revision, err := s.store.GetRevision(ctx, definition.ID, revisionID)
	if err != nil {
		return definition, revision, err
	}
	if err := verifyRevision(revision); err != nil {
		return definition, revision, err
	}
	if _, err := validateSourcePolicies(revision.Manifest); err != nil {
		return definition, revision, err
	}
	if defaults.PermissionLevel == "" {
		defaults.PermissionLevel = constants.PermissionLevelDefault
	}
	if !constants.SelectablePermissionLevel(defaults.PermissionLevel) {
		return definition, revision, status_error.BadRequest
	}
	if _, err := validateWorkerRetryPolicies(revision.Manifest, &defaults); err != nil {
		return definition, revision, err
	}
	for _, node := range revision.Manifest.Nodes {
		if node.Kind != "worker" {
			continue
		}
		if adapter, ok := s.worker.(interface {
			ValidateAdmission(context.Context, map[string]any, models.WorkflowExecutionDefaults) error
		}); ok {
			if err = adapter.ValidateAdmission(ctx, node.Options, defaults); err != nil {
				return definition, revision, err
			}
		}
	}
	return definition, revision, nil
}

// ReadTarget 只读取不可变工作流定义和版本，供参数说明与契约比较使用。
func (s *Service) ReadTarget(ctx context.Context, definitionID, revisionID string) (models.WorkflowDefinition, models.WorkflowRevision, error) {
	definition, err := s.store.GetDefinition(ctx, definitionID)
	if err != nil {
		return definition, models.WorkflowRevision{}, err
	}
	if revisionID == "" {
		revisionID = definition.CurrentRevisionID
	}
	revision, err := s.store.GetRevision(ctx, definitionID, revisionID)
	return definition, revision, err
}
