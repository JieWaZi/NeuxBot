package execution

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// configurationEnvironmentReader 提供执行接纳时冻结的模型与强度目录。
type configurationEnvironmentReader struct {
	// environment 保存当前智能体的默认配置和实际探测结果。
	environment models.RuntimeEnvironment
}

// Get 返回同一份环境快照，避免测试依赖本机 CLI 与持久化设置。
func (reader configurationEnvironmentReader) Get(context.Context, string) (models.RuntimeEnvironment, error) {
	return reader.environment, nil
}

// configurationRuntime 提供 WorkBuddy 原生工作目录布局，不调用外部 CLI。
type configurationRuntime struct {
	// accountRuntime 复用既有执行端口与其他智能体目录。
	accountRuntime
}

// Runtimes 使配置解析继续执行真实的工作目录布局校验。
func (runtime *configurationRuntime) Runtimes() []runtimeconfig.RuntimeSummary {
	return append(runtime.accountRuntime.Runtimes(), runtimeconfig.RuntimeSummary{
		ID: constants.RuntimeIDWorkBuddy, BaseRuntimeID: constants.RuntimeIDWorkBuddy,
		InstructionFileName: "CODEBUDDY.md", SkillDirectoryName: ".codebuddy",
	})
}

// TestResolveConfigurationUsesSelectedModelReasoning 覆盖不同模型不能直接借用环境默认模型强度。
func TestResolveConfigurationUsesSelectedModelReasoning(t *testing.T) {
	step := readyTestModel("step-5-preview")
	step.ReasoningOptions = []runtimeconfig.ReasoningOption{{ID: "minimal"}, {ID: "high"}}
	hy3 := readyTestModel("hy3")
	hy4 := readyTestModel("hy4-preview")
	hy4.ReasoningOptions = []runtimeconfig.ReasoningOption{{ID: "high"}}
	environment := models.RuntimeEnvironment{
		RuntimeID: constants.RuntimeIDWorkBuddy, BaseRuntimeID: constants.RuntimeIDWorkBuddy,
		Enabled: true, DefaultModel: step.ID, DefaultReasoning: "minimal",
		Detection: runtimeconfig.DetectionResult{
			State:  runtimeconfig.DetectionStateReady,
			Models: []runtimeconfig.ModelOption{step, hy3, hy4},
		},
	}
	tests := []struct {
		// name 描述本次默认值解析或显式校验场景。
		name string
		// model 是用户选择的模型，空值表示环境默认模型。
		model string
		// reasoning 是用户显式选择的强度，空值表示继承默认配置。
		reasoning string
		// resolved 表示配置是否已经在先前接纳时冻结。
		resolved bool
		// withoutDefault 表示环境未配置默认强度，需保留 CLI 自身的默认选择。
		withoutDefault bool
		// wantModel 是最终执行所用的明确模型。
		wantModel string
		// wantReasoning 是目标模型实际支持的默认或显式强度。
		wantReasoning string
		// wantError 表示显式的无效选择必须继续被拒绝。
		wantError bool
	}{
		{name: "no concrete grades", model: hy3.ID, wantModel: hy3.ID},
		{name: "different grades", model: hy4.ID, wantModel: hy4.ID, wantReasoning: "high"},
		{name: "default model", wantModel: step.ID, wantReasoning: "minimal"},
		{name: "same grades", model: step.ID, wantModel: step.ID, wantReasoning: "minimal"},
		{name: "explicit valid grade", model: step.ID, reasoning: "high", wantModel: step.ID, wantReasoning: "high"},
		{name: "explicit invalid grade", model: hy4.ID, reasoning: "minimal", wantError: true},
		{name: "unknown model", model: "missing", wantError: true},
		{name: "frozen empty grade", model: hy4.ID, resolved: true, wantModel: hy4.ID},
		{name: "empty native default", model: step.ID, withoutDefault: true, wantModel: step.ID},
		{name: "empty default on another model", model: hy4.ID, withoutDefault: true, wantModel: hy4.ID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentEnvironment := environment
			if test.withoutDefault {
				currentEnvironment.DefaultReasoning = ""
			}
			manager := &service{runtime: &configurationRuntime{}, runtimeEnvironments: configurationEnvironmentReader{environment: currentEnvironment}}
			resolved, err := manager.ResolveConfiguration(context.Background(), ExecutionConfiguration{
				RuntimeID: environment.RuntimeID, AccountID: constants.AiAccountSystemID,
				Model: test.model, Reasoning: test.reasoning, Resolved: test.resolved,
				PermissionLevel: constants.PermissionLevelDefault,
			})
			if test.wantError {
				if !errors.Is(err, status_error.BadRequest) {
					t.Fatalf("invalid selection error = %v, want BadRequest", err)
				}
				return
			}
			if err != nil || !resolved.Resolved || resolved.Model != test.wantModel || resolved.Reasoning != test.wantReasoning {
				t.Fatalf("resolved = %+v, error = %v; want model=%q reasoning=%q", resolved, err, test.wantModel, test.wantReasoning)
			}
		})
	}
}

