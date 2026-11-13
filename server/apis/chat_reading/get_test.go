package chat_reading

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversationfixture"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestGetChatReadingPreservesCursorWhenLogIsIsolated(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "reading.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldChats, oldReading := global.ChatService, global.ChatReadingControllerManager
	oldBoard, oldThreads, oldHub := global.WorkBoardControllerManager, global.ChannelThreadControllerManager, global.EventHub
	hub := events.New()
	reading := controllers.NewChatReadingController(database.DB)
	global.ChatReadingControllerManager = reading
	global.WorkBoardControllerManager = controllers.NewWorkBoardController(database.DB)
	global.ChannelThreadControllerManager = controllers.NewChannelThreadController(database.DB)
	global.EventHub = hub
	t.Cleanup(func() {
		global.ChatService, global.ChatReadingControllerManager = oldChats, oldReading
		global.WorkBoardControllerManager, global.ChannelThreadControllerManager, global.EventHub = oldBoard, oldThreads, oldHub
		hub.Close()
	})

	root, workspace := t.TempDir(), t.TempDir()
	store := conversationfixture.New(t, root)
	config := chat.Config{
		StorageDirectory: root,
		Conversations:    store,
		Projects:         controllers.NewProjectController(database.DB),
		QueryIndexes: chat.QueryIndexDependencies{
			Reading:         reading,
			Chats:           controllers.NewChatController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions:    controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)),
		},
	}
	manager, err := chat.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	global.ChatService = manager
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = httptest.NewRequest("GET", "/", nil).WithContext(t.Context())
	const publicID, privateID, messageID = "41414141414141414141414141414141", "42424242424242424242424242424242", "report"
	truncated := make(map[string]int64, 2)
	for _, id := range []string{publicID, privateID} {
		if _, err := manager.CreateDormantChats(t.Context(), []chat.DormantChat{{Chat: models.Chat{
			ID: id, WakerID: "reader", Title: "Reading", WorkspaceDirectory: workspace,
			AdditionalWorkingDirectories: []string{workspace}, HiddenFromWakerHistory: id == privateID,
		}}}); err != nil {
			t.Fatal(err)
		}
		path, err := store.LogPath(id)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		truncated[path] = info.Size()
		if err := manager.AppendParticipantMessages(t.Context(), id, []chat.ParticipantMessageInput{{
			ID: messageID, Actor: string(constants.ChannelMessageActorMemberReport), Content: "报告",
			SourceWakerID: "member-waker", SourceChatID: "member-chat",
		}}); err != nil {
			t.Fatal(err)
		}
	}
	unread := MarkChatUnread{ChatID: publicID, Body: MarkChatUnreadRequest{FromMessageID: messageID}}
	if _, err := unread.Output(ctx); err != nil {
		t.Fatal(err)
	}
	if err := reading.MarkUnread(t.Context(), constants.LocalReaderID, privateID, messageID, 0); err != nil {
		t.Fatal(err)
	}
	get := GetChatReading{ChatID: publicID}
	result, err := get.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := result.(ChatReadingResponse)
	if !before.Complete || before.ThroughSequence != 1 || before.UnreadGeneration != 1 || !before.HoldUnread ||
		before.UnreadMessageCount != 1 || before.FirstUnreadMessageID != messageID || before.LatestReadableMessageID != messageID {
		t.Fatalf("complete reading before corruption = %+v", before)
	}

	manager.Close()
	for path, size := range truncated {
		if err := os.Truncate(path, size); err != nil {
			t.Fatal(err)
		}
	}
	config.Conversations = nil
	restarted, err := chat.New(config)
	if err != nil {
		t.Fatal(err)
	}
	manager, global.ChatService = restarted, restarted
	if err := restarted.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Run("public cursor", func(t *testing.T) {
		result, err := get.Output(ctx)
		if err != nil {
			t.Fatal(err)
		}
		state := result.(ChatReadingResponse)
		if state.Complete || state.IncompleteReason != "isolated" || state.ChatReadCursor != before.ChatReadCursor ||
			state.UnreadMessageCount != 0 || state.FirstUnreadMessageID != "" || state.LatestReadableMessageID != "" {
			t.Fatalf("isolated reading = %+v, want saved cursor %+v without message bounds", state, before.ChatReadCursor)
		}
	})
	t.Run("private cursor", func(t *testing.T) {
		result, err := (&GetChatReading{ChatID: privateID}).Output(ctx)
		if result != nil || !util.HasStatusCode(err, status_error.ChatNotFound) {
			t.Fatalf("isolated private chat returned %+v, %v", result, err)
		}
	})
	t.Run("isolated writes", func(t *testing.T) {
		read := MarkChatRead{ChatID: publicID, Body: MarkChatReadRequest{
			ThroughMessageID: messageID, ObservedUnreadGeneration: 1, Intent: constants.ReadingIntentExplicit,
		}}
		if _, err := read.Output(ctx); !util.HasStatusCode(err, status_error.ChatLogCorrupted) {
			t.Fatalf("mark read on isolated chat = %v", err)
		}
		unread.Body.ObservedUnreadGeneration = 1
		if _, err := unread.Output(ctx); !util.HasStatusCode(err, status_error.ChatLogCorrupted) {
			t.Fatalf("mark unread on isolated chat = %v", err)
		}
		state, err := reading.Get(t.Context(), constants.LocalReaderID, publicID)
		if err != nil || state.ChatReadCursor != before.ChatReadCursor {
			t.Fatalf("persisted reading after rejected writes = %+v, %v", state, err)
		}
	})
}
