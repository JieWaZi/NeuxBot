package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/neux-knowledge/core"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"testing"
)

func newServiceFixture(t *testing.T) (*Service, *core.Library) {
	t.Helper()
	s, library, _, _ := newServiceFixtureWithDB(t)
	return s, library
}
func newServiceFixtureWithDB(t *testing.T) (*Service, *core.Library, string, *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	knowledgePath := filepath.Join(t.TempDir(), "knowledge.db")
	engine, err := core.Open(ctx, core.Options{DBPath: knowledgePath, RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "ally.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { sqlDB.Close() })
	refs := controllers.NewKnowledgeController(db.DB)
	r, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "知识", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	library, _ := engine.GetLibrary(ctx, r.ResourceID)
	s := New(Config{ResolveModel: func(_ context.Context, v models.LibraryExecutionDefaults) (models.LibraryExecutionDefaults, error) {
		if v.RuntimeID == "" {
			v.RuntimeID = constants.RuntimeIDCodex
		}
		return v, nil
	}, Engine: engine, References: refs, Projects: controllers.NewProjectController(db.DB)})
	return s, library, knowledgePath, db.DB
}
func TestOrganizeNoopIsDurableAndSameRequestCannotAcquireLaterRaw(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	input := OrganizeCommand{RequestID: "organize", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}}
	first, _, err := s.Organize(ctx, input)
	if err != nil || first.State != "no-op" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	_, err = s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "later", LibraryID: lib.ID, Filename: "later.md", Bytes: []byte("Later evidence")})
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := s.Organize(ctx, input)
	if err != nil || replay.State != "no-op" || replay.ResourceID != "" {
		t.Fatalf("replay acquired new source: %+v err=%v", replay, err)
	}
	input.Message = "different"
	if _, _, err := s.Organize(ctx, input); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("different same-key input=%v", err)
	}
}

// selectedChat 只公开当前已接纳Execution及其原始用户附件引用。
type selectedChat struct {
	// Service 保留真实Chat端口形状，未用方法不得调用。
	chat.Service
	// chat 保存实际受管工作区。
	chat models.Chat
	// message 保存当前执行的用户消息。
	message messageview.Message
	// execution 保存本轮实际身份，知识任务应使用独立库模型配置。
	execution models.Execution
}

func (c selectedChat) GetChatSummary(context.Context, string) (chat.ChatSummary, error) {
	return chat.ChatSummary{Chat: c.chat}, nil
}
func (c selectedChat) ReadExecution(context.Context, string, string) (models.Execution, error) {
	if c.execution.ID != "" {
		return c.execution, nil
	}
	return models.Execution{ID: c.chat.ActiveExecutionID, UserMessageID: c.message.ID}, nil
}
func (c selectedChat) ReadMessageHistory(context.Context, string, int64, int) (chat.MessageHistoryPage, error) {
	return chat.MessageHistoryPage{Resources: []messageview.Message{c.message}}, nil
}

