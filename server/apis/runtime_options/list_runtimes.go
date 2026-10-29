package runtime_options

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// 模型选项
type RuntimeModelOption struct {
	// 标识
	ID string `json:"id"`
	// 名称
	Name string `json:"name"`
	// 自定义模型
	Custom bool `json:"custom"`
	// 推理选项
	ReasoningOptions []RuntimeReasoningOption `json:"reasoningOptions"`
	// 权限等级
	PermissionLevels []constants.PermissionLevel `json:"permissionLevels"`
}

// 推理选项
type RuntimeReasoningOption struct {
	// 标识
	ID string `json:"id"`
	// 名称
	Name string `json:"name"`
}

// 运行时诊断
type RuntimeDiagnostic struct {
	// 诊断码
	Code string `json:"code"`
	// 严重级别
	Level string `json:"level"`
	// 结果消息
	Message string `json:"message"`
}

// 运行时 CLI 安装实例
type RuntimeInstallation struct {
	// 可执行文件绝对路径
	Path string `json:"path"`
	// 该路径报告的版本
	Version string `json:"version"`
	// 当前路径是否可执行
	Available bool `json:"available"`
}

// 运行时选项
type RuntimeOption struct {
	// 探测能力
	Capabilities runtimecontract.CapabilitySet `json:"capabilities"`
	// 默认账号
	DefaultAccountID string `json:"defaultAccountId"`
	// 基础 CLI 类型，用于账号协议兼容性判断。
	BaseRuntimeID string `json:"baseRuntimeId"`
	// 标识
	ID string `json:"id"`
	// 名称
	Name string `json:"name"`
	// 启用状态
	Enabled bool `json:"enabled"`
	// 是否默认
	IsDefault bool `json:"isDefault"`
	// 运行状态
	State string `json:"state"`
	// 可执行文件是否已在当前探测环境中找到
	Installed bool `json:"installed"`
	// 版本号
	Version string `json:"version"`
	// 当前选中的可执行文件绝对路径
	ResolvedBinary string `json:"resolvedBinary"`
	// 已发现及历史保存的安装实例
	Installations []RuntimeInstallation `json:"installations"`
	// 鉴权结论
	Auth string `json:"auth"`
	// 默认模型
	DefaultModel string `json:"defaultModel"`
	// 默认推理
	DefaultReasoning string `json:"defaultReasoning"`
	// 默认权限
	DefaultPermissionLevel constants.PermissionLevel `json:"defaultPermissionLevel"`
	// 自定义模型
	CustomModels []string `json:"customModels"`
	// 模型列表
	Models []RuntimeModelOption `json:"models"`
	// 推理选项
	ReasoningOptions []RuntimeReasoningOption `json:"reasoningOptions"`
	// 诊断信息
	Diagnostics []RuntimeDiagnostic `json:"diagnostics"`
	// 可执行路径
	Binary string `json:"binary"`
	// 前置参数
	PrefixArgs []string `json:"prefixArgs"`
	// 配置目录
	ConfigHome string `json:"configHome"`
	// 支持配置目录
	ConfigHomeSupported bool `json:"configHomeSupported"`
	// 探测时间
	DetectedAt int64 `json:"detectedAt"`
}

// 运行时列表
type ListRuntimes struct {
	ginx.MethodGet
}

// 运行时列表响应
type ListRuntimesResponse struct {
	// 资源列表
	Resources []RuntimeOption `json:"resources"`
	// 启动探测中
	StartupDetectionPending bool `json:"startupDetectionPending"`
	// 启动探测失败
	StartupDetectionFailed bool `json:"startupDetectionFailed"`
	// 启动需要引导
	StartupSetupRequired bool `json:"startupSetupRequired"`
}

// Path 返回 Runtime 列表接口路径。
func (l *ListRuntimes) Path() string {
	return ""
}

// Output 查询持久化 Runtime 环境条目并转换为前端稳定 DTO。
func (l *ListRuntimes) Output(ctx *gin.Context) (interface{}, error) {
	if _, err := global.RuntimeEnvironmentService.EnsureInitialized(ctx); err != nil {
		return nil, ginx.WithStack(err)
	}
	// 先读生命周期状态，再读持久化结果，避免用结束后的状态配对结束前的旧列表。
	pending, failed, setupRequired := global.RuntimeEnvironmentService.StartupDetection()
	environments, err := global.RuntimeEnvironmentControllerManager.List(ctx)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	resources := make([]RuntimeOption, 0, len(environments))
	for _, environment := range environments {
		resources = append(resources, runtimeOption(environment))
	}
	return ListRuntimesResponse{Resources: resources, StartupDetectionPending: pending, StartupDetectionFailed: failed, StartupSetupRequired: setupRequired}, nil
}

