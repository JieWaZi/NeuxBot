package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/mcp"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	packageplugin "github.com/JieWaZi/NeuxBot/server/internal/plugins"
	"github.com/JieWaZi/NeuxBot/server/internal/plugins/commandbridge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPluginConnectorProbeUsesSharedAuthorizationAndOnlyDiscoversTools(t *testing.T) {
	for _, scenario := range []string{"authorized", "unauthorized", "identity-changed"} {
		authorized := scenario != "unauthorized"
		t.Run(scenario, func(t *testing.T) {
			ctx := pluginAPIContext(t)
			var requests, calls atomic.Int64
			var beforeTools func()
			service := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "probe", Version: "1"}, nil)
			mcpsdk.AddTool[map[string]any, any](service, &mcpsdk.Tool{Name: "search", Description: "Find documents"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, any, error) {
				calls.Add(1)
				return nil, nil, nil
			})
			handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return service }, nil)
			remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method == http.MethodPost && beforeTools != nil {
					body, _ := io.ReadAll(r.Body)
					r.Body = io.NopCloser(bytes.NewReader(body))
					if bytes.Contains(body, []byte(`"tools/list"`)) {
						beforeTools()
					}
				}
				if r.Header.Get("Authorization") != "Bearer probe-private-token" {
					http.Error(w, "unauthorized", 401)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer remote.Close()
			previousTransport := http.DefaultTransport
			http.DefaultTransport = remote.Client().Transport
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			previousBridge := global.PluginBridges
			global.PluginBridges = commandbridge.NewRegistry(context.Background(), nil)
			t.Cleanup(func() { global.PluginBridges.Close(); global.PluginBridges = previousBridge })
			source := t.TempDir()
			write := func(path, body string) {
				t.Helper()
				path = filepath.Join(source, path)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(".codex-plugin/plugin.json", `{"name":"probe","version":"1.0.0"}`)
			write(".mcp.json", fmt.Sprintf(`{"mcpServers":{"service":{"type":"http","url":%q}}}`, remote.URL))
			write(".neuxally/integration.json", fmt.Sprintf(`{"authentication":{"strategy":"plugin-command","protocol":"neuxally-auth/1","command":"node","args":["bridge.mjs"],"bindings":[{"runtime":"codex","server":"service","url":%q,"header":"Authorization"}]}}`, remote.URL))
			write("bridge.mjs", fmt.Sprintf(`import readline from 'node:readline';import {existsSync} from 'node:fs';import path from 'node:path';
for await(const line of readline.createInterface({input:process.stdin})){
const r=JSON.parse(line);let result;
if(r.method==='auth/status')result={state:'authorized',account:{configured:true,available:%t&&!existsSync(path.join(r.params.dataDirectory,'revoked')),id:'probe-user',scopes:[]}};
else if(r.method==='auth/resolve')result={bindings:r.params.components.map(c=>({componentId:c.componentId,headers:{Authorization:'Bearer probe-private-token'}}))};
else throw Error('unexpected authorization action');
console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));}`, authorized))
			stage, err := packageplugin.Stage(stagedRoot(), source)
			if err != nil {
				t.Fatal(err)
			}
			detail, err := prepareCatalogFixture(t, ctx, stage, "", "", true)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "identity-changed" {
				version, err := global.PluginControllerManager.Version(ctx, detail.ResolvedVersionID)
				if err != nil {
					t.Fatal(err)
				}
				beforeTools = func() {
					if err := os.WriteFile(filepath.Join(global.Config.DataDirectory, "plugins/data", detail.Plugin.ID, "revoked"), []byte("revoked"), 0600); err != nil {
						t.Error(err)
						return
					}
					auth, _, err := installationService().Bridge(version)
					if err != nil {
						t.Error(err)
						return
					}
					if _, err := auth.Status(ctx, true); err != nil {
						t.Error(err)
					}
				}
			}
			request := TestPluginConnector{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, Key: "service"}
			value, err := request.Output(ctx)
			if !authorized {
				if !errors.Is(err, status_error.PluginNotReady) || requests.Load() != 0 {
					t.Fatalf("unauthorized probe reached MCP: %v %d", err, requests.Load())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := value.(mcp.ConnectorResponse)
			if scenario == "identity-changed" {
				if result.Connector.TestStatus == constants.MCPTestStatusSucceeded || len(result.Connector.TestTools) != 0 {
					t.Fatalf("late result restored revoked identity: %+v", result)
				}
				return
			}
			if result.Connector.TestStatus != constants.MCPTestStatusSucceeded || result.Connector.TestedAt == 0 || len(result.Connector.TestTools) != 1 || result.Connector.TestTools[0].Name != "search" || calls.Load() != 0 {
				t.Fatalf("incorrect discovery: %+v calls=%d", result, calls.Load())
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "probe-private-token") {
				t.Fatal("probe leaked authorization")
			}
			cached, err := (&GetPluginConnector{PluginID: request.PluginID, VersionID: request.VersionID, Key: request.Key}).Output(ctx)
			if err != nil || len(cached.(mcp.ConnectorResponse).Connector.TestTools) != 1 {
				t.Fatalf("probe disappeared after ordinary refresh: %+v %v", cached, err)
			}
			config, err := global.PluginControllerManager.Configuration(ctx, request.PluginID)
			if err != nil {
				t.Fatal(err)
			}
			if err := global.PluginControllerManager.UpdateConfiguration(ctx, config, config.Revision); err != nil {
				t.Fatal(err)
			}
			refreshed, err := (&GetPluginConnector{PluginID: request.PluginID, VersionID: request.VersionID, Key: request.Key}).Output(ctx)
			if err != nil || len(refreshed.(mcp.ConnectorResponse).Connector.TestTools) != 0 {
				t.Fatalf("old configuration retained diagnostic: %+v %v", refreshed, err)
			}
			request.WakerID = "w2"
			if _, err := request.Output(ctx); !errors.Is(err, status_error.PluginForbidden) {
				t.Fatalf("uninstalled partner probe: %v", err)
			}
		})
	}
}

