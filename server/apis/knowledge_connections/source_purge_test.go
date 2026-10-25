package knowledge_connections

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestRemotePurgeUnknownReconcilesWithoutRepeatingConfirmation(t *testing.T) {
	writes := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes++
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["requestId"] != "confirm-original" || input["sourceId"] != "source" || input["confirmed"] != true {
				t.Error(input)
			}
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"requestId":`))
			return
		}
		if r.URL.Query().Get("requestId") != "confirm-original" {
			t.Error(r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"requestId":"confirm-original","libraryId":"kb","resourceId":"purge","operation":"source.purge.confirm","state":"accepted","version":1}`))
	})
	operation := &ConfirmRemoteSourcePurge{ConnectionID: "connection", LibraryID: "kb", Body: knowledge.ConfirmSourcePurgeInput{RequestID: "confirm-original", SourceID: "source", PreviewDigest: strings.Repeat("a", 64), Confirmed: true}}
	_, err := operation.Output(ctx)
	if !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatal(err)
	}
	result, err := operation.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.(knowledge.Receipt).ResourceID != "purge" || writes != 1 {
		t.Fatal(result, writes)
	}
}

func TestOfficialPurgeIsExplicitlyUnsupported(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "weknora", func(http.ResponseWriter, *http.Request) { calls++ })
	_, err := (&PreviewRemoteSourcePurge{ConnectionID: "connection", LibraryID: "kb", SourceID: "source"}).Output(ctx)
	if !errors.Is(err, status_error.KnowledgeUnsupported) || calls != 0 {
		t.Fatal(err, calls)
	}
}
