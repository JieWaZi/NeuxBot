package integration_knowledge

import (
	"encoding/json"
	"github.com/JieWaZi/neux-knowledge/remote"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestPublishedPageDirectoryIsLightScopedAndKeepsDetailSeparate(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve)
	body := strings.Repeat("正文只在详情出现", 1000)
	receipt, err := e.WritePage(t.Context(), core.WritePageInput{RequestID: "directory-page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/page", Title: "目录页面", Summary: "简介", PageType: "concept", Content: body}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(e, nil, auth, Options{References: refs})
	path := "/api/v1/knowledgebase/" + lib.ID + "/wiki/page-directory"
	w := publishedRequest(h, "GET", path+"?limit=1000")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		// Items 是轻量目录。
		Items []map[string]json.RawMessage `json:"items"`
		// Total 是真实条数。
		Total int64 `json:"total"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Total != 1 || len(result.Items) != 1 {
		t.Fatal(w.Body.String(), err)
	}
	row := result.Items[0]
	for _, field := range []string{"content", "evidence", "source_refs", "chunk_refs", "in_links", "out_links", "page_metadata"} {
		if _, ok := row[field]; ok {
			t.Fatalf("heavy field %s in %s", field, w.Body.String())
		}
	}
	if string(row["id"]) != `"`+receipt.ResourceID+`"` || string(row["summary"]) != `"简介"` {
		t.Fatal(w.Body.String())
	}
	detail := publishedRequest(h, "GET", "/api/v1/knowledgebase/"+lib.ID+"/wiki/page-by-id/"+receipt.ResourceID)
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), body) {
		t.Fatal("detail missing", detail.Code)
	}
	if w = publishedRequest(h, "GET", strings.Replace(path, lib.ID, "foreign", 1)); w.Code == 200 {
		t.Fatal("foreign library allowed")
	}
	if w = publishedRequest(h, "GET", path+"?limit=1001"); w.Code != 400 {
		t.Fatal("unbounded directory", w.Code)
	}
}

func TestPublishedPageDirectorySDKRoundTrip(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve)
	_, err := e.WritePage(t.Context(), core.WritePageInput{RequestID: "directory-sdk", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/sdk", Title: "SDK 目录", PageType: "concept", Content: "正文"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(e, nil, auth, Options{References: refs}))
	defer server.Close()
	c, err := remote.NewIntegration(remote.Options{BaseURL: server.URL + "/api/v1", Token: "fixture-only"}, remote.ResourceRef{Backend: "integration", ConnectionID: "c", KnowledgeBaseID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.ListPageDirectory(t.Context(), 0, 1000)
	if err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].Title != "SDK 目录" {
		t.Fatal(result, err)
	}
}
