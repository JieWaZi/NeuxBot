// Package knowledge 适配独立知识 SDK 与 Ally 执行协议，不拥有第二套知识状态。
package knowledge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"slices"
)

// ResourceRef 保存提供者原生身份，禁止仅按库 ID 跨连接判断资源。
type ResourceRef struct {
	// Backend 是 local、weknora 或 custom。
	Backend string `json:"backend"`
	// ConnectionID 是本机连接身份。
	ConnectionID string `json:"connectionId"`
	// KnowledgeBaseID 是提供者原生库身份。
	KnowledgeBaseID string `json:"knowledgeBaseId"`
}

// Scope 保存接纳时冻结的资源和操作；当前关系只允许收缩权限。
type Scope struct {
	// ProjectID 是执行实际选中的项目，不能从伙伴拥有项目推断。
	ProjectID string
	// Resources 是冻结资源集合，不随关联变化增长。
	Resources []ResourceRef
	// Operations 是当前明确签发的精确业务操作。
	Operations []string
}

// FreezeScope 复制接纳事实，普通执行默认只读。
func FreezeScope(project string, resources []ResourceRef) Scope {
	return Scope{ProjectID: project, Resources: slices.Clone(resources), Operations: []string{"read", "search", "source"}}
}

// Authorize 同时检查冻结范围与当前关系，系统通用库由调用者显式签入。
func (s Scope) Authorize(ctx context.Context, ref ResourceRef, operation string, current func(context.Context, string) ([]ResourceRef, error)) error {
	if !slices.Contains(s.Resources, ref) || !slices.Contains(s.Operations, operation) {
		return status_error.KnowledgeForbidden
	}
	now, err := current(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if !slices.Contains(now, ref) {
		return status_error.KnowledgeForbidden
	}
	return nil
}

// LocalRef 返回明确本地资源身份。
func LocalRef(id string) ResourceRef {
	return ResourceRef{Backend: "local", ConnectionID: "local", KnowledgeBaseID: id}
}

// RequireLocal 拒绝尚未装配的提供者，不用本地空列表冒充远端数据。
func RequireLocal(ref ResourceRef) error {
	if ref.Backend != "local" || ref.ConnectionID != "local" {
		return status_error.KnowledgeUnsupported
	}
	if ref.KnowledgeBaseID == "" {
		return status_error.KnowledgeInvalid
	}
	return nil
}

// MaintenanceChatAnchor 返回维护聊天不可变范围的稳定引用身份。
func MaintenanceChatAnchor(chatID string) string {
	return (requestidempotency.Identity{ID: "wiki-maintenance"}).ResourceID("chat", chatID)
}
