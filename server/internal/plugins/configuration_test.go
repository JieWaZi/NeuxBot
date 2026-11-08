package plugins

import (
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"testing"
)

func TestClientExtensionUsesOnlyDeclaredCredentialFields(t *testing.T) {
	pkg := standardTestPackage()
	pkg.Manifest.Extensions = map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(`{"schemaVersion":1,"configuration":{"fields":[{"key":"TOKEN","secret":true,"required":true}],"bindings":[{"server":"remote","target":"header","name":"Authorization","value":"Bearer ${TOKEN}"}]}}`)}
	pkg.Servers = []Server{{Key: "remote", Type: "streamable-http", URL: "https://example.com/mcp", Headers: map[string]string{"X-Literal": "${UNRECOGNIZED}"}}}
	extension, err := ClientExtension(pkg)
	if err != nil || len(extension.Config) != 1 || extension.Config[0].Key != "TOKEN" {
		t.Fatalf("declared config: %+v %v", extension, err)
	}
	if err := ValidateValues(pkg, nil, nil); err == nil {
		t.Fatal("missing declared required secret")
	}
	if got := ExpandPortable("${PLUGIN_ROOT}:${PLUGIN_DATA}:${TOKEN}", "/root/${TOKEN}", "/data"); got != "/root/${TOKEN}:/data:${TOKEN}" {
		t.Fatal("recursive expansion")
	}
}
func TestClientExtensionDoesNotDiscoverImplicitSecrets(t *testing.T) {
	pkg := standardTestPackage()
	pkg.Servers = []Server{{Key: "remote", Type: "streamable-http", URL: "https://example.com/mcp"}}
	pkg.Manifest.Extensions = map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(`{"schemaVersion":1,"configuration":{"bindings":[{"server":"remote","target":"header","name":"Authorization","value":"${UNDECLARED}"}]}}`)}
	if _, err := ClientExtension(pkg); err == nil {
		t.Fatal("unknown variable silently created secret field")
	}
}
func TestClientExtensionRejectsNullAndLegacyConfiguration(t *testing.T) {
	for _, raw := range []string{`null`, `{"schemaVersion":1,"configuration":null}`, `{"schemaVersion":1,"configuration":{"fields":null}}`, `{"schemaVersion":1,"configuration":{"fields":[{"key":"token","secret":null}]}}`, `{"schemaVersion":1,"displayName":"old"}`, `{"schemaVersion":1,"requiredComponents":[]}`, `{"schemaVersion":1,"configuration":{"config":[]}}`} {
		pkg := standardTestPackage()
		pkg.Manifest.Extensions = map[string]json.RawMessage{constants.PluginExtensionNamespace: json.RawMessage(raw)}
		if _, err := ClientExtension(pkg); err == nil {
			t.Fatalf("accepted invalid own namespace: %s", raw)
		}
	}
}
