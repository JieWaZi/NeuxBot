package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxAlly/server/constants"
	"github.com/JieWaZi/NeuxAlly/server/constants/status_error"
	"github.com/JieWaZi/NeuxAlly/server/internal/logx"
)

// ListenerStatus 是统一发布服务的实际监听状态。
type ListenerStatus struct {
	// IP 是真实监听的 IP 地址。
	IP string `json:"ip"`
	// Port 是真实监听端口。
	Port int `json:"port"`
	// Available 在共享设置中表示实际绑定，在 API 状态中还要求 API 已启用。
	Available bool `json:"available"`
	// ErrorCode 是端口冲突等安全错误码。
	ErrorCode string `json:"errorCode,omitempty"`
}

// ListenerSettings 是唯一发布服务的安全设置和实际状态。
type ListenerSettings struct {
	// ListenerStatus 是实际监听状态。
	ListenerStatus
	// Enabled 表示允许对外接受 API 请求，不控制定时调度。
	Enabled bool `json:"enabled"`
	// MCPEnabled 表示允许接纳 HTTP MCP 请求，默认关闭。
	MCPEnabled bool `json:"mcpEnabled"`
	// PublicBaseURL 是可复制的完整 API 地址，通配监听不会自行推导访问主机。
	PublicBaseURL string `json:"publicBaseUrl"`
}

// listenerSaved 保存统一发布服务配置。
type listenerSaved struct {
	// IP 是配置的监听地址。
	IP string `json:"ip"`
	// Port 是配置的监听端口。
	Port int `json:"port"`
	// Enabled 是保存的 API 发布启用状态。
	Enabled bool `json:"enabled"`
	// MCPEnabled 保存 MCP 子功能偏好；旧配置缺省关闭，总开关关闭时仍保留。
	MCPEnabled bool `json:"mcpEnabled"`
	// PublicBaseURL 是用户配置的代理或访问地址。
	PublicBaseURL string `json:"publicBaseUrl,omitempty"`
}

// Listener 独立管理监听切换、失败回滚及服务生命周期。
type Listener struct {
	// dataDirectory 保存唯一的集成服务设置文件。
	dataDirectory string
	// handler 在绑定后装配明确发布的路由。
	handler func(string) http.Handler
	// life 保护上下文、实际监听和状态。
	life sync.Mutex
	// portGate 串行配置替换与关闭。
	portGate sync.Mutex
	// ctx 是显式 Start 后的根子上下文。
	ctx context.Context
	// cancel 结束服务请求上下文。
	cancel context.CancelFunc
	// workers 等待所有实际 Serve 退出。
	workers sync.WaitGroup
	// server 是当前实际 HTTP 服务。
	server *http.Server
	// listener 在 Serve 尚未启动时也保证端口可释放。
	listener net.Listener
	// api 是唯一监听状态。
	api ListenerStatus
	// enabled 是发布服务配置，不代表任务执行状态。
	enabled bool
	// mcpEnabled 是受总开关约束的 MCP 子功能偏好，不改变授权或业务状态。
	mcpEnabled bool
	// publicBaseURL 是已验证的访问地址。
	publicBaseURL string
}

// NewListener 创建尚未绑定端口的发布服务所有者。
func NewListener(dataDirectory string, handler func(string) http.Handler) *Listener {
	return &Listener{dataDirectory: dataDirectory, handler: handler, api: ListenerStatus{IP: constants.AutomaticTaskDefaultAPIIP, Port: constants.AutomaticTaskDefaultAPIPort}}
}

