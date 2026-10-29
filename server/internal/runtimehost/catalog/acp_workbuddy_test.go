package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

	"context"
	"reflect"

	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestWorkBuddyFreeModelCatalog 验证免费标记只来自当前零积分声明，不影响模型选择或其他智能体。
func TestWorkBuddyFreeModelCatalog(t *testing.T) {
	tests := []struct {
		// option 是原生 ACP 返回的模型选项。
		option acp.SessionConfigSelectOption
		// want 是 WorkBuddy 对外提供的模型展示名称。
		want string
	}{
		{acp.SessionConfigSelectOption{Value: "hy3", Name: "Hy3", Description: acp.Ptr("x0.00 credits")}, "Hy3 (free)"},
		{acp.SessionConfigSelectOption{Value: "hy4-preview", Name: "Hy4 preview", Description: acp.Ptr("x0.29 credits")}, "Hy4 preview"},
		{acp.SessionConfigSelectOption{Value: "unknown", Name: "Unknown"}, "Unknown"},
		{acp.SessionConfigSelectOption{Value: "night-only", Name: "Night only", Description: acp.Ptr("x0.00 credits at night")}, "Night only"},
		{acp.SessionConfigSelectOption{Value: "unsupported", Name: "Unsupported", Description: acp.Ptr("0.00")}, "Unsupported"},
		{acp.SessionConfigSelectOption{Value: "already-free", Name: "Already (free)", Description: acp.Ptr("x0.00 credits")}, "Already (free)"},
		{acp.SessionConfigSelectOption{Value: "unnamed", Description: acp.Ptr("x0.00 credits")}, "unnamed (free)"},
	}
	models := make(acp.SessionConfigSelectOptionsUngrouped, 0, len(tests))
	for _, test := range tests {
		models = append(models, test.option)
	}
	options := []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{
		Id: "model", CurrentValue: "hy3", Options: acp.SessionConfigSelectOptions{Ungrouped: &models},
	}}}
	for _, binding := range []acpRuntimeBinding{workBuddyACPRuntimeBinding(), cursorACPRuntimeBinding()} {
		t.Run(binding.baseRuntimeID, func(t *testing.T) {
			agent := &catalogDecoder{binding: binding}
			catalog, err := agent.detectSessionOptions(context.Background(), "free-catalog", options, nil)
			if err != nil {
				t.Fatal(err)
			}
			if catalog.DefaultModel != "hy3" || len(catalog.Models) != len(tests) {
				t.Fatalf("incorrect catalog: %+v", catalog)
			}
			for index, test := range tests {
				want := test.want
				if binding.baseRuntimeID != runtimecontract.RuntimeIDWorkBuddy {
					want = test.option.Name
					if want == "" {
						want = string(test.option.Value)
					}
				}
				if got := catalog.Models[index]; got.ID != string(test.option.Value) || got.Name != want {
					t.Fatalf("model %q = %+v, want name %q and original ID", test.option.Value, got, want)
				}
			}
		})
	}
	if models[0].Name != "Hy3" {
		t.Fatal("model name annotation mutated the upstream option")
	}
	models[0].Description = acp.Ptr("x0.29 credits")
	agent := &catalogDecoder{binding: workBuddyACPRuntimeBinding()}
	catalog, err := agent.detectSessionOptions(context.Background(), "refreshed-catalog", options, nil)
	if err != nil || catalog.Models[0].Name != "Hy3" {
		t.Fatalf("stale free label after catalog refresh: %+v, %v", catalog, err)
	}
}

