// Package oauthrelay 为原生客户端提供按租约授权的回环 Streamable HTTP 连接。
package oauthrelay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/mcpclient/oauth"
)

const (
	leasePath           = "/mcp/"
	maxLeases           = 512
	maxRequestsInFlight = 16
	localReadTimeout    = 30 * time.Second
)

// Options 指定代理对外通信的传输。
type Options struct {
	// Transport 允许测试或上层注入已审核的 HTTPS 传输。默认校验并固定 DNS 解析出的公网地址。
	Transport http.RoundTripper
}

// LeaseOptions 将一个本地连接能力绑定到租约所有者和已验证的 OAuth 资源。
type LeaseOptions struct {
	// Resource 是 OAuth 授权流程为当前账号验证的完整 MCP 资源地址。
	Resource string
	// AccessToken 在每个真实的 MCP 请求开始时获取并按需刷新当前 OAuth 令牌。
	AccessToken func(context.Context) (string, error)
	// RefreshRejected 可在上游明确拒绝当前令牌时续期。实现需按授权及被拒令牌合并并发刷新。
	RefreshRejected func(context.Context, string) (string, error)
	// TTL 限定租约存活时间。零值由原生资源租约关闭时显式撤销。
	TTL time.Duration
}

// Lease 表示私有运行时使用的短期连接。URL 和 Bearer 一起交付给当前原生客户端。
type Lease struct {
	// URL 是本机回环代理的独立 MCP 路径。
	URL string
	// Bearer 是该连接的本地秘密。
	Bearer string
	// revoke 取消当前授权和该租约内正在进行的网络请求。
	revoke func()
}

// Revoke 撤销租约并取消活动请求，可重复执行。
func (l Lease) Revoke() {
	if l.revoke != nil {
		l.revoke()
	}
}

// Relay 持有回环 HTTP 服务和当前会话的连接租约。
type Relay struct {
	// server 拥有回环监听器和本地连接。
	server *http.Server
	// host 限定 DNS rebinding 校验使用的回环主机与端口。
	host string
	// transport 执行向固定资源发出的独立 HTTP 请求。
	transport http.RoundTripper
	// ownedTransport 由代理创建并在关闭时释放空闲连接。
	ownedTransport *http.Transport
	// done 在监听器退出并清理租约后关闭。
	done chan struct{}
	// eventRequests 限制原生客户端同时打开的 GET 事件流数量。
	eventRequests chan struct{}
	// otherRequests 为 POST 和 DELETE 保留独立的请求容量。
	otherRequests chan struct{}
	// mu 保护租约索引和监听状态。
	mu sync.Mutex
	// leases 以独立的随机路径索引活动租约。
	leases map[string]*relayLease
	// closed 阻止服务结束后创建新租约。
	closed bool
}

type relayLease struct {
	// options 保存资源、令牌获取回调和生命周期约束。
	options LeaseOptions
	// bearerDigest 仅保存用于固定长度比较的本地秘密摘要。
	bearerDigest [sha256.Size]byte
	// ctx 在租约撤销、到期或服务关闭时取消。
	ctx context.Context
	// cancel 用于显式收回当前租约。
	cancel context.CancelFunc
}

// Listen 启动回环服务。调用者负责在运行时关闭时释放服务。
func Listen(options Options) (*Relay, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		slog.Warn("mcp oauth relay listen failed", "error", err)
		return nil, status_error.PluginNotReady
	}
	relay := &Relay{
		host:          listener.Addr().String(),
		transport:     options.Transport,
		done:          make(chan struct{}),
		eventRequests: make(chan struct{}, maxRequestsInFlight),
		otherRequests: make(chan struct{}, maxRequestsInFlight),
		leases:        make(map[string]*relayLease),
	}
	if relay.transport == nil {
		relay.ownedTransport = oauth.NewPublicTransport()
		relay.ownedTransport.MaxResponseHeaderBytes = 64 << 10
		// HTTP/1.1 避免 HTTP/2 对无请求体的 DELETE 自动重试。
		relay.ownedTransport.Protocols = new(http.Protocols)
		relay.ownedTransport.Protocols.SetHTTP1(true)
		relay.transport = relay.ownedTransport
	}
	relay.server = &http.Server{
		Handler:           http.HandlerFunc(relay.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       localReadTimeout,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	go func() {
		_ = relay.server.Serve(listener)
		relay.revokeAll()
		close(relay.done)
	}()
	return relay, nil
}

// CreateLease 分配专用于已验证 MCP 资源的私有回环地址和本地访问秘密。
func (r *Relay) CreateLease(options LeaseOptions) (Lease, error) {
	upstream, err := oauth.ParseResource(options.Resource)
	if err != nil {
		return Lease{}, err
	}
	if options.AccessToken == nil || options.TTL < 0 {
		return Lease{}, status_error.PluginNotReady
	}
	options.Resource = upstream.String()
	var secret [56]byte
	rand.Read(secret[:])
	path := leasePath + base64.RawURLEncoding.EncodeToString(secret[:24])
	bearer := base64.RawURLEncoding.EncodeToString(secret[24:])
	var ctx context.Context
	var cancel context.CancelFunc
	if options.TTL == 0 {
		ctx, cancel = context.WithCancel(context.Background())
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), options.TTL)
	}
	entry := &relayLease{options: options, bearerDigest: sha256.Sum256([]byte(bearer)), ctx: ctx, cancel: cancel}

	r.mu.Lock()
	if r.closed || len(r.leases) >= maxLeases || r.leases[path] != nil {
		r.mu.Unlock()
		cancel()
		return Lease{}, status_error.PluginNotReady
	}
	r.leases[path] = entry
	r.mu.Unlock()

	revoke := func() {
		r.mu.Lock()
		if r.leases[path] == entry {
			delete(r.leases, path)
		}
		r.mu.Unlock()
		cancel()
	}
	context.AfterFunc(ctx, revoke)
	return Lease{URL: "http://" + r.host + path, Bearer: bearer, revoke: revoke}, nil
}

// Close 回收全部租约、活动连接和回环监听器。
func (r *Relay) Close() {
	r.revokeAll()
	_ = r.server.Close()
	<-r.done
	if r.ownedTransport != nil {
		r.ownedTransport.CloseIdleConnections()
	}
}

func (r *Relay) revokeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	for _, entry := range r.leases {
		entry.cancel()
	}
	clear(r.leases)
}

func (r *Relay) authorize(request *http.Request) *relayLease {
	if request.Host != r.host || len(request.Header.Values("Origin")) != 0 {
		return nil
	}
	headers := request.Header.Values("Authorization")
	if len(headers) != 1 {
		return nil
	}
	scheme, bearer, found := strings.Cut(headers[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || len(bearer) != base64.RawURLEncoding.EncodedLen(32) {
		return nil
	}
	r.mu.Lock()
	entry := r.leases[request.RequestURI]
	r.mu.Unlock()
	digest := sha256.Sum256([]byte(bearer))
	if entry == nil || subtle.ConstantTimeCompare(digest[:], entry.bearerDigest[:]) != 1 || entry.ctx.Err() != nil {
		return nil
	}
	return entry
}
