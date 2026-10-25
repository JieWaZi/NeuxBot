package input

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
)

func testSchema(t *testing.T) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","required":["text"],"properties":{"text":{"type":"string","default":"fallback"},"repeat":{"type":"integer","minimum":1,"default":2},"segments":{"type":"array","items":{"type":"string"}},"flag":{"type":"boolean"},"nullable":{"type":["string","null"]}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}
func TestIndependentSourcesAndTypedArrays(t *testing.T) {
	schema := testSchema(t)
	bindings := map[string]Binding{"text": {Source: constants.WorkflowInputSourceAPI}, "repeat": {Source: constants.WorkflowInputSourceFixed, Value: 3}, "segments": {Source: constants.WorkflowInputSourceAPI}}
	contract, err := Analyze(schema, bindings, true, nil)
	if err != nil || len(contract.Fields) != 5 || !contract.ExampleValid {
		t.Fatalf("contract %+v %v", contract, err)
	}
	args, err := Resolve(schema, bindings, map[string]any{"text": "real", "segments": []any{"a", "b"}})
	if err != nil || args["repeat"] != float64(3) || args["text"] != "real" {
		t.Fatalf("resolved %+v %v", args, err)
	}
	for _, input := range []map[string]any{{}, {"text": "real", "repeat": 3}, {"text": "real", "segments": "a,b"}, {"text": "real", "segments": []any{1}}, {"text": nil}} {
		if _, err := Resolve(schema, bindings, input); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	if _, err := Analyze(schema, bindings, false, nil); err == nil {
		t.Fatal("schedule accepted API input")
	}
}
func TestExplicitNullAndFalseDoNotDisappear(t *testing.T) {
	var bindings map[string]Binding
	if err := json.Unmarshal([]byte(`{"text":{"source":"fixed","value":""},"flag":{"source":"fixed","value":false},"nullable":{"source":"fixed","value":null},"segments":{"source":"fixed","value":[]},"zero":{"source":"fixed","value":0}}`), &bindings); err != nil {
		t.Fatal(err)
	}
	schema := testSchema(t)
	schema["properties"].(map[string]any)["zero"] = map[string]any{"type": "integer"}
	contract, err := Analyze(schema, bindings, false, nil)
	if err != nil || !contract.ExampleValid {
		t.Fatal(contract, err)
	}
	args, err := Resolve(schema, bindings, nil)
	if err != nil || args["text"] != "" || args["flag"] != false {
		t.Fatal(args, err)
	}
	if value, exists := args["nullable"]; !exists || value != nil {
		t.Fatal("lost null")
	}
	if value, exists := args["zero"]; !exists || value != float64(0) {
		t.Fatal("lost zero")
	}
	if value, ok := args["segments"].([]any); !ok || len(value) != 0 {
		t.Fatal("lost empty array")
	}
	data, _ := json.Marshal(bindings)
	var restored map[string]Binding
	_ = json.Unmarshal(data, &restored)
	if !restored["nullable"].provided {
		t.Fatal("storage lost presence")
	}
	_ = json.Unmarshal([]byte(`{"text":{"source":"fixed"}}`), &bindings)
	if _, err := Analyze(testSchema(t), bindings, true, nil); err == nil {
		t.Fatal("missing fixed value accepted")
	}
}
func TestVersionChangesProtectPublishedContract(t *testing.T) {
	bindings := map[string]Binding{"text": {Source: constants.WorkflowInputSourceAPI}}
	before := testSchema(t)
	after := testSchema(t)
	properties := after["properties"].(map[string]any)
	properties["text"].(map[string]any)["title"] = "新名称"
	properties["extra"] = map[string]any{"type": "string"}
	if err := Compatible(before, after, bindings, true); err != nil {
		t.Fatal(err)
	}
	properties["text"].(map[string]any)["type"] = "integer"
	if err := Compatible(before, after, bindings, true); err == nil {
		t.Fatal("changed API type accepted")
	}
}
func TestDuplicateKeysRejectedAtAnyDepth(t *testing.T) {
	for _, data := range []string{`{"args":{"text":"a","text":"b"}}`, `{"args":{},"args":{}}`, `{"args":{"items":[{"x":1,"x":2}]}}`, `{} {}`} {
		if err := ValidateJSONEnvelope([]byte(data)); err == nil {
			t.Fatal("accepted", data)
		}
	}
	if err := ValidateJSONEnvelope([]byte(`{"args":{"items":["a","b"]}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestLocalReferencesRemainValidInProjectedContract(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","required":["text","texts"],"properties":{"text":{"type":"string","minLength":2},"texts":{"type":"array","items":{"$ref":"#/properties/text"}}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	bindings := map[string]Binding{"text": {Source: constants.WorkflowInputSourceFixed, Value: "fixed"}, "texts": {Source: constants.WorkflowInputSourceAPI}}
	contract, err := Analyze(schema, bindings, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := workflowdsl.ValidateJSON(contract.Schema, map[string]any{"texts": []any{"valid"}}); err != nil {
		t.Fatal(err)
	}
	if err := workflowdsl.ValidateJSON(contract.Schema, map[string]any{"texts": []any{"x"}}); err == nil {
		t.Fatal("item constraint was lost")
	}
	if _, err := Resolve(schema, bindings, map[string]any{"texts": []any{1.0}}); err == nil {
		t.Fatal("item type was coerced")
	}
	if !contract.ExampleValid {
		t.Fatal("reference example is invalid")
	}
}

func TestEmptySchemaNeedsNoInputs(t *testing.T) {
	contract, err := Analyze(map[string]any{}, nil, true, nil)
	if err != nil || !contract.ExampleValid || len(contract.Fields) != 0 {
		t.Fatalf("%+v %v", contract, err)
	}
}

func TestConstrainedStringExampleIncludesRequiredField(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","required":["work_item_id"],"properties":{"work_item_id":{"type":"string","minLength":1,"maxLength":128,"pattern":"^[A-Za-z0-9_-]+$","x-ui-control":"input"}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	bindings := map[string]Binding{"work_item_id": {Source: constants.WorkflowInputSourceAPI}}
	contract, err := Analyze(schema, bindings, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, exists := contract.Example["work_item_id"]
	if !exists {
		t.Fatalf("required work_item_id missing from API example: %+v", contract.Example)
	}
	if text, ok := value.(string); !ok || text == "" || !contract.ExampleValid {
		t.Fatalf("expected a schema-valid string example: %+v", contract)
	}
	if _, err := Resolve(schema, bindings, contract.Example); err != nil {
		t.Fatal(err)
	}
}

func TestUnavailableExamplesKeepRequiredFieldStructure(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"type":"object","required":["code","options"],"properties":{"code":{"type":"string","pattern":"^REAL-[0-9]{20}$"},"options":{"type":"object","required":["token"],"properties":{"token":{"type":"string","pattern":"^REAL-[0-9]{20}$"}}}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	bindings := map[string]Binding{"code": {Source: constants.WorkflowInputSourceAPI}, "options": {Source: constants.WorkflowInputSourceAPI}}
	contract, err := Analyze(schema, bindings, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := contract.Example["code"]; !exists {
		t.Fatalf("required code missing from replacement example: %+v", contract.Example)
	}
	options, ok := contract.Example["options"].(map[string]any)
	if !ok {
		t.Fatalf("object field lost its JSON type: %+v", contract.Example)
	}
	if _, exists := options["token"]; !exists {
		t.Fatalf("nested required token missing from replacement example: %+v", options)
	}
	if contract.ExampleValid {
		t.Fatal("replacement example must remain marked as invalid")
	}
	if _, err := Resolve(schema, bindings, contract.Example); err == nil {
		t.Fatal("placeholder bypassed real input validation")
	}
}
