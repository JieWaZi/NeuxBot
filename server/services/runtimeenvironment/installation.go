package runtimeenvironment

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/installation"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// 智能体安装状态
type RuntimeInstallationSnapshot struct {
	// 当前维护操作
	Operation string `json:"operation"`
	// 安装方式
	InstallationMethod string `json:"installationMethod"`
	// 程序安装位置
	InstallationPath string `json:"installationPath"`
	// 允许安装
	CanInstall bool `json:"canInstall"`
	// 允许更新
	CanUpdate bool `json:"canUpdate"`
	// 允许卸载
	CanUninstall bool `json:"canUninstall"`
	// 维护限制原因
	MaintenanceReason string `json:"maintenanceReason,omitempty"`
	// 官方维护说明
	OfficialURL string `json:"officialUrl,omitempty"`
	// 程序来源
	Source constants.RuntimeInstallationSource `json:"source"`
	// 官方最新版本
	LatestVersion string `json:"latestVersion"`
	// 检查状态
	CheckState constants.RuntimeUpdateCheckState `json:"checkState"`
	// 成功检查时间
	CheckedAt int64 `json:"checkedAt"`
	// 存在可用更新
	UpdateAvailable *bool `json:"updateAvailable"`
	// 检查失败代码
	CheckErrorCode string `json:"checkErrorCode,omitempty"`
	// 失败操作
	ErrorOperation string `json:"errorOperation,omitempty"`
	// 失败原因
	ErrorReason string `json:"errorReason,omitempty"`
	// 上游响应状态
	ErrorStatus int `json:"errorStatus,omitempty"`
	// 智能体标识
	RuntimeID string `json:"runtimeId"`
	// 支持受管安装
	Supported bool `json:"supported"`
	// 安装状态
	State constants.RuntimeInstallationState `json:"state"`
	// 当前阶段
	Phase string `json:"phase"`
	// 下载进度
	Progress int `json:"progress"`
	// 已下载字节
	ReceivedBytes int64 `json:"receivedBytes"`
	// 总下载字节
	TotalBytes int64 `json:"totalBytes"`
	// 允许取消
	Cancelable bool `json:"cancelable"`
	// 失败代码
	ErrorCode string `json:"errorCode,omitempty"`
	// 已安装程序
	Installed bool `json:"installed"`
	// 已安装版本
	Version string `json:"version"`
	// 更新时间
	UpdatedAt int64 `json:"updatedAt"`
	// 快照修订
	Revision uint64 `json:"revision"`
}

// runtimeInstaller 是安装、只读恢复与平台支持的真实程序存储端口。
type runtimeInstaller interface {
	// Inspect 只读识别当前程序归属与可用维护动作。
	Inspect(context.Context, string, string, []string) (installation.Maintenance, error)
	// InstallAt 依据已核实的安装方式安装或替换当前程序。
	InstallAt(context.Context, string, string, []string, func(installation.Progress)) (installation.Result, error)
	// Uninstall 只移除已核实归属的程序，保留所有账号和配置。
	Uninstall(context.Context, string, string, []string) error
	// Installed 只读返回已发布程序，不发起网络请求。
	Installed(string) (installation.Result, error)
	// Supported 返回当前平台的安装支持。
	Supported(string) bool
	// CheckVersion 只解析官方版本元数据，不下载程序。
	CheckVersion(context.Context, string) (installation.Release, error)
}

type runtimeInstallationJob struct {
	// binary 固定接纳时实际选择的程序，维护中不跟随探测漂移。
	binary string
	// prefixArgs 保留所选程序的内部启动适配参数。
	prefixArgs []string
	// checkedBinary 将显式本机版本观察绑定到该程序路径。
	checkedBinary string
	// checkedLocalVersion 保存显式版本检查的原生只读回执。
	checkedLocalVersion string
	// checkedLocalAvailable 表示本机版本检查时实际找到了程序。
	checkedLocalAvailable bool
	// runtimeIDs 固定本次程序任务影响的配置身份，进度发布不查询数据库。
	runtimeIDs []string
	// snapshot 保存该基础智能体最近一次任务的可恢复视图。
	snapshot RuntimeInstallationSnapshot
	// cancel 中止当前下载、验证及其进程。
	cancel context.CancelFunc
	// lastPublished 限制相同阶段的进度事件频率。
	lastPublished time.Time
}

