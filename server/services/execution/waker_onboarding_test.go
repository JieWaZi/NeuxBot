package execution

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

type profileEditTestJournal struct {
	// agentTestJournal supplies the active chat and execution identity.
	*agentTestJournal
	// facts captures committed command events for the active turn.
	facts []eventlog.AppendInput
}

func (j *profileEditTestJournal) AppendExecutionFacts(_ context.Context, _ string, facts []eventlog.AppendInput) ([]eventlog.Record, error) {
	j.facts = append(j.facts, facts...)
	return nil, nil
}

func TestWakerOnboardingSavesAfterInterviewWithoutConfirmation(t *testing.T) {
	ctx := t.Context()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "partner.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	hub := events.New()
	t.Cleanup(hub.Close)
	database := controllers.WithEventPublisher(db.DB, hub)
	wakers := controllers.NewWakerController(database)
	profiles := controllers.NewProfileDocumentController(database)
	chatID := (requestidempotency.Identity{ID: "partner"}).ResourceID("chat", "partner")
	chat := models.Chat{ID: chatID, WakerID: "partner"}
	if err := wakers.Create(ctx, models.Waker{
		ID: chat.WakerID, Name: "Partner", Bio: "An overview", OnboardingStatus: constants.WakerOnboardingStatusPending, OnboardingChatID: chatID,
	}); err != nil {
		t.Fatal(err)
	}
	changes := hub.Subscribe("resources", events.Resource{Kind: "waker", ID: chat.WakerID})
	defer changes.Close()

	store := conversationfixture.New(t, t.TempDir())
	journal := &consentTestJournal{interactionTestJournal: &interactionTestJournal{store: store}, updates: make(chan eventlog.Record, 24)}
	if _, err := conversationfixture.CommitFacts(ctx, store, chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WakerID: chat.WakerID})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "first"},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "first"},
	}); err != nil {
		t.Fatal(err)
	}
	core := &service{
		wakerController: wakers, profileController: profiles,
		onboardingCommands:     controllers.NewAgentCommandController(database),
		onboardingTransactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database)),
		elicitations:           newElicitationCoordinator(journal, hub),
	}
	var stable, transient []agent.Instruction
	bindings, err := core.prepareWakerOnboarding(ctx, chat, "first", pendingMessage{}, &stable, &transient)
	if err != nil || len(bindings) != 2 || len(stable) != 1 || !strings.Contains(stable[0].Content, "An overview") || len(transient) != 0 {
		t.Fatalf("initial setup scope = %+v, instructions = %+v %+v, error = %v", bindings, stable, transient, err)
	}
	if _, err := bindings[0].Call(ctx, capability.Request{Operation: constants.WakerProfileSaveOperation, RequestID: "bypass", Params: map[string]any{"content": "Draft without an interview"}}); !errors.Is(err, status_error.WakerOnboardingInterviewRequired) {
		t.Fatalf("a profile draft bypassed the interactive interview: %v", err)
	}
	question := bindings[1]
	input := capability.Request{Operation: constants.WakerProfileInterviewOperation, RequestID: "interview", Params: map[string]any{
		"message": "Working agreement",
		"questions": []map[string]any{
			{"id": "role", "title": "Which work should I own?", "options": []map[string]any{
				{"id": "review", "label": "Review drafts"},
				{"id": "sources", "label": "Compare sources"},
			}},
			{"id": "style", "title": "How should we work together?", "options": []map[string]any{
				{"id": "direct", "label": "Send a concise summary"},
				{"id": "detail", "label": "Provide a full explanation"},
			}},
		},
	}}
	result, err := question.Call(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bindings[0].Call(ctx, capability.Request{Operation: constants.WakerProfileSaveOperation, RequestID: "bypass", Params: map[string]any{"content": "Draft while unanswered"}}); !errors.Is(err, status_error.WakerOnboardingInterviewRequired) {
		t.Fatalf("a profile draft bypassed an unanswered interview: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"role": []any{"review"}},
		{"role": "review", "style": []any{"detail"}},
		{"role": []any{"unlisted"}, "style": []any{"detail"}},
		{"role": []any{"review", "review"}, "style": []any{"detail"}},
		{"role": []any{}, "style": []any{"detail"}},
		{"role": []any{"review"}, "style__other": "   "},
		{"role": []any{"review"}, "style": []any{"detail"}, "style__other": "   "},
	} {
		if err := core.elicitations.resolveAccepted(ctx, chatID, result.CommandID, invalid); err == nil {
			t.Fatalf("invalid choice/Other answer accepted: %+v", invalid)
		}
	}
	if err := core.elicitations.resolveAccepted(ctx, chatID, result.CommandID, map[string]any{
		"role": []any{"review", "sources"}, "role__other": "Check release notes",
		"style": []any{"direct"}, "style__other": "Short and clear",
	}); err != nil {
		t.Fatal(err)
	}
	requireUnpublished := func() {
		t.Helper()
		waker, err := wakers.Get(ctx, chat.WakerID)
		if err != nil || waker.OnboardingStatus != constants.WakerOnboardingStatusPending || waker.RoleName != "" || waker.Bio != "An overview" || len(waker.Expertise) != 0 {
			t.Fatalf("uncommitted data was published: %+v, %v", waker, err)
		}
		if documents, err := profiles.ListWakerDocuments(ctx, waker.ID); err != nil || len(documents) != 0 {
			t.Fatalf("unconfirmed document became authoritative: %+v, %v", documents, err)
		}
		select {
		case event := <-changes.Events:
			t.Fatalf("uncommitted data emitted %v", event)
		default:
		}
	}

	const body = "\n# Working agreement\n\nUse the agreed process.  \n"
	const roleName = "文档评审员"
	const bio = "复核草稿、核对来源，并优先给出结论。"
	request := capability.Request{Operation: constants.WakerProfileSaveOperation, RequestID: "save", Params: map[string]any{
		"roleName":    " " + roleName + " ",
		"bio":         " " + bio + " ",
		"expertise":   []map[string]any{{"title": "文档评审 · 问题定位", "description": "复核草稿"}},
		"workStyle":   []string{"结论优先"},
		"workMethods": []map[string]any{{"name": "评审流程", "steps": []string{"核对来源"}}},
		"content":     body,
	}}
	chinese := request
	chinese.Params = maps.Clone(request.Params)
	chinese.Params["content"] = "# 中文档案"
	if _, err := bindings[0].Call(ctx, chinese); !errors.Is(err, status_error.AgentInputInvalid) {
		t.Fatalf("Chinese instruction body = %v", err)
	}
	incomplete := request
	incomplete.Params = maps.Clone(request.Params)
	incomplete.Params["workMethods"] = []map[string]any{{"name": "评审流程", "steps": []string{" "}}}
	if _, err := bindings[0].Call(ctx, incomplete); !errors.Is(err, status_error.AgentInputInvalid) {
		t.Fatalf("method with no usable steps = %v", err)
	}
	for _, field := range []string{"roleName", "bio"} {
		blank := request
		blank.Params = maps.Clone(request.Params)
		blank.Params[field] = " "
		if _, err := bindings[0].Call(ctx, blank); !errors.Is(err, status_error.AgentInputInvalid) {
			t.Fatalf("blank %s = %v", field, err)
		}
	}
	if _, err := core.saveWakerProfile(ctx, models.Chat{ID: "other-chat", WakerID: chat.WakerID}, request); !errors.Is(err, status_error.AgentContextUnauthorized) {
		t.Fatalf("cross-chat save = %v", err)
	}

	// Pending partners resume from the original accepted interview after a Core restart.
	if _, err := conversationfixture.CommitFacts(ctx, store, chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: "first", Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "next"},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "next"},
	}); err != nil {
		t.Fatal(err)
	}
	core.elicitations = newElicitationCoordinator(journal, hub)
	stable, transient = nil, nil
	bindings, err = core.prepareWakerOnboarding(ctx, chat, "next", pendingMessage{}, &stable, &transient)
	if err != nil || len(bindings) != 2 || len(transient) != 1 ||
		!strings.Contains(transient[0].Content, `"role":["review","sources"]`) ||
		!strings.Contains(transient[0].Content, "Check release notes") ||
		!strings.Contains(transient[0].Content, "Short and clear") {
		t.Fatalf("interview answers not recovered: %+v, %v", transient, err)
	}
	if err := db.DB.Exec("CREATE TRIGGER reject_receipt BEFORE INSERT ON agent_command_receipts BEGIN SELECT RAISE(ABORT, 'receipt denied'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := bindings[0].Call(ctx, request); err == nil {
		t.Fatal("save succeeded with an uncommitted receipt")
	}
	requireUnpublished()
	if err := db.DB.Exec("DROP TRIGGER reject_receipt").Error; err != nil {
		t.Fatal(err)
	}
	saved, err := bindings[0].Call(ctx, request)
	if err != nil || saved.Replayed || saved.Data.(map[string]any)["saved"] != true {
		t.Fatalf("save receipt = %+v, %v", saved, err)
	}
	replay, err := bindings[0].Call(ctx, request)
	if err != nil || !replay.Replayed || replay.CommandID != saved.CommandID {
		t.Fatalf("save replay = %+v, %v", replay, err)
	}
	changed := request
	changed.Params = maps.Clone(request.Params)
	changed.Params["content"] = "# Changed working agreement"
	if _, err := bindings[0].Call(ctx, changed); !errors.Is(err, status_error.AgentCommandRequestConflict) {
		t.Fatalf("changed content with the original request ID = %v", err)
	}
	document, err := profiles.GetWakerDocument(ctx, chat.WakerID, constants.ProfileDocumentKindAgents)
	if err != nil || document.Content != body {
		t.Fatalf("saved body differs from submitted body: %q, %v", document.Content, err)
	}
	waker, err := wakers.Get(ctx, chat.WakerID)
	if err != nil || waker.Name != "Partner" || waker.RoleName != roleName || waker.Bio != bio {
		t.Fatalf("saved partner name, role, or bio differs: %+v, %v", waker, err)
	}
	if err != nil || waker.OnboardingStatus != constants.WakerOnboardingStatusReady || len(waker.Expertise) != 1 || waker.Expertise[0].Description != "复核草稿" || len(waker.WorkStyle) != 1 || waker.WorkStyle[0] != "结论优先" || len(waker.WorkMethods) != 1 || waker.WorkMethods[0].Steps[0] != "核对来源" {
		t.Fatalf("saved display or onboarding state differs: %+v, %v", waker, err)
	}
	select {
	case change := <-changes.Events:
		if change.Type != "resources.changed" || change.Resource.ID != chat.WakerID {
			t.Fatalf("resource change = %+v", change)
		}
	default:
		t.Fatal("committed profile did not emit resource change")
	}
	select {
	case change := <-changes.Events:
		t.Fatalf("receipt replay emitted a second change: %+v", change)
	default:
	}
	stable, transient = nil, nil
	bindings, err = core.prepareWakerOnboarding(ctx, chat, "normal", pendingMessage{}, &stable, &transient)
	if err != nil || len(bindings) != 0 || len(stable) != 0 || len(transient) != 0 {
		t.Fatalf("completed onboarding still affects ordinary work: %+v, %v", bindings, err)
	}
	t.Run("ordinary chat can revise a profile without filling missing overview fields", func(t *testing.T) {
		chat := models.Chat{ID: "ordinary-chat", WakerID: "sparse-profile"}
		if err := wakers.Create(ctx, models.Waker{ID: chat.WakerID, Name: "Sparse profile"}); err != nil {
			t.Fatal(err)
		}
		manager := agentPreparationFixture(t)
		journal := &profileEditTestJournal{agentTestJournal: manager.journal.(*agentTestJournal)}
		journal.chat = models.Chat{ID: chat.ID, WakerID: chat.WakerID, ActiveExecutionID: "turn"}
		manager.journal = journal
		manager.wakerController, manager.profileController = wakers, profiles
		manager.onboardingTransactions, manager.onboardingCommands = core.onboardingTransactions, core.onboardingCommands
		manager.agentBindings = func(models.Chat, string, []string, string) []capability.Binding {
			return []capability.Binding{agentPreparationBinding(t, "memory.entries.search")}
		}
		run := recordedRun{Runtime: runtimeSelection{RuntimeID: "unsupported"}}
		activate, revoke, err := manager.prepareAgent(ctx, models.RuntimeEnvironment{}, run.Workspace, &run.Skills, &run.Instructions, &run.Connectors, &run.ContextInstructions,
			chat, "turn", pendingMessage{admission: constants.ExecutionAdmissionCommitOnRuntimeAccept, message: userMessage{Actor: "user"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer revoke()
		activate()
		connection, err := capability.ReadConnection(preparedContextPath(t, run))
		if err != nil {
			t.Fatal(err)
		}
		call := func(method, path, body string) string {
			t.Helper()
			request := httptest.NewRequest(method, constants.AgentCLIPath+path, strings.NewReader(body))
			request.RemoteAddr = "127.0.0.1:12345"
			request.Header.Set("Authorization", "Bearer "+connection.Token)
			response := httptest.NewRecorder()
			manager.agent.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("%s %s = %d: %s", method, path, response.Code, response.Body)
			}
			return response.Body.String()
		}
		operations := call(http.MethodGet, "/capabilities", "")
		for _, op := range []string{"waker.profile.get", "waker.profile.optimize", "memory.entries.search", "interaction.questions.create"} {
			if !strings.Contains(operations, `"id":"`+op+`"`) {
				t.Fatalf("ordinary chat omitted %s: %s", op, operations)
			}
		}
		var read capability.Response
		if err := json.Unmarshal([]byte(call(http.MethodPost, "/commands", `{"operation":"waker.profile.get","params":{}}`)), &read); err != nil || !read.OK {
			t.Fatalf("read profile = %+v, %v", read, err)
		}
		const revision = "# Review\n\nFollow the process in `研发手册.md`."
		body, err := json.Marshal(capability.Request{
			Operation: constants.WakerProfileOptimizeOperation, RequestID: "keep-existing-overview",
			Params: map[string]any{"revision": read.Data.(map[string]any)["revision"], "content": revision},
		})
		if err != nil {
			t.Fatal(err)
		}
		var result capability.Response
		if err := json.Unmarshal([]byte(call(http.MethodPost, "/commands", string(body))), &result); err != nil || !result.OK || result.Data.(map[string]any)["saved"] != true {
			t.Fatalf("body-only profile update = %+v, %v", result, err)
		}
		var savedFact eventlog.AgentCommandData
		if len(journal.facts) != 2 || json.Unmarshal(journal.facts[1].Data, &savedFact) != nil || savedFact.CommandID != result.CommandID {
			t.Fatalf("committed profile receipt was not recorded: %+v", journal.facts)
		}
		stored, err := wakers.Get(ctx, chat.WakerID)
		if err != nil || len(stored.Expertise)+len(stored.WorkStyle)+len(stored.WorkMethods) != 0 {
			t.Fatalf("body-only update changed the overview: %+v, %v", stored, err)
		}
		document, err := profiles.GetWakerDocument(ctx, chat.WakerID, constants.ProfileDocumentKindAgents)
		if err != nil || document.Content != revision {
			t.Fatalf("saved profile body = %+v, %v", document, err)
		}
	})
	t.Run("profile revision only fences the saved document and overview", func(t *testing.T) {
		chat := models.Chat{ID: "optimization-chat", WakerID: waker.ID}
		userInput := pendingMessage{admission: constants.ExecutionAdmissionCommitOnRuntimeAccept, message: userMessage{Actor: "user"}}
		hidden := chat
		hidden.HiddenFromWakerHistory = true
		if len(core.wakerProfileEditBindings(hidden, "hidden", userInput)) != 0 ||
			len(core.wakerProfileEditBindings(chat, "im", pendingMessage{message: userInput.message, admission: constants.ExecutionAdmissionCommitBeforeDispatch})) != 0 {
			t.Fatal("non-interactive work acquired profile bindings")
		}
		bindings := core.wakerProfileEditBindings(chat, "edit-turn", userInput)
		if len(bindings) != 2 {
			t.Fatalf("profile bindings = %d", len(bindings))
		}
		read := func() any {
			t.Helper()
			result, err := bindings[0].Call(ctx, capability.Request{})
			if err != nil {
				t.Fatal(err)
			}
			return result.Data.(map[string]any)["revision"]
		}
		edit := capability.Request{Operation: constants.WakerProfileOptimizeOperation, RequestID: "update-profile", Params: map[string]any{
			"revision":    read(),
			"expertise":   []any{map[string]any{"title": "核对证据", "description": "对照原文"}},
			"workStyle":   []any{"明确风险"},
			"workMethods": []any{map[string]any{"name": "查证", "steps": []any{"回查原文"}}},
			"content":     "# Review\n\nVerify every claim against its source.",
		}}
		if err := profiles.SaveWakerAgents(ctx, chat.WakerID, "# A newer manual revision", nowMillis()); err != nil {
			t.Fatal(err)
		}
		if _, err := bindings[1].Call(ctx, edit); !errors.Is(err, status_error.WakerProfileChanged) {
			t.Fatalf("stale profile revision = %v", err)
		}
		edit.Params["revision"] = read()
		waker.Avatar = "new-avatar"
		if err := wakers.Update(ctx, waker.ID, waker); err != nil {
			t.Fatal(err)
		}
		saved, err := bindings[1].Call(ctx, edit)
		if err != nil || saved.Replayed {
			t.Fatalf("save profile revision = %+v, %v", saved, err)
		}
		replayed, err := bindings[1].Call(ctx, edit)
		if err != nil || !replayed.Replayed || replayed.CommandID != saved.CommandID {
			t.Fatalf("replay profile revision = %+v, %v", replayed, err)
		}
		updated, err := wakers.Get(ctx, chat.WakerID)
		if err != nil || updated.Avatar != "new-avatar" || updated.RoleName != waker.RoleName || updated.Bio != waker.Bio ||
			len(updated.Expertise) != 1 || updated.Expertise[0].Description != "对照原文" ||
			len(updated.WorkStyle) != 1 || updated.WorkStyle[0] != "明确风险" ||
			len(updated.WorkMethods) != 1 || updated.WorkMethods[0].Name != "查证" {
			t.Fatalf("revised overview = %+v, %v", updated, err)
		}
		document, err := profiles.GetWakerDocument(ctx, chat.WakerID, constants.ProfileDocumentKindAgents)
		if err != nil || document.Content != edit.Params["content"] {
			t.Fatalf("revised document = %+v, %v", document, err)
		}
	})
}

func TestWakerOnboardingRetryReusesPublicChatAndKeepsInternalInputHidden(t *testing.T) {
	ctx := t.Context()
	manager := newMessageTestService(t, t.TempDir())
	wakerID := "onboarding-partner"
	chatID := (requestidempotency.Identity{ID: wakerID}).ResourceID("chat", wakerID)
	if err := manager.wakers.Create(ctx, models.Waker{
		ID: wakerID, Name: "Welcome partner", OnboardingStatus: constants.WakerOnboardingStatusPending, OnboardingChatID: chatID,
	}); err != nil {
		t.Fatal(err)
	}
	first := WorkIntent{
		ClientRequestID: wakerID,
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: wakerID}},
		Input:           WorkInput{Actor: constants.WakerOnboardingActor, Content: "provider-failure"},
		Options:         WorkOptions{RuntimeID: constants.RuntimeIDCodex, Model: "fixture-model", Reasoning: "high", PermissionLevel: constants.PermissionLevelDefault},
		Admission:       constants.ExecutionAdmissionCommitBeforeDispatch,
	}
	accepted, err := manager.execution.Start(ctx, first)
	if err != nil || accepted.Chat.ID != chatID || accepted.Chat.Title != "Welcome partner" {
		t.Fatalf("first kickoff = %+v, %v", accepted, err)
	}
	waitForChatTerminal(t, manager, chatID)
	module := manager.chats.(chatservice.Module)
	chat, err := module.LoadExecutionChat(ctx, chatID)
	if err != nil || chat.LastEndedExecutionID != accepted.Execution.ID || chat.LastEndedExecutionStatus != constants.ExecutionStatusFailed {
		t.Fatalf("post-admission failure not recorded: %+v, %v", chat, err)
	}
	messages, err := module.ReadExecutionMessages(ctx, chatID, accepted.Execution.ID)
	if err != nil || len(messages) != 2 || messages[0].Actor != constants.WakerOnboardingActor {
		t.Fatalf("internal kickoff was missing from original execution facts: %+v, %v", messages, err)
	}
	retry := WorkIntent{
		Chat:    ChatTarget{ChatID: chatID},
		Input:   WorkInput{Actor: constants.WakerOnboardingActor, Content: "Retry the welcome", SubmissionID: "retry-welcome"},
		Options: first.Options, Admission: first.Admission,
	}
	retry.Options.ParentExecutionID = accepted.Execution.ID
	retried, err := manager.execution.Start(ctx, retry)
	if err != nil || retried.Chat.ID != chatID || retried.Execution.ID == accepted.Execution.ID {
		t.Fatalf("retry did not continue the existing chat: %+v, %v", retried, err)
	}
	waitForChatTerminal(t, manager, chatID)
	page, err := module.ReadMessageTimeline(ctx, chatID, 0, 0, 20)
	if err != nil || len(page.Resources) != 2 {
		t.Fatalf("public history = %+v, %v", page.Resources, err)
	}
	for _, message := range page.Resources {
		if message.Role != constants.MessageRoleAssistant || message.Actor == constants.WakerOnboardingActor {
			t.Fatalf("private Core input appeared in public history: %+v", message)
		}
	}
	retry.Input.SubmissionID = "stale-continue"
	if _, err := manager.execution.Start(ctx, retry); !errors.Is(err, status_error.WakerOnboardingStateChanged) {
		t.Fatalf("stale retry started after the original failure had been superseded: %v", err)
	}
	list, err := module.ListChats(ctx, chatservice.ListChatsInput{WakerID: wakerID, Limit: 20})
	if err != nil || len(list.Resources) != 1 || list.Resources[0].ID != chatID {
		t.Fatalf("onboarding created an unexpected public chat: %+v, %v", list, err)
	}
}

