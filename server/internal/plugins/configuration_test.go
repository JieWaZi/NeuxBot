package plugins

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
)

func TestClientExtensionBindingsCollectOnlyExplicitCredentialVariables(t *testing.T) {
	pkg := Package{Format: constants.PluginFormatPortable, Manifest: Manifest{Extensions: map[string]json.RawMessage{"com.ally": json.RawMessage(`{"bindings":[{"server":"remote","target":"header","name":"Authorization","value":"Bearer ${TOKEN}"}]}`)}}, Servers: []Server{{Key: "remote", Type: "streamable-http", URL: "https://example.com/mcp", Headers: map[string]string{"X-Literal": "${UNRECOGNIZED}"}}}}
	extension, err := ClientExtension(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if len(extension.Config) != 1 || extension.Config[0].Key != "TOKEN" || !extension.Config[0].Secret || len(extension.Bindings) != 1 {
		t.Fatalf("configuration changed portable literals: %+v", extension)
	}
	got := ExpandPortable("${PLUGIN_ROOT}:${PLUGIN_DATA}:${TOKEN}", "/root/${TOKEN}", "/data")
	if got != "/root/${TOKEN}:/data:${TOKEN}" {
		t.Fatalf("recursive or nonstandard expansion: %q", got)
	}
}

func TestClientExtensionRejectsNullObjectArraysAndFields(t *testing.T) {
	for _, raw := range []string{`null`, `{"config":null}`, `{"scopes":null}`, `{"bindings":null}`, `{"config":[{"key":"token","secret":null}]}`} {
		pkg := Package{Manifest: Manifest{Extensions: map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(raw)}}}
		if _, err := ClientExtension(pkg); err == nil {
			t.Fatalf("accepted invalid own namespace: %s", raw)
		}
	}
}
