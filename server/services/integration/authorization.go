// Package integration 共享知识、自动任务和恢复入口的集成认证及提交屏障。
package integration

import (
	"context"
	"crypto/subtle"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Config 注入唯一持久授权源和可控时间。
type Config struct {
	// Keys 是知识与任务共同使用的主体存储。
	Keys controllers.IntegrationKeyController
	// Tasks 只读取原运行和当前任务授权，不拥有任务执行。
	Tasks controllers.AutomaticTaskController
	// Now 只用于认证时效和最近使用时间，省略时使用系统时间。
	Now func() time.Time
}

// Service 只缓存提交协调锁，不持有第二份可编辑授权。
type Service struct {
	// config 是存储与时间依赖。
	config Config
	// policy 将授权变更与实际提交排在同一协调边界。
	policy sync.RWMutex
	// touch 串行检查最近使用，避免并发请求重复写入。
	touch sync.Mutex
}

// New 创建不启动后台 goroutine 的认证服务。
func New(config Config) *Service {
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config}
}

// Authenticate 通过摘要唯一索引查找凭据并检查有效期，空凭据拒绝认证。
func (s *Service) Authenticate(ctx context.Context, token string) (models.IntegrationIdentity, error) {
	if s == nil || s.config.Keys == nil || token == "" || len(token) > 16384 || strings.TrimSpace(token) != token {
		return models.IntegrationIdentity{}, status_error.IntegrationUnauthorized
	}
	s.policy.RLock()
	defer s.policy.RUnlock()
	digest := rules.HashKey(token)
	key, err := s.config.Keys.FindByDigest(ctx, digest)
	if err != nil {
		// 未命中也执行同长度比较，不扫描明文或通过前缀查找主体。
		_ = subtle.ConstantTimeCompare([]byte(digest), []byte(strings.Repeat("0", 64)))
		return models.IntegrationIdentity{}, err
	}
	if subtle.ConstantTimeCompare([]byte(digest), []byte(key.KeyHash)) != 1 || !s.active(key) {
		return models.IntegrationIdentity{}, status_error.IntegrationUnauthorized
	}
	// 最近使用时间按一分钟节流并同步写入，不启动额外后台任务。
	// 每次重新读权威时间，让并发到达的同一主体也只写一次；使用失败不否定已确认认证。
	if key.LastUsedAt == nil || s.config.Now().UnixMilli()-*key.LastUsedAt >= time.Minute.Milliseconds() {
		s.touch.Lock()
		current, readErr := s.config.Keys.Get(ctx, key.ID)
		if readErr == nil && (current.LastUsedAt == nil || s.config.Now().UnixMilli()-*current.LastUsedAt >= time.Minute.Milliseconds()) {
			_ = s.config.Keys.TouchLastUsed(ctx, key.ID, s.config.Now().UnixMilli())
		}
		s.touch.Unlock()
	}
	return models.IntegrationIdentity{KeyID: key.ID, CredentialGeneration: key.CredentialGeneration, GrantVersion: key.GrantVersion}, nil
}

func (s *Service) active(key models.IntegrationKey) bool {
	return key.Enabled && key.RevokedAt == nil && (key.ExpiresAt == nil || *key.ExpiresAt > s.config.Now().UnixMilli())
}

func (s *Service) identity(ctx context.Context, identity models.IntegrationIdentity) (models.IntegrationKey, error) {
	if s == nil || s.config.Keys == nil || identity.KeyID == "" {
		return models.IntegrationKey{}, status_error.IntegrationUnauthorized
	}
	key, err := s.config.Keys.Get(ctx, identity.KeyID)
	if err != nil {
		return key, err
	}
	if !s.active(key) || key.CredentialGeneration != identity.CredentialGeneration || key.GrantVersion != identity.GrantVersion {
		return key, status_error.IntegrationUnauthorized
	}
	return key, nil
}

// Authorize 同时检查明确资源与全部必要能力，不能用整理权直接编辑。
func (s *Service) Authorize(ctx context.Context, identity models.IntegrationIdentity, kind constants.IntegrationResourceKind, resourceID string, required []constants.IntegrationCapability) (models.IntegrationAuthorization, error) {
	s.policy.RLock()
	defer s.policy.RUnlock()
	return s.authorize(ctx, identity, kind, resourceID, required)
}

func (s *Service) authorize(ctx context.Context, identity models.IntegrationIdentity, kind constants.IntegrationResourceKind, resourceID string, required []constants.IntegrationCapability) (models.IntegrationAuthorization, error) {
	key, err := s.identity(ctx, identity)
	if err != nil {
		return models.IntegrationAuthorization{}, err
	}
	if !rules.Allows(key.Grants, kind, resourceID, required...) {
		return models.IntegrationAuthorization{}, status_error.IntegrationForbidden
	}
	return models.IntegrationAuthorization{Identity: identity, ResourceKind: kind, ResourceID: resourceID, Required: slices.Clone(required)}, nil
}

// ResourceIDs 返回该能力集合真正获准的明确资源，用于服务端列表过滤与计数。
func (s *Service) ResourceIDs(ctx context.Context, identity models.IntegrationIdentity, kind constants.IntegrationResourceKind, required []constants.IntegrationCapability) ([]string, error) {
	s.policy.RLock()
	defer s.policy.RUnlock()
	key, err := s.identity(ctx, identity)
	if err != nil {
		return nil, err
	}
	for _, grant := range key.Grants {
		if grant.ResourceKind != kind {
			continue
		}
		out := []string{}
		for _, id := range grant.ResourceIDs {
			if rules.Allows(key.Grants, kind, id, required...) {
				out = append(out, id)
			}
		}
		return out, nil
	}
	return []string{}, nil
}

// AcquireGuard 将原授权重检和实际短提交放入读锁，释放后才允许撤权成功返回。
func (s *Service) AcquireGuard(ctx context.Context, auth models.IntegrationAuthorization) (func(), error) {
	s.policy.RLock()
	_, err := s.authorize(ctx, auth.Identity, auth.ResourceKind, auth.ResourceID, auth.Required)
	if err != nil {
		s.policy.RUnlock()
		return nil, err
	}

	var once sync.Once
	return func() { once.Do(s.policy.RUnlock) }, nil
}

// WithAuthorizationWrite 只协调数据库授权变更，不允许回调等待网络或模型。
func (s *Service) WithAuthorizationWrite(ctx context.Context, change func(context.Context) error) error {
	if ctx.Err() != nil {
		return status_error.DatabaseOperationCancelled
	}
	if ctx.Value(authorizationWriteKey{}) == s {
		return change(ctx)
	}
	s.policy.Lock()
	defer s.policy.Unlock()
	if ctx.Err() != nil {
		return status_error.DatabaseOperationCancelled
	}
	return change(context.WithValue(ctx, authorizationWriteKey{}, s))
}

// authorizationWriteKey 仅允许同一服务内部的嵌套数据库协调重入。
type authorizationWriteKey struct{}
