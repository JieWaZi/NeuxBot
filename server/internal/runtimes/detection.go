package runtimes

import (
	"context"
	"fmt"
	"os"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func (sdk *SDK) definition(id string) (runtimeDefinition, bool) {
	definition, exists := sdk.definitions[id]
	if !exists {
		return runtimeDefinition{}, false
	}
	return cloneDefinition(definition), true
}

func (sdk *SDK) definitionForRequest(
	request DetectRequest,
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
func (sdk *SDK) ResolveExecutable(request DetectRequest) (string, error) {
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
func (sdk *SDK) Detect(
	ctx context.Context,
	request DetectRequest,
) DetectionResult {
	if ctx == nil {
		return DetectionResult{
			State: DetectionStateUnavailable,
			Auth:  AuthStatusUnknown,
			Diagnostics: []Diagnostic{{
				Code:    string(FailureCodeInvalidRequest),
				Level:   DiagnosticLevelError,
				Message: "context is required",
			}},
		}
	}
	definition, exists, definitionErr := sdk.definitionForRequest(request)
	if !exists {
		return DetectionResult{
			State: DetectionStateUnavailable,
			Auth:  AuthStatusUnknown,
			Diagnostics: []Diagnostic{{
				Code:    "RUNTIME_NOT_FOUND",
				Level:   DiagnosticLevelError,
				Message: fmt.Sprintf("runtime %q is not registered", request.RuntimeID),
			}},
		}
	}
	if definitionErr != nil {
		return DetectionResult{
			Runtime: RuntimeSummary{ID: request.RuntimeID},
			State:   DetectionStateUnavailable,
			Auth:    AuthStatusUnknown,
			Diagnostics: []Diagnostic{{
				Code:    string(FailureCodeInvalidRequest),
				Level:   DiagnosticLevelError,
				Message: definitionErr.Error(),
			}},
		}
	}
	result := DetectionResult{
		Runtime: definitionSummaries(map[string]runtimeDefinition{definition.id: definition})[0],
		State:   DetectionStateUnavailable,
		Auth:    AuthStatusUnknown,
		Models:  cloneModels(definition.models),
		ReasoningOptions: cloneReasoningOptions(
			definition.reasoningOptions,
		),
		Capabilities: CapabilitySet{},
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    "ENVIRONMENT_INVALID",
			Level:   DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	binary, err := resolveExecutable(definitionExecutableCandidates(definition, request.HomeDirectory), environment)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    "RUNTIME_UNAVAILABLE",
			Level:   DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	result.ResolvedBinary = binary

	probeDirectory := request.WorkingDirectory
	if probeDirectory == "" {
		probeDirectory, err = os.MkdirTemp("", "ally-runtime-probe-")
	} else {
		probeDirectory, err = validateDirectoryPath(probeDirectory, "probe")
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    "PROBE_DIRECTORY_FAILED",
			Level:   DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	if request.WorkingDirectory == "" {
		defer os.RemoveAll(probeDirectory)
	}

	catalog, catalogErr := sdk.acp.DetectOptions(ctx, acpOptionsRequest{
		RefreshCredentials: request.RefreshCredentials,
		RuntimeID:          definition.id,
		Model:              request.Model,
		Prepared: preparedRuntime{
			definition:  definition,
			detection:   result,
			environment: environment,
			defaults:    sdk.defaults,
		},
		WorkingDirectory: probeDirectory,
	})
	result.Version = catalog.Version
	result.Capabilities = cloneCapabilitySet(catalog.Capabilities)
	if result.Capabilities == nil {
		result.Capabilities = CapabilitySet{}
	}
	if len(catalog.Models) > 0 || catalog.SessionReady {
		result.Models, result.ReasoningOptions = acpCatalogOptions(catalog)
		result.DetectedDefaultModel = catalog.DefaultModel
		result.DetectedDefaultReasoning = catalog.DefaultReasoning
		result.Capabilities[CapabilityDynamicModels] = len(catalog.Models) > 0
	}
	authMissing := catalogErr != nil && acpFailureCode(catalogErr) == FailureCodeAuthRequired
	if catalog.SessionReady || authMissing {
		// 已解析的 CLI 返回鉴权错误时保留可选择性，由执行阶段报告具体鉴权失败。
		result.State = DetectionStateReady
	}
	if authMissing {
		result.Auth = AuthStatusMissing
	}
	if catalogErr != nil {
		level := DiagnosticLevelWarning
		if !catalog.AgentConnected || !catalog.SessionReady {
			level = DiagnosticLevelError
		}
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:    DiagnosticCodeACPOptionsUnavailable,
			Level:   level,
			Message: catalogErr.Error(),
		})
	}
	return result
}
