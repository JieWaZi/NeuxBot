package commandbridge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/authorization"
	"sync"
)

// Registry 拥有当前Core中每个插件唯一的授权与占用边界。
type Registry struct {
	// mu 保护创建与关闭。
	mu sync.Mutex
	// root 绑定Core退出。
	root context.Context
	// entries 防止不同版本绕过同一插件的共享身份租约。
	entries map[string]entry
	// changed 只发送资源失效，不发送挑战或凭据。
	changed func(string)
}

// entry 绑定一个已固定入口和唯一共享身份。
type entry struct {
	// digest 固定当前桥接实现。
	digest string
	// auth 复用既有身份生命周期。
	auth *authorization.Authorizer
	// driver 提供组件范围内秘密解析。
	driver *Driver
}

// NewRegistry 不启动进程。
func NewRegistry(ctx context.Context, changed func(string)) *Registry {
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

func (r *Registry) getLocked(id, digest string, config Config, scopes []string) (*authorization.Authorizer, *Driver, error) {
	if previous, ok := r.entries[id]; ok {
		if previous.digest != digest {
			config.Scopes = append([]string{}, scopes...)
			driver := New(config)
			if err := previous.auth.ReplaceDriver(driver); err != nil {
				return nil, nil, err
			}
			previous.digest = digest
			previous.driver = driver
			r.entries[id] = previous
		}
		return previous.auth, previous.driver, nil
	}
	config.Scopes = append([]string{}, scopes...)
	driver := New(config)
	auth := authorization.New(authorization.Config{Driver: driver, Scopes: nil, Changed: func(authorization.Session) {
		if r.changed != nil {
			r.changed(id)
		}
	}})
	auth.Open(r.root)
	r.entries[id] = entry{digest: digest, auth: auth, driver: driver}
	return auth, driver, nil
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
