package aiaccount

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// Login 保存前端完成设备授权需要的非秘密信息。
type Login struct {
	// 任务标识
	ID string `json:"id"`
	// 验证地址
	VerificationURL string `json:"verificationUrl"`
	// 设备码
	UserCode string `json:"userCode"`
	// 到期时间
	ExpiresAt int64 `json:"expiresAt"`
	// 轮询间隔
	IntervalMS int64 `json:"intervalMs"`
	// 完成状态
	Complete bool `json:"complete"`
	// 失败代码
	ErrorCode string `json:"errorCode,omitempty"`
}

// loginJob 保存设备授权的内存状态，取消与提交由同一把锁保护。
type loginJob struct {
	// ctx 在用户取消或启动新授权时中止未完成的网络轮询。
	ctx context.Context
	// cancel 终止该次授权任务。
	cancel context.CancelFunc
	// mutex 串行后台授权、快照读取和账号提交。
	mutex sync.Mutex
	// snapshotMutex 让快照读取不等待官方网络请求。
	snapshotMutex sync.Mutex
	// view 是可返回页面的安全状态。
	view Login
	// device 是服务端设备授权信息。
	device codexoauth.Device
	// revision 是开始登录时的账号版本。
	revision int64
	// tokens 暂存已兑换但尚未提交的令牌束。
	tokens *codexoauth.Tokens
	// canceled 阻止被取消的任务保存账号。
	canceled bool
	// kimiExit 在原生授权进程完全退出后关闭，账号删除需等待它再清理密钥目录。
	kimiExit chan struct{}
	// kimiReady 在设备登录地址已解析后允许提交原生进程结果。
	kimiReady chan struct{}
	// release 在授权完成、失败、过期或取消后释放该账号的独占租约。
	release func()
}

// StartLogin 开始独立设备授权，已有登录状态仅在成功提交后替换。
func (s *Service) StartLogin(ctx context.Context, id string) (Login, error) {
	ctx, done, beginErr := s.begin(ctx)
	if beginErr != nil {
		return Login{}, beginErr
	}
	defer done()
	account, err := s.accounts.Get(ctx, id)
	if err != nil {
		return Login{}, err
	}
	if account.AuthMode != constants.AccountAuthModeSubscription {
		return Login{}, status_error.AIAccountConfigurationInvalid
	}
	if len(account.Endpoints) == 1 && account.Endpoints[0].Protocol == constants.AccountProtocolOpenAICompletions && account.Endpoints[0].BaseURL == constants.AIAccountKimiURL {
		return s.startKimiLogin(id, account.Revision)
	}
	if len(account.Endpoints) != 1 || account.Endpoints[0].Protocol != constants.AccountProtocolOpenAIResponses || account.Endpoints[0].BaseURL != constants.AIAccountOpenAIURL {
		return Login{}, status_error.AIAccountConfigurationInvalid
	}
	release, err := s.Exclusive(id)
	if err != nil {
		return Login{}, err
	}
	// 闲置会话回收可能保存原生刷新结果，登录必须固定回收后的账号修订。
	account, err = s.accounts.Get(ctx, id)
	if err != nil {
		release()
		return Login{}, err
	}
	if account.AuthMode != constants.AccountAuthModeSubscription || len(account.Endpoints) != 1 || account.Endpoints[0].Protocol != constants.AccountProtocolOpenAIResponses || account.Endpoints[0].BaseURL != constants.AIAccountOpenAIURL {
		release()
		return Login{}, status_error.AIAccountConfigurationInvalid
	}
	device, err := s.oauth.Start(ctx)
	if err != nil {
		release()
		return Login{}, err
	}
	job := &loginJob{release: release, device: device, revision: account.Revision, view: Login{ID: util.New(), VerificationURL: constants.CodexOAuthVerificationURI, UserCode: device.UserCode, ExpiresAt: time.Now().Add(time.Duration(device.ExpiresIn) * time.Second).UnixMilli(), IntervalMS: device.PollInterval().Milliseconds()}}
	s.loginCommit.Lock()
	s.mutex.Lock()
	if s.ctx == nil || s.ctx.Err() != nil {
		s.mutex.Unlock()
		s.loginCommit.Unlock()
		release()
		return Login{}, status_error.OperationCanceled
	}
	job.ctx, job.cancel = context.WithDeadline(s.ctx, time.UnixMilli(job.view.ExpiresAt))
	s.workers.Add(1)
	previous := s.jobs[id]
	if previous != nil {
		previous.cancel()
	}
	s.jobs[id] = job
	s.mutex.Unlock()
	s.loginCommit.Unlock()
	if previous != nil {
		previous.mutex.Lock()
		previous.canceled = true
		previous.mutex.Unlock()
	}
	initial := job.view
	s.publishLogin(id, initial)
	go s.runCodexLogin(id, job)
	return initial, nil
}

