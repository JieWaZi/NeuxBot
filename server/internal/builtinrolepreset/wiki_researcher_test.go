package builtinrolepreset

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/internal/profile"
)

func TestWikiResearcherProvidesOptionalRoleWithoutDefaultWaker(t *testing.T) {
	var researcher Definition
	for _, definition := range Definitions() {
		if definition.RoleName == "维基研究员" {
			researcher = definition
		}
	}
	if researcher.ID == "" {
		t.Fatal("missing Wiki researcher role preset")
	}
	if researcher.DefaultWaker != nil {
		t.Fatalf("optional researcher must not seed a default waker: %+v", researcher.DefaultWaker)
	}
	if err := validateDefinitions([]Definition{researcher}); err != nil {
		t.Fatalf("invalid researcher definition: %v", err)
	}
	projection := researcher.DisplayProjection()
	if len(projection.Expertise) == 0 || len(projection.WorkStyle) == 0 || len(projection.WorkMethods) == 0 {
		t.Fatalf("incomplete researcher display: %+v", projection)
	}
	for _, kind := range profile.EditableKinds() {
		if researcher.Documents[kind] == "" {
			t.Fatalf("missing researcher document %q", kind)
		}
	}
}
