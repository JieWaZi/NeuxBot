// Package aiaccount 管理账号使用租约、隔离配置与官方设备授权生命周期。
package aiaccount

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/acp-go/pkg/claude"
)

// Service 复用设置登录、账号修改和执行启动所需的账号生命周期。
type Service struct {
	// programReaders 统计独立认证与账号观察实际持有的原生程序。
	programReaders map[string]int
	// programMaintenance 在程序替换前阻止新的独立原生消费者。
	programMaintenance map[string]bool
	// programCleanupMutex 串行重试已退出业务操作的原生关闭，不持账号状态锁执行 I/O。
	programCleanupMutex sync.Mutex
	// programPendingCloses 保留系统账号观察关闭失败的原始客户端。
	programPendingCloses map[string][]programClientCloser
	// programResolver 从运行环境目录定位实际 CLI，包含受管安装的解释器参数。
	programResolver func(context.Context, string) (string, []string, error)
	// claudeFactory 复用原生客户端，测试以 Fake 隔离浏览器和进程。
	claudeFactory func(context.Context, claude.Options) (claudeSubscriptionClient, error)
	// claudeCredentials 只搬运应用受管目录及其精确 Keychain 项。
	claudeCredentials *accountconfig.ClaudeCredentialStore
	// claudeLeaseCleanups 接管准入失败但原生关闭尚未确认的账号消费者。
	claudeLeaseCleanups map[string][]*claudeLeaseCleanup
	// loginCommit 串行提交订阅身份，避免并发新增同一账号。
	loginCommit sync.Mutex
	// oauth 使用设备码认证协议。
	oauth *codexoauth.Client
	// accounts 读取账号及执行需要的解密凭据。
	accounts controllers.AIAccountController
	// mutex 保护进程生命周期、账号租约和登录任务。
	mutex sync.Mutex
	// readers 统计共享执行租约；负数仅表示配置修改或登录等独占使用。
	readers map[string]int
	// exclusiveReleases 保留独占租约的实际释放器，供失败原生关闭移交同一所有者。
	exclusiveReleases map[string]func()
	// pendingWriters 在独占变更回收闲置消费者时阻止新读租约。
	pendingWriters map[string]bool
	// closeIdleSessions 通过原生会话所有者回收闲置账号租约，不复制执行状态。
	closeIdleSessions func(context.Context, string) error
	// invalidateAccountAuth 在凭据提交后使旧消费者和目录缓存失效。
	invalidateAccountAuth func(context.Context, string, string, uint64)
	// credentials 仅在准备、刷新和回收时串行同账号凭据，不覆盖模型调用。
	credentials sync.Map
	// leaseChanged 在租约释放时唤醒能力查询和后台调用，不轮询账号使用状态。
	leaseChanged chan struct{}
	// jobs 保存每个账号最近一次登录任务的安全状态。
	jobs map[string]*loginJob
	// ctx 绑定 Core 生命周期，关闭时取消全部登录任务。
	ctx context.Context
	// cancel 取消登录任务的根上下文。
	cancel context.CancelFunc
	// workers 等待全部设备认证请求退出。
	workers sync.WaitGroup
	// publisher 交付不含凭据的账号授权进度。
	publisher events.Publisher
	// dataDirectory 是全部受管原生账号目录的根目录。
	dataDirectory string
	// kimiDataDirectory 持久隔离每个 Kimi 订阅账号的原生登录和会话数据。
	kimiDataDirectory string
}

// New 创建尚未启动的账号服务。
func New(accounts controllers.AIAccountController, dataDirectory ...string) *Service {
	service := &Service{oauth: codexoauth.New(), accounts: accounts, readers: map[string]int{}, jobs: map[string]*loginJob{}}
	if len(dataDirectory) > 0 {
		service.dataDirectory = dataDirectory[0]
		service.claudeCredentials = accountconfig.NewClaudeCredentialStore(filepath.Join(dataDirectory[0], "ai-accounts", constants.RuntimeIDClaude), accountconfig.NewClaudeKeychain())
		service.kimiDataDirectory = filepath.Join(dataDirectory[0], "ai-accounts", "kimi")
	}
	return service
}

// SetEventPublisher 注入全局状态事件，不建立新的物理连接。
func (s *Service) SetEventPublisher(publisher events.Publisher) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.publisher = publisher
}

// Start 绑定由 Core 拥有的后台生命周期。
func (s *Service) Start(ctx context.Context) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.ctx, s.cancel = context.WithCancel(ctx)
}

