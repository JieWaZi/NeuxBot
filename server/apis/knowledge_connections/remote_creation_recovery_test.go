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
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
	"gorm.io/gorm"
)

func TestRemoteCreationLostResponseRecoversOwnReceiptWithoutSecondPOST(t *testing.T) {
	e, err := core.Open(context.Background(), core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	policy, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"secret": {Subject: "creator", CreateLibrary: true}}})
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
				t.Errorf("server did not commit creation: %d %s", committed.Code, committed.Body)
			}
			w.Header().Set("Content-Length", "10000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"requestId":`))
			return
		}
		reads++
		if r.URL.Path != "/api/knowledge/v1/libraries/creation-receipts" || r.URL.Query().Get("requestId") != "lost-create" {
			t.Errorf("creation recovery used an unbound library: %s", r.URL)
		}
		handler.ServeHTTP(w, r)
	})
	command := &CreateRemoteLibrary{ConnectionID: "connection", Body: knowledge.CreateLibraryInput{RequestID: "lost-create", Name: "远端新库", WikiLanguage: "zh-CN"}}
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatal("lost response was not retained as unknown", err)
	}
	committed, err := e.GetReceipt(ctx, "lost-create")
	if err != nil {
		t.Fatal("remote creation was not actually committed", err)
	}
	global.KnowledgeControllerManager = controllers.NewKnowledgeController(ctx.MustGet("testReferenceDB").(*gorm.DB))
	if err := policy.Replace(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"secret": {Subject: "creator"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) || writes != 1 || reads != 1 {
		t.Fatal("receipt permission denial was mistaken for rejection of the original creation", err, writes, reads)
	}
	if err := policy.Replace(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"secret": {Subject: "creator", CreateLibrary: true}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		out, err := command.Output(ctx)
		if err != nil || out.(knowledge.Receipt).ResourceID != committed.LibraryID || writes != 1 || reads != i+2 {
			t.Fatalf("creation recovery did not query original receipt: out=%+v err=%v writes=%d reads=%d", out, err, writes, reads)
		}
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, "lost-create")
	if err != nil || !link.ReceiptConfirmed || link.KnowledgeBaseID != committed.LibraryID || link.ResourceID != committed.LibraryID {
		t.Fatal("creation receipt did not bind the real remote library", link, err)
	}
	_, total, err := e.ListLibraries(ctx, 0, 50)
	if err != nil || total != 1 {
		t.Fatal("creation recovery created another library", total, err)
	}
}

func TestRemoteMissingCreationReceiptRemainsUnknownWithoutResending(t *testing.T) {
	writes, reads := 0, 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reads++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found"}`))
	})
	command := &CreateRemoteLibrary{ConnectionID: "connection", Body: knowledge.CreateLibraryInput{RequestID: "missing-create", Name: "远端新库", WikiLanguage: "zh-CN"}}
	for i := 0; i < 2; i++ {
		if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
			t.Fatal(err)
		}
	}
	if writes != 1 || reads != 1 {
		t.Fatalf("missing receipt must only be queried: writes=%d reads=%d", writes, reads)
	}
}

func TestRemoteCreationReconciliationCannotReplaceBoundLibrary(t *testing.T) {
	writes := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		library := "original-library"
		if r.Method == http.MethodPost {
			writes++
		} else {
			library = "another-library"
		}
		_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: "bound-create", Operation: "library.create", LibraryID: library, ResourceID: library, Version: 1, State: "saved"})
	})
	command := &CreateRemoteLibrary{ConnectionID: "connection", Body: knowledge.CreateLibraryInput{RequestID: "bound-create", Name: "原库", WikiLanguage: "zh-CN"}}
	if _, err := command.Output(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) || writes != 1 {
		t.Fatal("recovery replaced a bound library", err, writes)
	}
	link, err := global.KnowledgeControllerManager.GetOperation(ctx, "bound-create")
	if err != nil || link.KnowledgeBaseID != "original-library" {
		t.Fatal(link, err)
	}
}
