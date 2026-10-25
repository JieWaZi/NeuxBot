package runtimes

import (
	"context"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// SDK 是并发安全且构造后不可变的 Runtime 深模块门面。
type SDK struct {
	// definitions 保存已经合并 Local Profile 的只读 Runtime Definition。
	definitions map[string]runtimeDefinition
	// defaults 保存构造时规范化的 Invocation 默认限制。
	defaults Defaults
	// acp 是 Codex 与 Claude 调用统一使用的内嵌 ACP Client Runtime Module。
	acp acpRunner
}

// New 校验显式配置并构造一个不启动 goroutine 的 Runtime SDK。
func New(config Config) (*SDK, error) {
	defaults, err := normalizeDefaults(config.Defaults)
	if err != nil {
		return nil, err
	}
	definitions, err := newRegistry(config.Profiles)
	if err != nil {
		return nil, err
	}
	return &SDK{
		definitions: definitions,
		defaults:    defaults,
		acp:         newACPBackend(),
	}, nil
}

// Close 释放 SDK 持有的长期 ACP Agent，并对 nil SDK 保持幂等。
func (sdk *SDK) Close(ctx context.Context) error {
	if sdk == nil || sdk.acp == nil {
		return nil
	}
	return sdk.acp.Close(ctx)
}

// Runtimes 返回按 Runtime ID 排序的不可变 Definition 摘要。
func (sdk *SDK) Runtimes() []RuntimeSummary {
	return definitionSummaries(sdk.definitions)
}

// preparedRuntime 保存同一次探测得到且可直接启动的不可变运行信息。
type preparedRuntime struct {
	// definition 保存已经解析 Profile 的有效 Runtime Definition。
	definition runtimeDefinition
	// detection 保存调用方可以持久化或展示的探测结论。
	detection DetectionResult
	// environment 保存探测和后续启动共享的显式环境快照。
	environment environmentSnapshot
	// defaults 保存 SDK 构造时冻结的 Invocation 默认限制。
	defaults Defaults
}

// detectionResult 返回准备结果对应的探测结论副本。
func (prepared preparedRuntime) detectionResult() DetectionResult {
	result := prepared.detection
	result.Models = cloneModels(result.Models)
	result.ReasoningOptions = cloneReasoningOptions(result.ReasoningOptions)
	result.Capabilities = cloneCapabilitySet(result.Capabilities)
	result.Diagnostics = append([]Diagnostic(nil), result.Diagnostics...)
	return result
}

// prepare 探测 Runtime 并在必要能力满足时返回可直接启动的包内快照。
func (sdk *SDK) prepare(
	ctx context.Context,
	request DetectRequest,
) (preparedRuntime, error) {
	detection := sdk.Detect(ctx, request)
	return sdk.prepareFromDetection(request, detection)
}

// prepareFromDetection 使用调用方已经持久化的探测快照准备 Runtime，不执行新的探测。
func (sdk *SDK) prepareFromDetection(
	request DetectRequest,
	detection DetectionResult,
) (preparedRuntime, error) {
	if detection.State != DetectionStateReady {
		return preparedRuntime{}, status_error.RuntimeUnavailable
	}
	definition, exists, err := sdk.definitionForRequest(request)
	if !exists {
		return preparedRuntime{}, status_error.RuntimeUnavailable
	}
	if err != nil {
		return preparedRuntime{}, err
	}
	if strings.TrimSpace(detection.Runtime.ID) != definition.id ||
		strings.TrimSpace(detection.ResolvedBinary) == "" {
		return preparedRuntime{}, status_error.RuntimeUnavailable
	}
	environment, err := buildPreparedEnvironment(
		request.BaseEnvironment,
		definition.environment,
		request.HomeDirectory,
	)
	if err != nil {
		return preparedRuntime{}, err
	}
	detection = cloneDetectionResult(detection)
	detection.Runtime = definitionSummaries(map[string]runtimeDefinition{
		definition.id: definition,
	})[0]
	if detection.Capabilities == nil {
		detection.Capabilities = CapabilitySet{}
	}
	return preparedRuntime{
		definition:  definition,
		detection:   detection,
		environment: environment,
		defaults:    sdk.defaults,
	}, nil
}

func cloneDetectionResult(source DetectionResult) DetectionResult {
	clone := source
	clone.Runtime = cloneRuntimeSummary(source.Runtime)
	clone.Models = cloneModels(source.Models)
	clone.ReasoningOptions = cloneReasoningOptions(source.ReasoningOptions)
	clone.Capabilities = cloneCapabilitySet(source.Capabilities)
	clone.Diagnostics = append([]Diagnostic(nil), source.Diagnostics...)
	clone.Installations = append([]CLIInstallation(nil), source.Installations...)
	clone.DetectedDefaultModel = source.DetectedDefaultModel
	clone.DetectedDefaultReasoning = source.DetectedDefaultReasoning
	return clone
}

func cloneCapabilitySet(source CapabilitySet) CapabilitySet {
	cloned := make(CapabilitySet, len(source))
	for capability, enabled := range source {
		cloned[capability] = enabled
	}
	return cloned
}

// cloneRuntimeSummary 深拷贝 Runtime 摘要中的全部可变集合。
func cloneRuntimeSummary(summary RuntimeSummary) RuntimeSummary {
	clone := summary
	clone.BinaryCandidates = append([]string(nil), summary.BinaryCandidates...)
	clone.PrefixArgs = append([]string(nil), summary.PrefixArgs...)
	clone.Models = cloneModels(summary.Models)
	clone.ReasoningOptions = append([]ReasoningOption(nil), summary.ReasoningOptions...)
	return clone
}
