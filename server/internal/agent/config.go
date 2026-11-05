package agent

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"os"
	"path/filepath"
	"strings"
)

const (
	// RuntimeIDCodex 是内置 Codex CLI 运行时标识。
	RuntimeIDCodex = constants.RuntimeIDCodex
	// RuntimeIDClaude 是内置 Claude Code 运行时标识。
	RuntimeIDClaude = constants.RuntimeIDClaude
	// RuntimeIDKimi 是 Kimi CLI 运行时标识。
	RuntimeIDKimi = constants.RuntimeIDKimi
	// RuntimeIDCursor 是 Cursor CLI 运行时标识。
	RuntimeIDCursor = constants.RuntimeIDCursor
	// RuntimeIDPi 是 Pi 运行时标识。
	RuntimeIDPi = constants.RuntimeIDPi
	// RuntimeIDOpenCode 是 OpenCode 运行时标识。
	RuntimeIDOpenCode = constants.RuntimeIDOpenCode
	// RuntimeIDGemini 是 Gemini CLI 运行时标识。
	RuntimeIDGemini = constants.RuntimeIDGemini
	// RuntimeIDGrok 是 Grok Build 运行时标识。
	RuntimeIDGrok = constants.RuntimeIDGrok
	// RuntimeIDWorkBuddy 是 WorkBuddy 标准 ACP 运行时标识。
	RuntimeIDWorkBuddy = constants.RuntimeIDWorkBuddy
	// RuntimeIDQoder 是 Qoder CLI 标准 ACP 运行时标识。
	RuntimeIDQoder = constants.RuntimeIDQoder
	// RuntimeIDQoderCN 是 Qoder 中国版 CLI 标准 ACP 运行时标识。
	RuntimeIDQoderCN = constants.RuntimeIDQoderCN
)

// DetectRequest selects the installation or profile to inspect.
type DetectRequest struct {
	// RuntimeID 指定内置 Runtime 或 Local Profile 的开放字符串 ID。
	RuntimeID string
	// BaseEnvironment 是调用方显式提供的基础进程环境。
	BaseEnvironment []string
	// HomeDirectory 用于补全 GUI 进程可能缺失的常见可执行文件目录。
	HomeDirectory string
	// LaunchOverride 保存调用方允许覆盖的命令、前置参数和环境层。
	LaunchOverride LaunchOverride
}

// LaunchOverride 保存运行时环境条目允许用户编辑的有限启动覆盖。
type LaunchOverride struct {
	// Binary 是单个可执行文件名或绝对路径，空值表示沿用 Definition 候选。
	Binary string
	// PrefixArgs 是放在 Runtime 子命令之前的参数；nil 表示沿用 Definition 参数。
	PrefixArgs []string
	// Environment 是受控的 Runtime 环境覆盖，例如 CODEX_HOME。
	Environment map[string]string
}

// ModelOption 描述 Runtime 可以选择的一个模型。
type ModelOption struct {
	// ID 是通过 ACP Session 配置传递的模型标识。
	ID string
	// Name 是面向调用方展示的模型名称。
	Name string
	// ReasoningOptions 是该模型通过 Runtime 协议声明支持的推理强度。
	ReasoningOptions []ReasoningOption
	// PermissionLevels 是该模型当前可以准确表达的产品权限等级。
	PermissionLevels []PermissionLevel
}

// ReasoningOption 描述 Runtime 可以选择的一个推理强度。
type ReasoningOption struct {
	// ID 是通过 ACP Session 配置传递的推理强度标识。
	ID string
	// Name 是面向调用方展示的推理强度名称。
	Name string
}

// ProfileSpec 描述对内置 Runtime Definition 的有限覆盖。
type ProfileSpec struct {
	// ID 是 Profile 自己的开放字符串 Runtime ID。
	ID string
	// BaseRuntimeID 指定被继承的内置 Runtime ID。
	BaseRuntimeID string
	// Name 覆盖 Runtime 的展示名称，空值表示继承。
	Name string
	// BinaryCandidates 覆盖按优先级尝试的可执行文件候选，nil 表示继承。
	BinaryCandidates []string
	// PrefixArgs 覆盖每次调用都放在子命令之前的参数，nil 表示继承。
	PrefixArgs []string
	// Environment 合并到 Base Definition 环境层，同名键覆盖 Base。
	Environment map[string]string
	// Models 覆盖模型提示列表，nil 表示继承。
	Models []ModelOption
	// DefaultModel 覆盖默认模型标识，空值表示继承。
	DefaultModel string
}

