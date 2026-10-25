package knowledge_connections

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/gin-gonic/gin"
)

// connectionFixture 只替代连接配置仓库，远端请求使用真实HTTP。
type connectionFixture struct {
	// backend 是该测试固定的实际协议。
	backend string
	// address 指向测试HTTP服务器。
	address string
}

func (f connectionFixture) Get(context.Context, string) (models.KnowledgeConnection, error) {
	return models.KnowledgeConnection{ID: "connection", Backend: f.backend, BaseURL: f.address, AuthMode: "bearer", Enabled: true, Revision: 1}, nil
}
func (f connectionFixture) Snapshot(ctx context.Context, id string) (models.KnowledgeConnection, string, error) {
	connection, err := f.Get(ctx, id)
	return connection, "secret", err
}
func (f connectionFixture) Credential(context.Context, string) (string, error) { return "secret", nil }
func (f connectionFixture) List(context.Context) ([]models.KnowledgeConnection, error) {
	return nil, nil
}
func (f connectionFixture) Save(context.Context, controllers.SaveKnowledgeConnectionInput) (models.KnowledgeConnectionMutation, error) {
	return models.KnowledgeConnectionMutation{}, status_error.KnowledgeForbidden
}
func remoteContext(t *testing.T, backend string, handler http.HandlerFunc) *gin.Context {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	old := global.KnowledgeConnectionControllerManager
	global.KnowledgeConnectionControllerManager = connectionFixture{backend: backend, address: server.URL}
	t.Cleanup(func() { global.KnowledgeConnectionControllerManager = old })
	database, e := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "references.db"), Migrate: true})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, e := database.DB.DB()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldReferences := global.KnowledgeControllerManager
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(database.DB)
	t.Cleanup(func() { global.KnowledgeControllerManager = oldReferences })
	oldEngine := global.KnowledgeEngine
	global.KnowledgeEngine = nil
	t.Cleanup(func() { global.KnowledgeEngine = oldEngine })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	ctx.Set("testReferenceDB", database.DB)
	return ctx
}
func TestRemotePagesRetainServerPaginationAndReference(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/knowledge/v1/libraries/kb/pages" || r.URL.Query().Get("offset") != "50" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request %s", r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []*core.Page{{ID: "page", KnowledgeBaseID: "kb", Version: 3}}, "offset": 50, "limit": 50, "total": 51})
	})
	out, err := (&ListRemotePages{ConnectionID: "connection", LibraryID: "kb", Offset: 50, Limit: 50}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := out.(knowledge.PageList)
	if data.Total != 51 || data.Items[0].ID != "page" || calls != 1 {
		t.Fatal(data, calls)
	}
}
func TestOfficialPageUsesExplicitSlugAndKeepsStableID(t *testing.T) {
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledgebase/kb/wiki/pages/概念/身份" {
			t.Errorf("ID was used as slug: %s", r.URL)
		}
		w.Write([]byte(`{"id":"stable-id","knowledge_base_id":"kb","slug":"概念/身份","summary":"页面说明","content":"正文","version":3}`))
	})
	out, err := (&GetRemotePage{ConnectionID: "connection", LibraryID: "kb", Slug: "概念/身份", PageID: "stable-id"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page := out.(knowledge.Page)
	if page.ID != "stable-id" || page.Slug != "概念/身份" || page.Summary != "页面说明" {
		t.Fatal(page)
	}
}
func TestOfficialWriteIsUnsupportedWithoutHTTPRequest(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) { calls++ })
	_, err := (&WriteRemotePage{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
	if !errors.Is(err, status_error.KnowledgeUnsupported) || calls != 0 {
		t.Fatal(err, calls)
	}
}
func TestUnknownRemoteWriteIsSentOnceAndPreservesRequestID(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["requestId"] != "same-request" || body["profileId"] != "server-profile" {
			t.Errorf("unscoped request %v", body)
		}
		if _, ok := body["runtimeId"]; ok {
			t.Error("local runtime leaked")
		}
		w.WriteHeader(500)
	})
	_, err := (&OrganizeRemote{ConnectionID: "connection", LibraryID: "kb", Body: RemoteOrganizeInput{RequestID: "same-request", ExpectedVersion: 1, StructureVersion: 1, ProfileID: "server-profile"}}).Output(ctx)
	if !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestOfficialCapabilitiesDoNotPromiseCustomRulesOrWrites(t *testing.T) {
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
		t.Error("capability contract should not send credential probe")
	})
	out, err := (&GetRemoteCapabilities{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v := out.(RemoteCapabilities)
	if v.Operations["write"] || v.Operations["softwareRules"] || !v.Operations["read"] || v.Permissions["read"] != "unknown" || len(v.Profiles) != 0 {
		t.Fatal(v)
	}
}
func TestOfficialSlugCannotReturnAnotherStablePage(t *testing.T) {
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"another-id","knowledge_base_id":"kb","slug":"concept","content":"真实正文","version":3}`))
	})
	_, err := (&GetRemotePage{ConnectionID: "connection", LibraryID: "kb", Slug: "concept", PageID: "expected-id"}).Output(ctx)
	if !errors.Is(err, status_error.KnowledgeNotFound) {
		t.Fatal(err)
	}
}
func TestRemoteRestoreKeepsCASAndCreatesOnlyRemoteWrite(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/knowledge/v1/libraries/kb/pages/page/restore" {
			t.Error(r.URL)
		}
		var in core.RestorePageInput
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.LibraryID != "kb" || in.PageID != "page" || in.ExpectedVersion != 4 || in.RevisionVersion != 2 || in.StructureVersion != 9 {
			t.Errorf("CAS changed %+v", in)
		}
		json.NewEncoder(w).Encode(core.Receipt{RequestID: in.RequestID, LibraryID: "kb", ResourceID: "page", Operation: "page.restore", Version: 5, State: "saved"})
	})
	out, err := (&RestoreRemotePage{ConnectionID: "connection", LibraryID: "kb", PageID: "page", Body: knowledge.RestorePageInput{RequestID: "restore", ExpectedVersion: 4, RevisionVersion: 2, StructureVersion: 9}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out.(knowledge.Receipt).Version != 5 || calls != 1 {
		t.Fatal(out, calls)
	}
}

func TestRemoteWriteOpenAPIKeepsConcreteReceipt(t *testing.T) {
	raw, err := os.ReadFile("../../openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("invalid OpenAPI")
	}
	paths := doc["paths"].(map[string]any)
	for suffix, method := range map[string]string{"/maintenance": "post", "/organize": "post", "/sources": "post", "/themes": "post", "/pages/{pageId}": "put", "/pages/{pageId}/restore": "post"} {
		path := "/api/v1/knowledge/connections/{connectionId}/libraries/{libraryId}" + suffix
		operation := paths[path].(map[string]any)[method].(map[string]any)
		schema := operation["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		if len(schema) == 0 {
			t.Errorf("missing concrete receipt schema: %s %s", method, path)
		}
	}
}

func TestRemoteUnknownWriteReconcilesAfterRestartWithoutSecondPOST(t *testing.T) {
	writes, reads := 0, 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
			w.WriteHeader(500)
			return
		}
		reads++
		if r.URL.Path != "/api/knowledge/v1/libraries/kb/receipts" || r.URL.Query().Get("requestId") != "restart-request" {
			t.Error(r.URL)
		}
		json.NewEncoder(w).Encode(core.Receipt{RequestID: "restart-request", LibraryID: "kb", ResourceID: "job", JobID: "job", Operation: "job.accept", State: "accepted", Version: 1})
	})
	command := &OrganizeRemote{ConnectionID: "connection", LibraryID: "kb", Body: RemoteOrganizeInput{RequestID: "restart-request", ExpectedVersion: 1, StructureVersion: 1, ProfileID: "server-profile"}}
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatal(err)
	}
	db := ctx.MustGet("testReferenceDB").(*gorm.DB)
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(db)
	out, err := command.Output(ctx)
	if err != nil || out.(knowledge.Receipt).JobID != "job" || writes != 1 || reads != 1 {
		t.Fatal(out, err, writes, reads)
	}
	_, total, err := global.KnowledgeControllerManager.ListRemoteOperations(ctx, "custom", "connection", "kb", 0, 50)
	if err != nil || total != 0 {
		t.Fatal(total, err)
	}
	command.Body.ProfileID = "changed"
	if _, err = command.Output(ctx); !errors.Is(err, status_error.KnowledgeConflict) || writes != 1 {
		t.Fatal("same key changed input", err, writes)
	}
}

func TestRemoteMissingReceiptNeverResendsUnknownWrite(t *testing.T) {
	writes, reads := 0, 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
			w.WriteHeader(500)
			return
		}
		reads++
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"not found"}`))
	})
	command := &OrganizeRemote{ConnectionID: "connection", LibraryID: "kb", Body: RemoteOrganizeInput{RequestID: "missing-receipt", ExpectedVersion: 1, StructureVersion: 1, ProfileID: "server-profile"}}
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatal(err)
	}
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(ctx.MustGet("testReferenceDB").(*gorm.DB))
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) || writes != 1 || reads != 1 {
		t.Fatal("404 cannot authorize resend", err, writes, reads)
	}
	_, total, err := global.KnowledgeControllerManager.ListRemoteOperations(ctx, "custom", "connection", "kb", 0, 50)
	if err != nil || total != 1 {
		t.Fatal("unresolved operation must survive", total, err)
	}
}

