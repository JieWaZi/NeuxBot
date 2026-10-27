// Package aiaccount 管理账号使用租约、隔离配置与官方设备授权生命周期。
package aiaccount

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/codexoauth"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// Service 复用设置登录、账号修改和执行启动所需的账号生命周期。
type Service struct {
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
	// kimiDataDirectory 持久隔离每个 Kimi 订阅账号的原生登录和会话数据。
	kimiDataDirectory string
}

// New 创建尚未启动的账号服务。
func New(accounts controllers.AIAccountController, dataDirectory ...string) *Service {
	service := &Service{oauth: codexoauth.New(), accounts: accounts, readers: map[string]int{}, jobs: map[string]*loginJob{}}
	if len(dataDirectory) > 0 {
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
}

// Exclusive 为账号设置修改或登录获取独占租约。
func (s *Service) Exclusive(id string) (func(), error) { return s.acquire(id, true) }

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
		if current >= 0 && (!exclusive || current == 0) {
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
	current := s.readers[id]
	if exclusive {
		s.readers[id] = -1
	} else {
		s.readers[id] = current + 1
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mutex.Lock()
			defer s.mutex.Unlock()
			if s.readers[id] < 0 {
				delete(s.readers, id)
			} else {
				s.readers[id]--
				if s.readers[id] == 0 {
					delete(s.readers, id)
				}
			}
			if s.leaseChanged != nil {
				close(s.leaseChanged)
				s.leaseChanged = nil
			}
		})
	}, nil
}

// Validate 只确认关联的账号存在，不限制协议、登录状态或智能体种类。
func (s *Service) Validate(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	_, err := s.accounts.Get(ctx, id)
	return err
}

