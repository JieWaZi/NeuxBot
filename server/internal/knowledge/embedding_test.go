package knowledge

import (
	"context"
	"encoding/json"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLibraryEmbeddingErrorsPreserveDomainAndCancellation(t *testing.T) {
	for _, scenario := range []struct {
		// name 标识嵌入端点的实际故障。
		name string
		// status 是服务返回的HTTP状态。
		status int
		// body 是服务返回的原始正文。
		body string
		// invalidURL 表示请求应在本机构造阶段失败。
		invalidURL bool
		// cancel 标识调用前取消，不能被覆盖成服务不可用。
		cancel bool
		// expired 标识调用前已经到期的上下文。
		expired bool
		// want 是该故障必须保留的原始稳定错误。
		want status_error.StatusError
	}{
		{name: "request", invalidURL: true, want: status_error.KnowledgeEmbeddingUnavailable},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: `{}`, want: status_error.KnowledgeEmbeddingUnavailable},
		{name: "response", status: http.StatusOK, body: `{"data":[]}`, want: status_error.KnowledgeEmbeddingResponseInvalid},
		{name: "cancel", cancel: true, want: status_error.OperationCanceled},
		{name: "deadline", expired: true, want: status_error.OperationTimeout},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if scenario.status != 0 {
					w.WriteHeader(scenario.status)
				}
				_, _ = w.Write([]byte(scenario.body))
			}))
			defer endpoint.Close()
			baseURL := endpoint.URL
			if scenario.invalidURL {
				baseURL = "://invalid"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario.cancel {
				cancel()
			}
			if scenario.expired {
				var expire context.CancelFunc
				ctx, expire = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer expire()
			}
			_, err := libraryDocumentEmbedder(baseURL, "model", "key").Embed(ctx, []string{"text"})
			if err != scenario.want {
				t.Fatalf("embedding error changed: got %v want %v", err, scenario.want)
			}
		})
	}
}

