package runtimes

import (
	"context"
	"os"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
)

// SDK 提供智能体探测、启动准备、工作区物化与原生分叉。
type SDK struct {
	// definitions 保存已经合并 Local Profile 的只读 Runtime Definition。
	definitions map[string]runtimeDefinition
	// acp 只接纳探测和分叉所需的短生命周期管理操作。
	acp acpManager
}

// New 校验显式配置并构造一个不启动 goroutine 的 Runtime SDK。
func New(config Config) (*SDK, error) {
	definitions, err := newRegistry(config.Profiles)
	if err != nil {
		return nil, err
	}
	return &SDK{
		definitions: definitions,
		acp:         newACPBackend(),
	}, nil
}

// Close 停止接纳新的探测和分叉；各管理操作负责关闭自己的 ACP 连接。
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

// LaunchFacts 是已有探测与账号准备共同确认的原生启动事实，不创建协议连接。
type LaunchFacts struct {
	// BaseRuntimeID 决定公开适配器的协议类型。
	BaseRuntimeID string
	// Executable 是有效探测已经解析的真实程序路径。
	Executable string
	// PrefixArgs 是 Profile 与启动覆盖合并后的前置参数。
	PrefixArgs []string
	// Environment 是账号隔离、Profile 覆盖与工具路径补充后的完整环境。
	Environment []string
}

// PrepareLaunch 复用既有启动准备规则，仅输出事实，不执行探测、会话或模型请求。
func (sdk *SDK) PrepareLaunch(target RuntimeTarget, detection DetectionResult) (LaunchFacts, error) {
	if sdk == nil {
		return LaunchFacts{}, status_error.RuntimeUnavailable
	}
	prepared, err := sdk.prepareFromDetection(sdk.prepareRequestForTarget(target), detection)
	if err != nil {
		return LaunchFacts{}, err
	}
	return LaunchFacts{BaseRuntimeID: prepared.definition.baseRuntimeID, Executable: prepared.detection.ResolvedBinary, PrefixArgs: cloneStrings(prepared.definition.prefixArgs), Environment: prepared.environment.list()}, nil
}

// preparedRuntime 保存同一次探测得到且可直接启动的不可变运行信息。
type preparedRuntime struct {
	// definition 保存已经解析 Profile 的有效 Runtime Definition。
	definition runtimeDefinition
	// detection 保存调用方可以持久化或展示的探测结论。
	detection DetectionResult
	// environment 保存探测和后续启动共享的显式环境快照。
	environment environmentSnapshot
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

// prepareRequestForTarget 合并所选账号隔离、Profile 与显式启动覆盖。
func (sdk *SDK) prepareRequestForTarget(target RuntimeTarget) DetectRequest {
	environment := make(map[string]string)
	if definition, exists := sdk.definitions[target.RuntimeID]; exists &&
		target.ConfigHome != "" &&
		definition.configHomeEnvironmentVariable != "" {
		environment[definition.configHomeEnvironmentVariable] = target.ConfigHome
	}
	for key, value := range target.Environment {
		environment[key] = value
	}
	baseEnvironment := os.Environ()
	if target.Isolated {
		baseEnvironment = accountconfig.Environment(baseEnvironment)
	}
	homeDirectory, _ := os.UserHomeDir()
	return DetectRequest{
		RuntimeID:       target.RuntimeID,
		BaseEnvironment: baseEnvironment,
		HomeDirectory:   homeDirectory,
		LaunchOverride: LaunchOverride{
			Binary:      target.Binary,
			PrefixArgs:  append([]string(nil), target.PrefixArgs...),
			Environment: environment,
		},
	}
}
