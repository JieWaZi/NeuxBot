package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
)

// readConnectionFixture 仅提供宿主配置快照，真实读取仍经过HTTP和独立核心。
type readConnectionFixture struct {
	// KnowledgeConnectionController 保持未使用存取接口。
	controllers.KnowledgeConnectionController
	// value 是可被测试撤销或换代的真实连接配置。
	value models.KnowledgeConnection
}

func (c *readConnectionFixture) Get(context.Context, string) (models.KnowledgeConnection, error) {
	return c.value, nil
}
func (c *readConnectionFixture) List(context.Context) ([]models.KnowledgeConnection, error) {
	return []models.KnowledgeConnection{c.value}, nil
}
func (c *readConnectionFixture) Credential(context.Context, string) (string, error) {
	return "test-token", nil
}

func (c *readConnectionFixture) Snapshot(context.Context, string) (models.KnowledgeConnection, string, error) {
	return c.value, "test-token", nil
}

func TestOrdinaryRemoteReadsFreezeCompleteRefsAndNeverUseLocalEngineOrGenerator(t *testing.T) {
	ctx := context.Background()
	s, _ := newServiceFixture(t)
	remotePath := filepath.Join(t.TempDir(), "remote.db")
	remoteEngine, err := core.Open(ctx, core.Options{DBPath: remotePath, RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer remoteEngine.Close()
	generalReceipt, err := remoteEngine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "general", Name: "General", Kind: "general", WikiLanguage: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	general, _ := remoteEngine.GetLibrary(ctx, generalReceipt.ResourceID)
	page, err := remoteEngine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: general.ID, StructureVersion: general.StructureVersion, Page: core.Page{KnowledgeBaseID: general.ID, Slug: "concept/remote", Title: "Remote", PageType: "concept", Status: "published", Content: "HTTP authority"}})
	if err != nil {
		t.Fatal(err)
	}
	grants := map[string]httpapi.LibraryGrant{general.ID: {Operations: []string{"read"}, PolicyVersion: general.PolicyVersion}}
	policy, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"test-token": {Subject: "reader", Libraries: grants}}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(httpapi.New(remoteEngine, httpapi.Options{Security: policy}))
	defer endpoint.Close()
	connections := &readConnectionFixture{value: models.KnowledgeConnection{ID: "remote-one", Backend: "custom", BaseURL: endpoint.URL, AuthMode: "bearer", Revision: 1, Enabled: true}}
	s.config.Connections = connections
	// 无本机Engine以及未装配Runtime端口，若远端分支意外访问本机实现测试立即失败。
	s.config.Engine = nil
	bindings := s.AgentBindings(ctx, models.Chat{})
	call := func(operation string, params map[string]any) (agentcapability.Result, error) {
		t.Helper()
		for _, binding := range bindings {
			if binding.Operation == operation {
				return binding.Call(ctx, agentcapability.Request{Params: params})
			}
		}
		t.Fatalf("missing operation %s", operation)
		return agentcapability.Result{}, nil
	}
	list, err := call("knowledge.libraries.list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	items := list.Data.(map[string]any)["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("unexpected remote library discovery %+v", list)
	}
	params := map[string]any{"backend": "custom", "connectionId": "remote-one", "libraryId": general.ID, "pageId": page.ResourceID}
	if _, err := call("knowledge.pages.get", params); err != nil {
		t.Fatal(err)
	}
	if _, err := call("knowledge.pages.get", map[string]any{"backend": "custom", "connectionId": "other", "libraryId": general.ID, "pageId": page.ResourceID}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("crossconnection=%v", err)
	}
	laterReceipt, err := remoteEngine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "later", Name: "Later", Kind: "general", WikiLanguage: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	later, _ := remoteEngine.GetLibrary(ctx, laterReceipt.ResourceID)
	grants[later.ID] = httpapi.LibraryGrant{Operations: []string{"read"}, PolicyVersion: later.PolicyVersion}
	if err := policy.Replace(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"test-token": {Subject: "reader", Libraries: grants}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := call("knowledge.pages.get", map[string]any{"backend": "custom", "connectionId": "remote-one", "libraryId": later.ID, "pageId": "page"}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("new remote general expanded scope=%v", err)
	}
	var retargetCalls atomic.Int32
	retarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { retargetCalls.Add(1) }))
	defer retarget.Close()
	connections.value.Revision++
	connections.value.BaseURL = retarget.URL
	if _, err := call("knowledge.pages.get", params); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("connection retarget kept old grant=%v", err)
	}
	if retargetCalls.Load() != 0 {
		t.Fatal("old execution contacted retargeted endpoint")
	}
}

