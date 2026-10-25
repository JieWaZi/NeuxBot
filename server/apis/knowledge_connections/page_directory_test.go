package knowledge_connections

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestRemotePageDirectoryUsesOwnedLightContractAndRejectsForeignRows(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-library", true: "foreign-library"}[foreign], func(t *testing.T) {
			ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/knowledge/v1/libraries/kb/page-directory" || r.URL.Query().Get("limit") != "1000" {
					t.Errorf("full list requested: %s", r.URL)
				}
				library := "kb"
				if foreign {
					library = "foreign"
				}
				json.NewEncoder(w).Encode(map[string]any{"items": []*core.PageDirectoryEntry{{ID: "p", KnowledgeBaseID: library, Slug: "concept/p", Title: "目录页面", Summary: "简介", Version: 1, FolderID: "f", CategoryPath: []string{"目录"}, SortOrder: 3}}, "total": 1, "offset": 0, "limit": 1000})
			})
			out, err := (&ListRemotePageDirectory{ConnectionID: "connection", LibraryID: "kb", Limit: 1000}).Output(ctx)
			if foreign {
				if err != status_error.KnowledgeRemoteIncompatible {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data := out.(knowledge.PageDirectoryList)
			if data.Total != 1 || len(data.Items) != 1 || data.Items[0].ThemeID != "f" || data.Items[0].SortOrder != 3 {
				t.Fatal(data)
			}
		})
	}
}
func TestOfficialPageDirectoryPreservesOfficialPaginationAndDropsBody(t *testing.T) {
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledgebase/kb/wiki/pages" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("page_size") != "100" {
			t.Errorf("wrong official contract: %s", r.URL)
		}
		w.Write([]byte(`{"pages":[{"id":"p","knowledge_base_id":"kb","slug":"concept/p","title":"官方页面","page_type":"concept","folder_id":"f","category_path":["主题"],"sort_order":7,"summary":"简介","version":2,"content":"不需要的正文","source_refs":["s"],"out_links":["other"]}],"total":101,"page":2,"page_size":100,"total_pages":2}`))
	})
	out, err := (&ListRemotePageDirectory{ConnectionID: "connection", LibraryID: "kb", Offset: 100, Limit: 1000}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := out.(knowledge.PageDirectoryList)
	if data.Total != 101 || len(data.Items) != 1 || data.Items[0].ThemeID != "f" || data.Items[0].Version != 2 || data.Items[0].SortOrder != 7 || len(data.Items[0].CategoryPath) != 1 {
		t.Fatal(data)
	}
	raw, _ := json.Marshal(data)
	for _, value := range []string{`"content"`, `"sourceRefs"`, `"outLinks"`, "不需要的正文"} {
		if strings.Contains(string(raw), value) {
			t.Fatal("heavy proxy response", string(raw))
		}
	}
}

func TestRemotePageDirectoryHonorsCancelledHTTPRequest(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":1000}`))
	})
	requestCtx, cancel := context.WithCancel(t.Context())
	cancel()
	ctx.Request = ctx.Request.WithContext(requestCtx)
	_, err := (&ListRemotePageDirectory{ConnectionID: "connection", LibraryID: "kb", Limit: 1000}).Output(ctx)
	if err != status_error.KnowledgeCancelled || calls != 0 {
		t.Fatalf("cancel ignored: err=%v calls=%d", err, calls)
	}
}

func TestOwnedThemeDirectoryReturnsWholeTreeInOneRead(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/knowledge/v1/libraries/kb/themes" {
			t.Errorf("unexpected URL %s", r.URL)
		}
		json.NewEncoder(w).Encode([]*core.Theme{{ID: "root", KnowledgeBaseID: "kb", Name: "根目录"}, {ID: "child", KnowledgeBaseID: "kb", ParentID: "root", Name: "子目录"}, {ID: "leaf", KnowledgeBaseID: "kb", ParentID: "child", Name: "末级"}})
	})
	result, err := (&ListRemoteThemes{ConnectionID: "connection", LibraryID: "kb", All: true}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(RemoteThemeList)
	if !data.Complete || len(data.Items) != 3 || calls != 1 {
		t.Fatalf("directory=%+v calls=%d", data, calls)
	}
	if !data.Items[0].HasChildren || !data.Items[1].HasChildren || data.Items[2].HasChildren {
		t.Fatal(data.Items)
	}
}
