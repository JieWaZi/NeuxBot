package commandbridge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"sync"
)

// CancelAuthorization 只取消精确旧上下文的已有等待，不因新配置创建或切换驱动。
func (r *Registry) CancelAuthorization(ctx context.Context, id, digest, sessionID string, expected OperationContext) error {
	r.mu.Lock()
	current, ok := r.entries[id]
	if !ok || current.digest != digest || current.driver == nil || current.driver.OperationContext() != expected {
		r.mu.Unlock()
		return status_error.PluginAccountConflict
	}
	auth := current.auth
	r.mu.Unlock()
	return auth.CancelAndWait(ctx, sessionID)
}

// Registry 拥有当前Core中每个插件唯一的授权与占用边界。
type Registry struct {
	// mu 保护创建与关闭。
	mu sync.Mutex
	// root 绑定Core退出。
	root context.Context
	// entries 防止不同版本绕过同一插件的共享身份租约。
	entries map[string]entry
	// changed 区分身份占用与授权资源失效，不发送挑战或凭据。
	changed func(id string, usageOnly bool)
}

// ContextDriver 在唯一共享账号上绑定固定版本和配置事实。
type ContextDriver interface {
	authorization.Driver
	OperationContext() OperationContext
}

// entry 绑定一个已固定入口和唯一共享身份。
type entry struct {
	// digest 固定当前桥接实现。
	digest string
	// auth 复用既有身份生命周期。
	auth *authorization.Authorizer
	// driver 固定包内命令或宿主托管授权。
	driver ContextDriver
}

// NewRegistry 不启动进程。
func NewRegistry(ctx context.Context, changed func(id string, usageOnly bool)) *Registry {
	return &Registry{root: ctx, changed: changed, entries: map[string]entry{}}
}

// Get 复用同一资源的身份，桥接更新不能越过活跃租约。
func (r *Registry) Get(id, digest string, config Config, scopes []string) (*authorization.Authorizer, *Driver, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getLocked(id, digest, config, scopes)
}

// Acquire 原子固定桥接版本和身份租约，避免Get与Acquire之间被其他版本替换。
func (r *Registry) Acquire(id, digest string, config Config, scopes []string, uses ...authorization.Usage) (*authorization.Authorizer, *Driver, func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	auth, driver, err := r.getLocked(id, digest, config, scopes)
	if err != nil {
		return nil, nil, nil, err
	}
	release, err := auth.Acquire(uses...)
	return auth, driver, release, err
}

// AcquireIdentity 只创建进程内身份占用；聊天不会构造或解析包内授权入口。
func (r *Registry) AcquireIdentity(id string, uses ...authorization.Usage) (func(...authorization.Usage), func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.identityLocked(id).AcquireUsage(uses...)
}

// GuardIdleIdentity 排斥真实消费者和授权，允许调用方原子切换全部相关安装。
func (r *Registry) GuardIdleIdentity(id string) (*authorization.ReferenceGuard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.identityLocked(id).GuardIdleIdentity()
}

func (r *Registry) identityLocked(id string) *authorization.Authorizer {
	current, exists := r.entries[id]
	if !exists {
		current.auth = authorization.New(r.authorizationConfig(id, nil))
		current.auth.Open(r.root)
		r.entries[id] = current
	}
	return current.auth
}

func (r *Registry) getLocked(id, digest string, config Config, scopes []string) (*authorization.Authorizer, *Driver, error) {
	expected := OperationContext{PluginID: config.PluginID, VersionID: config.VersionID, Digest: config.Digest, ConfigurationRevision: config.ConfigurationRevision, AccountGeneration: config.AccountGeneration}
	if previous := r.entries[id]; previous.digest == digest {
		if driver, ok := previous.driver.(*Driver); ok && driver.OperationContext() == expected {
			return previous.auth, driver, nil
		}
	}
	config.Scopes = append([]string{}, scopes...)
	driver := New(config)
	auth, err := r.bindLocked(id, digest, driver)
	if err != nil {
		return nil, nil, err
	}
	return auth, driver, nil
}

