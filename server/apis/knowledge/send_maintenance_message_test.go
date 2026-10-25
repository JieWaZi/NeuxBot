package knowledge

import (
	"context"
	"errors"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	service "github.com/JieWaZi/NeuxBot/server/services/knowledge"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agentcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/builtinrolepreset"
	bridge "github.com/JieWaZi/NeuxBot/server/internal/knowledge"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/JieWaZi/neux-knowledge/core"
)

func TestMaintenanceChatDefaultBindingsCannotAcquireAnotherLibraryOrPage(t *testing.T) {
	s, lib := newMaintenanceFixture(t)
	ctx := context.Background()
	page, err := s.engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "topic", Title: "主题", PageType: "concept", Content: "旧内容"}})
	if err != nil {
		t.Fatal(err)
	}
	otherPage, err := s.engine.WritePage(ctx, core.WritePageInput{RequestID: "other-page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "other", Title: "其他", PageType: "concept", Content: "旧内容"}})
	if err != nil {
		t.Fatal(err)
	}
	otherLib, err := s.engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "other-lib", Name: "其他库", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	intent := execution.WorkIntent{ClientRequestID: "maintenance", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: builtinrolepreset.PersonalAssistantWakerID}}, Input: execution.WorkInput{Content: "修复主题"}}
	bound, err := bindWikiMaintenance(ctx, intent, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: page.ResourceID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	chat := models.Chat{ID: (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", builtinrolepreset.PersonalAssistantWakerID), WakerID: builtinrolepreset.PersonalAssistantWakerID, ActiveExecutionID: bound.ExecutionID}
	s.chats.chat = chat
	bindings := s.AgentBindings(ctx, chat)
	var replace agentcapability.Binding
	var libraryRead agentcapability.Binding
	for _, binding := range bindings {
		if binding.Operation == "knowledge.jobs.start" {
			t.Fatal("maintenance chat acquired whole-library organization")
		}
		if binding.Operation == "knowledge.pages.replace" {
			replace = binding
		}
		if binding.Operation == "knowledge.libraries.get" {
			libraryRead = binding
		}
	}
	if replace.Call == nil || libraryRead.Call == nil {
		t.Fatal("scoped maintenance bindings missing")
	}
	_, err = libraryRead.Call(ctx, agentcapability.Request{Params: map[string]any{"libraryId": otherLib.LibraryID}})
	if !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("outside library read: %v", err)
	}
	params := map[string]any{"libraryId": lib.ID, "pageId": otherPage.ResourceID, "expectedVersion": 1, "structureVersion": lib.StructureVersion, "oldText": "旧内容", "newText": "新内容"}
	_, err = replace.Call(ctx, agentcapability.Request{RequestID: "outside-page", Params: params})
	if !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("outside page write: %v", err)
	}
	params["pageId"] = page.ResourceID
	if _, err = replace.Call(ctx, agentcapability.Request{RequestID: "inside-page", Params: params}); err != nil {
		t.Fatalf("frozen page write: %v", err)
	}
	// 页面已是第二版，续发仍携带首次确认的第一版范围；新一轮工具必须绑定新执行。
	continued, err := bindWikiMaintenance(ctx, execution.WorkIntent{Chat: execution.ChatTarget{ChatID: chat.ID}, Input: execution.WorkInput{SubmissionID: "next-turn", Content: "继续完善"}}, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: page.ResourceID, ExpectedVersion: 1})
	if err != nil {
		t.Fatalf("continuation after saved revision: %v", err)
	}
	s.chats.chat.ActiveExecutionID = continued.ExecutionID
	if _, err = replace.Call(ctx, agentcapability.Request{RequestID: "old-execution", Params: params}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("previous execution retained write authorization: %v", err)
	}
	params["expectedVersion"], params["oldText"], params["newText"] = 2, "新内容", "继续完善后的内容"
	for _, binding := range continued.Options.ToolCapabilities[0].Bindings {
		if binding.Operation == "knowledge.pages.replace" {
			if _, err = binding.Call(ctx, agentcapability.Request{RequestID: "next-write", Params: params}); err != nil {
				t.Fatalf("new execution page revision: %v", err)
			}
		}
	}
	current, err := s.engine.GetPage(ctx, lib.ID, page.ResourceID)
	if err != nil || current.Version != 3 || current.Content != "继续完善后的内容" {
		t.Fatalf("continued revision: %+v %v", current, err)
	}
}

