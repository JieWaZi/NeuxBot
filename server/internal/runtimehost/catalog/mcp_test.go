package catalog

import (
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/runtimehost/contract"

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
		transport runtimecontract.ConnectorTransport
		// expected 表示该 transport 是否受支持。
		expected bool
	}{
		{name: "codex stdio", runtimeID: runtimecontract.RuntimeIDCodex, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "codex http", runtimeID: runtimecontract.RuntimeIDCodex, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "codex sse", runtimeID: runtimecontract.RuntimeIDCodex, transport: runtimecontract.ConnectorTransportSSE, expected: false},
		{name: "claude stdio", runtimeID: runtimecontract.RuntimeIDClaude, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "claude http", runtimeID: runtimecontract.RuntimeIDClaude, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "claude sse", runtimeID: runtimecontract.RuntimeIDClaude, transport: runtimecontract.ConnectorTransportSSE, expected: true},
		{name: "kimi stdio", runtimeID: runtimecontract.RuntimeIDKimi, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "kimi http", runtimeID: runtimecontract.RuntimeIDKimi, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "kimi sse", runtimeID: runtimecontract.RuntimeIDKimi, transport: runtimecontract.ConnectorTransportSSE, expected: false},
		{name: "cursor stdio", runtimeID: runtimecontract.RuntimeIDCursor, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "cursor http", runtimeID: runtimecontract.RuntimeIDCursor, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "cursor sse", runtimeID: runtimecontract.RuntimeIDCursor, transport: runtimecontract.ConnectorTransportSSE, expected: false},
		{name: "pi stdio", runtimeID: runtimecontract.RuntimeIDPi, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "pi http", runtimeID: runtimecontract.RuntimeIDPi, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "pi sse", runtimeID: runtimecontract.RuntimeIDPi, transport: runtimecontract.ConnectorTransportSSE, expected: true},
		{name: "opencode stdio", runtimeID: runtimecontract.RuntimeIDOpenCode, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "opencode http", runtimeID: runtimecontract.RuntimeIDOpenCode, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "opencode sse", runtimeID: runtimecontract.RuntimeIDOpenCode, transport: runtimecontract.ConnectorTransportSSE, expected: true},
		{name: "gemini stdio", runtimeID: runtimecontract.RuntimeIDGemini, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "gemini http", runtimeID: runtimecontract.RuntimeIDGemini, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "gemini sse", runtimeID: runtimecontract.RuntimeIDGemini, transport: runtimecontract.ConnectorTransportSSE, expected: true},
		{name: "grok stdio", runtimeID: runtimecontract.RuntimeIDGrok, transport: runtimecontract.ConnectorTransportStdio, expected: true},
		{name: "grok http", runtimeID: runtimecontract.RuntimeIDGrok, transport: runtimecontract.ConnectorTransportStreamableHTTP, expected: true},
		{name: "grok sse", runtimeID: runtimecontract.RuntimeIDGrok, transport: runtimecontract.ConnectorTransportSSE, expected: true},
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
	detection := runtimecontract.DetectionResult{
		State: runtimecontract.DetectionStateReady,
		Capabilities: runtimecontract.CapabilitySet{
			runtimecontract.CapabilityMCPStreamableHTTP: true,
		},
	}
	if !runtimecontract.DetectedConnectorTransportSupported(detection, runtimecontract.ConnectorTransportStdio) ||
		!runtimecontract.DetectedConnectorTransportSupported(detection, runtimecontract.ConnectorTransportStreamableHTTP) {
		t.Fatal("ready runtime lost declared MCP transports")
	}
	if runtimecontract.DetectedConnectorTransportSupported(detection, runtimecontract.ConnectorTransportSSE) {
		t.Fatal("runtime gained undeclared SSE transport")
	}
}

func TestPrepareACPMCPConnectorsRejectsDuplicateRuntimeKey(t *testing.T) {
	_, err := prepareACPMCPConnectors(acp.McpCapabilities{Http: true, Sse: true}, []runtimecontract.Connector{
		{ID: "first", Name: "第一个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: runtimecontract.ConnectorTransportStdio, Command: "first"},
		{ID: "second", Name: "第二个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: runtimecontract.ConnectorTransportStdio, Command: "second"},
	})
	if err == nil || !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("duplicate runtime key error = %v", err)
	}
}
