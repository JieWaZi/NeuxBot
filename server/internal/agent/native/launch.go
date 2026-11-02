package native

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	sdk "github.com/JieWaZi/acp-go/sdk"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LaunchConfig 冻结原生启动事实与已有账号权威的资源端口，不读取业务数据库。
type LaunchConfig struct {
	// Native 是产品已经确认的原生程序、参数与工作目录。
	Native AgentConfig
	// UseNativeAuth 明确由原生认证文件提供系统账号凭据；无刷新回调不隐含系统身份。
	UseNativeAuth bool
	// Refresh 只向既有账号凭据权威请求令牌轮换。
	Refresh codexoauth.RefreshFunc
	// Release 在原生消费者退出及认证对账完成后释放账号与受管目录，失败可重试。
	Release func(context.Context) error
}

// LaunchLease 持有一次原生消费者的启动与认证资源，不拥有原生协议会话。
type LaunchLease struct {
	// native 保存原生启动事实与准备阶段的认证环境。
	native AgentConfig
	// environment 是按最后覆盖值冻结的准备阶段环境，任务资源可在启动前追加授权贡献。
	environment map[string]string
	// broker 是当前消费者独有的刷新入口。
	broker *codexRefreshBroker
	// sourceAuth 是消费者退出后需要对账的系统认证权威文件。
	sourceAuth string
	// release 由产品资源所有者提供，成功之前持续保留。
	release func(context.Context) error
	// gate 串行化创建与关闭并允许调用方取消等待。
	gate *codexoauth.CredentialGate
	// preparationError 阻止部分准备失败后启动原生消费者。
	preparationError error
	// closing 禁止关闭开始后重新创建原生消费者。
	closing bool
	// closed 标记全部资源已成功释放，使后续关闭无需再次接纳上下文。
	closed atomic.Bool
}

// PrepareLaunch 取得当前消费者的认证资源；失败仍交回持有部分资源的可关闭所有者。
func PrepareLaunch(ctx context.Context, config LaunchConfig) (prepared *LaunchLease, err error) {
	values := make(map[string]string, len(config.Native.Environment))
	for _, pair := range config.Native.Environment {
		if key, value, ok := strings.Cut(pair, "="); ok {
			values[key] = value
		}
	}
	// 刷新能力地址只能由当前所有者提供，不能继承已结束消费者的端点。
	delete(values, codexRefreshURLVariable)
	config.Native.PrefixArgs = slices.Clone(config.Native.PrefixArgs)
	config.Native.CodexConfiguration = maps.Clone(config.Native.CodexConfiguration)
	config.Native.NativePluginDirectories = slices.Clone(config.Native.NativePluginDirectories)
	prepared = &LaunchLease{native: config.Native, environment: values, release: config.Release, gate: codexoauth.NewCredentialGate()}
	defer func() {
		prepared.native.Environment = prepared.Environment()
		prepared.preparationError = err
	}()
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	if config.Native.RuntimeID != "codex" {
		return prepared, nil
	}
	refresh := config.Refresh
	if refresh == nil && config.UseNativeAuth {
		path, err := codexAuthPathForEnvironment(prepared.Environment())
		if err != nil {
			return prepared, err
		}
		refresh, err = codexFileRefresh(path, nil)
		if err != nil {
			return prepared, err
		}
		prepared.sourceAuth = path
	}
	if refresh != nil {
		prepared.broker, err = newCodexRefreshBroker(refresh)
		if err != nil {
			return prepared, err
		}
		prepared.environment[codexRefreshURLVariable] = prepared.broker.URL()
	}
	return prepared, nil
}

// New 从同一启动租约创建原生适配器，冻结资源物化后交付的最终授权环境；nil 使用准备快照。
// 调用方先关闭原生消费者，再关闭本资源所有者。
func (p *LaunchLease) New(ctx context.Context, environment map[string]string) (sdk.Agent, error) {
	unlock, err := p.gate.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if p.preparationError != nil {
		return nil, p.preparationError
	}
	if p.closing {
		return nil, fault(FaultPreparation, "launch", "native launch is unavailable", nil)
	}
	values := maps.Clone(environment)
	if values == nil {
		values = maps.Clone(p.environment)
	}
	// 刷新入口始终属于本租约，任务贡献不能替换或继承其他消费者的端点。
	delete(values, codexRefreshURLVariable)
	if p.broker != nil {
		values[codexRefreshURLVariable] = p.broker.URL()
	}
	config := p.native
	config.Environment = orderedEnvironment(values)
	return NewAgent(ctx, config)
}

// Credential 向连接调用方交付准备阶段的独立环境副本，资源贡献在启动前合并后交给 New。
func (p *LaunchLease) Credential() Credential {
	return Credential{Environment: maps.Clone(p.environment)}
}

