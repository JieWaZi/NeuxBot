package integration_mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability/contract"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	knowledgeservice "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// keyTransport 在 HTTP 边界提供测试凭据。
type keyTransport struct {
	token string // 测试凭据。
}

func (t keyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-API-Key", t.token)
	return http.DefaultTransport.RoundTrip(r)
}

// fixture 保存真实临时存储与 HTTP 服务，不运行后台模型。
type fixture struct {
	engine    *core.Engine                         // 知识权威。
	library   *core.Library                        // 授权库。
	other     *core.Library                        // 真实存在但未授权的库。
	authority *integrationservice.Service          // 集成认证。
	keys      controllers.IntegrationKeyController // 主体存储。
	refs      controllers.KnowledgeController      // 操作引用。
	service   *knowledgeservice.Service            // 共享领域服务。
	server    *httptest.Server                     // SDK 测试服务器。
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	db, e := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "app.db"), Migrate: true})
	if e != nil {
		t.Fatal(e)
	}
	pool, _ := db.DB.DB()
	t.Cleanup(func() { pool.Close() })
	engine, e := core.Open(ctx, core.Options{DBPath: filepath.Join(root, "knowledge.db"), RawDir: filepath.Join(root, "raw")})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { engine.Close() })
	f := &fixture{engine: engine, keys: controllers.NewIntegrationKeyController(db.DB), refs: controllers.NewKnowledgeController(db.DB)}
	for _, name := range []string{"允许库", "秘密库"} {
		r, e := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: name, Name: name, Kind: "general", WikiLanguage: "zh-CN"})
		if e != nil {
			t.Fatal(e)
		}
		if f.library == nil {
			f.library, _ = engine.GetLibrary(ctx, r.ResourceID)
		} else {
			f.other, _ = engine.GetLibrary(ctx, r.ResourceID)
		}
	}
	f.authority = integrationservice.New(integrationservice.Config{Keys: f.keys})
	for _, id := range []string{"all", "read", "ingest", "organize", "second"} {
		caps := []constants.IntegrationCapability{constants.IntegrationCapabilityRetrieve}
		if id == "all" || id == "ingest" {
			caps = append(caps, constants.IntegrationCapabilityIngest)
		}
		if id == "all" || id == "organize" || id == "second" {
			caps = append(caps, constants.IntegrationCapabilityOrganize)
		}
		_, _, e = f.keys.ClaimMutation(ctx, models.IntegrationKeyMutation{RequestID: id, Digest: id, KeyID: id, Action: "create"})
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.keys.CompleteMutation(ctx, id, id, models.IntegrationKey{ID: id, Name: id, KeyHash: rules.HashKey(id), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{f.library.ID}, Capabilities: caps}}}, 0)
		if e != nil {
			t.Fatal(e)
		}
	}
	f.service = knowledgeservice.New(knowledgeservice.Config{Engine: engine, References: f.refs, IntegrationGuard: f.authority.RestoreKnowledgeGuard, IntegrationPublicationGuard: f.authority.RestoreKnowledgeGuards, ResolveModel: func(context.Context, models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		return models.LibraryExecutionDefaults{RuntimeID: "fixture", Model: "fixture"}, nil
	}})
	f.server = httptest.NewServer(NewHandler(f.service, f.authority))
	t.Cleanup(f.server.Close)
	return f
}
func (f *fixture) client(t *testing.T, key string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "fixture", Version: "1"}, nil)
	session, e := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: f.server.URL + "/mcp", HTTPClient: &http.Client{Transport: keyTransport{key}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func call(t *testing.T, c *sdk.ClientSession, opID string, params map[string]any, wantError bool) agentcapability.Response {
	t.Helper()
	var arguments any
	if params != nil {
		arguments = params
	}
	r, e := c.CallTool(context.Background(), &sdk.CallToolParams{Name: strings.ReplaceAll(opID, ".", "_"), Arguments: arguments})
	if e != nil {
		t.Fatalf("%s: %v", opID, e)
	}
	if r.IsError != wantError {
		raw, _ := json.Marshal(r.StructuredContent)
		t.Fatalf("%s error=%v content=%s", opID, r.IsError, raw)
	}
	raw, _ := json.Marshal(r.StructuredContent)
	var out agentcapability.Response
	if json.Unmarshal(raw, &out) != nil {
		t.Fatal(string(raw))
	}
	op, _ := contract.Find(opID)
	encoded, _ := json.Marshal(responseSchema(op))
	var schema jsonschema.Schema
	_ = json.Unmarshal(encoded, &schema)
	resolved, e := schema.Resolve(nil)
	if e != nil {
		t.Fatal(e)
	}
	var data any
	_ = json.Unmarshal(raw, &data)
	if e = resolved.Validate(data); e != nil {
		t.Fatalf("%s output schema: %v %s", opID, e, raw)
	}
	return out
}
func getReceipt(t *testing.T, out agentcapability.Response) *core.Receipt {
	t.Helper()
	raw, _ := json.Marshal(out.Data)
	var data struct {
		Receipt *core.Receipt `json:"receipt"` // 真实领域回执。
	}
	if json.Unmarshal(raw, &data) != nil || data.Receipt == nil {
		t.Fatalf("no receipt: %s", raw)
	}
	return data.Receipt
}
func (f *fixture) lib(t *testing.T) *core.Library {
	t.Helper()
	l, e := f.engine.GetLibrary(context.Background(), f.library.ID)
	if e != nil {
		t.Fatal(e)
	}
	return l
}

func TestSDKHTTPAllKnowledgeContractsAndSharedWrites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	c := f.client(t, "all")
	tools, e := c.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	operations := knowledgeOperations()
	if len(tools.Tools) != len(operations) {
		t.Fatal(len(tools.Tools), len(operations))
	}
	for _, tool := range tools.Tools {
		if tool.Name == "knowledge_pages_purge" || tool.Name == "knowledge_jobs_wait" {
			t.Fatal("external MCP exposed an internal maintenance-only operation")
		}
		op, ok := contract.Find(strings.ReplaceAll(tool.Name, "_", "."))
		if !ok || !strings.HasPrefix(op.ID, "knowledge.") {
			t.Fatal(tool.Name)
		}
		a, _ := json.Marshal(tool.InputSchema)
		b, _ := json.Marshal(toolSchema(op))
		if string(a) != string(b) {
			t.Fatal("schema drift", op.ID)
		}
		if tool.Annotations.ReadOnlyHint != (op.Effect == constants.AgentCLIEffectRead) {
			t.Fatal(tool.Name)
		}
	}
	lib := f.lib(t)
	create := map[string]any{"requestId": "create", "libraryId": lib.ID, "expectedVersion": 0, "structureVersion": lib.StructureVersion, "slug": "concept/topic", "title": "主题", "summary": "简介", "content": "旧说明与保留内容", "pageType": "concept"}
	pageReceipt := getReceipt(t, call(t, c, "knowledge.pages.write", create, false))
	call(t, c, "knowledge.pages.write", create, false)
	page, e := f.engine.GetPage(ctx, lib.ID, pageReceipt.ResourceID)
	if e != nil || page.Version != 1 {
		t.Fatal(page, e)
	}
	changed := map[string]any{}
	for k, v := range create {
		changed[k] = v
	}
	changed["content"] = "different"
	call(t, c, "knowledge.pages.write", changed, true)
	imported := getReceipt(t, call(t, c, "knowledge.sources.import", map[string]any{"requestId": "import", "libraryId": lib.ID, "filename": "source.md", "bytes": base64.StdEncoding.EncodeToString([]byte("# 原文\n持久内容"))}, false))
	call(t, c, "knowledge.pages.replace", map[string]any{"requestId": "first-replace", "libraryId": lib.ID, "pageId": page.ID, "expectedVersion": page.Version, "structureVersion": f.lib(t).StructureVersion, "oldText": "旧说明", "newText": "新说明"}, false)
	stale := map[string]any{}
	for k, v := range create {
		stale[k] = v
	}
	stale["requestId"] = "stale-time"
	stale["pageId"] = page.ID
	stale["expectedVersion"] = 2
	stale["expectedUpdatedAt"] = page.UpdatedAt.Format(time.RFC3339Nano)
	stale["structureVersion"] = f.lib(t).StructureVersion
	call(t, c, "knowledge.pages.write", stale, true)
	for _, op := range operations {
		if op.Effect != constants.AgentCLIEffectRead {
			continue
		}
		p := map[string]any{"libraryId": lib.ID}
		switch op.ID {
		case "knowledge.libraries.list":
			p = nil
		case "knowledge.pages.get":
			p["pageId"] = page.ID
		case "knowledge.pages.search":
			p["query"] = "主题"
		case "knowledge.sources.get", "knowledge.sources.read":
			p["sourceVersionId"] = imported.ResourceID
		case "knowledge.revisions.list", "knowledge.revisions.get":
			p["pageId"] = page.ID
			if op.ID == "knowledge.revisions.get" {
				p["version"] = 1
			}
		case "knowledge.jobs.get":
			continue
		}
		out := call(t, c, op.ID, p, false)
		if op.ID == "knowledge.libraries.list" {
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "秘密库") {
				t.Fatal("library leak")
			}
		}
	}
	common := func(request string) map[string]any {
		p, _ := f.engine.GetPage(ctx, lib.ID, page.ID)
		return map[string]any{"requestId": request, "libraryId": lib.ID, "pageId": page.ID, "expectedVersion": p.Version, "structureVersion": f.lib(t).StructureVersion}
	}
	p := common("replace")
	p["oldText"] = "新说明"
	p["newText"] = "后续说明"
	call(t, c, "knowledge.pages.replace", p, false)
	p = common("rename")
	p["newSlug"] = "concept/renamed"
	call(t, c, "knowledge.pages.rename", p, false)
	p = common("restore")
	p["revisionVersion"] = 1
	call(t, c, "knowledge.revisions.restore", p, false)
	call(t, c, "knowledge.pages.delete", common("archive"), false)
	lib = f.lib(t)
	theme := getReceipt(t, call(t, c, "knowledge.themes.write", map[string]any{"requestId": "theme", "libraryId": lib.ID, "structureVersion": lib.StructureVersion, "name": "目录"}, false))
	call(t, c, "knowledge.themes.write", map[string]any{"requestId": "theme-update", "libraryId": lib.ID, "themeId": theme.ResourceID, "structureVersion": f.lib(t).StructureVersion, "name": "新目录"}, false)
	call(t, c, "knowledge.themes.write", map[string]any{"requestId": "theme-delete", "libraryId": lib.ID, "themeId": theme.ResourceID, "structureVersion": f.lib(t).StructureVersion, "name": "新目录", "delete": true}, false)
	call(t, c, "knowledge.jobs.start", map[string]any{"requestId": "attachments", "libraryId": lib.ID, "attachmentIndexes": []int{0}}, true)
	accepted := getReceipt(t, call(t, c, "knowledge.jobs.start", map[string]any{"requestId": "job", "libraryId": lib.ID, "sourceVersionIds": []string{imported.ResourceID}}, false))
	call(t, c, "knowledge.jobs.get", map[string]any{"libraryId": lib.ID, "jobId": accepted.JobID}, false)
	job, _ := f.engine.GetJob(ctx, accepted.JobID)
	if _, e = f.engine.FailOrganize(ctx, job.ID, job.Attempt, "fixture failure"); e != nil {
		t.Fatal(e)
	}
	job, _ = f.engine.GetJob(ctx, job.ID)
	retry := map[string]any{"requestId": "retry", "libraryId": lib.ID, "jobId": job.ID, "expectedVersion": job.Version}
	call(t, c, "knowledge.jobs.retry", retry, false)
	call(t, c, "knowledge.jobs.retry", retry, false)
	job, _ = f.engine.GetJob(ctx, job.ID)
	if job.Attempt != 2 {
		t.Fatal(job)
	}
	found := false
	for _, binding := range f.service.AgentBindings(ctx, models.Chat{}) {
		if binding.Operation == "knowledge.pages.get" {
			found = true
			if _, e := binding.Call(ctx, agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "pageId": page.ID}}); e != nil {
				t.Fatal(e)
			}
		}
	}
	if !found {
		t.Fatal("internal read binding missing")
	}
}