// Start 显式加载集成设置并开始监听，绑定失败只影响 API 可用性。
func (s *Listener) Start(ctx context.Context) error {
	s.portGate.Lock()
	defer s.portGate.Unlock()
	s.life.Lock()
	if s.ctx != nil || s.handler == nil {
		s.life.Unlock()
		return status_error.IntegrationListenerError
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	data, err := os.ReadFile(filepath.Join(s.dataDirectory, "integration-api.json"))
	if err == nil {
		var saved listenerSaved
		if json.Unmarshal(data, &saved) != nil || net.ParseIP(saved.IP) == nil || saved.Port < 1 || saved.Port > 65535 {
			s.api.ErrorCode = status_error.AutomaticTaskAPIUnavailable.Key()
			s.life.Unlock()
			return nil
		}
		s.api.IP, s.api.Port = net.ParseIP(saved.IP).String(), saved.Port
		s.enabled, s.mcpEnabled = saved.Enabled, saved.MCPEnabled
		s.publicBaseURL = saved.PublicBaseURL
	} else if !errors.Is(err, os.ErrNotExist) {
		s.api.ErrorCode = status_error.AutomaticTaskAPIUnavailable.Key()
		s.life.Unlock()
		return nil
	}
	s.life.Unlock()
	if s.enabled {
		s.listen()
	}
	return nil
}

func listenAddress(ip string, port int) (net.Listener, error) {
	network := "tcp6"
	if net.ParseIP(ip).To4() != nil {
		network = "tcp4"
	}
	return net.Listen(network, net.JoinHostPort(ip, strconv.Itoa(port)))
}

func (s *Listener) listen() {
	s.life.Lock()
	defer s.life.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil || !s.enabled {
		return
	}
	listener, err := listenAddress(s.api.IP, s.api.Port)
	if err != nil {
		logx.Errorf("listen integration API failed: ip=%s port=%d error=%v", s.api.IP, s.api.Port, err)
		s.api.Available, s.api.ErrorCode = false, status_error.AutomaticTaskAPIUnavailable.Key()
		return
	}
	s.serveLocked(listener)
}

func (s *Listener) serveLocked(listener net.Listener) {
	server := &http.Server{Handler: s.handler(s.api.IP), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return s.ctx }}
	s.server, s.listener = server, listener
	s.api.Available, s.api.ErrorCode = true, ""
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorf("serve integration API failed: error=%v", err)
			s.life.Lock()
			if s.server == server {
				s.api.Available, s.api.ErrorCode = false, status_error.AutomaticTaskAPIUnavailable.Key()
			}
			s.life.Unlock()
		}
	}()
}

// GetAPIStatus 返回总开关允许且实际绑定的发布服务状态。
func (s *Listener) GetAPIStatus() ListenerStatus {
	s.life.Lock()
	defer s.life.Unlock()
	status := s.api
	status.Available = status.Available && s.enabled
	return status
}

// Settings 返回总服务开关、MCP 子功能偏好、实际监听状态及可复制 API 基地址。
func (s *Listener) Settings() ListenerSettings {
	s.life.Lock()
	defer s.life.Unlock()
	base := s.publicBaseURL
	if base == "" && !net.ParseIP(s.api.IP).IsUnspecified() {
		base = "http://" + net.JoinHostPort(s.api.IP, strconv.Itoa(s.api.Port)) + "/api/v1"
	}
	return ListenerSettings{ListenerStatus: s.api, Enabled: s.enabled, MCPEnabled: s.mcpEnabled, PublicBaseURL: base}
}

// SetAPIAddress 保持原管理入口的语义，在同一配置锁内保留两个开关。
func (s *Listener) SetAPIAddress(ctx context.Context, ip string, port int) error {
	s.portGate.Lock()
	defer s.portGate.Unlock()
	s.life.Lock()
	enabled, mcpEnabled, publicURL := s.enabled, s.mcpEnabled, s.publicBaseURL
	s.life.Unlock()
	return s.configureLocked(ctx, ip, port, enabled, mcpEnabled, publicURL)
}

// Configure 保留 MCP 当前开关，只更新 API 设置；所有字段在同一配置锁内读取与替换。
func (s *Listener) Configure(ctx context.Context, ip string, port int, enabled bool, publicURL string) error {
	s.portGate.Lock()
	defer s.portGate.Unlock()
	s.life.Lock()
	mcpEnabled := s.mcpEnabled
	s.life.Unlock()
	return s.configureLocked(ctx, ip, port, enabled, mcpEnabled, publicURL)
}

// SetMCPEnabled 只保存 MCP 子功能偏好，不绕过总开关或覆盖地址和代理配置。
func (s *Listener) SetMCPEnabled(ctx context.Context, enabled bool) error {
	s.portGate.Lock()
	defer s.portGate.Unlock()
	s.life.Lock()
	ip, port, apiEnabled, publicURL := s.api.IP, s.api.Port, s.enabled, s.publicBaseURL
	s.life.Unlock()
	return s.configureLocked(ctx, ip, port, apiEnabled, enabled, publicURL)
}

