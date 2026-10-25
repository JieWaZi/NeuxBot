package mcpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStreamableHTTPListsToolsWithConfiguredHeaders(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mcpclient-test",
		Version: "1",
	}, nil)
	mcp.AddTool[map[string]any, any](server, &mcp.Tool{
		Name:        "search",
		Description: "搜索代码",
	}, func(
		context.Context,
		*mcp.CallToolRequest,
		map[string]any,
	) (*mcp.CallToolResult, any, error) {
		return nil, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		nil,
	)
	httpServer := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			http.Error(writer, "missing authorization", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(writer, request)
	}))
	t.Cleanup(httpServer.Close)

	result := Test(context.Background(), models.MCPConnector{
		Transport: constants.MCPTransportStreamableHTTP,
		URL:       httpServer.URL,
		TimeoutMS: 5_000,
		Headers: []models.MCPSecretDefinition{{
			Name:       "Authorization",
			Configured: true,
			Value:      "Bearer secret",
		}},
	})

	if result.Status != constants.MCPTestStatusSucceeded {
		t.Fatalf("test status = %q, error = %q", result.Status, result.Error)
	}
	if len(result.Tools) != 1 ||
		result.Tools[0].Name != "search" ||
		result.Tools[0].Description != "搜索代码" {
		t.Fatalf("tools = %+v", result.Tools)
	}
}

func TestConfiguredSecretRequiresDecryptedValue(t *testing.T) {
	t.Parallel()

	result := Test(context.Background(), models.MCPConnector{
		Transport: constants.MCPTransportStreamableHTTP,
		URL:       "https://example.invalid/mcp",
		TimeoutMS: 100,
		Headers: []models.MCPSecretDefinition{{
			Name:       "Authorization",
			Configured: true,
		}},
	})

	if result.Status != constants.MCPTestStatusFailed {
		t.Fatalf("test status = %q, want failed", result.Status)
	}
	if result.Error == "" {
		t.Fatal("test error is empty")
	}
}

func TestPluginDiagnosticDoesNotForwardAuthorizationThroughRedirects(t *testing.T) {
	var received bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true; http.Error(w, "not MCP", 400) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	result := TestInDirectory(context.Background(), models.MCPConnector{Transport: constants.MCPTransportStreamableHTTP, URL: origin.URL, TimeoutMS: 1000, Headers: []models.MCPSecretDefinition{{Name: "Authorization", Value: "Bearer private-token", Configured: true}}}, "")
	if received {
		t.Fatal("plugin authorization was forwarded to a redirected endpoint")
	}
	if result.Status != constants.MCPTestStatusFailed {
		t.Fatalf("redirect was accepted as a valid plugin connection: %+v", result)
	}
}

func TestPluginDiagnosticUsesPinnedWorkingDirectory(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "expected.txt"), []byte("Pinned directory instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	script := `import readline from 'node:readline';import {readFileSync} from 'node:fs';
for await(const line of readline.createInterface({input:process.stdin})){
const r=JSON.parse(line);if(r.id===undefined)continue;let result;
if(r.method==='initialize')result={protocolVersion:r.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'directory-fixture',version:'1'}};
else if(r.method==='tools/list')result={tools:[{name:'inspect',description:readFileSync('expected.txt','utf8'),inputSchema:{type:'object'}}]};
else result={};console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));}`
	if err := os.WriteFile(filepath.Join(root, "server.mjs"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	result := TestInDirectory(context.Background(), models.MCPConnector{Transport: constants.MCPTransportStdio, Command: node, Arguments: []string{"server.mjs"}, TimeoutMS: 3000}, root)
	if result.Status != constants.MCPTestStatusSucceeded || len(result.Tools) != 1 || result.Tools[0].Description != "Pinned directory instructions" {
		t.Fatalf("wrong working directory: %+v", result)
	}
}

func TestPluginDiagnosticSupportsSameOriginSSEMessages(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "sse", Version: "1"}, nil)
	mcp.AddTool[map[string]any, any](server, &mcp.Tool{Name: "search", Description: "SSE discovery"}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
		return nil, nil, nil
	})
	handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, nil)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-sse-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer remote.Close()
	result := TestInDirectory(context.Background(), models.MCPConnector{Transport: constants.MCPTransportSSE, URL: remote.URL, TimeoutMS: 3000, Headers: []models.MCPSecretDefinition{{Name: "Authorization", Value: "Bearer private-sse-token", Configured: true}}}, "")
	if result.Status != constants.MCPTestStatusSucceeded || len(result.Tools) != 1 || result.Tools[0].Name != "search" {
		t.Fatalf("same-origin SSE messages rejected: %+v", result)
	}
}

func TestPluginDiagnosticRejectsCrossOriginSSEMessages(t *testing.T) {
	var received atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(true)
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: endpoint\ndata: " + destination.URL + "/messages\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer origin.Close()
	result := TestInDirectory(context.Background(), models.MCPConnector{
		Transport: constants.MCPTransportSSE,
		URL:       origin.URL,
		TimeoutMS: 1000,
		Headers: []models.MCPSecretDefinition{{
			Name: "Authorization", Value: "Bearer private-token", Configured: true,
		}},
	}, "")
	if received.Load() || result.Status != constants.MCPTestStatusFailed {
		t.Fatalf("cross-origin SSE endpoint was accepted: %+v", result)
	}
}
