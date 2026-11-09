// Package runtimeenvironment 提供运行时环境条目的初始化和探测生命周期。
package runtimeenvironment

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/installation"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"golang.org/x/sync/singleflight"
)

// Service 提供运行时环境条目的初始化和探测生命周期能力。
type Service interface {
	// CheckRuntimeUpdates 显式检查官方版本，不下载程序。
	CheckRuntimeUpdates(context.Context, string) error
	// RestartRuntime 排空闲置原生进程并重新探测，活动工作明确拒绝。
	RestartRuntime(context.Context, string) error
	// AuthenticationSnapshot 读取当前系统账号的原生认证能力和任务。
	AuthenticationSnapshot(context.Context, string, bool) (RuntimeAuthenticationSnapshot, error)
	// AuthenticateRuntime 接纳原生系统账号登录或重新连接。
	AuthenticateRuntime(context.Context, string, string) (RuntimeAuthenticationSnapshot, error)
	// CancelRuntimeAuthentication 取消精确任务身份对应的原生登录。
	CancelRuntimeAuthentication(context.Context, string, string) error
	// InstallationSnapshot 读取安装任务及已发布程序快照。
	InstallationSnapshot(context.Context, string) (RuntimeInstallationSnapshot, error)
	// InstallRuntime 接纳安装、重试或更新程序任务。
	InstallRuntime(context.Context, string) error
	// UninstallRuntime 卸载同一基础智能体的程序，保留账号和配置。
	UninstallRuntime(context.Context, string) error
	// CancelRuntimeInstallation 请求取消当前安装任务。
	CancelRuntimeInstallation(context.Context, string) error
	// ReadModelCatalog 读取指定账号的原生模型与能力。
	ReadModelCatalog(context.Context, string, string, bool) (AccountModelCatalog, error)
	// InvalidateModelCatalog 移除已撤销认证代次的发现结果。
	InvalidateModelCatalog(string, uint64)
	// Start 对账内置 Runtime 并启动后台探测生命周期。
	Start(ctx context.Context) error
	// Close 取消后台探测并等待所有资源退出。
	Close()
	// EnsureInitialized 在运行时环境表为空时建立未探测条目，不等待外部进程。
	EnsureInitialized(ctx context.Context) (bool, error)
	// StartupDetection 返回本次启动探测是否仍在进行及是否发生持久化故障。
	StartupDetection() (pending bool, failed bool, setupRequired bool)
	// CompleteStartupSetup 在用户保存合法的智能体默认配置后解除本次启动引导。
	CompleteStartupSetup()
	// RefreshAll 重新探测全部已注册 Runtime。
	RefreshAll(ctx context.Context) error
	// RefreshOne 重新探测指定 Runtime。
	RefreshOne(ctx context.Context, runtimeID string) error
}

// Config 声明运行时环境服务所需的外部依赖。
type Config struct {
	// BeforeRuntimeMaintenance 排空闲置进程并持有程序维护围栏。
	BeforeRuntimeMaintenance func(context.Context, string) (func(), error)
	// RestartProvider 由原生生命周期关闭指定基础智能体的闲置进程。
	RestartProvider func(context.Context, string) error
	// BeforeRuntimeAuthentication 阻止新系统消费者并排空已有空闲消费者。
	BeforeRuntimeAuthentication func(context.Context, string) (func(), error)
	// Installer 管理与账号目录分离的受管程序存储。
	Installer *installation.Installer
	// ProviderRuntime 提供与执行共享的原生进程所有者。
	ProviderRuntime *agent.ProviderRuntime
	// ResolveAccount 解析受管 Codex 账号的稳定认证选择。
	ResolveAccount func(context.Context, agent.AccountRequest, string) (agent.AccountRequest, error)
	// Accounts 为所选默认账号提供受管配置。
	Accounts *aiaccount.Service
	// AccountWorkspaceDirectory 保存仅用于探测的受管工作区。
	AccountWorkspaceDirectory string
	// RuntimeCatalog 提供 Runtime 摘要和本机安装位置。
	RuntimeCatalog *agent.Catalog
	// Environments 持久化运行时环境条目。
	Environments controllers.RuntimeEnvironmentController
	// ModelCatalogs 保存按运行时和账号隔离的目录快照。
	ModelCatalogs controllers.RuntimeModelCatalogController
	// Transactions 提供跨条目默认值替换的事务边界。
	Transactions controllers.TransactionController
	// Events 在启动探测结束后唤醒已有的运行时资源订阅。
	Events events.Publisher
}

