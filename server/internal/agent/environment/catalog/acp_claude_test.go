package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"

	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestClaudeACPDetectOptionsUsesAdapterConfigIDs 验证 Claude model/effort 目录来自 ACP。
func TestClaudeACPDetectOptionsUsesAdapterConfigIDs(t *testing.T) {
	t.Parallel()

	models := []acp.SessionConfigSelectOption{
		{Value: "sonnet", Name: "Sonnet"},
		{Value: "opus", Name: "Opus"},
	}
	sonnetOptions := testClaudeACPConfigOptions(
		"sonnet",
		models,
		[]acp.SessionConfigSelectOption{{Value: "medium", Name: "Medium"}},
	)
	opusOptions := testClaudeACPConfigOptions(
		"opus",
		models,
		[]acp.SessionConfigSelectOption{{Value: "high", Name: "High"}},
	)
	fakeAgent := &fakeCodexACPAgent{
		sessionID:       "claude-option-session",
		selectedConfigs: make(map[acp.SessionConfigId]acp.SessionConfigValueId),
		configOptions:   sonnetOptions,
		configOptionsByModel: map[acp.SessionConfigValueId][]acp.SessionConfigOption{
			"opus": opusOptions,
		},
	}
	backend := newACPBackend()
	replaceACPAgentFactory(backend, runtimeconfig.RuntimeIDClaude, func(
		context.Context,
		acpAdapterConfig,
	) (acp.Agent, error) {
		return fakeAgent, nil
	})
	t.Cleanup(func() { _ = backend.Close(context.Background()) })

	catalog, err := backend.DetectOptions(context.Background(), acpOptionsRequest{
		RuntimeID: runtimeconfig.RuntimeIDClaude,
		Prepared: preparedRuntime{
			definition:  runtimeDefinition{id: runtimeconfig.RuntimeIDClaude, baseRuntimeID: runtimeconfig.RuntimeIDClaude},
			detection:   runtimeconfig.DetectionResult{ResolvedBinary: "/fake/claude"},
			environment: environmentSnapshot{fingerprint: "claude-options"},
		},
		WorkingDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("DetectOptions() error = %v", err)
	}
	if len(catalog.Models) != 2 || catalog.Models[0].ID != "sonnet" ||
		len(catalog.Models[0].ReasoningOptions) != 1 ||
		catalog.Models[0].ReasoningOptions[0].ID != "medium" ||
		len(catalog.Models[1].ReasoningOptions) != 1 ||
		catalog.Models[1].ReasoningOptions[0].ID != "high" {
		t.Fatalf("catalog = %#v", catalog)
	}
}

// testClaudeACPConfigOptions 创建 model 与当前模型 effort 配置。
func testClaudeACPConfigOptions(
	currentModel string,
	models []acp.SessionConfigSelectOption,
	reasoning []acp.SessionConfigSelectOption,
) []acp.SessionConfigOption {
	modelValues := acp.SessionConfigSelectOptionsUngrouped(models)
	reasoningValues := acp.SessionConfigSelectOptionsUngrouped(reasoning)
	return []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{
			Id: acp.SessionConfigId("model"), Name: "Model",
			CurrentValue: acp.SessionConfigValueId(currentModel),
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &modelValues},
			Type:         "select",
		}},
		{Select: &acp.SessionConfigOptionSelect{
			Id: acp.SessionConfigId("effort"), Name: "Effort",
			CurrentValue: reasoningValues[0].Value,
			Options:      acp.SessionConfigSelectOptions{Ungrouped: &reasoningValues},
			Type:         "select",
		}},
	}
}
