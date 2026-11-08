package runtimeenvironment

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// 原生认证方法
type RuntimeAuthenticationMethod struct {
	// 方法标识
	ID string `json:"id"`
	// 方法名称
	Name string `json:"name"`
	// 交互方式
	Kind string `json:"kind"`
}

// 系统账号认证状态
type RuntimeAuthenticationSnapshot struct {
	// 认证结果
	Result constants.RuntimeAuthenticationResult `json:"result"`
	// 智能体标识
	RuntimeID string `json:"runtimeId"`
	// 账号范围
	Scope string `json:"scope"`
	// 认证状态
	State constants.RuntimeAuthenticationState `json:"state"`
	// 登录任务标识
	JobID string `json:"jobId"`
	// 原生认证方法
	Methods []RuntimeAuthenticationMethod `json:"methods"`
	// 支持原生登录
	CanAuthenticate bool `json:"canAuthenticate"`
	// 支持密钥配置
	APIKeySupported bool `json:"apiKeySupported"`
	// 需要终端认证
	TerminalRequired bool `json:"terminalRequired"`
	// 本机配置限制
	ConfigurationReason string `json:"configurationReason,omitempty"`
	// 允许取消
	Cancelable bool `json:"cancelable"`
	// 失败代码
	ErrorCode string `json:"errorCode,omitempty"`
	// 原生授权地址
	VerificationURL string `json:"verificationUrl,omitempty"`
	// 原生授权码
	UserCode string `json:"userCode,omitempty"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
	// 快照修订
	Revision uint64 `json:"revision"`
}

// runtimeAuthenticationManager 仅拥有系统原生认证，不创建或投射受管账号。
type runtimeAuthenticationManager struct {
	// service 提供程序定位、已有事件与登录完成后的真实重检。
	service *service
	// mutex 串行任务准入、快照和关闭失败后的同一所有者交接。
	mutex sync.Mutex
	// jobs 按实际启动配置隔离原生认证任务。
	jobs map[string]*runtimeAuthenticationJob
	// before 在原生修改前阻止新系统消费者并排空已有空闲消费者。
	before func(context.Context, string) (func(), error)
	// factory 仅构造原生进程所有者，测试替换为无副作用端口。
	factory func(context.Context, agent.AccountRequest, agent.ProviderLaunch) (agent.NativeAuthenticationClient, error)
	// closing 在退出等待前阻止新任务。
	closing bool
	// workers 等待每个已接纳的初始化、授权和关闭过程。
	workers sync.WaitGroup
}

// runtimeAuthenticationJob 保留原生进程和准入围栏直到同一进程确认关闭。
type runtimeAuthenticationJob struct {
	// provider 固定原生 CLI 家族，供安装与重启共享维护准入。
	provider string
	// mutating 区分只读能力握手和会改变原生认证的任务。
	mutating bool
	// snapshot 是对外可恢复的非秘密状态。
	snapshot RuntimeAuthenticationSnapshot
	// cancel 请求结束当前原生操作，不能提前释放进程所有权。
	cancel context.CancelFunc
	// client 在关闭失败后保留原始实例供显式重试。
	client agent.NativeAuthenticationClient
	// release 在原生进程退出后解除系统消费者准入围栏。
	release func()
	// signature 固定本次程序和原生配置范围，防止旧程序广告跨安装复用。
	signature string
}

func newRuntimeAuthenticationManager(svc *service, before func(context.Context, string) (func(), error)) *runtimeAuthenticationManager {
	return &runtimeAuthenticationManager{service: svc, before: before, factory: agent.NewSystemAuthenticationClient, jobs: map[string]*runtimeAuthenticationJob{}}
}

// AuthenticationSnapshot 首次读取只协商认证能力，不启动登录或原生会话。
func (s *service) AuthenticationSnapshot(ctx context.Context, runtimeID string, refresh bool) (RuntimeAuthenticationSnapshot, error) {
	if s.authentication == nil {
		return RuntimeAuthenticationSnapshot{}, status_error.RuntimeUnavailable
	}
	return s.authentication.snapshot(ctx, runtimeID, refresh)
}

// AuthenticateRuntime 接纳当前系统配置范围的原生登录，返回取消所需的任务身份。
func (s *service) AuthenticateRuntime(ctx context.Context, runtimeID, methodID string) (RuntimeAuthenticationSnapshot, error) {
	if s.authentication == nil {
		return RuntimeAuthenticationSnapshot{}, status_error.RuntimeUnavailable
	}
	return s.authentication.start(ctx, runtimeID, methodID, true)
}

// CancelRuntimeAuthentication 只取消调用方观察到的任务，旧弹层不能取消新登录。
func (s *service) CancelRuntimeAuthentication(_ context.Context, runtimeID, jobID string) error {
	if s.authentication == nil {
		return status_error.RuntimeUnavailable
	}
	m := s.authentication
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if job := m.jobs[runtimeID]; job != nil && job.snapshot.JobID == jobID && job.cancel != nil {
		job.cancel()
	}
	return nil
}

func (m *runtimeAuthenticationManager) snapshot(ctx context.Context, runtimeID string, refresh bool) (RuntimeAuthenticationSnapshot, error) {
	_, _, signature, err := m.launch(ctx, runtimeID)
	if err != nil {
		return RuntimeAuthenticationSnapshot{}, err
	}
	m.mutex.Lock()
	if job := m.jobs[runtimeID]; job != nil && ((!refresh && (job.signature == signature || job.client != nil)) || job.cancel != nil) {
		view := cloneAuthenticationSnapshot(job.snapshot)
		m.mutex.Unlock()
		return view, nil
	}
	m.mutex.Unlock()
	return m.start(ctx, runtimeID, "", false)
}

func (m *runtimeAuthenticationManager) start(ctx context.Context, runtimeID, methodID string, authenticate bool) (RuntimeAuthenticationSnapshot, error) {
	account, launch, signature, err := m.launch(ctx, runtimeID)
	if err != nil {
		return RuntimeAuthenticationSnapshot{}, err
	}
	// 维护准入始终按 installationMutex → authentication.mutex 加锁；安装也遵守相同顺序。
	m.service.installationMutex.Lock()
	defer m.service.installationMutex.Unlock()
	if m.service.restarting[account.Provider] {
		return RuntimeAuthenticationSnapshot{}, status_error.RuntimeRestartBusy
	}
	if installation := m.service.installationJobs[account.Provider]; installation != nil && installation.snapshot.State == constants.RuntimeInstallationStateInstalling {
		return RuntimeAuthenticationSnapshot{}, status_error.RuntimeInstallationBusy
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.closing || m.service.lifecycleContext == nil || m.service.lifecycleContext.Err() != nil {
		return RuntimeAuthenticationSnapshot{}, status_error.OperationCanceled
	}
	previous := m.jobs[runtimeID]
	for id, other := range m.jobs {
		if id != runtimeID && other.provider == account.Provider && (authenticate || other.mutating) && (other.cancel != nil || other.client != nil) {
			// 管理进程也是真实系统凭据消费者；不同启动配置不能绕过同一家族的认证围栏。
			return RuntimeAuthenticationSnapshot{}, status_error.AIAccountBusy
		}
	}
	if previous != nil && previous.cancel != nil {
		if !authenticate {
			return cloneAuthenticationSnapshot(previous.snapshot), nil
		}
		return RuntimeAuthenticationSnapshot{}, status_error.AIAccountBusy
	}
	if previous != nil && previous.client != nil {
		if err := m.closeOwner(previous); err != nil {
			return RuntimeAuthenticationSnapshot{}, err
		}
	}
	job := &runtimeAuthenticationJob{provider: account.Provider, mutating: authenticate, signature: signature, snapshot: RuntimeAuthenticationSnapshot{
		RuntimeID: runtimeID, Scope: agent.SystemAccountID, JobID: util.New(), Methods: []RuntimeAuthenticationMethod{},
		State: constants.RuntimeAuthenticationStateChecking, APIKeySupported: nativeAPIKeySupported(account.Provider),
		Cancelable: true, UpdatedAt: time.Now().UnixMilli(), Revision: 1,
	}}
	if !authenticate {
		job.snapshot.JobID = ""
		job.snapshot.Cancelable = false
	}
	if previous != nil {
		job.snapshot.Revision = previous.snapshot.Revision + 1
		job.snapshot.Methods = slices.Clone(previous.snapshot.Methods)
		job.snapshot.CanAuthenticate = previous.snapshot.CanAuthenticate
		job.snapshot.TerminalRequired = previous.snapshot.TerminalRequired
	}
	jobContext, cancel := context.WithTimeout(m.service.lifecycleContext, 10*time.Minute)
	job.cancel = cancel
	m.jobs[runtimeID] = job
	m.workers.Add(1)
	go m.run(jobContext, job, account, launch, methodID, authenticate)
	return cloneAuthenticationSnapshot(job.snapshot), nil
}

// IsAuthenticating 在安装准入锁之内读取，关闭失败仍占有原生账号维护范围。
func (m *runtimeAuthenticationManager) IsAuthenticating(provider string) bool {
	return m.hasNativeOwner(provider, true)
}

// HasNativeOwner 区分程序维护与模型编辑；只读握手也持有不可在原位替换的真实程序。
func (m *runtimeAuthenticationManager) HasNativeOwner(provider string) bool {
	return m.hasNativeOwner(provider, false)
}

func (m *runtimeAuthenticationManager) hasNativeOwner(provider string, mutatingOnly bool) bool {
	if m == nil {
		return false
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, job := range m.jobs {
		if job.provider == provider && (!mutatingOnly || job.mutating) && (job.cancel != nil || job.release != nil || job.client != nil) {
			return true
		}
	}
	return false
}

func (m *runtimeAuthenticationManager) launch(ctx context.Context, runtimeID string) (agent.AccountRequest, agent.ProviderLaunch, string, error) {
	environment, err := m.service.environments.Get(ctx, runtimeID)
	if err != nil {
		return agent.AccountRequest{}, agent.ProviderLaunch{}, "", err
	}
	var summary agent.RuntimeSummary
	for _, candidate := range m.service.runtimeCatalog.Runtimes() {
		if candidate.ID == runtimeID {
			summary = candidate
			break
		}
	}
	if summary.ID == "" {
		return agent.AccountRequest{}, agent.ProviderLaunch{}, "", status_error.RuntimeNotRegistered
	}
	provider := summary.BaseRuntimeID
	if provider == "" {
		provider = summary.ID
	}
	binary := environment.Detection.ResolvedBinary
	if binary == "" {
		binary, err = m.service.runtimeCatalog.ResolveExecutable(runtimeDetectRequest(summary, environment))
		if err != nil || binary == "" {
			return agent.AccountRequest{}, agent.ProviderLaunch{}, "", status_error.RuntimeUnavailable
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return agent.AccountRequest{}, agent.ProviderLaunch{}, "", status_error.RuntimeConfigurationInvalid
	}
	base := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		base[key] = value
	}
	// 静态配置保留原生根的词法选择；默认 Claude 根不能被显式化后改变 Keychain 身份。
	account := agent.AccountRequest{Provider: provider, AccountID: agent.SystemAccountID, Executable: binary, PrefixArgs: slices.Clone(environment.PrefixArgs), NativeConfigHome: summary.ConfigHome, CWD: home}
	launch := agent.ProviderLaunch{Executable: binary, PrefixArgs: slices.Clone(environment.PrefixArgs), BaseEnvironment: base}
	if len(launch.PrefixArgs) == 0 {
		launch.PrefixArgs = slices.Clone(summary.PrefixArgs)
		account.PrefixArgs = slices.Clone(summary.PrefixArgs)
	}
	signature := binary + "\x00" + strings.Join(launch.PrefixArgs, "\x00") + "\x00" + summary.ConfigHome
	return account, launch, signature, nil
}

func (m *runtimeAuthenticationManager) run(ctx context.Context, job *runtimeAuthenticationJob, account agent.AccountRequest, launch agent.ProviderLaunch, methodID string, authenticate bool) {
	defer m.workers.Done()
	var err error
	var result constants.RuntimeAuthenticationResult
	if authenticate {
		if m.before == nil {
			err = status_error.RuntimeUnavailable
		} else {
			var release func()
			release, err = m.before(ctx, account.Provider)
			m.mutex.Lock()
			job.release = release
			m.mutex.Unlock()
		}
	}
	if err == nil {
		var client agent.NativeAuthenticationClient
		client, err = m.factory(ctx, account, launch)
		m.mutex.Lock()
		job.client = client
		m.mutex.Unlock()
	}
	var capabilities agent.NativeAuthenticationCapabilities
	if err == nil {
		probeContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		capabilities, err = job.client.Initialize(probeContext)
		cancel()
	}
	if err == nil {
		methods, terminal := actionableNativeMethods(account.Provider, capabilities)
		m.change(job, func(view *RuntimeAuthenticationSnapshot) {
			view.Methods, view.TerminalRequired, view.CanAuthenticate = methods, terminal, len(methods) > 0
			view.ConfigurationReason = ""
			switch {
			case capabilities.EnvironmentCredential || capabilities.APIKeyMode:
				view.ConfigurationReason = constants.RuntimeAuthenticationConfigurationReasonEnvironmentCredentials
			case terminal:
				view.ConfigurationReason = constants.RuntimeAuthenticationConfigurationReasonTerminalAuthentication
			case len(methods) == 0:
				view.ConfigurationReason = constants.RuntimeAuthenticationConfigurationReasonNoNativeAuthentication
			}
		})
		if authenticate {
			if methodID == "" && len(methods) == 1 {
				methodID = methods[0].ID
			}
			if !slices.ContainsFunc(methods, func(method RuntimeAuthenticationMethod) bool { return method.ID == methodID }) {
				err = status_error.RuntimePolicyUnsupported
			} else {
				m.change(job, func(view *RuntimeAuthenticationSnapshot) {
					view.State = constants.RuntimeAuthenticationStateAuthenticating
				})
				result, err = job.client.Authenticate(ctx, methodID)
				if err == nil && result == constants.RuntimeAuthenticationResultUnspecified {
					err = status_error.RuntimeProtocolIncompatible
				}
			}
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		err = status_error.OperationTimeout
	} else if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		err = status_error.OperationCanceled
	}
	// 关闭失败仍归本任务所有；不得因请求结束或失败终态提前放开系统账号。
	m.mutex.Lock()
	closeErr := m.closeOwner(job)
	m.mutex.Unlock()
	if closeErr != nil {
		err = closeErr
	}
	if err == nil && authenticate {
		err = m.service.RefreshOne(ctx, job.snapshot.RuntimeID)
		if err == nil {
			// 重检可能确认另一处等价程序路径，不能把刚完成的登录误作新能力任务覆盖。
			_, _, signature, launchErr := m.launch(ctx, job.snapshot.RuntimeID)
			if launchErr == nil {
				m.mutex.Lock()
				job.signature = signature
				m.mutex.Unlock()
			}
		}
	}
	m.change(job, func(view *RuntimeAuthenticationSnapshot) {
		view.Cancelable = false
		view.ErrorCode = authenticationErrorCode(err)
		switch {
		case errors.Is(err, status_error.OperationCanceled):
			view.State = constants.RuntimeAuthenticationStateCancelled
		case err != nil:
			view.State = constants.RuntimeAuthenticationStateFailed
		case authenticate:
			view.State = constants.RuntimeAuthenticationStateSucceeded
			view.Result = result
		default:
			view.State = constants.RuntimeAuthenticationStateIdle
		}
		job.cancel()
		job.cancel = nil
	})
}

// closeOwner 只在任务停止或准入锁持有时调用，重试始终使用原始 native client。
func (m *runtimeAuthenticationManager) closeOwner(job *runtimeAuthenticationJob) error {
	if job.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := job.client.Close(ctx); err != nil {
			return status_error.RuntimeShutdownFailed
		}
		job.client = nil
	}
	if job.release != nil {
		job.release()
		job.release = nil
	}
	return nil
}

func (m *runtimeAuthenticationManager) change(job *runtimeAuthenticationJob, change func(*RuntimeAuthenticationSnapshot)) {
	m.mutex.Lock()
	change(&job.snapshot)
	job.snapshot.Revision++
	job.snapshot.UpdatedAt = time.Now().UnixMilli()
	runtimeID := job.snapshot.RuntimeID
	m.mutex.Unlock()
	if m.service.eventPublisher != nil {
		m.service.eventPublisher.Publish(events.Event{Topic: constants.RuntimeEventTopic, Type: constants.RuntimeAuthenticationChangedEventType, Resource: events.Resource{Kind: "runtime", ID: runtimeID}})
	}
}

func (m *runtimeAuthenticationManager) Close() {
	m.mutex.Lock()
	m.closing = true
	for _, job := range m.jobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
	m.mutex.Unlock()
	m.workers.Wait()
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, job := range m.jobs {
		_ = m.closeOwner(job)
	}
}

func cloneAuthenticationSnapshot(view RuntimeAuthenticationSnapshot) RuntimeAuthenticationSnapshot {
	view.Methods = slices.Clone(view.Methods)
	return view
}

func nativeAPIKeySupported(provider string) bool {
	for _, protocol := range []constants.AccountProtocol{constants.AccountProtocolOpenAIResponses, constants.AccountProtocolOpenAICompletions, constants.AccountProtocolAnthropicMessages, constants.AccountProtocolGoogleGenerativeAI} {
		if accountconfig.Supports(provider, protocol) {
			return true
		}
	}
	return false
}

func actionableNativeMethods(provider string, capabilities agent.NativeAuthenticationCapabilities) ([]RuntimeAuthenticationMethod, bool) {
	methods := []RuntimeAuthenticationMethod{}
	terminal := provider == constants.RuntimeIDOpenCode || provider == constants.RuntimeIDPi || provider == constants.RuntimeIDKimi
	for _, method := range capabilities.Methods {
		if method.Kind == "terminal" {
			terminal = true
		}
		if method.Kind != "agent" || capabilities.APIKeyMode || capabilities.EnvironmentCredential || provider == constants.RuntimeIDKimi || provider == constants.RuntimeIDOpenCode || provider == constants.RuntimeIDPi {
			continue
		}
		// 这些广告是凭据配置或缓存恢复，不能作为用户主动网页登录执行。
		switch method.ID {
		case "api-key", "xai.api_key", "cached_token", "gemini-api-key", "vertex-ai":
			continue
		}
		methods = append(methods, RuntimeAuthenticationMethod{ID: method.ID, Name: method.Name, Kind: "agent"})
	}
	return methods, terminal
}

func authenticationErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status_error.OperationTimeout.Key()
	}
	if errors.Is(err, context.Canceled) {
		return status_error.OperationCanceled.Key()
	}
	var status status_error.StatusError
	if errors.As(err, &status) {
		return status.Key()
	}
	return status_error.RuntimeAuthRequired.Key()
}
