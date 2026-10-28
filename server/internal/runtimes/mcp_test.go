package runtimes

import (
	"errors"

	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	acp "github.com/JieWaZi/acp-go/sdk"
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
		transport ConnectorTransport
		// expected 表示该 transport 是否受支持。
		expected bool
	}{
		{name: "codex stdio", runtimeID: RuntimeIDCodex, transport: ConnectorTransportStdio, expected: true},
		{name: "codex http", runtimeID: RuntimeIDCodex, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "codex sse", runtimeID: RuntimeIDCodex, transport: ConnectorTransportSSE, expected: false},
		{name: "claude stdio", runtimeID: RuntimeIDClaude, transport: ConnectorTransportStdio, expected: true},
		{name: "claude http", runtimeID: RuntimeIDClaude, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "claude sse", runtimeID: RuntimeIDClaude, transport: ConnectorTransportSSE, expected: true},
		{name: "kimi stdio", runtimeID: RuntimeIDKimi, transport: ConnectorTransportStdio, expected: true},
		{name: "kimi http", runtimeID: RuntimeIDKimi, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "kimi sse", runtimeID: RuntimeIDKimi, transport: ConnectorTransportSSE, expected: false},
		{name: "cursor stdio", runtimeID: RuntimeIDCursor, transport: ConnectorTransportStdio, expected: true},
		{name: "cursor http", runtimeID: RuntimeIDCursor, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "cursor sse", runtimeID: RuntimeIDCursor, transport: ConnectorTransportSSE, expected: false},
		{name: "pi stdio", runtimeID: RuntimeIDPi, transport: ConnectorTransportStdio, expected: true},
		{name: "pi http", runtimeID: RuntimeIDPi, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "pi sse", runtimeID: RuntimeIDPi, transport: ConnectorTransportSSE, expected: true},
		{name: "opencode stdio", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportStdio, expected: true},
		{name: "opencode http", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "opencode sse", runtimeID: RuntimeIDOpenCode, transport: ConnectorTransportSSE, expected: true},
		{name: "gemini stdio", runtimeID: RuntimeIDGemini, transport: ConnectorTransportStdio, expected: true},
		{name: "gemini http", runtimeID: RuntimeIDGemini, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "gemini sse", runtimeID: RuntimeIDGemini, transport: ConnectorTransportSSE, expected: true},
		{name: "grok stdio", runtimeID: RuntimeIDGrok, transport: ConnectorTransportStdio, expected: true},
		{name: "grok http", runtimeID: RuntimeIDGrok, transport: ConnectorTransportStreamableHTTP, expected: true},
		{name: "grok sse", runtimeID: RuntimeIDGrok, transport: ConnectorTransportSSE, expected: true},
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
	detection := DetectionResult{
		State: DetectionStateReady,
		Capabilities: CapabilitySet{
			CapabilityMCPStreamableHTTP: true,
		},
	}
	if !DetectedConnectorTransportSupported(detection, ConnectorTransportStdio) ||
		!DetectedConnectorTransportSupported(detection, ConnectorTransportStreamableHTTP) {
		t.Fatal("ready runtime lost declared MCP transports")
	}
	if DetectedConnectorTransportSupported(detection, ConnectorTransportSSE) {
		t.Fatal("runtime gained undeclared SSE transport")
	}
}

func TestPrepareACPMCPConnectorsRejectsDuplicateRuntimeKey(t *testing.T) {
	_, err := prepareACPMCPConnectors(acp.McpCapabilities{Http: true, Sse: true}, []Connector{
		{ID: "first", Name: "第一个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: ConnectorTransportStdio, Command: "first"},
		{ID: "second", Name: "第二个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: ConnectorTransportStdio, Command: "second"},
	})
	if err == nil || !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("duplicate runtime key error = %v", err)
	}
}