// InstallationSnapshot 读取当前任务及本机已发布程序，不查询官方版本。
func (s *service) InstallationSnapshot(ctx context.Context, runtimeID string) (RuntimeInstallationSnapshot, error) {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return RuntimeInstallationSnapshot{}, err
	}
	base := environment.BaseRuntimeID
	snapshot := RuntimeInstallationSnapshot{RuntimeID: runtimeID, State: constants.RuntimeInstallationStateIdle, CheckState: constants.RuntimeUpdateCheckStateUnknown, Source: constants.RuntimeInstallationSourceUnknown}
	s.installationMutex.Lock()
	var checkedBinary, checkedVersion string
	var checkedAvailable bool
	if job := s.installationJobs[base]; job != nil {
		snapshot = job.snapshot
		checkedBinary, checkedVersion, checkedAvailable = job.checkedBinary, job.checkedLocalVersion, job.checkedLocalAvailable
		snapshot.RuntimeID = runtimeID
		if job.cancel != nil {
			s.installationMutex.Unlock()
			applyUpdateAvailability(environment.BaseRuntimeID, &snapshot)
			return snapshot, nil
		}
	}
	s.installationMutex.Unlock()
	snapshot.Installed = environment.Detection.ResolvedBinary != ""
	snapshot.Version = agent.NormalizeRuntimeVersion(environment.Detection.Version)
	if checkedBinary != "" && checkedBinary == environment.Detection.ResolvedBinary && snapshot.CheckedAt >= environment.DetectedAt {
		snapshot.Version, snapshot.Installed = checkedVersion, checkedAvailable
	}
	if snapshot.Installed {
		snapshot.Source = constants.RuntimeInstallationSourceSystem
		if snapshot.State == constants.RuntimeInstallationStateIdle {
			snapshot.State = constants.RuntimeInstallationStateInstalled
		}
	}
	if s.installer == nil {
		applyUpdateAvailability(environment.BaseRuntimeID, &snapshot)
		return snapshot, nil
	}
	snapshot.Supported = s.installer.Supported(base)
	maintenance, inspectErr := s.installer.Inspect(ctx, base, environment.Detection.ResolvedBinary, s.programSelectionArguments(environment))
	if inspectErr != nil {
		return RuntimeInstallationSnapshot{}, inspectErr
	}
	snapshot.InstallationMethod, snapshot.InstallationPath = maintenance.Method, maintenance.Path
	snapshot.Installed = maintenance.Installed
	if !snapshot.Installed {
		snapshot.Version, snapshot.Source = "", constants.RuntimeInstallationSourceUnknown
		if snapshot.State == constants.RuntimeInstallationStateInstalled {
			snapshot.State = constants.RuntimeInstallationStateIdle
		}
	}
	snapshot.CanInstall, snapshot.CanUpdate, snapshot.CanUninstall = maintenance.CanInstall, maintenance.CanUpdate, maintenance.CanUninstall
	snapshot.MaintenanceReason, snapshot.OfficialURL = maintenance.Reason, maintenance.OfficialURL
	if maintenance.Reason == "cleanup_pending" {
		// 文件提交已开始但清理尚未完成；重启后也只允许继续原卸载。
		snapshot.Operation, snapshot.State, snapshot.Phase = "uninstall", constants.RuntimeInstallationStateFailed, "uninstalling"
		snapshot.Source = constants.RuntimeInstallationSourceManaged
		snapshot.CanInstall, snapshot.CanUpdate, snapshot.CanUninstall = false, false, true
		if snapshot.ErrorCode == "" {
			snapshot.ErrorCode = status_error.RuntimeInstallationFailed.Key()
		}
		if snapshot.ErrorOperation == "" {
			snapshot.ErrorOperation = "uninstalling"
		}
		if snapshot.ErrorReason == "" {
			snapshot.ErrorReason = "filesystem"
		}
		applyUpdateAvailability(environment.BaseRuntimeID, &snapshot)
		return snapshot, nil
	}
	if !snapshot.Supported {
		applyUpdateAvailability(environment.BaseRuntimeID, &snapshot)
		return snapshot, nil
	}
	// 私有副本的存在不能替换用户当前选择的系统程序。
	if environment.Detection.ResolvedBinary != "" && maintenance.Method != "managed" {
		applyUpdateAvailability(environment.BaseRuntimeID, &snapshot)
		return snapshot, nil
	}
	result, err := s.installer.Installed(base)
	if err == nil {
		snapshot.Installed, snapshot.Version = true, result.Version
		snapshot.Source = constants.RuntimeInstallationSourceManaged
		if snapshot.State == constants.RuntimeInstallationStateIdle {
			snapshot.State = constants.RuntimeInstallationStateInstalled
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		snapshot.Installed, snapshot.Version = false, ""
		snapshot.Source = constants.RuntimeInstallationSourceManaged
		snapshot.State, snapshot.ErrorCode = constants.RuntimeInstallationStateFailed, status_error.RuntimeInstallationFailed.Key()
	} else {
		if !snapshot.Installed && snapshot.State == constants.RuntimeInstallationStateInstalled {
			snapshot.State = constants.RuntimeInstallationStateIdle
		}
	}
	applyUpdateAvailability(environment.BaseRuntimeID, &snapshot)
	return snapshot, nil
}

