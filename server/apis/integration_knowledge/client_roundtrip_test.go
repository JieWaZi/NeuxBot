package integration_knowledge

import (
	"context"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/remote"
	"net/http/httptest"
	"testing"
)

func TestPublishedUnifiedClientRoundTripPreservesVersionsAndPagination(t *testing.T) {
	e, lib, auth, refs := publishedFixture(t, constants.IntegrationCapabilityRetrieve, constants.IntegrationCapabilityIngest)
	server := httptest.NewServer(NewHandler(e, nil, auth, Options{References: refs}))
	defer server.Close()
	c, err := remote.NewIntegration(remote.Options{BaseURL: server.URL + "/api/v1", Token: "fixture-only"}, remote.ResourceRef{Backend: "integration", ConnectionID: "c", KnowledgeBaseID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	library, err := c.GetLibrary(t.Context())
	if err != nil || library.ID != lib.ID || library.Kind != "general" {
		t.Fatal(library, err)
	}
	for _, id := range []string{"source-a", "source-b"} {
		receipt, err := c.ImportSource(t.Context(), core.ImportSourceInput{RequestID: id, LibraryID: lib.ID, Filename: id + ".md", Bytes: []byte("# 原件")})
		if err != nil || receipt.RequestID != id || receipt.State != "saved" {
			t.Fatal(receipt, err)
		}
	}
	sources, err := c.ListSources(t.Context(), 1, 1)
	if err != nil || sources.Total != 2 || len(sources.Items) != 1 || sources.Offset != 1 {
		t.Fatal(sources, err)
	}
	receipt, err := c.WritePage(t.Context(), core.WritePageInput{RequestID: "create-page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/new", Title: "页面", PageType: "concept", Content: "正文"}})
	if err != nil {
		t.Fatal(receipt, err)
	}
	page, err := c.GetPage(context.Background(), receipt.ResourceID)
	if err != nil || page.Version != 1 || page.Content != "正文" {
		t.Fatal(page, err)
	}
	page.Content = "第一次修改"
	if _, err = c.WritePage(t.Context(), core.WritePageInput{RequestID: "edit", LibraryID: lib.ID, ExpectedVersion: 1, StructureVersion: lib.StructureVersion, Page: *page}); err != nil {
		t.Fatal(err)
	}
	page.Content = "失效修改"
	if _, err = c.WritePage(t.Context(), core.WritePageInput{RequestID: "stale", LibraryID: lib.ID, ExpectedVersion: 1, StructureVersion: lib.StructureVersion, Page: *page}); err == nil {
		t.Fatal("stale edit committed")
	}
}
