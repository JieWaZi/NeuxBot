package agent

import "testing"

func TestRecordedFailureDisplayMessagePreservesUnstructuredText(t *testing.T) {
	if actual := RecordedFailureDisplayMessage("API Error: 402 Insufficient Balance"); actual != "API Error: 402 Insufficient Balance" {
		t.Fatalf("plain message = %q", actual)
	}
	if actual := RecordedFailureDisplayMessage(`{"code":-32603,"message":"Internal error","data":{"error":"令牌已失效"}}`); actual != "令牌已失效" {
		t.Fatalf("recorded ACP detail = %q", actual)
	}
}
