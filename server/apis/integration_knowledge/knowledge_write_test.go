package integration_knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/neux-knowledge/core"
)

func uploadPublished(t *testing.T, handler http.Handler, libraryID, requestID, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "example.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte(content))
	_ = form.Close()
	r := httptest.NewRequest("POST", "/api/v1/knowledge-bases/"+libraryID+"/knowledge/file", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-API-Key", "fixture-only")
	r.Header.Set("Idempotency-Key", requestID)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestPublishedUploadOnlySavesRawAndReplaysOriginalReceipt(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityIngest)
	h := NewHandler(e, nil, auth, Options{References: refs})
	w := uploadPublished(t, h, lib.ID, "file-a", "# immutable original")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result SourceResponse
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.ID == "" || result.Receipt == nil || result.Receipt.RequestID != "file-a" {
		t.Fatal(w.Body.String())
	}
	raw, err := e.ReadRaw(context.Background(), lib.ID, result.Data.ID)
	if err != nil || string(raw) != "# immutable original" {
		t.Fatal(string(raw), err)
	}
	_, total, err := e.ListJobs(context.Background(), lib.ID, 0, 10)
	if err != nil || total != 0 {
		t.Fatal("upload started organization", total, err)
	}
	w = uploadPublished(t, h, lib.ID, "file-a", "# immutable original")
	if w.Code != 200 || !strings.Contains(w.Body.String(), result.Data.ID) {
		t.Fatal("same request did not replay", w.Code, w.Body.String())
	}
	w = uploadPublished(t, h, lib.ID, "file-a", "different content")
	if w.Code != 409 {
		t.Fatal("same request accepted different original", w.Code)
	}
}

func TestPublishedWikiCASRetainsReadVersionToCommit(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityIngest)
	_, err := e.WritePage(context.Background(), core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{KnowledgeBaseID: lib.ID, Slug: "concept/example", Title: "正文", Content: "original", PageType: "concept"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(e, nil, auth, Options{References: refs})
	for i, content := range []string{"first edit", "first edit", "stale edit"} {
		body := `{"version":1,"content":"` + content + `"}`
		r := httptest.NewRequest("PUT", "/api/v1/knowledgebase/"+lib.ID+"/wiki/pages/concept/example", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-API-Key", "fixture-only")
		r.Header.Set("Idempotency-Key", content)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if i == 2 {
			want = 409
		}
		if w.Code != want {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
	page, err := e.GetPageBySlug(context.Background(), lib.ID, "concept/example")
	if err != nil || page.Content != "first edit" || page.Version != 2 {
		t.Fatal(page, err)
	}
}

func TestPublishedOrganizeDoesNotGrantArbitraryEditing(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityOrganize)
	w := uploadPublished(t, NewHandler(e, nil, auth, Options{References: refs}), lib.ID, "file-a", "original")
	if w.Code != 403 {
		t.Fatal("organize scope became ingest", w.Code, w.Body.String())
	}
}
