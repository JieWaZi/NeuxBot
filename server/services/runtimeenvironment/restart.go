package runtimeenvironment

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// RestartRuntime 只在没有活动原生工作时关闭旧进程，随后通过现有目录连接重新验证程序。
func (s *service) RestartRuntime(ctx context.Context, runtimeID string) error {
	environment, err := s.environments.Get(ctx, runtimeID)
	if err != nil {
		return err
	}
	if s.restartProvider == nil {
		return status_error.RuntimeEnvironmentServiceUnavailable
	}
	base := environment.BaseRuntimeID
	s.installationMutex.Lock()
	if s.authentication != nil && s.authentication.HasNativeOwner(base) {
		s.installationMutex.Unlock()
		return status_error.AIAccountBusy
	}
	if s.installationClosing || s.restarting[base] {
		s.installationMutex.Unlock()
		return status_error.RuntimeRestartBusy
	}
	if job := s.installationJobs[base]; job != nil {
		if job.cancel != nil {
			s.installationMutex.Unlock()
			return status_error.RuntimeInstallationBusy
		}
		if job.snapshot.CheckState == constants.RuntimeUpdateCheckStateChecking {
			s.installationMutex.Unlock()
			return status_error.RuntimeUpdateCheckBusy
		}
	}
	if s.restarting == nil {
		s.restarting = make(map[string]bool)
	}
	s.restarting[base] = true
	s.waitGroup.Add(1)
	s.installationMutex.Unlock()
	defer func() {
		s.installationMutex.Lock()
		delete(s.restarting, base)
		s.installationMutex.Unlock()
		s.waitGroup.Done()
	}()
	if err := s.restartProvider(ctx, base); err != nil {
		return err
	}
	return s.RefreshOne(ctx, runtimeID)
}