// Close 取消并等待设备认证请求退出。
func (s *Service) Close() {
	s.mutex.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.ctx = nil
	s.mutex.Unlock()
	s.workers.Wait()
	s.mutex.Lock()
	jobs := make([]*loginJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	s.mutex.Unlock()
	for _, job := range jobs {
		job.mutex.Lock()
		if job.claudeCleanup != nil {
			if err := job.claudeCleanup(); err == nil {
				job.claudeCleanup = nil
				job.release()
			}
		}
		job.mutex.Unlock()
	}
	s.mutex.Lock()
	pendingIDs := make([]string, 0, len(s.claudeLeaseCleanups))
	for id := range s.claudeLeaseCleanups {
		pendingIDs = append(pendingIDs, id)
	}
	s.mutex.Unlock()
	for _, id := range pendingIDs {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.retryClaudeLeaseCleanups(ctx, id)
		cancel()
	}
	_ = s.retryProgramCleanups(context.Background(), constants.RuntimeIDClaude)

}

// SetIdleSessionCloser 注入账号变更前的闲置会话回收能力，活动会话由宿主拒绝回收。
func (s *Service) SetIdleSessionCloser(closeIdle func(context.Context, string) error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.closeIdleSessions = closeIdle
}

// Exclusive 为账号设置修改或登录获取独占租约，先回收仍持有共享租约的闲置会话。
func (s *Service) Exclusive(id string) (func(), error) {
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	if err := s.retryClaudeLeaseCleanups(cleanupCtx, id); err != nil {
		return nil, err
	}
	s.mutex.Lock()
	if s.pendingWriters[id] || s.readers[id] < 0 {
		s.mutex.Unlock()
		return nil, status_error.AIAccountBusy
	}
	if s.readers[id] == 0 {
		release := s.grantLeaseLocked(id, true)
		s.mutex.Unlock()
		return release, nil
	}
	closeIdle := s.closeIdleSessions
	if closeIdle == nil {
		s.mutex.Unlock()
		return nil, status_error.AIAccountBusy
	}
	if s.pendingWriters == nil {
		s.pendingWriters = make(map[string]bool)
	}
	s.pendingWriters[id] = true
	s.mutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := closeIdle(ctx, id)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.pendingWriters, id)
	s.notifyLeaseChangedLocked()
	if err != nil {
		return nil, err
	}
	if s.readers[id] != 0 {
		return nil, status_error.AIAccountBusy
	}
	return s.grantLeaseLocked(id, true), nil
}

func (s *Service) acquire(id string, exclusive bool) (func(), error) {
	return s.acquireLease(context.Background(), id, exclusive, false)
}

