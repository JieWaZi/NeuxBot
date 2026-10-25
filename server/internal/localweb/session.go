// Package localweb 管理本机浏览器的会话和静态资源信任边界。
package localweb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const CookieName = "ally_web_session"
const CSRFHeader = "X-Ally-CSRF"

// Session 保存仅属于当前 Core 的会话身份及撤销信号。
type Session struct {
	// ID 是当前浏览器会话的非凭据身份，用于绑定工作区句柄。
	ID string
	// CSRF 是与会话关联的请求证明，只交付给同源页面内存。
	CSRF string
	// ExpiresAt 是会话的绝对到期时间。
	ExpiresAt time.Time
	// Context 在退出、到期或 Core 关闭时取消订阅。
	Context context.Context
	// cancel 撤销当前会话。
	cancel context.CancelFunc
	// timer 在绝对到期时间撤销会话。
	timer *time.Timer
}

// Manager 拥有会话和本机来源白名单。
type Manager struct {
	// mu 保护签发和会话集合。
	mu sync.Mutex
	// host 是 Core 实际监听的完整主机与端口。
	host string
	// origins 保存生产来源及显式开发代理来源。
	origins map[string]bool
	// files 是限制目录逃逸及软链接的静态资源根。
	files *os.Root
	// sessions 以 Cookie 摘要索引当前会话。
	sessions map[[32]byte]*Session
	// now 为到期提供可测试时钟。
	now func() time.Time
}

// New 只接纳完整回环来源，并以受限目录提供已构建资源。
func New(host, directory, developmentOrigin string) (*Manager, error) {
	hostname, port, err := net.SplitHostPort(host)
	number, portErr := strconv.Atoi(port)
	if err != nil || hostname != "127.0.0.1" || portErr != nil || number < 1 || number > 65535 {
		return nil, errors.New("Web 只能使用 127.0.0.1 回环地址")
	}
	origins := map[string]bool{"http://" + host: true}
	if developmentOrigin != "" {
		u, err := url.Parse(developmentOrigin)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("Web 开发来源必须是完整回环 Origin")
		}
		number, err := strconv.Atoi(u.Port())
		if err != nil || number < 1 || number > 65535 {
			return nil, errors.New("Web 开发端口无效")
		}
		origins[developmentOrigin] = true
	}
	var root *os.Root
	if directory != "" {
		var err error
		root, err = os.OpenRoot(directory)
		if err != nil {
			return nil, err
		}
		f, err := root.Open("index.html")
		if err != nil {
			root.Close()
			return nil, err
		}
		f.Close()
	}
	return &Manager{host: host, origins: origins, files: root, sessions: make(map[[32]byte]*Session), now: time.Now}, nil
}

func randomSecret() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// TrustedHost 拒绝 DNS 重绑定及非预期代理 Host。
func (m *Manager) TrustedHost(r *http.Request) bool { return r.Host == m.host }

// TrustedOrigin 精确校验单个 Origin；不接受缺失的浏览器握手来源。
func (m *Manager) TrustedOrigin(r *http.Request) bool {
	return m.TrustedHost(r) && len(r.Header.Values("Origin")) == 1 && m.origins[r.Header.Get("Origin")]
}

// CreateSession 为可信本机页面签发会话 Cookie；响应不包含 Cookie 密钥。
func (m *Manager) CreateSession(w http.ResponseWriter, r *http.Request) (*Session, int) {
	if r.Method != http.MethodPost || !m.TrustedOrigin(r) || r.Header.Get("Authorization") != "" {
		return nil, http.StatusForbidden
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if len(m.sessions) >= 32 {
		return nil, http.StatusTooManyRequests
	}
	token, err := randomSecret()
	if err != nil {
		return nil, http.StatusInternalServerError
	}
	csrf, err := randomSecret()
	if err != nil {
		return nil, http.StatusInternalServerError
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{CSRF: csrf, ExpiresAt: now.Add(12 * time.Hour), Context: ctx, cancel: cancel}
	key := sha256.Sum256([]byte(token))
	s.ID = hex.EncodeToString(key[:])
	m.sessions[key] = s
	s.timer = time.AfterFunc(12*time.Hour, func() { m.revoke(key) })
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60})
	return s, http.StatusOK
}

// Authenticate 同源读取使用 Cookie，写入和 WS 额外校验来源及请求证明。
func (m *Manager) Authenticate(r *http.Request, websocket bool) (*Session, bool) {
	if !m.TrustedHost(r) || r.Header.Get("Authorization") != "" {
		return nil, false
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sha256.Sum256([]byte(cookie.Value))
	s := m.sessions[key]
	if s == nil {
		return nil, false
	}
	if !m.now().Before(s.ExpiresAt) {
		m.revokeLocked(key)
		return nil, false
	}
	if websocket || r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !m.TrustedOrigin(r) {
			return nil, false
		}
		if !websocket && subtle.ConstantTimeCompare([]byte(r.Header.Get(CSRFHeader)), []byte(s.CSRF)) != 1 {
			return nil, false
		}
	}
	return s, true
}

func (m *Manager) revokeLocked(key [32]byte) {
	if s := m.sessions[key]; s != nil {
		s.cancel()
		s.timer.Stop()
		delete(m.sessions, key)
	}
}
func (m *Manager) revoke(key [32]byte) { m.mu.Lock(); defer m.mu.Unlock(); m.revokeLocked(key) }

// Logout 撤销当前 Cookie 及其订阅，不取消已接纳业务执行。
func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie(CookieName); e == nil {
		m.revoke(sha256.Sum256([]byte(c.Value)))
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// Close 回收静态根并撤销全部会话。
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.sessions {
		m.revokeLocked(key)
	}
	if m.files != nil {
		m.files.Close()
	}
}

// ServeStatic 只提供实际构建文件；API、不存在资源及目录不会落到首页。
func (m *Manager) ServeStatic(w http.ResponseWriter, r *http.Request) {
	if !m.TrustedHost(r) {
		http.Error(w, "不受信任的地址", http.StatusForbidden)
		return
	}
	if m.files == nil || r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if strings.Contains(name, "\\") || path.Clean(name) != name || strings.HasPrefix(name, "../") {
		http.NotFound(w, r)
		return
	}
	file, err := m.files.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-cache")
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}
