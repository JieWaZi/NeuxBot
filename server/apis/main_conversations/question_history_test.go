package main_conversations

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestMainAndSourceQuestionHistoryAfterRendererAndCoreRestart(t *testing.T) {
	ctx := t.Context()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "core.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	oldMain, oldStore, oldTransactions, oldChats := global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager, global.ChatService
	t.Cleanup(func() {
		global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager, global.ChatService = oldMain, oldStore, oldTransactions, oldChats
	})
	wakers, teams := controllers.NewWakerController(db.DB), controllers.NewChannelController(db.DB)
	transactions := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	chatStore := controllers.NewChatController(db.DB)
	mainStore := controllers.NewMainConversationController(db.DB)
	main := mainconversation.New(mainStore, transactions, wakers, teams)
	global.MainConversationService, global.MainConversationControllerManager, global.TransactionControllerManager = main, mainStore, transactions
	if err := wakers.Create(ctx, models.Waker{ID: "partner", Name: "伙伴"}); err != nil {
		t.Fatal(err)
	}
	work, err := main.ReserveWork(ctx, "waker", "partner", "partner", "managed")
	if err != nil {
		t.Fatal(err)
	}
	config := chat.Config{
		StorageDirectory: t.TempDir(),
		Projects:         controllers.NewProjectController(db.DB),
		QueryIndexes: chat.QueryIndexDependencies{
			Chats: chatStore, Transactions: transactions,
			DeletionIntents: controllers.NewChatDeletionIntentController(db.DB),
		},
	}
	startChat := func() chat.Module {
		t.Helper()
		module, err := chat.New(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(module.Close)
		if err := module.Start(ctx); err != nil {
			t.Fatal(err)
		}
		global.ChatService = module
		return module
	}
	module := startChat()
	workspace := t.TempDir()
	sourceChat := models.Chat{
		ID: work.WorkChatID, WakerID: "partner", HiddenFromWakerHistory: true,
		WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace},
	}
	const executionID, questionID, sourceInputID = "work-execution", "original-question", "source-input"
	data := func(value any) []byte {
		t.Helper()
		result, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	facts := []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: data(eventlog.ChatCreatedData{
			WakerID: sourceChat.WakerID, HiddenFromWakerHistory: true, WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace},
		})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: sourceInputID, Data: data(eventlog.MessageCreatedData{
			Role: constants.MessageRoleUser, Content: "原交办",
		})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: executionID, Data: data(eventlog.ExecutionCreatedData{RuntimeID: "codex", UserMessageID: sourceInputID})},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: executionID},
	}
	if _, err := module.CommitAcceptedTurn(ctx, chat.StartedTurn{
		NewChat: true, Chat: sourceChat, Facts: facts,
		Execution: models.Execution{ID: executionID, ChatID: sourceChat.ID, UserMessageID: sourceInputID, RuntimeID: "codex", Status: constants.ExecutionStatusRunning},
	}); err != nil {
		t.Fatal(err)
	}
	input, err := main.AdmitUser(ctx, "waker", "partner", "submit", "原交办", "", work.WorkChatID, executionID, "", "", "", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	request, err := module.AppendInteractionFact(work.WorkChatID, eventlog.AppendInput{
		Type: constants.ChatEventTypeElicitationRequested, ExecutionID: executionID,
		Data: data(eventlog.ElicitationRequestedData{
			ElicitationID: questionID, Mode: "url", Message: "原来问的问题", URL: "https://example.invalid/?temporary=token",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	module.PublishInteractionFact(ctx, request)
	readViews := func() (MainConversationResponse, MainConversationSourceResponse) {
		t.Helper()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/", nil).WithContext(ctx)
		mainResponse, err := (&GetMainConversation{SubjectType: "waker", SubjectID: "partner"}).Output(c)
		if err != nil {
			t.Fatalf("main history: %v", err)
		}
		sourceResponse, err := (&GetMainConversationSource{SubjectType: "waker", SubjectID: "partner", WorkChatID: work.WorkChatID}).Output(c)
		if err != nil {
			t.Fatalf("source history: %+v, recovery: %+v, error: %v", mainResponse, global.ChatService.RecoveryHealth(), err)
		}
		return mainResponse.(MainConversationResponse), sourceResponse.(MainConversationSourceResponse)
	}
	for i := 0; i < 2; i++ {
		mainPage, sourcePage := readViews()
		if len(mainPage.Messages) != 1 || len(mainPage.Messages[0].Questions) != 0 || len(mainPage.ActiveWork) != 1 ||
			!mainPage.ActiveWork[0].WaitingElicitation || len(sourcePage.Chat.PendingElicitations) != 1 {
			t.Fatalf("fresh page lost live question or duplicated it as history: %+v, %+v", mainPage, sourcePage.Chat)
		}
	}

	module.Close()
	// 模拟启动时需要重建的旧查询投影，权威日志与工作绑定保持原状。
	if err := db.DB.Model(&models.Chat{}).Where("id = ?", work.WorkChatID).UpdateColumn("created_at", int64(1)).Error; err != nil {
		t.Fatal(err)
	}
	if health := startChat().RecoveryHealth(); health.Rebuilt != 1 {
		t.Fatalf("expected Core to rebuild the original Chat projection: %+v", health)
	}
	mainPage, sourcePage := readViews()
	if len(mainPage.ActiveWork) != 0 || len(sourcePage.Chat.PendingElicitations) != 0 ||
		len(mainPage.Messages) != 1 || mainPage.Messages[0].ID != input.ID ||
		len(mainPage.Messages[0].Questions) != 1 || mainPage.Messages[0].Readable {
		t.Fatalf("Core restart lost history or produced an actionable fake: %+v, %+v", mainPage, sourcePage.Chat)
	}
	projected := mainPage.Messages[0].Questions[0]
	if projected.ElicitationID != questionID || projected.Message != "原来问的问题" ||
		projected.Result != constants.ElicitationResultInvalidated || projected.URL != "" {
		t.Fatalf("interrupted question did not preserve its original safe display: %+v", projected)
	}
	for _, message := range sourcePage.Resources {
		if message.Question != nil && message.Question.ElicitationID == questionID {
			if message.Question.Result != projected.Result || message.Question.URL != "" {
				t.Fatalf("source history disagrees with main question: %+v", message.Question)
			}
			return
		}
	}
	t.Fatal("source history lost the interrupted question")
}