func TestWakerOnboardingRejectsNativeProseWithoutStructuredInterview(t *testing.T) {
	ctx := t.Context()
	binary := filepath.Join(t.TempDir(), "allycli")
	if err := os.WriteFile(binary, []byte("fixture CLI publication"), 0700); err != nil {
		t.Fatal(err)
	}
	registry := capability.New(nil)
	t.Cleanup(registry.Close)
	manager := newMessageTestService(t, t.TempDir(), messageTestOptions{agent: registry, agentBinary: binary})
	wakerID := "interview-partner"
	chatID := requestidempotency.WakerOnboardingChatID(wakerID)
	if err := manager.wakers.Create(ctx, models.Waker{
		ID: wakerID, Name: "Interview partner", OnboardingStatus: constants.WakerOnboardingStatusPending, OnboardingChatID: chatID,
	}); err != nil {
		t.Fatal(err)
	}
	accepted, err := manager.execution.Start(ctx, WorkIntent{
		ClientRequestID: wakerID,
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: wakerID}},
		Input:           WorkInput{Actor: constants.WakerOnboardingActor, Content: "Ask through the question card"},
		Options:         WorkOptions{RuntimeID: constants.RuntimeIDCodex, Model: "fixture-model", PermissionLevel: constants.PermissionLevelDefault},
		Admission:       constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil || accepted.Chat.ID != chatID {
		t.Fatalf("kickoff = %+v, %v", accepted, err)
	}
	waitForChatTerminal(t, manager, chatID)
	module := manager.chats.(chatservice.Module)
	messages, err := module.ReadExecutionMessages(ctx, chatID, accepted.Execution.ID)
	if err != nil || len(messages) != 2 || messages[1].ErrorCode != status_error.WakerOnboardingInterviewRequired.Key() ||
		messages[1].Status != constants.ExecutionStatusFailed {
		t.Fatalf("plain-text response incorrectly completed setup: %+v, %v", messages, err)
	}
}
