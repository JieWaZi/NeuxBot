package plugins

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
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

// 旧授权声明必须拒绝，不能在删除宿主供应商实现后作为普通插件交付。
func TestClientExtensionRejectsLegacyProviderDeclarations(t *testing.T) {
	for _, provider := range []string{"feishu", "github", "unsupported"} {
		t.Run(provider, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"provider": provider})
			pkg := Package{Manifest: Manifest{Extensions: map[string]json.RawMessage{constants.PluginExtensionNamespace: raw}}}
			if _, err := ClientExtension(pkg); !errors.Is(err, status_error.PluginManifestInvalid) {
				t.Fatalf("legacy provider accepted: %v", err)
			}
			pkg.Integration.Authentication = &Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Command: "./auth"}
			if _, err := ClientExtension(pkg); !errors.Is(err, status_error.PluginManifestInvalid) {
				t.Fatalf("bridge masked legacy provider declaration: %v", err)
			}
		})
	}
}