// InstallRuntime 接纳独立于 HTTP 请求的受管安装或更新，同一基础智能体只运行一个任务。
func (s *service) InstallRuntime(ctx context.Context, runtimeID string) error {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return err
	}
	base := environment.BaseRuntimeID
	if s.installer == nil || !s.installer.Supported(base) {
		return status_error.RuntimeInstallationUnsupported
	}
	previous, err := s.InstallationSnapshot(ctx, runtimeID)
	if err != nil {
		return err
	}
	if previous.State == constants.RuntimeInstallationStateInstalling {
		return status_error.RuntimeInstallationBusy
	}
	if !previous.CanInstall && !previous.CanUpdate {
		return status_error.RuntimeMaintenanceUnsupported
	}
	if previous.MaintenanceReason != "repairable" && previous.Installed && previous.State != constants.RuntimeInstallationStateFailed {
		if err := s.CheckRuntimeUpdates(ctx, runtimeID); err != nil {
			return err
		}
		previous, err = s.InstallationSnapshot(ctx, runtimeID)
		if err != nil {
			return err
		}
		if previous.UpdateAvailable == nil {
			return status_error.RuntimeVersionUnknown
		}
		if !*previous.UpdateAvailable {
			return nil
		}
	}
	return s.startRuntimeMaintenance(ctx, environment, previous, "install")
}

// UninstallRuntime 接纳独立于请求的程序卸载，仅处理已核实的基础程序归属。
func (s *service) UninstallRuntime(ctx context.Context, runtimeID string) error {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return err
	}
	previous, err := s.InstallationSnapshot(ctx, runtimeID)
	if err != nil {
		return err
	}
	if previous.State == constants.RuntimeInstallationStateInstalling {
		return status_error.RuntimeInstallationBusy
	}
	if !previous.CanUninstall {
		return status_error.RuntimeMaintenanceUnsupported
	}
	return s.startRuntimeMaintenance(ctx, environment, previous, "uninstall")
}