// Get 返回账号模型选择与协议配置，供执行入口校验当前选择。
func (s *Service) Get(ctx context.Context, id string) (models.AIAccount, error) {
	return s.accounts.Get(ctx, id)
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
		if model != "" {
			allowed := false
			for _, selected := range account.SelectedModels {
				allowed = allowed || selected == model
			}
			if !allowed {
				return account, status_error.BadRequest
			}
		}
		if levels := account.ModelReasoningLevels[model]; !accountconfig.IsNativeAccountRuntime(runtimeID) && len(levels) > 0 && reasoning != "" {
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

// Prepare 冻结账号配置并取得共享执行租约，Runtime 退出后回收本次投射。
func (s *Service) Prepare(ctx context.Context, id, runtimeID, workspace, model string) (runtimes.RuntimeTarget, func(), error) {
	target, closeLease, err := s.prepareWithLease(ctx, id, runtimeID, workspace, model, false, false)
	return target, legacyRelease(ctx, closeLease), err
}

// PrepareQueued 为后台模型调用等待账号租约；等待可取消，取得后仍沿既有认证投射与回收流程。
func (s *Service) PrepareQueued(ctx context.Context, id, runtimeID, workspace, model string) (runtimes.RuntimeTarget, func(), error) {
	target, closeLease, err := s.prepareWithLease(ctx, id, runtimeID, workspace, model, true, false)
	return target, legacyRelease(ctx, closeLease), err
}

// PrepareQueuedLease 为独立 Runtime 返回可报告、可重试的认证回收租约。
func (s *Service) PrepareQueuedLease(ctx context.Context, id, runtimeID, workspace, model string) (runtimes.RuntimeTarget, func(context.Context) error, error) {
	return s.prepareWithLease(ctx, id, runtimeID, workspace, model, true, true)
}

func legacyRelease(ctx context.Context, closeLease func(context.Context) error) func() {
	if closeLease == nil {
		return nil
	}
	return func() {
		if err := closeLease(context.WithoutCancel(ctx)); err != nil {
			logx.Errorf("recover account CLI credentials failed: error=%v", err)
		}
	}
}

// PrepareProbe 让只读能力探测等待账号设置修改，与普通执行共享账号。
func (s *Service) PrepareProbe(ctx context.Context, id, runtimeID, workspace, model string) (runtimes.RuntimeTarget, func(), error) {
	target, closeLease, err := s.prepareWithLease(ctx, id, runtimeID, workspace, model, true, false)
	return target, legacyRelease(ctx, closeLease), err
}

func (s *Service) prepareWithLease(ctx context.Context, id, runtimeID, workspace, model string, wait, retainFailedClose bool) (runtimes.RuntimeTarget, func(context.Context) error, error) {
	account, err := s.accounts.Get(ctx, id)
	if err != nil {
		return runtimes.RuntimeTarget{}, nil, err
	}
	release, err := s.acquireLease(ctx, id, false, wait)
	if err != nil {
		return runtimes.RuntimeTarget{}, nil, err
	}
	unlock, err := s.credentialGate(id).Lock(ctx)
	if err != nil {
		release()
		return runtimes.RuntimeTarget{}, nil, err
	}
	// 账号可能在取得共享租约前被修改，认证模式必须使用租约内的当前快照。
	account, err = s.accounts.Get(ctx, id)
	if err != nil {
		unlock()
		release()
		return runtimes.RuntimeTarget{}, nil, err
	}
	if err := s.recoverWorkspaceAuth(ctx, account.ID, runtimeID, workspace); err != nil {
		unlock()
		release()
		return runtimes.RuntimeTarget{}, nil, err
	}
	target, err := s.prepare(ctx, account, runtimeID, workspace, model)
	if err != nil {
		unlock()
		release()
		return runtimes.RuntimeTarget{}, nil, err
	}
	seed := ""
	if account.AuthMode == constants.AccountAuthModeSubscription && runtimeID == constants.RuntimeIDCodex {
		seed, err = s.accounts.Credential(ctx, id)
		if err != nil {
			unlock()
			release()
			return runtimes.RuntimeTarget{}, nil, err
		}
		target.RefreshCredentials = func(ctx context.Context, previous string) (codexoauth.Tokens, error) {
			return s.refreshSubscription(ctx, id, previous)
		}
	}
	unlock()
	var mutex sync.Mutex
	closed := false
	return target, func(closeContext context.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		if closed {
			return nil
		}
		if seed != "" {
			if err := s.backfillProjection(closeContext, account.ID, filepath.Join(accountconfig.Home(workspace, runtimeID), "auth.json"), seed); err != nil {
				if !retainFailedClose {
					release()
					closed = true
				}
				return err
			}
		}
		release()
		closed = true
		return nil
	}, nil
}

func (s *Service) credentialGate(id string) *codexoauth.CredentialGate {
	gate, _ := s.credentials.LoadOrStore(id, codexoauth.NewCredentialGate())
	return gate.(*codexoauth.CredentialGate)
}

func (s *Service) prepare(ctx context.Context, account models.AIAccount, runtimeID, workspace, model string) (runtimes.RuntimeTarget, error) {
	// 获取租约后重新读取，确保修改与冻结配置没有时间窗口。
	current, err := s.accounts.Get(ctx, account.ID)
	if err != nil {
		return runtimes.RuntimeTarget{}, err
	}
	if _, ok := accountEndpoint(current, runtimeID); !ok {
		return runtimes.RuntimeTarget{}, status_error.AIAccountConfigurationInvalid
	}
	if !current.HasCredential {
		return runtimes.RuntimeTarget{}, status_error.RuntimeAuthRequired
	}
	if current.AuthMode == constants.AccountAuthModeSubscription && runtimeID == constants.RuntimeIDKimi {
		return s.prepareKimiSubscription(current)
	}
	key, err := s.accounts.Credential(ctx, current.ID)
	if err != nil {
		return runtimes.RuntimeTarget{}, err
	}
	if current.AuthMode == constants.AccountAuthModeSubscription {
		if runtimeID != constants.RuntimeIDCodex {
			return runtimes.RuntimeTarget{}, status_error.AIAccountConfigurationInvalid
		}
		if err := s.prepareSubscription(ctx, &current, workspace, key); err != nil {
			return runtimes.RuntimeTarget{}, err
		}
	}
	endpoint, _ := accountEndpoint(current, runtimeID)
	environment, err := accountconfig.Prepare(workspace, accountconfig.Spec{
		ID: current.ID, RuntimeID: runtimeID, AuthMode: current.AuthMode, BaseURL: endpoint.BaseURL, Protocol: endpoint.Protocol,
		APIKey: key, Model: model, MaxContextSize: current.ModelContextSizes[model], Revision: current.Revision,
		SelectedModels: current.SelectedModels, ModelContextSizes: current.ModelContextSizes,
		ModelDisplayNames: current.ModelDisplayNames, ModelReasoningLevels: current.ModelReasoningLevels,
		ClaudeModelRoles: current.ClaudeModelRoles,
	})
	if err != nil {
		return runtimes.RuntimeTarget{}, err
	}
	return runtimes.RuntimeTarget{AccountID: current.ID, Environment: environment, Isolated: true, CloseAfterRun: true}, nil
}

// accountEndpoint 只使用账号的唯一协议入口，不进行协议转换。
func accountEndpoint(account models.AIAccount, runtimeID string) (models.AIAccountEndpoint, bool) {
	if len(account.Endpoints) == 0 {
		return models.AIAccountEndpoint{}, false
	}
	if account.AuthMode == constants.AccountAuthModeSubscription {
		endpoint := account.Endpoints[0]
		if (runtimeID == constants.RuntimeIDCodex && endpoint.Protocol == constants.AccountProtocolOpenAIResponses && endpoint.BaseURL == constants.AIAccountOpenAIURL) ||
			(runtimeID == constants.RuntimeIDKimi && endpoint.Protocol == constants.AccountProtocolOpenAICompletions && endpoint.BaseURL == constants.AIAccountKimiURL) {
			return endpoint, true
		}
		return models.AIAccountEndpoint{}, false
	}
	if accountconfig.Supports(runtimeID, account.Endpoints[0].Protocol) {
		return account.Endpoints[0], true
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