func TestOrdinaryChatStartsOnlyRequestedAttachmentWithoutComposerAuthorization(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	if _, err := s.config.References.UpdateLibraryExecutionDefaults(ctx, "knowledge-defaults", 0, models.LibraryExecutionDefaults{LibraryID: lib.ID, RuntimeID: constants.RuntimeIDClaude, AccountID: "knowledge-account", Model: "knowledge-model"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "unrelated", LibraryID: lib.ID, Filename: "unrelated.md", Bytes: []byte("Unrelated source")})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "second.md"), []byte("Requested evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	current := selectedChat{
		chat: models.Chat{ID: "chat", WakerID: "waker", WorkspaceDirectory: root, ActiveExecutionID: "execution"},
		message: messageview.Message{ID: "user-message", Content: "只整理第二份附件", Attachments: []eventlog.AttachmentReference{
			{Name: "first.md", Path: "first.md", Managed: true},
			{Name: "second.md", Path: "second.md", MediaType: "text/markdown", Managed: true},
		}},
		execution: models.Execution{ID: "execution", UserMessageID: "user-message", RuntimeID: constants.RuntimeIDCodex, Model: "chosen-model", PermissionLevel: constants.PermissionLevelFullAccess},
	}
	s.config.Chats = current
	var start agentcapability.Binding
	for _, binding := range s.AgentBindings(ctx, current.chat) {
		if binding.Operation == "knowledge.jobs.start" {
			start = binding
		}
	}
	if start.Call == nil || start.Receipt == nil {
		t.Fatal("ordinary chat has no organization command")
	}
	request := agentcapability.Request{RequestID: "natural-request", Params: map[string]any{"libraryId": lib.ID, "attachmentIndexes": []int{1}, "message": "按主题提炼"}}
	result, err := start.Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	receipt := result.Data.(map[string]any)["receipt"].(*core.Receipt)
	job, err := s.config.Engine.GetJob(ctx, receipt.ResourceID)
	if err != nil || len(job.SourceVersionIDs) != 1 {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	source, err := s.config.Engine.GetSource(ctx, lib.ID, job.SourceVersionIDs[0])
	if err != nil || source.Filename != "second.md" {
		t.Fatalf("wrong attachment imported: %+v err=%v", source, err)
	}
	link, err := s.config.References.GetOperation(ctx, receipt.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.config.References.GetTaskConfiguration(ctx, link.ConfigurationID)
	if err != nil || session.RuntimeID != constants.RuntimeIDClaude || session.AccountID != "knowledge-account" || session.Model != "knowledge-model" || session.Message != "按主题提炼" {
		t.Fatalf("knowledge configuration inherited chat settings: %+v err=%v", session, err)
	}
	for _, recover := range []func() (agentcapability.Result, error){
		func() (agentcapability.Result, error) { return start.Call(ctx, request) },
		func() (agentcapability.Result, error) { return start.Receipt(ctx, request.RequestID) },
	} {
		replayed, err := recover()
		if err != nil || replayed.Data.(map[string]any)["receipt"].(*core.Receipt).ResourceID != job.ID {
			t.Fatalf("replay=%+v err=%v", replayed, err)
		}
	}
	request.Params["attachmentIndexes"] = []int{0}
	if _, err := start.Call(ctx, request); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("same request changed attachment: %v", err)
	}
	_, total, err := s.config.Engine.ListSources(ctx, lib.ID, 0, 50)
	if err != nil || total != 2 {
		t.Fatalf("unexpected imports: total=%d err=%v", total, err)
	}
}

func TestOrdinaryChatOrganizeRejectsExpandedScopeAndInvalidAttachments(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	current := selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}, message: messageview.Message{ID: "user", Attachments: []eventlog.AttachmentReference{{Name: "one.md", Path: "one.md", Managed: true}}}}
	s.config.Chats = current
	var start agentcapability.Binding
	for _, binding := range s.AgentBindings(ctx, current.chat) {
		if binding.Operation == "knowledge.jobs.start" {
			start = binding
		}
	}
	if start.Call == nil {
		t.Fatal("ordinary chat has no organization command")
	}
	later, err := s.config.Engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "later", Name: "Later", Kind: "general", WikiLanguage: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	for _, params := range []map[string]any{
		{},
		{"libraryId": later.ResourceID},
		{"libraryId": lib.ID, "backend": "custom", "connectionId": "remote"},
		{"libraryId": lib.ID, "attachmentIndexes": []int{1}},
		{"libraryId": lib.ID, "attachmentIndexes": []int{0, 0}},
		{"libraryId": lib.ID, "attachmentIndexes": []int{-1}},
	} {
		if _, err := start.Call(ctx, agentcapability.Request{RequestID: "invalid", Params: params}); err == nil {
			t.Fatalf("expanded or invalid request accepted: %v", params)
		}
	}
	current.chat.ActiveExecutionID = "later-execution"
	s.config.Chats = current
	if _, err := start.Call(ctx, agentcapability.Request{RequestID: "stale", Params: map[string]any{"libraryId": lib.ID}}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("old execution imported later message: %v", err)
	}
}

func TestOrdinaryChatNoopReplayDoesNotAcquireLaterPendingMaterials(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	current := selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}, message: messageview.Message{ID: "user", Content: "整理库内待处理资料"}}
	s.config.Chats = current
	var start agentcapability.Binding
	for _, binding := range s.AgentBindings(ctx, current.chat) {
		if binding.Operation == "knowledge.jobs.start" {
			start = binding
		}
	}
	if start.Call == nil {
		t.Fatal("ordinary chat has no organization command")
	}
	request := agentcapability.Request{RequestID: "empty", Params: map[string]any{"libraryId": lib.ID}}
	result, err := start.Call(ctx, request)
	if err != nil || !result.Data.(map[string]any)["noop"].(bool) {
		t.Fatalf("first=%+v err=%v", result, err)
	}
	if _, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "later", LibraryID: lib.ID, Filename: "later.md", Bytes: []byte("Later source")}); err != nil {
		t.Fatal(err)
	}
	replayed, err := start.Call(ctx, request)
	if err != nil || !replayed.Data.(map[string]any)["noop"].(bool) {
		t.Fatalf("replay acquired later sources: %+v err=%v", replayed, err)
	}
	_, total, err := s.config.Engine.ListJobs(ctx, lib.ID, 0, 50)
	if err != nil || total != 0 {
		t.Fatalf("replay created work: jobs=%d err=%v", total, err)
	}
}

