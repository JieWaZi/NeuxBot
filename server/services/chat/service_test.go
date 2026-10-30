package chat

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	eventlog "github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestNewRejectsEmptyLogDirectory(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() error = nil, want missing log directory rejected")
	}
}

func TestAppendParticipantMessagesIsIdempotentAndPreservesSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "participant-messages.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	chatService := newTestService(t, Config{
		StorageDirectory: t.TempDir(),
		QueryIndexes: QueryIndexDependencies{
			Chats:           controllers.NewChatController(database.DB),
			RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			DeletionIntents: controllers.NewChatDeletionIntentController(database.DB),
			Transactions: controllers.NewTransactionController(
				dbhelper.NewGormTransactionManager(database.DB),
			),
		},
	})
	chatID := "91919191919191919191919191919191"
	workspaceDirectory := t.TempDir()
	if _, err := chatService.CreateDormantChats(ctx, []DormantChat{{
		Chat: models.Chat{
			ID:                           chatID,
			WakerID:                      "lead-waker",
			WorkspaceDirectory:           workspaceDirectory,
			AdditionalWorkingDirectories: []string{workspaceDirectory},
			Title:                        "团队主会话",
		},
	}}); err != nil {
		t.Fatalf("create dormant lead chat: %v", err)
	}
	report := ParticipantMessageInput{
		ID:            "92929292929292929292929292929292",
		Actor:         string(constants.ChannelMessageActorMemberReport),
		Content:       "## 完成情况\n已完成。",
		SourceWakerID: "member-waker",
		SourceChatID:  "member-chat",
		Truncated:     true,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := chatService.AppendParticipantMessages(ctx, chatID, []ParticipantMessageInput{report}); err != nil {
			t.Fatalf("append participant messages attempt %d: %v", attempt+1, err)
		}
	}
	history, err := chatService.ReadMessageHistory(ctx, chatID, 0, 10)
	if err != nil {
		t.Fatalf("read lead chat history: %v", err)
	}
	if len(history.Resources) != 1 {
		t.Fatalf("participant message count = %d, want 1", len(history.Resources))
	}
	message := history.Resources[0]
	if message.Actor != string(constants.ChannelMessageActorMemberReport) ||
		message.SourceWakerID != report.SourceWakerID ||
		message.SourceChatID != report.SourceChatID || !message.Truncated {
		t.Fatalf("participant message metadata = %+v", message)
	}
}

