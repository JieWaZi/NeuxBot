package knowledge_connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// catalogConnections 只替换连接仓库，聚合窗口使用真实知识引擎和远端HTTP。
type catalogConnections struct {
	// KnowledgeConnectionController 保留本测试不使用的写接口。
	controllers.KnowledgeConnectionController
	// rows 是权威连接列表。
	rows []models.KnowledgeConnection
	// failure 模拟连接列表读取失败。
	failure error
}

func searchCatalogForTest(t *testing.T, ctx *gin.Context, location string, offset, limit int, search string) knowledge.LibraryList {
	t.Helper()
	ctx.Request = httptest.NewRequest("GET", "/?search="+url.QueryEscape(search), nil)
	op := &ListAvailableLibraries{Location: constants.KnowledgeLibraryLocation(location), Offset: offset, Limit: limit}
	if err := ginx.ParameterBinding(ctx, op, ginx.GetOperatorTypeInfo(reflect.TypeOf(op))); err != nil {
		t.Fatal(err)
	}
	raw, err := op.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return raw.(knowledge.LibraryList)
}

func TestAvailableLibrariesSearchBeforePaginationAcrossLocalPages(t *testing.T) {
	ctx := catalogContext(t)
	for i := 0; i < 121; i++ {
		purpose := "一般资料"
		if i == 5 || i == 120 {
			purpose = "DNS 中文部署说明"
		}
		if _, err := global.KnowledgeEngine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: fmt.Sprint("search-", i), Name: fmt.Sprintf("资料-%02d", i), Purpose: purpose, Kind: "general", WikiLanguage: "zh-CN"}); err != nil {
			t.Fatal(err)
		}
	}
	first := searchCatalogForTest(t, ctx, "local", 0, 1, "  dns 中文  ")
	second := searchCatalogForTest(t, ctx, "local", 1, 1, "dns 中文")
	if first.Total != 2 || second.Total != 2 || len(first.Items) != 1 || len(second.Items) != 1 || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("search did not paginate the full matching catalog: first=%+v second=%+v", first, second)
	}
	if out := searchCatalogForTest(t, ctx, "local", 0, 10, "不存在的资料"); out.Total != 0 || len(out.Items) != 0 {
		t.Fatalf("unmatched search returned libraries: %+v", out)
	}
}

func TestAvailableLibrariesSearchUsesMatchingCountsAcrossSources(t *testing.T) {
	ctx := catalogContext(t)
	for i, name := range []string{"普通一", "普通二", "匹配资料"} {
		if _, err := global.KnowledgeEngine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: fmt.Sprint("mixed-search-", i), Name: name, Kind: "general", WikiLanguage: "zh-CN"}); err != nil {
			t.Fatal(err)
		}
	}
	calls := [][2]int{}
	global.KnowledgeConnectionControllerManager = catalogConnections{rows: []models.KnowledgeConnection{{ID: "mixed-search", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: catalogServer(t, 2, "匹配远端", &calls)}}}
	out := searchCatalogForTest(t, ctx, "all", 1, 1, "匹配")
	if out.Total != 3 || len(out.Items) != 1 || out.Items[0].Ref.ConnectionID != "mixed-search" || out.Items[0].ID != "0" {
		t.Fatalf("global offset used unfiltered source totals: %+v", out)
	}
}

func TestAvailableLibrariesSearchScansRemoteMetadataInBoundedPages(t *testing.T) {
	ctx := catalogContext(t)
	calls := [][2]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		calls = append(calls, [2]int{offset, limit})
		items := []*core.Library{}
		for i := offset; i < min(offset+limit, 205); i++ {
			purpose := "普通资料"
			if i == 150 || i == 204 {
				purpose = "跨页目标"
			}
			items = append(items, &core.Library{ID: fmt.Sprint(i), Name: fmt.Sprint("远端-", i), Purpose: purpose, Kind: "general"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": 205, "offset": offset, "limit": limit})
	}))
	defer server.Close()
	global.KnowledgeConnectionControllerManager = catalogConnections{rows: []models.KnowledgeConnection{{ID: "remote-search", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: server.URL}}}
	out := searchCatalogForTest(t, ctx, "remote", 1, 1, "跨页目标")
	if out.Total != 2 || len(out.Items) != 1 || out.Items[0].ID != "204" || out.Items[0].Ref.ConnectionID != "remote-search" {
		t.Fatalf("search lost remote matches beyond the first page: %+v", out)
	}
	for _, call := range calls {
		if call[1] > 100 {
			t.Fatalf("unbounded catalog request: %v", calls)
		}
	}
	if len(calls) > 5 {
		t.Fatalf("catalog search performed per-library requests: %v", calls)
	}
}

