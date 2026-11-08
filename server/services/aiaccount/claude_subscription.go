package aiaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	acp "github.com/JieWaZi/acp-go-sdk"
	"github.com/JieWaZi/acp-go/pkg/agentclient"
	"github.com/JieWaZi/acp-go/pkg/claude"
)

type claudeSubscriptionClient interface {
	Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error)
	Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error)
	Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error)
	ReadLoginStatus(context.Context) (claude.LoginStatus, error)
	Close(context.Context) error
}

func (s *Service) claudeOptions(ctx context.Context, account models.AIAccount, home string) (claude.Options, error) {
	binary, args, err := s.accountProgram(ctx, account)
	if err != nil {
		return claude.Options{}, err
	}
	return claude.Options{Executable: binary, PrefixArgs: args, WorkingDirectory: home, Environment: nativeAccountEnvironment(claudeAccountEnvironment(home))}, nil
}

func (s *Service) newClaudeClient(ctx context.Context, options claude.Options) (claudeSubscriptionClient, error) {
	release, err := s.acquireProgramUse(ctx, constants.RuntimeIDClaude)
	if err != nil {
		return nil, err
	}
	var client claudeSubscriptionClient
	if s.claudeFactory != nil {
		client, err = s.claudeFactory(ctx, options)
	} else {
		client, err = claude.NewClient(ctx, options, agentclient.HostCallbacks{})
	}
	if err != nil {
		release()
		return nil, err
	}
	return &programClaudeClient{claudeSubscriptionClient: client, release: release}, nil
}

// programClaudeClient 将程序使用期绑定到同一原生实例的确认关闭。
type programClaudeClient struct {
	// claudeSubscriptionClient 保留全部原生账号能力，不复制认证状态。
	claudeSubscriptionClient
	// release 在原生确认关闭后释放维护围栏计数，失败重试仍归同一实例。
	release func()
}

func (c *programClaudeClient) Close(ctx context.Context) error {
	if err := c.claudeSubscriptionClient.Close(ctx); err != nil {
		return err
	}
	c.release()
	return nil
}

func closeClaudeClient(ctx context.Context, client claudeSubscriptionClient) error {
	closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := client.Close(closing); err != nil {
		return status_error.RuntimeShutdownFailed
	}
	return nil
}

func claudeSubscription(account models.AIAccount) bool {
	if account.AuthMode != constants.AccountAuthModeSubscription {
		return false
	}
	_, ok := accountEndpoint(account, constants.RuntimeIDClaude)
	return ok
}

func (s *Service) startClaudeLogin(ctx context.Context, id string) (Login, error) {
	release, err := s.Exclusive(id)
	if err != nil {
		return Login{}, err
	}
	account, err := s.accounts.Get(ctx, id)
	if err != nil || !claudeSubscription(account) {
		release()
		if err != nil {
			return Login{}, err
		}
		return Login{}, status_error.AIAccountConfigurationInvalid
	}
	view := Login{ID: util.New(), ExpiresAt: time.Now().Add(5 * time.Minute).UnixMilli()}
	home, err := s.managedAccountHome(constants.RuntimeIDClaude, account, true, "login-"+view.ID)
	if err != nil {
		release()
		return Login{}, err
	}
	options, err := s.claudeOptions(ctx, account, home)
	if err != nil {
		release()
		return Login{}, err
	}
	s.loginCommit.Lock()
	s.mutex.Lock()
	if s.ctx == nil || s.ctx.Err() != nil {
		s.mutex.Unlock()
		s.loginCommit.Unlock()
		release()
		return Login{}, status_error.OperationCanceled
	}
	job := &loginJob{release: release, revision: account.Revision, view: view, claudeExit: make(chan struct{})}
	job.ctx, job.cancel = context.WithDeadline(s.ctx, time.UnixMilli(view.ExpiresAt))
	s.jobs[id] = job
	s.workers.Add(1)
	s.mutex.Unlock()
	s.loginCommit.Unlock()
	s.publishLogin(id, view)
	go s.runClaudeLogin(account, home, options, job)
	return view, nil
}