// TestWorkBuddyConcreteModelCatalog 验证目录隐藏自动路由与思考开关，保留实际强度及模型专属目录。
func TestWorkBuddyConcreteModelCatalog(t *testing.T) {
	levels := acp.SessionConfigSelectOptionsUngrouped{}
	for _, id := range []string{"minimal", "low", "medium", "high", "xhigh", "max", "enabled", "disabled"} {
		levels = append(levels, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(id), Name: id})
	}
	glmLevels := acp.SessionConfigSelectOptionsUngrouped{{Value: "high", Name: "High"}, {Value: "enabled", Name: "On (default)"}}
	models := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "fast-model", Name: "快速"},
		{Value: "step-5-preview", Name: "Step-5-Preview", Description: acp.Ptr("x0.00 credits")},
		{Value: "balanced-model", Name: "均衡"},
		{Value: "glm-5.3", Name: "GLM-5.3", Meta: map[string]any{"acp-go/model-config-options": []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &glmLevels}}}}}},
		{Value: "deep-model", Name: "极致"},
		{Value: "unknown-model", Name: "Unknown"},
	}
	options := []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{Id: "model", CurrentValue: "step-5-preview", Options: acp.SessionConfigSelectOptions{Ungrouped: &models}}},
		{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &levels}}},
	}
	agent := &catalogDecoder{binding: workBuddyACPRuntimeBinding()}
	catalog, err := agent.detectSessionOptions(context.Background(), "concrete-catalog", options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultModel != "step-5-preview" || len(catalog.Models) != 3 {
		t.Fatalf("incorrect concrete catalog: %+v", catalog)
	}
	model, reasoning := runtimecontract.NormalizeDetectedDefaults(runtimecontract.DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "step-5-preview", "enabled")
	if model != "step-5-preview" || reasoning != "minimal" {
		t.Fatalf("hidden toggle remained selected: model=%q reasoning=%q", model, reasoning)
	}
	wantLevels := []runtimecontract.ReasoningOption{{ID: "minimal", Name: "minimal"}, {ID: "low", Name: "low"}, {ID: "medium", Name: "medium"}, {ID: "high", Name: "high"}, {ID: "xhigh", Name: "xhigh"}, {ID: "max", Name: "max"}}
	if catalog.Models[0].ID != "step-5-preview" || catalog.Models[0].Name != "Step-5-Preview (free)" || len(catalog.ReasoningOptions) != 0 || !reflect.DeepEqual(catalog.Models[0].ReasoningOptions, wantLevels) {
		t.Fatalf("concrete levels changed: %+v", catalog)
	}
	if catalog.Models[1].ID != "glm-5.3" || !reflect.DeepEqual(catalog.Models[1].ReasoningOptions, []runtimecontract.ReasoningOption{{ID: "high", Name: "High"}}) || catalog.Models[2].ID != "unknown-model" || catalog.Models[2].ReasoningOptions != nil {
		t.Fatalf("model-specific options lost or inferred: %+v", catalog.Models)
	}
	if len(levels) != 8 || len(models) != 6 || len(glmLevels) != 2 {
		t.Fatal("catalog filtering mutated native options")
	}
	other := &catalogDecoder{binding: cursorACPRuntimeBinding()}
	unchanged, err := other.detectSessionOptions(context.Background(), "other-catalog", options, nil)
	if err != nil || len(unchanged.Models) != 6 || len(unchanged.ReasoningOptions) != 8 || unchanged.DefaultReasoning != "enabled" {
		t.Fatalf("WorkBuddy product scope leaked to another runtime: %+v, %v", unchanged, err)
	}
}

// TestWorkBuddyCatalogDefaultReasoning 验证有效默认值保持不变，隐藏项不再成为默认或跨模型能力。
func TestWorkBuddyCatalogDefaultReasoning(t *testing.T) {
	levels := []runtimecontract.ReasoningOption{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}, {ID: "enabled", Name: "On (default)"}}
	for _, selected := range []string{"high", "enabled"} {
		catalog := workBuddyCatalogOptions(acpOptionCatalog{DefaultModel: "glm-5.3", DefaultReasoning: selected, Models: []runtimecontract.ModelOption{{ID: "glm-5.3", ReasoningOptions: levels}}})
		want := "high"
		if selected == "enabled" {
			want = "low"
		}
		_, reasoning := runtimecontract.NormalizeDetectedDefaults(runtimecontract.DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "glm-5.3", "enabled")
		if reasoning != want || len(catalog.Models[0].ReasoningOptions) != 2 {
			t.Fatalf("default %q: %+v", selected, catalog)
		}
	}
	catalog := workBuddyCatalogOptions(acpOptionCatalog{DefaultModel: "fast-model", DefaultReasoning: "high", ReasoningOptions: levels, Models: []runtimecontract.ModelOption{{ID: "fast-model", ReasoningOptions: levels}, {ID: "glm-5.3"}}})
	if catalog.DefaultModel != "" || catalog.DefaultReasoning != "" || catalog.ReasoningOptions != nil || catalog.Models[0].ReasoningOptions != nil {
		t.Fatalf("hidden model options leaked to the replacement model: %+v", catalog)
	}
	catalog = workBuddyCatalogOptions(acpOptionCatalog{DefaultModel: "hy3", DefaultReasoning: "enabled", Models: []runtimecontract.ModelOption{{ID: "hy3", ReasoningOptions: []runtimecontract.ReasoningOption{{ID: "enabled"}, {ID: "disabled"}}}}})
	_, reasoning := runtimecontract.NormalizeDetectedDefaults(runtimecontract.DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "hy3", "enabled")
	if reasoning != "" || len(catalog.Models[0].ReasoningOptions) != 0 || len(catalog.ReasoningOptions) != 0 {
		t.Fatalf("invented concrete thinking levels for a toggle-only model: %+v", catalog)
	}
}

