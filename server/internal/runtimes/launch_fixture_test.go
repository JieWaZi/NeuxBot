package runtimes

import (
	"testing"
)

// readyDetectionForRun 构造测试发送链路使用的已持久化可用快照。
func readyDetectionForRun(
	t *testing.T,
	sdk *SDK,
	runtimeID string,
	resolvedBinary string,
) DetectionResult {
	t.Helper()
	definition, exists := sdk.definition(runtimeID)
	if !exists {
		t.Fatalf("runtime %q is not registered", runtimeID)
	}
	return DetectionResult{
		Runtime: definitionSummaries(map[string]runtimeDefinition{
			definition.id: definition,
		})[0],
		State:          DetectionStateReady,
		ResolvedBinary: resolvedBinary,
		Capabilities:   CapabilitySet{},
	}
}
