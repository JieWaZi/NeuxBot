package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
)

func TestWikiBindingsReplaceWithAgentHistoryAndReplay(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	r, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/topic", Title: "主题", PageType: "concept", Content: "旧说明与保留内容"}})
	if err != nil {
		t.Fatal(err)
	}
	chat := models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}
	s.config.Chats = selectedChat{chat: chat}
	bindings := s.AgentBindings(ctx, chat)
	var replace *agentcapability.Binding
	for i := range bindings {
		if bindings[i].Operation == "knowledge.pages.replace" {
			replace = &bindings[i]
		}
	}
	if replace == nil {
		t.Fatal("Wiki replacement binding missing")
	}
	request := agentcapability.Request{Operation: replace.Operation, RequestID: "replace", Params: map[string]any{"libraryId": lib.ID, "pageId": r.ResourceID, "expectedVersion": 1, "structureVersion": lib.StructureVersion, "oldText": "旧说明", "newText": "新说明"}}
	result, err := replace.Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := s.config.Engine.GetPage(ctx, lib.ID, r.ResourceID)
	if page.Content != "新说明与保留内容" || page.LastEditSource != "agent" {
		t.Fatalf("page=%+v result=%+v", page, result)
	}
	if _, err = replace.Call(ctx, request); err != nil {
		t.Fatalf("replay=%v", err)
	}
	after, _ := s.config.Engine.GetPage(ctx, lib.ID, r.ResourceID)
	if after.Version != page.Version {
		t.Fatal("replay created a second revision")
	}
	stale := request
	stale.RequestID = "stale"
	if _, err = replace.Call(ctx, stale); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("stale page error=%v", err)
	}
	request.Params["libraryId"] = "outside"
	if _, err = replace.Call(ctx, request); err == nil {
		t.Fatal("cross-library replacement accepted")
	}
}

func TestLibraryMaintenancePagePlacementPreservesOmittedFolderAndManualProtection(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	folder, err := s.config.Engine.WriteTheme(ctx, core.WriteThemeInput{RequestID: "folder", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Theme: core.Theme{Name: "归并后的目录"}})
	if err != nil {
		t.Fatal(err)
	}
	lib, _ = s.config.Engine.GetLibrary(ctx, lib.ID)
	created, err := s.config.Engine.WriteAgentPage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/topic", Title: "主题", PageType: "concept", Content: "保留正文"}})
	if err != nil {
		t.Fatal(err)
	}
	chat := models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}
	s.config.Chats = selectedChat{chat: chat}
	bindings, err := s.LibraryMaintenanceBindings(ctx, chat, bridge.LocalRef(lib.ID))
	if err != nil {
		t.Fatal(err)
	}
	var write agentcapability.Binding
	for _, binding := range bindings {
		if binding.Operation == "knowledge.pages.write" {
			write = binding
		}
	}
	if write.Call == nil {
		t.Fatal("library page revision binding missing")
	}
	for _, step := range []struct {
		name   string  // 本次目录操作的稳定身份。
		folder *string // 省略保留，空值表示移至根目录。
		want   string  // 预期持久目录身份。
	}{
		{name: "move", folder: &folder.ResourceID, want: folder.ResourceID},
		{name: "preserve", want: folder.ResourceID},
		{name: "root", folder: new(string), want: ""},
	} {
		page, _ := s.config.Engine.GetPage(ctx, lib.ID, created.ResourceID)
		currentLibrary, _ := s.config.Engine.GetLibrary(ctx, lib.ID)
		params := map[string]any{"libraryId": lib.ID, "pageId": page.ID, "expectedVersion": page.Version, "expectedUpdatedAt": page.UpdatedAt.Format(time.RFC3339Nano), "structureVersion": currentLibrary.StructureVersion, "slug": page.Slug, "title": page.Title, "summary": page.Summary, "content": page.Content, "pageType": page.PageType}
		if step.folder != nil {
			params["folderId"] = *step.folder
		}
		if _, err := write.Call(ctx, agentcapability.Request{RequestID: step.name, Params: params}); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		page, _ = s.config.Engine.GetPage(ctx, lib.ID, page.ID)
		if page.FolderID != step.want || page.Content != "保留正文" {
			t.Fatalf("%s changed placement incorrectly: %+v", step.name, page)
		}
	}
	page, _ := s.config.Engine.GetPage(ctx, lib.ID, created.ResourceID)
	currentLibrary, _ := s.config.Engine.GetLibrary(ctx, lib.ID)
	params := map[string]any{"libraryId": lib.ID, "pageId": page.ID, "expectedVersion": page.Version, "expectedUpdatedAt": page.UpdatedAt.Format(time.RFC3339Nano), "structureVersion": currentLibrary.StructureVersion, "slug": page.Slug, "title": page.Title, "summary": page.Summary, "content": page.Content, "pageType": page.PageType, "folderId": folder.ResourceID}
	singlePage, err := s.MaintenanceBindings(ctx, chat, bridge.LocalRef(lib.ID), page.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range singlePage {
		if binding.Operation == "knowledge.pages.purge" {
			t.Fatal("single-page correction acquired permanent duplicate cleanup")
		}
		if binding.Operation == "knowledge.wiki.auto-fix" {
			t.Fatal("single-page scope acquired library AutoFix")
		}
		if binding.Operation == "knowledge.pages.write" {
			if _, err := binding.Call(ctx, agentcapability.Request{RequestID: "single-page-move", Params: params}); !errors.Is(err, status_error.KnowledgeForbidden) {
				t.Fatalf("single-page scope changed navigation: %v", err)
			}
		}
	}
	page.ManualPlacement = true
	if _, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "manual-placement", LibraryID: lib.ID, ExpectedVersion: page.Version, StructureVersion: currentLibrary.StructureVersion, Page: *page}); err != nil {
		t.Fatal(err)
	}
	page, _ = s.config.Engine.GetPage(ctx, lib.ID, page.ID)
	currentLibrary, _ = s.config.Engine.GetLibrary(ctx, lib.ID)
	params["expectedVersion"], params["expectedUpdatedAt"], params["structureVersion"] = page.Version, page.UpdatedAt.Format(time.RFC3339Nano), currentLibrary.StructureVersion
	if _, err := write.Call(ctx, agentcapability.Request{RequestID: "manual-page-move", Params: params}); err == nil {
		t.Fatal("whole-library agent overwrote manual placement")
	}
}