// TestWorkBuddyDetectsConcreteModelThinking 验证默认模型被隐藏或仅有开关时，仍逐模型取得真实强度。
func TestWorkBuddyDetectsConcreteModelThinking(t *testing.T) {
	for _, test := range []struct {
		// name 标识默认模型或错误回执的回归场景。
		name string
		// current 是临时会话初始生效的模型。
		current string
		// confirm 控制模型 setter 是否确认本次目标模型。
		confirm bool
	}{{"hidden default", "fast-model", true}, {"toggle-only default", "hy3", true}, {"concrete default", "step-5-preview", true}, {"mismatched receipt", "fast-model", false}} {
		t.Run(test.name, func(t *testing.T) {
			current := test.current
			glmLevels := acp.SessionConfigSelectOptionsUngrouped{{Value: "high", Name: "High"}, {Value: "enabled", Name: "On (default)"}}
			models := acp.SessionConfigSelectOptionsUngrouped{
				{Value: "fast-model", Name: "快速"},
				{Value: "hy3", Name: "Hy3"},
				{Value: "balanced-model", Name: "均衡"},
				{Value: "step-5-preview", Name: "Step-5-Preview", Description: acp.Ptr("x0.00 credits")},
				{Value: "deep-model", Name: "极致"},
				{Value: "glm-5.3", Name: "GLM-5.3", Meta: map[string]any{"acp-go/model-config-options": []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &glmLevels}}}}}},
			}
			config := func(model string, levels ...string) []acp.SessionConfigOption {
				values := acp.SessionConfigSelectOptionsUngrouped{}
				for _, level := range levels {
					values = append(values, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(level), Name: level})
				}
				return []acp.SessionConfigOption{
					{Select: &acp.SessionConfigOptionSelect{Id: "model", CurrentValue: acp.SessionConfigValueId(model), Options: acp.SessionConfigSelectOptions{Ungrouped: &models}}},
					{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: "enabled", Options: acp.SessionConfigSelectOptions{Ungrouped: &values}}},
				}
			}
			configs := map[acp.SessionConfigValueId][]acp.SessionConfigOption{
				"fast-model":     config("fast-model", "medium", "enabled"),
				"hy3":            config("hy3", "enabled", "disabled"),
				"step-5-preview": config("step-5-preview", "minimal", "low", "medium", "high", "xhigh", "max", "enabled"),
			}
			if !test.confirm {
				configs["step-5-preview"] = configs["hy3"]
			}
			fixture := &fakeCodexACPAgent{sessionID: "thinking-catalog", selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId), configOptions: configs[acp.SessionConfigValueId(current)], configOptionsByModel: configs}
			backend := newACPBackend()
			t.Cleanup(func() { _ = backend.Close(context.Background()) })
			replaceACPAgentFactory(backend, runtimecontract.RuntimeIDWorkBuddy, func(context.Context, acpAdapterConfig) (acp.Agent, error) { return fixture, nil })
			catalog, err := backend.DetectOptions(context.Background(), acpOptionsRequest{
				RuntimeID: runtimecontract.RuntimeIDWorkBuddy,
				Prepared: preparedRuntime{
					definition:  runtimeDefinition{id: runtimecontract.RuntimeIDWorkBuddy, baseRuntimeID: runtimecontract.RuntimeIDWorkBuddy},
					detection:   runtimecontract.DetectionResult{ResolvedBinary: "/fake/codebuddy"},
					environment: environmentSnapshot{fingerprint: "workbuddy-thinking"},
				},
				WorkingDirectory: t.TempDir(),
			})
			if !test.confirm {
				if err == nil || fixture.promptCalls != 0 || fixture.closeSessionCalls != 1 {
					t.Fatalf("unconfirmed model accepted or detection session leaked: err=%v prompts=%d close=%d", err, fixture.promptCalls, fixture.closeSessionCalls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.Models) != 3 || catalog.Models[0].ID != "hy3" || len(catalog.Models[0].ReasoningOptions) != 0 || catalog.Models[1].ID != "step-5-preview" || catalog.Models[1].Name != "Step-5-Preview (free)" || len(catalog.Models[1].ReasoningOptions) != 6 || catalog.Models[1].ReasoningOptions[5].ID != "max" || !reflect.DeepEqual(catalog.Models[2].ReasoningOptions, []runtimecontract.ReasoningOption{{ID: "high", Name: "High"}}) {
				t.Fatalf("concrete model thinking lost or borrowed: %+v", catalog)
			}
			if len(catalog.ReasoningOptions) != 0 {
				t.Fatalf("global thinking would leak to toggle-only models: %+v", catalog.ReasoningOptions)
			}
			_, reasoning := runtimecontract.NormalizeDetectedDefaults(runtimecontract.DetectionResult{Models: catalog.Models, ReasoningOptions: catalog.ReasoningOptions, DetectedDefaultModel: catalog.DefaultModel, DetectedDefaultReasoning: catalog.DefaultReasoning}, nil, "step-5-preview", "enabled")
			if reasoning != "minimal" {
				t.Fatalf("filtered default strength = %q, want minimal", reasoning)
			}
			wantSwitches := 1
			if current == "fast-model" {
				wantSwitches = 2
			}
			if fixture.configCalls != wantSwitches || fixture.promptCalls != 0 || fixture.newCalls != 1 || fixture.closeSessionCalls != 1 {
				t.Fatalf("temporary detection lifecycle: switches=%d prompts=%d new=%d close=%d", fixture.configCalls, fixture.promptCalls, fixture.newCalls, fixture.closeSessionCalls)
			}
		})
	}
}