func TestReadMessageHistoryReadsLatestAndOlderJSONLWindows(t *testing.T) {
	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "abababababababababababababababab"
	_, err = logStore.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WorkspaceDirectory: "/workspace",
			}),
		},
		{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: "user-1",
			Data: marshalData(eventlog.MessageCreatedData{
				Role:    constants.MessageRoleUser,
				Content: "第一轮",
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: "execution-1",
			Data:        marshalData(eventlog.ExecutionCreatedData{UserMessageID: "user-1"}),
		},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "execution-1"},
		{
			Type:        constants.ChatEventTypeAssistantMessage,
			ExecutionID: "execution-1",
			Data: marshalData(eventlog.RuntimeMessageData{
				MessageRevision: 6,
				Content: []eventlog.RuntimeMessageContent{{
					Type: "text_delta",
					Data: marshalData(struct {
						// Text 是第一轮测试回复的完整文本。
						Text string `json:"text"`
					}{Text: "回答一"}),
				}},
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCompleted,
			ExecutionID: "execution-1",
			Data: marshalData(eventlog.ExecutionFinishedData{
				Status: constants.ExecutionStatusCompleted,
			}),
		},
		{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: "user-2",
			Data: marshalData(eventlog.MessageCreatedData{
				Role:    constants.MessageRoleUser,
				Content: "第二轮",
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCreated,
			ExecutionID: "execution-2",
			Data:        marshalData(eventlog.ExecutionCreatedData{UserMessageID: "user-2"}),
		},
		{Type: constants.ChatEventTypeExecutionStarted, ExecutionID: "execution-2"},
		{
			Type:        constants.ChatEventTypeAssistantMessage,
			ExecutionID: "execution-2",
			Data: marshalData(eventlog.RuntimeMessageData{
				MessageRevision: 12,
				Content: []eventlog.RuntimeMessageContent{
					{
						Type: "tool_use",
						Data: marshalData(map[string]any{
							"name": "file_modify", "status": "in_progress", "toolCallId": "edit-2",
							"content": []any{map[string]any{
								"type": "diff", "path": "/workspace/projects/Desktop/main.go",
								"newText": "package main\n",
							}},
						}),
					},
					{
						Type: "tool_result",
						Data: marshalData(map[string]any{
							"status": "completed", "toolCallId": "edit-2",
						}),
					},
					{
						Type: "text_delta",
						Data: marshalData(struct {
							// Text 是第二轮测试回复的完整文本。
							Text string `json:"text"`
						}{Text: "回答二"}),
					},
				},
			}),
		},
		{
			Type:        constants.ChatEventTypeExecutionCompleted,
			ExecutionID: "execution-2",
			Data: marshalData(eventlog.ExecutionFinishedData{
				Status: constants.ExecutionStatusCompleted,
			}),
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	chatService := &service{
		logStore: logStore,
	}

	latest, err := chatService.ReadMessageHistory(context.Background(), chatID, 0, 2)
	if err != nil {
		t.Fatalf("latest ReadMessageHistory() error = %v", err)
	}
	if latest.ForkLatestExecutionID != "execution-2" || !latest.HasMore || latest.NextBeforeSequence != 7 ||
		latest.LatestSequence != 11 ||
		len(latest.Resources) != 2 || latest.Resources[1].Content != "回答二" {
		t.Fatalf("latest ReadMessageHistory() = %+v", latest)
	}
	if latest.Resources[0].CreatedSequence != 7 || latest.Resources[1].CreatedSequence != 8 {
		t.Fatalf("latest message creation sequences = %d, %d, want 7, 8",
			latest.Resources[0].CreatedSequence,
			latest.Resources[1].CreatedSequence,
		)
	}
	if changes := latest.Resources[1].FileChanges; len(changes) != 1 ||
		changes[0].Path != "projects/Desktop/main.go" || changes[0].AddedLines != 1 {
		t.Fatalf("latest file changes = %+v, want managed workspace relative path", changes)
	}
	if len(latest.Resources[1].RuntimeEvents) != 0 || len(latest.Resources[1].FileChangeDiffs) != 0 {
		t.Fatal("message history must not retain private runtime events or full file diffs")
	}
	diff, err := chatService.GetMessageFileChange(
		context.Background(),
		chatID,
		"execution-2",
		"projects/Desktop/main.go",
	)
	if err != nil {
		t.Fatalf("GetMessageFileChange() error = %v", err)
	}
	if diff.Path != "projects/Desktop/main.go" || diff.OldContent != "" ||
		diff.NewContent != "package main\n" {
		t.Fatalf("GetMessageFileChange() = %+v, want JSONL execution diff", diff)
	}

	older, err := chatService.ReadMessageHistory(context.Background(), chatID, 7, 2)
	if err != nil {
		t.Fatalf("older ReadMessageHistory() error = %v", err)
	}
	if older.ForkLatestExecutionID != "" || older.HasMore || len(older.Resources) != 2 || older.Resources[1].Content != "回答一" {
		t.Fatalf("older ReadMessageHistory() = %+v", older)
	}
}

func TestListEventsReadsAuthorityWithoutChatIndex(t *testing.T) {
	t.Parallel()
	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "10101010101010101010101010101010"
	if _, err := logStore.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            "waker-1",
				WorkspaceDirectory: "/tmp/project",
				Title:              "权威事件",
			}),
		},
		{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: "user-1",
			Data: marshalData(eventlog.MessageCreatedData{
				Role:    constants.MessageRoleUser,
				Content: "只读取 JSONL",
			}),
		},
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	chatService := newTestService(t, Config{StorageDirectory: logStore.Root()})

	events, err := chatService.ListEvents(context.Background(), chatID, 0, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 || events[1].Sequence != 2 || events[1].Message == nil ||
		events[1].Message.Content != "只读取 JSONL" {
		t.Fatalf("ListEvents() = %+v, want public events built from JSONL", events)
	}
}

func TestSubscribeEventsBacklogContainsOnlyPublicEventsAfterCursor(t *testing.T) {
	t.Parallel()
	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "20202020202020202020202020202020"
	if _, err := logStore.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            "waker-1",
				WorkspaceDirectory: "/tmp/project",
				Title:              "游标订阅",
			}),
		},
		{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: "user-1",
			Data: marshalData(eventlog.MessageCreatedData{
				Role:    constants.MessageRoleUser,
				Content: "第一条",
			}),
		},
		{
			Type:      constants.ChatEventTypeUserMessageCreated,
			MessageID: "user-2",
			Data: marshalData(eventlog.MessageCreatedData{
				Role:    constants.MessageRoleUser,
				Content: "第二条",
			}),
		},
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	chatService := newTestService(t, Config{StorageDirectory: logStore.Root()})

	subscription, err := chatService.SubscribeEvents(context.Background(), chatID, 2)
	if err != nil {
		t.Fatalf("SubscribeEvents() error = %v", err)
	}
	defer subscription.Close()
	if len(subscription.Backlog) != 1 || subscription.Backlog[0].Sequence != 3 ||
		subscription.Backlog[0].Message == nil ||
		subscription.Backlog[0].Message.Content != "第二条" {
		t.Fatalf("SubscribeEvents() backlog = %+v", subscription.Backlog)
	}
}