func TestRemoteOrganizePreservesExplicitRebuildAndServerProfile(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.Method != "POST" || r.URL.Path != "/api/knowledge/v1/libraries/kb/organize" || body["rebuild"] != true || body["profileId"] != "server" || body["instruction"] != "保留边界与来源" {
			t.Errorf("rebuild contract changed: %s %s %+v", r.Method, r.URL, body)
		}
		if _, ok := body["runtimeId"]; ok {
			t.Error("local execution configuration leaked")
		}
		_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: "rebuild", LibraryID: "kb", ResourceID: "job", JobID: "job", Operation: "job.accept", State: "accepted"})
	})
	var body RemoteOrganizeInput
	if err := json.Unmarshal([]byte(`{"requestId":"rebuild","expectedVersion":2,"structureVersion":3,"sourceVersionIds":["source"],"profileId":"server","rebuild":true,"instruction":"保留边界与来源"}`), &body); err != nil {
		t.Fatal(err)
	}
	if _, err := (&OrganizeRemote{ConnectionID: "connection", LibraryID: "kb", Body: body}).Output(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestOfficialRevisionRetainsSummaryForComparison 验证官方历史正文不丢失页面说明。
func TestOfficialRevisionRetainsSummaryForComparison(t *testing.T) {
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledgebase/kb/wiki/revisions/concept/one" || r.URL.Query().Get("content_version") != "2" {
			t.Errorf("unexpected revision request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"revisions":[{"id":"r2","page_id":"page","version":2,"title":"旧标题","summary":"旧说明","content":"旧正文"}],"total":1,"current_version":3}`))
	})
	out, err := (&GetRemoteRevision{ConnectionID: "connection", LibraryID: "kb", PageID: "page", Slug: "concept/one", Version: 2}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := out.(knowledge.Revision)
	if data.Summary != "旧说明" || data.Content != "旧正文" {
		t.Fatal(data)
	}
}

// TestCustomRevisionRetainsSummaryForComparison 验证自有远端保留历史说明。
func TestCustomRevisionRetainsSummaryForComparison(t *testing.T) {
	snapshot := &core.Revision{Summary: "旧说明", Content: "旧正文"}
	if got := customRevision(snapshot); got.Summary != snapshot.Summary || got.Content != snapshot.Content {
		t.Fatal(got)
	}
}
