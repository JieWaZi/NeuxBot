package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
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
}

// Runtimes 使配置解析继续执行真实的工作目录布局校验。
func (runtime *configurationRuntime) Runtimes() []agentapi.RuntimeSummary {
	return []agentapi.RuntimeSummary{{
		ID: constants.RuntimeIDWorkBuddy, BaseRuntimeID: constants.RuntimeIDWorkBuddy,
		InstructionFileName: "CODEBUDDY.md", SkillDirectoryName: ".codebuddy",
	}}
}

// TestResolveConfigurationUsesSelectedModelReasoning 覆盖不同模型不能直接借用环境默认模型强度。
func TestResolveConfigurationUsesSelectedModelReasoning(t *testing.T) {
	step := readyTestModel("step-5-preview")
	step.ReasoningOptions = []agentapi.ReasoningOption{{ID: "minimal"}, {ID: "high"}}
	hy3 := readyTestModel("hy3")
	hy4 := readyTestModel("hy4-preview")
	hy4.ReasoningOptions = []agentapi.ReasoningOption{{ID: "high"}}
	environment := models.RuntimeEnvironment{
		RuntimeID: constants.RuntimeIDWorkBuddy, BaseRuntimeID: constants.RuntimeIDWorkBuddy,
		Enabled: true, DefaultModel: step.ID, DefaultReasoning: "minimal",
		Detection: agentapi.DetectionResult{
			State:  agentapi.DetectionStateReady,
			Models: []agentapi.ModelOption{step, hy3, hy4},
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
	}{
		{name: "no concrete grades", model: hy3.ID, wantModel: hy3.ID},
		{name: "different model keeps native grade", model: hy4.ID, wantModel: hy4.ID},
		{name: "default model", wantModel: step.ID, wantReasoning: "minimal"},
		{name: "same grades", model: step.ID, wantModel: step.ID, wantReasoning: "minimal"},
		{name: "explicit valid grade", model: step.ID, reasoning: "high", wantModel: step.ID, wantReasoning: "high"},
		{name: "explicit grade goes to native setter", model: hy4.ID, reasoning: "minimal", wantModel: hy4.ID, wantReasoning: "minimal"},
		{name: "explicit model goes to native setter", model: "missing", wantModel: "missing"},
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
			manager := &service{runtimeSummaries: (&configurationRuntime{}).Runtimes(), runtimeEnvironments: configurationEnvironmentReader{environment: currentEnvironment}}
			resolved, err := manager.ResolveConfiguration(context.Background(), ExecutionConfiguration{
				RuntimeID: environment.RuntimeID, AccountID: constants.AiAccountSystemID,
				Model: test.model, Reasoning: test.reasoning, Resolved: test.resolved,
				PermissionLevel: constants.PermissionLevelDefault,
			})
			if err != nil || !resolved.Resolved || resolved.Model != test.wantModel || resolved.Reasoning != test.wantReasoning {
				t.Fatalf("resolved = %+v, error = %v; want model=%q reasoning=%q", resolved, err, test.wantModel, test.wantReasoning)
			}
		})
	}
}