func TestMaintenanceIntentGuardRejectsOrdinaryContinuationAndRetry(t *testing.T) {
	s, lib := newMaintenanceFixture(t)
	ctx := context.Background()
	p, err := s.engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "topic", Title: "主题", PageType: "concept", Content: "正文"}})
	if err != nil {
		t.Fatal(err)
	}
	intent := execution.WorkIntent{ClientRequestID: "maintenance", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: builtinrolepreset.PersonalAssistantWakerID}}, Input: execution.WorkInput{Content: "核查"}}
	bound, err := bindWikiMaintenance(ctx, intent, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: p.ResourceID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateWorkIntent(ctx, bound); err != nil {
		t.Fatalf("specialized creation rejected: %v", err)
	}
	chatID := (requestidempotency.Identity{ID: intent.ClientRequestID}).ResourceID("chat", builtinrolepreset.PersonalAssistantWakerID)
	s.chats.chat = models.Chat{ID: chatID, WakerID: builtinrolepreset.PersonalAssistantWakerID}
	continued, err := bindWikiMaintenance(ctx, execution.WorkIntent{Chat: execution.ChatTarget{ChatID: chatID}, Input: execution.WorkInput{SubmissionID: "next", Content: "继续核查"}}, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: p.ResourceID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateWorkIntent(ctx, continued); err != nil {
		t.Fatalf("specialized continuation rejected: %v", err)
	}
	for _, test := range []struct {
		// name 描述被拒绝的边界变化。
		name string
		// change 只改动一次接纳的授权约束。
		change func(*execution.WorkIntent)
	}{
		{"ordinary continuation", func(in *execution.WorkIntent) { in.Options = execution.WorkOptions{} }},
		{"ordinary retry", func(in *execution.WorkIntent) { in.RetryExecutionID = "failed-execution" }},
		{"writable workspace", func(in *execution.WorkIntent) { in.Options.WorkspaceReadOnly = false }},
		{"inherited domains", func(in *execution.WorkIntent) { in.Options.AllowedAgentDomains = nil }},
		{"inherited connectors", func(in *execution.WorkIntent) { in.Options.AllowedConnectorIDs = nil }},
		{"unsigned tools", func(in *execution.WorkIntent) { in.Options.ToolCapabilities = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := continued
			test.change(&input)
			if err := s.ValidateWorkIntent(ctx, input); !errors.Is(err, status_error.KnowledgeForbidden) {
				t.Fatalf("maintenance bypass accepted: %v", err)
			}
		})
	}
	if err = s.ValidateWorkIntent(ctx, execution.WorkIntent{Chat: execution.ChatTarget{ChatID: "ordinary-chat"}}); err != nil {
		t.Fatalf("ordinary chat rejected: %v", err)
	}
}

func TestMaintenanceWhitespaceFirstRequestKeepsCanonicalChatGuardAndBindings(t *testing.T) {
	s, lib := newMaintenanceFixture(t)
	ctx := context.Background()
	p, err := s.engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "topic", Title: "主题", PageType: "concept", Content: "正文"}})
	if err != nil {
		t.Fatal(err)
	}
	intent := execution.WorkIntent{ClientRequestID: " maintenance ", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: builtinrolepreset.PersonalAssistantWakerID}}, Input: execution.WorkInput{Content: "核查"}}
	bound, err := bindWikiMaintenance(ctx, intent, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: p.ResourceID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := requestidempotency.New(intent.ClientRequestID, nil)
	if err != nil {
		t.Fatal(err)
	}
	chatID := identity.ResourceID("chat", builtinrolepreset.PersonalAssistantWakerID)
	// 首发还未创建Chat时，普通入口同身份及空格变体都必须命中已经登记的维护锚点。
	for _, requestID := range []string{"maintenance", " maintenance "} {
		ordinary := execution.WorkIntent{ClientRequestID: requestID, Chat: intent.Chat, Input: intent.Input}
		if err := s.ValidateWorkIntent(ctx, ordinary); !errors.Is(err, status_error.KnowledgeForbidden) {
			t.Fatalf("ordinary creation %q escaped maintenance guard: %v", requestID, err)
		}
	}
	if err := s.ValidateWorkIntent(ctx, bound); err != nil {
		t.Fatalf("canonical specialized creation rejected: %v", err)
	}
	intent.ClientRequestID = "maintenance"
	replayed, err := bindWikiMaintenance(ctx, intent, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: p.ResourceID, ExpectedVersion: 1})
	if err != nil || replayed.ExecutionID != bound.ExecutionID || bound.ClientRequestID != "maintenance" {
		t.Fatalf("whitespace variant changed accepted execution identity: %+v %+v %v", bound, replayed, err)
	}
	chat := models.Chat{ID: chatID, WakerID: builtinrolepreset.PersonalAssistantWakerID, ActiveExecutionID: bound.ExecutionID}
	s.chats.chat = chat
	if err := s.ValidateWorkIntent(ctx, execution.WorkIntent{Chat: execution.ChatTarget{ChatID: chatID}}); !errors.Is(err, status_error.KnowledgeForbidden) {
		t.Fatalf("actual canonical chat lost its maintenance guard: %v", err)
	}
	var scopedReplace agentcapability.Binding
	for _, binding := range s.AgentBindings(ctx, chat) {
		if binding.Operation == "knowledge.jobs.start" {
			t.Fatal("actual canonical chat acquired whole-library organization")
		}
		if binding.Operation == "knowledge.pages.replace" {
			scopedReplace = binding
		}
	}
	if scopedReplace.Call == nil {
		t.Fatal("actual canonical chat lost its page maintenance tools")
	}
	if _, err := scopedReplace.Call(ctx, agentcapability.Request{RequestID: "replace", Params: map[string]any{"libraryId": lib.ID, "pageId": p.ResourceID, "expectedVersion": 1, "structureVersion": lib.StructureVersion, "oldText": "正文", "newText": "核查后正文"}}); err != nil {
		t.Fatalf("canonical chat page revision failed: %v", err)
	}
	current, err := s.engine.GetPage(ctx, lib.ID, p.ResourceID)
	if err != nil || current.Content != "核查后正文" {
		t.Fatalf("actual page revision: %+v %v", current, err)
	}
}