// RuntimeSummary 是调用方可安全展示的 Runtime Definition 摘要。
type RuntimeSummary struct {
	// ID 是 Runtime 或 Local Profile 的开放字符串标识。
	ID string
	// BaseRuntimeID 是 Profile 继承的内置 Runtime ID，内置项等于自身 ID。
	BaseRuntimeID string
	// Name 是 Runtime 的展示名称。
	Name string
	// Profile 表示该项是否由调用方提供的 Local Profile 生成。
	Profile bool
	// BinaryCandidates 是按优先级尝试的可执行文件候选。
	BinaryCandidates []string
	// PrefixArgs 是每次调用都放在 Runtime 子命令之前的参数。
	PrefixArgs []string
	// Models 是当前 Definition 提供的模型提示。
	Models []ModelOption
	// DefaultModel 是未显式选择模型时使用的模型标识。
	DefaultModel string
	// ReasoningOptions 是该 Runtime 明确支持的推理强度选项。
	ReasoningOptions []ReasoningOption
	// InstructionFileName 是该 Runtime 在工作区根目录使用的指令文件名。
	InstructionFileName string
	// SkillDirectoryName 是该 Runtime 在工作区根目录使用的技能目录名。
	SkillDirectoryName string
	// ConfigHomeEnvironmentVariable 是配置主目录对应的受控环境变量名。
	ConfigHomeEnvironmentVariable string
}

// DetectionState 是共享 Runtime 探测状态枚举的引用。
type DetectionState = constants.RuntimeDetectionState

const (
	// DetectionStateUnavailable 表示未找到或不能调用 Runtime 可执行文件。
	DetectionStateUnavailable = constants.RuntimeDetectionStateUnavailable
	// DetectionStateIncompatible 表示可执行文件存在但缺少必要协议能力。
	DetectionStateIncompatible = constants.RuntimeDetectionStateIncompatible
	// DetectionStateReady 表示 Runtime 必要能力满足且可以准备调用。
	DetectionStateReady = constants.RuntimeDetectionStateReady
)

// AuthStatus 是共享 Runtime 鉴权状态枚举的引用。
type AuthStatus = constants.RuntimeAuthStatus

const (
	// AuthStatusUnknown 表示 SDK 无法可靠判断鉴权状态。
	AuthStatusUnknown = constants.RuntimeAuthStatusUnknown
	// AuthStatusOK 表示鉴权探测没有发现缺失凭据。
	AuthStatusOK = constants.RuntimeAuthStatusOK
	// AuthStatusMissing 表示 ACP Agent 明确报告尚未登录或缺少凭据。
	AuthStatusMissing = constants.RuntimeAuthStatusMissing
)

// Capability 是共享 Runtime 能力枚举的引用。
type Capability = constants.RuntimeCapability