func TestWikiMaintenanceCannotCreateAnotherPageByOmittingPageID(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	chat := models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}
	s.config.Chats = selectedChat{chat: chat}
	provider, err := s.freezeReadProvider(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bindings := s.wikiBindings(ctx, chat, []bridge.ResourceRef{bridge.LocalRef(lib.ID)}, provider, "selected-page")
	for _, binding := range bindings {
		if binding.Operation == "knowledge.sources.import" || binding.Operation == "knowledge.themes.write" || binding.Operation == "knowledge.jobs.retry" {
			t.Fatalf("single-page scope acquired %s", binding.Operation)
		}
		if binding.Operation != "knowledge.pages.write" {
			continue
		}
		_, err = binding.Call(ctx, agentcapability.Request{RequestID: "create-outside", Params: map[string]any{"libraryId": lib.ID, "expectedVersion": 0, "structureVersion": lib.StructureVersion, "slug": "outside", "title": "外部", "content": "正文", "pageType": "concept"}})
		if !errors.Is(err, status_error.KnowledgeForbidden) {
			t.Fatalf("omitted pageId: %v", err)
		}
	}
	pages, total, err := s.config.Engine.ListPages(ctx, core.PageQuery{LibraryID: lib.ID, Limit: 10})
	if err != nil || total != 0 || len(pages) != 0 {
		t.Fatalf("unexpected pages=%+v total=%d err=%v", pages, total, err)
	}
}

