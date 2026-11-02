package execution

import (
	"context"
	"encoding/json"
	"time"

	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
)

func TestValidateElicitationContentUsesFrozenSchema(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"mode":{"type":"string","enum":["safe","fast"]},
			"retries":{"type":"integer"}
		},
		"required":["mode"],
		"additionalProperties":false
	}`)
	requested := eventlog.ElicitationRequestedData{Mode: "form", RequestedSchema: schema}
	for name, content := range map[string]map[string]any{
		"missing required": {},
		"wrong type":       {"mode": "safe", "retries": "2"},
		"outside enum":     {"mode": "unsafe"},
		"extra field":      {"mode": "safe", "other": true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateElicitationContent(requested, content); !errors.Is(err, status_error.BadRequest) {
				t.Fatalf("validation error = %v, want BadRequest", err)
			}
		})
	}
	if err := validateElicitationContent(requested, map[string]any{"mode": "fast", "retries": float64(2)}); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}
}

func TestNativeQuestionsSharePendingFactsAndResolveExactlyOnce(t *testing.T) {
	for _, action := range []string{"answer", "decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			store, err := eventlog.NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			chatID := "51515151515151515151515151515151"
			if _, err := store.Create(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated}}); err != nil {
				t.Fatal(err)
			}
			journal := &consentTestJournal{runtimeEventTestJournal: &runtimeEventTestJournal{store: store}, updates: make(chan []models.PendingElicitation, 4)}
			coordinator := newElicitationCoordinator(journal)
			state := coordinator.inputState("execution")
			if state.Pending() {
				t.Fatal("undelivered request paused native inactivity")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan agentapi.ElicitationDecision, 1)
			go func() {
				decision, err := coordinator.handler(chatID, "execution").Request(ctx, agentapi.ElicitationRequest{InvocationID: "invocation", Mode: "form", Message: "选择交付方式", RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}, "required": []string{"answer"}}})
				if err != nil {
					t.Errorf("request failed: %v", err)
				}
				done <- decision
			}()
			pending := <-journal.updates
			if len(pending) != 1 || !state.Pending() {
				t.Fatalf("durable request missing: %+v", pending)
			}
			id := pending[0].ElicitationID
			wantResult := constants.ElicitationResultAccepted
			switch action {
			case "answer":
				err = coordinator.resolveAccepted(context.Background(), chatID, id, map[string]any{"answer": "file"})
			case "decline":
				wantResult = constants.ElicitationResultDeclined
				err = coordinator.resolveDeclined(context.Background(), chatID, id)
			case "cancel":
				wantResult = constants.ElicitationResultInvalidated
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case decision := <-done:
				if decision.Accepted != (action == "answer") || decision.Declined != (action == "decline") || decision.Cancelled != (action == "cancel") {
					t.Fatalf("wrong native decision: %+v", decision)
				}
			case <-time.After(time.Second):
				t.Fatal("native wait was not released")
			}
			if state.Pending() {
				t.Fatal("resolved request permanently paused native inactivity")
			}
			if err := coordinator.resolveAccepted(context.Background(), chatID, id, map[string]any{"answer": "late"}); !errors.Is(err, status_error.ElicitationAlreadyResolved) {
				t.Fatalf("late response accepted: %v", err)
			}
			records, err := store.ReadAll(chatID)
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 3 {
				t.Fatalf("resolution was not unique: %+v", records)
			}
			var resolved eventlog.ElicitationResolvedData
			if err := json.Unmarshal(records[2].Data, &resolved); err != nil || resolved.Result != wantResult {
				t.Fatalf("wrong durable resolution: %+v %v", resolved, err)
			}
		})
	}
}