// workBuddyFixtureAgent 给共用测试 Agent 补充 WorkBuddy 实际声明的模式目录。
type workBuddyFixtureAgent struct {
	// fakeCodexACPAgent 复用标准 ACP 的请求记录与响应，不引入私有协议。
	*fakeCodexACPAgent
}

// NewSession 保留标准测试行为并返回原生权限菜单。
func (agent *workBuddyFixtureAgent) NewSession(ctx context.Context, request acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	response, err := agent.fakeCodexACPAgent.NewSession(ctx, request)
	response.Modes = &acp.SessionModeState{AvailableModes: []acp.SessionMode{{Id: "default", Name: "Default"}, {Id: "auto", Name: "Auto"}, {Id: "bypassPermissions", Name: "Bypass"}}}
	return response, err
}

// TestWorkBuddyRegistryAndPermissionCatalog 只验证仍被探测及安装使用的声明。
func TestWorkBuddyRegistryAndPermissionCatalog(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions[runtimecontract.RuntimeIDWorkBuddy]
	if definition.name != "WorkBuddy" || definition.instructionFileName != "CODEBUDDY.md" || definition.skillDirectoryName != ".codebuddy" || definition.configHomeEnvironmentVariable != "CODEBUDDY_CONFIG_DIR" || !reflect.DeepEqual(definition.binaryCandidates, []string{"codebuddy", "cbc"}) || len(definition.models) != 0 {
		t.Fatalf("native definition: %+v", definition)
	}
	binding := defaultACPRuntimeBindings()[runtimecontract.RuntimeIDWorkBuddy]
	if binding.newAgent == nil || !binding.nativeProcess {
		t.Fatal("missing native factory")
	}
	modes := []acp.SessionMode{{Id: "default"}, {Id: "auto"}, {Id: "bypassPermissions"}, {Id: "plan"}}
	if got := binding.permissionLevels(nil, modes); !reflect.DeepEqual(got, []runtimecontract.PermissionLevel{runtimecontract.PermissionLevelDefault, runtimecontract.PermissionLevelAuto, runtimecontract.PermissionLevelFullAccess}) {
		t.Fatalf("native permission catalog: %v", got)
	}
	for _, transport := range []runtimecontract.ConnectorTransport{runtimecontract.ConnectorTransportStdio, runtimecontract.ConnectorTransportStreamableHTTP, runtimecontract.ConnectorTransportSSE} {
		if ok, _ := ConnectorCompatibility(runtimecontract.RuntimeIDWorkBuddy, transport); !ok {
			t.Fatalf("missing transport: %s", transport)
		}
	}
	for _, flag := range []string{"--acp-transport=http", "--permission-mode=fullAccess", "--resume=old", "--model=other", "--mcp-config=other", "--system-prompt=other", "--multitask"} {
		if LaunchOverrideArgumentsAllowed(runtimecontract.RuntimeIDWorkBuddy, []string{flag}) {
			t.Fatalf("managed argument override: %s", flag)
		}
	}
}