func (s *Service) runClaudeLogin(account models.AIAccount, home string, options claude.Options, job *loginJob) {
	defer s.workers.Done()
	defer close(job.claudeExit)
	defer job.cancel()
	client, err := s.newClaudeClient(job.ctx, options)
	cleanup := func() error {
		if client != nil {
			if err := closeClaudeClient(job.ctx, client); err != nil {
				return err
			}
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(job.ctx), 5*time.Second)
		defer cancel()
		if err := s.claudeCredentials.Remove(cleanupCtx, home); err != nil {
			return err
		}
		if err := os.RemoveAll(home); err != nil {
			return status_error.AIAccountWorkspaceError
		}
		return nil
	}
	job.mutex.Lock()
	job.claudeCleanup = cleanup
	job.mutex.Unlock()
	if err == nil {
		_, err = client.Initialize(job.ctx, acp.InitializeRequest{})
	}
	if err == nil {
		_, err = client.Authenticate(job.ctx, acp.AuthenticateRequest{MethodId: constants.ClaudeSubscriptionAuthMethod})
	}
	var snapshot accountconfig.ClaudeCredentialSnapshot
	if err == nil {
		snapshot, err = s.readClaudeSnapshot(job.ctx, home, client)
	}
	if client != nil {
		if closeErr := closeClaudeClient(job.ctx, client); closeErr != nil {
			err = closeErr
		} else {
			client = nil
		}
	}
	job.mutex.Lock()
	if err == nil {
		err = s.commitClaudeLogin(account.ID, job, snapshot)
	}
	committed := err == nil
	if cleanupErr := cleanup(); cleanupErr != nil {
		logx.Errorf("managed Claude cleanup failed: error_type=%T", cleanupErr)
		err = cleanupErr
	} else {
		job.claudeCleanup = nil
		job.release()
	}
	job.snapshotMutex.Lock()
	job.view.Complete = committed && err == nil
	if err != nil {
		job.view.ErrorCode = loginErrorCode(err)
	}
	view := job.view
	job.snapshotMutex.Unlock()
	job.mutex.Unlock()
	s.publishLogin(account.ID, view)
}

func (s *Service) readClaudeSnapshot(ctx context.Context, home string, client claudeSubscriptionClient) (accountconfig.ClaudeCredentialSnapshot, error) {
	status, err := client.ReadLoginStatus(ctx)
	if err != nil {
		return accountconfig.ClaudeCredentialSnapshot{}, nativeAccountStatusError(ctx, constants.RuntimeIDClaude, err)
	}
	if !status.Authenticated || status.Kind != "subscription" || status.AccountID == "" || status.OrganizationID == "" {
		return accountconfig.ClaudeCredentialSnapshot{}, status_error.RuntimeAuthRequired
	}
	if status.ConfigDirectory != "" && !sameClaudeRoot(status.ConfigDirectory, home) {
		return accountconfig.ClaudeCredentialSnapshot{}, status_error.RuntimeAuthRequired
	}
	snapshot, err := s.claudeCredentials.Read(ctx, home)
	if err != nil {
		return snapshot, err
	}
	snapshot.AccountID, snapshot.OrganizationID = status.AccountID, status.OrganizationID
	if !snapshot.Valid() {
		return snapshot, status_error.AIAccountCredentialError
	}
	return snapshot, nil
}

