package runtime_options

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"

	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestRuntimeOptionIncludesDetectedModelReasoningOptions(t *testing.T) {
	detection := agentapi.DetectionResult{
		Runtime: agentapi.RuntimeSummary{
			ID:            agentapi.RuntimeIDCodex,
			BaseRuntimeID: agentapi.RuntimeIDCodex,
			Name:          "Codex CLI",
		},
		DetectedDefaultModel:     "gpt-stable",
		DetectedDefaultReasoning: "high",
		Models: []agentapi.ModelOption{
			{
				ID:   "gpt-stable",
				Name: "GPT Stable",
				ReasoningOptions: []agentapi.ReasoningOption{
					{ID: "high", Name: "High"},
					{ID: "medium", Name: "Medium"},
				},
			},
		},
		ReasoningOptions: []agentapi.ReasoningOption{
			{ID: "high", Name: "High"},
			{ID: "medium", Name: "Medium"},
		},
	}

	option := runtimeOption(models.RuntimeEnvironment{
		RuntimeID:        agentapi.RuntimeIDCodex,
		Name:             "Codex CLI",
		DefaultModel:     "stale-model",
		DefaultReasoning: "stale-reasoning",
		Detection:        detection,
	})
	want := []RuntimeReasoningOption{
		{ID: "high", Name: "High"},
		{ID: "medium", Name: "Medium"},
	}
	if !reflect.DeepEqual(option.ReasoningOptions, want) {
		t.Fatalf(
			"ReasoningOptions = %#v, want %#v",
			option.ReasoningOptions,
			want,
		)
	}
	if option.DefaultModel != "gpt-stable" || option.DefaultReasoning != "high" {
		t.Fatalf(
			"defaults = %q/%q, want gpt-stable/high",
			option.DefaultModel,
			option.DefaultReasoning,
		)
	}
	if !reflect.DeepEqual(option.Models[0].ReasoningOptions, want) {
		t.Fatalf(
			"Models[0].ReasoningOptions = %#v, want %#v",
			option.Models[0].ReasoningOptions,
			want,
		)
	}
}

func TestRuntimeOptionSeparatesExecutableDiscoveryFromReadiness(t *testing.T) {
	for _, test := range []struct {
		// name 是当前探测情形的名称。
		name string
		// path 是探测到的可执行文件路径，空值表示未安装。
		path string
		// state 是协议探测的就绪结论。
		state agentapi.DetectionState
		// installed 是设置页应展示的安装状态。
		installed bool
	}{
		{name: "已安装但协议不可用", path: "/usr/local/bin/kimi", state: agentapi.DetectionStateUnavailable, installed: true},
		{name: "未安装", state: agentapi.DetectionStateUnavailable, installed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			option := runtimeOption(models.RuntimeEnvironment{Detection: agentapi.DetectionResult{
				State: test.state, ResolvedBinary: test.path,
			}})
			if option.Installed != test.installed || option.State != string(test.state) {
				t.Fatalf("RuntimeOption = %+v", option)
			}
		})
	}
}

func TestRuntimeOptionIncludesCustomModels(t *testing.T) {
	detection := agentapi.DetectionResult{
		DetectedDefaultModel: "gpt-stable",
		Models:               []agentapi.ModelOption{{ID: "gpt-stable", Name: "GPT Stable"}},
		ReasoningOptions: []agentapi.ReasoningOption{
			{ID: "high", Name: "High"},
		},
	}
	option := runtimeOption(models.RuntimeEnvironment{
		RuntimeID:    agentapi.RuntimeIDCodex,
		CustomModels: []string{"gpt-team-custom"},
		DefaultModel: "gpt-team-custom",
		Detection:    detection,
	})
	if option.DefaultModel != "gpt-team-custom" {
		t.Fatalf("DefaultModel = %q, want gpt-team-custom", option.DefaultModel)
	}
	if len(option.Models) != 2 || option.Models[1].ID != "gpt-team-custom" || !option.Models[1].Custom {
		t.Fatalf("Models = %#v, want detected and custom models", option.Models)
	}
	if !reflect.DeepEqual(option.CustomModels, []string{"gpt-team-custom"}) {
		t.Fatalf("CustomModels = %#v, want persisted custom model", option.CustomModels)
	}
}