func TestAvailableLibrariesSearchRecountsOfflineFallbackAfterPartialRemoteRead(t *testing.T) {
	ctx := catalogContext(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if offset > 0 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		items := []*core.Library{}
		for i := 0; i < limit; i++ {
			items = append(items, &core.Library{ID: fmt.Sprint(i), Name: "匹配在线", Kind: "general"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": 150, "offset": offset, "limit": limit})
	}))
	defer server.Close()
	global.KnowledgeConnectionControllerManager = catalogConnections{rows: []models.KnowledgeConnection{{ID: "offline-search", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: server.URL, Detection: remote.Detection{Library: remote.Library{ID: "known", Name: "匹配离线"}}}}}
	out := searchCatalogForTest(t, ctx, "remote", 0, 10, "匹配")
	if out.Total != 1 || len(out.Items) != 1 || out.Items[0].ID != "known" || !out.Items[0].Offline {
		t.Fatalf("partial online search leaked into offline fallback: %+v", out)
	}
}

func (f catalogConnections) List(context.Context) ([]models.KnowledgeConnection, error) {
	return f.rows, f.failure
}
func (f catalogConnections) Snapshot(_ context.Context, id string) (models.KnowledgeConnection, string, error) {
	for _, v := range f.rows {
		if v.ID == id {
			return v, "secret", nil
		}
	}
	return models.KnowledgeConnection{}, "", status_error.KnowledgeNotFound
}
func catalogContext(t *testing.T) *gin.Context {
	t.Helper()
	oldEngine, oldConnections, oldSummaries := global.KnowledgeEngine, global.KnowledgeConnectionControllerManager, global.KnowledgeControllerManager
	engine, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	global.KnowledgeEngine = engine
	global.KnowledgeControllerManager = nil
	global.KnowledgeConnectionControllerManager = catalogConnections{}
	t.Cleanup(func() {
		engine.Close()
		global.KnowledgeEngine = oldEngine
		global.KnowledgeConnectionControllerManager = oldConnections
		global.KnowledgeControllerManager = oldSummaries
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)
	return ctx
}
func catalogServer(t *testing.T, total int, prefix string, calls *[][2]int) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		*calls = append(*calls, [2]int{offset, limit})
		if r.URL.Path != "/api/knowledge/v1/libraries" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request %s", r.URL)
		}
		items := []*core.Library{}
		for i := offset; i < min(offset+limit, total); i++ {
			items = append(items, &core.Library{ID: fmt.Sprint(i), Name: prefix, Kind: "general"})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items, "total": total, "offset": offset, "limit": limit})
	}))
	t.Cleanup(server.Close)
	return server.URL
}
func TestAvailableLibrariesUsesBoundedGlobalWindowAndFullReferences(t *testing.T) {
	ctx := catalogContext(t)
	for i := 0; i < 3; i++ {
		if _, err := global.KnowledgeEngine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: fmt.Sprint("local", i), Name: "本地", Kind: "general", WikiLanguage: "zh-CN"}); err != nil {
			t.Fatal(err)
		}
	}
	a, b := [][2]int{}, [][2]int{}
	first := models.KnowledgeConnection{ID: "a", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: catalogServer(t, 70, "重名", &a)}
	second := models.KnowledgeConnection{ID: "b", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: catalogServer(t, 2, "重名", &b)}
	global.KnowledgeConnectionControllerManager = catalogConnections{rows: []models.KnowledgeConnection{second, first, {ID: "disabled", Enabled: false}}}
	raw, err := (&ListAvailableLibraries{Location: "all", Offset: 2, Limit: 4}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := raw.(knowledge.LibraryList)
	if out.Total != 75 || len(out.Items) != 4 || out.Items[0].Ref.Backend != "local" || out.Items[1].Ref.ConnectionID != "a" {
		t.Fatalf("wrong aggregate: %+v", out)
	}
	if fmt.Sprint(a) != "[[0 1] [0 3]]" || fmt.Sprint(b) != "[[0 1]]" {
		t.Fatalf("unbounded or incorrect reads: %v %v", a, b)
	}
	raw, err = (&ListAvailableLibraries{Location: "remote", Offset: 69, Limit: 3}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out = raw.(knowledge.LibraryList)
	if out.Total != 72 || len(out.Items) != 3 || out.Items[0].Ref.ConnectionID != "a" || out.Items[1].Ref.ConnectionID != "b" {
		t.Fatalf("lost source identity: %+v", out)
	}
	raw, err = (&ListAvailableLibraries{Location: "remote", Offset: 0, Limit: 100}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out = raw.(knowledge.LibraryList)
	if len(out.Items) != 72 || out.Items[0].ID != out.Items[70].ID || out.Items[0].Ref.ConnectionID == out.Items[70].Ref.ConnectionID {
		t.Fatalf("same ID across connections was merged: %+v", out)
	}

}
func TestAvailableLibrariesKeepsDatabaseFailuresAndIsolatesRemoteFailures(t *testing.T) {
	ctx := catalogContext(t)
	global.KnowledgeConnectionControllerManager = catalogConnections{failure: status_error.DatabaseError}
	raw, err := (&ListAvailableLibraries{Location: "local", Limit: 10}).Output(ctx)
	if err != nil || raw.(knowledge.LibraryList).Total != 0 {
		t.Fatal(raw, err)
	}
	if _, err := (&ListAvailableLibraries{Location: "all", Limit: 10}).Output(ctx); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("connection failure became empty: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	global.KnowledgeConnectionControllerManager = catalogConnections{rows: []models.KnowledgeConnection{{ID: "broken", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: server.URL}}}
	global.KnowledgeEngine = nil
	raw, err = (&ListAvailableLibraries{Location: "remote", Limit: 10}).Output(ctx)
	if err != nil || raw.(knowledge.LibraryList).Total != 0 {
		t.Fatalf("remote failure blocked catalog or touched local: %v %v", raw, err)
	}
}

func TestAvailableLibrariesStaticRouteCoexistsWithConnectionRoutes(t *testing.T) {
	router := gin.New()
	group := router.Group(Router.BasePath())
	group.GET((&ListAvailableLibraries{}).Path(), func(ctx *gin.Context) { ctx.String(http.StatusOK, "catalog") })
	group.GET((&ListRemoteLibraries{}).Path(), func(ctx *gin.Context) { ctx.String(http.StatusOK, ctx.Param("connectionId")) })
	group.GET((&GetRemoteLibrary{}).Path(), func(ctx *gin.Context) { ctx.String(http.StatusOK, ctx.Param("libraryId")) })
	for path, want := range map[string]string{
		"/knowledge/connections/libraries":                    "catalog",
		"/knowledge/connections/connection/libraries":         "connection",
		"/knowledge/connections/connection/libraries/library": "library",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != http.StatusOK || response.Body.String() != want {
			t.Fatalf("route %s: %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestAvailableLibrariesRemoteFailureKeepsLocalAndOtherConnections(t *testing.T) {
	for _, failWindow := range []bool{false, true} {
		t.Run(fmt.Sprint("window=", failWindow), func(t *testing.T) {
			ctx := catalogContext(t)
			if _, err := global.KnowledgeEngine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "local", Name: "本地", Kind: "general", WikiLanguage: "zh-CN"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if failWindow && calls == 1 {
					json.NewEncoder(w).Encode(map[string]any{"items": []*core.Library{{ID: "a", Name: "远端"}}, "total": 20, "offset": 0, "limit": 1})
					return
				}
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer broken.Close()
			healthyCalls := [][2]int{}
			global.KnowledgeConnectionControllerManager = catalogConnections{rows: []models.KnowledgeConnection{
				{ID: "a", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: broken.URL, Detection: remote.Detection{Library: remote.Library{ID: "a", Name: "已知远端"}}},
				{ID: "b", Backend: "custom", Enabled: true, AuthMode: "bearer", BaseURL: catalogServer(t, 2, "正常远端", &healthyCalls)},
			}}
			raw, err := (&ListAvailableLibraries{Location: "all", Limit: 10}).Output(ctx)
			if err != nil {
				t.Fatal(err)
			}
			out := raw.(knowledge.LibraryList)
			if out.Total != 4 || len(out.Items) != 4 || out.Items[0].Ref.Backend != "local" || !out.Items[1].Offline || out.Items[1].Name != "已知远端" || out.Items[2].Ref.ConnectionID != "b" || out.Items[2].Offline {
				t.Fatalf("remote failure changed unrelated libraries or offline identity: %+v", out)
			}
		})
	}
}