// GetLogin 只读取授权快照；官方协议由 Core 后台作业推进。
func (s *Service) GetLogin(ctx context.Context, id, jobID string) (Login, error) {
	if err := ctx.Err(); err != nil {
		return Login{}, status_error.OperationCanceled
	}
	s.mutex.Lock()
	job := s.jobs[id]
	s.mutex.Unlock()
	if job == nil {
		return Login{}, status_error.AIAccountLoginFailed
	}
	job.snapshotMutex.Lock()
	defer job.snapshotMutex.Unlock()
	if job.view.ID != jobID {
		return Login{}, status_error.AIAccountLoginFailed
	}
	return job.view, nil
}

// runCodexLogin 按官方允许的间隔推进授权，与页面是否打开无关。
func (s *Service) runCodexLogin(id string, job *loginJob) {
	defer s.workers.Done()
	defer job.release()
	defer job.cancel()
	for {
		job.mutex.Lock()
		_, err := s.advanceLogin(job.ctx, id, job)
		job.snapshotMutex.Lock()
		complete := job.view.Complete
		if err != nil && job.view.ErrorCode == "" {
			job.view.ErrorCode = loginErrorCode(err)
		}
		view := job.view
		if err != nil {
			job.tokens = nil
		}
		job.snapshotMutex.Unlock()
		job.mutex.Unlock()
		if err != nil || complete {
			job.release()
			s.publishLogin(id, view)
			return
		}
		timer := time.NewTimer(job.device.PollInterval())
		select {
		case <-job.ctx.Done():
			timer.Stop()
			job.snapshotMutex.Lock()
			if !job.view.Complete && job.view.ErrorCode == "" {
				job.view.ErrorCode = loginErrorCode(job.ctx.Err())
			}
			view := job.view
			job.snapshotMutex.Unlock()
			job.release()
			s.publishLogin(id, view)
			return
		case <-timer.C:
		}
	}
}

func loginErrorCode(err error) string {
	var status status_error.StatusError
	if errors.As(err, &status) {
		return status.Key()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationTimeout.Key()
	}
	if errors.Is(err, context.Canceled) {
		return status_error.OperationCanceled.Key()
	}
	return status_error.AIAccountLoginFailed.Key()
}

func (s *Service) publishLogin(id string, view Login) {
	s.mutex.Lock()
	publisher := s.publisher
	s.mutex.Unlock()
	_ = events.Emit(publisher, "account-login", "account-login.updated", events.Resource{Kind: "account", ID: id, ParentID: view.ID}, struct {
		// Login 是不含令牌和内部设备身份的授权快照。
		Login Login `json:"login"`
	}{Login: view})
}