// GetHosted 将宿主 OAuth 绑定到与包内命令和实际消费者共用的身份所有者。
func (r *Registry) GetHosted(id, digest string, driver ContextDriver) (*authorization.Authorizer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous := r.entries[id]; previous.digest == digest && previous.driver != nil {
		if _, command := previous.driver.(*Driver); !command && previous.driver.OperationContext() == driver.OperationContext() {
			return previous.auth, nil
		}
	}
	return r.bindLocked(id, digest, driver)
}

func (r *Registry) bindLocked(id, digest string, driver ContextDriver) (*authorization.Authorizer, error) {
	current := r.entries[id]
	if current.auth == nil {
		current.auth = authorization.New(r.authorizationConfig(id, driver))
		current.auth.Open(r.root)
	} else if err := current.auth.ReplaceDriver(driver); err != nil {
		return nil, err
	}
	current.digest = digest
	current.driver = driver
	r.entries[id] = current
	return current.auth, nil
}

// IdentityUsages 返回共享身份仍在进行中的工作，不要求进程已经构造某个版本的授权驱动。
func (r *Registry) IdentityUsages(id string) []authorization.Usage {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.entries[id]; ok {
		return current.auth.Usages()
	}
	return []authorization.Usage{}
}

func (r *Registry) authorizationConfig(id string, driver authorization.Driver) authorization.Config {
	return authorization.Config{
		Driver: driver,
		Changed: func(authorization.Session) {
			if r.changed != nil {
				r.changed(id, false)
			}
		},
		UsageChanged: func() {
			if r.changed != nil {
				r.changed(id, true)
			}
		},
	}
}

// CachedAccount 只读取已创建且版本一致的身份摘要，不创建或替换桥接。
func (r *Registry) CachedAccount(id, digest string) authorization.Account {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.entries[id]; ok && current.digest == digest {
		return current.auth.CachedAccount()
	}
	return authorization.Account{Scopes: []string{}}
}

// Close 取消并等待全部真实授权流程。
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.entries {
		entry.auth.Close()
	}
	r.entries = map[string]entry{}
}

// CachedAuthorization 只投影已存在且完整上下文一致的身份和等待状态。
func (r *Registry) CachedAuthorization(id, digest string, context OperationContext) (authorization.Account, authorization.Session, []authorization.Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.entries[id]; ok && current.digest == digest && current.driver != nil && current.driver.OperationContext() == context {
		return current.auth.CachedAccount(), current.auth.Snapshot(), current.auth.Usages()
	}
	return authorization.Account{Scopes: []string{}}, authorization.Session{}, []authorization.Usage{}
}

// CommitPreparation 将安装预检已核验的同一身份推进到成功提交后的代次，不替换驱动或丢弃账号摘要。
// 调用方必须在提交成功后、释放预检身份租约前调用；旧上下文不能推进其他版本或较新的身份。
func (r *Registry) CommitPreparation(expected OperationContext, digest string, revision, generation int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.entries[expected.PluginID]
	if !ok || current.digest != digest || current.driver == nil || revision < expected.ConfigurationRevision || revision > expected.ConfigurationRevision+1 || generation < expected.AccountGeneration || generation > expected.AccountGeneration+1 {
		return
	}
	driver, command := current.driver.(*Driver)
	if !command {
		return
	}
	driver.contextMu.Lock()
	defer driver.contextMu.Unlock()
	actual := OperationContext{PluginID: driver.config.PluginID, VersionID: driver.config.VersionID, Digest: driver.config.Digest, ConfigurationRevision: driver.config.ConfigurationRevision, AccountGeneration: driver.config.AccountGeneration}
	if actual != expected {
		return
	}
	driver.config.ConfigurationRevision = revision
	driver.config.AccountGeneration = generation
}
