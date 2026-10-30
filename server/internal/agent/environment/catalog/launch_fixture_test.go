package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"testing"
)

// readyDetectionForRun 构造测试发送链路使用的已持久化可用快照。
func readyDetectionForRun(
	t *testing.T,
	sdk *Catalog,
	runtimeID string,
	resolvedBinary string,
) runtimeconfig.DetectionResult {
	t.Helper()
	definition, exists := sdk.definition(runtimeID)
	if !exists {
		t.Fatalf("runtime %q is not registered", runtimeID)
	}
	return runtimeconfig.DetectionResult{
		Runtime: definitionSummaries(map[string]runtimeDefinition{
			definition.id: definition,
		})[0],
		State:          runtimeconfig.DetectionStateReady,
		ResolvedBinary: resolvedBinary,
		Capabilities:   runtimeconfig.CapabilitySet{},
	}
}
