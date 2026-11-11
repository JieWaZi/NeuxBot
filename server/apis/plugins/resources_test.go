package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestPluginResourceResponsesKeepPublicCapabilitiesWithoutExecutionMetadata(t *testing.T) {
	pkg := packageplugin.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: packageplugin.Manifest{Schema: constants.PluginManifestSchema, Name: "demo", Extensions: map[string]json.RawMessage{"private": json.RawMessage(`{"command":"internal-command"}`), constants.PluginExtensionNamespace: json.RawMessage(`{"schemaVersion":1,"configuration":{"fields":[{"key":"API_TOKEN","secret":true}]}}`)}}, SourcePath: "private-source", ReadmePath: "private-readme", Readme: "Public guide", Skills: []packageplugin.Skill{{Key: "skills/demo", Name: "demo", Description: "Public skill"}}, Servers: []packageplugin.Server{{Key: "service", Type: "streamable-http", URL: "https://example.com/private?token=secret", Headers: map[string]string{"Authorization": "private-header"}}}, Integration: packageplugin.Integration{Authentication: &packageplugin.Authentication{Strategy: "plugin-command", Protocol: constants.PluginAuthProtocol, Command: "internal-command", Args: []string{"private-arg"}, PreferredAction: "open-url"}}}

	detail, err := detailFromState(context.Background(), models.Plugin{ID: "p"}, models.PluginState{Versions: []models.PluginVersion{{ID: "v", PluginID: "p", Digest: "version-digest", TrustedAt: 7, Package: pkg}}, Configuration: models.PluginConfiguration{Values: map[string]string{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Versions[0].Package.HasConfiguration {
		t.Fatal("public capability lost configuration availability")
	}
	body, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sourcePath", "readmePath", "nativeComponents", "private-source", "private-target", "private-header", "internal-command", "private-arg", "token=secret", "extensions"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public resource leaked %q: %s", forbidden, body)
		}
	}
	for _, retained := range []string{"Public guide", "Public skill", "service", "open-url", "version-digest"} {
		if !strings.Contains(string(body), retained) {
			t.Fatalf("public resource lost %q: %s", retained, body)
		}
	}
}
