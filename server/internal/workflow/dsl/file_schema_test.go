package dsl

import (
	"reflect"
	"testing"
)

func TestFileSchemaTraversesReferencesArraysAndSelectedBranches(t *testing.T) {
	schema := map[string]any{
		"type": "object", "$defs": map[string]any{"file": map[string]any{"type": "string", "format": "file"}},
		"properties": map[string]any{
			"docs/~": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/file"}},
			"choice": map[string]any{"oneOf": []any{
				map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"const": "file"}, "data": map[string]any{"$ref": "#/$defs/file"}}},
				map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"const": "text"}, "data": map[string]any{"type": "string"}}},
			}},
			"note": map[string]any{"type": "string"},
		},
	}
	unused := map[string]any{"type": "string", "default": "report.txt", "$defs": schema["$defs"]}
	if len(FileValues(unused, "report.txt")) != 0 {
		t.Fatal("unused file definitions were treated as output fields")
	}
	value := map[string]any{"docs/~": []any{"first.xlsx", "second.docx"}, "choice": map[string]any{"kind": "text", "data": "text.docx"}, "note": "private.txt"}
	expected := map[string]string{"/docs~1~0/0": "first.xlsx", "/docs~1~0/1": "second.docx"}
	if got := FileValues(schema, value); !reflect.DeepEqual(got, expected) {
		t.Fatalf("file fields: %#v", got)
	}
	converted, err := MapFiles(schema, value, func(pointer, path string) (string, error) { return "uploaded/" + path, nil })
	if err != nil || converted.(map[string]any)["docs/~"].([]any)[0] != "uploaded/first.xlsx" || value["docs/~"].([]any)[0] != "first.xlsx" {
		t.Fatalf("copy conversion: %#v %v", converted, err)
	}
	value["choice"] = map[string]any{"kind": "file", "data": "report.pdf"}
	if FileValues(schema, value)["/choice/data"] != "report.pdf" {
		t.Fatal("selected file branch missing")
	}
	if _, err := ResolveSchema(map[string]any{"type": "object", "format": "file"}); err == nil {
		t.Fatal("file schema allowed object values")
	}
}
