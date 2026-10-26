package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestLibraryAutoFixBindingKeepsScopeAndDurableReceipt(t *testing.T) {
	s, library := newServiceFixture(t)
	ctx := context.Background()
	chat := models.Chat{ID: "maintenance-chat", WakerID: "maintainer", ActiveExecutionID: "active"}
	s.config.Chats = selectedChat{chat: chat}
	bindings, err := s.LibraryMaintenanceBindings(ctx, chat, bridge.LocalRef(library.ID))
	if err != nil {
		t.Fatal(err)
	}
	var fix agentcapability.Binding
	for _, binding := range bindings {
		if binding.Operation == "knowledge.wiki.auto-fix" {
			fix = binding
		}
	}
	if fix.Call == nil || fix.Receipt == nil {
		t.Fatal("upstream AutoFix is not exposed to whole-library maintenance")
	}
	params := map[string]any{"libraryId": "another-library", "structureVersion": library.StructureVersion}
	if _, err := fix.Call(ctx, agentcapability.Request{RequestID: "outside", Params: params}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("AutoFix changed library scope: %v", err)
	}
	params["libraryId"] = library.ID
	if _, err := fix.Call(ctx, agentcapability.Request{RequestID: "upstream-fix", Params: params}); err != nil {
		t.Fatal(err)
	}
	if result, err := fix.Receipt(ctx, "upstream-fix"); err != nil || !result.Replayed {
		t.Fatalf("durable AutoFix receipt: %+v %v", result, err)
	}
	chat.ActiveExecutionID = "later"
	s.config.Chats = selectedChat{chat: chat}
	if _, err := fix.Receipt(ctx, "upstream-fix"); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("prior execution recovered a maintenance receipt: %v", err)
	}
}

func TestAutomaticMaintenanceUpdatesOnlyFrozenLibraryAndRevokesTools(t *testing.T) {
	s, library := newServiceFixture(t)
	ctx := context.Background()
	page, err := s.config.Engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: library.ID, StructureVersion: library.StructureVersion, Page: core.Page{Slug: "concept/topic", Title: "主题", PageType: "concept", Content: "待核查正文"}})
	if err != nil {
		t.Fatal(err)
	}
	auth := models.KnowledgeMaintenanceAuthorization{Backend: "local", ConnectionID: "local", KnowledgeBaseID: library.ID, Operation: "maintain"}
	chat := models.Chat{ID: "maintenance-chat", WakerID: "maintainer", ActiveExecutionID: "maintenance-execution"}
	s.config.Chats = selectedChat{chat: chat}
	run := models.AutomaticRun{ID: "maintenance-run", WakerID: chat.WakerID, Configuration: models.AutomaticTaskConfiguration{Knowledge: &auth}}
	s.config.AutomaticRun = func(context.Context, string) (models.AutomaticRun, error) { return run, nil }
	revoked := false
	s.config.InvocationGuard = func(context.Context, string) (func(), error) {
		if revoked {
			return nil, status_error.IntegrationForbidden
		}
		return func() {}, nil
	}
	intent := execution.WorkIntent{AutomaticRunID: run.ID, ExecutionID: chat.ActiveExecutionID, Chat: execution.ChatTarget{ChatID: chat.ID}, Input: execution.WorkInput{Content: "核查并整理已有 Wiki"}}
	bound, err := s.BindIntent(ctx, intent, auth)
	if err != nil {
		t.Fatalf("maintenance task rejected: %v", err)
	}
	if bound.Options.WorkspaceReadOnly || bound.Options.PermissionLevel != constants.PermissionLevelFullAccess || len(bound.Options.SupplementalInstructions) == 0 {
		t.Fatal("maintenance did not retain the full-access upstream fixer workflow")
	}
	if err := s.ValidateWorkIntent(ctx, bound); err != nil {
		t.Fatalf("authorized automatic maintenance was rejected: %v", err)
	}
	if err := s.ValidateWorkIntent(ctx, execution.WorkIntent{Chat: execution.ChatTarget{ChatID: chat.ID}}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("ordinary continuation escaped automatic maintenance scope: %v", err)
	}
	bindings := s.AutomaticBindings(ctx, chat, run.ID)
	var replace agentcapability.Binding
	for _, binding := range bindings {
		if binding.Operation == "knowledge.sources.import" || binding.Operation == "knowledge.jobs.start" || binding.Operation == "knowledge.jobs.retry" {
			t.Fatalf("wiki maintenance acquired source processing: %s", binding.Operation)
		}
		if binding.Operation == "knowledge.pages.replace" {
			replace = binding
		}
	}
	if replace.Call == nil {
		t.Fatal("automatic restore lost Wiki maintenance tools")
	}
	params := map[string]any{"libraryId": "different-library", "pageId": page.ResourceID, "expectedVersion": 1, "structureVersion": library.StructureVersion, "oldText": "待核查正文", "newText": "已核查正文"}
	if _, err := replace.Call(ctx, agentcapability.Request{RequestID: "outside", Params: params}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("different library accepted: %v", err)
	}
	params["libraryId"] = library.ID
	if _, err := replace.Call(ctx, agentcapability.Request{RequestID: "inside", Params: params}); err != nil {
		t.Fatal(err)
	}
	current, err := s.config.Engine.GetPage(ctx, library.ID, page.ResourceID)
	if err != nil || current.Content != "已核查正文" {
		t.Fatalf("actual page update: %+v %v", current, err)
	}
	revoked = true
	params["expectedVersion"], params["oldText"], params["newText"] = 2, "已核查正文", "越权修改"
	if _, err := replace.Call(ctx, agentcapability.Request{RequestID: "after-revoke", Params: params}); !errors.Is(err, status_error.IntegrationForbidden) {
		t.Fatalf("revoked maintenance tool remained writable: %v", err)
	}
}