func (s *Service) commitClaudeLogin(id string, job *loginJob, snapshot accountconfig.ClaudeCredentialSnapshot) error {
	s.loginCommit.Lock()
	defer s.loginCommit.Unlock()
	s.mutex.Lock()
	currentJob := s.jobs[id]
	s.mutex.Unlock()
	if currentJob != job || job.canceled || job.ctx.Err() != nil {
		return status_error.OperationCanceled
	}
	current, err := s.accounts.Get(job.ctx, id)
	if err != nil {
		return err
	}
	if current.Revision != job.revision || !claudeSubscription(current) {
		return status_error.AIAccountRevisionConflict
	}
	if current.HasCredential {
		previous, err := s.claudeStoredCredential(job.ctx, current)
		if err != nil {
			return err
		}
		if previous.AccountID != snapshot.AccountID || previous.OrganizationID != snapshot.OrganizationID {
			return status_error.AIAccountLoginFailed
		}
	}
	accounts, err := s.accounts.ListForRuntime(job.ctx, current.RuntimeID)
	if err != nil {
		return err
	}
	for _, other := range accounts {
		if other.ID == current.ID || !other.HasCredential || !claudeSubscription(other) {
			continue
		}
		saved, err := s.claudeStoredCredential(job.ctx, other)
		if err != nil {
			return err
		}
		if saved.AccountID == snapshot.AccountID && saved.OrganizationID == snapshot.OrganizationID {
			return status_error.AIAccountRevisionConflict
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return status_error.AIAccountCredentialError
	}
	defer clear(encoded)
	if err := s.RemoveClaudeCredentialFiles(job.ctx, current); err != nil {
		return err
	}
	if err := s.accounts.StoreLoginCredential(job.ctx, id, current.Revision, string(encoded)); err != nil {
		return err
	}
	s.InvalidateAccountAuth(job.ctx, current)
	return nil
}

func (s *Service) claudeStoredCredential(ctx context.Context, account models.AIAccount) (accountconfig.ClaudeCredentialSnapshot, error) {
	secret, err := s.accounts.Credential(ctx, account.ID)
	if err != nil {
		return accountconfig.ClaudeCredentialSnapshot{}, err
	}
	var snapshot accountconfig.ClaudeCredentialSnapshot
	if json.Unmarshal([]byte(secret), &snapshot) != nil || !snapshot.Valid() {
		return snapshot, status_error.AIAccountCredentialError
	}
	return snapshot, nil
}

func (s *Service) prepareClaudeSubscription(ctx context.Context, request agent.AccountRequest, account models.AIAccount) (agent.AccountLease, error) {
	unlock, err := s.credentialGate(account.ID).Lock(ctx)
	if err != nil {
		return agent.AccountLease{}, err
	}
	defer unlock()
	snapshot, err := s.claudeStoredCredential(ctx, account)
	if err != nil {
		return agent.AccountLease{}, err
	}
	home, err := s.managedAccountHome(constants.RuntimeIDClaude, account, true)
	if err != nil || home != request.ConfigHome {
		return agent.AccountLease{}, status_error.RuntimeAuthRequired
	}
	// 同一世代的并行消费者共用原生刷新结果，不能把旧数据库投射覆盖到正在使用的目录。
	if _, readErr := s.claudeCredentials.Read(ctx, home); readErr != nil {
		// 不可读的已存在 Keychain/文件必须保留；只有明确缺失才允许初次投射。
		if !errors.Is(readErr, status_error.RuntimeAuthRequired) {
			return agent.AccountLease{}, readErr
		}
		// credentialGate 串行首次物化；共享租约计数也包含正在等待准备的消费者。
		if err := s.claudeCredentials.Write(ctx, home, snapshot); err != nil {
			return agent.AccountLease{}, err
		}
	}
	observation := &claudeLeaseObservation{service: s, home: home, options: claude.Options{Executable: request.Executable, PrefixArgs: request.PrefixArgs, WorkingDirectory: request.CWD, Environment: nativeAccountEnvironment(claudeAccountEnvironment(home))}}
	verify := func(ctx context.Context) error {
		observed, err := observation.read(ctx)
		if err != nil {
			return err
		}
		if observed.AccountID != snapshot.AccountID || observed.OrganizationID != snapshot.OrganizationID {
			return status_error.RuntimeAuthRequired
		}
		return nil
	}
	if err := verify(ctx); err != nil {
		if observation.pending() {
			return agent.AccountLease{Close: observation.retire}, err
		}
		return agent.AccountLease{}, err
	}
	return agent.AccountLease{ConfigHome: home, Environment: claudeAccountEnvironment(home), VerifyLogin: verify, Close: func(ctx context.Context) error {
		if err := observation.close(ctx); err != nil {
			return err
		}
		return s.persistClaudeSubscription(ctx, request, observation.read)
	}}, nil
}

func (s *Service) persistClaudeSubscription(ctx context.Context, request agent.AccountRequest, observe func(context.Context) (accountconfig.ClaudeCredentialSnapshot, error)) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	unlock, err := s.credentialGate(request.AccountID).Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	account, err := s.accounts.Get(ctx, request.AccountID)
	if err != nil {
		return err
	}
	if account.AuthGeneration != request.AuthGeneration {
		// 新世代已成为权威，旧消费者退出时不得覆盖它的凭据。
		return nil
	}
	saved, err := s.claudeStoredCredential(ctx, account)
	if err != nil {
		return err
	}
	current, err := observe(ctx)
	if err != nil {
		return err
	}
	if current.AccountID != saved.AccountID || current.OrganizationID != saved.OrganizationID {
		return status_error.RuntimeAuthRequired
	}
	before, _ := json.Marshal(saved)
	after, _ := json.Marshal(current)
	defer clear(before)
	defer clear(after)
	if bytes.Equal(before, after) {
		return nil
	}
	return s.accounts.StoreRefreshedCredential(ctx, account.ID, account.Revision, request.AuthGeneration, string(after))
}

