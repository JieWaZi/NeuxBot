package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestPluginConnectorReadsPinnedSnapshotWithoutCredentialsOrExecution(t *testing.T) {
	ctx := pluginAPIContext(t)
	source := t.TempDir()
	for path, body := range map[string]string{
		"plugin.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"connector-reader","version":"1.0.0"}`,
		"mcp.json":    `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"github":{"type":"streamable-http","url":"https://example.com/mcp?token=secret-query","headers":{"Authorization":"secret-header"}},"local":{"type":"stdio","command":"node","args":["secret-argument"],"env":{"TOKEN":"secret-env"}}}}`,
	} {
		if err := os.WriteFile(filepath.Join(source, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stage, err := packageplugin.Stage(stagedRoot(), source)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := prepareCatalogFixture(t, ctx, stage, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	request := GetPluginConnector{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, Key: "github"}
	value, err := request.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(mcp.ConnectorResponse)
	if result.Connector.Name != "github" || result.Connector.URL != "https://example.com/mcp" || len(result.Compatibility) == 0 || result.Connector.TestedAt != 0 {
		t.Fatalf("missing read-only information: %+v", result)
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("response cannot be serialized: %v", marshalErr)
	}
	if strings.Contains(string(raw), "secret-") {
		t.Fatalf("credentials leaked: %s", raw)
	}
	request.Key = "local"
	value, err = request.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, marshalErr = json.Marshal(value)
	if marshalErr != nil {
		t.Fatalf("response cannot be serialized: %v", marshalErr)
	}
	if strings.Contains(string(raw), "secret-") {
		t.Fatalf("stdio credentials leaked: %s", raw)
	}
	request.Key = "missing"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("unknown component: %v", err)
	}
	request.Key, request.WakerID = "github", "w1"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginForbidden) {
		t.Fatalf("uninstalled component visible: %v", err)
	}
	if err := global.PluginControllerManager.Install(ctx, models.WakerPlugin{PluginID: detail.Plugin.ID, WakerID: "w1", VersionID: detail.ResolvedVersionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := request.Output(ctx); err != nil {
		t.Fatal(err)
	}
	request.VersionID = "another-version"
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("wrong installed version: %v", err)
	}
}

func TestPluginConnectorUsesNativeSummaryAndSanitizesURL(t *testing.T) {
	pkg := packageplugin.Package{NativeImplementations: map[string]packageplugin.NativeImplementation{
		"codex": {Servers: []packageplugin.NativeServerSummary{{Key: "native", Name: "Native service", Transport: "streamable-http", Endpoint: "https://user:password@example.com/mcp?token=hidden#hidden"}}},
	}}
	value, err := pluginConnectorResponse(pkg, "native")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(value); err != nil {
		t.Fatalf("native response cannot be serialized: %v", err)
	}
	if value.Connector.Name != "Native service" || value.Connector.URL != "https://example.com/mcp" {
		t.Fatalf("wrong native info: %+v", value)
	}
}

func TestCatalogPluginConnectorPinsSourceWithoutInstalling(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", root)
	source, archive := strings.Repeat("a", 40), strings.Repeat("b", 64)
	document := map[string]any{"schemaVersion": 1, "entries": []map[string]any{{
		"key": "reader", "directory": "catalog/reader.zip", "sourceCommit": source, "archiveSHA256": archive,
		"package": map[string]any{"manifest": map[string]any{"name": "reader"}, "servers": []map[string]any{{"key": "service", "type": "streamable-http", "url": "https://example.com/mcp?secret=hidden"}}},
	}}}
	raw, _ := json.Marshal(document)
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	request := GetCatalogPluginConnector{Key: "reader", ConnectorKey: "service", SourceCommit: source, ArchiveSHA256: archive}
	value, err := request.Output(ctx)
	if err != nil || value.(mcp.ConnectorResponse).Connector.URL != "https://example.com/mcp" {
		t.Fatalf("catalog connector missing: %+v %v", value, err)
	}
	if _, err := json.Marshal(value); err != nil {
		t.Fatalf("catalog response cannot be serialized: %v", err)
	}
	listed, err := (&ListPlugins{}).Output(ctx)
	if err != nil || len(listed.(ListPluginsResponse).Resources) != 0 {
		t.Fatalf("read created resources: %+v %v", listed, err)
	}
	request.ArchiveSHA256 = strings.Repeat("c", 64)
	if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("stale catalog read: %v", err)
	}
}

func TestPluginConnectorIncludesManagedHeaderNamesWithoutRequestingCredentials(t *testing.T) {
	pkg := packageplugin.Package{Servers: []packageplugin.Server{{Key: "service", Type: "streamable-http", URL: "https://example.com/mcp"}}, Integration: packageplugin.Integration{Authentication: &packageplugin.Authentication{Bindings: []packageplugin.AuthenticationBinding{{Runtime: "codex", Server: "service", URL: "https://example.com/mcp", Header: "Authorization"}, {Runtime: "claude", Server: "service", URL: "https://example.com/mcp", Header: "Authorization"}}}}}
	result, err := pluginConnectorResponse(pkg, "service")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Connector.Headers) != 1 || result.Connector.Headers[0].Name != "Authorization" {
		t.Fatalf("managed authorization hidden as empty configuration: %+v", result)
	}
}

func TestCatalogPluginConnectorFallsBackWhenSnapshotOmitsRequestedComponent(t *testing.T) {
	ctx := pluginAPIContext(t)
	root := t.TempDir()
	t.Setenv("ALLY_PLUGIN_CATALOG_URL", root)
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for path, body := range map[string]string{
		".claude-plugin/plugin.json": `{"name":"reader","version":"1.0.0"}`,
		".mcp.json":                  `{"native":{"type":"http","url":"https://example.com/native"}}`,
	} {
		entry, err := writer.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "catalog/reader.zip"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data.Bytes())
	document := map[string]any{"schemaVersion": 1, "entries": []map[string]any{{"key": "reader", "directory": "catalog/reader.zip", "sourceCommit": strings.Repeat("a", 40), "archiveSHA256": hex.EncodeToString(digest[:]), "package": map[string]any{"manifest": map[string]any{"name": "reader"}, "servers": []map[string]any{{"key": "other", "type": "stdio", "command": "node"}}}}}}
	raw, _ := json.Marshal(document)
	if err := os.WriteFile(filepath.Join(root, "catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	value, err := (&GetCatalogPluginConnector{Key: "reader", ConnectorKey: "native"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if value.(mcp.ConnectorResponse).Connector.URL != "https://example.com/native" {
		t.Fatalf("wrong archive component: %+v", value)
	}
	entries, err := os.ReadDir(stagedRoot())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary snapshot leaked: %+v", entries)
	}
}