// Environment 返回按键稳定排序的准备环境副本，不允许调用方修改已冻结事实。
func (p *LaunchLease) Environment() []string {
	return orderedEnvironment(p.environment)
}

func orderedEnvironment(environment map[string]string) []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+environment[key])
	}
	return values
}

// Close 停止刷新并等待持久化，然后对账系统凭据及释放账号；每一失败保留原所有者。
func (p *LaunchLease) Close(ctx context.Context) error {
	if p.closed.Load() {
		return nil
	}
	unlock, err := p.gate.Lock(ctx)
	if err != nil {
		if p.closed.Load() {
			return nil
		}
		return err
	}
	defer unlock()
	p.closing = true
	if p.broker != nil {
		if err := p.broker.Close(ctx); err != nil {
			return err
		}
		p.broker = nil
	}
	if p.sourceAuth != "" {
		unlockFile, err := codexFileCredentialGate(p.sourceAuth).Lock(ctx)
		if err != nil {
			return err
		}
		_, err = reconcileCodexFileAuth(p.sourceAuth)
		unlockFile()
		if err != nil {
			return err
		}
		p.sourceAuth = ""
	}
	if p.release != nil {
		if err := p.release(ctx); err != nil {
			return err
		}
		p.release = nil
	}
	p.closed.Store(true)
	return nil
}

// codexRefreshURLVariable 是原生 CLI 提供的认证刷新端点覆盖。
const codexRefreshURLVariable = "CODEX_REFRESH_TOKEN_URL_OVERRIDE"

// codexRefreshBroker 只在 CLI 请求刷新时调用凭据权威，生命周期随 Agent 结束。
type codexRefreshBroker struct {
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

// newCodexRefreshBroker 在调用接纳后建立当前原生进程的刷新入口。
func newCodexRefreshBroker(refresh codexoauth.RefreshFunc) (*codexRefreshBroker, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logx.Errorf("listen Codex credential broker failed: error=%v", err)
		return nil, fault(FaultPreparation, "launch", "native launch is unavailable", nil)
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		_ = listener.Close()
		return nil, fault(FaultPreparation, "launch", "native launch is unavailable", nil)
	}
	path := "/" + hex.EncodeToString(secret[:])
	broker := &codexRefreshBroker{url: "http://" + listener.Addr().String() + path, done: make(chan struct{}), closeDone: make(chan struct{})}
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
func (b *codexRefreshBroker) Close(ctx context.Context) error {
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

// codexFileCredentialGate 复用同一系统认证路径的短期凭据互斥。
func codexFileCredentialGate(path string) *codexoauth.CredentialGate {
	return codexFileCredentialStateFor(path).gate
}

// reconcileCodexFileAuth 必须持有文件凭据锁；只修复同身份的已知旧令牌，不恢复已删除的登录。
func reconcileCodexFileAuth(path string) ([]byte, error) {
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

// codexFileRefresh 以系统原生认证文件为权威；临时副本不自行轮换刷新令牌。
func codexFileRefresh(path string, client *codexoauth.Client) (codexoauth.RefreshFunc, error) {
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
		unlock, err := codexFileCredentialGate(path).Lock(ctx)
		if err != nil {
			return codexoauth.Tokens{}, err
		}
		defer unlock()
		data, err := reconcileCodexFileAuth(path)
		if err != nil {
			return codexoauth.Tokens{}, err
		}
		var document map[string]json.RawMessage
		var current codexoauth.Tokens
		if json.Unmarshal(data, &document) != nil || json.Unmarshal(document["tokens"], &current) != nil || !current.Valid() || current.Identity() != initial.Identity() || current.AccountID() != initial.AccountID() {
			return codexoauth.Tokens{}, fault(FaultAuthenticationRequired, "credential", "native authentication is required", nil)
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
			return codexoauth.Tokens{}, fault(FaultAuthenticationRequired, "credential", "native authentication is required", nil)
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

// codexAuthPathForEnvironment 定位有效原生 Home 下的系统认证文件。
func codexAuthPathForEnvironment(environment []string) (string, error) {
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
			return "", fault(FaultConfigurationInvalid, "credential", "native credential configuration is invalid", nil)
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
		return nil, fault(FaultConfigurationInvalid, "credential", "native credential configuration is invalid", nil)
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) > limit {
		return nil, fault(FaultConfigurationInvalid, "credential", "native credential configuration is invalid", nil)
	}
	return data, nil
}

// URL 返回仅供当前 CLI 使用的刷新能力地址。
func (b *codexRefreshBroker) URL() string { return b.url }
