package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
	"github.com/JieWaZi/neux-knowledge/remote"
)

func readFixture(t *testing.T) (*core.Engine, *core.Library, *core.Page, *core.SourceVersion, *core.Job) {
	t.Helper()
	ctx := context.Background()
	e, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	r, err := e.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "Read fixture", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := e.GetLibrary(ctx, r.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = e.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{KnowledgeBaseID: lib.ID, Slug: "concept/read", Title: "Read", PageType: "concept", Status: "published", Content: "Stored wiki body"}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.GetPage(ctx, lib.ID, r.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	lib, _ = e.GetLibrary(ctx, lib.ID)
	updated := *page
	updated.Content = "Updated stored wiki body"
	if _, err = e.WritePage(ctx, core.WritePageInput{RequestID: "page-update", LibraryID: lib.ID, ExpectedVersion: page.Version, StructureVersion: lib.StructureVersion, Page: updated}); err != nil {
		t.Fatal(err)
	}
	page, _ = e.GetPage(ctx, lib.ID, page.ID)
	raw := []byte("# Stored source\n\nFirst persisted paragraph.\n\nSecond persisted paragraph.")
	r, err = e.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: raw})
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.GetSource(ctx, lib.ID, r.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	lib, _ = e.GetLibrary(ctx, lib.ID)
	r, err = e.AcceptJob(ctx, core.AcceptJobInput{RequestID: "job", LibraryID: lib.ID, ExpectedVersion: lib.Version, StructureVersion: lib.StructureVersion, SourceVersionIDs: []string{source.ID}, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "test", ConfigDigest: "test"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := e.GetJob(ctx, r.JobID)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := (core.MarkdownReader{}).Read(ctx, core.ReadRequest{SourceID: source.SourceID, SourceVersionID: source.ID, SHA256: source.SHA256, Filename: source.Filename, Raw: raw, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.IndexDocument(ctx, job.ID, source.ID, artifact, core.DocumentEmbedder{}); err != nil {
		t.Fatal(err)
	}
	return e, lib, page, source, job
}

func callRead(t *testing.T, bindings []agentcapability.Binding, operation string, params map[string]any) (agentcapability.Result, error) {
	t.Helper()
	for _, b := range bindings {
		if b.Operation == operation {
			return b.Call(context.Background(), agentcapability.Request{Params: params})
		}
	}
	t.Fatalf("missing read binding %s", operation)
	return agentcapability.Result{}, nil
}

func TestReadPageAcceptsExactlyOneIdentityAndResolvesSlug(t *testing.T) {
	e, lib, page, _, _ := readFixture(t)
	ref := LocalRef(lib.ID)
	bindings := ReadBindings(e, FreezeScope("", []ResourceRef{ref}), func(context.Context, string) ([]ResourceRef, error) { return []ResourceRef{ref}, nil })
	result, err := callRead(t, bindings, "knowledge.pages.get", map[string]any{"libraryId": lib.ID, "slug": page.Slug})
	if err != nil || result.Data.(map[string]any)["page"].(*core.Page).ID != page.ID {
		t.Fatalf("slug read result=%+v error=%v", result, err)
	}
	for _, params := range []map[string]any{{"libraryId": lib.ID}, {"libraryId": lib.ID, "pageId": page.ID, "slug": page.Slug}} {
		if _, err = callRead(t, bindings, "knowledge.pages.get", params); !errors.Is(err, status_error.KnowledgeInvalid) {
			t.Fatalf("ambiguous identity error=%v", err)
		}
	}
}

func TestPageListReturnsDirectoryMetadataAndKeepsBodyForExplicitRead(t *testing.T) {
	e, lib, page, _, _ := readFixture(t)
	ref := LocalRef(lib.ID)
	for _, query := range []string{"", "Read"} {
		data, err := (LocalReadProvider{Engine: e}).ExtendedRead(context.Background(), ref, "knowledge.pages.list", map[string]any{"query": query, "offset": 0, "limit": 100})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{`"content"`, `"evidence"`, `"chunk_refs"`, `"in_links"`, `"out_links"`} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("list leaked detail field %s", forbidden)
			}
		}
		if !strings.Contains(string(raw), page.ID) || data["total"] != int64(1) || data["nextOffset"] != 1 || data["hasMore"] != false {
			t.Fatalf("lost directory identity or pagination: %s", raw)
		}
	}
	got, err := (LocalReadProvider{Engine: e}).Page(context.Background(), ref, page.ID, "")
	if err != nil || got.Content != page.Content {
		t.Fatalf("explicit body read changed: %v", err)
	}
}

func TestDirectoryReadParametersBoundNativeToolOutput(t *testing.T) {
	for operation, limits := range map[string][2]int{"knowledge.pages.list": {10, 20}, "knowledge.index.get": {5, 10}} {
		defaults, err := readParams(operation, map[string]any{})
		if err != nil || defaults["limit"] != limits[0] {
			t.Fatalf("%s default limit=%v err=%v", operation, defaults["limit"], err)
		}
		if _, err := readParams(operation, map[string]any{"limit": limits[1]}); err != nil {
			t.Fatalf("%s maximum rejected: %v", operation, err)
		}
		if _, err := readParams(operation, map[string]any{"limit": limits[1] + 1}); !errors.Is(err, status_error.KnowledgeInvalid) {
			t.Fatalf("%s oversized response accepted: %v", operation, err)
		}
	}
}

func TestExtendedReadsUsePersistentContentAndRejectCrossLibraryJobs(t *testing.T) {
	e, lib, page, source, job := readFixture(t)
	ref := LocalRef(lib.ID)
	current := []ResourceRef{ref}
	bindings := ReadBindings(e, FreezeScope("", current), func(context.Context, string) ([]ResourceRef, error) { return current, nil })
	for _, op := range []string{"knowledge.libraries.get", "knowledge.sources.list", "knowledge.pages.list", "knowledge.themes.list", "knowledge.index.get", "knowledge.revisions.list", "knowledge.revisions.get", "knowledge.jobs.list", "knowledge.audit.get", "knowledge.activity.list", "knowledge.export.get"} {
		if _, err := callRead(t, bindings, op, map[string]any{"libraryId": lib.ID, "pageId": page.ID, "version": 1}); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	result, err := callRead(t, bindings, "knowledge.sources.read", map[string]any{"libraryId": lib.ID, "sourceVersionId": source.ID, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	chunks := data["items"].([]core.IndexedDocumentChunk)
	if len(chunks) != 1 || chunks[0].SourceVersionID != source.ID || chunks[0].Locator.Kind != "line" || chunks[0].Content == "" {
		t.Fatalf("lost stored evidence %+v", data)
	}
	result, err = callRead(t, bindings, "knowledge.jobs.get", map[string]any{"libraryId": lib.ID, "jobId": job.ID})
	if err != nil || result.Data.(map[string]any)["job"].(*core.Job).State != "queued" {
		t.Fatalf("acceptance became completion: %+v %v", result, err)
	}
	other, err := e.CreateLibrary(context.Background(), core.CreateLibraryInput{RequestID: "other", Name: "Other", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	otherRef := LocalRef(other.ResourceID)
	otherBindings := ReadBindings(e, FreezeScope("", []ResourceRef{otherRef}), func(context.Context, string) ([]ResourceRef, error) { return []ResourceRef{otherRef}, nil })
	if _, err = callRead(t, otherBindings, "knowledge.jobs.get", map[string]any{"libraryId": other.ResourceID, "jobId": job.ID}); !errors.Is(err, status_error.KnowledgeNotFound) {
		t.Fatalf("cross-library job escaped: %v", err)
	}
	for _, bad := range []any{-1, 0.5, "2", 101} {
		if _, err = callRead(t, bindings, "knowledge.sources.list", map[string]any{"libraryId": lib.ID, "limit": bad}); !errors.Is(err, status_error.KnowledgeInvalid) {
			t.Fatalf("invalid page size %v: %v", bad, err)
		}
	}
	current = nil
	if _, err = callRead(t, bindings, "knowledge.sources.read", map[string]any{"libraryId": lib.ID, "sourceVersionId": source.ID}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("revoked read escaped: %v", err)
	}
}

func TestCustomExtendedReadsUseRealHTTPAuthority(t *testing.T) {
	e, lib, page, source, job := readFixture(t)
	policy, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"token": {Subject: "reader", Libraries: map[string]httpapi.LibraryGrant{lib.ID: {PolicyVersion: lib.PolicyVersion, Operations: []string{"read", "write"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	clientHTTP := readHTTPClient(httpapi.New(e, httpapi.Options{Security: policy}))
	ref := ResourceRef{Backend: "custom", ConnectionID: "remote", KnowledgeBaseID: lib.ID}
	client, err := remote.NewCustom(remote.Options{BaseURL: "http://knowledge.test", AuthMode: "bearer", Token: "token", HTTPClient: clientHTTP}, remote.ResourceRef{Backend: "custom", ConnectionID: "remote", KnowledgeBaseID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	bindings := ProviderReadBindings(RemoteReadProvider{Custom: client}, FreezeScope("", []ResourceRef{ref}), func(context.Context, string) ([]ResourceRef, error) { return []ResourceRef{ref}, nil })
	for _, op := range []string{"knowledge.libraries.get", "knowledge.sources.list", "knowledge.sources.read", "knowledge.pages.list", "knowledge.pages.get", "knowledge.themes.list", "knowledge.revisions.list", "knowledge.revisions.get", "knowledge.jobs.list", "knowledge.jobs.get", "knowledge.audit.get", "knowledge.activity.list", "knowledge.export.get"} {
		params := map[string]any{"backend": "custom", "connectionId": "remote", "libraryId": lib.ID, "sourceVersionId": source.ID, "jobId": job.ID, "pageId": page.ID, "version": 1}
		if op == "knowledge.pages.get" {
			delete(params, "pageId")
			params["slug"] = page.Slug
		}
		if _, err = callRead(t, bindings, op, params); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	if _, err = callRead(t, bindings, "knowledge.index.get", map[string]any{"backend": "custom", "connectionId": "remote", "libraryId": lib.ID}); !errors.Is(err, status_error.KnowledgeUnsupported) {
		t.Fatalf("unsupported remote index fell back: %v", err)
	}
}

func TestSourceChunkReadPaginatesAndBoundsUTF8WithoutChangingEvidence(t *testing.T) {
	ref := LocalRef("library")
	source := &core.SourceVersion{ID: "version", LibraryID: "library", SourceID: "source", Version: 2}
	rows := []core.IndexedDocumentChunk{}
	for _, id := range []string{"first", "second", "third"} {
		rows = append(rows, core.IndexedDocumentChunk{ID: id, ChunkID: id, LibraryID: "library", SourceVersionID: "version", Content: strings.Repeat("原", 14000), Locator: core.SourceLocator{Kind: "line", StartLine: 2, EndLine: 3}})
	}
	data, err := sourceChunks(ref, source, rows, map[string]any{"sourceVersionId": "version", "offset": 0, "limit": 3})
	if err != nil {
		t.Fatal(err)
	}
	items := data["items"].([]core.IndexedDocumentChunk)
	if len(items) != 2 || !data["truncated"].(bool) || !data["hasMore"].(bool) || data["nextOffset"] != 2 || data["total"] != 3 {
		t.Fatalf("wrong continuation items=%d next=%v total=%v truncated=%v hasMore=%v", len(items), data["nextOffset"], data["total"], data["truncated"], data["hasMore"])
	}
	if !utf8.ValidString(items[1].Content) || len(items[0].Content)+len(items[1].Content) > 64<<10 || items[1].Locator.StartLine != 2 || len(rows[1].Content) != 42000 {
		t.Fatal("content bound changed persistent evidence")
	}
	data, err = sourceChunks(ref, source, rows, map[string]any{"sourceVersionId": "version", "offset": 2, "limit": 1})
	if err != nil || data["hasMore"].(bool) || data["nextOffset"] != 3 || data["items"].([]core.IndexedDocumentChunk)[0].ChunkID != "third" {
		t.Fatalf("bad next page %+v %v", data, err)
	}
	if _, err = sourceChunks(LocalRef("other"), source, rows, map[string]any{"sourceVersionId": "version", "limit": 1}); !errors.Is(err, status_error.KnowledgeNotFound) {
		t.Fatal("cross-library evidence returned", err)
	}
}

func TestOfficialSourceListUsesNativeContractWithoutInventingVersions(t *testing.T) {
	clientHTTP := readHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledge-bases/kb/knowledge" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("keyword") != "literal" {
			t.Errorf("wrong official query %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]any{{"id": "native", "knowledge_base_id": "kb", "file_name": "source.md", "parse_status": "pending"}}, "total": 3, "page": 2, "page_size": 1})
	}))
	client, err := remote.NewWeKnora(remote.Options{BaseURL: "http://knowledge.test", AuthMode: "bearer", Token: "token", HTTPClient: clientHTTP})
	if err != nil {
		t.Fatal(err)
	}
	ref := ResourceRef{Backend: "weknora", ConnectionID: "official", KnowledgeBaseID: "kb"}
	bindings := ProviderReadBindings(RemoteReadProvider{Official: client}, FreezeScope("", []ResourceRef{ref}), func(context.Context, string) ([]ResourceRef, error) { return []ResourceRef{ref}, nil })
	result, err := callRead(t, bindings, "knowledge.sources.list", map[string]any{"backend": "weknora", "connectionId": "official", "libraryId": "kb", "offset": 1, "limit": 1, "query": "literal"})
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	if data["versioning"] != "unavailable" || data["total"] != int64(3) {
		t.Fatalf("invented source versions %+v", data)
	}
}

func TestSourceReadKeepsHistoricalVersionAndQueryLocator(t *testing.T) {
	e, lib, _, source, _ := readFixture(t)
	ref := LocalRef(lib.ID)
	bindings := ReadBindings(e, FreezeScope("", []ResourceRef{ref}), func(context.Context, string) ([]ResourceRef, error) { return []ResourceRef{ref}, nil })
	replacement, err := e.ImportSource(context.Background(), core.ImportSourceInput{RequestID: "replace-source", LibraryID: lib.ID, SourceID: source.SourceID, ExpectedVersion: source.Version, Filename: "source.md", Bytes: []byte("Entirely new content without stored chunks")})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"libraryId": lib.ID, "sourceVersionId": source.ID, "query": "PERSISTED"}
	result, err := callRead(t, bindings, "knowledge.sources.read", params)
	if err != nil {
		t.Fatal(err)
	}
	data := result.Data.(map[string]any)
	items := data["items"].([]core.IndexedDocumentChunk)
	if data["source"].(*core.SourceVersion).ID != source.ID || len(items) == 0 || !strings.Contains(items[0].Content, "persisted") || items[0].SourceVersionID != source.ID || items[0].Locator.StartLine < 1 {
		t.Fatal("historical evidence was replaced or lost its locator")
	}
	delete(params, "query")
	params["chunkId"] = items[0].ChunkID
	if _, err = callRead(t, bindings, "knowledge.sources.read", params); err != nil {
		t.Fatal("stored chunk could not be located", err)
	}
	params["query"] = "persisted"
	if _, err = callRead(t, bindings, "knowledge.sources.read", params); !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatalf("ambiguous query accepted: %v", err)
	}
	delete(params, "query")
	params["sourceVersionId"] = replacement.ResourceID
	if _, err = callRead(t, bindings, "knowledge.sources.read", params); !errors.Is(err, status_error.KnowledgeNotFound) {
		t.Fatalf("old chunk was attached to new version: %v", err)
	}
	delete(params, "chunkId")
	result, err = callRead(t, bindings, "knowledge.sources.read", params)
	if err != nil || result.Data.(map[string]any)["total"] != 0 {
		t.Fatalf("unparsed replacement was fabricated: %+v %v", result, err)
	}
}

// readTransport 在内存中传递真实 HTTP 请求与响应，避免测试依赖本机监听权限。
type readTransport struct {
	handler http.Handler // 接收完整请求的真实协议处理器。
}

func (p readTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	r.RemoteAddr = "192.0.2.1:1234"
	p.handler.ServeHTTP(w, r)
	return w.Result(), nil
}

func readHTTPClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: readTransport{handler: handler}}
}
