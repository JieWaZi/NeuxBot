package knowledge_connections

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
)

func TestCommittedRemoteWikiWriteKeepsOriginalIdentityWhenReceiptPermissionIsRevoked(t *testing.T) {
	e, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	created, err := e.CreateLibrary(context.Background(), core.CreateLibraryInput{RequestID: "library", Name: "远端 Wiki", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	lib, _ := e.GetLibrary(context.Background(), created.ResourceID)
	config := httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"secret": {Subject: "editor", Libraries: map[string]httpapi.LibraryGrant{lib.ID: {Operations: []string{"read", "write"}, PolicyVersion: lib.PolicyVersion}}}}}
	policy, err := httpapi.NewPolicyStore(config)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(e, httpapi.Options{Security: policy})
	writes, reads := 0, 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes++
			committed := httptest.NewRecorder()
			handler.ServeHTTP(committed, r)
			if committed.Code != http.StatusOK {
				t.Errorf("Wiki write was not actually committed: %d %s", committed.Code, committed.Body)
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reads++
		if r.URL.Path != "/api/knowledge/v1/libraries/"+lib.ID+"/receipts" || r.URL.Query().Get("requestId") != "wiki-lost" {
			t.Error("reconciliation changed original Wiki request", r.URL)
		}
		handler.ServeHTTP(w, r)
	})
	command := &CreateRemotePage{ConnectionID: "connection", LibraryID: lib.ID, Body: knowledge.CreatePageInput{RequestID: "wiki-lost", StructureVersion: lib.StructureVersion, Page: knowledge.Page{Slug: "concept/original", Title: "原页面", PageType: "concept", Content: "已保存的正文"}}}
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatal(err)
	}
	committed, err := e.GetReceipt(ctx, "wiki-lost")
	if err != nil {
		t.Fatal("fixture did not persist the Wiki write", err)
	}
	if err := policy.Replace(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"secret": {Subject: "editor"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) || writes != 1 || reads != 1 {
		t.Fatal("receipt denial cleared the pending Wiki write", err, writes, reads)
	}
	if err := policy.Replace(config); err != nil {
		t.Fatal(err)
	}
	out, err := command.Output(ctx)
	if err != nil || out.(knowledge.Receipt).ResourceID != committed.ResourceID || writes != 1 || reads != 2 {
		t.Fatal("restored read permission did not recover the original write", out, err, writes, reads)
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, "wiki-lost")
	if err != nil || !link.ReceiptConfirmed || link.RemoteErrorCode != 0 {
		t.Fatal(link, err)
	}
	_, total, err := e.ListPages(ctx, core.PageQuery{LibraryID: lib.ID, Limit: 50})
	if err != nil || total != 1 {
		t.Fatal("reconciliation created duplicate Wiki pages", total, err)
	}
}

func TestReceiptReadFailuresCannotRejectPersistedRemoteWrites(t *testing.T) {
	for _, kind := range []string{"creation", "wiki"} {
		for failure, status := range map[string]int{"unauthenticated": 401, "forbidden": 403, "not_found": 404, "protocol": 0, "network": -1} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				writes, reads := 0, 0
				ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						writes++
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					reads++
					if status < 0 {
						connection, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = connection.Close()
						return
					}
					if status > 0 {
						w.WriteHeader(status)
						_ = json.NewEncoder(w).Encode(map[string]string{"code": failure})
						return
					}
					_, _ = w.Write([]byte(`{}`))
				})
				call := func() error {
					_, err := (&CreateRemoteLibrary{ConnectionID: "connection", Body: knowledge.CreateLibraryInput{RequestID: "pending", Name: "新库", WikiLanguage: "zh-CN"}}).Output(ctx)
					return err
				}
				if kind == "wiki" {
					call = func() error {
						_, err := (&CreateRemotePage{ConnectionID: "connection", LibraryID: "kb", Body: knowledge.CreatePageInput{RequestID: "pending", StructureVersion: 1, Page: knowledge.Page{Slug: "original", Title: "原页面"}}}).Output(ctx)
						return err
					}
				}
				for i := 0; i < 2; i++ {
					if err := call(); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
						t.Fatalf("receipt failure changed the original outcome to a rejection: %v", err)
					}
				}
				if writes != 1 || reads < 1 {
					t.Fatal("reconciliation resent the write", writes, reads)
				}
				link, err := global.KnowledgeControllerManager.GetOperation(ctx, "pending")
				if err != nil || link.ReceiptConfirmed || link.RemoteErrorCode != 0 {
					t.Fatal("receipt failure persisted a write rejection", link, err)
				}
			})
		}
	}
}

func TestPersistedOriginalWriteRejectionRemainsDefinitiveOnReplay(t *testing.T) {
	writes, reads := 0, 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes++
		} else {
			reads++
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden"}`))
	})
	command := &CreateRemotePage{ConnectionID: "connection", LibraryID: "kb", Body: knowledge.CreatePageInput{RequestID: "rejected", StructureVersion: 1, Page: knowledge.Page{Title: "原请求"}}}
	for i := 0; i < 2; i++ {
		if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeForbidden) {
			t.Fatal("persisted original rejection became unknown", err)
		}
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, "rejected")
	if err != nil || link.RemoteErrorCode != status_error.KnowledgeForbidden.Code() || writes != 1 || reads != 0 {
		t.Fatal(link, err, writes, reads)
	}
}
