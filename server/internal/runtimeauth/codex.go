// Package runtimeauth 统一新旧 Runtime 的系统认证与 OAuth 刷新协调。
package runtimeauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// CodexRefreshURLVariable 是原生 CLI 提供的认证刷新端点覆盖。
const CodexRefreshURLVariable = "CODEX_REFRESH_TOKEN_URL_OVERRIDE"

// CodexRefreshBroker 只在 CLI 请求刷新时调用凭据权威，生命周期随 Agent 结束。
type CodexRefreshBroker struct {
	// url 包含随机能力路径，仅传入当前原生进程的环境。
	url string
	// server 只监听 IPv4 回环，不公开模型或账号接口。
	server *http.Server
	// done 等待唯一 HTTP 服务协程结束。
	done chan struct{}
	// mutex 协调请求接纳与关闭屏障，禁止等待开始后登记新请求。
	mutex sync.Mutex
	// closing 标记刷新入口已停止接纳。
	closing bool
	// requests 等待已接纳的刷新完成凭据持久化。
	requests sync.WaitGroup
	// closeOnce 只启动一次关闭屏障，失败的等待可以安全重试。
	closeOnce sync.Once
	// closeDone 在监听与全部刷新请求结束后关闭。
	closeDone chan struct{}
}

// NewCodexRefreshBroker 在调用接纳后建立当前原生进程的刷新入口。
func NewCodexRefreshBroker(refresh codexoauth.RefreshFunc) (*CodexRefreshBroker, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logx.Errorf("listen Codex credential broker failed: error=%v", err)
		return nil, status_error.RuntimeUnavailable
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		_ = listener.Close()
		return nil, status_error.RuntimeUnavailable
	}
	path := "/" + hex.EncodeToString(secret[:])
	broker := &CodexRefreshBroker{url: "http://" + listener.Addr().String() + path, done: make(chan struct{}), closeDone: make(chan struct{})}
	broker.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		broker.mutex.Lock()
		if broker.closing {
			broker.mutex.Unlock()
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		broker.requests.Add(1)
		broker.mutex.Unlock()
		defer broker.requests.Done()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != path || r.Method != http.MethodPost || r.Header.Get("Origin") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var request struct {
			// GrantType 仅接受原生 OAuth 刷新请求。
			GrantType string `json:"grant_type"`
			// RefreshToken 是当前 CLI 持有的旧令牌，不进入日志。
			RefreshToken string `json:"refresh_token"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request) != nil || request.GrantType != "refresh_token" || request.RefreshToken == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tokens, err := refresh(r.Context(), request.RefreshToken)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"credential_refresh_failed"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(tokens)
	})}
	go func() { defer close(broker.done); _ = broker.server.Serve(listener) }()
	return broker, nil
}

// Close 停止刷新接纳并等待已接纳请求；超时不丢弃关闭所有权。
func (b *CodexRefreshBroker) Close(ctx context.Context) error {
	b.closeOnce.Do(func() {
		b.mutex.Lock()
		b.closing = true
		b.mutex.Unlock()
		_ = b.server.Close()
		go func() { <-b.done; b.requests.Wait(); close(b.closeDone) }()
	})
	select {
	case <-b.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// codexFileCredentialState 识别同进程原生进程延迟写入的已轮换令牌。
type codexFileCredentialState struct {
	// gate 保护刷新、快照和原子写回；不覆盖模型调用。
	gate *codexoauth.CredentialGate
	// latest 保存本进程已成功持久化的最新原生认证。
	latest []byte
	// known 只保存已见刷新令牌的摘要，用于区分旧副本和外部新登录。
	known map[[32]byte]struct{}
}

// codexFileCredentialGates 统一系统账号刷新与文本回写的短期文件访问。
var codexFileCredentialGates sync.Map

func codexFileCredentialStateFor(path string) *codexFileCredentialState {
	canonical, _ := filepath.Abs(path)
	if parent, err := filepath.EvalSymlinks(filepath.Dir(canonical)); err == nil {
		canonical = filepath.Join(parent, filepath.Base(canonical))
	}
	state, _ := codexFileCredentialGates.LoadOrStore(canonical, &codexFileCredentialState{gate: codexoauth.NewCredentialGate(), known: make(map[[32]byte]struct{})})
	return state.(*codexFileCredentialState)
}

// CodexFileCredentialGate 复用同一系统认证路径的短期凭据互斥。
func CodexFileCredentialGate(path string) *codexoauth.CredentialGate {
	return codexFileCredentialStateFor(path).gate
}

// ReconcileCodexFileAuth 必须持有文件凭据锁；只修复同身份的已知旧令牌，不恢复已删除的登录。
func ReconcileCodexFileAuth(path string) ([]byte, error) {
	data, err := readCredentialFile(path, 1<<20)
	if err != nil || len(data) == 0 {
		return data, err
	}
	state := codexFileCredentialStateFor(path)
	var disk, latest struct {
		// Tokens 是比较身份和刷新代次需要的最小认证快照。
		Tokens codexoauth.Tokens `json:"tokens"`
	}
	if len(state.latest) == 0 || json.Unmarshal(data, &disk) != nil || json.Unmarshal(state.latest, &latest) != nil || disk.Tokens.RefreshToken == latest.Tokens.RefreshToken {
		return data, nil
	}
	if disk.Tokens.Identity() != latest.Tokens.Identity() || disk.Tokens.AccountID() != latest.Tokens.AccountID() {
		return data, nil
	}
	if _, known := state.known[sha256.Sum256([]byte(disk.Tokens.RefreshToken))]; !known {
		return data, nil
	}
	if err := accountconfig.WriteFile(path, state.latest); err != nil {
		return nil, err
	}
	return state.latest, nil
}

// CodexFileRefresh 以系统原生认证文件为权威；临时副本不自行轮换刷新令牌。
func CodexFileRefresh(path string, client *codexoauth.Client) (codexoauth.RefreshFunc, error) {
	seed, err := readCredentialFile(path, 1<<20)
	if err != nil {
		return nil, err
	}
	var original map[string]json.RawMessage
	if len(seed) == 0 || json.Unmarshal(seed, &original) != nil {
		return nil, nil
	}
	var initial codexoauth.Tokens
	if json.Unmarshal(original["tokens"], &initial) != nil || initial.RefreshToken == "" {
		return nil, nil
	}
	if client == nil {
		client = codexoauth.New()
	}
	return func(ctx context.Context, previous string) (codexoauth.Tokens, error) {
		unlock, err := CodexFileCredentialGate(path).Lock(ctx)
		if err != nil {
			return codexoauth.Tokens{}, err
		}
		defer unlock()
		data, err := ReconcileCodexFileAuth(path)
		if err != nil {
			return codexoauth.Tokens{}, err
		}
		var document map[string]json.RawMessage
		var current codexoauth.Tokens
		if json.Unmarshal(data, &document) != nil || json.Unmarshal(document["tokens"], &current) != nil || !current.Valid() || current.Identity() != initial.Identity() || current.AccountID() != initial.AccountID() {
			return codexoauth.Tokens{}, status_error.RuntimeAuthRequired
		}
		state := codexFileCredentialStateFor(path)
		// 接纳同身份的外部新凭据；不能在下次协调时误认成已知旧副本。
		state.latest = data
		state.known[sha256.Sum256([]byte(current.RefreshToken))] = struct{}{}
		if current.RefreshToken != previous {
			return current, nil
		}
		tokens, err := client.Refresh(ctx, current)
		if err != nil {
			return codexoauth.Tokens{}, err
		}
		if tokens.AccountID() != current.AccountID() {
			return codexoauth.Tokens{}, status_error.RuntimeAuthRequired
		}
		// 保留原生认证的其他字段及 account_id，不改变当前账号身份。
		var native map[string]json.RawMessage
		_ = json.Unmarshal(document["tokens"], &native)
		for key, value := range map[string]string{"access_token": tokens.AccessToken, "refresh_token": tokens.RefreshToken, "id_token": tokens.IDToken} {
			native[key], _ = json.Marshal(value)
		}
		document["tokens"], _ = json.Marshal(native)
		document["last_refresh"], _ = json.Marshal(time.Now().UTC())
		updated, _ := json.Marshal(document)
		if err := accountconfig.WriteFile(path, updated); err != nil {
			return codexoauth.Tokens{}, err
		}
		state.latest = updated
		state.known[sha256.Sum256([]byte(tokens.RefreshToken))] = struct{}{}
		return tokens, nil
	}, nil
}

// CodexAuthPathForEnvironment 定位有效原生 Home 下的系统认证文件。
func CodexAuthPathForEnvironment(environment []string) (string, error) {
	home := ""
	for _, pair := range environment {
		key, value, _ := strings.Cut(pair, "=")
		if key == "CODEX_HOME" {
			home = value
		}
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", status_error.RuntimeConfigurationInvalid
		}
		home = filepath.Join(userHome, ".codex")
	}
	return filepath.Join(home, "auth.json"), nil
}

// readCredentialFile 读取有界普通认证文件，避免沿链接意外读取其他身份。
func readCredentialFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) > limit {
		return nil, status_error.RuntimeConfigurationInvalid
	}
	return data, nil
}

// URL 返回仅供当前 CLI 使用的刷新能力地址。
func (b *CodexRefreshBroker) URL() string { return b.url }
