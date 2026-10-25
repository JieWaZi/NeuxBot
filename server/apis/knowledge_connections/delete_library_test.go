package knowledge_connections

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

func TestRemoteLibraryDeleteUnknownReconcilesWithoutRepeatingDelete(t *testing.T) {
	writes := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes++
			if r.URL.Path != "/api/knowledge/v1/libraries/kb/delete" {
				t.Error(r.URL.Path)
			}
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["requestId"] != "delete-original" || input["expectedVersion"] != float64(3) {
				t.Error(input)
			}
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"requestId":`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"requestId":"delete-original","libraryId":"kb","resourceId":"kb","operation":"library.delete","state":"accepted","version":3}`))
	})
	operation := &DeleteRemoteLibrary{ConnectionID: "connection", LibraryID: "kb", Body: knowledge.DeleteLibraryInput{RequestID: "delete-original", ExpectedVersion: 3}}
	if _, err := operation.Output(ctx); !errors.Is(err, status_error.KnowledgeRemoteOutcomeUnknown) {
		t.Fatal(err)
	}
	result, err := operation.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.(knowledge.Receipt).State != "accepted" || writes != 1 {
		t.Fatal(result, writes)
	}
}

func TestOfficialLibraryDeletionIsExplicitlyUnsupported(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "weknora", func(http.ResponseWriter, *http.Request) { calls++ })
	_, err := (&DeleteRemoteLibrary{ConnectionID: "connection", LibraryID: "kb", Body: knowledge.DeleteLibraryInput{RequestID: "official-delete", ExpectedVersion: 1}}).Output(ctx)
	if !errors.Is(err, status_error.KnowledgeUnsupported) || calls != 0 {
		t.Fatal(err, calls)
	}
}

func TestRemoteLibraryDeletionStatusKeepsBlockedCleanupVisible(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/knowledge/v1/libraries/kb/deletion" {
			t.Error(r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"libraryId":"kb","version":2,"policyVersion":1,"state":"blocked","blockedBlobCount":1,"pendingBlobCount":1,"deletedBlobCount":0,"retainedSharedBlobCount":0}`))
	})
	value, err := (&GetRemoteLibraryDeletion{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result["state"] != "blocked" || result["blockedBlobCount"] != float64(1) {
		t.Fatal(result)
	}
}
