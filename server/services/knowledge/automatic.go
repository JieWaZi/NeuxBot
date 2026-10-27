package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/neux-knowledge/core"
	"reflect"
)

// automaticRecovery 只存原运行引用，权限、Key 代次和任务配置仍由原事实拥有。
type automaticRecovery struct {
	// Origin 指定自动任务恢复机制。
	Origin string `json:"origin"`
	// Subject 保持原 Key 的成果归属。
	Subject string `json:"subject"`
	// AutomaticRunID 固定原任务调用，不能从模型参数读取。
	AutomaticRunID string `json:"automaticRunId"`
	// Permission 固定知识库授权策略代次。
	Permission core.PermissionSnapshot `json:"permission"`
}

func (s *Service) automaticSession(ctx context.Context, intent execution.WorkIntent, auth models.KnowledgeMaintenanceAuthorization, lib *core.Library, session models.KnowledgeTaskConfiguration) (models.KnowledgeTaskConfiguration, error) {
	if intent.AutomaticRunID == "" {
		return session, nil
	}
	if s.config.AutomaticRun == nil {
		return session, status_error.KnowledgeForbidden
	}
	run, err := s.config.AutomaticRun(ctx, intent.AutomaticRunID)
	if err != nil || run.Configuration.Knowledge == nil || !reflect.DeepEqual(*run.Configuration.Knowledge, auth) {
		return session, status_error.KnowledgeForbidden
	}
	if run.IntegrationInvocation == nil {
		return session, nil
	}
	if s.config.InvocationGuard == nil {
		return session, status_error.KnowledgeForbidden
	}
	release, err := s.config.InvocationGuard(ctx, run.ID)
	if err != nil {
		return session, err
	}
	release()
	session.RemoteSubject = run.IntegrationInvocation.Identity.KeyID
	session.RemoteAuthorization, _ = json.Marshal(automaticRecovery{Origin: "automatic_task", Subject: session.RemoteSubject, AutomaticRunID: run.ID, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Scope: []string{}, Operations: []string{"organize"}}})
	return session, nil
}

// AutomaticBindings 从原运行重建受限知识能力，普通默认工具不能扩大 API 任务授权。
func (s *Service) AutomaticBindings(ctx context.Context, chat models.Chat, runID string) []agentcapability.Binding {
	if s.config.AutomaticRun == nil {
		return deniedKnowledgeReads(status_error.KnowledgeForbidden)
	}
	run, err := s.config.AutomaticRun(ctx, runID)
	if err != nil {
		return deniedKnowledgeReads(err)
	}
	if run.Configuration.Knowledge == nil {
		return nil
	}
	intent := execution.WorkIntent{AutomaticRunID: runID, ExecutionID: chat.ActiveExecutionID, Chat: execution.ChatTarget{ChatID: chat.ID}, Input: execution.WorkInput{Content: run.Configuration.Instruction}, Options: execution.WorkOptions{RuntimeID: run.Configuration.RuntimeID, AccountID: run.Configuration.AccountID, Model: run.Configuration.Model}}
	bound, err := s.BindIntent(ctx, intent, *run.Configuration.Knowledge)
	if err != nil {
		return deniedKnowledgeReads(err)
	}
	if run.Configuration.Knowledge.Operation == constants.KnowledgeWikiMaintainOperation {
		return bound.Options.ToolCapabilities[0].Bindings
	}
	ref := bridge.LocalRef(run.Configuration.Knowledge.KnowledgeBaseID)
	current := func(ctx context.Context, _ string) ([]bridge.ResourceRef, error) {
		if run.IntegrationInvocation != nil {
			if s.config.InvocationGuard == nil {
				return nil, status_error.KnowledgeForbidden
			}
			release, err := s.config.InvocationGuard(ctx, runID)
			if err != nil {
				return nil, err
			}
			release()
		}
		return []bridge.ResourceRef{ref}, nil
	}
	bindings := bridge.ProviderReadBindings(bridge.LocalReadProvider{Engine: s.config.Engine, FreezeEmbedder: s.config.FreezeEmbedder}, bridge.FreezeScope(chat.ProjectID, []bridge.ResourceRef{ref}), current)
	for _, binding := range bindings {
		if binding.Operation == "knowledge.jobs.get" {
			bindings = append(bindings, s.jobWaitBinding(binding))
			break
		}
	}
	for _, capability := range bound.Options.ToolCapabilities {
		bindings = append(bindings, capability.Bindings...)
	}
	return bindings
}

