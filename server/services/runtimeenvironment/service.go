// Package runtimeenvironment 提供运行时环境条目的初始化和探测生命周期。
package runtimeenvironment

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
)

// Service 提供运行时环境条目的初始化和探测生命周期能力。
type Service interface {
	// Start 对账内置 Runtime 并启动后台探测生命周期。
	Start(ctx context.Context) error
	// Close 取消后台探测并等待所有资源退出。
	Close()
	// EnsureInitialized 在运行时环境表为空时建立未探测条目，不等待外部进程。
	EnsureInitialized(ctx context.Context) (bool, error)
	// StartupDetection 返回本次启动探测是否仍在进行及是否发生持久化故障。
	StartupDetection() (pending bool, failed bool, setupRequired bool)
	// CompleteStartupSetup 在用户保存已安装智能体的默认选择后解除本次启动引导。
	CompleteStartupSetup()
	// RefreshAll 重新探测全部已注册 Runtime。
	RefreshAll(ctx context.Context) error
	// RefreshOne 重新探测指定 Runtime。
	RefreshOne(ctx context.Context, runtimeID string) error
}

// Config 声明运行时环境服务所需的外部依赖。
type Config struct {
	// Accounts 为所选默认账号提供受管配置。
	Accounts *aiaccount.Service
	// AccountWorkspaceDirectory 保存仅用于探测的受管工作区。
	AccountWorkspaceDirectory string
	// RuntimeSDK 执行不触发模型推理的探测并提供 Runtime 摘要。
	RuntimeSDK *runtimes.SDK
	// Environments 持久化运行时环境条目。
	Environments controllers.RuntimeEnvironmentController
	// Transactions 提供跨条目默认值替换的事务边界。
	Transactions controllers.TransactionController
	// Events 在启动探测结束后唤醒已有的运行时资源订阅。
	Events events.Publisher
}

// service 是运行时环境服务的生命周期和并发协调实现。
type service struct {
	// accounts 生成默认账号的受管探测配置。
	accounts *aiaccount.Service
	// accountWorkspaceDirectory 是认证探测的受管目录。
	accountWorkspaceDirectory string
	// runtimeCatalog 提供 Runtime 摘要和 ACP 探测。
	runtimeCatalog runtimeCatalog
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
	// Runtimes 返回全部可持久化的 Runtime 摘要。
	Runtimes() []runtimes.RuntimeSummary
	// ResolveExecutable 仅检查 CLI 是否存在，不依赖账号准备。
	ResolveExecutable(runtimes.DetectRequest) (string, error)
	// Detect 对指定 Runtime 执行不触发模型推理的能力探测。
	Detect(context.Context, runtimes.DetectRequest) runtimes.DetectionResult
	// DiscoverInstallations 枚举可切换的 CLI 安装路径及版本。
	DiscoverInstallations(context.Context, runtimes.DetectRequest, []runtimes.CLIInstallation) []runtimes.CLIInstallation
}

var _ Service = (*service)(nil)

// New 创建尚未启动后台生命周期的运行时环境服务。
func New(config Config) (Service, error) {
	if config.RuntimeSDK == nil {
		return nil, status_error.RuntimeUnavailable
	}
	created, err := newWithRuntimeCatalog(config.RuntimeSDK, config.Environments, config.Transactions)
	if err != nil {
		return nil, err
	}
	concrete := created.(*service)
	concrete.accounts = config.Accounts
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
	// 启动准入只读取持久化安装证据，不等待 CLI 或模型进程。
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
	for _, environment := range previous {
		if environment.Detection.ResolvedBinary != "" {
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
		s.eventPublisher.Publish(events.Event{Topic: "runtimes", Type: "runtimes.changed", Resource: events.Resource{Kind: "global"}})
	}
}

// Close 取消后台探测并等待所有资源退出。
func (s *service) Close() {
	if s == nil || s.cancelLifecycle == nil {
		return
	}
	s.cancelLifecycle()
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
			s.eventPublisher.Publish(events.Event{Topic: "runtimes", Type: "runtimes.changed", Resource: events.Resource{Kind: "global"}})
		}
	}()
	return s.refreshAllLocked(ctx)
}

