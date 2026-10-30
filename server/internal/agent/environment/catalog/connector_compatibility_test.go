package catalog

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"

	"testing"
)

// TestConnectorCompatibilityMatchesUpstreamTransports 锁住各 Runtime 的 upstream MCP 边界。
func TestConnectorCompatibilityMatchesUpstreamTransports(t *testing.T) {
	t.Parallel()
	tests := []struct {
		// name 是能力组合名称。
		name string
		// runtimeID 是被检查的基础 Runtime。
		runtimeID string
		// transport 是目标 MCP transport。
		transport runtimeconfig.ConnectorTransport
		// expected 表示该 transport 是否受支持。
		expected bool
	}{
		{name: "codex stdio", runtimeID: runtimeconfig.RuntimeIDCodex, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "codex http", runtimeID: runtimeconfig.RuntimeIDCodex, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "codex sse", runtimeID: runtimeconfig.RuntimeIDCodex, transport: runtimeconfig.ConnectorTransportSSE, expected: false},
		{name: "claude stdio", runtimeID: runtimeconfig.RuntimeIDClaude, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "claude http", runtimeID: runtimeconfig.RuntimeIDClaude, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "claude sse", runtimeID: runtimeconfig.RuntimeIDClaude, transport: runtimeconfig.ConnectorTransportSSE, expected: true},
		{name: "kimi stdio", runtimeID: runtimeconfig.RuntimeIDKimi, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "kimi http", runtimeID: runtimeconfig.RuntimeIDKimi, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "kimi sse", runtimeID: runtimeconfig.RuntimeIDKimi, transport: runtimeconfig.ConnectorTransportSSE, expected: false},
		{name: "cursor stdio", runtimeID: runtimeconfig.RuntimeIDCursor, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "cursor http", runtimeID: runtimeconfig.RuntimeIDCursor, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "cursor sse", runtimeID: runtimeconfig.RuntimeIDCursor, transport: runtimeconfig.ConnectorTransportSSE, expected: false},
		{name: "pi stdio", runtimeID: runtimeconfig.RuntimeIDPi, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "pi http", runtimeID: runtimeconfig.RuntimeIDPi, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "pi sse", runtimeID: runtimeconfig.RuntimeIDPi, transport: runtimeconfig.ConnectorTransportSSE, expected: true},
		{name: "opencode stdio", runtimeID: runtimeconfig.RuntimeIDOpenCode, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "opencode http", runtimeID: runtimeconfig.RuntimeIDOpenCode, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "opencode sse", runtimeID: runtimeconfig.RuntimeIDOpenCode, transport: runtimeconfig.ConnectorTransportSSE, expected: true},
		{name: "gemini stdio", runtimeID: runtimeconfig.RuntimeIDGemini, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "gemini http", runtimeID: runtimeconfig.RuntimeIDGemini, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "gemini sse", runtimeID: runtimeconfig.RuntimeIDGemini, transport: runtimeconfig.ConnectorTransportSSE, expected: true},
		{name: "grok stdio", runtimeID: runtimeconfig.RuntimeIDGrok, transport: runtimeconfig.ConnectorTransportStdio, expected: true},
		{name: "grok http", runtimeID: runtimeconfig.RuntimeIDGrok, transport: runtimeconfig.ConnectorTransportStreamableHTTP, expected: true},
		{name: "grok sse", runtimeID: runtimeconfig.RuntimeIDGrok, transport: runtimeconfig.ConnectorTransportSSE, expected: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, reason := ConnectorCompatibility(test.runtimeID, test.transport)
			if actual != test.expected || (actual && reason != "") || (!actual && reason == "") {
				t.Fatalf("compatibility = %v/%q, want %v", actual, reason, test.expected)
			}
		})
	}
}

func TestDetectedConnectorTransportSupportedUsesCapabilitySnapshot(t *testing.T) {
	detection := runtimeconfig.DetectionResult{
		State: runtimeconfig.DetectionStateReady,
		Capabilities: runtimeconfig.CapabilitySet{
			runtimeconfig.CapabilityMCPStreamableHTTP: true,
		},
	}
	if !runtimecontract.DetectedConnectorTransportSupported(detection, runtimeconfig.ConnectorTransportStdio) ||
		!runtimecontract.DetectedConnectorTransportSupported(detection, runtimeconfig.ConnectorTransportStreamableHTTP) {
		t.Fatal("ready runtime lost declared MCP transports")
	}
	if runtimecontract.DetectedConnectorTransportSupported(detection, runtimeconfig.ConnectorTransportSSE) {
		t.Fatal("runtime gained undeclared SSE transport")
	}
}
