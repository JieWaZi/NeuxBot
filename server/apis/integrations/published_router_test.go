package integrations

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	publication "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
)

func TestPublishedRouterExcludesDesktopManagementAndServesOnlyPublicContract(t *testing.T) {
	old := global.IntegrationAPIListener
	root := t.TempDir()
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	_ = reservation.Close()
	data, _ := json.Marshal(map[string]any{"ip": "127.0.0.1", "port": port, "enabled": true})
	if err = os.WriteFile(filepath.Join(root, "integration-api.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	global.IntegrationAPIListener = publication.NewListener(root, func(string) http.Handler { return http.NotFoundHandler() })
	if err = global.IntegrationAPIListener.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer global.IntegrationAPIListener.Close()
	defer func() { global.IntegrationAPIListener = old }()
	handler := NewPublishedHandler(nil, schedule.Config{}, "127.0.0.1:8431")
	for _, path := range []string{"/api/v1/integrations/keys", "/api/v1/chats", "/api/v1/knowledge/connections", "/openapi.json", "/api/v1/runtime-options"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-API-Key", "invalid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatalf("management route %s exposed with %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/integration-openapi.json", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var doc struct { // Paths 是公开文档实际暴露的路径。
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("empty public contract")
	}
	for path := range doc.Paths {
		if strings.Contains(path, "/integrations/") || strings.Contains(path, "/chats") || strings.Contains(path, "/connections") {
			t.Fatalf("management schema leaked: %s", path)
		}
	}
}

func TestPublishedMCPRequiresEnabledParentService(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	defer pool.Close()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(root, "knowledge.db"), RawDir: filepath.Join(root, "raw")})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	created, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "route-library", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	keys := controllers.NewIntegrationKeyController(db.DB)
	_, _, err = keys.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: "key", Digest: "key", KeyID: "route-key", Action: "create"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = keys.CompleteMutation(ctx, "key", "key", models.IntegrationKey{ID: "route-key", Name: "route key", KeyHash: publication.HashKey("fixture-only"), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{created.ResourceID}, Capabilities: []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	oldListener, oldEngine, oldKnowledge, oldAuthority, oldReferences := global.IntegrationAPIListener, global.KnowledgeEngine, global.KnowledgeService, global.IntegrationService, global.KnowledgeControllerManager
	defer func() {
		global.IntegrationAPIListener, global.KnowledgeEngine, global.KnowledgeService, global.IntegrationService, global.KnowledgeControllerManager = oldListener, oldEngine, oldKnowledge, oldAuthority, oldReferences
	}()
	global.KnowledgeEngine = engine
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(db.DB)
	global.IntegrationService = integrationservice.New(integrationservice.Config{Keys: keys})
	global.KnowledgeService = knowledgeservice.New(knowledgeservice.Config{Engine: engine, References: global.KnowledgeControllerManager})
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	_ = reservation.Close()
	saved, _ := json.Marshal(map[string]any{"ip": "127.0.0.1", "port": port, "enabled": true})
	if err = os.WriteFile(filepath.Join(root, "integration-api.json"), saved, 0600); err != nil {
		t.Fatal(err)
	}
	listener := publication.NewListener(root, func(address string) http.Handler { return NewPublishedHandler(nil, schedule.Config{}, address) })
	global.IntegrationAPIListener = listener
	if err = listener.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	manage, _ := gin.CreateTestContext(httptest.NewRecorder())
	manage.Request = httptest.NewRequest("PUT", "/api/v1/integrations/mcp", nil)
	base := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	probe := func(path string, want int, content string) {
		t.Helper()
		method := "GET"
		var body io.Reader
		if path == "/mcp" {
			method = "POST"
			body = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_libraries_list","arguments":{}}}`)
		}
		request, e := http.NewRequest(method, base+path, body)
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("X-API-Key", "fixture-only")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != want || content != "" && !strings.Contains(string(raw), content) {
			t.Fatalf("%s: status=%d body=%s", path, response.StatusCode, raw)
		}
	}
	status, err := (&GetAPIService{}).Output(manage)
	if err != nil {
		t.Fatal(err)
	}
	if status.(publication.ListenerSettings).MCPEnabled {
		t.Fatal("old settings opened MCP")
	}
	probe("/mcp", 404, "")
	probe("/api/v1/knowledge-bases/"+created.ResourceID, 200, "route-library")
	if _, err = (&UpdateMCPService{Body: MCPServiceInput{Enabled: true}}).Output(manage); err != nil {
		t.Fatal(err)
	}
	probe("/mcp", 200, "route-library")
	if _, err = (&UpdateAPIService{Body: APIServiceInput{Enabled: false, IP: "127.0.0.1", Port: port, PublicBaseURL: "https://proxy.example.test/prefix"}}).Output(manage); err != nil {
		t.Fatal(err)
	}
	if settings := listener.Settings(); !settings.MCPEnabled || settings.Available || settings.Enabled || listener.GetAPIStatus().Available {
		t.Fatal("MCP preference bypassed disabled parent", settings)
	}
	handler := NewPublishedHandler(nil, schedule.Config{}, "127.0.0.1")
	disabled := func() {
		t.Helper()
		for _, path := range []string{"/mcp", "/api/v1/knowledge-bases/" + created.ResourceID, "/integration-openapi.json"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if response.Code != 404 {
				t.Fatal("disabled parent admitted route", path, response.Code)
			}
		}
	}
	disabled()
	if _, err = (&UpdateMCPService{Body: MCPServiceInput{Enabled: true}}).Output(manage); err != nil {
		t.Fatal(err)
	}
	if listener.Settings().Available {
		t.Fatal("direct child enable opened disabled service")
	}
	disabled()
	connection, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if e == nil {
		connection.Close()
		t.Fatal("disabled parent retained listening port")
	}
	if _, err = (&UpdateAPIService{Body: APIServiceInput{Enabled: true, IP: "127.0.0.1", Port: port}}).Output(manage); err != nil {
		t.Fatal(err)
	}
	probe("/mcp", 200, "route-library")
	probe("/api/v1/knowledge-bases/"+created.ResourceID, 200, "route-library")
	if _, err = (&UpdateMCPService{Body: MCPServiceInput{Enabled: false}}).Output(manage); err != nil {
		t.Fatal(err)
	}
	probe("/mcp", 404, "")
	probe("/api/v1/knowledge-bases/"+created.ResourceID, 200, "route-library")
	if !listener.Settings().Enabled || !listener.Settings().Available || listener.Settings().MCPEnabled {
		t.Fatal("child disable disrupted parent", listener.Settings())
	}
}