// RemoveClaudeCredentialFiles 清除旧认证世代的受管投射，调用方必须持有账号独占租约。
func (s *Service) RemoveClaudeCredentialFiles(ctx context.Context, account models.AIAccount) error {
	if !claudeSubscription(account) || s.dataDirectory == "" {
		return nil
	}
	home, err := s.managedAccountHome(constants.RuntimeIDClaude, account, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(home); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return s.claudeCredentials.Remove(ctx, home)
}

// LogoutClaudeSubscription 在调用方持有独占租约时原生退出选定受管账号，不触碰系统账号根。
func (s *Service) LogoutClaudeSubscription(ctx context.Context, account models.AIAccount) error {
	if !claudeSubscription(account) {
		return status_error.AIAccountConfigurationInvalid
	}
	s.mutex.Lock()
	release := s.exclusiveReleases[account.ID]
	exclusive := s.readers[account.ID] < 0
	s.mutex.Unlock()
	if !exclusive || release == nil {
		return status_error.AIAccountBusy
	}
	snapshot, err := s.claudeStoredCredential(ctx, account)
	if err != nil {
		return err
	}
	home, err := s.managedAccountHome(constants.RuntimeIDClaude, account, true)
	if err != nil {
		return err
	}
	if err := s.claudeCredentials.Write(ctx, home, snapshot); err != nil {
		return err
	}
	options, err := s.claudeOptions(ctx, account, home)
	if err != nil {
		return err
	}
	client, err := s.newClaudeClient(ctx, options)
	if err != nil {
		return nativeAccountStatusError(ctx, constants.RuntimeIDClaude, err)
	}
	if _, err = client.Initialize(ctx, acp.InitializeRequest{}); err == nil {
		_, err = client.Logout(ctx, acp.LogoutRequest{})
	}
	if closeErr := closeClaudeClient(ctx, client); closeErr != nil {
		s.retainClaudeLeaseCleanup(account.ID, func(ctx context.Context) error { return closeClaudeClient(ctx, client) }, release)
		return closeErr
	}
	if err != nil {
		return nativeAccountStatusError(ctx, constants.RuntimeIDClaude, err)
	}
	return s.claudeCredentials.Remove(ctx, home)
}

func sameClaudeRoot(first, second string) bool {
	a, errA := filepath.EvalSymlinks(first)
	b, errB := filepath.EvalSymlinks(second)
	return errA == nil && errB == nil && a == b
}

// claudeLeaseObservation 保留原生探测进程及其未确认关闭的同一所有者。
type claudeLeaseObservation struct {
	// mutex 串行验证、回收和失败关闭重试。
	mutex sync.Mutex
	// service 提供原生客户端和受管快照搬运器。
	service *Service
	// home 固定为本租约认证世代的受管目录。
	home string
	// options 冻结本租约原生进程的启动范围。
	options claude.Options
	// client 仅在原生关闭尚未确认时保留。
	client claudeSubscriptionClient
}

func (o *claudeLeaseObservation) closeLocked(ctx context.Context) error {
	if o.client == nil {
		return nil
	}
	if err := closeClaudeClient(ctx, o.client); err != nil {
		return err
	}
	o.client = nil
	return nil
}
func (o *claudeLeaseObservation) close(ctx context.Context) error {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.closeLocked(ctx)
}
func (o *claudeLeaseObservation) pending() bool {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.client != nil
}
func (o *claudeLeaseObservation) retire(ctx context.Context) error { return o.close(ctx) }

func (o *claudeLeaseObservation) read(ctx context.Context) (accountconfig.ClaudeCredentialSnapshot, error) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if err := o.closeLocked(ctx); err != nil {
		return accountconfig.ClaudeCredentialSnapshot{}, err
	}
	client, err := o.service.newClaudeClient(ctx, o.options)
	if err != nil {
		return accountconfig.ClaudeCredentialSnapshot{}, nativeAccountStatusError(ctx, constants.RuntimeIDClaude, err)
	}
	o.client = client
	snapshot, readErr := o.service.readClaudeSnapshot(ctx, o.home, client)
	if err := o.closeLocked(ctx); err != nil {
		// 未关闭的客户端继续由本所有者持有，旧观察绝不能在稍后借用新数据库修订提交。
		clear(snapshot.Credential)
		clear(snapshot.Metadata)
		clear(snapshot.LegacyMetadata)
		return accountconfig.ClaudeCredentialSnapshot{}, err
	}
	return snapshot, readErr
}