const (
	// CapabilityStructuredOutput 表示 Runtime 支持要求的结构化输出格式。
	CapabilityStructuredOutput = constants.RuntimeCapabilityStructuredOutput
	// CapabilityResumeSession 表示 Runtime 支持恢复原生 Session。
	CapabilityResumeSession = constants.RuntimeCapabilityResumeSession
	// CapabilityDynamicModels 表示本次探测取得了动态模型列表。
	CapabilityDynamicModels = constants.RuntimeCapabilityDynamicModels
	// CapabilityStreamingUserMessages 表示活跃调用可以继续接收用户消息。
	CapabilityStreamingUserMessages = constants.RuntimeCapabilityStreamingUserMessages
	// CapabilityAddDirectory 表示 Runtime 支持声明附加工作目录。
	CapabilityAddDirectory = constants.RuntimeCapabilityAddDirectory
	// CapabilityPartialMessages 表示 Runtime 支持更细粒度的流式消息。
	CapabilityPartialMessages = constants.RuntimeCapabilityPartialMessages
	// CapabilityImagePath 表示 Runtime 支持通过本地路径原生提交图片。
	CapabilityImagePath = constants.RuntimeCapabilityImagePath
	// CapabilitySkillDirectory 表示 Runtime 支持从调用方指定目录原生加载技能。
	CapabilitySkillDirectory = constants.RuntimeCapabilitySkillDirectory
	// CapabilityMCPStreamableHTTP 表示 Runtime 支持 Streamable HTTP MCP。
	CapabilityMCPStreamableHTTP = constants.RuntimeCapabilityMCPStreamableHTTP
	// CapabilityMCPSSE 表示 Runtime 支持旧版 SSE MCP。
	CapabilityMCPSSE = constants.RuntimeCapabilityMCPSSE
)

// CapabilitySet 保存一次探测确认可用的 Runtime 能力。
type CapabilitySet map[Capability]bool

// Has 返回指定能力是否在当前快照中可用。
func (set CapabilitySet) Has(capability Capability) bool {
	return set[capability]
}

// DiagnosticLevel 是共享 Runtime 诊断级别枚举的引用。
type DiagnosticLevel = constants.RuntimeDiagnosticLevel

const (
	// DiagnosticLevelInfo 表示不影响调用的补充信息。
	DiagnosticLevelInfo = constants.RuntimeDiagnosticLevelInfo
	// DiagnosticLevelWarning 表示已降级但仍可能继续调用。
	DiagnosticLevelWarning = constants.RuntimeDiagnosticLevelWarning
	// DiagnosticLevelError 表示当前 Runtime 不能安全启动。
	DiagnosticLevelError = constants.RuntimeDiagnosticLevelError
)

// Diagnostic 是不包含秘密值的稳定 Runtime 诊断。
type Diagnostic struct {
	// Code 是调用方可用于本地化和聚合的稳定诊断码。
	Code string
	// Level 表示诊断对当前操作的影响。
	Level DiagnosticLevel
	// Message 是面向开发和日志的简明技术说明。
	Message string
}

// DetectionResult 是一次无副作用 Runtime 探测的完整结论。
type DetectionResult struct {
	// Runtime 是本次探测使用的有效 Definition 摘要。
	Runtime RuntimeSummary
	// State 表示 Runtime 是否可用及协议是否兼容。
	State DetectionState
	// ResolvedBinary 是实际接受探测并将用于 Run 的可执行文件路径。
	ResolvedBinary string
	// Installations 保存当前发现及此前选择过的 CLI 安装实例。
	Installations []CLIInstallation
	// Version 是 ACP Agent 包装的 Runtime CLI 版本，未知时为空。
	Version string
	// Auth 是独立于安装状态的鉴权探测结论。
	Auth AuthStatus
	// Models 是动态探测结果或 Definition 提供的模型提示。
	Models []ModelOption
	// ReasoningOptions 是当前默认模型通过 Runtime 协议声明的推理强度。
	ReasoningOptions []ReasoningOption
	// DetectedDefaultModel 是探测时 ACP Session 当前生效的模型标识。
	DetectedDefaultModel string
	// DetectedDefaultReasoning 是探测时 ACP Session 当前生效的推理强度标识。
	DetectedDefaultReasoning string
	// Capabilities 是本次 Help 和协议探测确认的能力快照。
	Capabilities CapabilitySet
	// Diagnostics 保存探测失败、未知和能力降级信息。
	Diagnostics []Diagnostic
}

// CLIInstallation 是以可执行文件绝对路径标识的一个 CLI 安装实例。
type CLIInstallation struct {
	// Path 是该安装实例的可执行文件绝对路径。
	Path string
	// Version 是该路径报告的 CLI 版本，探测失败时为空或保留上次结果。
	Version string
	// Available 表示本次刷新时该路径仍可执行。
	Available bool
}

// PermissionLevel 是 NeuxBot 跨 Runtime 使用的三档产品权限。
type PermissionLevel = constants.PermissionLevel

