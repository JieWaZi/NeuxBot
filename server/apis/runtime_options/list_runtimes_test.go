package runtime_options

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestRuntimeOptionIncludesDetectedModelReasoningOptions(t *testing.T) {
	detection := runtimeconfig.DetectionResult{
		Runtime: runtimeconfig.RuntimeSummary{
			ID:            runtimeconfig.RuntimeIDCodex,
			BaseRuntimeID: runtimeconfig.RuntimeIDCodex,
			Name:          "Codex CLI",
		},
		DetectedDefaultModel:     "gpt-stable",
		DetectedDefaultReasoning: "high",
		Models: []runtimeconfig.ModelOption{
			{
				ID:   "gpt-stable",
				Name: "GPT Stable",
				ReasoningOptions: []runtimeconfig.ReasoningOption{
					{ID: "high", Name: "High"},
					{ID: "medium", Name: "Medium"},
				},
			},
		},
		ReasoningOptions: []runtimeconfig.ReasoningOption{
			{ID: "high", Name: "High"},
			{ID: "medium", Name: "Medium"},
		},
	}

	option := runtimeOption(models.RuntimeEnvironment{
		RuntimeID:        runtimeconfig.RuntimeIDCodex,
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
		state runtimeconfig.DetectionState
		// installed 是设置页应展示的安装状态。
		installed bool
	}{
		{name: "已安装但协议不可用", path: "/usr/local/bin/kimi", state: runtimeconfig.DetectionStateUnavailable, installed: true},
		{name: "未安装", state: runtimeconfig.DetectionStateUnavailable, installed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			option := runtimeOption(models.RuntimeEnvironment{Detection: runtimeconfig.DetectionResult{
				State: test.state, ResolvedBinary: test.path,
			}})
			if option.Installed != test.installed || option.State != string(test.state) {
				t.Fatalf("RuntimeOption = %+v", option)
			}
		})
	}
}

func TestRuntimeOptionIncludesCustomModels(t *testing.T) {
	detection := runtimeconfig.DetectionResult{
		DetectedDefaultModel: "gpt-stable",
		Models:               []runtimeconfig.ModelOption{{ID: "gpt-stable", Name: "GPT Stable"}},
		ReasoningOptions: []runtimeconfig.ReasoningOption{
			{ID: "high", Name: "High"},
		},
	}
	option := runtimeOption(models.RuntimeEnvironment{
		RuntimeID:    runtimeconfig.RuntimeIDCodex,
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