func TestNativeModelOptionPreservesProviderMenus(t *testing.T) {
	for _, provider := range []string{"claude", "cursor"} {
		t.Run(provider, func(t *testing.T) {
			on, off := "on", "off"
			if provider == "cursor" {
				on, off = "true", "false"
			}
			row := agent.AgentChoice{ID: "native-model", AutoMode: provider == "claude", Config: &agent.AgentModelConfig{Options: []agent.AgentSessionOption{
				{ID: "effort", Category: "thought_level", Value: agent.AgentOptionValue{ID: "max"}, Choices: []agent.AgentChoice{{ID: "max", Name: "Max"}}},
				{ID: "fast", Value: agent.AgentOptionValue{ID: off}, Choices: []agent.AgentChoice{{ID: on}, {ID: off}}},
			}}}
			got := nativeModelOption(provider, row)
			if !got.FastSupported || got.DefaultFast == nil || *got.DefaultFast || got.DefaultReasoning != "max" || len(got.ReasoningOptions) != 1 {
				t.Fatalf("provider menus lost: %+v", got)
			}
			want := 2
			if provider == "claude" {
				want = 3
			}
			if len(got.PermissionLevels) != want {
				t.Fatalf("permissions = %v", got.PermissionLevels)
			}
		})
	}
}

func TestQoderNativeModelKeepsExactReasoningChoices(t *testing.T) {
	for _, provider := range []string{"qoder", "qoder-cn"} {
		row := agent.AgentChoice{
			ID: "qfmodel", Name: "Qwen3.8-Flash (default)",
			Config: &agent.AgentModelConfig{Options: []agent.AgentSessionOption{{
				ID: "reasoning_effort", Category: "thought_level", Value: agent.AgentOptionValue{ID: "xhigh"},
				Choices: []agent.AgentChoice{{ID: "xhigh", Name: "Extra High"}, {ID: "low", Name: "Low"},
					{ID: "medium", Name: "Medium"}, {ID: "none", Name: "None"}},
			}}},
		}
		want := []RuntimeReasoningOption{{ID: "xhigh", Name: "Extra High"}, {ID: "low", Name: "Low"},
			{ID: "medium", Name: "Medium"}, {ID: "none", Name: "None"}}
		got := nativeModelOption(provider, row)
		if got.ID != "qfmodel" || got.DefaultReasoning != "xhigh" || !got.ConfigurationKnown ||
			got.FastSupported || !reflect.DeepEqual(got.ReasoningOptions, want) {
			t.Fatalf("%s changed the native Qoder choices: %+v", provider, got)
		}
	}
}

func TestNativeModelOptionKeepsUnknownSettingsOutOfMenus(t *testing.T) {
	for _, unknown := range [][]string{{"reasoning", "fast"}, {"effort", "service_tier"}, {"thought_level", "fast"}} {
		tier := "priority"
		row := agent.AgentChoice{ID: "listed-model", PermissionModes: []string{"default"}, FastServiceTier: tier, DefaultServiceTier: &tier,
			Config: &agent.AgentModelConfig{UnknownOptionIDs: unknown, Options: []agent.AgentSessionOption{
				{ID: "effort", Category: "thought_level", Value: agent.AgentOptionValue{ID: "high"}, Choices: []agent.AgentChoice{{ID: "high"}}},
				{ID: "fast", Value: agent.AgentOptionValue{ID: "on"}},
				{ID: "service_tier", Choices: []agent.AgentChoice{{ID: "priority"}}},
			}},
		}
		got := nativeModelOption("workbuddy", row)
		if got.ID != row.ID || len(got.PermissionLevels) != 1 || got.ConfigurationKnown {
			t.Fatalf("listed model or independent permission evidence lost: %+v", got)
		}
		if len(got.ReasoningOptions) != 0 || got.DefaultReasoning != "" || got.FastSupported || got.DefaultFast != nil || len(got.ServiceTiers) != 0 || got.DefaultServiceTier != nil {
			t.Fatalf("unknown settings %v leaked into choices: %+v", unknown, got)
		}
	}
}