// advanceLogin 仅供持有账号租约和作业锁的 Core 作业调用。
func (s *Service) advanceLogin(ctx context.Context, id string, job *loginJob) (Login, error) {
	if job.canceled || job.ctx.Err() != nil {
		return Login{}, status_error.AIAccountLoginFailed
	}
	if job.tokens == nil {
		tokens, err := s.oauth.Poll(ctx, job.device)
		if err != nil {
			return Login{}, err
		}
		if tokens == nil {
			return job.view, nil
		}
		job.tokens = tokens
	}
	s.loginCommit.Lock()
	defer s.loginCommit.Unlock()
	s.mutex.Lock()
	currentJob := s.jobs[id]
	s.mutex.Unlock()
	if currentJob != job || ctx.Err() != nil || job.ctx.Err() != nil {
		return Login{}, status_error.AIAccountLoginFailed
	}
	accounts, listErr := s.accounts.List(ctx)
	if listErr != nil {
		return Login{}, listErr
	}
	for _, existing := range accounts {
		if existing.ID == id || existing.AuthMode != constants.AccountAuthModeSubscription || !existing.HasCredential {
			continue
		}
		if _, ok := accountEndpoint(existing, constants.RuntimeIDCodex); !ok {
			continue
		}
		secret, readErr := s.accounts.Credential(ctx, existing.ID)
		if readErr != nil {
			return Login{}, readErr
		}
		var saved codexCredential
		if json.Unmarshal([]byte(secret), &saved) != nil {
			return Login{}, status_error.AIAccountCredentialError
		}
		if saved.Tokens.Identity() == job.tokens.Identity() {
			return Login{}, status_error.AIAccountRevisionConflict
		}
	}
	current, err := s.accounts.Get(ctx, id)
	if err != nil {
		return Login{}, err
	}
	if current.Revision != job.revision {
		return Login{}, status_error.AIAccountRevisionConflict
	}
	// 定向重新登录不能把既有账号替换成另一个用户或 workspace。
	if current.HasCredential {
		encoded, err := s.accounts.Credential(ctx, id)
		if err != nil {
			return Login{}, err
		}
		var previous codexCredential
		if json.Unmarshal([]byte(encoded), &previous) != nil || previous.Tokens.Identity() != job.tokens.Identity() {
			return Login{}, status_error.AIAccountLoginFailed
		}
	}
	credential := codexCredential{Tokens: *job.tokens, LastRefresh: time.Now().UTC()}
	encoded, _ := json.Marshal(credential)
	if err := s.RemoveCodexCredentialFiles(ctx, current); err != nil {
		return Login{}, err
	}
	if err := s.accounts.StoreLoginCredential(ctx, id, current.Revision, string(encoded)); err != nil {
		return Login{}, err
	}
	s.InvalidateCodexAuth(ctx, current)
	job.tokens = nil
	job.snapshotMutex.Lock()
	job.view.Complete = true
	view := job.view
	job.snapshotMutex.Unlock()
	return view, nil
}

// CancelLogin 取消未提交授权，保留原有账号和凭据。
func (s *Service) CancelLogin(id, jobID string) {
	s.loginCommit.Lock()
	s.mutex.Lock()
	job := s.jobs[id]
	s.mutex.Unlock()
	if job != nil && job.view.ID == jobID {
		job.cancel()
	}
	s.loginCommit.Unlock()
	if job == nil {
		return
	}
	job.mutex.Lock()
	defer job.mutex.Unlock()
	job.snapshotMutex.Lock()
	defer job.snapshotMutex.Unlock()
	if job.view.ID == jobID && !job.view.Complete {
		job.canceled = true
		job.tokens = nil
		job.view.ErrorCode = status_error.OperationCanceled.Key()
	}
}

// codexCredential 保存完整令牌及刷新时间，用于原生认证配置。
type codexCredential struct {
	// Tokens 是加密持久化的完整可刷新令牌束。
	Tokens codexoauth.Tokens `json:"tokens"`
	// LastRefresh 是该令牌束最近产生的时间。
	LastRefresh time.Time `json:"last_refresh"`
}

// codexLiveAuth 是 Codex auth.json 原生字段集合。
type codexLiveAuth struct {
	// APIKey 在订阅模式下为空，避免误走 API 计费路径。
	APIKey *string `json:"OPENAI_API_KEY"`
	// Tokens 保存官方 CLI 可以继续刷新的原生令牌字段。
	Tokens codexLiveTokens `json:"tokens"`
	// LastRefresh 是 CLI 使用的 RFC3339 刷新时间。
	LastRefresh time.Time `json:"last_refresh"`
}

// codexLiveTokens 与 Codex 原生 token bundle 保持字段一致。
type codexLiveTokens struct {
	// IDToken 用于识别稳定用户。
	IDToken string `json:"id_token"`
	// AccessToken 用于当前官方请求。
	AccessToken string `json:"access_token"`
	// RefreshToken 用于官方 CLI 后续刷新。
	RefreshToken string `json:"refresh_token"`
	// AccountID 是官方 ChatGPT workspace 标识。
	AccountID string `json:"account_id"`
}

func writeCodexChatGPTCredential(home string, credential codexCredential) error {
	live := struct {
		// Mode fixes file-backed ChatGPT authentication in the native home.
		Mode string `json:"auth_mode"`
		codexLiveAuth
	}{
		Mode: "chatgpt",
		codexLiveAuth: codexLiveAuth{LastRefresh: credential.LastRefresh, Tokens: codexLiveTokens{
			IDToken: credential.Tokens.IDToken, AccessToken: credential.Tokens.AccessToken,
			RefreshToken: credential.Tokens.RefreshToken, AccountID: credential.Tokens.AccountID(),
		}},
	}
	data, _ := json.Marshal(live)
	defer clear(data)
	return accountconfig.WriteFile(filepath.Join(home, "auth.json"), data)
}
