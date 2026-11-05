package runtimeenvironment

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
	"slices"
	"testing"
)

func TestNativeACPPermissionsRequireAdvertisedModes(t *testing.T) {
	for _, provider := range []string{"workbuddy", "qoder", "qoder-cn"} {
		if levels := NativePermissionLevels(provider, agent.AgentChoice{}); len(levels) != 0 {
			t.Fatalf("%s invented permission menu: %v", provider, levels)
		}
		limited := NativePermissionLevels(provider, agent.AgentChoice{PermissionModes: []string{"default"}})
		if len(limited) != 1 || limited[0] != constants.PermissionLevelDefault {
			t.Fatalf("limited: %v", limited)
		}
		full := NativePermissionLevels(provider, agent.AgentChoice{PermissionModes: []string{"default", "auto", "bypassPermissions"}})
		if len(full) != 3 || full[2] != constants.PermissionLevelFullAccess {
			t.Fatalf("native alias ignored: %v", full)
		}
	}
}

func TestManagedCatalogExcludesOtherProviderModelsWithMatchingIDs(t *testing.T) {
	for _, provider := range []string{"opencode", "pi"} {
		native := accountconfig.NativeModelID(provider, "chosen")
		original := agent.ModelCatalogSnapshot{AgentModelCatalog: agent.AgentModelCatalog{DefaultID: "other/chosen", Models: []agent.AgentChoice{
			{ID: "chosen"}, {ID: "other/chosen"}, {ID: native}, {ID: accountconfig.NativeModelID(provider, "unselected")},
		}}}
		selected := selectManagedCatalog(provider, original, []string{"chosen"})
		if len(selected.Models) != 1 || selected.Models[0].ID != "chosen" || selected.DefaultID != "" {
			t.Fatalf("%s mixed managed and native credentials: %+v", provider, selected)
		}
		if len(original.Models) != 4 || original.Models[2].ID != native {
			t.Fatal("account projection changed the cached native catalog")
		}
	}
}

func TestNativeFamilyPermissionMenusFollowRealPolicies(t *testing.T) {
	for _, provider := range []string{"gemini", "opencode", "grok", "kimi"} {
		if slices.Contains(NativePermissionLevels(provider, agent.AgentChoice{}), constants.PermissionLevelAuto) {
			t.Fatalf("%s invented automatic approval", provider)
		}
	}
	if levels := NativePermissionLevels("kimi", agent.AgentChoice{PermissionModes: []string{"default", "auto", "yolo"}}); len(levels) != 3 {
		t.Fatalf("Kimi advertised permission modes lost: %v", levels)
	}
	if levels := NativePermissionLevels("grok", agent.AgentChoice{AutoMode: true}); len(levels) != 3 {
		t.Fatalf("Grok reported auto mode lost: %v", levels)
	}
	if levels := NativePermissionLevels("pi", agent.AgentChoice{}); len(levels) != 3 {
		t.Fatalf("Pi tool bridge permission policies lost: %v", levels)
	}
}
