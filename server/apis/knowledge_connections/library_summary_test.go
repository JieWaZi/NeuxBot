package knowledge_connections

import (
	"encoding/json"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/apis/knowledge"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/neux-knowledge/core"
	"net/http"
	"testing"
)

func TestRemotePageTotalsPersistWithoutFilteredOverwrite(t *testing.T) {
	calls := 0
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/api/knowledge/v1/libraries" {
			json.NewEncoder(w).Encode(map[string]any{"items": []*core.Library{{ID: "kb", Name: "Library"}}, "total": 1, "offset": 0, "limit": 50})
			return
		}
		total := int64(12)
		if r.URL.Query().Get("query") != "" {
			total = 2
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": total, "offset": 0, "limit": 50})
	})
	for _, search := range []string{"", "filter"} {
		if _, err := (&ListRemotePages{ConnectionID: "connection", LibraryID: "kb", Query: search}).Output(ctx); err != nil {
			t.Fatal(err)
		}
	}
	out, err := (&ListRemoteLibraries{ConnectionID: "connection"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows := out.(knowledge.LibraryList).Items
	if len(rows) != 1 || rows[0].PageTotal == nil || *rows[0].PageTotal != 12 || rows[0].PageTotalObservedAt == 0 || calls != 3 {
		t.Fatal(rows, calls)
	}
	values, err := global.KnowledgeControllerManager.LibrarySummaries(ctx, "custom", "connection", 1, []string{"kb"})
	if err != nil || len(values) != 1 || values[0].PageTotal != 12 {
		t.Fatal(values, err)
	}
}

func TestRemoteDetailRefreshesPersistedTotal(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/knowledge/v1/libraries/kb" {
			json.NewEncoder(w).Encode(core.Library{ID: "kb", Name: "Remote", Kind: "general", Version: 1})
			return
		}
		if r.URL.Query().Get("limit") != "1" {
			t.Errorf("detail count reads too many pages: %s", r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "offset": 0, "limit": 1})
	})
	out, err := (&GetRemoteLibrary{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := out.(knowledge.Library)
	if item.PageTotal == nil || *item.PageTotal != 0 {
		t.Fatal(item)
	}
	rows, err := global.KnowledgeControllerManager.LibrarySummaries(ctx, "custom", "connection", 1, []string{"kb"})
	if err != nil || len(rows) != 1 || rows[0].PageTotal != 0 {
		t.Fatal(rows, err)
	}
}

func TestRemoteLibraryPreservesServerConfiguration(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/knowledge/v1/libraries/kb" {
			_ = json.NewEncoder(w).Encode(core.Library{ID: "kb", Name: "Remote", Kind: "general", Version: 2, PolicyVersion: 1, StructureVersion: 3, IndexingStrategy: core.IndexingStrategy{WikiEnabled: true}, WikiConfig: &core.WikiConfig{MaxPagesPerIngest: 7}, EmbeddingModelID: "server-embedding", ChunkingConfig: &core.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 80}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "offset": 0, "limit": 1})
	})
	out, err := (&GetRemoteLibrary{ConnectionID: "connection", LibraryID: "kb"}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := out.(knowledge.Library)
	if item.WikiConfig == nil || item.WikiConfig.MaxPagesPerIngest != 7 || !item.IndexingStrategy.WikiEnabled || item.EmbeddingModelID != "server-embedding" || item.ChunkingConfig == nil || item.ChunkingConfig.ChunkSize != 512 {
		t.Fatalf("server configuration was dropped: %+v", item)
	}
}

func TestRemoteLibraryUpdateForwardsServerConfiguration(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		var in core.UpdateLibraryInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/api/knowledge/v1/libraries/kb" || in.LibraryID != "kb" || in.ExpectedVersion != 2 || in.PolicyVersion != 1 || in.WikiConfig == nil || in.WikiConfig.MaxPagesPerIngest != 7 || in.IndexingStrategy == nil || !in.IndexingStrategy.WikiEnabled || in.EmbeddingModelID == nil || *in.EmbeddingModelID != "server-embedding" || in.ChunkingConfig == nil || in.ChunkingConfig.ChunkSize != 512 {
			t.Errorf("server configuration was dropped on write: %+v", in)
		}
		_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: "config-update", LibraryID: "kb", ResourceID: "kb", Operation: "library.update", State: "saved", Version: 3})
	})
	model := "server-embedding"
	_, err := (&UpdateRemoteLibrary{ConnectionID: "connection", LibraryID: "kb", Body: knowledge.UpdateLibraryInput{RequestID: "config-update", ExpectedVersion: 2, PolicyVersion: 1, Name: "Remote", WikiConfig: &core.WikiConfig{MaxPagesPerIngest: 7}, IndexingStrategy: &core.IndexingStrategy{WikiEnabled: true}, EmbeddingModelID: &model, ChunkingConfig: &core.ChunkingConfig{ChunkSize: 512}}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoteLibraryCreateForwardsCoreConfiguration(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		var in core.CreateLibraryInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/api/knowledge/v1/libraries" || in.WikiConfig == nil || in.WikiConfig.MaxPagesPerIngest != 7 || in.IndexingStrategy == nil || !in.IndexingStrategy.WikiEnabled || in.EmbeddingModelID != "server-embedding" || in.ChunkingConfig == nil || in.ChunkingConfig.ChunkSize != 512 {
			t.Errorf("core configuration was dropped on create: %+v", in)
		}
		_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: "config-create", LibraryID: "kb", ResourceID: "kb", Operation: "library.create", State: "saved", Version: 1})
	})
	_, err := (&CreateRemoteLibrary{ConnectionID: "connection", Body: knowledge.CreateLibraryInput{RequestID: "config-create", Name: "Remote", WikiLanguage: "zh-CN", WikiConfig: &core.WikiConfig{MaxPagesPerIngest: 7}, IndexingStrategy: &core.IndexingStrategy{WikiEnabled: true}, EmbeddingModelID: "server-embedding", ChunkingConfig: &core.ChunkingConfig{ChunkSize: 512}}}).Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoteLibraryCreateRejectsUntranslatableHostConfiguration(t *testing.T) {
	ctx := remoteContext(t, "custom", func(w http.ResponseWriter, r *http.Request) {
		t.Error("host configuration must not be silently dropped")
		_ = json.NewEncoder(w).Encode(core.Receipt{RequestID: "host-config", LibraryID: "kb", ResourceID: "kb", Operation: "library.create", State: "saved"})
	})
	for _, body := range []knowledge.CreateLibraryInput{
		{RequestID: "host-config", EmbeddingProfile: &knowledge.LibraryEmbeddingInput{}},
		{RequestID: "host-config", ExecutionDefaults: &knowledge.LibraryExecutionDefaultsInput{}},
	} {
		if _, err := (&CreateRemoteLibrary{ConnectionID: "connection", Body: body}).Output(ctx); !errors.Is(err, status_error.KnowledgeUnsupported) {
			t.Fatalf("host-only configuration was accepted: %v", err)
		}
	}
}
