package integration

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxAlly/server/constants"
	"github.com/JieWaZi/NeuxAlly/server/constants/status_error"
	"github.com/JieWaZi/NeuxAlly/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"sync"
)

// KnowledgeRecovery 是服务端生成的持久授权信封，不接受请求正文中的主体声明。
type KnowledgeRecovery struct {
	// Origin 区分直接集成调用与自动任务冻结授权。
	Origin string `json:"origin"`
	// Subject 固定稳定 Key 身份，轮换不改变成果归属。
	Subject string `json:"subject"`
	// Permission 固定核心策略代次与允许的整理范围。
	Permission core.PermissionSnapshot `json:"permission"`
	// Authorization 保存原凭据代次、授权版本和真实来源。
	Authorization models.IntegrationAuthorization `json:"authorization"`
	// Invocation 保存自动任务原调用，直接调用省略。
	Invocation *models.IntegrationInvocation `json:"invocation,omitempty"`
}

// RestoreKnowledgeGuard 只恢复原身份；授权失效不能通过重读新 Key 配置扩权。
func (s *Service) RestoreKnowledgeGuard(ctx context.Context, raw json.RawMessage) (func(), error) {
	return s.RestoreKnowledgeGuards(ctx, []json.RawMessage{raw}, "")
}

// RestoreKnowledgeGuards 在一次授权读锁内核对全部重试约束和原自动运行，禁止嵌套读锁。
func (s *Service) RestoreKnowledgeGuards(ctx context.Context, raws []json.RawMessage, runID string) (func(), error) {
	if s == nil || s.config.Keys == nil || (runID != "" && s.config.Tasks == nil) || (len(raws) == 0 && runID == "") {
		return nil, status_error.IntegrationForbidden
	}
	s.policy.RLock()
	for _, raw := range raws {
		var in KnowledgeRecovery
		if json.Unmarshal(raw, &in) != nil || in.Origin != "direct_api" || in.Invocation != nil || in.Subject != in.Authorization.Identity.KeyID || in.Subject == "" || in.Authorization.ResourceKind != constants.IntegrationResourceKindKnowledge || in.Authorization.ResourceID == "" || len(in.Authorization.Required) != 2 || in.Authorization.Required[0] != constants.IntegrationCapabilityRetrieve || in.Authorization.Required[1] != constants.IntegrationCapabilityOrganize || in.Permission.PolicyVersion < 1 {
			s.policy.RUnlock()
			return nil, status_error.IntegrationForbidden
		}
		if _, err := s.authorize(ctx, in.Authorization.Identity, in.Authorization.ResourceKind, in.Authorization.ResourceID, in.Authorization.Required); err != nil {
			s.policy.RUnlock()
			return nil, err
		}
	}
	if runID != "" {
		return s.acquireInvocationGuardLocked(ctx, runID)
	}
	var once sync.Once
	return func() { once.Do(s.policy.RUnlock) }, nil
}