func TestWikiMaintenanceIntentPinsPageAndUsesExistingExecutionTools(t *testing.T) {
	s, lib := newMaintenanceFixture(t)
	ctx := context.Background()
	r, err := s.engine.WritePage(ctx, core.WritePageInput{RequestID: "page", LibraryID: lib.ID, StructureVersion: lib.StructureVersion, Page: core.Page{Slug: "concept/topic", Title: "主题", PageType: "concept", Content: "正文"}})
	if err != nil {
		t.Fatal(err)
	}
	intent := execution.WorkIntent{ClientRequestID: "start", Chat: execution.ChatTarget{Create: &execution.CreateChatSpec{WakerID: builtinrolepreset.PersonalAssistantWakerID}}, Input: execution.WorkInput{Content: "核查内容"}}
	bound, err := bindWikiMaintenance(ctx, intent, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: r.ResourceID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if bound.Options.AllowedAgentDomains == nil || len(*bound.Options.AllowedAgentDomains) != 1 || (*bound.Options.AllowedAgentDomains)[0] != "interaction" || len(bound.Options.ToolCapabilities) != 1 || !bound.Options.WorkspaceReadOnly {
		t.Fatalf("maintenance tools were not isolated: %+v", bound.Options)
	}
	if len(bound.Options.SupplementalInstructions) == 0 {
		t.Fatal("upstream fixer instructions missing")
	}
	if bound.ExecutionID == "" {
		t.Fatal("maintenance attempt identity missing")
	}
	_, err = bindWikiMaintenance(ctx, intent, wikiMaintenanceTarget{Ref: bridge.LocalRef(lib.ID), PageID: "another-page", ExpectedVersion: 1})
	if err == nil {
		t.Fatal("original request changed page scope")
	}
}

// maintenanceChat 只提供维护入口真实使用的聊天摘要。
type maintenanceChat struct {
	chatservice.Module             // 其余聊天端口在本回归中不调用。
	chat               models.Chat // 当前维护聊天及活跃执行。
}

func (c *maintenanceChat) GetChatSummary(context.Context, string) (chatservice.ChatSummary, error) {
	return chatservice.ChatSummary{Chat: c.chat}, nil
}

// maintenanceFixture 保留独立内容权威、真实宿主引用和共享签发服务。
type maintenanceFixture struct {
	*service.Service                  // 被API和普通执行共同调用的知识服务。
	engine           *core.Engine     // 唯一知识内容权威。
	chats            *maintenanceChat // 已接纳的聊天身份。
}

func newMaintenanceFixture(t *testing.T) (*maintenanceFixture, *core.Library) {
	t.Helper()
	ctx := context.Background()
	engine, err := core.Open(ctx, core.Options{DBPath: filepath.Join(t.TempDir(), "knowledge.db"), RawDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "host.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	refs := controllers.NewKnowledgeController(db.DB)
	chats := &maintenanceChat{}
	shared := service.New(service.Config{Engine: engine, References: refs, Chats: chats, Projects: controllers.NewProjectController(db.DB)})
	oldEngine, oldRefs, oldService, oldChats := global.KnowledgeEngine, global.KnowledgeControllerManager, global.KnowledgeService, global.ChatService
	global.KnowledgeEngine, global.KnowledgeControllerManager, global.KnowledgeService, global.ChatService = engine, refs, shared, chats
	t.Cleanup(func() {
		global.KnowledgeEngine, global.KnowledgeControllerManager, global.KnowledgeService, global.ChatService = oldEngine, oldRefs, oldService, oldChats
	})
	receipt, err := engine.CreateLibrary(ctx, core.CreateLibraryInput{RequestID: "library", Name: "知识", Kind: "general", WikiLanguage: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := engine.GetLibrary(ctx, receipt.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	return &maintenanceFixture{Service: shared, engine: engine, chats: chats}, lib
}
