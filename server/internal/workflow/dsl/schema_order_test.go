package dsl

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestManifestKeepsDeclaredSchemaFieldOrderAfterSerialization(t *testing.T) {
	source := `export const meta={sdk:'ally.workflow/2',title:'顺序',description:'声明顺序',inputSchema:{type:'object',properties:{z:{type:'string'},a:{type:'object',properties:{second:{type:'string'},first:{type:'string'}}},list:{type:'array',items:{type:'object',properties:{y:{type:'string'},x:{type:'string'}}}}}},phases:[{id:'one',title:'one',detail:'one',canExit:true}]};return args;`
	compiled, diagnostics, err := Compile(source)
	if err != nil {
		t.Fatal(err, diagnostics)
	}
	encoded, err := json.Marshal(compiled.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var restored Manifest
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string][]string{"": {"z", "a", "list"}, "/properties/a": {"second", "first"}, "/properties/list/items": {"y", "x"}} {
		if !reflect.DeepEqual(restored.InputPropertyOrder[path], expected) {
			t.Fatalf("source order lost at %s: %+v", path, restored.InputPropertyOrder)
		}
	}
}