// TestExecutionIgnoresFailedProbe 验证失败快照不能阻止配置解析或实际原生首发。
func TestExecutionIgnoresFailedProbe(t *testing.T) {
	for _, state := range []agentapi.DetectionState{"", agentapi.DetectionStateUnavailable, agentapi.DetectionStateIncompatible} {
		t.Run(string(state), func(t *testing.T) {
			environment := models.RuntimeEnvironment{
				RuntimeID: "codex", BaseRuntimeID: "codex", Enabled: true,
				DefaultModel: "fixture-model", DefaultReasoning: "high",
				Detection: agentapi.DetectionResult{State: state, Auth: agentapi.AuthStatusUnknown},
			}
			manager := newMessageTestService(t, t.TempDir(), messageTestOptions{
				runtimeEnvironments: configurationEnvironmentReader{environment: environment},
			})
			resolved, err := manager.execution.ResolveConfiguration(t.Context(), ExecutionConfiguration{
				RuntimeID: "codex", PermissionLevel: constants.PermissionLevelFullAccess,
				RequiredConnectorTransport: constants.RuntimeConnectorTransportStreamableHTTP,
			})
			if err != nil || resolved.Model != "fixture-model" || resolved.Reasoning != "high" {
				t.Fatalf("probe changed configured selection: %+v, error=%v", resolved, err)
			}
			accepted, err := manager.sendTestMessage(t.Context(), testMessageInput{
				NewChat: &testChatInput{WakerID: "waker-1"}, Message: userMessage{Text: "try actual runtime"},
				Runtime: runtimeSelection{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelFullAccess},
			})
			if err != nil {
				t.Fatalf("probe blocked actual launch: %v", err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			if len(manager.nativeCalls(t, "turn/start")) != 1 {
				t.Fatal("input did not reach native protocol")
			}
		})
	}
}

// TestResolveConfigurationFreezesFast 验证 Fast 默认在解析边界固定，已解析空值不再继承。
func TestResolveConfigurationFreezesFast(t *testing.T) {
	for _, defaultValue := range []bool{false, true} {
		t.Run(map[bool]string{false: "默认关闭", true: "默认开启"}[defaultValue], func(t *testing.T) {
			environment := models.RuntimeEnvironment{RuntimeID: constants.RuntimeIDWorkBuddy, BaseRuntimeID: constants.RuntimeIDWorkBuddy, Enabled: true, DefaultFast: &defaultValue}
			manager := &service{runtimeSummaries: (&configurationRuntime{}).Runtimes(), runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
			config := ExecutionConfiguration{RuntimeID: environment.RuntimeID, PermissionLevel: constants.PermissionLevelDefault}
			selected, err := manager.ResolveConfiguration(t.Context(), config)
			if err != nil || selected.Fast == nil || *selected.Fast != defaultValue {
				t.Fatalf("effective Fast was not frozen: %+v error=%v", selected, err)
			}
			*selected.Fast = !defaultValue
			if *environment.DefaultFast != defaultValue {
				t.Fatal("effective Fast aliases mutable runtime defaults")
			}
			native, err := manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{Resolved: true, RuntimeID: environment.RuntimeID, PermissionLevel: constants.PermissionLevelDefault})
			if err != nil || native.Fast != nil {
				t.Fatalf("resolved native Fast was replaced: %+v error=%v", native, err)
			}
			explicit := !defaultValue
			selected, err = manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{RuntimeID: environment.RuntimeID, Fast: &explicit, PermissionLevel: constants.PermissionLevelDefault})
			if err != nil || selected.Fast == nil || *selected.Fast != explicit {
				t.Fatalf("explicit Fast was replaced: %+v error=%v", selected, err)
			}
		})
	}
}

// TestNativeContinuationKeepsFrozenFast 验证真实原生派发沿用已接纳 Fast，显式 false 仍可覆盖。
func TestNativeContinuationKeepsFrozenFast(t *testing.T) {
	for _, defaultValue := range []bool{false, true} {
		t.Run(map[bool]string{false: "关闭后默认开启", true: "开启后默认关闭"}[defaultValue], func(t *testing.T) {
			manager := newMessageTestService(t, t.TempDir())
			core := manager.execution.(*service)
			environment, err := core.runtimeEnvironments.Get(t.Context(), constants.RuntimeIDCodex)
			if err != nil {
				t.Fatal(err)
			}
			environment.DefaultModel, environment.DefaultFast = "fixture-model", &defaultValue
			core.runtimeEnvironments = configurationEnvironmentReader{environment: environment}
			first, err := manager.sendTestMessage(t.Context(), testMessageInput{NewChat: &testChatInput{WakerID: "waker-1"}, Message: userMessage{Text: "first fast selection"}, Runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex}})
			if err != nil {
				t.Fatal(err)
			}
			if first.Execution.Fast == nil || *first.Execution.Fast != defaultValue {
				t.Fatalf("first admission did not freeze Fast: %+v", first.Execution)
			}
			waitForChatTerminal(t, manager, first.Chat.ID)
			changed := !defaultValue
			environment.DefaultFast = &changed
			core.runtimeEnvironments = configurationEnvironmentReader{environment: environment}
			second, err := manager.sendTestMessage(t.Context(), testMessageInput{ChatID: first.Chat.ID, Message: userMessage{Text: "continue fast selection"}, Runtime: runtimeSelection{RuntimeID: constants.RuntimeIDCodex}})
			if err != nil {
				t.Fatal(err)
			}
			if second.Execution.Fast == nil || *second.Execution.Fast != defaultValue {
				t.Fatalf("continuation re-inherited Fast: %+v", second.Execution)
			}
			waitForChatTerminal(t, manager, first.Chat.ID)
			third, err := core.Start(t.Context(), WorkIntent{Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", Content: "explicit fast selection"}, Options: WorkOptions{RuntimeID: constants.RuntimeIDCodex, Fast: &changed, PermissionLevel: constants.PermissionLevelAuto}})
			if err != nil {
				t.Fatal(err)
			}
			if third.Execution.Fast == nil || *third.Execution.Fast != changed {
				t.Fatalf("explicit Fast did not override prior selection: %+v", third.Execution)
			}
			waitForChatTerminal(t, manager, first.Chat.ID)
		})
	}
}

// TestSystemAccountDoesNotInheritManagedAccountModel 验证显式原生账号不会借用受管账号的默认模型。
func TestSystemAccountDoesNotInheritManagedAccountModel(t *testing.T) {
	for _, defaultAccount := range []string{"managed", constants.AiAccountSystemID, ""} {
		for _, frozen := range []bool{false, true} {
			for _, model := range []string{"", "explicit-model"} {
				environment := models.RuntimeEnvironment{RuntimeID: constants.RuntimeIDWorkBuddy, BaseRuntimeID: constants.RuntimeIDWorkBuddy, Enabled: true, DefaultAccountID: defaultAccount, DefaultModel: "managed-model", DefaultReasoning: "high"}
				manager := &service{runtimeSummaries: (&configurationRuntime{}).Runtimes(), runtimeEnvironments: configurationEnvironmentReader{environment: environment}}
				selected, err := manager.ResolveConfiguration(t.Context(), ExecutionConfiguration{RuntimeID: environment.RuntimeID, AccountID: constants.AiAccountSystemID, Model: model, Resolved: frozen, PermissionLevel: constants.PermissionLevelDefault})
				expected := model
				if model == "" && !frozen && defaultAccount != "managed" {
					expected = "managed-model"
				}
				if err != nil || selected.Model != expected {
					t.Fatalf("defaultAccount=%q frozen=%v explicit=%q: model=%q want=%q error=%v", defaultAccount, frozen, model, selected.Model, expected, err)
				}
			}
		}
	}
}