func TestPluginDiagnosticRedactsArgumentAndQueryTokens(t *testing.T) {
	connector := models.MCPConnector{Arguments: []string{"--token=private-argument-token"}, URL: "https://example.com/mcp?api_key=private-query-token&other=private%2Fencoded%2Ftoken"}
	redact := diagnosticRedactor(connector, nil)
	value := redact("private-argument-token private-query-token private/encoded/token --token=private-argument-token")
	for _, secret := range []string{"private-argument-token", "private-query-token", "private/encoded/token"} {
		if strings.Contains(value, secret) {
			t.Fatalf("diagnostic leaked a private source value: %q", value)
		}
	}
}

func TestPluginConnectorProbeKeepsOwnedSourceWhenNativeReferenceExists(t *testing.T) {
	ctx := pluginAPIContext(t)
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "owned", Version: "1"}, nil)
	mcpsdk.AddTool[map[string]any, any](server, &mcpsdk.Tool{Name: "owned_search"}, func(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, any, error) {
		return nil, nil, nil
	})
	remote := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil))
	defer remote.Close()
	source := t.TempDir()
	for name, body := range map[string]string{
		".codex-plugin/plugin.json":        `{"name":"owned-native","version":"1.0.0"}`,
		".mcp.json":                        fmt.Sprintf(`{"mcpServers":{"owned":{"type":"http","url":%q}}}`, remote.URL),
		".neuxally/integration.json":       `{"nativePlugins":{"codex":{"path":"native"}}}`,
		"native/.codex-plugin/plugin.json": `{"name":"native","version":"1.0.0"}`,
		"native/.mcp.json":                 `{"mcpServers":{"other":{"command":"node","args":["other.mjs"]}}}`,
	} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
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
	value, err := (&TestPluginConnector{PluginID: detail.Plugin.ID, VersionID: detail.ResolvedVersionID, Key: "owned"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(mcp.ConnectorResponse)
	if result.Connector.TestStatus != constants.MCPTestStatusSucceeded || len(result.Connector.TestTools) != 1 || result.Connector.TestTools[0].Name != "owned_search" {
		t.Fatalf("probe switched away from owned server: %+v", result)
	}
}
