package aiaccount

import (
	"context"
	"os"
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// ModelDetector 是查询账号能力所需的只读原生探测边界。
type ModelDetector interface {
	// Detect 在临时会话读取真实模型目录，并在原生使用者退出后调用请求中的 ReleaseResources。
	Detect(context.Context, runtimecontract.DetectRequest) runtimecontract.DetectionResult
}

// DetectModel 为设置查询和执行验证复用同一账号、原始模型与私有目录准备链路。
func (s *Service) DetectModel(ctx context.Context, detector ModelDetector, id string, environment models.RuntimeEnvironment, model string) (runtimecontract.DetectionResult, error) {
	account, err := s.ValidateSelection(ctx, id, environment.BaseRuntimeID, model, "")
	if err != nil {
		return runtimecontract.DetectionResult{}, err
	}
	if account.AuthMode == constants.AccountAuthModeAPIKey && !slices.Contains(account.SelectedModels, model) {
		return runtimecontract.DetectionResult{}, status_error.BadRequest
	}
	workspace, err := os.MkdirTemp("", "ally-account-probe-")
	if err != nil {
		logx.Errorf("create account capability workspace failed: account_id=%s error=%v", id, err)
		return runtimecontract.DetectionResult{}, status_error.AIAccountWorkspaceError
	}
	removeDirectory := true
	defer func() {
		if removeDirectory {
			_ = os.RemoveAll(workspace)
		}
	}()
	target, release, err := s.PrepareProbe(ctx, id, environment.BaseRuntimeID, workspace, model)
	if err != nil {
		return runtimecontract.DetectionResult{}, err
	}
	home, _ := os.UserHomeDir()
	nativeModel := accountconfig.NativeModelID(environment.BaseRuntimeID, model)
	request := runtimecontract.DetectRequest{RuntimeID: environment.RuntimeID, WorkingDirectory: workspace, BaseEnvironment: accountconfig.Environment(os.Environ()), HomeDirectory: home, LaunchOverride: runtimecontract.LaunchOverride{Binary: environment.Binary, PrefixArgs: slices.Clone(environment.PrefixArgs), Environment: target.Environment}}
	// 既有运行时保持原探测约定；新绑定对目标模型验证原生 setter 回执。
	request.RefreshCredentials = target.RefreshCredentials
	request.ReleaseResources = func() {
		release()
		_ = os.RemoveAll(workspace)
	}
	if accountconfig.IsNativeAccountRuntime(environment.BaseRuntimeID) {
		request.Model = nativeModel
	}
	removeDirectory = false
	result := detector.Detect(ctx, request)
	if result.State != runtimecontract.DetectionStateReady {
		return result, status_error.RuntimeUnavailable
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == runtimecontract.DiagnosticCodeACPOptionsUnavailable {
			return result, status_error.RuntimeUnavailable
		}
	}
	for index := range result.Models {
		result.Models[index].ID = accountconfig.AccountModelID(environment.BaseRuntimeID, result.Models[index].ID)
	}
	result.DetectedDefaultModel = accountconfig.AccountModelID(environment.BaseRuntimeID, result.DetectedDefaultModel)
	return result, nil
}