// service 是运行时环境服务的生命周期和并发协调实现。
type service struct {
	// beforeRuntimeMaintenance 将程序维护绑定到同一原生进程所有者。
	beforeRuntimeMaintenance func(context.Context, string) (func(), error)
	// restartProvider 复用实际原生进程所有者的重启边界。
	restartProvider func(context.Context, string) error
	// restarting 防止同一基础智能体的重启请求与安装交错。
	restarting map[string]bool
	// authentication 拥有系统账号原生管理会话与登录任务。
	authentication *runtimeAuthenticationManager
	// installer 下载及校验程序，本服务独占后台任务生命周期。
	installer runtimeInstaller
	// installationMutex 串行安装任务接纳、取消、快照及退出。
	installationMutex sync.Mutex
	// installationJobs 按基础智能体合并所有启动配置的程序任务。
	installationJobs map[string]*runtimeInstallationJob
	// installationClosing 在退出等待前拒绝新的安装任务。
	installationClosing bool
	// modelCatalog 保存按账号隔离的原生模型发现结果。
	modelCatalog *agent.ModelCatalog
	// modelCatalogs 为普通读取提供最近探测的持久快照。
	modelCatalogs controllers.RuntimeModelCatalogController
	// modelCatalogRefreshes 合并同一启动配置的首次读取和主动刷新。
	modelCatalogRefreshes singleflight.Group
	// resolveAccount 复用受管账号认证选择。
	resolveAccount func(context.Context, agent.AccountRequest, string) (agent.AccountRequest, error)
	// accounts 生成默认账号的受管探测配置。
	accounts *aiaccount.Service
	// accountWorkspaceDirectory 是认证探测的受管目录。
	accountWorkspaceDirectory string
	// runtimeCatalog 提供 Runtime 摘要和本机安装位置。
	runtimeCatalog runtimeCatalog
	// probeProvider 读取已选账号的原生安装状态与目录。
	probeProvider func(context.Context, agent.RuntimeSummary, models.RuntimeEnvironment, agent.DetectRequest) agent.DetectionResult
	// environments 持久化条目与探测快照。
	environments controllers.RuntimeEnvironmentController
	// transactions 提供原子默认值替换。
	transactions controllers.TransactionController
	// lifecycleContext 是服务启动后后台探测使用的根 Context。
	lifecycleContext context.Context
	// cancelLifecycle 取消后台探测生命周期。
	cancelLifecycle context.CancelFunc
	// waitGroup 等待全部后台探测 goroutine 退出。
	waitGroup sync.WaitGroup
	// refreshMutex 串行化探测结果提交与默认选择，不持锁等待外部 CLI。
	refreshMutex sync.Mutex
	// refreshAllMutex 串行化全量刷新轮次，单条刷新不等待其他智能体。
	refreshAllMutex sync.Mutex
	// runtimeMutex 保护各智能体刷新锁的创建和读取。
	runtimeMutex sync.Mutex
	// runtimeRefreshLocks 只串行化同一智能体的探测与结果提交。
	runtimeRefreshLocks map[string]*sync.Mutex
	// initializeMutex 合并空库初始化请求，确保只执行一轮初始化探测。
	initializeMutex sync.Mutex
	// startupMutex 保护本次启动探测的完成与失败事实。
	startupMutex sync.RWMutex
	// startupPending 区分旧数据库快照与本次仍未结束的启动探测。
	startupPending bool
	// startupFailed 表示本次启动探测未能完整提交，不能据此判定没有 CLI。
	startupFailed bool
	// startupSetupRequired 由启动前证据决定，只在用户确认默认选择后解除，不随后台探测抖动。
	startupSetupRequired bool
	// eventPublisher 复用应用统一事件通道交付启动探测终态。
	eventPublisher events.Publisher
}

