package runtimeenvironment

import (
	"context"
	"errors"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/installation"
)

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

// CheckRuntimeUpdates 显式读取官方元数据并保存检查事实，不下载或执行新程序。
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
	if job != nil && (job.cancel != nil || job.snapshot.CheckState == constants.RuntimeUpdateCheckStateChecking) {
		s.installationMutex.Unlock()
		return status_error.RuntimeUpdateCheckBusy
	}
	if s.installationJobs == nil {
		s.installationJobs = make(map[string]*runtimeInstallationJob)
	}
	if job == nil {
		job = &runtimeInstallationJob{snapshot: previous, runtimeIDs: []string{runtimeID}}
		s.installationJobs[base] = job
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