const (
	// PermissionLevelDefault 表示敏感操作需要用户逐次确认。
	PermissionLevelDefault = constants.PermissionLevelDefault
	// PermissionLevelAuto 表示 Runtime 可以自动执行风险可接受的操作。
	PermissionLevelAuto = constants.PermissionLevelAuto
	// PermissionLevelFullAccess 表示用户明确允许 Runtime 使用最大可用权限。
	PermissionLevelFullAccess = constants.PermissionLevelFullAccess
)

// ConnectorTransport 是共享 Runtime MCP 传输枚举的引用。
type ConnectorTransport = constants.RuntimeConnectorTransport

const (
	// ConnectorTransportStdio 表示本地标准输入输出传输。
	ConnectorTransportStdio = constants.RuntimeConnectorTransportStdio
	// ConnectorTransportStreamableHTTP 表示 Streamable HTTP 传输。
	ConnectorTransportStreamableHTTP = constants.RuntimeConnectorTransportStreamableHTTP
	// ConnectorTransportSSE 表示旧版 HTTP SSE 传输。
	ConnectorTransportSSE = constants.RuntimeConnectorTransportSSE
)

const (
	// DiagnosticCodeACPOptionsUnavailable 表示 ACP 模型目录未能完整刷新。
	DiagnosticCodeACPOptionsUnavailable = "ACP_OPTIONS_UNAVAILABLE"
	// DiagnosticCodeACPOptionsFallback 表示本次探测沿用了上一次成功的模型目录。
	DiagnosticCodeACPOptionsFallback = "ACP_OPTIONS_FALLBACK"
	// DiagnosticCodePermissionDefaultDowngraded 表示存盘默认权限已按当前探测能力安全降级。
	DiagnosticCodePermissionDefaultDowngraded = "PERMISSION_DEFAULT_DOWNGRADED"
)

// PreservePreviousOptionsOnFailure 在 ACP 模型目录刷新失败时保留上次成功目录。
func PreservePreviousOptionsOnFailure(
	previous DetectionResult,
	current DetectionResult,
) DetectionResult {
	previousCatalogAvailable := previous.Capabilities[CapabilityDynamicModels] ||
		hasDiagnostic(previous.Diagnostics, DiagnosticCodeACPOptionsFallback)
	if !hasDiagnostic(current.Diagnostics, DiagnosticCodeACPOptionsUnavailable) ||
		!previousCatalogAvailable || len(previous.Models) == 0 {
		return current
	}
	current.Models = cloneModels(previous.Models)
	current.ReasoningOptions = cloneReasoningOptions(previous.ReasoningOptions)
	current.DetectedDefaultModel = previous.DetectedDefaultModel
	current.DetectedDefaultReasoning = previous.DetectedDefaultReasoning
	current.Diagnostics = append(current.Diagnostics, Diagnostic{
		Code:    DiagnosticCodeACPOptionsFallback,
		Level:   DiagnosticLevelWarning,
		Message: "using the previous model catalog after ACP option refresh failed",
	})
	return current
}

// AvailableModels 合并 Runtime 探测目录与用户维护的自定义模型标识。
func AvailableModels(
	detection DetectionResult,
	customModelIDs []string,
) []ModelOption {
	models := cloneModels(detection.Models)
	seen := make(map[string]struct{}, len(models)+len(customModelIDs))
	for _, model := range models {
		seen[model.ID] = struct{}{}
	}
	for _, rawID := range customModelIDs {
		modelID := strings.TrimSpace(rawID)
		if modelID == "" {
			continue
		}
		if _, exists := seen[modelID]; exists {
			continue
		}
		seen[modelID] = struct{}{}
		permissionLevels := []PermissionLevel(nil)
		if len(models) > 0 {
			permissionLevels = append(permissionLevels, models[0].PermissionLevels...)
		}
		models = append(models, ModelOption{
			ID:               modelID,
			Name:             modelID,
			PermissionLevels: permissionLevels,
		})
	}
	return models
}

