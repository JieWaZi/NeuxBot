package knowledge_connections

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestCustomSourceDetailPreservesGeneratedSummary(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(core.SourceVersion{ID: "source", LibraryID: "kb", SourceID: "original", Description: "生成的资料摘要", Profile: &core.KnowledgeProfile{Gist: "资料画像"}, SummaryStatus: "completed"})
	})
	out, err := (&GetRemoteSource{ConnectionID: "connection", LibraryID: "kb", SourceID: "source"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source := out.(knowledge.Source)
	if source.Description != "生成的资料摘要" || source.Profile == nil || source.Profile.Gist != "资料画像" || source.SummaryStatus != "completed" {
		t.Fatalf("remote source lost generated metadata: %+v", source)
	}
}

func TestOfficialActivityAndSourceChunksRejectWithoutRequests(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "weknora", func(http.ResponseWriter, *http.Request) { calls++ })
	if _, err := (&ListRemoteLibraryActivity{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx); !errors.Is(err, status_error.KnowledgeUnsupported) {
		t.Fatal(err)
	}
	if _, err := (&GetRemoteSourceChunks{ConnectionID: "connection", LibraryID: "kb", SourceID: "source"}).Output(ctx); !errors.Is(err, status_error.KnowledgeUnsupported) {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("unsupported read contacted official server", calls)
	}
}

func TestCustomReadContractsRejectCrossLibraryResults(t *testing.T) {
	for _, kind := range []string{"activity", "chunks"} {
		t.Run(kind, func(t *testing.T) {
			ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
				if kind == "activity" {
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":1,"scope_id":"other-library"}],"next_cursor":1}`))
					return
				}
				_, _ = w.Write([]byte(`[{"id":"chunk","libraryId":"other-library","sourceVersionId":"source","content":"other content"}]`))
			})
			var err error
			if kind == "activity" {
				_, err = (&ListRemoteLibraryActivity{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
			} else {
				_, err = (&GetRemoteSourceChunks{ConnectionID: "connection", LibraryID: "kb", SourceID: "source"}).Output(ctx)
			}
			if !errors.Is(err, status_error.KnowledgeRemoteIncompatible) {
				t.Fatal("cross-library response became success", err)
			}
		})
	}
}

func TestRemoteActivityPermissionDenialIsNotAnEmptyList(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden"}`))
	})
	if out, err := (&ListRemoteLibraryActivity{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx); !errors.Is(err, status_error.KnowledgeForbidden) || out != nil {
		t.Fatal(out, err)
	}
}
