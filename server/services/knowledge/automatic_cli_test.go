package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcli"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
)

// TestAutomaticMaintenanceCLIUsesAuthorizedTools 验证真实命令解析、HTTP 授权、领域提交和未知结果恢复，不以工具桩存在代替维护能力。
func TestAutomaticMaintenanceCLIUsesAuthorizedTools(t *testing.T) {
	s, library := newServiceFixture(t)
	ctx := context.Background()
	raw := []byte("# 原件\n\nNTP 配置改变后才重启服务。")
	sourceReceipt, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: library.ID, Filename: "time.md", Bytes: raw})
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.config.Engine.GetSource(ctx, library.ID, sourceReceipt.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.config.Engine.AcceptJob(ctx, core.AcceptJobInput{RequestID: "index", LibraryID: library.ID, ExpectedVersion: library.Version, StructureVersion: library.StructureVersion, SourceVersionIDs: []string{source.ID}, Permission: core.PermissionSnapshot{PolicyVersion: library.PolicyVersion, Operations: []string{"organize"}}, RulesVersion: "test", ConfigDigest: "test"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := (core.MarkdownReader{}).Read(ctx, core.ReadRequest{SourceID: source.SourceID, SourceVersionID: source.ID, SHA256: source.SHA256, Filename: source.Filename, Raw: raw, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.config.Engine.IndexDocument(ctx, job.ResourceID, source.ID, artifact, core.DocumentEmbedder{}); err != nil {
		t.Fatal(err)
	}
	created, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: library.ID, StructureVersion: library.StructureVersion, Page: core.Page{Slug: "concept/time", Title: "时间管理", PageType: "concept", Content: "原内容保留，原件核查后补全。"}})
	if err != nil {
		t.Fatal(err)
	}
	chat := models.Chat{ID: "maintenance-chat", WakerID: "maintainer", ActiveExecutionID: "maintenance-execution"}
	s.config.Chats = selectedChat{chat: chat}
	auth := models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: library.ID, Operation: constants.KnowledgeWikiMaintainOperation}
	run := models.AutomaticRun{ID: "maintenance-run", WakerID: chat.WakerID, Configuration: models.AutomaticTaskConfiguration{Knowledge: &auth}}
	s.config.AutomaticRun = func(context.Context, string) (models.AutomaticRun, error) { return run, nil }
	revoked := false
	s.config.InvocationGuard = func(context.Context, string) (func(), error) {
		if revoked {
			return nil, status_error.IntegrationForbidden
		}
		return func() {}, nil
	}
	registry := agentcapability.New(builtinskills.Guide)
	defer registry.Close()
	grant, activate, revoke, err := registry.Prepare(agentcapability.GrantSpec{ExecutionID: chat.ActiveExecutionID, Bindings: s.AutomaticBindings(ctx, chat, run.ID), Validate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	activate()
	dropNextWriteResponse := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		response := httptest.NewRecorder()
		registry.ServeHTTP(response, request)
		if dropNextWriteResponse && request.Method == http.MethodPost && response.Code == http.StatusOK {
			dropNextWriteResponse = false
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("incomplete response"))
			return
		}
		for key, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	}))
	defer server.Close()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	connection, err := agentcapability.WriteConnection(directory, "context.json", agentcapability.Connection{ProtocolVersion: constants.AgentCLIProtocolVersion, Endpoint: server.URL + constants.AgentCLIPath, CoreEpoch: grant.Epoch, GrantID: grant.ID, Token: grant.Token})
	if err != nil {
		t.Fatal(err)
	}
	cli := func(input string, args ...string) (int, agentcapability.Response) {
		t.Helper()
		var output, errors bytes.Buffer
		code := agentcli.Execute(ctx, args, agentcli.IO{In: strings.NewReader(input), Out: &output, Err: &errors, ContextFile: connection})
		data := output.Bytes()
		if code != 0 {
			data = errors.Bytes()
		}
		var response agentcapability.Response
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatalf("CLI envelope: args=%v code=%d output=%s errors=%s", args, code, &output, &errors)
		}
		return code, response
	}
	success := func(input string, args ...string) agentcapability.Response {
		t.Helper()
		code, response := cli(input, args...)
		if code != 0 || !response.OK {
			t.Fatalf("CLI rejected: args=%v code=%d result=%+v", args, code, response)
		}
		return response
	}
	capabilities := success("", "capabilities")
	encoded, _ := json.Marshal(capabilities.Data)
	for _, operation := range []string{"knowledge.wiki.auto-fix", "knowledge.pages.get", "knowledge.pages.list", "knowledge.sources.read", "knowledge.pages.write", "knowledge.themes.write"} {
		if !bytes.Contains(encoded, []byte(`"`+operation+`"`)) {
			t.Fatalf("maintenance cannot discover %s", operation)
		}
		success("", "describe", operation)
	}
	for _, forbidden := range []string{"knowledge.sources.import", "knowledge.jobs.start", "knowledge.jobs.retry"} {
		if bytes.Contains(encoded, []byte(`"`+forbidden+`"`)) {
			t.Fatalf("maintenance discovered source processing %s", forbidden)
		}
	}
	guide := success("", "guide", "knowledge.maintenance")
	guideJSON, _ := json.Marshal(guide.Data)
	if !bytes.Contains(guideJSON, []byte("knowledge wiki auto-fix")) {
		t.Fatal("maintenance capability does not expose its executable guide")
	}
	success("", "knowledge", "pages", "list", "--library-id", library.ID, "--limit", "10")
	read := success("", "knowledge", "sources", "read", "--library-id", library.ID, "--source-version-id", source.ID, "--limit", "10")
	readJSON, _ := json.Marshal(read.Data)
	if !bytes.Contains(readJSON, []byte("NTP 配置改变后才重启服务")) {
		t.Fatalf("CLI source read lost saved evidence: %s", readJSON)
	}
	fix := success("", "knowledge", "wiki", "auto-fix", "--library-id", library.ID, "--structure-version", strconv.FormatInt(library.StructureVersion, 10), "--request-id", "fix")
	if fix.Completion != constants.AgentCLICompletionImmediate {
		t.Fatalf("deterministic fix incorrectly reported admission only: %+v", fix)
	}
	page, _ := s.config.Engine.GetPage(ctx, library.ID, created.ResourceID)
	currentLibrary, _ := s.config.Engine.GetLibrary(ctx, library.ID)
	folder := success("", "knowledge", "themes", "write", "--library-id", library.ID, "--name", "时间", "--structure-version", strconv.FormatInt(currentLibrary.StructureVersion, 10), "--request-id", "folder")
	folderJSON, _ := json.Marshal(folder.Data)
	var folderResult struct {
		Receipt core.Receipt `json:"receipt"` // 真实目录创建回执。
	}
	if err := json.Unmarshal(folderJSON, &folderResult); err != nil || folderResult.Receipt.ResourceID == "" {
		t.Fatalf("directory result: %s %v", folderJSON, err)
	}
	currentLibrary, _ = s.config.Engine.GetLibrary(ctx, library.ID)
	updatedContent := "原内容保留，原件核查后补全。\n\n已核查 NTP 原件。"
	writeInput, _ := json.Marshal(map[string]any{"libraryId": library.ID, "pageId": page.ID, "expectedVersion": page.Version, "expectedUpdatedAt": page.UpdatedAt.Format(time.RFC3339Nano), "structureVersion": currentLibrary.StructureVersion, "slug": page.Slug, "title": page.Title, "summary": page.Summary, "content": updatedContent, "pageType": page.PageType, "folderId": folderResult.Receipt.ResourceID, "sourceVersionIds": []string{source.ID}})
	dropNextWriteResponse = true
	_, unknown := cli(string(writeInput), "knowledge", "pages", "write", "--input", "-", "--request-id", "uncertain-edit")
	if unknown.Error == nil || unknown.Error.Code != status_error.AgentCommandOutcomeUnknown.Key() || unknown.Error.RetryAdvice != "query_receipt" {
		t.Fatalf("lost response did not preserve uncertainty: %+v error=%+v", unknown, unknown.Error)
	}
	receipt := success("", "receipts", "get", "--operation", "knowledge.pages.write", "--request-id", "uncertain-edit")
	if !receipt.Replayed {
		t.Fatal("CLI did not recover the original durable receipt")
	}
	updated, err := s.config.Engine.GetPage(ctx, library.ID, page.ID)
	if err != nil || updated.Content != updatedContent || updated.FolderID != folderResult.Receipt.ResourceID || len(updated.Evidence.Sources) != 1 {
		t.Fatalf("CLI did not update actual page, placement and evidence: %+v %v", updated, err)
	}
	code, denied := cli("", "knowledge", "pages", "list", "--library-id", "another-library")
	if code == 0 || denied.Error == nil || denied.Error.Code != status_error.KnowledgeForbidden.Key() {
		t.Fatalf("CLI escaped the frozen library: code=%d result=%+v", code, denied)
	}
	revoked = true
	code, denied = cli("", "receipts", "get", "--operation", "knowledge.pages.write", "--request-id", "uncertain-edit")
	if code == 0 || denied.Error == nil || denied.Error.Code != status_error.IntegrationForbidden.Key() {
		t.Fatalf("revoked maintenance recovered a writable authorization: code=%d result=%+v", code, denied)
	}
}