func (s *service) startRuntimeMaintenance(ctx context.Context, environment models.RuntimeEnvironment, previous RuntimeInstallationSnapshot, operation string) error {
	base := environment.BaseRuntimeID
	environments, err := s.environments.List(ctx)
	if err != nil {
		return err
	}
	var affected []string
	for _, selected := range environments {
		if selected.BaseRuntimeID == base {
			affected = append(affected, selected.RuntimeID)
		}
	}
	s.installationMutex.Lock()
	if s.restarting[base] {
		s.installationMutex.Unlock()
		return status_error.RuntimeRestartBusy
	}
	if s.authentication != nil && s.authentication.HasNativeOwner(base) {
		s.installationMutex.Unlock()
		return status_error.AIAccountBusy
	}
	if s.installationClosing || s.lifecycleContext == nil || s.lifecycleContext.Err() != nil {
		s.installationMutex.Unlock()
		return status_error.RuntimeEnvironmentServiceUnavailable
	}
	if current := s.installationJobs[base]; current != nil {
		if current.cancel != nil {
			s.installationMutex.Unlock()
			return status_error.RuntimeInstallationBusy
		}
		if current.snapshot.CheckState == constants.RuntimeUpdateCheckStateChecking {
			s.installationMutex.Unlock()
			return status_error.RuntimeUpdateCheckBusy
		}
	}
	if s.installationJobs == nil {
		s.installationJobs = make(map[string]*runtimeInstallationJob)
	}
	taskContext, cancel := context.WithTimeout(s.lifecycleContext, 30*time.Minute)
	previous.Operation = operation
	previous.State, previous.Phase = constants.RuntimeInstallationStateInstalling, "resolving"
	if operation == "uninstall" {
		previous.Phase = "uninstalling"
	}
	previous.ErrorCode, previous.ErrorOperation, previous.ErrorReason, previous.ErrorStatus = "", "", "", 0
	previous.Progress, previous.ReceivedBytes, previous.TotalBytes = 0, 0, 0
	previous.Cancelable, previous.UpdatedAt, previous.Revision = true, time.Now().UnixMilli(), previous.Revision+1
	job := &runtimeInstallationJob{snapshot: previous, cancel: cancel, runtimeIDs: affected, binary: environment.Detection.ResolvedBinary, prefixArgs: s.programSelectionArguments(environment)}
	s.installationJobs[base] = job
	s.waitGroup.Add(1)
	s.installationMutex.Unlock()
	s.publishInstallation(base)
	go s.runInstallation(taskContext, base, job)
	return nil
}

// CancelRuntimeInstallation 请求取消当前基础智能体任务，终态只在回收完成后发布。
func (s *service) CancelRuntimeInstallation(ctx context.Context, runtimeID string) error {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return err
	}
	s.installationMutex.Lock()
	defer s.installationMutex.Unlock()
	if job := s.installationJobs[environment.BaseRuntimeID]; job != nil && job.cancel != nil && job.snapshot.Cancelable {
		job.cancel()
	}
	return nil
}

