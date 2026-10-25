package knowledge

import (
	"context"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
)

// frozenKnowledgeReads 在执行接纳时固定连接代次，不能因连接改址而把旧授权带到新服务器。
type frozenKnowledgeReads struct {
	// Local 仅在完整引用明确为local时使用。
	Local bridge.LocalReadProvider
	// Connections 只读取连接和宿主安全凭据。
	Connections controllers.KnowledgeConnectionController
	// Snapshots 固定执行开始时的已启用连接配置，不保存token。
	Snapshots map[string]models.KnowledgeConnection
}

func (p *frozenKnowledgeReads) remote(ctx context.Context, ref bridge.ResourceRef) (bridge.RemoteReadProvider, error) {
	saved, ok := p.Snapshots[ref.ConnectionID]
	if !ok || saved.Backend != ref.Backend || p.Connections == nil {
		return bridge.RemoteReadProvider{}, status_error.KnowledgeForbidden
	}
	current, token, err := p.Connections.Snapshot(ctx, ref.ConnectionID)
	if err != nil {
		return bridge.RemoteReadProvider{}, err
	}
	if !current.Enabled || current.Revision != saved.Revision || current.Backend != saved.Backend || current.BaseURL != saved.BaseURL {
		return bridge.RemoteReadProvider{}, status_error.KnowledgeForbidden
	}

	options := remote.Options{BaseURL: saved.BaseURL, AuthMode: saved.AuthMode, Token: token}
	out := bridge.RemoteReadProvider{}
	switch ref.Backend {
	case "custom":
		out.Custom, err = remote.NewCustom(options, remote.ResourceRef{Backend: ref.Backend, ConnectionID: ref.ConnectionID, KnowledgeBaseID: ref.KnowledgeBaseID})
	case "weknora":
		out.Official, err = remote.NewWeKnora(options)
	default:
		return out, status_error.KnowledgeUnsupported
	}
	return out, bridge.MapRemoteError(err)
}
func (p *frozenKnowledgeReads) Library(ctx context.Context, ref bridge.ResourceRef) (*core.Library, error) {
	if ref.Backend == "local" {
		return p.Local.Library(ctx, ref)
	}
	remote, err := p.remote(ctx, ref)
	if err != nil {
		return nil, err
	}
	return remote.Library(ctx, ref)
}
func (p *frozenKnowledgeReads) Pages(ctx context.Context, ref bridge.ResourceRef, q core.PageQuery) ([]*core.Page, int64, error) {
	if ref.Backend == "local" {
		return p.Local.Pages(ctx, ref, q)
	}
	remote, err := p.remote(ctx, ref)
	if err != nil {
		return nil, 0, err
	}
	return remote.Pages(ctx, ref, q)
}

// SearchChunks 只在本地资源使用本地索引，远端继续由服务器负责。
func (p *frozenKnowledgeReads) SearchChunks(ctx context.Context, ref bridge.ResourceRef, query string) ([]bridge.DocumentReadHit, string, error) {
	if ref.Backend != "local" {
		return nil, "", nil
	}
	return p.Local.SearchChunks(ctx, ref, query)
}
func (p *frozenKnowledgeReads) Page(ctx context.Context, ref bridge.ResourceRef, id, slug string) (*core.Page, error) {
	if ref.Backend == "local" {
		return p.Local.Page(ctx, ref, id, slug)
	}
	remote, err := p.remote(ctx, ref)
	if err != nil {
		return nil, err
	}
	return remote.Page(ctx, ref, id, slug)
}
func (p *frozenKnowledgeReads) Source(ctx context.Context, ref bridge.ResourceRef, id string) (map[string]any, error) {
	if ref.Backend == "local" {
		return p.Local.Source(ctx, ref, id)
	}
	remote, err := p.remote(ctx, ref)
	if err != nil {
		return nil, err
	}
	return remote.Source(ctx, ref, id)
}

// ExtendedRead 复用同一冻结连接校验，不在远端失败时读取本机同名库。
func (p *frozenKnowledgeReads) ExtendedRead(ctx context.Context, ref bridge.ResourceRef, operation string, params map[string]any) (map[string]any, error) {
	if ref.Backend == "local" {
		return p.Local.ExtendedRead(ctx, ref, operation, params)
	}
	provider, err := p.remote(ctx, ref)
	if err != nil {
		return nil, err
	}
	return provider.ExtendedRead(ctx, ref, operation, params)
}

func (s *Service) freezeReadProvider(ctx context.Context) (*frozenKnowledgeReads, error) {
	out := &frozenKnowledgeReads{Local: bridge.LocalReadProvider{Engine: s.config.Engine, FreezeEmbedder: s.config.FreezeEmbedder}, Connections: s.config.Connections, Snapshots: map[string]models.KnowledgeConnection{}}
	if s.config.Connections == nil {
		return out, nil
	}
	connections, err := s.config.Connections.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, connection := range connections {
		if connection.Enabled {
			out.Snapshots[connection.ID] = connection
		}
	}
	return out, nil
}

// agentReadResources 仅发现提供者明确支持的通用库，不恢复项目或软件库权限。
func (s *Service) agentReadResources(ctx context.Context, provider *frozenKnowledgeReads) ([]bridge.ResourceRef, error) {
	refs, err := s.currentResources(ctx)
	if err != nil {
		return nil, err
	}
	for _, connection := range provider.Snapshots {
		if connection.Backend != "custom" {
			continue
		}
		ref := bridge.ResourceRef{Backend: connection.Backend, ConnectionID: connection.ID}
		client, err := provider.remote(ctx, ref)
		if err != nil {
			return nil, err
		}
		for offset := 0; ; offset += 100 {
			list, err := client.Custom.ListLibraries(ctx, offset, 100)
			if err != nil {
				return nil, bridge.MapRemoteError(err)
			}
			for _, library := range list.Items {
				if library.Kind == "general" && !library.Archived {
					ref.KnowledgeBaseID = library.ID
					if !slices.Contains(refs, ref) {
						refs = append(refs, ref)
					}
				}
			}
			if int64(offset+100) >= list.Total {
				break
			}
		}
	}

	return refs, nil
}

// deniedKnowledgeReads 保留可发现的准确失败，不用空能力或本机空数据冒充远端已读。
func deniedKnowledgeReads(err error) []agentcapability.Binding {
	bindings := []agentcapability.Binding{}
	for _, operation := range []string{"knowledge.libraries.list", "knowledge.pages.search", "knowledge.pages.get", "knowledge.sources.get"} {
		bindings = append(bindings, agentcapability.Binding{Operation: operation, Call: func(context.Context, agentcapability.Request) (agentcapability.Result, error) {
			return agentcapability.Result{}, err
		}})
	}
	return bindings
}