func TestHTTPAuthenticationStatelessAndCapabilities(t *testing.T) {
	f := newFixture(t)
	for _, test := range []struct {
		method string // HTTP 方法。
		token  string // 边界凭据。
		origin string // 浏览器来源。
		status int    // 预期状态码。
	}{{"GET", "", "", 401}, {"GET", "all", "https://evil.example", 403}, {"GET", "all", "", 405}, {"DELETE", "all", "", 405}} {
		r, _ := http.NewRequest(test.method, f.server.URL+"/mcp", nil)
		if test.token != "" {
			r.Header.Set("X-API-Key", test.token)
		}
		r.Header.Set("Origin", test.origin)
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatal(test, response.StatusCode)
		}
		if response.Header.Get("Mcp-Session-Id") != "" {
			t.Fatal("session leak")
		}
	}
	r, _ := http.NewRequest("POST", f.server.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	r.Header.Set("X-API-Key", "all")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatal(response.StatusCode)
	}
	read := f.client(t, "read")
	ingest := f.client(t, "ingest")
	organize := f.client(t, "organize")
	id := f.library.ID
	call(t, read, "knowledge.sources.import", map[string]any{"requestId": "denied", "libraryId": id, "filename": "x.md", "bytes": "eA=="}, true)
	call(t, ingest, "knowledge.jobs.start", map[string]any{"requestId": "denied", "libraryId": id}, true)
	call(t, organize, "knowledge.sources.import", map[string]any{"requestId": "denied", "libraryId": id, "filename": "x.md", "bytes": "eA=="}, true)
	call(t, read, "knowledge.libraries.list", map[string]any{"requestId": "unexpected"}, true)
	call(t, read, "knowledge.pages.get", map[string]any{"libraryId": id, "pageId": "missing", "backend": "custom", "connectionId": "remote"}, true)
	ctx := context.Background()
	foreignPage, e := f.engine.WritePage(ctx, core.WritePageInput{RequestID: "foreign-page", LibraryID: f.other.ID, StructureVersion: f.other.StructureVersion, Page: core.Page{Slug: "secret", Title: "秘密", PageType: "concept", Content: "秘密正文"}})
	if e != nil {
		t.Fatal(e)
	}
	foreignSource, e := f.engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "foreign-source", LibraryID: f.other.ID, Filename: "secret.md", Bytes: []byte("秘密原件")})
	if e != nil {
		t.Fatal(e)
	}
	foreignJob, _, e := f.service.Organize(ctx, knowledgeservice.OrganizeCommand{RequestID: "foreign-job", LibraryID: f.other.ID, SourceVersionIDs: []string{foreignSource.ResourceID}, ExplicitSources: true})
	if e != nil {
		t.Fatal(e)
	}
	call(t, read, "knowledge.libraries.get", map[string]any{"libraryId": f.other.ID}, true)
	call(t, read, "knowledge.pages.get", map[string]any{"libraryId": id, "pageId": foreignPage.ResourceID}, true)
	call(t, read, "knowledge.sources.get", map[string]any{"libraryId": id, "sourceVersionId": foreignSource.ResourceID}, true)
	call(t, read, "knowledge.jobs.get", map[string]any{"libraryId": id, "jobId": foreignJob.JobID}, true)
	call(t, organize, "knowledge.jobs.retry", map[string]any{"requestId": "cross-job", "libraryId": id, "jobId": foreignJob.JobID, "expectedVersion": 1}, true)
	call(t, read, "knowledge.libraries.get", map[string]any{"libraryId": "outside"}, true)
	key, _ := f.keys.Get(context.Background(), "read")
	key.Enabled = false
	e = f.authority.WithAuthorizationWrite(context.Background(), func(ctx context.Context) error { _, e := f.keys.UpdateAuthorization(ctx, key, key.Revision); return e })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = read.CallTool(context.Background(), &sdk.CallToolParams{Name: "knowledge_libraries_list"}); e == nil {
		t.Fatal("revoked key accepted")
	}
}
