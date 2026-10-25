package workflow

import (
	"context"
	"encoding/json"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcommand"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// SaveSourceCommand 是源码保存和恢复共同遵守的 CAS 命令。
type SaveSourceCommand struct {
	// CreationExecutionID 是工具所在回合，事务内再次验证，阻止旧回合迟到保存。
	CreationExecutionID string `json:"-"`
	// CreationToken 是只用于事务内隔离校验的能力凭证。
	CreationToken string `json:"-"`
	// DefinitionID 是允许写入的工作流。
	DefinitionID string `json:"-"`
	// RequestID 是同一保存内容的幂等身份。
	RequestID string `json:"requestId"`
	// ExpectedRevisionID 是保存前读取到的当前版本。
	ExpectedRevisionID string `json:"expectedRevisionId"`
	// Source 是不裁剪和不格式化的完整创作源码。
	Source string `json:"source"`
	// SDKVersion 是本次创作使用的原生契约。
	SDKVersion string `json:"sdkVersion"`
	// CreationSessionID 是服务端校验租约后填入的创作身份。
	CreationSessionID string `json:"-"`
	// RestoredFromRevisionID 是恢复操作的来源版本。
	RestoredFromRevisionID string `json:"-"`
	// ResolvedAnnotationIDs 是工具明确声明已经处理的批注身份。
	ResolvedAnnotationIDs []string `json:"resolvedAnnotationIds,omitempty"`
}

// SaveSource 同时提交源码、投影、程序、当前指针、回执与事件。
func (s *Service) SaveSource(ctx context.Context, command SaveSourceCommand) (workflowdsl.SaveReceipt, []workflowdsl.Diagnostic, error) {
	if command.RequestID == "" || len(command.RequestID) > 128 || command.SDKVersion != constants.WorkflowSDKVersion {
		return workflowdsl.SaveReceipt{}, nil, status_error.BadRequest
	}
	digest := workflowstate.Digest(struct {
		// Expected 是创作读取的当前版本。
		Expected string
		// Source 是精确源码。
		Source string
		// SDK 是契约版本。
		SDK string
		// Restored 是恢复来源。
		Restored string
		// Annotations 是本次明确处理的批注。
		Annotations []string
	}{command.ExpectedRevisionID, command.Source, command.SDKVersion, command.RestoredFromRevisionID, command.ResolvedAnnotationIDs})
	// 编译不持有数据库事务；任何诊断都不产生版本或保存成功事件。
	compiled, diagnostics, err := workflowdsl.CompileForAuthoring(command.Source)
	if err != nil {
		return workflowdsl.SaveReceipt{}, diagnostics, err
	}
	if err := s.validateSourcePolicies(ctx, compiled.Manifest); err != nil {
		return workflowdsl.SaveReceipt{}, diagnostics, err
	}
	s.state.Commands.Lock()
	defer s.state.Commands.Unlock()
	var saved workflowdsl.SaveReceipt
	err = s.transactions.Transaction(ctx, func(ctx context.Context) error {
		if command.CreationSessionID != "" {
			if s.creationSessions == nil {
				return status_error.WorkflowCapabilityUnsupported
			}
			session, err := s.creationSessions.GetCreationSession(ctx, command.CreationSessionID)
			if err != nil {
				return err
			}
			if session.DefinitionID != command.DefinitionID || session.ActiveExecutionID != command.CreationExecutionID {
				return status_error.WorkflowAuthoringSuperseded
			}
			if err := s.validateCreationExecution(ctx, session, command.CreationToken); err != nil {
				return err
			}
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
		definition, err := s.store.GetDefinition(ctx, command.DefinitionID)
		if err != nil {
			return err
		}
		if definition.CurrentRevisionID != command.ExpectedRevisionID {
			return status_error.WorkflowRevisionConflict
		}
		if definition.CurrentRevisionID != "" {
			current, err := s.store.GetRevision(ctx, definition.ID, definition.CurrentRevisionID)
			if err != nil {
				return err
			}
			if current.SourceDigest == compiled.SourceDigest && command.RestoredFromRevisionID == "" {
				saved = workflowdsl.SaveReceipt{DefinitionID: definition.ID, RevisionID: current.ID, Ordinal: current.Ordinal, SourceDigest: current.SourceDigest, Unchanged: true}
				if err := s.recordSourceAnnotations(ctx, command, current.ID); err != nil {
					return err
				}
				return s.saveReceipt(ctx, command, digest, saved)
			}
		}
		revision := models.WorkflowRevision{ID: util.New(), DefinitionID: definition.ID, Ordinal: definition.Ordinal + 1, Source: command.Source, SourceDigest: compiled.SourceDigest, ManifestDigest: compiled.ManifestDigest, IRDigest: compiled.IRDigest, SDKVersion: constants.WorkflowSDKVersion, CompilerVersion: constants.WorkflowCompilerVersion, IRVersion: constants.WorkflowIRVersion, Manifest: compiled.Manifest, Bundle: compiled.Bundle, CreationSessionID: command.CreationSessionID, RestoredFromRevisionID: command.RestoredFromRevisionID}
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
		saved = workflowdsl.SaveReceipt{DefinitionID: definition.ID, RevisionID: revision.ID, Ordinal: revision.Ordinal, SourceDigest: revision.SourceDigest}
		if err := s.recordSourceAnnotations(ctx, command, revision.ID); err != nil {
			return err
		}
		if err := s.saveReceipt(ctx, command, digest, saved); err != nil {
			return err
		}
		_, err = s.store.AppendEvent(ctx, models.WorkflowEvent{StreamID: definition.ID, EventID: util.New(), Type: "workflow.revision.saved", EntityID: revision.ID, Payload: map[string]any{"definitionId": definition.ID, "revisionId": revision.ID, "ordinal": revision.Ordinal, "sourceDigest": revision.SourceDigest}})
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
	return agentcommand.Commit(ctx, result)
}

// validateSourcePolicies 在源码提交前核验强制策略及伙伴绑定，避免保存不可运行的限制。
func (s *Service) validateSourcePolicies(ctx context.Context, manifest workflowdsl.Manifest) error {
	for _, node := range manifest.Nodes {
		if node.Kind != "worker" {
			continue
		}
		allowed, err := workerConnectorAllowlist(node.Options)
		if err != nil {
			return err
		}
		if allowed == nil || len(*allowed) == 0 {
			continue
		}
		if s.connectors == nil {
			return status_error.WorkflowResourceInvalid
		}
		resolve, _ := node.Options["resolve"].(map[string]any)
		wakerID, _ := resolve["wakerId"].(string)
		bound, err := s.connectors.ListEnabled(ctx, wakerID)
		if err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, connector := range bound {
			ids[connector.ID] = true
		}
		for _, id := range *allowed {
			if !ids[id] {
				return status_error.WorkflowResourceInvalid.WithField("connectorId", id)
			}
		}
	}
	return nil
}

func verifyRevision(revision models.WorkflowRevision) error {
	if revision.SDKVersion != constants.WorkflowSDKVersion || revision.IRVersion != constants.WorkflowIRVersion || revision.Bundle.Version != constants.WorkflowIRVersion {
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
	if defaults.PermissionLevel == "" {
		defaults.PermissionLevel = constants.PermissionLevelDefault
	}
	if !constants.SelectablePermissionLevel(defaults.PermissionLevel) {
		return definition, revision, status_error.BadRequest
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
