package global

import (
	"context"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
)

// prepareModelRuntime 为知识文本调用和工作流格式纠正准备可取消的账号租约。
func prepareModelRuntime(ctx context.Context, runtimeID, accountID, model string) (runtimes.RuntimeTarget, runtimes.DetectionResult, func(), error) {
	if RuntimeSDK == nil || RuntimeEnvironmentControllerManager == nil {
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, nil, status_error.RuntimeUnavailable
	}
	environment, err := RuntimeEnvironmentControllerManager.Get(ctx, runtimeID)
	if err != nil {
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, nil, err
	}
	if !environment.Enabled || environment.Detection.State != runtimes.DetectionStateReady {
		return runtimes.RuntimeTarget{}, runtimes.DetectionResult{}, nil, status_error.RuntimeUnavailable
	}
	target := runtimes.RuntimeTarget{RuntimeID: runtimeID, Binary: environment.Binary, PrefixArgs: append([]string(nil), environment.PrefixArgs...), ConfigHome: environment.ConfigHome, CloseAfterRun: true}
	cleanup := func() {}
	if accountID != "" && accountID != constants.AiAccountSystemID {
		directory, err := os.MkdirTemp("", "neux-model-account-")
		if err != nil {
			logx.Errorf("prepare model account workspace failed: error=%v", err)
			return target, environment.Detection, nil, status_error.RuntimeUnavailable
		}
		prepared, release, err := AIAccountService.PrepareQueued(ctx, accountID, environment.BaseRuntimeID, directory, model)
		if err != nil {
			_ = os.RemoveAll(directory)
			return target, environment.Detection, nil, err
		}
		target.AccountID, target.Environment, target.Isolated = prepared.AccountID, prepared.Environment, prepared.Isolated
		target.RefreshCredentials = prepared.RefreshCredentials
		if prepared.ConfigHome != "" {
			target.ConfigHome = prepared.ConfigHome
		}
		cleanup = func() {
			release()
			if err := os.RemoveAll(directory); err != nil {
				logx.Errorf("remove model account workspace failed: error=%v", err)
			}
		}
	}
	return target, environment.Detection, cleanup, nil
}
