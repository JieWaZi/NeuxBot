package knowledge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
)

// ChatLibrary 是当前真实聊天可读的完整知识目标；选择器不签发写权限。
type ChatLibrary struct {
	// 资源引用
	Ref bridge.ResourceRef `json:"ref"`
	// 知识库名称
	Name string `json:"name"`
	// 库类型
	Kind string `json:"kind"`
	// 支持聊天摄入
	DirectIngestSupported bool `json:"directIngestSupported"`
}

// ChatLibraries 复用通用库只读发现；已存在聊天以持久伙伴身份为准。
func (s *Service) ChatLibraries(ctx context.Context, chatID, wakerID string) ([]ChatLibrary, error) {
	if chatID != "" {
		summary, err := s.config.Chats.GetChatSummary(ctx, chatID)
		if err != nil {
			return nil, err
		}
		wakerID = summary.Chat.WakerID
		// 项目不是知识库授权来源，但真实Chat仍必须属于其持久伙伴。
		if summary.Chat.ProjectID != "" {
			project, err := s.config.Projects.Get(ctx, summary.Chat.ProjectID)
			if err != nil {
				return nil, err
			}
			if project.WakerID != wakerID {
				return nil, status_error.KnowledgeForbidden
			}
		}
	}
	if wakerID == "" {
		return nil, status_error.KnowledgeInvalid
	}

	provider, err := s.freezeReadProvider(ctx)
	if err != nil {
		return nil, err
	}
	refs, err := s.agentReadResources(ctx, provider)
	if err != nil {
		return nil, err
	}
	values := []ChatLibrary{}
	for _, ref := range refs {
		lib, err := provider.Library(ctx, ref)
		if err != nil {
			return nil, err
		}
		if !lib.Archived {
			values = append(values, ChatLibrary{Ref: ref, Name: lib.Name, Kind: lib.Kind, DirectIngestSupported: ref.Backend == "local" && ref.ConnectionID == "local"})
		}
	}
	return values, nil
}
