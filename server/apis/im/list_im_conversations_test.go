package im

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

type conversationLogReader struct {
	// Module 保留列表用例没有调用的其余聊天能力。
	chat.Module
	// store 使用真实权威日志复现带会话标识的不存在错误。
	store *conversation.Store
	// failure 注入日志故障，验证只有明确的不存在结果会被过滤。
	failure error
}

func (r conversationLogReader) CountUserMessages(ctx context.Context, chatID string) (int64, error) {
	if r.failure != nil {
		return 0, r.failure
	}
	rows, err := r.store.ReadUserInputs(ctx, chatID)
	return int64(len(rows)), err
}

func TestListIMConversations(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "im.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := conversationfixture.New(t, t.TempDir())
	validChatID := "11111111111111111111111111111111"
	missingChatID := "22222222222222222222222222222222"
	data, err := json.Marshal(eventlog.MessageCreatedData{Role: constants.MessageRoleUser})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conversationfixture.CommitFacts(t.Context(), store, validChatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-message", Data: data},
	}); err != nil {
		t.Fatal(err)
	}
	controller := controllers.NewIMController(db.DB)
	for _, link := range []models.IMConversationLink{
		{ID: "valid", BindingID: "binding", ChatID: validChatID},
		{ID: "deleted", BindingID: "binding", ChatID: missingChatID},
	} {
		if err := controller.SaveLink(context.Background(), link); err != nil {
			t.Fatal(err)
		}
	}
	previousIM, previousChats := global.IMControllerManager, global.ChatService
	global.IMControllerManager, global.ChatService = controller, conversationLogReader{store: store}
	t.Cleanup(func() {
		global.IMControllerManager, global.ChatService = previousIM, previousChats
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	output, err := (&ListIMConversations{BindingID: "binding"}).Output(ctx)
	if err != nil {
		t.Fatalf("list must omit the missing chat instead of returning its error: %v", err)
	}
	resources := output.(ListIMConversationsResponse).Resources
	if len(resources) != 1 || resources[0].ID != "valid" || resources[0].TurnCount != 1 {
		t.Fatalf("resources = %#v; want only the existing conversation with one user turn", resources)
	}
	links, err := controller.Links(context.Background(), "binding")
	if err != nil || len(links) != 2 {
		t.Fatalf("history links were changed: links=%#v err=%v", links, err)
	}
	previousThreads := global.ChannelThreadControllerManager
	global.ChannelThreadControllerManager = controllers.NewChannelThreadController(db.DB)
	t.Cleanup(func() { global.ChannelThreadControllerManager = previousThreads })

	t.Run("deleted thread with residual member records", func(t *testing.T) {
		if err := controller.SaveLink(ctx, models.IMConversationLink{ID: "deleted-team", BindingID: "binding", ThreadID: "deleted-thread", ChatID: validChatID}); err != nil {
			t.Fatal(err)
		}
		if err := db.DB.Create(&models.ChannelThreadMember{ThreadID: "deleted-thread", WakerID: "member", ChatID: validChatID}).Error; err != nil {
			t.Fatal(err)
		}
		output, err := (&ListIMConversations{BindingID: "binding"}).Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		resources := output.(ListIMConversationsResponse).Resources
		if len(resources) != 1 || resources[0].ID != "valid" {
			t.Fatalf("resources = %#v; deleted team conversation must not appear", resources)
		}
	})

	t.Run("history keeps the original team after rebinding", func(t *testing.T) {
		teamChatID := "33333333333333333333333333333333"
		if _, err := conversationfixture.CommitFacts(t.Context(), store, teamChatID, []eventlog.AppendInput{
			{Type: constants.ChatEventTypeCreated},
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.DB.Create(&models.IMBinding{ID: "binding", ChannelID: "current-team"}).Error; err != nil {
			t.Fatal(err)
		}
		readyAt := int64(1)
		thread := models.ChannelThread{
			ID: "historical-thread", ChannelID: "historical-team", LeadWakerID: "member", LeadChatID: teamChatID,
			AdditionalWorkingDirectories: []string{}, ReadyAt: &readyAt,
		}
		if err := db.DB.Create(&thread).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.DB.Create(&models.ChannelThreadMember{ThreadID: thread.ID, WakerID: "member", ChatID: teamChatID}).Error; err != nil {
			t.Fatal(err)
		}
		if err := controller.SaveLink(ctx, models.IMConversationLink{
			ID: "historical-link", BindingID: "binding", ThreadID: thread.ID, ChatID: teamChatID,
			Configuration: models.IMConversationConfiguration{ChannelID: "historical-team"},
		}); err != nil {
			t.Fatal(err)
		}
		output, err := (&ListIMConversations{BindingID: "binding"}).Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(output)
		if err != nil {
			t.Fatal(err)
		}
		var listing map[string][]map[string]any
		if err := json.Unmarshal(payload, &listing); err != nil {
			t.Fatal(err)
		}
		for _, resource := range listing["resources"] {
			if resource["id"] == "historical-link" {
				if resource["channelId"] != "historical-team" || resource["threadId"] != "historical-thread" {
					t.Fatalf("historical team route = %v; want historical-team/historical-thread", resource)
				}
				return
			}
		}
		t.Fatalf("missing historical team link: %s", payload)
	})

	t.Run("log failure is not treated as an empty conversation list", func(t *testing.T) {
		global.ChatService = conversationLogReader{store: store, failure: status_error.ChatLogError}
		defer func() { global.ChatService = conversationLogReader{store: store} }()
		output, err := (&ListIMConversations{BindingID: "binding"}).Output(ctx)
		if output != nil || !util.HasStatusCode(err, status_error.ChatLogError) {
			t.Fatalf("output=%#v err=%v; want the original log failure", output, err)
		}
	})
}