// interruptedJobReference 模拟核心已接纳而宿主回执引用尚未补齐的故障。
type interruptedJobReference struct {
	// KnowledgeController 其余操作继续使用真实临时数据库。
	controllers.KnowledgeController
	// failed 确保只中断第一次任务引用补齐。
	failed bool
}

func (c *interruptedJobReference) CompleteOperation(ctx context.Context, id, library, resource, job string) error {
	if !c.failed && job != "" {
		c.failed = true
		return status_error.DatabaseError
	}
	return c.KnowledgeController.CompleteOperation(ctx, id, library, resource, job)
}

func TestOrdinaryChatReceiptRepairsInterruptedAcceptance(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	s.config.References = &interruptedJobReference{KnowledgeController: s.config.References}
	current := selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}, message: messageview.Message{ID: "user", Content: "整理待处理资料"}}
	s.config.Chats = current
	if _, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "source", LibraryID: lib.ID, Filename: "source.md", Bytes: []byte("Evidence")}); err != nil {
		t.Fatal(err)
	}
	var start agentcapability.Binding
	for _, binding := range s.AgentBindings(ctx, current.chat) {
		if binding.Operation == "knowledge.jobs.start" {
			start = binding
		}
	}
	request := agentcapability.Request{RequestID: "interrupted", Params: map[string]any{"libraryId": lib.ID}}
	if _, err := start.Call(ctx, request); !errors.Is(err, status_error.DatabaseError) {
		t.Fatalf("expected interruption after core acceptance: %v", err)
	}
	result, err := start.Receipt(ctx, request.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := result.Data.(map[string]any)["receipt"].(*core.Receipt)
	for _, id := range []string{receipt.RequestID, receipt.RequestID + "/input"} {
		link, err := s.config.References.GetOperation(ctx, id)
		if err != nil || !link.ReceiptConfirmed || link.ResourceID != receipt.ResourceID {
			t.Fatalf("acceptance not repaired: %+v err=%v", link, err)
		}
	}
	if _, err := start.Call(ctx, request); err != nil {
		t.Fatal(err)
	}
	_, total, err := s.config.Engine.ListJobs(ctx, lib.ID, 0, 50)
	if err != nil || total != 1 {
		t.Fatalf("recovery repeated acceptance: jobs=%d err=%v", total, err)
	}
}
func TestExplicitChatAttachmentSelectionExcludesOtherPendingRaw(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	_, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "unrelated", LibraryID: lib.ID, Filename: "unrelated.md", Bytes: []byte("Unrelated pending source")})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "attachments"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "attachments", "selected.md"), []byte("Selected evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	s.config.Chats = selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", WorkspaceDirectory: root, ActiveExecutionID: "execution"}, message: messageview.Message{ID: "user-message", Attachments: []eventlog.AttachmentReference{{Name: "ignored.md", Path: "attachments/ignored.md", Managed: true}, {Name: "selected.md", Path: "attachments/selected.md", MediaType: "text/markdown", Managed: true}}}}
	intent := execution.WorkIntent{Chat: execution.ChatTarget{ChatID: "chat"}, Input: execution.WorkInput{Content: "只整理第二份附件", Attachments: []execution.AttachmentInput{{SourcePath: "ignored"}, {SourcePath: "selected"}}}, Options: execution.WorkOptions{RuntimeID: constants.RuntimeIDCodex}}
	bound, err := s.BindIntent(ctx, intent, models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: lib.ID, Operation: "organize", AttachmentIndexes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := bound.Options.ToolCapabilities[0].Bindings[0].Call(ctx, agentcapability.Request{RequestID: "selected-job"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := result.Data.(map[string]any)["receipt"].(*core.Receipt)
	job, err := s.config.Engine.GetJob(ctx, receipt.ResourceID)
	if err != nil || len(job.SourceVersionIDs) != 1 {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	source, _ := s.config.Engine.GetSource(ctx, lib.ID, job.SourceVersionIDs[0])
	if source.Filename != "selected.md" {
		t.Fatalf("wrong attachment imported: %+v", source)
	}
	if _, _, err := s.config.Engine.ListJobs(ctx, lib.ID, 0, 50); err != nil {
		t.Fatal(err)
	}
}

func TestFailedJobRetryKeepsSameJobFrozenSourcesAndDoesNotExpandPending(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	first, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "first", LibraryID: lib.ID, Filename: "first.md", Bytes: []byte("first evidence")})
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "organize-failed", LibraryID: lib.ID, SourceVersionIDs: []string{first.ResourceID}, ExplicitSources: true, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := s.config.Engine.GetJob(ctx, accepted.JobID)
	if _, err := s.config.Engine.FailJob(ctx, core.FailJobInput{JobActionInput: core.JobActionInput{RequestID: "failed", JobID: job.ID, ExpectedVersion: job.Version}, Diagnostic: "model response missing", Sources: []core.SourceConclusion{{SourceVersionID: first.ResourceID, State: "failed", Diagnostic: "model response missing"}}}); err != nil {
		t.Fatal(err)
	}
	failed, _ := s.config.Engine.GetJob(ctx, job.ID)
	if _, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "later", LibraryID: lib.ID, Filename: "later.md", Bytes: []byte("unrelated pending")}); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Retry(ctx, "retry-failed", job.ID, failed.Version)
	if err != nil {
		t.Fatal(err)
	}
	retried, _ := s.config.Engine.GetJob(ctx, receipt.ResourceID)
	if retried.ID != job.ID || retried.Attempt != 2 || len(retried.SourceVersionIDs) != 1 || retried.SourceVersionIDs[0] != first.ResourceID {
		t.Fatalf("retry expanded frozen job %+v", retried)
	}
	replay, err := s.Retry(ctx, "retry-failed", job.ID, failed.Version)
	if err != nil || replay.ResourceID != job.ID {
		t.Fatalf("retry replay=%+v err=%v", replay, err)
	}
	after, _ := s.config.Engine.GetJob(ctx, job.ID)
	if after.Attempt != 2 {
		t.Fatalf("replay added attempt %+v", after)
	}
}