func TestWikiRemoteReplaysExplicitRejectionAndValidatesRecoveredTarget(t *testing.T) {
	for _, mode := range []string{"rejected", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, lib := newServiceFixture(t)
			ctx := context.Background()
			created, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "topic", Title: "主题", PageType: "concept", Content: "旧内容"}})
			if err != nil {
				t.Fatal(err)
			}
			policy, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"test-token": {Subject: "writer", Libraries: map[string]httpapi.LibraryGrant{lib.ID: {Operations: []string{"read", "write"}, PolicyVersion: lib.PolicyVersion}}}}})
			if err != nil {
				t.Fatal(err)
			}
			authority := httpapi.New(s.config.Engine, httpapi.Options{Security: policy})
			var posts, receipts atomic.Int32
			var correct, badState atomic.Bool
			chat := models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}
			id := (requestidempotency.Identity{ID: "replace"}).ResourceID("knowledge-agent", chat.ID, chat.ActiveExecutionID)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/replace-text") {
					posts.Add(1)
					if mode == "unknown" {
						_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: id, LibraryID: lib.ID, Operation: "page.write", ResourceID: created.ResourceID, State: "saved", Version: 2})
						return
					}
				}
				if strings.HasSuffix(r.URL.Path, "/receipts") {
					receipts.Add(1)
					resource := "another-page"
					if correct.Load() {
						resource = created.ResourceID
					}
					state := "saved"
					if badState.Load() {
						state = "failed"
					}
					_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: id, LibraryID: lib.ID, Operation: "page.replace-text", ResourceID: resource, State: state, Version: 2})
					return
				}
				authority.ServeHTTP(w, r)
			}))
			defer server.Close()
			s.config.Connections = &readConnectionFixture{value: models.KnowledgeConnection{ID: "remote", Backend: "custom", BaseURL: server.URL, AuthMode: "bearer", Revision: 1, Enabled: true}}
			s.config.Chats = selectedChat{chat: chat}
			var binding agentcapability.Binding
			for _, candidate := range s.AgentBindings(ctx, chat) {
				if candidate.Operation == "knowledge.pages.replace" {
					binding = candidate
				}
			}
			if binding.Call == nil {
				t.Fatal("remote replace missing")
			}
			request := agentcapability.Request{RequestID: "replace", Params: map[string]any{"backend": "custom", "connectionId": "remote", "libraryId": lib.ID, "pageId": created.ResourceID, "expectedVersion": 99, "structureVersion": lib.StructureVersion, "oldText": "旧内容", "newText": "新内容"}}
			want := status_error.KnowledgeConflict
			if mode == "unknown" {
				want = status_error.KnowledgeRemoteOutcomeUnknown
			}
			for i := 0; i < 2; i++ {
				if _, err = binding.Call(ctx, request); !errors.Is(err, want) {
					t.Fatalf("attempt %d: %v, want %v", i, err, want)
				}
			}
			if _, err = binding.Receipt(ctx, "replace"); !errors.Is(err, want) {
				t.Fatalf("receipt: %v, want %v", err, want)
			}
			if mode == "rejected" && receipts.Load() != 0 {
				t.Fatal("known rejection incorrectly queried a missing receipt")
			}
			if mode == "unknown" {
				correct.Store(true)
				badState.Store(true)
				if _, err = binding.Receipt(ctx, "replace"); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
					t.Fatalf("failed receipt accepted: %v", err)
				}
				badState.Store(false)
				if _, err = binding.Receipt(ctx, "replace"); err != nil {
					t.Fatalf("original target did not recover: %v", err)
				}
				link, e := s.config.References.GetOperation(ctx, id)
				if e != nil || !link.ReceiptConfirmed || link.ResourceID != created.ResourceID {
					t.Fatalf("recovery link=%+v err=%v", link, e)
				}
			}
			if posts.Load() != 1 {
				t.Fatalf("write re-sent %d times", posts.Load())
			}
		})
	}
}

func TestWikiRemoteCreationKeepsConfirmedResourceOnReplay(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	policy, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"test-token": {Subject: "writer", Libraries: map[string]httpapi.LibraryGrant{lib.ID: {Operations: []string{"read", "write"}, PolicyVersion: lib.PolicyVersion}}}}})
	if err != nil {
		t.Fatal(err)
	}
	authority := httpapi.New(s.config.Engine, httpapi.Options{Security: policy})
	chat := models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}
	id := (requestidempotency.Identity{ID: "create"}).ResourceID("knowledge-agent", chat.ID, chat.ActiveExecutionID)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pages/agent-write") {
			posts.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/receipts") {
			_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: id, LibraryID: lib.ID, Operation: "page.agent-write", ResourceID: "another-page", State: "saved", Version: 1})
			return
		}
		authority.ServeHTTP(w, r)
	}))
	defer server.Close()
	s.config.Connections = &readConnectionFixture{value: models.KnowledgeConnection{ID: "remote", Backend: "custom", BaseURL: server.URL, AuthMode: "bearer", Revision: 1, Enabled: true}}
	s.config.Chats = selectedChat{chat: chat}
	var binding agentcapability.Binding
	for _, candidate := range s.AgentBindings(ctx, chat) {
		if candidate.Operation == "knowledge.pages.write" {
			binding = candidate
		}
	}
	if binding.Call == nil {
		t.Fatal("page write binding missing")
	}
	request := agentcapability.Request{RequestID: "create", Params: map[string]any{"backend": "custom", "connectionId": "remote", "libraryId": lib.ID, "expectedVersion": 0, "structureVersion": lib.StructureVersion, "slug": "topic", "title": "主题", "summary": "简介", "content": "正文", "pageType": "concept"}}
	if _, err = binding.Call(ctx, request); err != nil {
		t.Fatal(err)
	}
	original, err := s.config.References.GetOperation(ctx, id)
	if err != nil || original.ResourceID == "" || !original.ReceiptConfirmed {
		t.Fatalf("original link=%+v err=%v", original, err)
	}
	if _, err = binding.Call(ctx, request); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatalf("replay swapped page: %v", err)
	}
	if _, err = binding.Receipt(ctx, "create"); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatalf("receipt swapped page: %v", err)
	}
	after, err := s.config.References.GetOperation(ctx, id)
	if err != nil || after.ResourceID != original.ResourceID || posts.Load() != 1 {
		t.Fatalf("changed confirmed link=%+v posts=%d err=%v", after, posts.Load(), err)
	}
}

