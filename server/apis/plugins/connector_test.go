package plugins

import (
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
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
	detail, err := prepareCatalogFixture(t, ctx, stage, "", true)
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

func TestPluginConnectorSanitizesSavedURL(t *testing.T) {
	pkg := packageplugin.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: packageplugin.Manifest{Schema: constants.PluginManifestSchema}, Servers: []packageplugin.Server{{Key: "service", Type: "streamable-http", URL: "https://user:password@example.com/mcp?token=hidden#hidden"}}}
	result, err := pluginConnectorResponse(pkg, "service")
	if err != nil || result.Connector.URL != "https://example.com/mcp" {
		t.Fatal("saved URL disclosed private parts", result.Connector.URL, err)
	}
}

func TestCatalogPluginConnectorPinsSourceWithoutInstalling(t *testing.T) {
	f := newDetailsFixture(t, "")
	request := GetCatalogPluginConnector{Key: "recovery", ConnectorKey: "remote", SourceCommit: f.commit, ArchiveSHA256: f.archiveHash}
	value, err := request.Output(f.ctx)
	if err != nil || value.(mcp.ConnectorResponse).Connector.URL != "https://example.invalid/mcp" {
		t.Fatalf("catalog connector missing: %+v %v", value, err)
	}
	listed, err := (&ListPlugins{}).Output(f.ctx)
	if err != nil || len(listed.(ListPluginsResponse).Resources) != 0 || f.archiveRequests.Load() != 0 {
		t.Fatalf("read created resources or fetched ZIP: %+v %v", listed, err)
	}
	request.ArchiveSHA256 = strings.Repeat("c", 64)
	if _, err := request.Output(f.ctx); !errors.Is(err, status_error.PluginVersionConflict) {
		t.Fatalf("stale catalog read: %v", err)
	}
}

func TestCatalogPluginConnectorReadsPublicAPIKeySummaryWithoutPrivateBindings(t *testing.T) {
	f := newDetailsFixture(t, "api-key")
	request := GetCatalogPluginConnector{Key: "recovery", ConnectorKey: "remote", SourceCommit: f.commit, ArchiveSHA256: f.archiveHash}
	value, err := request.Output(f.ctx)
	if err != nil {
		t.Fatalf("public API key connector was validated as an installed package: %v", err)
	}
	connector := value.(mcp.ConnectorResponse).Connector
	if connector.URL != "https://example.invalid/mcp" || connector.TestStatus != constants.MCPTestStatusUntested || len(connector.Headers) != 0 {
		t.Fatalf("public summary invented private configuration or diagnostics: %+v", connector)
	}
	_, total, err := global.PluginControllerManager.List(f.ctx, "", "", 0, 100)
	if err != nil || total != 0 || f.archiveRequests.Load() != 0 || f.detailsRequests.Load() != 1 {
		t.Fatalf("preview installed a resource or downloaded an archive: total=%d zip=%d details=%d err=%v", total, f.archiveRequests.Load(), f.detailsRequests.Load(), err)
	}
	request.ConnectorKey = "missing"
	if _, err := request.Output(f.ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("unknown public connector accepted: %v", err)
	}
}

func TestPluginConnectorRejectsInstalledAPIKeyWithoutPrivateBindings(t *testing.T) {
	pkg := packageplugin.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: packageplugin.Manifest{Schema: constants.PluginManifestSchema}, Servers: []packageplugin.Server{{Key: "remote", Type: "streamable-http", URL: "https://example.invalid/mcp", Authentication: &packageplugin.MCPAuthentication{Type: "api_key", HeaderScheme: "bearer"}}}}
	if _, err := pluginConnectorResponse(pkg, "remote"); !errors.Is(err, status_error.PluginManifestInvalid) {
		t.Fatalf("installed connector skipped authentication binding validation: %v", err)
	}
}

func TestPluginConnectorIncludesManagedHeaderNamesWithoutRequestingCredentials(t *testing.T) {
	pkg := packageplugin.Package{ContractVersion: constants.PluginTargetContractVersion, Format: constants.PluginFormatPortable, Manifest: packageplugin.Manifest{Schema: constants.PluginManifestSchema}, Servers: []packageplugin.Server{{Key: "service", Type: "streamable-http", URL: "https://example.com/mcp"}}, Integration: packageplugin.Integration{Authentication: &packageplugin.Authentication{Strategy: constants.PluginAuthCommandStrategy, Protocol: constants.PluginAuthProtocol, Bindings: []packageplugin.AuthenticationBinding{{Runtime: "codex", Server: "service", URL: "https://example.com/mcp", Header: "Authorization"}, {Runtime: "claude", Server: "service", URL: "https://example.com/mcp", Header: "Authorization"}}}}}
	result, err := pluginConnectorResponse(pkg, "service")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Connector.Headers) != 1 || result.Connector.Headers[0].Name != "Authorization" {
		t.Fatalf("managed authorization hidden as empty configuration: %+v", result)
	}
}

func TestCatalogPluginConnectorNeverFallsBackWhenDetailsOmitRequestedComponent(t *testing.T) {
	f := newDetailsFixture(t, "")
	if _, err := (&GetCatalogPluginConnector{Key: "recovery", ConnectorKey: "missing"}).Output(f.ctx); !errors.Is(err, status_error.PluginContentInvalid) {
		t.Fatalf("missing details component accepted: %v", err)
	}
	if f.archiveRequests.Load() != 0 {
		t.Fatal("missing public component downloaded archive")
	}
}
