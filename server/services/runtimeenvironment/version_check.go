package runtimeenvironment

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/installation"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
)

// checkInstalledRuntimeUpdates 检查本次启动已探测到的程序，同一基础智能体优先使用内置配置。
func (s *service) checkInstalledRuntimeUpdates(ctx context.Context, probeStartedAt int64) {
	if s.installer == nil || ctx.Err() != nil {
		return
	}
	environments, err := s.environments.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logx.Warnf("list installed runtimes for startup update check: error=%v", err)
		}
		return
	}
	selected := make(map[string]string)
	for _, environment := range s.registeredEnvironments(environments) {
		id, base := environment.RuntimeID, environment.BaseRuntimeID
		if environment.DetectedAt < probeStartedAt || environment.Detection.ResolvedBinary == "" || !s.installer.Supported(base) {
			continue
		}
		if selected[base] == "" || id == base {
			selected[base] = id
		}
	}
	var checks sync.WaitGroup
	for _, id := range selected {
		checks.Go(func() {
			if err := s.CheckRuntimeUpdates(ctx, id); err != nil && ctx.Err() == nil && !errors.Is(err, status_error.RuntimeUpdateCheckBusy) {
				logx.Warnf("check runtime update on startup: runtime_id=%s error=%v", id, err)
			}
		})
	}
	checks.Wait()
}

func applyUpdateAvailability(provider string, snapshot *RuntimeInstallationSnapshot) {
	snapshot.UpdateAvailable = nil
	if snapshot.CheckState == "" {
		snapshot.CheckState = constants.RuntimeUpdateCheckStateUnknown
	}
	if snapshot.CheckState != constants.RuntimeUpdateCheckStateChecked || !snapshot.Installed {
		return
	}
	if order, known := installation.CompareVersions(provider, snapshot.Version, snapshot.LatestVersion); known {
		available := order < 0
		snapshot.UpdateAvailable = &available
	}
}

// CheckRuntimeUpdates 读取官方元数据并保存检查事实，不下载或执行新程序。
func (s *service) CheckRuntimeUpdates(ctx context.Context, runtimeID string) error {
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
	s.installationMutex.Lock()
	if s.installationClosing {
		s.installationMutex.Unlock()
		return status_error.RuntimeEnvironmentServiceUnavailable
	}
	job := s.installationJobs[base]
	if s.restarting[base] || job != nil && (job.cancel != nil || job.snapshot.CheckState == constants.RuntimeUpdateCheckStateChecking) {
		s.installationMutex.Unlock()
		return status_error.RuntimeUpdateCheckBusy
	}
	if s.installationJobs == nil {
		s.installationJobs = make(map[string]*runtimeInstallationJob)
	}
	if job == nil {
		job = &runtimeInstallationJob{snapshot: previous}
		s.installationJobs[base] = job
	}
	// 官方版本归基础智能体，所选配置和同一基础智能体的订阅都需要对账。
	for _, id := range []string{runtimeID, base} {
		if !slices.Contains(job.runtimeIDs, id) {
			job.runtimeIDs = append(job.runtimeIDs, id)
		}
	}
	job.snapshot.CheckState, job.snapshot.CheckErrorCode, job.snapshot.UpdateAvailable = constants.RuntimeUpdateCheckStateChecking, "", nil
	job.snapshot.Revision++
	job.snapshot.UpdatedAt = time.Now().UnixMilli()
	s.waitGroup.Add(1)
	s.installationMutex.Unlock()
	defer s.waitGroup.Done()
	s.publishInstallation(base)
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if s.lifecycleContext != nil {
		stop := context.AfterFunc(s.lifecycleContext, cancel)
		defer stop()
	}
	var local agent.CLIInstallation
	var localErr error
	if previous.Source == constants.RuntimeInstallationSourceManaged {
		local = agent.CLIInstallation{Path: environment.Detection.ResolvedBinary, Version: previous.Version, Available: previous.Installed}
	} else {
		request := runtimeDetectRequest(environment.Detection.Runtime, environment)
		request.RuntimeID = runtimeID
		local, localErr = s.runtimeCatalog.ProbeInstallationVersion(checkCtx, request)
		if localErr == nil && local.Version == "" {
			localErr = status_error.RuntimeVersionUnknown
		}
	}
	release, checkErr := s.installer.CheckVersion(checkCtx, base)
	if checkErr == nil && localErr != nil {
		checkErr = localErr
	}
	s.installationMutex.Lock()
	job.snapshot.Revision++
	job.snapshot.UpdatedAt = time.Now().UnixMilli()
	job.checkedBinary, job.checkedLocalVersion, job.checkedLocalAvailable = local.Path, local.Version, local.Available
	if release.Version != "" {
		job.snapshot.LatestVersion, job.snapshot.CheckedAt = release.Version, time.Now().UnixMilli()
	}
	if checkErr == nil {
		job.snapshot.CheckState = constants.RuntimeUpdateCheckStateChecked
	} else {
		job.snapshot.CheckState, job.snapshot.CheckErrorCode = constants.RuntimeUpdateCheckStateFailed, status_error.RuntimeInstallationFailed.Key()
		var status status_error.StatusError
		if errors.As(checkErr, &status) {
			job.snapshot.CheckErrorCode = status.Key()
		}
	}
	s.installationMutex.Unlock()
	s.publishInstallation(base)
	return checkErr
}