func TestLibraryEmbeddingFreezeUsesOnlyItsOwnProfileAndStableCoreReceipt(t *testing.T) {
	ctx := context.Background()
	e, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "core.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "host.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	defer sqlDB.Close()
	refs := controllers.NewKnowledgeController(db.DB)
	profiles := LibraryEmbeddings{Engine: e, References: refs}
	a, _ := e.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "a", Name: "A", WikiLanguage: "zh-CN"})
	b, _ := e.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "b", Name: "B", WikiLanguage: "zh-CN"})
	if _, err = profiles.Freeze(ctx, a.LibraryID, ""); err != status_error.KnowledgeEmbeddingNotConfigured {
		t.Fatalf("missing independent embedding configuration: %v", err)
	}
	seen := []string{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		seen = append(seen, r.Header.Get("Authorization")+"/"+in.Model)
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer endpoint.Close()
	saved, err := profiles.Save(ctx, SaveLibraryEmbeddingInput{RequestID: "profile-a", LibraryID: a.LibraryID, ExpectedLibraryVersion: 1, BaseURL: endpoint.URL, Model: "model-a", APIKey: "key-a"})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := profiles.Freeze(ctx, a.LibraryID, "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = profiles.Freeze(ctx, b.LibraryID, "model-a"); err != status_error.KnowledgeEmbeddingNotConfigured {
		t.Fatal("cross-library credentials", err)
	}
	if _, err = profiles.Save(ctx, SaveLibraryEmbeddingInput{RequestID: "profile-b", LibraryID: b.LibraryID, ExpectedLibraryVersion: 1, BaseURL: endpoint.URL, Model: "model-b", APIKey: "key-b"}); err != nil {
		t.Fatal(err)
	}
	current, err := profiles.Freeze(ctx, b.LibraryID, "model-b")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = frozen.Embed(ctx, []string{"a"})
	_, _ = current.Embed(ctx, []string{"b"})
	if strings.Join(seen, ",") != "Bearer key-a/model-a,Bearer key-b/model-b" {
		t.Fatal("profiles mixed", seen)
	}
	raw, _ := json.Marshal(saved)
	if strings.Contains(string(raw), "key-a") {
		t.Fatal("key echoed")
	}
	// 同请求在核心版本被其他编辑推进后仍恢复首次派生更新，不改变库名或重复增版。
	lib, _ := e.GetLibrary(ctx, a.LibraryID)
	_, err = e.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "rename", LibraryID: lib.ID, ExpectedVersion: lib.Version, Name: "Renamed", Purpose: lib.Purpose, PolicyVersion: lib.PolicyVersion})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := profiles.Save(ctx, SaveLibraryEmbeddingInput{RequestID: "profile-a", LibraryID: a.LibraryID, ExpectedLibraryVersion: 1, BaseURL: endpoint.URL, Model: "model-a", APIKey: "key-a"})
	if err != nil || replay.Version != saved.Version {
		t.Fatal(replay, err)
	}
	lib, _ = e.GetLibrary(ctx, a.LibraryID)
	if lib.Name != "Renamed" {
		t.Fatal("replay overwrote later core settings")
	}

	// 实际分块/索引与既有检索使用同一库profile，换端点后保留旧向量空间冲突。
	strategy := core.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}
	if _, err = e.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "enable-vectors", LibraryID: lib.ID, ExpectedVersion: lib.Version, Name: lib.Name, PolicyVersion: lib.PolicyVersion, IndexingStrategy: &strategy}); err != nil {
		t.Fatal(err)
	}
	lib, _ = e.GetLibrary(ctx, lib.ID)
	rawSource := []byte("独立知识库的真实检索内容必须使用本库模型与密钥生成向量。")
	imported, err := e.ImportSource(ctx, core.ImportSourceInput{RequestID: "raw", LibraryID: lib.ID, Filename: "source.md", Bytes: rawSource})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := e.GetSource(ctx, lib.ID, imported.ResourceID)
	accepted, err := e.AcceptJob(ctx, core.AcceptJobInput{RequestID: "index", LibraryID: lib.ID, ExpectedVersion: lib.Version, StructureVersion: lib.StructureVersion, SourceVersionIDs: []string{source.ID}, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "test", ConfigDigest: "test", EmbeddingFingerprint: frozen.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := (core.MarkdownReader{}).Read(ctx, core.ReadRequest{SourceID: source.SourceID, SourceVersionID: source.ID, SHA256: source.SHA256, Filename: source.Filename, MIME: source.MIME, Raw: rawSource, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.IndexDocument(ctx, accepted.JobID, source.ID, artifact, frozen); err != nil {
		t.Fatal(err)
	}
	provider := LocalReadProvider{Engine: e, FreezeEmbedder: profiles.Freeze}
	hits, mode, err := provider.SearchChunks(ctx, LocalRef(lib.ID), "query")
	if err != nil || mode != "hybrid" || len(hits) == 0 {
		t.Fatal("per-library query not using indexed profile", mode, err)
	}
	if seen[len(seen)-1] != "Bearer key-a/model-a" {
		t.Fatal("query used wrong profile")
	}
	changed := httptest.NewServer(endpoint.Config.Handler)
	defer changed.Close()
	if _, err = profiles.Save(ctx, SaveLibraryEmbeddingInput{RequestID: "endpoint-change", LibraryID: lib.ID, ExpectedVersion: 1, ExpectedLibraryVersion: lib.Version, BaseURL: changed.URL, Model: "model-a"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = provider.SearchChunks(ctx, LocalRef(lib.ID), "query"); err != status_error.KnowledgeConflict {
		t.Fatal("endpoint change silently searched old vector space", err)
	}
	// 已冻结调用仍使用原端点/密钥，不被配置切换追溯改写。
	if _, err = frozen.Embed(ctx, []string{"frozen"}); err != nil {
		t.Fatal(err)
	}
}