func TestValidateExecutionConfigurationSupportsCodexAndClaudeMatrix(t *testing.T) {
	permissionLevels := []runtimeconfig.PermissionLevel{
		runtimeconfig.PermissionLevelDefault,
		runtimeconfig.PermissionLevelAuto,
		runtimeconfig.PermissionLevelFullAccess,
	}
	tests := []struct {
		// name 是 Runtime 与模型组合的子测试名称。
		name string
		// environment 是创建或更新团队成员时读取的冻结探测快照。
		environment models.RuntimeEnvironment
		// reasoning 是该模型显式选择的推理强度；空值表示使用 Runtime 默认值。
		reasoning string
	}{
		{
			name: "codex",
			environment: models.RuntimeEnvironment{
				RuntimeID:    runtimeconfig.RuntimeIDCodex,
				Enabled:      true,
				DefaultModel: "gpt-5.6",
				Detection: runtimeconfig.DetectionResult{
					State:        runtimeconfig.DetectionStateReady,
					Capabilities: runtimeconfig.CapabilitySet{runtimeconfig.CapabilityMCPStreamableHTTP: true},
					Models: []runtimeconfig.ModelOption{{
						ID:               "gpt-5.6",
						ReasoningOptions: []runtimeconfig.ReasoningOption{{ID: "low"}, {ID: "high"}},
						PermissionLevels: permissionLevels,
					}},
				},
			},
			reasoning: "high",
		},
		{
			name: "claude",
			environment: models.RuntimeEnvironment{
				RuntimeID:    runtimeconfig.RuntimeIDClaude,
				Enabled:      true,
				DefaultModel: "claude-sonnet",
				Detection: runtimeconfig.DetectionResult{
					State:        runtimeconfig.DetectionStateReady,
					Capabilities: runtimeconfig.CapabilitySet{runtimeconfig.CapabilityMCPStreamableHTTP: true},
					Models: []runtimeconfig.ModelOption{{
						ID:               "claude-sonnet",
						PermissionLevels: permissionLevels,
					}},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, permissionLevel := range permissionLevels {
				err := validateExecutionConfiguration(test.environment, ExecutionConfiguration{
					Model:                      test.environment.DefaultModel,
					Reasoning:                  test.reasoning,
					PermissionLevel:            permissionLevel,
					RequiredConnectorTransport: constants.RuntimeConnectorTransportStreamableHTTP,
				})
				if err != nil {
					t.Fatalf("permission %q rejected: %v", permissionLevel, err)
				}
			}
		})
	}
}

func TestValidateExecutionConfigurationDefersUnknownPermissionsWhenLoginIsMissing(t *testing.T) {
	environment := models.RuntimeEnvironment{
		RuntimeID: runtimeconfig.RuntimeIDKimi,
		Enabled:   true,
		Detection: runtimeconfig.DetectionResult{
			State: runtimeconfig.DetectionStateReady,
			Auth:  runtimeconfig.AuthStatusMissing,
		},
	}
	for _, level := range []runtimeconfig.PermissionLevel{
		runtimeconfig.PermissionLevelDefault,
		runtimeconfig.PermissionLevelAuto,
		runtimeconfig.PermissionLevelFullAccess,
	} {
		if err := validateExecutionConfiguration(environment, ExecutionConfiguration{PermissionLevel: level}); err != nil {
			t.Fatalf("未登录且尚无权限目录时不应提前拒绝 %q: %v", level, err)
		}
	}
}

func TestValidateExecutionConfigurationRejectsUnavailableCombination(t *testing.T) {
	environment := models.RuntimeEnvironment{
		RuntimeID:    runtimeconfig.RuntimeIDCodex,
		Enabled:      true,
		DefaultModel: "gpt-5.6",
		Detection: runtimeconfig.DetectionResult{
			State: runtimeconfig.DetectionStateReady,
			Models: []runtimeconfig.ModelOption{{
				ID:               "gpt-5.6",
				ReasoningOptions: []runtimeconfig.ReasoningOption{{ID: "low"}},
				PermissionLevels: []runtimeconfig.PermissionLevel{runtimeconfig.PermissionLevelDefault},
			}},
		},
	}
	tests := []struct {
		// name 是非法组合的子测试名称。
		name string
		// mutate 把合法基线改成一个必须被拒绝的配置或环境。
		mutate func(*models.RuntimeEnvironment, *ExecutionConfiguration)
		// expected 是对外稳定的错误类别。
		expected error
	}{
		{
			name: "unknown model",
			mutate: func(_ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
				config.Model = "missing-model"
			},
			expected: status_error.BadRequest,
		},
		{
			name: "unsupported reasoning",
			mutate: func(_ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
				config.Reasoning = "ultra"
			},
			expected: status_error.BadRequest,
		},
		{
			name: "unsupported permission",
			mutate: func(_ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
				config.PermissionLevel = constants.PermissionLevelFullAccess
			},
			expected: status_error.BadRequest,
		},
		{
			name: "disabled runtime",
			mutate: func(environment *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
				environment.Enabled = false
			},
			expected: status_error.RuntimeUnavailable,
		},
		{
			name: "stale detection",
			mutate: func(environment *models.RuntimeEnvironment, _ *ExecutionConfiguration) {
				environment.Detection.State = runtimeconfig.DetectionStateUnavailable
			},
			expected: status_error.RuntimeUnavailable,
		},
		{
			name: "missing team transport",
			mutate: func(_ *models.RuntimeEnvironment, config *ExecutionConfiguration) {
				config.RequiredConnectorTransport = constants.RuntimeConnectorTransportStreamableHTTP
			},
			expected: status_error.RuntimeUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentEnvironment := environment
			config := ExecutionConfiguration{
				Model:           environment.DefaultModel,
				Reasoning:       "low",
				PermissionLevel: constants.PermissionLevelDefault,
			}
			test.mutate(&currentEnvironment, &config)
			if err := validateExecutionConfiguration(currentEnvironment, config); !errors.Is(err, test.expected) {
				t.Fatalf("error = %v, want %v", err, test.expected)
			}
		})
	}
}
