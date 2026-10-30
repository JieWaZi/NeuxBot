package runtimehost

import (
	runtimediagnostic "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/diagnostic"

	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	c "github.com/JieWaZi/NeuxBot/server/internal/agent/core/contract"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	acp "github.com/JieWaZi/acp-go/sdk"
)

func TestChatToolContentKeepsPublicPayloadAndEmptyReplacement(t *testing.T) {
	sink := &chatSinkFixture{}
	observer := &chatObserver{sink: sink}
	blocks := []c.ContentBlock{
		{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/png", URI: "file:///image.png"},
		{Type: "audio", Data: "YXVkaW8=", MIMEType: "audio/wav"},
		{Type: "resource", Data: "YmxvYg==", MIMEType: "application/octet-stream", URI: "resource://blob"},
		{Type: "resource", Text: "resource text", MIMEType: "text/plain", URI: "resource://text"},
		{Type: "resource_link", Name: "report", MIMEType: "text/plain", URI: "resource://report"},
	}
	if err := observer.Accept(context.Background(), c.Event{DriverEvent: c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Content: blocks}}}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(sink.events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	content := payload["content"].([]any)
	decoded := make([]map[string]any, len(content))
	for i, value := range content {
		item := value.(map[string]any)
		if item["type"] != "content" {
			t.Fatalf("nonstandard content wrapper: %+v", item)
		}
		decoded[i] = item["content"].(map[string]any)
	}
	if decoded[0]["data"] != blocks[0].Data || decoded[0]["mimeType"] != "image/png" || decoded[1]["data"] != blocks[1].Data ||
		decoded[2]["resource"].(map[string]any)["blob"] != blocks[2].Data || decoded[3]["resource"].(map[string]any)["text"] != "resource text" ||
		decoded[4]["name"] != "report" || decoded[4]["mimeType"] != "text/plain" {
		t.Fatalf("lost public content: %+v", decoded)
	}
	for _, replacement := range [][]c.ContentBlock{nil, {}} {
		if err := observer.Accept(context.Background(), c.Event{DriverEvent: c.DriverEvent{Kind: c.EventTool, Tool: &c.ToolEvent{ID: "tool", Update: true, Content: replacement}}}); err != nil {
			t.Fatal(err)
		}
		payload = nil
		_ = json.Unmarshal(sink.events[len(sink.events)-1].Data, &payload)
		_, exists := payload["content"]
		if exists != (replacement != nil) {
			t.Fatalf("lost omission/replacement distinction: %+v", payload)
		}
	}
}

func TestChatCursorUpgradeClassification(t *testing.T) {
	err := acp.NewInternalError(map[string]any{"cursorErrorCode": "upgrade"})
	result := chatResult(runtimecontract.SessionPlan{Detection: runtimeconfig.DetectionResult{Runtime: runtimeconfig.RuntimeSummary{BaseRuntimeID: runtimeconfig.RuntimeIDCursor}}}, runtimecontract.SessionDirective{}, c.Result{Status: c.StatusFailed, Fault: &c.Fault{Code: c.FaultDriver, Cause: err}}, &chatObserver{})
	if result.Failure == nil || result.Failure.Code != runtimediagnostic.FailureCodePlanUpgradeRequired {
		t.Fatalf("lost native upgrade classification: %+v", result)
	}
}

// usageFailureSink 只拒绝最终用量，验证交付失败保留已经执行的事实。
type usageFailureSink struct{}

func (usageFailureSink) Accept(_ context.Context, event runtimecontract.Event) error {
	if event.Type == runtimecontract.EventTypeUsage {
		return status_error.ChatLogError
	}
	return nil
}

func TestChatFinalUsageDeliveryFailureDoesNotReplayPrompt(t *testing.T) {
	driver := &chatDriverFixture{result: &c.DriverResult{Status: c.StatusCompleted, StopReason: c.StopEndTurn, Usage: c.Usage{Known: true, InputTokens: 1}}}
	host, request := chatFixture(t, driver)
	result := runChatFixture(context.Background(), host, request, usageFailureSink{})
	if result.Failure == nil || result.Failure.Code != runtimediagnostic.FailureCodeEventDelivery || !result.ProtocolHealthy || len(driver.turns) != 1 {
		t.Fatalf("delivery failure lost execution or replayed: %+v turns=%d", result, len(driver.turns))
	}
}

func TestChatNativeCancellationReachesProductResult(t *testing.T) {
	driver := &chatDriverFixture{result: &c.DriverResult{Status: c.StatusCancelled, StopReason: c.StopCancelled}}
	host, request := chatFixture(t, driver)
	result := runChatFixture(context.Background(), host, request, &chatSinkFixture{})
	if result.Failure == nil || result.Failure.Code != runtimediagnostic.FailureCodeContextCanceled || result.ExitCode == 0 {
		t.Fatalf("native cancelled became successful: %+v", result)
	}
}
