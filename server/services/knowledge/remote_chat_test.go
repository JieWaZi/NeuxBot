package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/JieWaZi/neux-knowledge/httpapi"
)

// remoteChatExecution 仅在远端服务接纳真实核心任务，不包含客户端Runtime。
type remoteChatExecution struct {
	// ExecutionPort 保持未使用的远端执行入口形状。
	httpapi.ExecutionPort
	// engine 是远端唯一权威内容库。
	engine *core.Engine
	// calls 记录远端整理实际接纳次数。
	calls atomic.Int32
	// references 模拟同实例服务端也登记local执行锚点。
	references controllers.KnowledgeController
}

func (p *remoteChatExecution) Organize(ctx context.Context, in httpapi.OrganizeRequest, auth httpapi.Authorization) (*core.Receipt, error) {
	p.calls.Add(1)
	if p.references != nil {
		if err := p.references.BeginOperation(ctx, models.KnowledgeOperationLink{RequestID: in.RequestID, Operation: "job.accept", Digest: "server-" + in.RequestID, Backend: "local", ConnectionID: "local", KnowledgeBaseID: in.LibraryID}); err != nil {
			return nil, err
		}
	}
	return p.engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: in.RequestID, LibraryID: in.LibraryID, ExpectedVersion: in.ExpectedVersion, StructureVersion: in.StructureVersion, SourceVersionIDs: in.SourceVersionIDs, Permission: auth.Permission, RulesVersion: "rules", ConfigDigest: "profile"})
}

func TestRemoteChatSelectedAttachmentsUseHTTPWithoutLocalModelAndRecoverOriginalReceipt(t *testing.T) {
	ctx := context.Background()
	s, _ := newServiceFixture(t)
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "remote.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	receipt, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "Remote", Kind: "general", WikiLanguage: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	library, _ := engine.GetLibrary(ctx, receipt.ResourceID)
	existing, err := engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "unselected-pending", LibraryID: library.ID, Filename: "unselected.md", Bytes: []byte("Do not organize")})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := httpapi.NewPolicyStore(httpapi.SecurityConfig{Tokens: map[string]httpapi.Principal{"test-token": {Subject: "chat", Libraries: map[string]httpapi.LibraryGrant{library.ID: {Operations: []string{"read", "write", "organize"}, PolicyVersion: library.PolicyVersion}}}}})
	if err != nil {
		t.Fatal(err)
	}
	execution := &remoteChatExecution{engine: engine, references: s.config.References}
	handler := httpapi.New(engine, httpapi.Options{Security: policy, Execution: execution, Profiles: []httpapi.Profile{{ID: "profile", Name: "Server profile"}}})
	var imports atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/knowledge/v1/libraries/"+library.ID+"/sources" {
			imports.Add(1)
		}
		if r.Method == "POST" && r.URL.Path == "/api/knowledge/v1/libraries/"+library.ID+"/organize" {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if recorder.Code != 200 {
				t.Errorf("remote admission %d %s", recorder.Code, recorder.Body.String())
			}
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"requestId":`))
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer endpoint.Close()
	s.config.Engine = nil
	s.config.Connections = &readConnectionFixture{value: models.KnowledgeConnection{ID: "remote", Backend: "custom", BaseURL: endpoint.URL, AuthMode: "bearer", Revision: 1, Enabled: true}}
	root := t.TempDir()
	if err = workspace.EnsureExecutionWorkspace(root, "chat", "waker", nil); err != nil {
		t.Fatal(err)
	}
	s.config.Chats = selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", WorkspaceDirectory: root}}
	file := filepath.Join(t.TempDir(), "selected.md")
	if err = os.WriteFile(file, []byte("Only selected raw"), 0600); err != nil {
		t.Fatal(err)
	}
	input := RemoteChatCommand{RequestID: "remote-chat", ChatID: "chat", Ref: bridge.ResourceRef{Backend: "custom", ConnectionID: "remote", KnowledgeBaseID: library.ID}, ProfileID: "profile", Attachments: []workspace.AttachmentInput{{SourcePath: file}}}
	result, err := s.OrganizeRemoteChat(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "unknown" || imports.Load() != 1 || execution.calls.Load() != 1 {
		t.Fatal(result, imports.Load(), execution.calls.Load())
	}
	reopened := New(s.config)
	links, total, err := reopened.ListRemoteChatOperations(ctx, "chat", 0, 50)
	if err != nil || total != 1 || len(links) != 1 || links[0].RequestID != input.RequestID {
		t.Fatal("durable chat operation lost", links, total, err)
	}
	result, err = reopened.ReadRemoteChatOperation(ctx, "chat", input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "accepted" || result.Receipt == nil || result.Job == nil || result.Job.State != "queued" || len(result.Job.SourceVersionIDs) != 1 || result.Job.SourceVersionIDs[0] == existing.ResourceID {
		t.Fatal(result)
	}
	result, err = s.OrganizeRemoteChat(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "accepted" || imports.Load() != 1 || execution.calls.Load() != 1 {
		t.Fatal(result, imports.Load(), execution.calls.Load())
	}
	sources, total, err := engine.ListSources(ctx, library.ID, 0, 100)
	if err != nil || total != 2 || !slices.ContainsFunc(sources, func(source *core.SourceVersion) bool { return source.Filename == "selected.md" }) {
		t.Fatal(sources, total, err)
	}
}
