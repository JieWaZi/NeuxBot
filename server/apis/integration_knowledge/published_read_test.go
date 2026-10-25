package integration_knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	rules "github.com/JieWaZi/NeuxBot/server/internal/integration"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	integrationservice "github.com/JieWaZi/NeuxBot/server/services/integration"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func publishedFixture(t *testing.T, capabilities ...constants.IntegrationCapability) (*core.Engine, *core.Library, *integrationservice.Service, controllers.KnowledgeController) {
	t.Helper()
	root := t.TempDir()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(root, "app.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB.DB()
	t.Cleanup(func() { _ = pool.Close() })
	engine, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(root, "knowledge.db"), RawDir: filepath.Join(root, "raw")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	r, err := engine.CreateLibrary(context.Background(), core.CreateLibraryInput{RequestID: "create", Name: "授权库", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	lib, _ := engine.GetLibrary(context.Background(), r.ResourceID)
	_, _ = engine.CreateLibrary(context.Background(), core.CreateLibraryInput{RequestID: "other", Name: "不可见名称", Kind: "general", WikiLanguage: "zh-CN"})
	store := controllers.NewIntegrationKeyController(db.DB)
	_, _, _ = store.ClaimMutation(context.Background(), models.IntegrationKeyMutation{RequestID: "key", Digest: "fixture", KeyID: "key-a", Action: "create"})
	_, err = store.CompleteMutation(context.Background(), "key", "fixture", models.IntegrationKey{ID: "key-a", Name: "集成", KeyHash: rules.HashKey("fixture-only"), Enabled: true, Grants: []models.IntegrationGrant{{ResourceKind: constants.IntegrationResourceKindKnowledge, ResourceIDs: []string{lib.ID}, Capabilities: capabilities}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return engine, lib, integrationservice.New(integrationservice.Config{Keys: store}), controllers.NewKnowledgeController(db.DB)
}

func publishedRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("X-API-Key", "fixture-only")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestPublishedKnowledgeReadContract(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve)
	h := NewHandler(e, nil, auth, Options{References: refs})
	w := publishedRequest(h, "GET", "/api/v1/knowledge-bases/"+lib.ID)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"success":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = publishedRequest(h, "GET", "/api/v1/knowledge-bases")
	if w.Code != 200 || strings.Contains(w.Body.String(), "不可见名称") {
		t.Fatal("unauthorized collection", w.Code, w.Body.String())
	}
	var list struct {
		// Data 是经过授权过滤后的实际库集合。
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Data) != 1 {
		t.Fatal("wrong scoped total", w.Body.String())
	}
	w = publishedRequest(h, "GET", "/api/v1/knowledge-bases/not-authorized")
	if w.Code != 403 {
		t.Fatal("out-of-scope library allowed", w.Code)
	}
	w = publishedRequest(h, "GET", "/api/v1/wakers")
	if w.Code == 200 {
		t.Fatal("desktop management route exposed")
	}
}

func TestPublishedCapabilitiesSeparateSupportAndPermission(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve)
	w := publishedRequest(NewHandler(e, nil, auth, Options{References: refs}), "GET", "/api/v1/knowledge-bases/"+lib.ID+"/integration-capabilities")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		// ServiceKind 是后端声明的明确产品身份。
		ServiceKind string `json:"service_kind"`
		// UploadMode 是真实处理模式。
		UploadMode string `json:"upload_mode"`
		// Details 区分支持与实际授权。
		Details map[string]struct {
			// Supported 表示协议操作是否实现。
			Supported bool `json:"supported"`
			// Permission 是本 Key 实际授权。
			Permission string `json:"permission"`
		} `json:"details"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ServiceKind != "neuxally" || result.UploadMode != "separate" || !result.Details["rawOnly"].Supported || result.Details["rawOnly"].Permission != "denied" {
		t.Fatal(w.Body.String())
	}
}
