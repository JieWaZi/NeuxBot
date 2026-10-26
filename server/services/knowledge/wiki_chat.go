package knowledge

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/neux-knowledge/core"
)

// maintenanceChatLink 只读取已有操作锚点，普通聊天不新增维护范围事实。
func (s *Service) maintenanceChatLink(ctx context.Context, chatID string) (*models.KnowledgeOperationLink, error) {
	if chatID == "" {
		return nil, nil
	}
	if s.config.References == nil {
		return nil, status_error.KnowledgeUnsupported
	}
	link, err := s.config.References.GetOperation(ctx, bridge.MaintenanceChatAnchor(chatID))
	if errors.Is(err, status_error.KnowledgeNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if (link.Operation != "wiki.maintenance.chat" && link.Operation != constants.KnowledgeLibraryMaintenanceChatOperation && link.Operation != constants.KnowledgeAutomaticMaintenanceChatOperation) || link.ChatID != chatID || link.ResourceID == "" || link.KnowledgeBaseID == "" || link.Operation == constants.KnowledgeLibraryMaintenanceChatOperation && link.ResourceID != link.KnowledgeBaseID {
		return nil, status_error.KnowledgeForbidden
	}
	return &link, nil
}

// ValidateWorkIntent 防止普通续聊和原生重试丢弃维护聊天的冻结权限与工具范围。
func (s *Service) ValidateWorkIntent(ctx context.Context, intent execution.WorkIntent) error {
	chatID := strings.TrimSpace(intent.Chat.ChatID)
	if intent.Chat.Create != nil {
		identity, err := requestidempotency.New(intent.ClientRequestID, nil)
		if err != nil {
			return err
		}
		chatID = identity.ResourceID("chat", intent.Chat.Create.WakerID)
	}
	link, err := s.maintenanceChatLink(ctx, chatID)
	if err != nil || link == nil {
		return err
	}
	options := intent.Options
	validPermissions := options.WorkspaceReadOnly
	if link.Operation == constants.KnowledgeAutomaticMaintenanceChatOperation {
		validPermissions = !options.WorkspaceReadOnly && options.PermissionLevel == constants.PermissionLevelFullAccess
	}
	if intent.RetryExecutionID != "" || !validPermissions ||
		options.AllowedAgentDomains == nil || len(*options.AllowedAgentDomains) != 1 || (*options.AllowedAgentDomains)[0] != "interaction" ||
		options.AllowedConnectorIDs == nil || len(*options.AllowedConnectorIDs) != 0 ||
		len(options.ToolCapabilities) != 1 || options.ToolCapabilities[0].ID != "knowledge.maintenance" || len(options.ToolCapabilities[0].Bindings) == 0 {
		return status_error.KnowledgeForbidden
	}
	return nil
}

// MaintenanceBindings 为维护入口和普通执行恢复共用同一单页签发规则。
func (s *Service) MaintenanceBindings(ctx context.Context, chat models.Chat, ref bridge.ResourceRef, pageID string) ([]agentcapability.Binding, error) {
	if pageID == "" {
		return nil, status_error.KnowledgeInvalid
	}
	return s.maintenanceBindings(ctx, chat, ref, pageID)
}

// LibraryMaintenanceBindings 只签发当前库的 Wiki 修订和目录维护，不授予原件导入或整理重试。
func (s *Service) LibraryMaintenanceBindings(ctx context.Context, chat models.Chat, ref bridge.ResourceRef) ([]agentcapability.Binding, error) {
	if err := bridge.RequireLocal(ref); err != nil {
		return nil, err
	}
	bindings, err := s.maintenanceBindings(ctx, chat, ref, "")
	if err != nil {
		return nil, err
	}
	filtered := bindings[:0]
	for _, binding := range bindings {
		if binding.Operation != "knowledge.sources.import" && binding.Operation != "knowledge.jobs.retry" {
			filtered = append(filtered, binding)
		}
	}
	lib, err := s.config.Engine.GetLibrary(ctx, ref.KnowledgeBaseID)
	if err != nil {
		return nil, bridge.MapError(err)
	}
	return append(filtered, s.libraryAutoFixBinding(chat, ref, lib.PolicyVersion)), nil
}

// libraryAutoFixBinding 复用核心上游确定性修复，回执与写入都保持原库、执行和策略代次。
func (s *Service) libraryAutoFixBinding(chat models.Chat, ref bridge.ResourceRef, policyVersion int64) agentcapability.Binding {
	authorize := func(ctx context.Context) error {
		lib, err := s.config.Engine.GetLibrary(ctx, ref.KnowledgeBaseID)
		if err != nil {
			return bridge.MapError(err)
		}
		if lib.PolicyVersion != policyVersion {
			return status_error.KnowledgeForbidden
		}
		if err := s.validateTarget(ctx, chat.WakerID, chat.ProjectID, lib); err != nil {
			return err
		}
		current, err := s.config.Chats.GetChatSummary(ctx, chat.ID)
		if err != nil {
			return err
		}
		if current.Chat.ActiveExecutionID != chat.ActiveExecutionID || current.Chat.WakerID != chat.WakerID || current.Chat.ProjectID != chat.ProjectID {
			return status_error.KnowledgeForbidden
		}
		return nil
	}
	identity := func(requestID string) string {
		return (requestidempotency.Identity{ID: requestID}).ResourceID("knowledge-agent", chat.ID, chat.ActiveExecutionID)
	}
	return s.autoFixBinding(ref, policyVersion, authorize, identity, bridge.LocalUserActivity)
}

// autoFixBinding 在聊天或独立知识任务的真实授权下复用同一核心修复。
func (s *Service) autoFixBinding(ref bridge.ResourceRef, policyVersion int64, authorize func(context.Context) error, identity func(string) string, activity func(context.Context) context.Context) agentcapability.Binding {
	return agentcapability.Binding{Operation: "knowledge.wiki.auto-fix", Call: func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
		s.policy.RLock()
		defer s.policy.RUnlock()
		if err := authorize(ctx); err != nil {
			return agentcapability.Result{}, err
		}
		var in wikiCommand
		if err := agentcapability.DecodeParams(request.Params, &in); err != nil {
			return agentcapability.Result{}, err
		}
		if in.ref() != ref {
			return agentcapability.Result{}, status_error.KnowledgeForbidden
		}
		if request.RequestID == "" || in.StructureVersion < 1 {
			return agentcapability.Result{}, status_error.KnowledgeInvalid
		}
		result, err := s.config.Engine.AutoFixLibrary(activity(ctx), core.AutoFixInput{RequestID: identity(request.RequestID), LibraryID: ref.KnowledgeBaseID, StructureVersion: in.StructureVersion, ExpectedPolicyVersion: policyVersion})
		if err != nil {
			return agentcapability.Result{}, bridge.MapError(err)
		}
		if s.config.Events != nil && result.Fixed > 0 {
			_ = events.Emit(s.config.Events, "knowledge", "knowledge.changed", events.Resource{Kind: "knowledge_library", ID: ref.KnowledgeBaseID, ParentID: ref.ConnectionID}, map[string]any{"ref": ref, "requestId": request.RequestID})
		}
		return agentcapability.Result{CommandID: request.RequestID, Data: map[string]any{"ref": ref, "receipt": result.Receipt, "fixed": result.Fixed, "audit": result.Report}}, nil
	}, Receipt: func(ctx context.Context, requestID string) (agentcapability.Result, error) {
		s.policy.RLock()
		defer s.policy.RUnlock()
		if err := authorize(ctx); err != nil {
			return agentcapability.Result{}, err
		}
		receipt, err := s.config.Engine.GetReceipt(ctx, identity(requestID))
		if err != nil {
			return agentcapability.Result{}, bridge.MapError(err)
		}
		if receipt.Operation != "wiki.auto-fix" || receipt.LibraryID != ref.KnowledgeBaseID || receipt.ResourceID != ref.KnowledgeBaseID || receipt.State != "saved" {
			return agentcapability.Result{}, status_error.KnowledgeForbidden
		}
		return agentcapability.Result{CommandID: requestID, Replayed: true, Data: map[string]any{"ref": ref, "receipt": receipt}}, nil
	}}
}

func (s *Service) maintenanceBindings(ctx context.Context, chat models.Chat, ref bridge.ResourceRef, pageID string) ([]agentcapability.Binding, error) {
	provider, err := s.freezeReadProvider(ctx)
	if err != nil {
		return nil, err
	}
	lib, err := provider.Library(ctx, ref)
	if err != nil {
		return nil, err
	}
	if err = s.validateTarget(ctx, chat.WakerID, chat.ProjectID, lib); err != nil {
		return nil, err
	}
	refs := []bridge.ResourceRef{ref}
	current := func(ctx context.Context, _ string) ([]bridge.ResourceRef, error) {
		return s.agentReadResources(ctx, provider)
	}
	bindings := bridge.ProviderReadBindings(provider, bridge.FreezeScope(chat.ProjectID, refs), current)
	for i := range bindings {
		call := bindings[i].Call
		bindings[i].Call = func(ctx context.Context, request agentcapability.Request) (agentcapability.Result, error) {
			s.policy.RLock()
			defer s.policy.RUnlock()
			return call(ctx, request)
		}
	}
	return append(bindings, s.wikiBindings(ctx, chat, refs, provider, pageID)...), nil
}
