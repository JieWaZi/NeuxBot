package main_conversations

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/apis/chats"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
)

func TestMainConversationSourceModelPickerPersistsAndIsScoped(t *testing.T) {
	root := t.TempDir()
	configureNativeMainConversationAt(t, root, "", true)
	gate := filepath.Join(root, "release-model-fixture")
	t.Setenv(queueFixtureGate, gate)
	t.Cleanup(func() { _ = os.WriteFile(gate, nil, 0600) })
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	httpCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpCtx.Request = httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	const waker = "native-main-partner"
	first, err := (&SendMainConversationMessage{SubjectType: "waker", SubjectID: waker,
		Body: MainConversationInput{SubmissionID: "before-model-picker", Content: "Say hello."}}).Output(httpCtx)
	if err != nil {
		t.Fatal(err)
	}
	source := first.(MainConversationAcceptance).Message.SourceChatID
	if source == "" {
		t.Fatalf("missing original source: %+v", first)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		return !global.ExecutionService.IsExecutionActive(source, first.(MainConversationAcceptance).ExecutionID)
	})
	response, err := global.ChatService.GetChatSummary(ctx, source)
	if err != nil || response.Chat.LastModel == "" {
		t.Fatalf("initial work model: %+v, %v", response, err)
	}

	setter := SetMainConversationSourceModel{SubjectType: "waker", SubjectID: waker, WorkChatID: source,
		Body: MainConversationSourceModelInput{Model: "fixture-alternate"}}
	raw, err := setter.Output(httpCtx)
	if err != nil {
		t.Fatal(err)
	}
	accepted := raw.(MainConversationSourceModelAcceptance)
	if change := accepted.Change; accepted.SelectedModel != setter.Body.Model || change == nil ||
		change.Actor != "user" || change.FromModel != response.Chat.LastModel || change.ToModel != setter.Body.Model ||
		change.Sequence <= response.Chat.LatestSequence || change.CreatedAt <= 0 {
		t.Fatalf("unexpected model history receipt: %+v, change=%+v", accepted, change)
	}
	retry, err := setter.Output(httpCtx)
	if err != nil || retry.(MainConversationSourceModelAcceptance).Change != nil {
		t.Fatalf("retry added a model change: %+v, %v", retry, err)
	}
	getter := GetMainConversationSource{SubjectType: "waker", SubjectID: waker, WorkChatID: source}
	current, err := getter.Output(httpCtx)
	if err != nil {
		t.Fatal(err)
	}
	page := current.(MainConversationSourceResponse)
	if page.SelectedModel != setter.Body.Model || len(page.ModelChanges) != 1 || page.ModelChanges[0] != *accepted.Change ||
		page.Chat.LastModel != response.Chat.LastModel {
		t.Fatalf("source history did not restore just the saved model selection: %+v", page)
	}
	events, err := global.ChatService.ListEvents(ctx, source, accepted.Change.Sequence-1, 10)
	if err != nil || len(events) != 1 || events[0].Type != constants.ChatEventTypeSourceModelSelected {
		t.Fatalf("picker fact did not advance the source stream: %+v, %v", events, err)
	}

	reopened, err := conversation.NewStore(global.Config.ManagedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.ReadSourceModelChanges(ctx, source, 0, 0, 2)
	if err != nil || len(records) != 1 || records[0].Cursor != accepted.Change.Sequence {
		t.Fatalf("reopened journal lost the change: %+v, %v", records, err)
	}

	if err := global.WakerControllerManager.Create(ctx, models.Waker{ID: "wrong-model-owner", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	other := setter
	other.SubjectID = "wrong-model-owner"
	if _, err := other.Output(httpCtx); !util.HasStatusCode(err, status_error.ChatNotFound) {
		t.Fatalf("another subject set work model: %v", err)
	}
	other.SubjectType = "team"
	if _, err := other.Output(httpCtx); !util.HasStatusCode(err, status_error.BadRequest) {
		t.Fatalf("team model is no longer fixed: %v", err)
	}

	sender := SendMainConversationSourceMessage{SubjectType: "waker", SubjectID: waker, WorkChatID: source,
		Body: chats.SendMessageRequest{SubmissionID: "after-model-picker",
			MessageRequest: chats.MessageRequest{Text: "gate work after choosing model", RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}}}
	sent, err := sender.Output(httpCtx)
	if err != nil {
		t.Fatal(err)
	}
	run := sent.(MainConversationSourceAcceptance).Execution
	if run == nil || run.Model != setter.Body.Model {
		t.Fatalf("source send did not inherit the selected model: %+v", run)
	}

	queue := newNativeMainQueueFixture(t, root)
	if err := queue.Open(ctx); err != nil {
		t.Fatal(err)
	}
	mainSend := &SendMainConversationMessage{SubjectType: "waker", SubjectID: waker,
		Body: MainConversationInput{SubmissionID: "main-model-before-later-pick", Content: "Please continue this work.", ReplyToMessageID: first.(MainConversationAcceptance).Message.ID}}
	mainInput, err := mainSend.Output(httpCtx)
	if err != nil || mainInput.(MainConversationAcceptance).QueuedInput == nil {
		t.Fatalf("main input not accepted while source busy: %+v, %v", mainInput, err)
	}
	if !global.ExecutionService.IsExecutionActive(source, run.ID) {
		t.Fatal("source fixture did not hold an active turn")
	}
	setter.Body.Model = response.Chat.LastModel
	raw, err = setter.Output(httpCtx)
	if err != nil || raw.(MainConversationSourceModelAcceptance).Change == nil {
		t.Fatalf("change after queuing: %+v, %v", raw, err)
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitMainQueueFixture(t, ctx, func() bool {
		row, err := global.ChatService.ReadSubmission(ctx, source, mainSend.Body.SubmissionID)
		if err != nil || row == nil {
			return false
		}
		if row.Model != run.Model {
			t.Fatalf("later picker action changed frozen main model: %+v", row)
		}
		return row.Status == constants.ExecutionStatusCompleted
	})
}
