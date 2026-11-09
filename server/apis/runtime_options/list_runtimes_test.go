package runtime_options

import (
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"encoding/json"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/runtimeenvironment"
	"github.com/gin-gonic/gin"
)

func TestRuntimeOptionIncludesDetectedModelReasoningOptions(t *testing.T) {
	detection := agent.DetectionResult{
		Runtime: agent.RuntimeSummary{
			ID:            agent.RuntimeIDCodex,
			BaseRuntimeID: agent.RuntimeIDCodex,
			Name:          "Codex CLI",
		},
		DetectedDefaultModel:     "gpt-stable",
		DetectedDefaultReasoning: "high",
		Models: []agent.ModelOption{
			{
				ID:   "gpt-stable",
				Name: "GPT Stable",
				ReasoningOptions: []agent.ReasoningOption{
					{ID: "high", Name: "High"},
					{ID: "medium", Name: "Medium"},
				},
			},
		},
		ReasoningOptions: []agent.ReasoningOption{
			{ID: "high", Name: "High"},
			{ID: "medium", Name: "Medium"},
		},
	}

	option := runtimeOption(models.RuntimeEnvironment{
		RuntimeID:        agent.RuntimeIDCodex,
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
	if option.DefaultModel != "stale-model" || option.DefaultReasoning != "stale-reasoning" {
		t.Fatalf(
			"defaults = %q/%q, want saved stale-model/stale-reasoning",
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
		state agent.DetectionState
		// installed 是设置页应展示的安装状态。
		installed bool
	}{
		{name: "已安装但协议不可用", path: "/usr/local/bin/kimi", state: agent.DetectionStateUnavailable, installed: true},
		{name: "未安装", state: agent.DetectionStateUnavailable, installed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			option := runtimeOption(models.RuntimeEnvironment{Detection: agent.DetectionResult{
				State: test.state, ResolvedBinary: test.path,
			}})
			if option.Installed != test.installed || option.State != string(test.state) {
				t.Fatalf("RuntimeOption = %+v", option)
			}
		})
	}
}

func TestRuntimeOptionDoesNotExposeEditableLaunchConfiguration(t *testing.T) {
	option := runtimeOption(models.RuntimeEnvironment{
		RuntimeID: agent.RuntimeIDCodex,
		Detection: agent.DetectionResult{ResolvedBinary: "/managed/codex", Models: []agent.ModelOption{{ID: "native-model"}}},
	})
	encoded, err := json.Marshal(option)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binary", "prefixArgs", "configHome", "configHomeSupported", "customModels"} {
		if _, exists := fields[name]; exists {
			t.Fatalf("removed setting remains in API: %s", name)
		}
	}
	if option.ResolvedBinary != "/managed/codex" || len(option.Models) != 1 || option.Models[0].ID != "native-model" {
		t.Fatalf("readonly program/model facts lost: %+v", option)
	}
}

func TestNativeModelOptionProjectsCursorContextSeparatelyFromMaximum(t *testing.T) {
	row := agent.AgentChoice{ID: "gpt-5.6-sol", Config: &agent.AgentModelConfig{
		Options: []agent.AgentSessionOption{{
			ID: "context", Category: "model_config", Value: agent.AgentOptionValue{ID: "272k"},
			Choices: []agent.AgentChoice{{ID: "272k"}, {ID: "1m"}, {ID: "auto"}},
		}},
	}}
	got := nativeModelOption("cursor", row)
	if got.ContextWindowTokens != 272_000 || got.MaxContextWindowTokens != 1_000_000 {
		t.Fatalf("Cursor context windows = %+v", got)
	}
	if got.DefaultContextWindow != "272k" || len(got.ContextWindowOptions) != 2 ||
		got.ContextWindowOptions[1].ID != "1m" || got.ContextWindowOptions[1].Tokens != 1_000_000 {
		t.Fatalf("Cursor selectable context windows = %+v", got)
	}
	row.Config.UnknownOptionIDs = []string{"context"}
	got = nativeModelOption("cursor", row)
	if got.ContextWindowTokens != 0 || got.MaxContextWindowTokens != 0 {
		t.Fatalf("unknown context was exposed: %+v", got)
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
			if provider == "claude" {
				row.PermissionModes = []string{"default", "bypassPermissions"}
			}
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
	for _, provider := range []string{"qoder"} {
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
		if got.ID != "qfmodel" || got.DefaultReasoning != "xhigh" ||
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
		if got.ID != row.ID || len(got.PermissionLevels) != 1 {
			t.Fatalf("listed model or independent permission evidence lost: %+v", got)
		}
		if len(got.ReasoningOptions) != 0 || got.DefaultReasoning != "" || got.FastSupported || got.DefaultFast != nil {
			t.Fatalf("unknown settings %v leaked into choices: %+v", unknown, got)
		}
	}
}

func TestNativeModelOptionOffersOnlyMultipleNativeContextWindows(t *testing.T) {
	for _, provider := range []string{"qoder", "workbuddy", "opencode"} {
		row := agent.AgentChoice{Config: &agent.AgentModelConfig{Options: []agent.AgentSessionOption{{
			ID: "context_window", Value: agent.AgentOptionValue{ID: "200000"},
			Choices: []agent.AgentChoice{{ID: "200000"}},
		}}}}
		if got := nativeModelOption(provider, row); len(got.ContextWindowOptions) != 0 {
			t.Fatalf("%s: single window was offered: %+v", provider, got)
		}
		row.Config.Options[0].Choices = append(row.Config.Options[0].Choices, agent.AgentChoice{ID: "400000"})
		if got := nativeModelOption(provider, row); got.DefaultContextWindow != "200000" || len(got.ContextWindowOptions) != 2 {
			t.Fatalf("%s: native choices missing: %+v", provider, got)
		}
	}
}

// TestRuntimeOptionPreservesNativeDefaultSelection 验证空模型代表原生默认，不会被探测候选替换。
func TestRuntimeOptionPreservesNativeDefaultSelection(t *testing.T) {
	option := runtimeOption(models.RuntimeEnvironment{
		BaseRuntimeID: agent.RuntimeIDClaude,
		Detection:     agent.DetectionResult{DetectedDefaultModel: "sonnet", DetectedDefaultReasoning: "high", Models: []agent.ModelOption{{ID: "sonnet"}}},
	})
	if option.DefaultModel != "" || option.DefaultReasoning != "" {
		t.Fatalf("native default replaced by probe: %+v", option)
	}
}

// TestListRuntimesExcludesUnregisteredPersistedEnvironment 验证已移除智能体的历史条目不会出现在目录，注册的自定义配置仍然保留。
func TestListRuntimesExcludesUnregisteredPersistedEnvironment(t *testing.T) {
	runtimeUpdateFixture(t)
	catalog, err := agent.NewCatalog(agent.CatalogConfig{Profiles: []agent.ProfileSpec{{ID: "fixture-qoder-profile", BaseRuntimeID: "qoder", Name: "Fixture Qoder"}}})
	if err != nil {
		t.Fatal(err)
	}
	previousCatalog, previousService := global.RuntimeCatalog, global.RuntimeEnvironmentService
	t.Cleanup(func() { global.RuntimeCatalog, global.RuntimeEnvironmentService = previousCatalog, previousService })
	global.RuntimeCatalog = catalog
	service, err := runtimeenvironment.New(runtimeenvironment.Config{RuntimeCatalog: catalog, Environments: global.RuntimeEnvironmentControllerManager, Transactions: global.TransactionControllerManager})
	if err != nil {
		t.Fatal(err)
	}
	global.RuntimeEnvironmentService = service
	for _, id := range []string{"qoder", "fixture-qoder-profile", "retired-runtime"} {
		if err := global.RuntimeEnvironmentControllerManager.Ensure(t.Context(), models.RuntimeEnvironment{ID: id, RuntimeID: id, BaseRuntimeID: "qoder", Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, _ := gin.CreateTestContext(nil)
	output, err := (&ListRuntimes{}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resources := output.(ListRuntimesResponse).Resources
	if len(resources) != 2 {
		t.Fatalf("runtime list retained unregistered history: %+v", resources)
	}
	seen := map[string]bool{}
	for _, resource := range resources {
		seen[resource.ID] = true
	}
	if !seen["qoder"] || !seen["fixture-qoder-profile"] {
		t.Fatalf("registered runtimes lost: %+v", resources)
	}
}
