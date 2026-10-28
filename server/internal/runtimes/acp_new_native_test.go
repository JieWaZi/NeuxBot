package runtimes

import (
	"testing"

	acp "github.com/JieWaZi/acp-go/sdk"
)

// TestNewNativeRuntimeRegistry 验证运行时可发现且交付真实指令与技能目录，不伪造模型目录。
func TestNewNativeRuntimeRegistry(t *testing.T) {
	definitions, err := newRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opencode", "gemini", "grok"} {
		definition, ok := definitions[id]
		if !ok {
			t.Errorf("runtime missing: %s", id)
			continue
		}
		instruction := "AGENTS.md"
		if id == "gemini" {
			instruction = "GEMINI.md"
		}
		if definition.instructionFileName != instruction || definition.skillDirectoryName != "."+id || len(definition.models) != 0 {
			t.Errorf("invented native files/models: %+v", definition)
		}
		binding, ok := defaultACPRuntimeBindings()[id]
		if !ok || binding.newAgent == nil || !binding.nativeProcess {
			t.Errorf("unsafe lifecycle binding: %s", id)
		}
	}
}

// TestNewNativeVersionDoesNotUseBridgeBuild 防止版本探测超时后把适配器构建号当作 CLI 版本。
func TestNewNativeVersionDoesNotUseBridgeBuild(t *testing.T) {
	for _, id := range []string{"opencode", "gemini", "grok"} {
		catalog := acpInitializeCatalog(defaultACPRuntimeBindings()[id], acp.InitializeResponse{AgentInfo: &acp.Implementation{Name: id, Version: "bridge-build"}})
		if catalog.Version != "" {
			t.Errorf("%s exposed bridge version as CLI: %s", id, catalog.Version)
		}
		actual := acpInitializeCatalog(defaultACPRuntimeBindings()[id], acp.InitializeResponse{AgentInfo: &acp.Implementation{Name: id, Version: "bridge-build", Meta: map[string]any{"runtime": map[string]any{"version": "native-1.2.3"}}}})
		if actual.Version != "native-1.2.3" {
			t.Errorf("native version lost: %s", actual.Version)
		}
	}
}

// TestExistingNativeVersionFallback 保留原有原生绑定的握手版本兼容行为。
func TestExistingNativeVersionFallback(t *testing.T) {
	catalog := acpInitializeCatalog(kimiACPRuntimeBinding(), acp.InitializeResponse{AgentInfo: &acp.Implementation{Version: "legacy-cli"}})
	if catalog.Version != "legacy-cli" {
		t.Fatalf("legacy version lost: %s", catalog.Version)
	}
}