func runtimeOption(environment models.RuntimeEnvironment) RuntimeOption {
	detection := environment.Detection
	defaultModel, defaultReasoning := runtimecontract.NormalizeDetectedDefaults(
		detection,
		environment.CustomModels,
		environment.DefaultModel,
		environment.DefaultReasoning,
	)
	availableModels := runtimecontract.AvailableModels(detection, environment.CustomModels)
	detectedModelIDs := make(map[string]struct{}, len(detection.Models))
	for _, model := range detection.Models {
		detectedModelIDs[model.ID] = struct{}{}
	}
	models := make([]RuntimeModelOption, 0, len(availableModels))
	for _, model := range availableModels {
		_, detected := detectedModelIDs[model.ID]
		models = append(models, RuntimeModelOption{
			ID:               model.ID,
			Name:             model.Name,
			Custom:           !detected,
			ReasoningOptions: runtimeReasoningOptions(model.ReasoningOptions),
			PermissionLevels: runtimePermissionLevels(model.PermissionLevels),
		})
	}
	reasoningSource := detection.ReasoningOptions
	if reasoningSource == nil && detection.Runtime.BaseRuntimeID != runtimecontract.RuntimeIDCodex {
		reasoningSource = detection.Runtime.ReasoningOptions
	}
	reasoningOptions := runtimeReasoningOptions(reasoningSource)
	diagnostics := make([]RuntimeDiagnostic, 0, len(detection.Diagnostics))
	for _, diagnostic := range detection.Diagnostics {
		diagnostics = append(diagnostics, RuntimeDiagnostic{
			Code:    diagnostic.Code,
			Level:   string(diagnostic.Level),
			Message: diagnostic.Message,
		})
	}
	return RuntimeOption{
		Capabilities:           detection.Capabilities,
		DefaultAccountID:       environment.DefaultAccountID,
		BaseRuntimeID:          environment.BaseRuntimeID,
		ID:                     environment.RuntimeID,
		Name:                   environment.Name,
		Enabled:                environment.Enabled,
		IsDefault:              environment.IsDefault,
		State:                  string(detection.State),
		Installed:              detection.ResolvedBinary != "",
		Version:                detection.Version,
		ResolvedBinary:         detection.ResolvedBinary,
		Installations:          runtimeInstallations(detection.Installations),
		Auth:                   string(detection.Auth),
		DefaultModel:           defaultModel,
		DefaultReasoning:       defaultReasoning,
		DefaultPermissionLevel: environment.DefaultPermissionLevel,
		CustomModels:           append([]string(nil), environment.CustomModels...),
		Models:                 models,
		ReasoningOptions:       reasoningOptions,
		Diagnostics:            diagnostics,
		Binary:                 environment.Binary,
		PrefixArgs:             append([]string(nil), environment.PrefixArgs...),
		ConfigHome:             environment.ConfigHome,
		ConfigHomeSupported:    detection.Runtime.ConfigHomeEnvironmentVariable != "",
		DetectedAt:             environment.DetectedAt,
	}
}

func runtimeInstallations(installations []runtimecontract.CLIInstallation) []RuntimeInstallation {
	result := make([]RuntimeInstallation, 0, len(installations))
	for _, installation := range installations {
		result = append(result, RuntimeInstallation{
			Path: installation.Path, Version: installation.Version, Available: installation.Available,
		})
	}
	return result
}

func runtimePermissionLevels(levels []runtimecontract.PermissionLevel) []constants.PermissionLevel {
	result := make([]constants.PermissionLevel, 0, len(levels))
	for _, level := range levels {
		if constants.SelectablePermissionLevel(level) {
			result = append(result, level)
		}
	}
	return result
}

func runtimeReasoningOptions(
	options []runtimecontract.ReasoningOption,
) []RuntimeReasoningOption {
	result := make([]RuntimeReasoningOption, 0, len(options))
	for _, option := range options {
		result = append(result, RuntimeReasoningOption{
			ID:   option.ID,
			Name: option.Name,
		})
	}
	return result
}
