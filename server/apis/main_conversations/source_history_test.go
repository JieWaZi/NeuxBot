package main_conversations

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/mainconversation"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

type sourceHistoryChat struct {
	// Module 保留未由来源读取用例调用的接口。
	chat.Module
	// summary 是已接纳的原工作会话。
	summary chat.ChatSummary
	// page 是原生日志在请求窗口内返回的消息。
	page chat.MessageHistoryPage
}

func (c sourceHistoryChat) GetChatSummary(_ context.Context, _ string) (chat.ChatSummary, error) {
	return c.summary, nil
}

func (c sourceHistoryChat) ReadMessageTimeline(_ context.Context, _ string, _, _ int64, _ int) (chat.MessageHistoryPage, error) {
	return c.page, nil
}

func TestMainConversationSourceReturnsActualPublicationsForItsWorkPage(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "source.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	wakers := controllers.NewWakerController(db.DB)
	if err := wakers.Create(t.Context(), models.Waker{ID: "partner", Name: "Partner"}); err != nil {
		t.Fatal(err)
	}
	store := controllers.NewMainConversationController(db.DB)
	service := mainconversation.New(store, controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), wakers, nil)
	work, err := service.ReserveWork(t.Context(), "waker", "partner", "partner", "managed")
	if err != nil {
		t.Fatal(err)
	}
	directPublicID := mainconversation.UserMessageID(work.ConversationID, "direct-submission")
	olderPublicID := mainconversation.UserMessageID(work.ConversationID, "older-submission")
	rows := []models.MainConversationMessage{
		{ID: "legacy", Sequence: 1, Role: "user", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, Content: "Legacy trigger"},
		{ID: directPublicID, Sequence: 8, Role: "user", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, SourceExecutionID: "source-run", SourceMessageID: "direct", Content: "Direct source input"},
		{ID: "main-public", Sequence: 9, Role: "user", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, SourceExecutionID: "main-public", SourceMessageID: "main-native", Content: "Authoritative trigger\nLong continuation"},
		{ID: olderPublicID, Sequence: 11, Role: "user", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, SourceExecutionID: olderPublicID, AdmissionSubmissionID: "older-submission", Content: "Older trigger"},
		{ID: "hidden-trigger", Sequence: 10, Role: "user", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, Content: "Removed trigger", RemovedAt: 1},
		{ID: "explicit", Sequence: 2, Role: "assistant", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, SourceExecutionID: "run", Content: "First public result"},
		{ID: "old-answer", Sequence: 3, Role: "assistant", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, ReplyToMessageID: "legacy", Content: "Legacy public result"},
		{ID: "second", Sequence: 4, Role: "assistant", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, SourceExecutionID: "run", Content: "Second public result"},
		{ID: "wrong-run", Sequence: 5, Role: "assistant", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, SourceExecutionID: "previous", ReplyToMessageID: "legacy"},
		{ID: "wrong-source", Sequence: 6, Role: "assistant", ConversationID: work.ConversationID, SourceChatID: "elsewhere", SourceExecutionID: "run"},
		{ID: "wrong-subject", Sequence: 1, Role: "assistant", ConversationID: "different", SourceChatID: work.WorkChatID, SourceExecutionID: "run"},
		{ID: "no-original", Sequence: 7, Role: "assistant", ConversationID: work.ConversationID, SourceChatID: work.WorkChatID, ReplyToMessageID: "missing"},
	}
	if err := db.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	oldMain, oldStore, oldChats := global.MainConversationService, global.MainConversationControllerManager, global.ChatService
	t.Cleanup(func() {
		global.MainConversationService, global.MainConversationControllerManager, global.ChatService = oldMain, oldStore, oldChats
	})
	global.MainConversationService, global.MainConversationControllerManager = service, store
	native := messageview.Message{
		ID: "assistant-run", Role: constants.MessageRoleAssistant, ExecutionID: "run",
		Content: "Native final", DisplayParts: []messageview.MessageDisplayPart{
			{ID: "post", Type: constants.MessageDisplayPartTool},
			{ID: "final", Type: constants.MessageDisplayPartText, Text: "Native final"},
		},
	}
	global.ChatService = sourceHistoryChat{
		summary: chat.ChatSummary{Chat: models.Chat{ID: work.WorkChatID, WakerID: work.WakerID, HiddenFromWakerHistory: true}},
		page: chat.MessageHistoryPage{Resources: []messageview.Message{
			{ID: "assistant-legacy", Role: constants.MessageRoleAssistant, ExecutionID: "legacy"},
			native,
			{ID: "legacy", Role: constants.MessageRoleUser},
			{ID: "main-native", Role: constants.MessageRoleUser, MainConversationTriggerID: "main-public"},
			{ID: "older-native", Role: constants.MessageRoleUser, SubmissionID: "older-submission"},
			{ID: "direct", Role: constants.MessageRoleUser, SubmissionID: "direct-submission"},
			{ID: "hidden-trigger", Role: constants.MessageRoleUser, Content: "Removed trigger"},
			{ID: "assistant-missing", Role: constants.MessageRoleAssistant, ExecutionID: "missing"},
		}},
	}
	httpCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	httpCtx.Request = httptest.NewRequest("GET", "/", nil)
	value, err := (&GetMainConversationSource{SubjectType: "waker", SubjectID: "partner", WorkChatID: work.WorkChatID}).Output(httpCtx)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		// Resources 保存权威工作记录。
		Resources []messageview.Message
		// InputTriggers 只引用与来源输入一致的公开主会话原话。
		InputTriggers []struct {
			// SourceMessageID 是原生输入的身份。
			SourceMessageID string
			// PublicMessageID 是当前仍可访问的公开原话。
			PublicMessageID string
			// Preview 是原话的首行。
			Preview string
		}
		// PublishedMessages 保存本页真实公开发言及其执行归属。
		PublishedMessages []struct {
			// ID 是公开消息的真实身份。
			ID string
			// ExecutionID 是该消息对应的工作回合。
			ExecutionID string
			// Content 是公开消息完整正文。
			Content string
		}
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	actual := make([][3]string, 0, len(response.PublishedMessages))
	for _, publication := range response.PublishedMessages {
		actual = append(actual, [3]string{publication.ID, publication.ExecutionID, publication.Content})
	}
	want := [][3]string{
		{"explicit", "run", "First public result"},
		{"old-answer", "legacy", "Legacy public result"},
		{"second", "run", "Second public result"},
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("publications = %+v, want %+v", actual, want)
	}
	if len(response.Resources) != 8 || !reflect.DeepEqual(response.Resources[1], native) {
		t.Fatalf("native message changed: %+v", response.Resources)
	}
	var triggers [][3]string
	for _, trigger := range response.InputTriggers {
		triggers = append(triggers, [3]string{trigger.SourceMessageID, trigger.PublicMessageID, trigger.Preview})
	}
	wantTriggers := [][3]string{
		{"legacy", "legacy", "Legacy trigger"},
		{"main-native", "main-public", "Authoritative trigger"},
		{"older-native", olderPublicID, "Older trigger"},
		{"hidden-trigger", "", "Removed trigger"},
	}
	if !reflect.DeepEqual(triggers, wantTriggers) {
		t.Fatalf("triggers = %+v, want %+v", triggers, wantTriggers)
	}
}
