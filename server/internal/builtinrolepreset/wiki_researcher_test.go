package builtinrolepreset

import (
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestWikiResearcherProvidesIndependentProfileAndDefaultWaker(t *testing.T) {
	var researcher Definition
	for _, definition := range Definitions() {
		if definition.RoleName == "维基研究员" {
			researcher = definition
		}
	}
	if researcher.ID == "" {
		t.Fatal("missing Wiki researcher role preset")
	}
	if researcher.DefaultWaker == nil || researcher.DefaultWaker.Name != "nova" {
		t.Fatalf("unexpected default waker: %+v", researcher.DefaultWaker)
	}
	if err := validateDefinitions([]Definition{researcher}); err != nil {
		t.Fatalf("invalid researcher definition: %v", err)
	}
	projection := researcher.DisplayProjection()
	if len(projection.Expertise) == 0 || len(projection.WorkStyle) == 0 || len(projection.WorkMethods) == 0 {
		t.Fatalf("incomplete researcher display: %+v", projection)
	}
	byWaker, ok := DefinitionByWakerID(researcher.DefaultWaker.ID)
	if !ok || byWaker.ID != researcher.ID {
		t.Fatal("researcher is not recognized as a builtin waker")
	}
	contents, err := ResolveWakerContents(researcher.DefaultWaker.ID, nil)
	if err != nil || len(contents) != len(profile.EditableKinds()) {
		t.Fatalf("researcher profile resolution failed: %v", err)
	}
	for _, kind := range profile.EditableKinds() {
		if contents[kind] == "" || contents[kind] != researcher.Documents[kind] {
			t.Fatalf("missing researcher document %q", kind)
		}
	}
	contents, err = ResolveWakerContents(researcher.DefaultWaker.ID, []models.WakerDocument{{
		Kind: constants.ProfileDocumentKindPersona, Content: "用户独立设置的研究风格",
	}})
	if err != nil || contents[constants.ProfileDocumentKindPersona] != "用户独立设置的研究风格" ||
		contents[constants.ProfileDocumentKindPlaybook] != researcher.Documents[constants.ProfileDocumentKindPlaybook] {
		t.Fatalf("researcher profile override lost: %v", err)
	}
}