func TestAgentKnowledgeSearchUsesRealRAGChunksAndParentContext(t *testing.T) {
	for _, keyword := range []bool{false, true} {
		name := "vector"
		if keyword {
			name = "hybrid"
		}
		t.Run(name, func(t *testing.T) {
			s, lib := newServiceFixture(t)
			ctx := context.Background()
			strategy := core.IndexingStrategy{WikiEnabled: true, VectorEnabled: true, KeywordEnabled: keyword}
			model := "selected"
			chunking := *lib.ChunkingConfig
			chunking.ParentChunkSize = 256
			chunking.ChildChunkSize = 128
			chunking.ChunkOverlap = 16
			if _, err := s.config.Engine.UpdateLibrary(ctx, core.UpdateLibraryInput{RequestID: "vector-config", LibraryID: lib.ID, ExpectedVersion: lib.Version, Name: lib.Name, PolicyVersion: lib.PolicyVersion, IndexingStrategy: &strategy, EmbeddingModelID: &model, ChunkingConfig: &chunking}); err != nil {
				t.Fatal(err)
			}
			lib, _ = s.config.Engine.GetLibrary(ctx, lib.ID)
			raw := []byte("# 原件上下文\n" + strings.Repeat("紫外线照射的真实证据。", 180))
			receipt, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "rag-original", LibraryID: lib.ID, Filename: "evidence.md", Bytes: raw})
			if err != nil {
				t.Fatal(err)
			}
			source, err := s.config.Engine.GetSource(ctx, lib.ID, receipt.ResourceID)
			if err != nil {
				t.Fatal(err)
			}
			jobReceipt, err := s.config.Engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: "rag-job", LibraryID: lib.ID, ExpectedVersion: lib.Version, StructureVersion: lib.StructureVersion, SourceVersionIDs: []string{source.ID}, Permission: core.PermissionSnapshot{PolicyVersion: lib.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "test", ConfigDigest: "test"})
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := (core.MarkdownReader{}).Read(ctx, core.ReadRequest{SourceID: source.SourceID, SourceVersionID: source.ID, SHA256: source.SHA256, Filename: source.Filename, MIME: source.MIME, Raw: raw, Scope: "all"})
			if err != nil {
				t.Fatal(err)
			}
			embed := core.DocumentEmbedder{Fingerprint: "test-space", Embed: func(_ context.Context, texts []string) ([][]float32, error) {
				v := make([][]float32, len(texts))
				for i := range v {
					v[i] = []float32{1, 0}
				}
				return v, nil
			}}
			if err = s.config.Engine.IndexDocument(ctx, jobReceipt.ResourceID, source.ID, artifact, embed); err != nil {
				t.Fatal(err)
			}
			queries := 0
			s.config.FreezeEmbedder = func(_ context.Context, libraryID, id string) (core.DocumentEmbedder, error) {
				if id != model {
					t.Fatalf("wrong model %q", id)
				}
				return core.DocumentEmbedder{Fingerprint: embed.Fingerprint, Embed: func(_ context.Context, texts []string) ([][]float32, error) {
					queries++
					if len(texts) != 1 || texts[0] != "semantic ultraviolet" {
						t.Fatalf("not a query embedding: %v", texts)
					}
					return [][]float32{{1, 0}}, nil
				}}, nil
			}
			bindings := s.AgentBindings(ctx, models.Chat{})
			var search agentcapability.Binding
			for _, b := range bindings {
				if b.Operation == "knowledge.pages.search" {
					search = b
				}
			}
			result, err := search.Call(ctx, agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "query": "semantic ultraviolet"}})
			if err != nil {
				t.Fatal(err)
			}
			data := result.Data.(map[string]any)
			expectedMode := "vector"
			if keyword {
				expectedMode = "hybrid"
			}
			if queries != 1 || data["retrievalMode"] != expectedMode {
				t.Fatalf("semantic query not consumed: %#v, queries=%d", data, queries)
			}
			hits, ok := data["chunks"].([]bridge.DocumentReadHit)
			if !ok || len(hits) == 0 {
				t.Fatalf("missing real chunks: %#v", data["chunks"])
			}
			withParent := 0
			for _, hit := range hits {
				// 上游分块允许没有父块的尾部子块；有父块身份的命中必须回读完整上下文。
				if hit.Chunk.ParentID != "" {
					if hit.Parent == nil || len(hit.Parent.Content) <= len(hit.Chunk.Content) {
						t.Fatalf("missing complete parent context: parent=%q", hit.Chunk.ParentID)
					}
					withParent++
				}
			}
			if withParent == 0 {
				t.Fatal("no parent context returned")
			}

			encoded, _ := json.Marshal(data["chunks"])
			if !bytes.Contains(encoded, []byte(source.ID)) || !bytes.Contains(encoded, []byte("紫外线照射的真实证据")) || !bytes.Contains(encoded, []byte("parent")) {
				t.Fatalf("missing real source/parent context: %s", encoded)
			}
			if len(data["items"].([]map[string]any)) != 0 {
				t.Fatal("vector-only query silently used Wiki keyword results")
			}
			// 公开检索允许更多命中；多个子块共用父块时也必须正常回读。
			many, _, err := (bridge.LocalReadProvider{Engine: s.config.Engine, FreezeEmbedder: s.config.FreezeEmbedder, Limit: 30}).SearchChunks(ctx, bridge.LocalRef(lib.ID), "semantic ultraviolet")
			parents := map[string]bool{}
			for _, hit := range many {
				if hit.Parent != nil {
					parents[hit.Parent.ID] = true
				}
			}
			if err != nil || len(many) <= 5 || len(parents) <= 5 {
				t.Fatalf("大窗口父块回读失败: count=%d parents=%d err=%v", len(many), len(parents), err)
			}
			// 已生成的同一授权仍走真实配置错误，不降级为空或关键词结果。
			s.config.FreezeEmbedder = func(context.Context, string, string) (core.DocumentEmbedder, error) {
				return core.DocumentEmbedder{}, status_error.KnowledgeEmbeddingUnavailable
			}
			failedBindings := s.AgentBindings(ctx, models.Chat{})
			for _, b := range failedBindings {
				if b.Operation == "knowledge.pages.search" {
					if _, err = b.Call(ctx, agentcapability.Request{Params: map[string]any{"libraryId": lib.ID, "query": "semantic ultraviolet"}}); !errors.Is(err, status_error.KnowledgeEmbeddingUnavailable) {
						t.Fatalf("embedding error hidden: %v", err)
					}
				}
			}
		})
	}
}
