package runtimeenvironment

import (
	"github.com/JieWaZi/NeuxBot/server/constants"
	agent "github.com/JieWaZi/NeuxBot/server/internal/agent_next"
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
