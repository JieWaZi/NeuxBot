package knowledge_connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
)

func TestCustomThemeContractReadsDirectChildrenAndWritesRealServer(t *testing.T) {
	e, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	receipt, err := e.CreateLibrary(context.Background(), core.CreateLibraryInput{RequestID: "library", Name: "远端目录", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	lib, _ := e.GetLibrary(context.Background(), receipt.ResourceID)
	policy, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"secret": {Subject: "owner", Libraries: map[string]httpapi.LibraryGrant{lib.ID: {Operations: []string{"read", "write"}, PolicyVersion: lib.PolicyVersion}}}}})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(e, httpapi.Options{Security: policy})
	ctx := remoteContext(t, "custom", handler.ServeHTTP)
	root, err := (&CreateRemoteTheme{ConnectionID: "connection", LibraryID: lib.ID, Body: knowledge.CreateThemeInput{RequestID: "root", StructureVersion: lib.StructureVersion, Theme: knowledge.Theme{Name: "根目录"}}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rootID := root.(knowledge.Receipt).ResourceID
	lib, _ = e.GetLibrary(ctx, lib.ID)
	child, err := (&CreateRemoteTheme{ConnectionID: "connection", LibraryID: lib.ID, Body: knowledge.CreateThemeInput{RequestID: "child", StructureVersion: lib.StructureVersion, Theme: knowledge.Theme{Name: "子目录", ParentID: rootID}}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	childID := child.(knowledge.Receipt).ResourceID
	rootRows, err := (&ListRemoteThemes{ConnectionID: "connection", LibraryID: lib.ID}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rootRows.(RemoteThemeList).Items {
		if row.ID == childID {
			t.Fatal("root listing flattened a nested theme")
		}
		if row.ID == rootID {
			found = row.HasChildren && row.ParentID == ""
		}
	}
	if !found {
		t.Fatal("root lost child-presence information", rootRows)
	}
	childRows, err := (&ListRemoteThemes{ConnectionID: "connection", LibraryID: lib.ID, ParentID: rootID}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows := childRows.(RemoteThemeList)
	if rows.ParentID != rootID || len(rows.Items) != 1 || rows.Items[0].ID != childID || rows.Items[0].ParentID != rootID || rows.Items[0].HasChildren {
		t.Fatalf("direct child contract changed: %+v", rows)
	}
	lib, _ = e.GetLibrary(ctx, lib.ID)
	if _, err := (&WriteRemoteTheme{ConnectionID: "connection", LibraryID: lib.ID, ThemeID: rootID, Body: knowledge.WriteThemeInput{RequestID: "nonempty", StructureVersion: lib.StructureVersion, Delete: true, Theme: knowledge.Theme{Name: "根目录"}}}).Output(ctx); !errors.Is(err, status_error.KnowledgeInvalid) {
		t.Fatal("nonempty remote theme was deleted", err)
	}
	if _, err := (&WriteRemoteTheme{ConnectionID: "connection", LibraryID: lib.ID, ThemeID: childID, Body: knowledge.WriteThemeInput{RequestID: "rename", StructureVersion: lib.StructureVersion, Theme: knowledge.Theme{Name: "新子目录", ParentID: rootID}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	lib, _ = e.GetLibrary(ctx, lib.ID)
	if _, err := (&WriteRemoteTheme{ConnectionID: "connection", LibraryID: lib.ID, ThemeID: childID, Body: knowledge.WriteThemeInput{RequestID: "stale-delete", StructureVersion: lib.StructureVersion - 1, Delete: true, Theme: knowledge.Theme{Name: "新子目录", ParentID: rootID}}}).Output(ctx); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatal("stale structure write was accepted", err)
	}
	if _, err := (&WriteRemoteTheme{ConnectionID: "connection", LibraryID: lib.ID, ThemeID: childID, Body: knowledge.WriteThemeInput{RequestID: "delete-child", StructureVersion: lib.StructureVersion, Delete: true, Theme: knowledge.Theme{Name: "新子目录", ParentID: rootID}}}).Output(ctx); err != nil {
		t.Fatal(err)
	}
	childRows, err = (&ListRemoteThemes{ConnectionID: "connection", LibraryID: lib.ID, ParentID: rootID}).Output(ctx)
	if err != nil || len(childRows.(RemoteThemeList).Items) != 0 {
		t.Fatal("deleted child remained on remote", childRows, err)
	}
}

func TestOfficialThemeContractRetainsParentCountAndReadErrors(t *testing.T) {
	ctx := remoteContext(t, "weknora", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/knowledgebase/kb/wiki/folders" {
			t.Error(r.URL)
		}
		switch r.URL.Query().Get("parent_id") {
		case "":
			_, _ = w.Write([]byte(`{"parent_id":"","folders":[{"id":"root","knowledge_base_id":"kb","parent_id":"","name":"根目录","page_count":2,"has_children":true}]}`))
		case "root":
			_, _ = w.Write([]byte(`{"parent_id":"root","folders":[{"id":"child","knowledge_base_id":"kb","parent_id":"root","name":"子目录","page_count":3,"has_children":false}]}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "forbidden"})
		}
	})
	for _, parent := range []string{"", "root"} {
		out, err := (&ListRemoteThemes{ConnectionID: "connection", LibraryID: "kb", ParentID: parent}).Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rows := out.(RemoteThemeList)
		if rows.ParentID != parent || len(rows.Items) != 1 || rows.Items[0].ParentID != parent || (parent == "" && (!rows.Items[0].HasChildren || rows.Items[0].PageCount != 2)) || (parent == "root" && (rows.Items[0].HasChildren || rows.Items[0].PageCount != 3)) {
			t.Fatal(rows)
		}
	}
	if out, err := (&ListRemoteThemes{ConnectionID: "connection", LibraryID: "kb", ParentID: "unreadable"}).Output(ctx); err == nil || out != nil {
		t.Fatal("subdirectory failure became empty success", out, err)
	}
}

func TestOfficialThemeWritesAreUnsupportedWithoutRequests(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "weknora", func(http.ResponseWriter, *http.Request) { calls++ })
	if _, err := (&CreateRemoteTheme{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx); !errors.Is(err, status_error.KnowledgeUnsupported) {
		t.Fatal(err)
	}
	if _, err := (&WriteRemoteTheme{ConnectionID: "connection", LibraryID: "kb", ThemeID: "theme"}).Output(ctx); !errors.Is(err, status_error.KnowledgeUnsupported) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("unsupported theme write contacted official server", calls)
	}
}

func TestCustomThemesRejectCorruptionOutsideRequestedRoot(t *testing.T) {
	for name, rows := range map[string][]*core.Theme{
		"orphan":    {{ID: "orphan", ParentID: "missing", KnowledgeBaseID: "kb"}},
		"cycle":     {{ID: "a", ParentID: "b", KnowledgeBaseID: "kb"}, {ID: "b", ParentID: "a", KnowledgeBaseID: "kb"}},
		"duplicate": {{ID: "same", KnowledgeBaseID: "kb"}, {ID: "same", KnowledgeBaseID: "kb"}},
		"self":      {{ID: "self", ParentID: "self", KnowledgeBaseID: "kb"}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(rows)
			})
			if out, err := (&ListRemoteThemes{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteIncompatible) || out != nil {
				t.Fatalf("corrupt full theme table became a root success: rows=%+v out=%+v err=%v", rows, out, err)
			}
		})
	}
}
