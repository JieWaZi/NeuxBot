package execution

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestCLIQuestionRejectsFormsTheUserCannotComplete(t *testing.T) {
	for _, source := range []string{
		`{"type":"object","properties":{"answer":{"type":"string"}},"required":["missing"]}`,
		`{"type":"object","properties":{"answer":{"type":"array","items":{"type":"string"}}}}`,
		`{"type":"object","properties":{"answer__other":{"type":"string"}}}`,
		`{"type":"object","properties":{"answer":{"type":"string","oneOf":[{"const":"same"},{"const":"same"}]}}}`,
		`{"type":"object","properties":{"answer":{"type":"string","enum":["A"],"minLength":2}},"required":["answer"]}`,
		`{"type":"object","properties":{"answer":{"type":"string","oneOf":[{"const":"too long"}],"maxLength":3}},"required":["answer"]}`,
	} {
		var schema jsonschema.Schema
		if err := json.Unmarshal([]byte(source), &schema); err != nil {
			t.Fatal(err)
		}
		if err := validateQuestionForm(&schema); !errors.Is(err, status_error.AgentInputInvalid) {
			t.Fatalf("unusable form accepted: %s %v", source, err)
		}
	}
}

func TestCLIQuestionsUseCanonicalFactsAndActualUserDecision(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "0123456789abcdef0123456789abcdef"
	if _, err := store.Create(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated}, {Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "execution"}, {Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "execution"}}); err != nil {
		t.Fatal(err)
	}
	journal := &consentTestJournal{runtimeEventTestJournal: &runtimeEventTestJournal{store: store}, updates: make(chan []models.PendingElicitation, 16)}
	hub := events.New()
	defer hub.Close()
	coordinator := newElicitationCoordinator(journal, hub)
	manager := &service{elicitations: coordinator, events: hub}
	state := coordinator.inputState("execution")
	bindings := map[string]agentcapability.Binding{}
	for _, value := range manager.questionBindings(chatID, "execution") {
		bindings[value.Operation] = value
	}
	invalid := agentcapability.Request{Operation: "interaction.questions.create", RequestID: "impossible", Params: map[string]any{"message": "无法完成", "allowSkip": false, "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"choice": map[string]any{"type": "string", "enum": []string{"A"}, "minLength": 2}}, "required": []string{"choice"}}}}
	if _, err := bindings[invalid.Operation].Call(context.Background(), invalid); !errors.Is(err, status_error.AgentInputInvalid) || state.Pending() {
		t.Fatalf("impossible question became pending: %v", err)
	}
	if _, err := bindings[invalid.Operation].Receipt(context.Background(), invalid.RequestID); !errors.Is(err, status_error.AgentReceiptNotFound) {
		t.Fatalf("impossible question published a receipt: %v", err)
	}
	request := agentcapability.Request{Operation: "interaction.questions.create", RequestID: "choice", Params: map[string]any{"message": "选择交付方式", "allowSkip": false, "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"choice": map[string]any{"type": "string", "enum": []string{"file", "text"}}}, "required": []string{"choice"}, "additionalProperties": false}}}
	first, err := bindings[request.Operation].Call(context.Background(), request)
	if err != nil || !state.Pending() {
		t.Fatalf("question was not pending: %+v %v", first, err)
	}
	id := first.CommandID
	replay, err := bindings[request.Operation].Call(context.Background(), request)
	if err != nil || !replay.Replayed || replay.CommandID != id {
		t.Fatalf("question duplicated: %+v %v", replay, err)
	}
	changed := request
	changed.Params = map[string]any{"message": "changed", "requestedSchema": request.Params["requestedSchema"]}
	if _, err := bindings[request.Operation].Call(context.Background(), changed); !errors.Is(err, status_error.AgentCommandRequestConflict) {
		t.Fatalf("changed question accepted: %v", err)
	}
	if err := coordinator.resolveDeclined(context.Background(), chatID, id); err == nil {
		t.Fatal("required question could be skipped")
	}
	if err := coordinator.resolveAccepted(context.Background(), chatID, id, map[string]any{"choice": "unlisted"}); err == nil {
		t.Fatal("answer bypassed frozen schema")
	}
	if err := coordinator.resolveAccepted(context.Background(), chatID, id, map[string]any{"choice": "file"}); err != nil {
		t.Fatal(err)
	}
	if state.Pending() {
		t.Fatal("resolved question kept runtime idle timer paused")
	}
	result, err := bindings["interaction.questions.wait"].Call(context.Background(), agentcapability.Request{Params: map[string]any{"questionId": id, "timeoutMs": 100}})
	if err != nil || result.Data.(map[string]any)["completed"] != true {
		t.Fatalf("actual answer not observed: %+v %v", result, err)
	}
	other := manager.questionBindings(chatID, "another-execution")
	if _, err := other[0].Receipt(context.Background(), "choice"); err == nil {
		t.Fatal("question receipt leaked into another execution")
	}
	coordinator.finishQuestions(chatID, "execution")
}
