package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestPluginResourceResponsesKeepPublicCapabilitiesWithoutExecutionMetadata(t *testing.T) {
	pkg := packageplugin.Package{Manifest: packageplugin.Manifest{Name: "demo", Extensions: map[string]json.RawMessage{"private": json.RawMessage(`{"command":"internal-command"}`)}}, SourcePath: "private-source", ReadmePath: "private-readme", Readme: "Public guide", Skills: []packageplugin.Skill{{Key: "skills/demo", Name: "demo", Description: "Public skill"}}, Servers: []packageplugin.Server{{Key: "service", Type: "streamable-http", URL: "https://example.com/private?token=secret", Headers: map[string]string{"Authorization": "private-header"}}}, Integration: packageplugin.Integration{Authentication: &packageplugin.Authentication{Strategy: "plugin-command", Protocol: "neuxally-auth/1", Command: "internal-command", Args: []string{"private-arg"}, PreferredAction: "open-url"}}, NativeImplementations: map[string]packageplugin.NativeImplementation{"codex": {Path: "private-target", Name: "target", Digest: "target-digest", Skills: []packageplugin.NativeSkillSummary{{Key: "skills/target", Name: "target-skill"}}, Servers: []packageplugin.NativeServerSummary{{Key: "target-server", Transport: "stdio"}}}}}
	detail, err := detailFromState(context.Background(), models.Plugin{ID: "p"}, models.PluginState{Versions: []models.PluginVersion{{ID: "v", PluginID: "p", Digest: "version-digest", TrustedAt: 7, Package: pkg}}, Configuration: models.PluginConfiguration{Values: map[string]string{}}})
	if err != nil {
		t.Fatal(err)
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
	for _, retained := range []string{"Public guide", "target-skill", "target-server", "open-url", "version-digest"} {
		if !strings.Contains(string(body), retained) {
			t.Fatalf("public resource lost %q: %s", retained, body)
		}
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	var versions []struct {
		// Package 是此响应真正公开的包数据。
		Package struct {
			// Targets 按运行目标公开真实组件摘要。
			Targets map[string]map[string]json.RawMessage `json:"nativeImplementations"`
		} `json:"package"`
	}
	if err := json.Unmarshal(response["versions"], &versions); err != nil {
		t.Fatal(err)
	}
	for name := range versions[0].Package.Targets["codex"] {
		if name != "skills" && name != "servers" {
			t.Fatalf("unused native target field %q is public", name)
		}
	}
}