// runtimeCatalog 是运行时环境服务所需的只读 Runtime SDK 视图。
type runtimeCatalog interface {
	// ProbeInstallationVersion 只检查当前选中程序的实际版本，不使用账号或模型。
	ProbeInstallationVersion(context.Context, agent.DetectRequest) (agent.CLIInstallation, error)
	// Runtimes 返回全部可持久化的 Runtime 摘要。
	Runtimes() []agent.RuntimeSummary
	// ResolveExecutable 仅检查 CLI 是否存在，不依赖账号准备。
	ResolveExecutable(agent.DetectRequest) (string, error)
	// DiscoverInstallations 枚举可切换的 CLI 安装路径及版本。
	DiscoverInstallations(context.Context, agent.DetectRequest, []agent.CLIInstallation) []agent.CLIInstallation
}

var _ Service = (*service)(nil)

// New 创建尚未启动后台生命周期的运行时环境服务。
func New(config Config) (Service, error) {
	if config.RuntimeCatalog == nil {
		return nil, status_error.RuntimeUnavailable
	}
	created, err := newWithRuntimeCatalog(config.RuntimeCatalog, config.Environments, config.Transactions)
	if err != nil {
		return nil, err
	}
	concrete := created.(*service)
	concrete.restartProvider = config.RestartProvider
	concrete.beforeRuntimeMaintenance = config.BeforeRuntimeMaintenance
	concrete.authentication = newRuntimeAuthenticationManager(concrete, config.BeforeRuntimeAuthentication)
	if config.Installer != nil {
		concrete.installer = config.Installer
	}
	concrete.accounts = config.Accounts
	concrete.modelCatalogs = config.ModelCatalogs
	if config.ProviderRuntime != nil {
		concrete.modelCatalog = agent.NewModelCatalog(config.ProviderRuntime)
		concrete.probeProvider = concrete.detectNative
	}
	concrete.resolveAccount = config.ResolveAccount
	concrete.accountWorkspaceDirectory = config.AccountWorkspaceDirectory
	concrete.eventPublisher = config.Events
	return concrete, nil
}

func newWithRuntimeCatalog(
	catalog runtimeCatalog,
	environments controllers.RuntimeEnvironmentController,
	transactions controllers.TransactionController,
) (Service, error) {
	if catalog == nil {
		return nil, status_error.RuntimeUnavailable
	}
	if environments == nil || transactions == nil {
		return nil, status_error.RuntimeEnvironmentServiceUnavailable
	}
	return &service{
		runtimeCatalog: catalog,
		environments:   environments,
		transactions:   transactions,
	}, nil
}

// Start 对账内置 Runtime 并启动后台探测生命周期。
func (s *service) Start(ctx context.Context) error {
	if ctx == nil {
		return status_error.RuntimeEnvironmentServiceUnavailable
	}
	s.lifecycleContext, s.cancelLifecycle = context.WithCancel(ctx)
	// 启动准入读取已有默认配置或安装证据，不等待 CLI 或模型进程。
	if _, err := s.EnsureInitialized(ctx); err != nil {
		s.cancelLifecycle()
		return err
	}
	previous, err := s.environments.List(ctx)
	if err != nil {
		s.cancelLifecycle()
		return err
	}
	s.startupMutex.Lock()
	s.startupPending, s.startupFailed, s.startupSetupRequired = true, false, true
	for _, environment := range s.registeredEnvironments(previous) {
		if environment.Detection.ResolvedBinary != "" ||
			(environment.Enabled && environment.IsDefault && strings.TrimSpace(environment.Binary) != "") {
			s.startupSetupRequired = false
			break
		}
	}
	s.startupMutex.Unlock()
	s.waitGroup.Add(1)
	go func() {
		defer s.waitGroup.Done()
		if err := s.RefreshAll(s.lifecycleContext); err != nil {
			logx.Errorf("refresh runtime environments on startup failed: error=%v", err)
		}
	}()
	return nil
}