// NormalizeDetectedDefaults 按探测目录与自定义模型保留有效默认值，并把失效值回落到探测快照。
func NormalizeDetectedDefaults(
	detection DetectionResult,
	customModelIDs []string,
	modelID string,
	reasoningID string,
) (string, string) {
	models := AvailableModels(detection, customModelIDs)
	fallbackModel := strings.TrimSpace(detection.DetectedDefaultModel)
	if fallbackModel == "" && len(models) > 0 {
		fallbackModel = models[0].ID
	}

	modelIndex := -1
	for index, model := range models {
		if model.ID == modelID {
			modelIndex = index
			break
		}
	}
	if modelIndex < 0 {
		modelID = fallbackModel
		for index, model := range models {
			if model.ID == modelID {
				modelIndex = index
				break
			}
		}
	}
	if modelIndex >= 0 {
		modelID = models[modelIndex].ID
	} else {
		modelID = ""
	}

	reasoningOptions := detection.ReasoningOptions
	if modelIndex >= 0 && len(models[modelIndex].ReasoningOptions) > 0 {
		reasoningOptions = models[modelIndex].ReasoningOptions
	}
	for _, option := range reasoningOptions {
		if option.ID == reasoningID {
			return modelID, reasoningID
		}
	}
	fallbackReasoning := strings.TrimSpace(detection.DetectedDefaultReasoning)
	if fallbackReasoning != "" {
		for _, option := range reasoningOptions {
			if option.ID == fallbackReasoning {
				return modelID, fallbackReasoning
			}
		}
	}
	if len(reasoningOptions) == 0 {
		return modelID, ""
	}
	return modelID, reasoningOptions[0].ID
}

// ModelPermissionLevels 返回指定模型当前探测到的产品权限等级；模型缺失或未声明时为空。
func ModelPermissionLevels(
	detection DetectionResult,
	customModelIDs []string,
	modelID string,
) []PermissionLevel {
	models := AvailableModels(detection, customModelIDs)
	selectedID := strings.TrimSpace(modelID)
	if selectedID == "" {
		selectedID = strings.TrimSpace(detection.DetectedDefaultModel)
	}
	if selectedID == "" && len(models) > 0 {
		selectedID = models[0].ID
	}
	for _, model := range models {
		if model.ID != selectedID {
			continue
		}
		return append([]PermissionLevel(nil), model.PermissionLevels...)
	}
	return nil
}

// PermissionLevelSupported 判断产品权限是否出现在探测声明的列表中。
func PermissionLevelSupported(
	supported []PermissionLevel,
	level PermissionLevel,
) bool {
	if !constants.SelectablePermissionLevel(level) {
		return false
	}
	for _, candidate := range supported {
		if candidate == level {
			return true
		}
	}
	return false
}

// SafePermissionLevel 在探测列表内保留或安全降级默认权限；列表为空时保持原值且不算已降级。
func SafePermissionLevel(
	preferred PermissionLevel,
	supported []PermissionLevel,
) (PermissionLevel, bool) {
	if PermissionLevelSupported(supported, preferred) {
		return preferred, false
	}
	if len(supported) == 0 {
		return preferred, false
	}
	for _, candidate := range []PermissionLevel{
		PermissionLevelAuto,
		PermissionLevelDefault,
	} {
		if PermissionLevelSupported(supported, candidate) {
			return candidate, true
		}
	}
	return supported[0], true
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func cloneModels(values []ModelOption) []ModelOption {
	cloned := make([]ModelOption, len(values))
	for index, model := range values {
		cloned[index] = model
		cloned[index].ReasoningOptions = cloneReasoningOptions(model.ReasoningOptions)
		cloned[index].PermissionLevels = append([]PermissionLevel(nil), model.PermissionLevels...)
	}
	return cloned
}

func cloneReasoningOptions(values []ReasoningOption) []ReasoningOption {
	return append([]ReasoningOption(nil), values...)
}

// ValidateDirectory 返回一个已经存在的绝对目录路径。
func ValidateDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", status_error.BadRequest
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", status_error.RuntimeUnavailable
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", status_error.RuntimeUnavailable
	}
	if !info.IsDir() {
		return "", status_error.BadRequest
	}
	return absolute, nil
}
