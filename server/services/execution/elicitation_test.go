package execution

import (
	"encoding/json"
	"errors"
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
