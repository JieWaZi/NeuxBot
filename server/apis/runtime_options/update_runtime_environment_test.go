package runtime_options

import (
	"testing"

	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestConfigurationRequiresDetectRefreshOnlyForLaunchChanges(t *testing.T) {
	current := models.RuntimeEnvironment{
		Binary:       "codex",
		PrefixArgs:   []string{"--profile", "ally"},
		ConfigHome:   "/tmp/codex-home",
		DefaultModel: "gpt-5",
	}
	permissionOnly := current
	permissionOnly.DefaultPermissionLevel = "full-access"
	if configurationRequiresDetectRefresh(current, permissionOnly) {
		t.Fatal("permission-only update should not require detect refresh")
	}
	binaryChanged := current
	binaryChanged.Binary = "/usr/local/bin/codex"
	if !configurationRequiresDetectRefresh(current, binaryChanged) {
		t.Fatal("binary change should require detect refresh")
	}
}

func TestCanCompleteStartupSetupRequiresDetectedModelsAndPermissions(t *testing.T) {
	ready := models.RuntimeEnvironment{
		Enabled:                true,
		IsDefault:              true,
		DefaultModel:           "free-model",
		DefaultPermissionLevel: runtimecontract.PermissionLevelFullAccess,
		CustomModels:           []string{},
		Detection: runtimecontract.DetectionResult{
			ResolvedBinary: "/installed/opencode",
			State:          runtimecontract.DetectionStateReady,
			Models: []runtimecontract.ModelOption{{
				ID:               "free-model",
				PermissionLevels: []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelFullAccess},
			}},
			Diagnostics: []runtimecontract.Diagnostic{},
		},
	}
	tests := []struct {
		// name 说明需要保留引导或允许完成的场景。
		name string
		// change 修改所选智能体的真实探测与默认配置。
		change func(*models.RuntimeEnvironment)
		// allowed 表示是否允许解除首次引导。
		allowed bool
	}{
		{name: "模型与权限就绪", allowed: true},
		{name: "所选智能体探测未就绪", change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.State = runtimecontract.DetectionStateUnavailable
		}},
		{name: "尚未设为默认", change: func(environment *models.RuntimeEnvironment) {
			environment.IsDefault = false
		}},
		{name: "只有安装证据", change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.Models = []runtimecontract.ModelOption{}
		}},
		{name: "没有默认模型", change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultModel = ""
		}},
		{name: "默认模型已失效", change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultModel = "removed-model"
		}},
		{name: "默认权限不支持", change: func(environment *models.RuntimeEnvironment) {
			environment.DefaultPermissionLevel = runtimecontract.PermissionLevelAuto
		}},
		{name: "旧目录仍在但刷新失败", change: func(environment *models.RuntimeEnvironment) {
			environment.Detection.Diagnostics = []runtimecontract.Diagnostic{{Code: runtimecontract.DiagnosticCodeACPOptionsUnavailable}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := ready
			if test.change != nil {
				test.change(&environment)
			}
			if got := canCompleteStartupSetup(environment); got != test.allowed {
				t.Fatalf("canCompleteStartupSetup() = %v, want %v", got, test.allowed)
			}
		})
	}
}
