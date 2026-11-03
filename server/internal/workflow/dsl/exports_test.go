package dsl

import (
	"strings"
	"testing"
)

func TestOnlyActualTopLevelMetaAndScriptsExportsAreAccepted(t *testing.T) {
	meta := `const meta={sdk:'ally.workflow',title:'test',description:'test',phases:[{id:'one',title:'one',detail:'one',canExit:true}]};`
	for _, source := range []string{
		meta + `export const scripts={};return true;`,
		`if(true){export ` + meta + `}return true;`,
		`export ` + strings.Replace(meta, "const meta=", "const metaExtra=", 1) + meta + `return true;`,
		`export ` + strings.TrimSuffix(meta, ";") + `,extra={};return true;`,
		`export ` + strings.Replace(meta, "title:'test'", "title:'test',title:'override'", 1) + `return true;`,
		`export ` + meta + `const scripts={};return true;`,
	} {
		if _, diagnostics, err := Compile(source); err == nil || len(diagnostics) == 0 {
			t.Fatalf("invalid export/literal accepted: %s", source)
		}
	}
	if _, diagnostics, err := Compile("export /* metadata */\n" + meta + "export\nconst scripts={};return true;"); err != nil {
		t.Fatalf("valid trivia around export rejected: %v %+v", err, diagnostics)
	}
}