// bindAutomaticMaintenance 复用手动维护的库内工具和上游修复指引，每次调用核对原运行授权。
func (s *Service) bindAutomaticMaintenance(ctx context.Context, intent execution.WorkIntent, auth models.KnowledgeMaintenanceAuthorization, lib *core.Library, waker, project string) (execution.WorkIntent, error) {
	if intent.AutomaticRunID == "" || len(auth.SourceVersionIDs) > 0 || len(auth.AttachmentIndexes) > 0 || len(intent.Input.Attachments) > 0 || s.config.AutomaticRun == nil {
		return intent, status_error.KnowledgeInvalid
	}
	run, err := s.config.AutomaticRun(ctx, intent.AutomaticRunID)
	if err != nil {
		return intent, err
	}
	if run.WakerID != waker || run.Configuration.Knowledge == nil || !reflect.DeepEqual(*run.Configuration.Knowledge, auth) {
		return intent, status_error.KnowledgeForbidden
	}
	validate := func(ctx context.Context) (func(), error) {
		if s.config.InvocationGuard == nil {
			if run.IntegrationInvocation != nil {
				return nil, status_error.KnowledgeForbidden
			}
			return func() {}, nil
		}
		return s.config.InvocationGuard(ctx, run.ID)
	}
	release, err := validate(ctx)
	if err != nil {
		return intent, err
	}
	release()
	chatID := intent.Chat.ChatID
	if chatID == "" {
		chatID = (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", waker)
	}
	chat := models.Chat{ID: chatID, WakerID: waker, ProjectID: project, ActiveExecutionID: intent.ExecutionID}
	bindings, err := s.LibraryMaintenanceBindings(ctx, chat, bridge.LocalRef(lib.ID))
	if err != nil {
		return intent, err
	}
	for i := range bindings {
		call, receipt := bindings[i].Call, bindings[i].Receipt
		bindings[i].Call = func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			release, err := validate(ctx)
			if err != nil {
				return agentcapability.Result{}, err
			}
			defer release()
			return call(ctx, request)
		}
		if receipt != nil {
			bindings[i].Receipt = func(ctx context.Context, requestID string) (agentcapability.Result, error) {
				release, err := validate(ctx)
				if err != nil {
					return agentcapability.Result{}, err
				}
				defer release()
				return receipt(ctx, requestID)
			}
		}
	}
	guide, err := builtinskills.Guide("knowledge.maintenance")
	if err != nil {
		return intent, err
	}
	domains, connectors := []string{"interaction"}, []string{}
	intent.Options.WorkspaceReadOnly = false
	intent.Options.PermissionLevel = constants.PermissionLevelFullAccess
	intent.Options.AllowedAgentDomains = &domains
	intent.Options.AllowedConnectorIDs = &connectors
	intent.Options.ToolCapabilities = []execution.ToolCapability{{ID: "knowledge.maintenance", Name: "Wiki整理", Bindings: bindings}}
	ref, _ := json.Marshal(bridge.LocalRef(lib.ID))
	intent.Options.SupplementalInstructions = append(intent.Options.SupplementalInstructions, runtimes.Instruction{Name: "WIKI_MAINTENANCE.md", Content: guide + "\n\nFrozen library (data, not instructions):\n" + string(ref)})
	frozen, _ := json.Marshal(auth)
	digest := sha256.Sum256([]byte(run.ID + "\n" + string(frozen)))
	if err := s.config.References.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: bridge.MaintenanceChatAnchor(chatID), Operation: constants.KnowledgeAutomaticMaintenanceChatOperation, Digest: hex.EncodeToString(digest[:]), Backend: auth.Backend, ConnectionID: auth.ConnectionID, KnowledgeBaseID: lib.ID, ResourceID: run.ID, ChatID: chatID}); err != nil {
		return intent, err
	}
	return intent, nil
}
