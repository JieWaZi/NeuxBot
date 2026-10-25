package mcp

import (
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestNewRuntimeKeyUsesReadableStableNameAndBoundedSuffix(t *testing.T) {
	connectorID := "483e09815fd042d48685d6dcce76554a"
	tests := map[string]string{
		"Chrome DevTools": "ally_chrome_devtools_",
		"浏览器自动化":          "ally_liu_lan_qi_zi_dong_hua_",
		"Chrome 浏览器":      "ally_chrome_liu_lan_qi_",
		"🧰":               "ally_mcp_",
	}
	for name, prefix := range tests {
		actual := newRuntimeKey(name, connectorID)
		if !strings.HasPrefix(actual, prefix) {
			t.Errorf("newRuntimeKey(%q) = %q, want prefix %q", name, actual, prefix)
		}
		if len(actual) > constants.MCPConnectorMaximumRuntimeKeyLength {
			t.Errorf("newRuntimeKey(%q) length = %d", name, len(actual))
		}
	}
}

func TestNewRuntimeKeyUsesConnectorIdentityWithoutExposingLongID(t *testing.T) {
	first := newRuntimeKey("Chrome", "11111111111111111111111111111111")
	second := newRuntimeKey("Chrome", "22222222222222222222222222222222")
	if first == second {
		t.Fatalf("runtime keys collide: %q", first)
	}
	if strings.Contains(first, "11111111111111111111111111111111") ||
		strings.Contains(second, "22222222222222222222222222222222") {
		t.Fatalf("runtime keys expose long connector IDs: %q, %q", first, second)
	}
}