// StartupDetection 返回本次启动探测状态，不执行额外查询或外部探测。
func (s *service) StartupDetection() (pending bool, failed bool, setupRequired bool) {
	s.startupMutex.RLock()
	defer s.startupMutex.RUnlock()
	return s.startupPending, s.startupFailed, s.startupSetupRequired
}

// CompleteStartupSetup 只提交本次进程的准入选择，持久化默认值仍由原配置用例负责。
func (s *service) CompleteStartupSetup() {
	s.startupMutex.Lock()
	changed := s.startupSetupRequired
	s.startupSetupRequired = false
	s.startupMutex.Unlock()
	if changed && s.eventPublisher != nil {
		s.eventPublisher.Publish(events.Event{Topic: "runtimes", Type: "runtimecontract.changed", Resource: events.Resource{Kind: "global"}})
	}
}

// Close 取消后台探测并等待所有资源退出。
func (s *service) Close() {
	if s != nil && s.authentication != nil {
		s.authentication.Close()
	}
	if s == nil || s.cancelLifecycle == nil {
		return
	}
	s.installationMutex.Lock()
	s.installationClosing = true
	s.cancelLifecycle()
	s.installationMutex.Unlock()
	s.waitGroup.Wait()
	s.cancelLifecycle = nil
}

// EnsureInitialized 在运行时环境表为空时建立未探测条目，不等待外部进程。
func (s *service) EnsureInitialized(ctx context.Context) (bool, error) {
	s.initializeMutex.Lock()
	defer s.initializeMutex.Unlock()
	count, err := s.environments.Count(ctx)
	if err != nil || count > 0 {
		return false, err
	}
	return true, s.initialize(ctx)
}

// RefreshAll 重新探测全部已注册 Runtime。
func (s *service) RefreshAll(ctx context.Context) (err error) {
	s.refreshAllMutex.Lock()
	defer s.refreshAllMutex.Unlock()
	// 与探测快照保持同一串行顺序，避免启动失败的旧终态覆盖成功重试。
	defer func() {
		s.startupMutex.Lock()
		changed := s.startupPending || (s.startupFailed && err == nil)
		if s.startupPending || s.startupFailed {
			s.startupPending, s.startupFailed = false, err != nil
		}
		s.startupMutex.Unlock()
		if changed && s.eventPublisher != nil {
			s.eventPublisher.Publish(events.Event{Topic: "runtimes", Type: "runtimecontract.changed", Resource: events.Resource{Kind: "global"}})
		}
	}()
	return s.refreshAllLocked(ctx)
}

// RefreshOne 重新探测指定 Runtime。
func (s *service) RefreshOne(ctx context.Context, runtimeID string) error {
	runtimeID = strings.TrimSpace(runtimeID)
	lock := s.runtimeRefreshLock(runtimeID)
	lock.Lock()
	defer lock.Unlock()
	return s.refreshOneLocked(ctx, runtimeID)
}

func (s *service) runtimeRefreshLock(runtimeID string) *sync.Mutex {
	s.runtimeMutex.Lock()
	if s.runtimeRefreshLocks == nil {
		s.runtimeRefreshLocks = make(map[string]*sync.Mutex)
	}
	lock := s.runtimeRefreshLocks[runtimeID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.runtimeRefreshLocks[runtimeID] = lock
	}
	s.runtimeMutex.Unlock()
	return lock
}

