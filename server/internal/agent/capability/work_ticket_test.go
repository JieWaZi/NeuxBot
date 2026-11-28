package capability

import "testing"

func TestWorkTicketReportIsPinnedAndAcceptsOnlyAnOutcomeAndContent(t *testing.T) {
	operation, exists := Find("work.tickets.report")
	if !exists {
		t.Fatal("ticket workers have no report operation")
	}
	if !operation.RequiresExecutionID() {
		t.Fatal("a work report could be sent without its current execution identity")
	}
	if err := operation.Validate(map[string]any{"kind": "completed", "content": "结果"}); err != nil {
		t.Fatalf("legitimate report rejected: %v", err)
	}
	for _, params := range []map[string]any{
		{"kind": "completed", "content": "结果", "ticketId": "another-ticket"},
		{"kind": "stopped", "content": "停止"},
		{"kind": "waiting", "content": " "},
	} {
		if err := operation.Validate(params); err == nil {
			t.Fatalf("report accepted caller-controlled scope or invalid outcome: %v", params)
		}
	}
}
