package catalog

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"fmt"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func (sdk *Catalog) definition(id string) (runtimeDefinition, bool) {
	definition, exists := sdk.definitions[id]
	if !exists {
		return runtimeDefinition{}, false
	}
	return cloneDefinition(definition), true
}

func (sdk *Catalog) definitionForRequest(
	request runtimeconfig.DetectRequest,
) (runtimeDefinition, bool, error) {
	definition, exists := sdk.definition(request.RuntimeID)
	if !exists {
		return runtimeDefinition{}, false, nil
	}
	resolved, err := definitionWithLaunchOverride(definition, request.LaunchOverride)
	if err != nil {
		return runtimeDefinition{}, true, err
	}
	return resolved, true, nil
}

// ResolveExecutable 只解析指定 Runtime 的可执行文件，不启动 ACP 或检查账号。
func (sdk *Catalog) ResolveExecutable(request runtimeconfig.DetectRequest) (string, error) {
	definition, exists, err := sdk.definitionForRequest(request)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", status_error.RuntimeNotRegistered.WithParams(map[string]interface{}{"RuntimeID": request.RuntimeID})
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		return "", err
	}
	return resolveExecutable(definitionExecutableCandidates(definition, request.HomeDirectory), environment)
}

// Detect 对指定 Runtime 执行有界且不触发模型推理的可用性探测。
func (sdk *Catalog) Detect(
	ctx context.Context,
	request runtimeconfig.DetectRequest,
) (result runtimeconfig.DetectionResult) {
	release := request.ReleaseResources
	defer func() {
		if release != nil {
			cleanupContext := ctx
			if cleanupContext == nil {
				cleanupContext = context.Background()
			}
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(cleanupContext), acpManagementCloseTimeout)
			defer cancel()
			if err := sdk.acp.ReleaseResources(cleanup, release); err != nil {
				result.Diagnostics = append(result.Diagnostics, runtimeconfig.Diagnostic{Code: runtimeconfig.DiagnosticCodeACPOptionsUnavailable, Level: runtimeconfig.DiagnosticLevelError, Message: runtimediagnostic.FailureDisplayMessage(err)})
			}
		}
	}()
	if ctx == nil {
		return runtimeconfig.DetectionResult{
			State: runtimeconfig.DetectionStateUnavailable,
			Auth:  runtimeconfig.AuthStatusUnknown,
			Diagnostics: []runtimeconfig.Diagnostic{{
				Code:    string(runtimediagnostic.FailureCodeInvalidRequest),
				Level:   runtimeconfig.DiagnosticLevelError,
				Message: "context is required",
			}},
		}
	}
	definition, exists, definitionErr := sdk.definitionForRequest(request)
	if !exists {
		return runtimeconfig.DetectionResult{
			State: runtimeconfig.DetectionStateUnavailable,
			Auth:  runtimeconfig.AuthStatusUnknown,
			Diagnostics: []runtimeconfig.Diagnostic{{
				Code:    "RUNTIME_NOT_FOUND",
				Level:   runtimeconfig.DiagnosticLevelError,
				Message: fmt.Sprintf("runtime %q is not registered", request.RuntimeID),
			}},
		}
	}
	if definitionErr != nil {
		return runtimeconfig.DetectionResult{
			Runtime: runtimeconfig.RuntimeSummary{ID: request.RuntimeID},
			State:   runtimeconfig.DetectionStateUnavailable,
			Auth:    runtimeconfig.AuthStatusUnknown,
			Diagnostics: []runtimeconfig.Diagnostic{{
				Code:    string(runtimediagnostic.FailureCodeInvalidRequest),
				Level:   runtimeconfig.DiagnosticLevelError,
				Message: definitionErr.Error(),
			}},
		}
	}
	result = runtimeconfig.DetectionResult{
		Runtime: definitionSummaries(map[string]runtimeDefinition{definition.id: definition})[0],
		State:   runtimeconfig.DetectionStateUnavailable,
		Auth:    runtimeconfig.AuthStatusUnknown,
		Models:  cloneModels(definition.models),
		ReasoningOptions: cloneReasoningOptions(
			definition.reasoningOptions,
		),
		Capabilities: runtimeconfig.CapabilitySet{},
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, runtimeconfig.Diagnostic{
			Code:    "ENVIRONMENT_INVALID",
			Level:   runtimeconfig.DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	binary, err := resolveExecutable(definitionExecutableCandidates(definition, request.HomeDirectory), environment)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, runtimeconfig.Diagnostic{
			Code:    "RUNTIME_UNAVAILABLE",
			Level:   runtimeconfig.DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	result.ResolvedBinary = binary

	probeDirectory := request.WorkingDirectory
	if probeDirectory == "" {
		probeDirectory, err = os.MkdirTemp("", "ally-runtime-probe-")
	} else {
		probeDirectory, err = runtimeconfig.ValidateDirectory(probeDirectory, "probe")
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, runtimeconfig.Diagnostic{
			Code:    "PROBE_DIRECTORY_FAILED",
			Level:   runtimeconfig.DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	if request.WorkingDirectory == "" {
		externalRelease := release
		release = func(ctx context.Context) error {
			if externalRelease != nil {
				if err := externalRelease(ctx); err != nil {
					return err
				}
				externalRelease = nil
			}
			if err := os.RemoveAll(probeDirectory); err != nil {
				logx.Errorf("remove runtime probe directory failed: error=%v", err)
				return status_error.RuntimeShutdownFailed
			}
			return nil
		}
	}

	probeRelease := release
	release = nil
	catalog, catalogErr := sdk.acp.DetectOptions(ctx, acpOptionsRequest{
		ReleaseResources:   probeRelease,
		RefreshCredentials: request.RefreshCredentials,
		UseNativeAuth:      request.UseNativeAuth,
		RuntimeID:          definition.id,
		Model:              request.Model,
		Prepared: preparedRuntime{
			definition:  definition,
			detection:   result,
			environment: environment,
		},
		WorkingDirectory: probeDirectory,
	})
	result.Version = catalog.Version
	result.Capabilities = cloneCapabilitySet(catalog.Capabilities)
	if result.Capabilities == nil {
		result.Capabilities = runtimeconfig.CapabilitySet{}
	}
	if len(catalog.Models) > 0 || catalog.SessionReady {
		result.Models, result.ReasoningOptions = acpCatalogOptions(catalog)
		result.DetectedDefaultModel = catalog.DefaultModel
		result.DetectedDefaultReasoning = catalog.DefaultReasoning
		result.Capabilities[runtimeconfig.CapabilityDynamicModels] = len(catalog.Models) > 0
	}
	authMissing := catalogErr != nil && runtimediagnostic.ACPFailureCode(catalogErr) == runtimediagnostic.FailureCodeAuthRequired
	if catalog.SessionReady || authMissing {
		// 已解析的 CLI 返回鉴权错误时保留可选择性，由执行阶段报告具体鉴权失败。
		result.State = runtimeconfig.DetectionStateReady
	}
	if authMissing {
		result.Auth = runtimeconfig.AuthStatusMissing
	}
	if catalogErr != nil {
		level := runtimeconfig.DiagnosticLevelWarning
		if !catalog.AgentConnected || !catalog.SessionReady {
			level = runtimeconfig.DiagnosticLevelError
		}
		result.Diagnostics = append(result.Diagnostics, runtimeconfig.Diagnostic{
			Code:    runtimeconfig.DiagnosticCodeACPOptionsUnavailable,
			Level:   level,
			Message: runtimediagnostic.FailureDisplayMessage(catalogErr),
		})
	}
	return result
}
