package runtimes

import (
	"context"
	acp "github.com/JieWaZi/acp-go/sdk"
	"testing"
)

func TestACPCatalogOptionsPreservesEmptyCatalog(t *testing.T) {
	models, reasoning := acpCatalogOptions(acpOptionCatalog{})
	if len(models) != 0 {
		t.Fatalf("models = %#v, want none", models)
	}
	if len(reasoning) != 0 {
		t.Fatalf("reasoning = %#v, want none", reasoning)
	}
}

// TestNativeACPModelThinkingCatalog 验证原生目录按模型传递思考选项，且探测不切换模型。
func TestNativeACPModelThinkingCatalog(t *testing.T) {
	thought := func(values ...string) []acp.SessionConfigOption {
		options := acp.SessionConfigSelectOptionsUngrouped{}
		for _, value := range values {
			options = append(options, acp.SessionConfigSelectOption{Value: acp.SessionConfigValueId(value), Name: value})
		}
		return []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "reasoning", CurrentValue: options[0].Value, Options: acp.SessionConfigSelectOptions{Ungrouped: &options}}}}
	}
	models := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "composer", Name: "Composer", Meta: map[string]any{"acp-go/model-config-options": []acp.SessionConfigOption{}}},
		{Value: "claude", Name: "Claude", Meta: map[string]any{"acp-go/model-config-options": thought("off", "medium", "high")}},
		{Value: "gpt", Name: "GPT", Meta: map[string]any{"acp-go/model-config-options": thought("low", "high")}},
	}
	agent := &embeddedACPAgent{binding: cursorACPRuntimeBinding()}
	catalog, err := agent.detectSessionOptions(context.Background(), "catalog", []acp.SessionConfigOption{{Select: &acp.SessionConfigOptionSelect{Id: "model", CurrentValue: "composer", Options: acp.SessionConfigSelectOptions{Ungrouped: &models}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultModel != "composer" {
		t.Fatalf("DefaultModel = %q, want composer", catalog.DefaultModel)
	}
	for index, want := range [][]string{nil, {"off", "medium", "high"}, {"low", "high"}} {
		got := catalog.Models[index].ReasoningOptions
		if len(got) != len(want) {
			t.Fatalf("%s thinking = %+v, want %v", catalog.Models[index].ID, got, want)
		}
		for n := range want {
			if got[n].ID != want[n] {
				t.Fatalf("thinking = %+v", got)
			}
		}
	}
}