func (s *service) initialize(ctx context.Context) error {
	return s.transactions.Transaction(ctx, func(transactionContext context.Context) error {
		for _, summary := range s.runtimeCatalog.Runtimes() {
			if err := s.environments.Ensure(transactionContext, newEnvironment(summary)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *service) refreshAllLocked(ctx context.Context) error {
	summaries := s.runtimeCatalog.Runtimes()
	environments, err := s.environments.List(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]models.RuntimeEnvironment, len(environments))
	for _, environment := range environments {
		byID[environment.RuntimeID] = environment
	}
	for _, summary := range summaries {
		environment, exists := byID[summary.ID]
		if !exists {
			environment = newEnvironment(summary)
			if err := s.environments.Ensure(ctx, environment); err != nil {
				return err
			}
		}
	}
	results := make(chan error, len(summaries))
	for _, summary := range summaries {
		go func() {
			results <- s.RefreshOne(ctx, summary.ID)
		}()
	}
	// 接收全部 worker 的唯一结果后再返回，既及时发布完成项，也保证生命周期等待完整。
	var firstError error
	for range summaries {
		if err := <-results; firstError == nil {
			firstError = err
		}
	}
	return firstError
}

func (s *service) refreshOneLocked(ctx context.Context, runtimeID string) error {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return err
	}
	var summary agent.RuntimeSummary
	for _, candidate := range s.runtimeCatalog.Runtimes() {
		if candidate.ID == runtimeID {
			summary = candidate
			break
		}
	}
	if summary.ID == "" {
		return status_error.RuntimeUnavailable
	}
	environment, err = s.reconcileEnvironment(ctx, summary, environment)
	if err != nil {
		return err
	}
	result := s.detect(ctx, summary, environment)
	s.refreshMutex.Lock()
	defer s.refreshMutex.Unlock()
	if err := s.transactions.Transaction(ctx, func(transactionContext context.Context) error {
		return s.persistDetection(
			transactionContext,
			environment,
			result,
			time.Now().UnixMilli(),
		)
	}); err != nil {
		return err
	}
	return s.ensureDefault(ctx)
}

// reconcileEnvironment 只同步注册表管理的产品名称，不改写用户配置的启动命令。
func (s *service) reconcileEnvironment(
	ctx context.Context,
	summary agent.RuntimeSummary,
	environment models.RuntimeEnvironment,
) (models.RuntimeEnvironment, error) {
	fields := map[string]any{}
	if environment.Name != summary.Name {
		environment.Name = summary.Name
		fields["name"] = summary.Name
	}
	if len(fields) == 0 {
		return environment, nil
	}
	return environment, s.environments.UpdateFields(ctx, environment.RuntimeID, fields)
}

// persistDetection 原子保存探测快照与安装定位；模型、推理和权限默认值只由用户配置用例修改。
func (s *service) persistDetection(
	ctx context.Context,
	environment models.RuntimeEnvironment,
	detection agent.DetectionResult,
	detectedAt int64,
) error {
	nativeCatalog := usesNativeCatalog(environment.BaseRuntimeID) && s.modelCatalog != nil
	if nativeCatalog {
		current, err := s.environments.Get(ctx, environment.RuntimeID)
		if err != nil {
			return err
		}
		// A response from the prior account or launch cannot update the new selection.
		if current.DefaultAccountID != environment.DefaultAccountID || current.Binary != environment.Binary ||
			current.ConfigHome != environment.ConfigHome || !slices.Equal(current.PrefixArgs, environment.PrefixArgs) {
			return nil
		}
		environment = current
	} else {
		detection = agent.PreservePreviousOptionsOnFailure(environment.Detection, detection)
	}
	if err := s.environments.UpdateDetection(
		ctx,
		environment.RuntimeID,
		detection,
		detectedAt,
	); err != nil {
		return err
	}
	return nil
}

func (s *service) detect(
	ctx context.Context,
	summary agent.RuntimeSummary,
	environment models.RuntimeEnvironment,
) agent.DetectionResult {
	managedSelected := false
	programArguments := s.programSelectionArguments(environment)
	environment.PrefixArgs = append(append([]string(nil), programArguments...), summary.PrefixArgs...)
	environment.ConfigHome = summary.ConfigHome
	preferManaged := environment.Detection.ResolvedBinary == ""
	if s.installer != nil && !preferManaged {
		maintenance, err := s.installer.Inspect(ctx, summary.BaseRuntimeID, environment.Detection.ResolvedBinary, programArguments)
		preferManaged = err == nil && maintenance.Method == "managed"
	}
	if s.installer != nil && preferManaged {
		installed, err := s.installer.Installed(summary.BaseRuntimeID)
		if err == nil {
			managedSelected = true
			environment.Binary = installed.Binary
			environment.PrefixArgs = append(append([]string(nil), installed.PrefixArgs...), summary.PrefixArgs...)
			summary.BinaryCandidates, summary.PrefixArgs = []string{installed.Binary}, environment.PrefixArgs
		} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, status_error.RuntimeInstallationUnsupported) {
			return agent.DetectionResult{Runtime: summary, State: agent.DetectionStateUnavailable, Auth: agent.AuthStatusUnknown,
				Diagnostics: []agent.Diagnostic{{Code: status_error.RuntimeInstallationFailed.Key(), Level: agent.DiagnosticLevelError, Message: "managed agent installation failed verification"}}}
		}
	}
	request := runtimeDetectRequest(summary, environment)
	if !managedSelected && request.LaunchOverride.Binary == firstBinary(summary.BinaryCandidates) && environment.Detection.ResolvedBinary != "" {
		request.LaunchOverride.Binary = environment.Detection.ResolvedBinary
	}
	// 安装版本命令只使用设备级启动配置，不依赖短期账号目录的存活。
	installationRequest := request
	finish := func(result agent.DetectionResult) agent.DetectionResult {
		// 目录适配器可返回静态声明；实际已验证的启动前缀归当前探测请求所有。
		result.Runtime.PrefixArgs = append([]string(nil), request.LaunchOverride.PrefixArgs...)
		result.Version = agent.NormalizeRuntimeVersion(result.Version)
		result.Installations = s.runtimeCatalog.DiscoverInstallations(ctx, installationRequest, environment.Detection.Installations)
		// 可执行检查短暂失败时只保留明确选中的同一路径版本，不恢复安装或就绪状态。
		if result.Version == "" && result.ResolvedBinary == "" && environment.Detection.ResolvedBinary != "" && request.LaunchOverride.Binary == environment.Detection.ResolvedBinary {
			result.Version = agent.NormalizeRuntimeVersion(environment.Detection.Version)
		}
		if result.ResolvedBinary != "" {
			found := false
			for index := range result.Installations {
				if result.Installations[index].Path == result.ResolvedBinary {
					found = true
					if result.Version == "" {
						result.Version = agent.NormalizeRuntimeVersion(result.Installations[index].Version)
					}
					if result.Version == "" && result.ResolvedBinary == environment.Detection.ResolvedBinary {
						result.Version = agent.NormalizeRuntimeVersion(environment.Detection.Version)
					}
					result.Installations[index].Version = result.Version
					break
				}
			}
			if !found {
				if result.Version == "" && result.ResolvedBinary == environment.Detection.ResolvedBinary {
					result.Version = agent.NormalizeRuntimeVersion(environment.Detection.Version)
				}
				result.Installations = append(result.Installations, agent.CLIInstallation{
					Path: result.ResolvedBinary, Version: result.Version, Available: true,
				})
			}
		}
		return result
	}
	if usesNativeCatalog(summary.BaseRuntimeID) && s.probeProvider != nil {
		return finish(s.probeProvider(ctx, summary, environment, request))
	}
	binary, _ := s.runtimeCatalog.ResolveExecutable(request)
	return finish(agent.DetectionResult{
		Runtime: summary, State: agent.DetectionStateUnavailable, Auth: agent.AuthStatusUnknown,
		ResolvedBinary: binary,
	})
}

// runtimeDetectRequest 把持久化配置投影为 ACP Agent 构造使用的受控启动输入。
func runtimeDetectRequest(
	summary agent.RuntimeSummary,
	environment models.RuntimeEnvironment,
) agent.DetectRequest {
	launchOverride := agent.LaunchOverride{
		Binary:      environment.Binary,
		PrefixArgs:  append([]string(nil), environment.PrefixArgs...),
		Environment: map[string]string{},
	}
	if environment.ConfigHome != "" && summary.ConfigHomeEnvironmentVariable != "" {
		launchOverride.Environment[summary.ConfigHomeEnvironmentVariable] = environment.ConfigHome
	}
	homeDirectory, _ := os.UserHomeDir()
	return agent.DetectRequest{
		RuntimeID:       summary.ID,
		BaseEnvironment: accountconfig.Environment(os.Environ()),
		HomeDirectory:   homeDirectory,
		LaunchOverride:  launchOverride,
	}
}

func (s *service) ensureDefault(ctx context.Context) error {
	// 检查与替换共享短事务，避免后台完成项覆盖用户刚保存的默认选择。
	return s.transactions.Transaction(ctx, func(transactionContext context.Context) error {
		environments, err := s.environments.List(transactionContext)
		if err != nil {
			return err
		}
		environments = s.registeredEnvironments(environments)
		for _, environment := range environments {
			// 已保存的默认选择不由后台探测重新决策，包括用户主动禁用的条目。
			if environment.IsDefault {
				return nil
			}
		}
		for _, environment := range environments {
			if !environment.Enabled || environment.Detection.State != agent.DetectionStateReady {
				continue
			}
			if err := s.environments.ClearDefault(transactionContext); err != nil {
				return err
			}
			return s.environments.UpdateFields(transactionContext, environment.RuntimeID, map[string]any{"is_default": true})
		}
		return nil
	})
}

// registeredEnvironments 只让当前目录中的智能体及配置参与启动和默认选择。
func (s *service) registeredEnvironments(environments []models.RuntimeEnvironment) []models.RuntimeEnvironment {
	registered := make(map[string]struct{})
	for _, summary := range s.runtimeCatalog.Runtimes() {
		registered[summary.ID] = struct{}{}
	}
	result := make([]models.RuntimeEnvironment, 0, len(environments))
	for _, environment := range environments {
		if _, exists := registered[environment.RuntimeID]; exists {
			result = append(result, environment)
		}
	}
	return result
}

func newEnvironment(summary agent.RuntimeSummary) models.RuntimeEnvironment {
	return models.RuntimeEnvironment{
		ID:                     util.New(),
		RuntimeID:              summary.ID,
		BaseRuntimeID:          summary.BaseRuntimeID,
		Name:                   summary.Name,
		Enabled:                true,
		Binary:                 firstBinary(summary.BinaryCandidates),
		PrefixArgs:             append([]string(nil), summary.PrefixArgs...),
		ConfigHome:             summary.ConfigHome,
		DefaultModel:           summary.DefaultModel,
		DefaultReasoning:       firstReasoning(summary.ReasoningOptions),
		DefaultPermissionLevel: agent.PermissionLevelFullAccess,
		Detection: agent.DetectionResult{
			Runtime: summary,
			Auth:    agent.AuthStatusUnknown,
		},
	}
}

func firstBinary(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func firstReasoning(options []agent.ReasoningOption) string {
	if len(options) == 0 {
		return ""
	}
	return options[0].ID
}