// RefreshOne 重新探测指定 Runtime。
func (s *service) RefreshOne(ctx context.Context, runtimeID string) error {
	runtimeID = strings.TrimSpace(runtimeID)
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
	lock.Lock()
	defer lock.Unlock()
	return s.refreshOneLocked(ctx, runtimeID)
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
	var summary runtimes.RuntimeSummary
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
	summary runtimes.RuntimeSummary,
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

// persistDetection 原子保存探测快照，在目录刷新失败时保留上次结果，并清理失效默认选择。
func (s *service) persistDetection(
	ctx context.Context,
	environment models.RuntimeEnvironment,
	detection runtimes.DetectionResult,
	detectedAt int64,
) error {
	detection = runtimes.PreservePreviousOptionsOnFailure(
		environment.Detection,
		detection,
	)
	defaultModel, defaultReasoning := runtimes.NormalizeDetectedDefaults(
		detection,
		environment.CustomModels,
		environment.DefaultModel,
		environment.DefaultReasoning,
	)
	nextPermission, degraded := runtimes.SafePermissionLevel(
		environment.DefaultPermissionLevel,
		runtimes.ModelPermissionLevels(
			detection,
			environment.CustomModels,
			defaultModel,
		),
	)
	if degraded {
		detection.Diagnostics = append(detection.Diagnostics, runtimes.Diagnostic{
			Code:  runtimes.DiagnosticCodePermissionDefaultDowngraded,
			Level: runtimes.DiagnosticLevelWarning,
			Message: "default permission level was reduced to a " +
				"level the current model can express",
		})
	}
	if err := s.environments.UpdateDetection(
		ctx,
		environment.RuntimeID,
		detection,
		detectedAt,
	); err != nil {
		return err
	}
	fields := map[string]any{}
	if environment.Binary == firstBinary(environment.Detection.Runtime.BinaryCandidates) && detection.ResolvedBinary != "" {
		fields["binary"] = detection.ResolvedBinary
	}
	if defaultModel != environment.DefaultModel {
		fields["default_model"] = defaultModel
	}
	if defaultReasoning != environment.DefaultReasoning {
		fields["default_reasoning"] = defaultReasoning
	}
	if nextPermission != environment.DefaultPermissionLevel {
		fields["default_permission_level"] = nextPermission
	}
	if len(fields) == 0 {
		return nil
	}
	return s.environments.UpdateFields(ctx, environment.RuntimeID, fields)
}

func (s *service) detect(
	ctx context.Context,
	summary runtimes.RuntimeSummary,
	environment models.RuntimeEnvironment,
) runtimes.DetectionResult {
	request := runtimeDetectRequest(summary, environment)
	if request.LaunchOverride.Binary == firstBinary(summary.BinaryCandidates) && environment.Detection.ResolvedBinary != "" {
		request.LaunchOverride.Binary = environment.Detection.ResolvedBinary
	}
	finish := func(result runtimes.DetectionResult) runtimes.DetectionResult {
		result.Version = runtimes.NormalizeRuntimeVersion(result.Version)
		result.Installations = s.runtimeCatalog.DiscoverInstallations(ctx, request, environment.Detection.Installations)
		// 可执行检查短暂失败时只保留明确选中的同一路径版本，不恢复安装或就绪状态。
		if result.Version == "" && result.ResolvedBinary == "" && environment.Detection.ResolvedBinary != "" && request.LaunchOverride.Binary == environment.Detection.ResolvedBinary {
			result.Version = runtimes.NormalizeRuntimeVersion(environment.Detection.Version)
		}
		if result.ResolvedBinary != "" {
			found := false
			for index := range result.Installations {
				if result.Installations[index].Path == result.ResolvedBinary {
					found = true
					if result.Version == "" {
						result.Version = runtimes.NormalizeRuntimeVersion(result.Installations[index].Version)
					}
					if result.Version == "" && result.ResolvedBinary == environment.Detection.ResolvedBinary {
						result.Version = runtimes.NormalizeRuntimeVersion(environment.Detection.Version)
					}
					result.Installations[index].Version = result.Version
					break
				}
			}
			if !found {
				if result.Version == "" && result.ResolvedBinary == environment.Detection.ResolvedBinary {
					result.Version = runtimes.NormalizeRuntimeVersion(environment.Detection.Version)
				}
				result.Installations = append(result.Installations, runtimes.CLIInstallation{
					Path: result.ResolvedBinary, Version: result.Version, Available: true,
				})
			}
		}
		return result
	}
	if environment.DefaultAccountID != "" && s.accounts != nil {
		workspace := filepath.Join(s.accountWorkspaceDirectory, summary.ID)
		directoryErr := os.MkdirAll(workspace, 0700)
		var target runtimes.RuntimeTarget
		var release func()
		err := directoryErr
		if err == nil {
			model := environment.DefaultModel
			if accountconfig.IsNativeAccountRuntime(summary.BaseRuntimeID) {
				account, accountErr := s.accounts.Get(ctx, environment.DefaultAccountID)
				if accountErr != nil {
					err = accountErr
				} else if len(account.SelectedModels) > 0 {
					selected := false
					for _, id := range account.SelectedModels {
						selected = selected || id == model
					}
					if !selected {
						model = account.SelectedModels[0]
					}
				}
			}
			if err == nil {
				target, release, err = s.accounts.Prepare(ctx, environment.DefaultAccountID, summary.BaseRuntimeID, workspace, model)
			}
			request.WorkingDirectory = workspace
			if accountconfig.IsNativeAccountRuntime(summary.BaseRuntimeID) {
				request.Model = accountconfig.NativeModelID(summary.BaseRuntimeID, model)
			}
		}
		if err != nil {
			result := environment.Detection
			result.ResolvedBinary, _ = s.runtimeCatalog.ResolveExecutable(request)
			if result.ResolvedBinary != environment.Detection.ResolvedBinary {
				result.Version = ""
			}
			if result.State == "" || result.ResolvedBinary == "" {
				result.State = runtimes.DetectionStateUnavailable
			}
			result.Auth = runtimes.AuthStatusMissing
			result.Diagnostics = append(result.Diagnostics, runtimes.Diagnostic{Code: constants.AIAccountDiagnosticUnavailable, Level: runtimes.DiagnosticLevelWarning, Message: "account configuration is unavailable"})
			return finish(result)
		}
		defer release()
		request.BaseEnvironment = accountconfig.Environment(request.BaseEnvironment)
		request.LaunchOverride.Environment = target.Environment
	}
	result := s.runtimeCatalog.Detect(ctx, request)
	if environment.DefaultAccountID != "" {
		for index := range result.Models {
			result.Models[index].ID = accountconfig.AccountModelID(summary.BaseRuntimeID, result.Models[index].ID)
		}
		result.DetectedDefaultModel = accountconfig.AccountModelID(summary.BaseRuntimeID, result.DetectedDefaultModel)
	}
	return finish(result)
}

// runtimeDetectRequest 把持久化配置投影为 ACP Agent 构造使用的受控启动输入。
func runtimeDetectRequest(
	summary runtimes.RuntimeSummary,
	environment models.RuntimeEnvironment,
) runtimes.DetectRequest {
	launchOverride := runtimes.LaunchOverride{
		Binary:      environment.Binary,
		PrefixArgs:  append([]string(nil), environment.PrefixArgs...),
		Environment: map[string]string{},
	}
	if environment.ConfigHome != "" && summary.ConfigHomeEnvironmentVariable != "" {
		launchOverride.Environment[summary.ConfigHomeEnvironmentVariable] = environment.ConfigHome
	}
	homeDirectory, _ := os.UserHomeDir()
	return runtimes.DetectRequest{
		RuntimeID:       summary.ID,
		BaseEnvironment: os.Environ(),
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
		for _, environment := range environments {
			if environment.IsDefault && environment.Enabled && environment.Detection.State == runtimes.DetectionStateReady {
				return nil
			}
		}
		for _, environment := range environments {
			if !environment.Enabled || environment.Detection.State != runtimes.DetectionStateReady {
				continue
			}
			if err := s.environments.ClearDefault(transactionContext); err != nil {
				return err
			}
			return s.environments.UpdateFields(transactionContext, environment.RuntimeID, map[string]any{"is_default": true})
		}
		return s.environments.ClearDefault(transactionContext)
	})
}

func newEnvironment(summary runtimes.RuntimeSummary) models.RuntimeEnvironment {
	return models.RuntimeEnvironment{
		ID:                     util.New(),
		RuntimeID:              summary.ID,
		BaseRuntimeID:          summary.BaseRuntimeID,
		Name:                   summary.Name,
		Enabled:                true,
		Binary:                 firstBinary(summary.BinaryCandidates),
		PrefixArgs:             append([]string(nil), summary.PrefixArgs...),
		CustomModels:           []string{},
		DefaultModel:           summary.DefaultModel,
		DefaultReasoning:       firstReasoning(summary.ReasoningOptions),
		DefaultPermissionLevel: runtimes.PermissionLevelFullAccess,
		Detection: runtimes.DetectionResult{
			Runtime: summary,
			Auth:    runtimes.AuthStatusUnknown,
		},
	}
}

func firstBinary(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func firstReasoning(options []runtimes.ReasoningOption) string {
	if len(options) == 0 {
		return ""
	}
	return options[0].ID
}