func TestMetadataFromRecordsUsesJSONLExecutionState(t *testing.T) {
	t.Parallel()
	records := []eventlog.Record{
		{
			ChatID:     "chat-1",
			Sequence:   1,
			RecordedAt: 10,
			Type:       constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:                      "waker-1",
				ProjectID:                    "project-1",
				ProjectName:                  "NeuxBot",
				WorkspaceDirectory:           "/tmp/project",
				AdditionalWorkingDirectories: []string{"/tmp/desktop", "/tmp/server"},
				Title:                        "初始标题",
			}),
		},
		{
			ChatID:      "chat-1",
			ExecutionID: "execution-1",
			Sequence:    2,
			RecordedAt:  20,
			Type:        constants.ChatEventTypeExecutionCreated,
			Data: marshalData(eventlog.ExecutionCreatedData{
				RuntimeID: "codex",
				Model:     "gpt-5",
			}),
		},
		{
			ChatID:      "chat-1",
			ExecutionID: "execution-1",
			Sequence:    3,
			RecordedAt:  30,
			Type:        constants.ChatEventTypeExecutionCompleted,
			Data: marshalData(eventlog.ExecutionFinishedData{
				Status: constants.ExecutionStatusCompleted,
			}),
		},
	}

	state, err := replayChatState(records)
	if err != nil {
		t.Fatalf("replayChatState() error = %v", err)
	}
	chat := state.chat
	if chat.ActiveExecutionID != "" ||
		chat.LatestSequence != 3 ||
		chat.ProjectID != "project-1" ||
		chat.ProjectName != "NeuxBot" ||
		!reflect.DeepEqual(
			chat.AdditionalWorkingDirectories,
			[]string{"/tmp/desktop", "/tmp/server"},
		) ||
		chat.LastRuntimeID != "codex" ||
		chat.LastModel != "gpt-5" {
		t.Fatalf("chat = %+v, want terminal state rebuilt from JSONL", chat)
	}
}