// configureLocked 在已持有配置锁时绑定并保存完整配置，失败恢复原有效监听。
func (s *Listener) configureLocked(ctx context.Context, ip string, port int, enabled, mcpEnabled bool, publicURL string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsMulticast() || port < 1 || port > 65535 {
		return status_error.BadRequest
	}
	if publicURL != "" {
		u, err := url.Parse(publicURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return status_error.IntegrationInvalid
		}
		publicURL = strings.TrimRight(publicURL, "/")
		if !strings.HasSuffix(publicURL, "/api/v1") {
			publicURL += "/api/v1"
		}
	}
	ip = parsed.String()
	s.life.Lock()
	if ctx.Err() != nil || s.ctx == nil || s.ctx.Err() != nil {
		s.life.Unlock()
		return status_error.OperationCanceled
	}
	old, server, oldListener := s.api, s.server, s.listener
	oldEnabled, oldMCPEnabled := s.enabled, s.mcpEnabled
	active := enabled
	if old.Available && old.IP == ip && old.Port == port && oldEnabled == enabled && oldMCPEnabled == mcpEnabled && s.publicBaseURL == publicURL {
		s.life.Unlock()
		return nil
	}
	s.life.Unlock()
	releasedOld, committed := false, false
	defer func() {
		if releasedOld && !committed {
			s.listen()
		}
	}()
	var listener net.Listener
	if active {
		var err error
		if old.Available && old.IP == ip && old.Port == port {
			listener = oldListener
		} else {
			listener, err = listenAddress(ip, port)
			if err != nil && server != nil && old.Port == port {
				_ = server.Close()
				if oldListener != nil {
					_ = oldListener.Close()
				}
				releasedOld = true
				listener, err = listenAddress(ip, port)
			}
			if err != nil {
				logx.Errorf("configure integration API: ip=%s port=%d error=%v", ip, port, err)
				return status_error.AutomaticTaskAPIUnavailable
			}
		}
		defer func() {
			if !committed && listener != oldListener {
				_ = listener.Close()
			}
		}()
	}
	data, _ := json.Marshal(listenerSaved{IP: ip, Port: port, Enabled: enabled, MCPEnabled: mcpEnabled, PublicBaseURL: publicURL})
	path := filepath.Join(s.dataDirectory, "integration-api.json")
	if err := os.MkdirAll(s.dataDirectory, 0700); err != nil {
		logx.Errorf("create integration API directory: %v", err)
		return status_error.AutomaticTaskAPIUnavailable
	}
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		logx.Errorf("write integration API settings: %v", err)
		return status_error.AutomaticTaskAPIUnavailable
	}
	defer os.Remove(path + ".tmp")
	if ctx.Err() != nil || s.ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		logx.Errorf("replace integration API settings: %v", err)
		return status_error.AutomaticTaskAPIUnavailable
	}
	s.life.Lock()
	s.api.IP, s.api.Port, s.enabled, s.mcpEnabled, s.publicBaseURL = ip, port, enabled, mcpEnabled, publicURL
	if active && listener == oldListener && !releasedOld {
		// 独立开关或访问地址变化继续复用原套接字，处理器逐次读取当前开关。
	} else if active {
		s.serveLocked(listener)
	} else {
		s.api.Available, s.api.ErrorCode, s.server, s.listener = false, "", nil, nil
	}
	s.life.Unlock()
	committed = true
	if server != nil && (!active || listener != oldListener) && !releasedOld {
		shutdown, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = server.Shutdown(shutdown)
		cancel()
		// Shutdown 只释放已被 Serve 接管的监听；未调度的套接字也必须同步关闭。
		if oldListener != nil {
			_ = oldListener.Close()
		}
	}
	return nil
}

// Close 只关闭发布入口，不触碰调度或已接纳执行，可重复调用。
func (s *Listener) Close() {
	s.portGate.Lock()
	defer s.portGate.Unlock()
	s.life.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	server, listener := s.server, s.listener
	s.server, s.listener, s.api.Available = nil, nil, false
	s.life.Unlock()
	if server != nil {
		_ = server.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
	s.workers.Wait()
}
