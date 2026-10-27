package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	builtinskills "github.com/JieWaZi/NeuxBot/server/internal/skills/builtin"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestOrdinaryChatMaintenanceUsesSharedTaskAndFrozenChatSelection(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	guide, err := builtinskills.Guide("knowledge.maintenance")
	if err != nil || guide == "" {
		t.Fatalf("shared maintenance guide unavailable: %v", err)
	}
	current := selectedChat{
		chat:      models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"},
		message:   messageview.Message{ID: "user-message", Content: "整理 Wiki，只合并同版本重复页面，保留来源。"},
		execution: models.Execution{ID: "execution", UserMessageID: "user-message", RuntimeID: "codex", AccountID: "chat-account", Model: "chat-model", Reasoning: "high"},
	}
	s.config.Chats = current
	var start, ingestion agentcapability.Binding
	for _, b := range s.AgentBindings(ctx, current.chat) {
		if b.Operation == "knowledge.wiki.maintain" {
			start = b
		}
		if b.Operation == "knowledge.jobs.start" {
			ingestion = b
		}
	}
	if start.Call == nil || start.Receipt == nil {
		t.Fatal("chat has no Wiki maintenance task entry")
	}
	request := agentcapability.Request{RequestID: "maintain", Params: map[string]any{"libraryId": lib.ID, "message": "发现两个同义目录；检查 concept/time 和 concept/ntp。"}}
	result, err := start.Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	receipt := result.Data.(map[string]any)["receipt"].(*core.Receipt)
	job, err := s.config.Engine.GetJob(ctx, receipt.JobID)
	if err != nil || !core.IsMaintenanceJob(job) || len(job.SourceVersionIDs) != 0 {
		t.Fatalf("wrong task: %+v %v", job, err)
	}
	for _, recover := range []func() (agentcapability.Result, error){
		func() (agentcapability.Result, error) { return start.Call(ctx, request) },
		func() (agentcapability.Result, error) { return start.Receipt(ctx, request.RequestID) },
	} {
		replayed, err := recover()
		if err != nil || replayed.Data.(map[string]any)["receipt"].(*core.Receipt).JobID != job.ID {
			t.Fatalf("replay changed task: %+v %v", replayed, err)
		}
	}
	if _, err := ingestion.Receipt(ctx, request.RequestID); !errors.Is(err, status_error.KnowledgeNotFound) {
		t.Fatalf("cross-operation receipt accepted: %v", err)
	}
	request.Params["message"] = "扩大到全库"
	if _, err := start.Call(ctx, request); !errors.Is(err, status_error.KnowledgeConflict) {
		t.Fatalf("same key changed requirements: %v", err)
	}

	called := make(chan MaintenanceInvocation, 1)
	s.config.RunMaintenance = func(_ context.Context, selection models.LibraryExecutionDefaults, input MaintenanceInvocation) (string, error) {
		if selection.RuntimeID != "codex" || selection.AccountID != "chat-account" || selection.Model != "chat-model" || selection.Reasoning != "high" {
			return "", status_error.KnowledgeInvalid
		}
		called <- input
		return "已合并同版本重复页面，保留来源与不同版本。", nil
	}
	// 接纳后聊天和模型切换不改变独立知识任务。
	s.config.Chats = selectedChat{chat: models.Chat{ID: "chat", ActiveExecutionID: "later"}}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	select {
	case invocation := <-called:
		if !strings.Contains(invocation.Message, current.message.Content) || !strings.Contains(invocation.Message, "发现两个同义目录") || !strings.HasPrefix(invocation.Instructions, guide) {
			t.Fatalf("missing original request, findings or shared prompt: %+v", invocation)
		}
		operations := map[string]bool{}
		for _, b := range invocation.Bindings {
			operations[b.Operation] = true
		}
		if !operations["knowledge.wiki.auto-fix"] || !operations["knowledge.pages.write"] || operations["knowledge.jobs.start"] || operations["knowledge.sources.import"] || operations["knowledge.jobs.retry"] {
			t.Fatalf("wrong maintenance tools: %v", operations)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance was not driven with frozen chat configuration")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err = s.config.Engine.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == "completed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("maintenance did not complete: %+v", job)
}

func TestOrdinaryChatMaintenanceRejectsSourcesRemoteAndRevokedExecution(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	current := selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "execution"}, message: messageview.Message{ID: "message"}}
	s.config.Chats = current
	var start agentcapability.Binding
	for _, b := range s.AgentBindings(ctx, current.chat) {
		if b.Operation == "knowledge.wiki.maintain" {
			start = b
		}
	}
	if start.Call == nil {
		t.Fatal("missing maintenance entry")
	}
	for _, params := range []map[string]any{
		{"libraryId": lib.ID, "sourceVersionIds": []string{"source"}},
		{"libraryId": lib.ID, "attachmentIndexes": []int{0}},
		{"libraryId": lib.ID, "backend": "custom", "connectionId": "remote"},
		{"libraryId": "unseen"},
	} {
		if _, err := start.Call(ctx, agentcapability.Request{RequestID: "invalid", Params: params}); err == nil {
			t.Fatalf("invalid maintenance accepted: %v", params)
		}
	}
	s.config.Chats = selectedChat{chat: models.Chat{ID: "chat", WakerID: "waker", ActiveExecutionID: "other"}}
	if _, err := start.Call(ctx, agentcapability.Request{RequestID: "revoked", Params: map[string]any{"libraryId": lib.ID}}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("revoked execution accepted: %v", err)
	}
	_, total, err := s.config.Engine.ListJobs(ctx, lib.ID, 0, 10)
	if err != nil || total != 0 {
		t.Fatalf("invalid requests admitted tasks: %d %v", total, err)
	}
}

func TestWikiMaintenanceRunsACPWithoutPartnerChatOrSources(t *testing.T) {
	s, lib := newServiceFixture(t)
	ctx := context.Background()
	called := make(chan MaintenanceInvocation, 1)
	s.config.RunMaintenance = func(_ context.Context, _ models.LibraryExecutionDefaults, input MaintenanceInvocation) (string, error) {
		called <- input
		return "已核查现有 Wiki；无须改动。", nil
	}
	r, _, err := s.Organize(ctx, OrganizeCommand{RequestID: "independent-wiki-maintenance", LibraryID: lib.ID, Maintenance: true, Configuration: models.KnowledgeTaskConfiguration{RuntimeID: "codex"}})
	if err != nil || r == nil || r.State != "accepted" {
		t.Fatalf("existing Wiki maintenance was not admitted independently: %+v %v", r, err)
	}
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	select {
	case invocation := <-called:
		if invocation.JobID != r.JobID || invocation.Instructions == "" || len(invocation.Bindings) == 0 {
			t.Fatalf("missing frozen independent scope: %+v", invocation)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("independent ACP maintenance was not driven")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, e := s.config.Engine.GetJob(ctx, r.JobID)
		if e != nil {
			t.Fatal(e)
		}
		if job.State == "completed" {
			if len(job.SourceVersionIDs) != 0 {
				t.Fatal("maintenance acquired original sources as ingestion work")
			}
			stages, e := s.config.Engine.ListStages(ctx, job.ID)
			if e != nil || len(stages) != 1 || stages[0].ResultText != "已核查现有 Wiki；无须改动。" {
				t.Fatalf("missing actual ACP result: %+v %v", stages, e)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ACP result did not complete the knowledge task")
}
