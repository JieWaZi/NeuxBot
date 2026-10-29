package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"context"
	"fmt"
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
	request runtimecontract.DetectRequest,
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
func (sdk *Catalog) ResolveExecutable(request runtimecontract.DetectRequest) (string, error) {
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
	request runtimecontract.DetectRequest,
) runtimecontract.DetectionResult {
	release := request.ReleaseResources
	defer func() {
		if release != nil {
			release()
		}
	}()
	if ctx == nil {
		return runtimecontract.DetectionResult{
			State: runtimecontract.DetectionStateUnavailable,
			Auth:  runtimecontract.AuthStatusUnknown,
			Diagnostics: []runtimecontract.Diagnostic{{
				Code:    string(runtimecontract.FailureCodeInvalidRequest),
				Level:   runtimecontract.DiagnosticLevelError,
				Message: "context is required",
			}},
		}
	}
	definition, exists, definitionErr := sdk.definitionForRequest(request)
	if !exists {
		return runtimecontract.DetectionResult{
			State: runtimecontract.DetectionStateUnavailable,
			Auth:  runtimecontract.AuthStatusUnknown,
			Diagnostics: []runtimecontract.Diagnostic{{
				Code:    "RUNTIME_NOT_FOUND",
				Level:   runtimecontract.DiagnosticLevelError,
				Message: fmt.Sprintf("runtime %q is not registered", request.RuntimeID),
			}},
		}
	}
	if definitionErr != nil {
		return runtimecontract.DetectionResult{
			Runtime: runtimecontract.RuntimeSummary{ID: request.RuntimeID},
			State:   runtimecontract.DetectionStateUnavailable,
			Auth:    runtimecontract.AuthStatusUnknown,
			Diagnostics: []runtimecontract.Diagnostic{{
				Code:    string(runtimecontract.FailureCodeInvalidRequest),
				Level:   runtimecontract.DiagnosticLevelError,
				Message: definitionErr.Error(),
			}},
		}
	}
	result := runtimecontract.DetectionResult{
		Runtime: definitionSummaries(map[string]runtimeDefinition{definition.id: definition})[0],
		State:   runtimecontract.DetectionStateUnavailable,
		Auth:    runtimecontract.AuthStatusUnknown,
		Models:  cloneModels(definition.models),
		ReasoningOptions: cloneReasoningOptions(
			definition.reasoningOptions,
		),
		Capabilities: runtimecontract.CapabilitySet{},
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, runtimecontract.Diagnostic{
			Code:    "ENVIRONMENT_INVALID",
			Level:   runtimecontract.DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	binary, err := resolveExecutable(definitionExecutableCandidates(definition, request.HomeDirectory), environment)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, runtimecontract.Diagnostic{
			Code:    "RUNTIME_UNAVAILABLE",
			Level:   runtimecontract.DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	result.ResolvedBinary = binary

	probeDirectory := request.WorkingDirectory
	if probeDirectory == "" {
		probeDirectory, err = os.MkdirTemp("", "ally-runtime-probe-")
	} else {
		probeDirectory, err = runtimecontract.ValidateDirectory(probeDirectory, "probe")
	}
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, runtimecontract.Diagnostic{
			Code:    "PROBE_DIRECTORY_FAILED",
			Level:   runtimecontract.DiagnosticLevelError,
			Message: err.Error(),
		})
		return result
	}
	if request.WorkingDirectory == "" {
		externalRelease := release
		release = func() {
			if externalRelease != nil {
				externalRelease()
			}
			_ = os.RemoveAll(probeDirectory)
		}
	}

	probeRelease := release
	release = nil
	catalog, catalogErr := sdk.acp.DetectOptions(ctx, acpOptionsRequest{
		ReleaseResources:   probeRelease,
		RefreshCredentials: request.RefreshCredentials,
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
		result.Capabilities = runtimecontract.CapabilitySet{}
	}
	if len(catalog.Models) > 0 || catalog.SessionReady {
		result.Models, result.ReasoningOptions = acpCatalogOptions(catalog)
		result.DetectedDefaultModel = catalog.DefaultModel
		result.DetectedDefaultReasoning = catalog.DefaultReasoning
		result.Capabilities[runtimecontract.CapabilityDynamicModels] = len(catalog.Models) > 0
	}
	authMissing := catalogErr != nil && runtimecontract.ACPFailureCode(catalogErr) == runtimecontract.FailureCodeAuthRequired
	if catalog.SessionReady || authMissing {
		// 已解析的 CLI 返回鉴权错误时保留可选择性，由执行阶段报告具体鉴权失败。
		result.State = runtimecontract.DetectionStateReady
	}
	if authMissing {
		result.Auth = runtimecontract.AuthStatusMissing
	}
	if catalogErr != nil {
		level := runtimecontract.DiagnosticLevelWarning
		if !catalog.AgentConnected || !catalog.SessionReady {
			level = runtimecontract.DiagnosticLevelError
		}
		result.Diagnostics = append(result.Diagnostics, runtimecontract.Diagnostic{
			Code:    runtimecontract.DiagnosticCodeACPOptionsUnavailable,
			Level:   level,
			Message: runtimecontract.FailureDisplayMessage(catalogErr),
		})
	}
	return result
}