func (s *service) runInstallation(ctx context.Context, base string, job *runtimeInstallationJob) {
	defer s.waitGroup.Done()
	defer job.cancel()
	var result installation.Result
	var err error
	var release func()
	if s.beforeRuntimeMaintenance == nil {
		err = status_error.RuntimeEnvironmentServiceUnavailable
	} else {
		release, err = s.beforeRuntimeMaintenance(ctx, base)
	}
	if release != nil {
		defer release()
	}
	progress := func(progress installation.Progress) {
		s.installationMutex.Lock()
		changedPhase := job.snapshot.Phase != progress.Phase
		job.snapshot.Phase = progress.Phase
		job.snapshot.ReceivedBytes, job.snapshot.TotalBytes = progress.ReceivedBytes, progress.TotalBytes
		if progress.TotalBytes > 0 {
			job.snapshot.Progress = int(min(int64(99), progress.ReceivedBytes*100/progress.TotalBytes))
		}
		job.snapshot.Revision++
		job.snapshot.UpdatedAt = time.Now().UnixMilli()
		publish := changedPhase || time.Since(job.lastPublished) >= 200*time.Millisecond
		if publish {
			job.lastPublished = time.Now()
		}
		s.installationMutex.Unlock()
		if publish {
			s.publishInstallation(base)
		}
	}
	if err == nil {
		if job.snapshot.Operation == "uninstall" {
			err = s.installer.Uninstall(ctx, base, job.binary, job.prefixArgs)
		} else {
			result, err = s.installer.InstallAt(ctx, base, job.binary, job.prefixArgs, progress)
			if err == nil && result.Binary == "" {
				err = status_error.RuntimeInstallationFailed.WithParams(map[string]interface{}{"operation": "verifying", "reason": "source", "status": 0})
			}
			if err == nil && job.snapshot.LatestVersion != "" {
				order, known := installation.CompareVersions(base, result.Version, job.snapshot.LatestVersion)
				if !known || order < 0 {
					err = status_error.RuntimeInstallationFailed.WithParams(map[string]interface{}{"operation": "verifying", "reason": "version", "status": 0})
				}
			}
		}
	}
	if err == nil {
		s.installationMutex.Lock()
		job.snapshot.Phase, job.snapshot.Installed, job.snapshot.Version = "detecting", job.snapshot.Operation != "uninstall", result.Version
		if job.snapshot.Operation == "uninstall" {
			job.snapshot.Source = constants.RuntimeInstallationSourceUnknown
		} else if job.snapshot.Source == constants.RuntimeInstallationSourceUnknown || job.snapshot.InstallationMethod == "managed" {
			job.snapshot.Source = constants.RuntimeInstallationSourceManaged
		}
		job.snapshot.Cancelable = false
		job.snapshot.Revision++
		job.snapshot.UpdatedAt = time.Now().UnixMilli()
		s.installationMutex.Unlock()
		s.publishInstallation(base)
		// 同一基础程序的全部配置重读新版本；账号目录仍分别由原租约持有。
		var environments []models.RuntimeEnvironment
		environments, err = s.environments.List(ctx)
		if err == nil {
			err = s.transactions.Transaction(ctx, func(transactionContext context.Context) error {
				summaries := make(map[string]agent.RuntimeSummary)
				for _, summary := range s.runtimeCatalog.Runtimes() {
					summaries[summary.ID] = summary
				}
				for _, selected := range environments {
					if selected.BaseRuntimeID != base {
						continue
					}
					summary, exists := summaries[selected.RuntimeID]
					if !exists {
						return status_error.RuntimeUnavailable
					}
					// 文件提交后、围栏释放前统一所有配置的真实启动事实，默认执行设置保留。
					detection := agent.DetectionResult{Runtime: summary}
					if job.snapshot.Operation != "uninstall" {
						detection = selected.Detection
						detection.Runtime = summary
						detection.Runtime.PrefixArgs = append(append([]string(nil), result.PrefixArgs...), summary.PrefixArgs...)
						detection.ResolvedBinary, detection.Version = result.Binary, result.Version
					}
					if err := s.environments.UpdateDetection(transactionContext, selected.RuntimeID, detection, time.Now().UnixMilli()); err != nil {
						return err
					}
				}
				return nil
			})
		}
		if release != nil {
			release()
		}
		for _, selected := range environments {
			if err == nil && selected.BaseRuntimeID == base {
				if job.snapshot.Operation == "uninstall" {
					err = s.refreshAfterProgramRemoval(ctx, selected.RuntimeID)
				} else {
					err = s.RefreshOne(ctx, selected.RuntimeID)
				}
			}
		}
	}
	if release != nil {
		release()
	}
	s.installationMutex.Lock()
	job.snapshot.Cancelable, job.snapshot.UpdatedAt = false, time.Now().UnixMilli()
	job.snapshot.Revision++
	if err == nil {
		job.snapshot.State, job.snapshot.Progress = constants.RuntimeInstallationStateInstalled, 100
		if job.snapshot.Operation == "uninstall" {
			job.snapshot.State = constants.RuntimeInstallationStateIdle
			job.snapshot.Source = constants.RuntimeInstallationSourceUnknown
			job.snapshot.LatestVersion, job.snapshot.CheckErrorCode = "", ""
			job.snapshot.CheckState, job.snapshot.CheckedAt, job.snapshot.UpdateAvailable = constants.RuntimeUpdateCheckStateUnknown, 0, nil
		}
		job.snapshot.Phase = ""
	} else if errors.Is(err, context.Canceled) || errors.Is(err, status_error.OperationCanceled) {
		job.snapshot.State, job.snapshot.ErrorCode = constants.RuntimeInstallationStateCancelled, status_error.OperationCanceled.Key()
	} else {
		job.snapshot.State, job.snapshot.ErrorCode = constants.RuntimeInstallationStateFailed, status_error.RuntimeInstallationFailed.Key()
		var status status_error.StatusError
		if errors.As(err, &status) {
			job.snapshot.ErrorCode = status.Key()
		} else if errors.Is(err, context.DeadlineExceeded) {
			job.snapshot.ErrorCode = status_error.OperationTimeout.Key()
		}
		job.snapshot.ErrorOperation, job.snapshot.ErrorReason, job.snapshot.ErrorStatus = installation.Diagnosis(err)
		logx.Warnf("runtime installation settled: base=%s phase=%s status=%s error_type=%T reason=%s http_status=%d", base, job.snapshot.Phase, job.snapshot.State, err, job.snapshot.ErrorReason, job.snapshot.ErrorStatus)
	}
	job.cancel = nil
	s.installationMutex.Unlock()
	s.publishInstallation(base)
	if s.eventPublisher != nil {
		s.eventPublisher.Publish(events.Event{Topic: constants.RuntimeEventTopic, Type: constants.RuntimeContractChangedEventType, Resource: events.Resource{Kind: "global"}})
	}
}