// claudeLeaseCleanup 接管准入失败后仍然持有原生客户端的共享租约。
type claudeLeaseCleanup struct {
	// mutex 确保并发重试只清理和释放一次。
	mutex sync.Mutex
	// close 只回收先前的同一个原生客户端，不开启新探测。
	close func(context.Context) error
	// release 在关闭确认后释放原账号租约。
	release func()
	// finished 防止并发清理重复释放。
	finished bool
}

func (s *Service) retainClaudeLeaseCleanup(id string, close func(context.Context) error, release func()) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.claudeLeaseCleanups == nil {
		s.claudeLeaseCleanups = make(map[string][]*claudeLeaseCleanup)
	}
	s.claudeLeaseCleanups[id] = append(s.claudeLeaseCleanups[id], &claudeLeaseCleanup{close: close, release: release})
}
func (s *Service) retryClaudeLeaseCleanups(ctx context.Context, id string) error {
	s.mutex.Lock()
	pending := append([]*claudeLeaseCleanup(nil), s.claudeLeaseCleanups[id]...)
	s.mutex.Unlock()
	for _, owner := range pending {
		owner.mutex.Lock()
		if !owner.finished {
			if err := owner.close(ctx); err != nil {
				owner.mutex.Unlock()
				return err
			}
			owner.release()
			owner.finished = true
			s.mutex.Lock()
			current := s.claudeLeaseCleanups[id]
			for index, item := range current {
				if item == owner {
					current = append(current[:index], current[index+1:]...)
					break
				}
			}
			if len(current) == 0 {
				delete(s.claudeLeaseCleanups, id)
			} else {
				s.claudeLeaseCleanups[id] = current
			}
			s.mutex.Unlock()
		}
		owner.mutex.Unlock()
	}
	return nil
}
