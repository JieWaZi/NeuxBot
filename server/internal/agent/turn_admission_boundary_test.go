package agent_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
)

func TestQueuedNativeGateKeepsCommittedFactWhenBoundaryFails(t *testing.T) {
	lifecycle, store, request, trace := newTurnFixture(t, "cursor")
	input := chatTurn(1, "do not send after boundary error")
	ports := chatPorts(request.ChatID, input, true)
	ports.CommitAdmission = func(ctx context.Context, commit func(context.Context) error) error {
		if err := commit(ctx); err != nil {
			return err
		}
		return status_error.DatabaseError
	}
	var committed agent.CommitReceipt
	ports.Committed = func(receipt agent.CommitReceipt) { committed = receipt }
	ports.Authorize = func(context.Context, agent.TurnAdmission, agent.CommitReceipt) (func(), error) {
		t.Fatal("boundary failure enabled the prompt")
		return nil, nil
	}
	turn, err := lifecycle.Prepare(t.Context(), request, input, ports)
	if err != nil {
		t.Fatal(err)
	}
	result := turn.Run(t.Context())
	if !errors.Is(result.OperationError, status_error.DatabaseError) || result.Submission != "not_sent" || !result.BusinessAdmitted || committed.Version == 0 {
		t.Fatalf("committed gated input = %+v, receipt = %+v", result, committed)
	}
	if _, err := store.ReadVersion(t.Context(), request.ChatID); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(trace)
	if err != nil || strings.Contains(string(wire), "session/prompt") {
		t.Fatalf("unexpected native prompt: %q %v", wire, err)
	}
}
