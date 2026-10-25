package builtinrolepreset

import (
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestInvalidDefinitionsReturnStableConfigurationError(t *testing.T) {
	for _, mutate := range []func(*Definition){
		func(definition *Definition) { definition.ID = "" },
		func(definition *Definition) { definition.RoleName = "" },
		func(definition *Definition) { definition.DefaultAvatar = "" },
		func(definition *Definition) { delete(definition.Documents, constants.ProfileDocumentKindIdentity) },
	} {
		definition := personalAssistantDefinition()
		mutate(&definition)
		err := validateDefinitions([]Definition{definition})
		var stable statuserror.CommonError
		if !errors.As(err, &stable) || stable.Code() != status_error.BuiltinRolePresetDefinitionInvalid.Code() {
			t.Fatalf("invalid definition error = %v, want BuiltinRolePresetDefinitionInvalid", err)
		}
	}
}

func TestDefinitionValidationPreservesProfileError(t *testing.T) {
	definition := personalAssistantDefinition()
	definition.Documents[constants.ProfileDocumentKindIdentity] = string([]byte{0xff})
	if err := validateDefinitions([]Definition{definition}); !errors.Is(err, status_error.ProfileDocumentEncodingInvalid) {
		t.Fatalf("invalid document error = %v, want ProfileDocumentEncodingInvalid", err)
	}
}

func TestRegistryValidatesCatalog(t *testing.T) {
	if len(definitions) == 0 {
		t.Fatal("builtin role preset catalog must not be empty")
	}
	if err := validateDefinitions(definitions); err != nil {
		t.Fatalf("validateDefinitions() error = %v", err)
	}
}

func TestDefinitionsWithDefaultWakerIncludesPersonalAssistant(t *testing.T) {
	items := DefinitionsWithDefaultWaker()
	if len(items) != 2 {
		t.Fatalf("default waker count = %d, want 2", len(items))
	}
	if items[0].ID != PersonalAssistantRolePresetID {
		t.Fatalf("default waker role preset id = %q", items[0].ID)
	}
}

func TestDefinitionByWakerIDFindsPersonalAssistant(t *testing.T) {
	definition, exists := DefinitionByWakerID(PersonalAssistantWakerID)
	if !exists {
		t.Fatal("missing personal assistant definition by waker id")
	}
	if definition.ID != PersonalAssistantRolePresetID {
		t.Fatalf("role preset id = %q", definition.ID)
	}
}