func TestMetadataFromRecordsAppliesLatestChatTitle(t *testing.T) {
	t.Parallel()
	records := []eventlog.Record{
		{
			ChatID:     "chat-1",
			Sequence:   1,
			RecordedAt: 1,
			Type:       constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            "waker-1",
				WorkspaceDirectory: "/tmp/project",
				Title:              "临时标题",
			}),
		},
		{
			ChatID:     "chat-1",
			Sequence:   2,
			RecordedAt: 2,
			Type:       constants.ChatEventTypeTitleUpdated,
			Data: marshalData(eventlog.ChatTitleUpdatedData{
				Title: "用户标题",
			}),
		},
	}

	state, err := replayChatState(records)
	if err != nil {
		t.Fatalf("replayChatState() error = %v", err)
	}
	chat := state.chat
	if chat.Title != "用户标题" {
		t.Fatalf("title = %q, want 用户标题", chat.Title)
	}
}

func TestReplayChatStateAppliesRuntimeSessionUpdatesAndClears(t *testing.T) {
	t.Parallel()
	records := []eventlog.Record{
		{
			ChatID:     "chat-1",
			Sequence:   1,
			RecordedAt: 10,
			Type:       constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            "waker-1",
				WorkspaceDirectory: "/tmp/project",
				Title:              "Session 重放",
			}),
		},
		{
			ChatID:     "chat-1",
			Sequence:   2,
			RecordedAt: 20,
			Type:       constants.ChatEventTypeNativeSessionUpdated,
			Data: marshalData(eventlog.RuntimeSessionData{
				RuntimeID:       "codex",
				SessionID:       "codex-session",
				LastExecutionID: "execution-1",
			}),
		},
		{
			ChatID:     "chat-1",
			Sequence:   3,
			RecordedAt: 30,
			Type:       constants.ChatEventTypeNativeSessionUpdated,
			Data: marshalData(eventlog.RuntimeSessionData{
				RuntimeID:       "claude",
				SessionID:       "claude-session",
				LastExecutionID: "execution-2",
			}),
		},
		{
			ChatID:     "chat-1",
			Sequence:   4,
			RecordedAt: 40,
			Type:       constants.ChatEventTypeNativeSessionCleared,
			Data: marshalData(eventlog.RuntimeSessionData{
				RuntimeID: "codex",
			}),
		},
	}

	state, err := replayChatState(records)
	if err != nil {
		t.Fatalf("replayChatState() error = %v", err)
	}
	if _, exists := state.runtimeSessions["codex"]; exists {
		t.Fatal("cleared Codex session remained in authoritative state")
	}
	claude, exists := state.runtimeSessions["claude"]
	if !exists || claude.SessionID != "claude-session" ||
		claude.LastExecutionID != "execution-2" || claude.UpdatedAt != 30 {
		t.Fatalf("Claude runtime session = %+v", claude)
	}
}

func TestLoadExecutionContextRepairsLaggingSessionProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "session-authority.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	logStore, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	chatID := "30303030303030303030303030303030"
	records, err := logStore.Create(chatID, []eventlog.AppendInput{
		{
			Type: constants.ChatEventTypeCreated,
			Data: marshalData(eventlog.ChatCreatedData{
				WakerID:            "waker-1",
				WorkspaceDirectory: "/tmp/project",
				Title:              "Session 权威状态",
			}),
		},
		{
			Type: constants.ChatEventTypeNativeSessionUpdated,
			Data: marshalData(eventlog.RuntimeSessionData{
				RuntimeID:       "codex",
				SessionID:       "jsonl-session",
				LastExecutionID: "execution-1",
			}),
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	chatController := controllers.NewChatController(database.DB)
	if err := chatController.Create(ctx, models.Chat{
		ID:                 chatID,
		WakerID:            "waker-1",
		WorkspaceDirectory: "/tmp/project",
		Title:              "Session 权威状态",
		LatestSequence:     records[0].Sequence,
		CreatedAt:          records[0].RecordedAt,
		UpdatedAt:          records[len(records)-1].RecordedAt,
	}); err != nil {
		t.Fatalf("create chat query index: %v", err)
	}
	chatService := newTestService(t, Config{
		StorageDirectory: logStore.Root(),
		QueryIndexes: QueryIndexDependencies{
			Chats:           chatController,
			RuntimeSessions: controllers.NewRuntimeSessionController(database.DB),
			Transactions:    controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)),
		},
	})

	detail, err := chatService.LoadExecutionContext(ctx, chatID)
	if err != nil {
		t.Fatalf("LoadExecutionContext() error = %v", err)
	}
	session, exists := detail.RuntimeSessions["codex"]
	if !exists || session.SessionID != "jsonl-session" ||
		session.LastExecutionID != "execution-1" {
		t.Fatalf("runtime session = %+v, want JSONL authority", session)
	}
}

