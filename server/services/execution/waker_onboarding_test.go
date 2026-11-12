package execution

import (
	"errors"
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

func TestWakerOnboardingSavesExactlyConfirmedVersionAndRecoversReceipt(t *testing.T) {
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
	if err != nil || len(bindings) != 3 || len(stable) != 1 || !strings.Contains(stable[0].Content, "An overview") || len(transient) != 0 {
		t.Fatalf("initial setup scope = %+v, instructions = %+v %+v, error = %v", bindings, stable, transient, err)
	}
	if _, err := bindings[0].Call(ctx, capability.Request{Operation: constants.WakerProfileProposeOperation, RequestID: "bypass", Params: map[string]any{"content": "Draft without an interview"}}); !errors.Is(err, status_error.WakerOnboardingInterviewRequired) {
		t.Fatalf("a profile draft bypassed the interactive interview: %v", err)
	}
	question := bindings[2]
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
	if _, err := bindings[0].Call(ctx, capability.Request{Operation: constants.WakerProfileProposeOperation, RequestID: "bypass", Params: map[string]any{"content": "Draft while unanswered"}}); !errors.Is(err, status_error.WakerOnboardingInterviewRequired) {
		t.Fatalf("a profile draft bypassed an unanswered interview: %v", err)
	}
	for _, invalid := range []map[string]any{
		{"role": "review"},
		{"role": "unlisted", "style": "detail"},
		{"role": "review", "style": "detail", "style__other": "Both"},
		{"role": "review", "style__other": "   "},
	} {
		if err := core.elicitations.resolveAccepted(ctx, chatID, result.CommandID, invalid); err == nil {
			t.Fatalf("invalid choice/Other answer accepted: %+v", invalid)
		}
	}
	if err := core.elicitations.resolveAccepted(ctx, chatID, result.CommandID, map[string]any{"role": "review", "style__other": "Short and clear"}); err != nil {
		t.Fatal(err)
	}
	propose := func(requestID, body string) string {
		t.Helper()
		result, err := bindings[0].Call(ctx, capability.Request{Operation: constants.WakerProfileProposeOperation, RequestID: requestID, Params: map[string]any{
			"expertise":   []map[string]any{{"title": "Review", "description": "Review drafts"}},
			"workStyle":   []string{"Clear"},
			"workMethods": []map[string]any{{"name": "Check", "steps": []string{"Read sources"}}},
			"content":     body, "language": "en",
		}})
		if err != nil {
			t.Fatal(err)
		}
		id := result.Data.(map[string]any)["draftId"].(string)
		answer, err := core.elicitations.questionSnapshot(chatID, "first", id)
		if err != nil || !strings.Contains(answer["message"].(string), body) || !strings.Contains(answer["message"].(string), "Review drafts") {
			t.Fatalf("preview does not contain the proposed contents: %+v, %v", answer, err)
		}
		return id
	}
	save := func(target models.Chat, draftID string) (capability.Result, error) {
		return core.saveWakerProfile(ctx, target, capability.Request{Operation: constants.WakerProfileSaveOperation, RequestID: "save", Params: map[string]any{"draftId": draftID}})
	}
	requireUnpublished := func() {
		t.Helper()
		waker, err := wakers.Get(ctx, chat.WakerID)
		if err != nil || waker.OnboardingStatus != constants.WakerOnboardingStatusPending || len(waker.Expertise) != 0 {
			t.Fatalf("unconfirmed data was published: %+v, %v", waker, err)
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

	first := propose("draft-one", "First draft")
	if _, err := save(chat, first); !errors.Is(err, status_error.WakerProfileConfirmationRequired) {
		t.Fatalf("unanswered draft save = %v", err)
	}
	if err := core.elicitations.resolveDeclined(ctx, chat.ID, first); err != nil {
		t.Fatal(err)
	}
	if _, err := save(chat, first); !errors.Is(err, status_error.WakerProfileConfirmationRequired) {
		t.Fatalf("skipped draft save = %v", err)
	}
	expired := propose("draft-expired", "Expired draft")
	core.elicitations.finishQuestions(chat.ID, "first")
	if _, err := save(chat, expired); !errors.Is(err, status_error.WakerProfileConfirmationRequired) {
		t.Fatalf("invalidated draft save = %v", err)
	}
	second := propose("draft-two", "Second draft")
	if err := core.elicitations.resolveAccepted(ctx, chat.ID, second, map[string]any{"decision": "revise"}); err != nil {
		t.Fatal(err)
	}
	if _, err := save(chat, second); !errors.Is(err, status_error.WakerProfileConfirmationRequired) {
		t.Fatalf("revision decision save = %v", err)
	}
	stale := propose("draft-three", "Superseded draft")
	if err := core.elicitations.resolveAccepted(ctx, chat.ID, stale, map[string]any{"decision": "save"}); err != nil {
		t.Fatal(err)
	}
	const body = "\n# Working agreement\n\nUse the agreed process.  \n"
	current := propose("draft-four", body)
	if _, err := save(chat, stale); !errors.Is(err, status_error.WakerProfileDraftStale) {
		t.Fatalf("confirmed superseded version save = %v", err)
	}
	requireUnpublished()
	if err := core.elicitations.resolveAccepted(ctx, chat.ID, current, map[string]any{"decision": "save"}); err != nil {
		t.Fatal(err)
	}
	if _, err := save(models.Chat{ID: "other-chat", WakerID: chat.WakerID}, current); !errors.Is(err, status_error.AgentContextUnauthorized) {
		t.Fatalf("cross-chat save = %v", err)
	}

	// A new active turn recovers the decision from the original execution, including after a Core restart.
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
	if err != nil || len(bindings) != 3 || len(transient) != 2 || !strings.Contains(transient[0].Content, "Review drafts") || !strings.Contains(transient[1].Content, current) {
		t.Fatalf("exact confirmation not recovered: %+v, %v", transient, err)
	}
	request := capability.Request{Operation: constants.WakerProfileSaveOperation, RequestID: "save", Params: map[string]any{"draftId": current}}
	if err := db.DB.Exec("CREATE TRIGGER reject_receipt BEFORE INSERT ON agent_command_receipts BEGIN SELECT RAISE(ABORT, 'receipt denied'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := bindings[1].Call(ctx, request); err == nil {
		t.Fatal("save succeeded with an uncommitted receipt")
	}
	requireUnpublished()
	if err := db.DB.Exec("DROP TRIGGER reject_receipt").Error; err != nil {
		t.Fatal(err)
	}
	saved, err := bindings[1].Call(ctx, request)
	if err != nil || saved.Replayed || saved.Data.(map[string]any)["saved"] != true {
		t.Fatalf("save receipt = %+v, %v", saved, err)
	}
	replay, err := bindings[1].Call(ctx, request)
	if err != nil || !replay.Replayed || replay.CommandID != saved.CommandID {
		t.Fatalf("save replay = %+v, %v", replay, err)
	}
	document, err := profiles.GetWakerDocument(ctx, chat.WakerID, constants.ProfileDocumentKindAgents)
	if err != nil || document.Content != body {
		t.Fatalf("saved body differs from preview: %q, %v", document.Content, err)
	}
	waker, err := wakers.Get(ctx, chat.WakerID)
	if err != nil || waker.OnboardingStatus != constants.WakerOnboardingStatusReady || waker.OnboardingDraft != nil || len(waker.Expertise) != 1 || waker.Expertise[0].Description != "Review drafts" || len(waker.WorkStyle) != 1 || len(waker.WorkMethods) != 1 {
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
	if _, err := manager.execution.Start(ctx, retry); !errors.Is(err, status_error.WakerProfileDraftStale) {
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
