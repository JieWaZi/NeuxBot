package knowledge

import (
	"context"
	"errors"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
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
	if link.Operation != "wiki.maintenance.chat" || link.ChatID != chatID || link.ResourceID == "" || link.KnowledgeBaseID == "" {
		return nil, status_error.KnowledgeForbidden
	}
	return &link, nil
}

// ValidateWorkIntent 防止普通续聊和原生重试丢弃维护聊天的短期单页工具及只读边界。
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
	if intent.RetryExecutionID != "" || !options.WorkspaceReadOnly ||
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
