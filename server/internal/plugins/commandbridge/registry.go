package commandbridge

import (
	"context"
	"slices"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
)

// CancelAuthorization 只取消精确旧上下文的已有等待，不因新配置创建或切换驱动。
func (r *Registry) CancelAuthorization(ctx context.Context, id, digest, sessionID string, expected OperationContext, nonce ...string) error {
	r.mu.Lock()
	current, ok := r.entries[id]
	if !ok || current.digest != digest || current.driver == nil || current.driver.OperationContext() != expected {
		r.mu.Unlock()
		return status_error.PluginAccountConflict
	}
	auth := current.auth
	r.mu.Unlock()
	if len(nonce) > 0 {
		return auth.CancelOwnedAndWait(ctx, sessionID, nonce[0])
	}
	return auth.CancelAndWait(ctx, sessionID)
}

// Registry 拥有当前Core中每个插件唯一的授权与占用边界。
type Registry struct {
	// mu 保护创建与关闭。
	mu sync.Mutex
	// closed 阻止 Core 退出后重新接纳身份。
	closed bool
	// root 绑定Core退出。
	root context.Context
	// entries 防止不同版本绕过同一插件的共享身份租约。
	entries map[string]entry
	// changed 区分身份占用与授权资源失效，不发送挑战或凭据。
	changed func(id string, usageOnly bool)
}

// entry 绑定一个已固定入口和唯一共享身份。
type entry struct {
	// digest 固定当前桥接实现。
	digest string
	// auth 复用既有身份生命周期。
	auth *authorization.Authorizer
	// driver 提供组件范围内秘密解析。
	driver *Driver
	// summaryContext 是最后一次公开摘要可安全投影到的已提交上下文。
	summaryContext OperationContext
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
	if r.closed {
		return nil, nil, status_error.PluginNotReady
	}
	return r.identityLocked(id).auth.AcquireUsage(uses...)
}

// BeginChange 为管理引用保留共享身份，validate 在释放注册表锁后复核实际数据。
func (r *Registry) BeginChange(id string, validate func() error) (*authorization.ReferenceGuard, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, status_error.PluginNotReady
	}
	current, existed := r.entries[id]
	if !existed {
		current = r.identityLocked(id)
	}
	guard, err := current.auth.GuardIdle(nil)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if validate != nil {
		if err := validate(); err != nil {
			if !existed {
				r.Retire(id, guard)
			}
			guard.Close()
			return nil, err
		}
	}
	return guard, nil
}

// AcquireChange 固定确切进程入口，并在同一共享身份上排斥新执行或管理操作。
func (r *Registry) AcquireChange(id, digest string, config Config, scopes []string) (*authorization.Authorizer, *Driver, *authorization.ReferenceGuard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	auth, driver, err := r.getLocked(id, digest, config, scopes)
	if err != nil {
		return nil, nil, nil, err
	}
	guard, err := auth.GuardIdle(nil)
	return auth, driver, guard, err
}

// AcquireRemoval 在同一围栏内接管原等待，并固定已授权的注销入口。
func (r *Registry) AcquireRemoval(id, digest string, config Config, scopes []string) (*Driver, *authorization.ReferenceGuard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	auth, driver, err := r.getLocked(id, digest, config, scopes)
	var logout authorization.Driver
	if err == status_error.PluginBusy {
		// 等待中的驱动只负责取消其自身进程。已持久确认的旧版本负责退出账号。
		auth = r.entries[id].auth
		config.Scopes = slices.Clone(scopes)
		driver = New(config)
		logout = driver
	} else if err != nil {
		return nil, nil, err
	}
	guard, err := auth.GuardReferences(logout)
	return driver, guard, err
}

// GuardDeletion 阻止最终删除期间新建或切换同一身份。
func (r *Registry) GuardDeletion(id string) (*authorization.ReferenceGuard, error) {
	_, guard, err := r.GuardRemoval(id)
	return guard, err
}

// GuardRemoval 在默认无退出入口时接管仍在运行的真实授权。
func (r *Registry) GuardRemoval(id string) (*Driver, *authorization.ReferenceGuard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, nil, status_error.PluginNotReady
	}
	current := r.identityLocked(id)
	guard, err := current.auth.GuardReferences(nil)
	if err != nil {
		return nil, nil, err
	}
	if guard.HasPendingLogin() {
		return current.driver, guard, nil
	}
	return nil, guard, nil
}

// Retire 仅在最终删除提交并仍持有身份围栏时移除同一旧实例。
func (r *Registry) Retire(id string, guard *authorization.ReferenceGuard) {
	r.mu.Lock()
	current, exists := r.entries[id]
	if exists && guard.Retire(current.auth) {
		delete(r.entries, id)
	} else {
		exists = false
	}
	r.mu.Unlock()
	if exists {
		current.auth.Close()
	}
}

// ForgetMissing 清理由删除后的迟到请求临时重建、且没有消费者的身份。
func (r *Registry) ForgetMissing(id string) {
	r.mu.Lock()
	current, exists := r.entries[id]
	if !exists {
		r.mu.Unlock()
		return
	}
	guard, err := current.auth.GuardIdle(nil)
	if err != nil || !guard.Retire(current.auth) {
		r.mu.Unlock()
		return
	}
	delete(r.entries, id)
	r.mu.Unlock()
	guard.Close()
	current.auth.Close()
}

func (r *Registry) identityLocked(id string) entry {
	current, exists := r.entries[id]
	if !exists {
		current.auth = authorization.New(r.authorizationConfig(id, nil))
		current.auth.Open(r.root)
		r.entries[id] = current
	}
	return current
}

func (r *Registry) getLocked(id, digest string, config Config, scopes []string) (*authorization.Authorizer, *Driver, error) {
	if r.closed {
		return nil, nil, status_error.PluginNotReady
	}
	if previous, ok := r.entries[id]; ok {
		if previous.digest != digest || previous.driver == nil || previous.driver.OperationContext() != (OperationContext{PluginID: config.PluginID, VersionID: config.VersionID, Digest: config.Digest, ConfigurationRevision: config.ConfigurationRevision, AccountGeneration: config.AccountGeneration}) {
			config.Scopes = append([]string{}, scopes...)
			driver := New(config)
			preserveSummary := previous.digest == digest && previous.summaryContext == driver.OperationContext()
			if err := previous.auth.SetDriver(driver, preserveSummary); err != nil {
				return nil, nil, err
			}
			previous.digest = digest
			previous.driver = driver
			previous.summaryContext = driver.OperationContext()
			r.entries[id] = previous
		}
		return previous.auth, previous.driver, nil
	}
	config.Scopes = append([]string{}, scopes...)
	driver := New(config)
	auth := authorization.New(r.authorizationConfig(id, driver))
	auth.Open(r.root)
	r.entries[id] = entry{digest: digest, auth: auth, driver: driver, summaryContext: driver.OperationContext()}
	return auth, driver, nil
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
	r.closed = true
	entries := r.entries
	r.entries = nil
	r.mu.Unlock()
	for _, entry := range entries {
		entry.auth.Close()
	}
}

// CachedAuthorization 只投影已存在且完整上下文一致的身份和等待状态。
func (r *Registry) CachedAuthorization(id, digest string, context OperationContext) (authorization.Account, authorization.Session, []authorization.Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.entries[id]; ok && current.digest == digest && current.driver != nil && current.summaryContext == context {
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
	if current.driver.OperationContext() != expected || current.summaryContext != expected {
		return
	}
	current.summaryContext.ConfigurationRevision = revision
	current.summaryContext.AccountGeneration = generation
	r.entries[expected.PluginID] = current
}