func (s *Service) acquireLease(ctx context.Context, id string, exclusive, wait bool) (func(), error) {
	for {
		s.mutex.Lock()
		if err := ctx.Err(); err != nil {
			s.mutex.Unlock()
			return nil, err
		}
		current := s.readers[id]
		if !s.pendingWriters[id] && current >= 0 && (!exclusive || current == 0) {
			break
		}
		if !wait {
			s.mutex.Unlock()
			return nil, status_error.AIAccountBusy
		}
		if s.leaseChanged == nil {
			s.leaseChanged = make(chan struct{})
		}
		changed := s.leaseChanged
		s.mutex.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
	defer s.mutex.Unlock()
	return s.grantLeaseLocked(id, exclusive), nil
}

func (s *Service) grantLeaseLocked(id string, exclusive bool) func() {
	if exclusive {
		s.readers[id] = -1
	} else {
		s.readers[id]++
	}
	var once sync.Once
	actualRelease := func() {
		once.Do(func() {
			s.mutex.Lock()
			defer s.mutex.Unlock()
			if s.readers[id] < 0 {
				delete(s.readers, id)
				delete(s.exclusiveReleases, id)
			} else {
				s.readers[id]--
				if s.readers[id] == 0 {
					delete(s.readers, id)
				}
			}
			s.notifyLeaseChangedLocked()
		})
	}
	if exclusive {
		if s.exclusiveReleases == nil {
			s.exclusiveReleases = make(map[string]func())
		}
		s.exclusiveReleases[id] = actualRelease
		return func() {
			s.mutex.Lock()
			retained := len(s.claudeLeaseCleanups[id]) > 0
			s.mutex.Unlock()
			if !retained {
				actualRelease()
			}
		}
	}
	return actualRelease
}

func (s *Service) notifyLeaseChangedLocked() {
	if s.leaseChanged != nil {
		close(s.leaseChanged)
		s.leaseChanged = nil
	}
}

// Get 返回账号模型选择与协议配置，供执行入口校验当前选择。
func (s *Service) Get(ctx context.Context, id string) (models.AIAccount, error) {
	return s.accounts.Get(ctx, id)
}

// ValidateRuntimeAccount 校验实际智能体归属及其原生协议，不要求账号已经登录。
func ValidateRuntimeAccount(account models.AIAccount, environment models.RuntimeEnvironment) error {
	if account.RuntimeID == "" || account.RuntimeID != environment.RuntimeID {
		return status_error.AIAccountConfigurationInvalid
	}
	if _, ok := accountEndpoint(account, environment.BaseRuntimeID); !ok {
		return status_error.AIAccountConfigurationInvalid
	}
	return nil
}

// RuntimeSelectionDefaults 只从所选账号及其绑定环境补齐未指定的模型与推理强度。
func RuntimeSelectionDefaults(account models.AIAccount, environment models.RuntimeEnvironment, model, reasoning string) (string, string) {
	if model == "" {
		candidates := []string{}
		if account.ID == environment.DefaultAccountID {
			candidates = append(candidates, environment.DefaultModel)
		}
		candidates = append(candidates, account.DefaultModel)
		if account.AuthMode == constants.AccountAuthModeAPIKey {
			candidates = append(candidates, account.SelectedModels...)
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			allowed := account.AuthMode != constants.AccountAuthModeAPIKey || slices.Contains(account.SelectedModels, candidate)
			if account.AuthMode == constants.AccountAuthModeAPIKey && environment.BaseRuntimeID == constants.RuntimeIDClaude {
				_, allowed = ManagedClaudeModelTarget(account, candidate)
			}
			if allowed {
				model = candidate
				break
			}
		}
	}
	if reasoning == "" {
		if account.ID == environment.DefaultAccountID && model == environment.DefaultModel {
			reasoning = environment.DefaultReasoning
		}
		if reasoning == "" && model == account.DefaultModel {
			reasoning = account.DefaultReasoning
		}
	}
	return model, reasoning
}

// ValidateSelection 在保存默认配置与执行准备前复用账号协议、凭据和模型校验。
func (s *Service) ValidateSelection(ctx context.Context, id, runtimeID, model, reasoning string) (models.AIAccount, error) {
	account, err := s.accounts.Get(ctx, id)
	if err != nil {
		return account, err
	}
	if _, ok := accountEndpoint(account, runtimeID); !ok {
		return account, status_error.AIAccountConfigurationInvalid
	}
	if !account.HasCredential {
		return account, status_error.RuntimeAuthRequired
	}
	if account.AuthMode == constants.AccountAuthModeAPIKey {
		if len(account.SelectedModels) == 0 {
			return account, status_error.AIAccountConfigurationInvalid
		}
		declaredModel := model
		if model != "" {
			allowed := slices.Contains(account.SelectedModels, model)
			if runtimeID == constants.RuntimeIDClaude {
				declaredModel, allowed = ManagedClaudeModelTarget(account, model)
			}
			if !allowed {
				return account, status_error.BadRequest
			}
		}
		if levels := account.ModelReasoningLevels[declaredModel]; !accountconfig.IsNativeAccountRuntime(runtimeID) && len(levels) > 0 && reasoning != "" {
			allowed := false
			for _, level := range levels {
				allowed = allowed || level == reasoning
			}
			if !allowed {
				return account, status_error.BadRequest
			}
		}
	}
	return account, nil
}

func (s *Service) credentialGate(id string) *codexoauth.CredentialGate {
	gate, _ := s.credentials.LoadOrStore(id, codexoauth.NewCredentialGate())
	return gate.(*codexoauth.CredentialGate)
}

// accountEndpoint 只使用账号的唯一协议入口，不进行协议转换。
func accountEndpoint(account models.AIAccount, runtimeID string) (models.AIAccountEndpoint, bool) {
	if account.Endpoint.Protocol == "" {
		return models.AIAccountEndpoint{}, false
	}
	if account.AuthMode == constants.AccountAuthModeSubscription {
		endpoint := account.Endpoint
		if (runtimeID == constants.RuntimeIDCodex && endpoint.Protocol == constants.AccountProtocolOpenAIResponses && endpoint.BaseURL == constants.AIAccountOpenAIURL) ||
			(runtimeID == constants.RuntimeIDKimi && endpoint.Protocol == constants.AccountProtocolOpenAICompletions && endpoint.BaseURL == constants.AIAccountKimiURL) ||
			(runtimeID == constants.RuntimeIDClaude && endpoint.Protocol == constants.AccountProtocolAnthropicMessages && endpoint.BaseURL == constants.AIAccountAnthropicURL) {
			return endpoint, true
		}
		return models.AIAccountEndpoint{}, false
	}
	if accountconfig.Supports(runtimeID, account.Endpoint.Protocol) {
		return account.Endpoint, true
	}
	return models.AIAccountEndpoint{}, false
}

// begin 将接口中的认证请求纳入 Core 生命周期，关闭时可取消并等待。
func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	s.mutex.Lock()
	if s.ctx == nil || s.ctx.Err() != nil {
		s.mutex.Unlock()
		return nil, nil, status_error.OperationCanceled
	}
	root := s.ctx
	s.workers.Add(1)
	s.mutex.Unlock()
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(root, cancel)
	return linked, func() { stop(); cancel(); s.workers.Done() }, nil
}