// programSelectionArguments 从程序归属判断中去掉 Catalog 已知的配置参数，仅保留解释器入口。
func (s *service) programSelectionArguments(environment models.RuntimeEnvironment) []string {
	arguments := append([]string(nil), environment.PrefixArgs...)
	for _, summary := range s.runtimeCatalog.Runtimes() {
		if summary.ID == environment.RuntimeID && len(summary.PrefixArgs) <= len(arguments) {
			start := len(arguments) - len(summary.PrefixArgs)
			if slices.Equal(arguments[start:], summary.PrefixArgs) {
				if start == 0 {
					return nil
				}
				return arguments[:start]
			}
		}
	}
	return arguments
}

// refreshAfterProgramRemoval 仅重读程序事实，卸载不重写账号、默认模型和用户执行设置。
func (s *service) refreshAfterProgramRemoval(ctx context.Context, runtimeID string) error {
	lock := s.runtimeRefreshLock(runtimeID)
	lock.Lock()
	defer lock.Unlock()
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return err
	}
	for _, summary := range s.runtimeCatalog.Runtimes() {
		if summary.ID == runtimeID {
			result := s.detect(ctx, summary, environment)
			return s.environments.UpdateDetection(ctx, runtimeID, result, time.Now().UnixMilli())
		}
	}
	return status_error.RuntimeUnavailable
}

func (s *service) publishInstallation(base string) {
	if s.eventPublisher != nil {
		s.installationMutex.Lock()
		var runtimeIDs []string
		if job := s.installationJobs[base]; job != nil {
			runtimeIDs = append(runtimeIDs, job.runtimeIDs...)
		}
		s.installationMutex.Unlock()
		for _, runtimeID := range runtimeIDs {
			s.eventPublisher.Publish(events.Event{Topic: constants.RuntimeEventTopic, Type: constants.RuntimeInstallationChangedEventType, Resource: events.Resource{Kind: "runtime", ID: runtimeID}})
		}
	}
}