func TestOrganizeDefaultSelectsFailedHead(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	source, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "failed-source", LibraryID: lib.ID, Filename: "failed.md", Bytes: []byte("evidence")})
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "organize-once", LibraryID: lib.ID, SourceVersionIDs: []string{source.ResourceID}, ExplicitSources: true, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := s.config.Engine.GetJob(ctx, accepted.JobID)
	if _, err := s.config.Engine.FailJob(ctx, core.FailJobInput{JobActionInput: core.JobActionInput{RequestID: "mark-failed", JobID: job.ID, ExpectedVersion: job.Version}, Diagnostic: "model request failed", Sources: []core.SourceConclusion{{SourceVersionID: source.ResourceID, State: "failed", Diagnostic: "model request failed"}}}); err != nil {
		t.Fatal(err)
	}
	receipt, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "organize-failed-default", LibraryID: lib.ID, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: constants.RuntimeIDCodex}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State == "no-op" {
		t.Fatal("expected failed head to be selected for default organize")
	}
	next, _ := s.config.Engine.GetJob(ctx, receipt.JobID)
	if len(next.SourceVersionIDs) != 1 || next.SourceVersionIDs[0] != source.ResourceID {
		t.Fatalf("unexpected frozen sources %+v", next.SourceVersionIDs)
	}
}

func TestOrganizeUsesLibraryDefaultsAndFreezesConfigurationBeforeLaterSettings(t *testing.T) {
	s, library := newServiceFixture(t)
	ctx := context.Background()
	if _, err := s.config.Engine.ImportSource(ctx, core.ImportSourceInput{RequestID: "defaults-raw", LibraryID: library.ID, Filename: "defaults.md", Bytes: []byte("# Existing evidence\nPreserve accepted execution configuration.")}); err != nil {
		t.Fatal(err)
	}
	saved, err := s.config.References.UpdateLibraryExecutionDefaults(ctx, "defaults-first", 0, models.LibraryExecutionDefaults{LibraryID: library.ID, RuntimeID: constants.RuntimeIDCodex, AccountID: "original-account", Model: "original-model"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, session, err := s.Organize(ctx, OrganizeCommand{RequestID: "use-defaults", LibraryID: library.ID, Configuration: models.KnowledgeTaskConfiguration{}})
	if err != nil || receipt == nil || receipt.ResourceID == "" || session.Model != "original-model" || session.AccountID != "original-account" {
		t.Fatalf("saved selection not used: %+v %v", session, err)
	}
	saved.Model = "later-model"
	if _, err = s.config.References.UpdateLibraryExecutionDefaults(ctx, "defaults-later", saved.Version, saved); err != nil {
		t.Fatal(err)
	}
	_, replayed, err := s.Organize(ctx, OrganizeCommand{RequestID: "use-defaults", LibraryID: library.ID, Configuration: models.KnowledgeTaskConfiguration{}})
	if err != nil || replayed.Model != "original-model" {
		t.Fatalf("accepted session drifted: %+v %v", replayed, err)
	}
}