// TestMaintenanceReadsWaitForPolicyWrite 防止维护专用读取绕过普通读取已有的撤权互斥。
func TestMaintenanceReadsWaitForPolicyWrite(t *testing.T) {
	s, lib := newServiceFixture(t)
	bindings, err := s.MaintenanceBindings(context.Background(), models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}, bridge.LocalRef(lib.ID), "page")
	if err != nil {
		t.Fatal(err)
	}
	var read agentcapability.Binding
	for _, binding := range bindings {
		if binding.Operation == "knowledge.libraries.get" {
			read = binding
		}
	}
	if read.Call == nil {
		t.Fatal("maintenance library read missing")
	}
	locked, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	policyResult := make(chan error, 1)
	go func() {
		defer close(stopped)
		policyResult <- s.WithPolicyWrite(func() error {
			close(locked)
			<-release
			_, err := s.config.Engine.UpdateLibrary(context.Background(), core.UpdateLibraryInput{RequestID: "archive", LibraryID: lib.ID, ExpectedVersion: lib.Version, Name: lib.Name, PolicyVersion: lib.PolicyVersion, Archived: true})
			return bridge.MapError(err)
		})
	}()
	<-locked
	result := make(chan error, 1)
	defer func() {
		close(release)
		<-stopped
		if err := <-policyResult; err != nil {
			t.Errorf("archive policy commit: %v", err)
		}
		if err := <-result; !errors.Is(err, status_error.KnowledgeForbidden) {
			t.Errorf("read after revocation: %v", err)
		}
	}()
	go func() {
		_, err := read.Call(context.Background(), agentcapability.Request{Params: map[string]any{"libraryId": lib.ID}})
		result <- err
		close(result)
	}()
	select {
	case err := <-result:
		t.Fatalf("maintenance read ran inside an unfinished policy mutation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestWikiWholePageCommandUsesCallerReadTime 防止智能体整页工具用最新读取时间掩盖旧正文。
func TestWikiWholePageCommandUsesCallerReadTime(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	rec, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "agent-time-page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/time", Title: "时间", PageType: "concept", Content: "需要保留的当前正文"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.config.Engine.GetPage(ctx, lib.ID, rec.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	chat := models.Chat{ID: "time-chat", WakerID: "waker", ActiveExecutionID: "time-execution"}
	s.config.Chats = selectedChat{chat: chat}
	var binding *agentcapability.Binding
	bindings := s.AgentBindings(ctx, chat)
	for i := range bindings {
		if bindings[i].Operation == "knowledge.pages.write" {
			binding = &bindings[i]
			break
		}
	}
	if binding == nil {
		t.Fatal("whole-page command missing")
	}
	params := map[string]any{"libraryId": lib.ID, "pageId": p.ID, "expectedVersion": p.Version, "expectedUpdatedAt": p.UpdatedAt.Add(-time.Millisecond).Format(time.RFC3339Nano), "structureVersion": lib.StructureVersion, "slug": p.Slug, "title": p.Title, "summary": p.Summary, "content": p.Content, "pageType": p.PageType}
	_, err = binding.Call(ctx, agentcapability.Request{Operation: binding.Operation, RequestID: "agent-old-time", Params: params})
	if !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("old agent read time error=%v", err)
	}
	params["expectedUpdatedAt"] = p.UpdatedAt.Format(time.RFC3339Nano)
	params["content"] = p.Content + "，本次合法补充"
	if _, err = binding.Call(ctx, agentcapability.Request{Operation: binding.Operation, RequestID: "agent-current-time", Params: params}); err != nil {
		t.Fatal(err)
	}
}