// newTestService 使用具体构造器创建测试实现，并统一处理无效日志目录错误。
func newTestService(t *testing.T, dependencies Config) *service {
	t.Helper()
	implementation, err := newService(dependencies)
	if err != nil {
		t.Fatalf("newService() error = %v", err)
	}
	return implementation
}

func TestMessageForkWindowAndTerminalDoNotReplayEarlierRounds(t *testing.T) {
	store, err := eventlog.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "abababababababababababababababab"
	facts := []eventlog.AppendInput{{Type: constants.ChatEventTypeCreated, Data: marshalData(eventlog.ChatCreatedData{WorkspaceDirectory: "/workspace", WakerID: "waker"})}}
	for _, id := range []string{"old", "new"} {
		configuration := marshalData(eventlog.ExecutionCreatedData{RuntimeID: "runtime", UserMessageID: "user-" + id})
		invocation := marshalData(eventlog.InvocationFinishedData{RecoveryStrategy: constants.InvocationRecoveryInitial, RuntimeID: "runtime", SessionOutputID: "private-session", ForkMode: "turn", ForkPosition: "private-position"})
		if id == "old" {
			invocation = json.RawMessage(`{"runtimeId":7}`)
			configuration = json.RawMessage(`{"runtimeId":7}`)
		}
		facts = append(facts,
			eventlog.AppendInput{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "user-" + id, Data: marshalData(eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Content: id})},
			eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: id, Data: configuration},
			eventlog.AppendInput{Type: constants.ChatEventTypeInvocationFinished, ExecutionID: id, Data: invocation},
			eventlog.AppendInput{Type: constants.ChatEventTypeExecutionCompleted, ExecutionID: id, Data: marshalData(eventlog.ExecutionFinishedData{Status: constants.ExecutionStatusCompleted})},
		)
	}
	if _, err := store.Create(chatID, facts); err != nil {
		t.Fatal(err)
	}
	manager := &service{logStore: store}
	if _, _, err := manager.loadChatState(chatID); err == nil {
		t.Fatal("sentinel old invocation did not reject full replay")
	}
	page, err := manager.ReadMessageHistory(context.Background(), chatID, 0, 1)
	if err != nil || len(page.Resources) != 1 || page.ForkLatestExecutionID != "new" || page.Resources[0].Fork == nil || !page.Resources[0].Fork.Available {
		t.Fatalf("window replayed earlier fork rounds: %+v %v", page, err)
	}
	window, err := store.ReadProjectionAfter(chatID, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := eventstream.ReplayAfter(window.Records, 5)
	if err != nil {
		t.Fatal(err)
	}
	events := adapter.EventsAfter(5, 0)
	terminal := events[len(events)-1]
	if terminal.Message == nil || terminal.Message.Fork == nil || !terminal.Message.Fork.Available {
		t.Fatalf("incremental terminal lost fork capability: %+v", terminal)
	}
}
