package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/conversation"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestReadingPublicMessageQualification(t *testing.T) {
	for _, tc := range []struct {
		// message 是原公开视图。
		message messageview.Message
		// want 是产品规定的可读类型。
		want string
	}{
		{messageview.Message{CreatedSequence: 2, Role: constants.MessageRoleUser, Content: "本人"}, ""},
		{messageview.Message{CreatedSequence: 2, Role: constants.MessageRoleUser, Actor: "team_member", Content: "成员正式报告"}, "member_report"},
		{messageview.Message{CreatedSequence: 2, Role: constants.MessageRoleAssistant, DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartProcessCommentary, Text: "过程"}}}, ""},
		{messageview.Message{CreatedSequence: 2, Role: constants.MessageRoleAssistant, DisplayParts: []messageview.MessageDisplayPart{{Type: constants.MessageDisplayPartText, Text: "公开答复"}}}, "answer"},
		{messageview.Message{CreatedSequence: 2, Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusFailed, ErrorCode: "ChatLogError"}, "failure"},
	} {
		if got := messageview.ReadingKind(tc.message); got != tc.want {
			t.Errorf("reading kind of %+v = %q, want %q", tc.message, got, tc.want)
		}
	}
}

func TestReadingProjectionAtomicRecovery(t *testing.T) {
	ctx := context.Background()
	db, m := newRecoveryTestService(t, t.TempDir())
	m.queryIndexes.reading = controllers.NewChatReadingController(db)
	chatID := "72727272727272727272727272727272"
	workspace := t.TempDir()
	if _, err := m.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{ID: chatID, WakerID: "reader", Title: "team", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_readable BEFORE INSERT ON chat_readable_messages BEGIN SELECT RAISE(ABORT, 'injected index failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := m.logStore.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "report", Data: recoveryTestData(t, eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Actor: "team_member", Content: "成员报告"})}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.catchUpLiveProjection(ctx, chatID); err == nil {
		t.Fatal("projection succeeded while the derived index failed")
	}
	before, err := m.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil || before.LatestSequence != rows[0].Sequence-1 {
		t.Fatalf("chat cursor passed the unread index: %+v %v", before, err)
	}
	if err = db.Exec(`DROP TRIGGER fail_readable`).Error; err != nil {
		t.Fatal(err)
	}
	if err = m.catchUpLiveProjection(ctx, chatID); err != nil {
		t.Fatal(err)
	}
	state, err := m.queryIndexes.reading.Get(ctx, constants.LocalReaderID, chatID)
	if err != nil || state.UnreadMessageCount != 1 || state.FirstUnreadMessageID != "report" {
		t.Fatalf("replayed unread report: %+v %v", state, err)
	}
	if err = m.queryIndexes.reading.MarkUnread(ctx, constants.LocalReaderID, chatID, "report", 0); err != nil {
		t.Fatal(err)
	}
	after, err := m.queryIndexes.chatController.Get(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.queryIndexes.replaceCurrentProjection(ctx, currentProjection{chat: after}); err != nil {
		t.Fatal(err)
	}
	state, err = m.queryIndexes.reading.Get(ctx, constants.LocalReaderID, chatID)
	if err != nil || state.UnreadGeneration != 1 || !state.HoldUnread || state.UnreadMessageCount != 1 {
		t.Fatalf("projection rebuild changed user facts: %+v %v", state, err)
	}
}

func TestReadingFailedProjectionRepairAndOverflow(t *testing.T) {
	ctx := context.Background()
	db, m := newRecoveryTestService(t, t.TempDir())
	m.queryIndexes.reading = controllers.NewChatReadingController(db)
	m.readingRepair = newReadingRepair()
	m.queryIndexes.failed = m.registerReadingFailure
	chatID := "81818181818181818181818181818185"
	workspace := t.TempDir()
	if _, err := m.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{ID: chatID, WakerID: "reader", Title: "late report", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.logStore.Append(chatID, []eventlog.AppendInput{{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "report", Data: recoveryTestData(t, eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Actor: "team_member", Content: "待修复"})}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < readingRepairLimit; i++ {
		m.registerReadingFailure(fmt.Sprintf("%032x", i+1000), status_error.DatabaseError)
	}
	m.registerReadingFailure(chatID, status_error.DatabaseError)
	if m.ReadingIncompleteReason(nil) != "recovering" || len(m.readingRepair.pending) != readingRepairLimit || !m.readingRepair.overflow {
		t.Fatal("overflow was not covered by original projection owner")
	}
	if more, failed := m.repairReadingPass(ctx); more || failed {
		t.Fatalf("repair did not drain covered log and deleted targets: %v %v", more, failed)
	}
	if m.ReadingIncompleteReason(nil) != "" {
		t.Fatal("complete projected catalog remained incomplete")
	}
	state, err := m.queryIndexes.reading.Get(ctx, constants.LocalReaderID, chatID)
	if err != nil || state.UnreadMessageCount != 1 || state.FirstUnreadMessageID != "report" {
		t.Fatalf("overflow omitted a real log: %+v %v", state, err)
	}
}

func TestReadingStartupHistoryAndInterruptedFailureUseSeparateBoundaries(t *testing.T) {
	ctx := context.Background()
	db, m := newRecoveryTestService(t, t.TempDir())
	m.queryIndexes.reading = controllers.NewChatReadingController(db)
	m.readingRepair = newReadingRepair()
	m.beforeRecovery = func(ctx context.Context, module Module) error { return module.InitializeReadingBaseline(ctx) }
	chatID := "91919191919191919191919191919195"
	workspace := t.TempDir()
	if _, err := m.logStore.Create(chatID, []eventlog.AppendInput{
		{Type: constants.ChatEventTypeCreated, Data: recoveryTestData(t, eventlog.ChatCreatedData{WakerID: "reader", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}})},
		{Type: constants.ChatEventTypeUserMessageCreated, MessageID: "historical-report", Data: recoveryTestData(t, eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Actor: "team_member", Content: "历史"})},
		{Type: constants.ChatEventTypeExecutionCreated, ExecutionID: "interrupted", Data: recoveryTestData(t, eventlog.ExecutionCreatedData{RuntimeID: "codex"})},
		{Type: constants.ChatEventTypeElicitationRequested, ExecutionID: "interrupted", Data: recoveryTestData(t, eventlog.ElicitationRequestedData{
			ElicitationID: "interrupted-question", Mode: "url", Message: "Authorize", URL: "https://example.invalid/?token=secret",
		})},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := m.queryIndexes.reading.Get(ctx, constants.LocalReaderID, chatID)
	if err != nil || state.UnreadMessageCount != 1 || state.FirstUnreadMessageID != "interrupted" || state.ThroughSequence != 2 {
		t.Fatalf("startup marked the new failure or lost historical baseline: %+v %v", state, err)
	}
	page, err := m.ReadMessageTimeline(ctx, chatID, 0, 0, 1)
	if err != nil || len(page.Resources) != 1 || page.Resources[0].ID != "interrupted-question" {
		t.Fatalf("question history after restart = %+v, %v", page, err)
	}
	question := page.Resources[0].Question
	if question == nil || question.Result != constants.ElicitationResultInvalidated || question.URL != "" {
		t.Fatalf("interrupted URL question was not closed safely: %+v", question)
	}
	events, err := m.ListEvents(ctx, chatID, page.Resources[0].CreatedSequence, 0)
	if err != nil || len(events) != 2 || events[0].Type != constants.ChatEventTypeElicitationResolved ||
		events[0].Message == nil || events[0].Message.ID != "interrupted-question" || events[1].Type != constants.ChatEventTypeExecutionFailed {
		t.Fatalf("recovery must commit an explicit question resolution before execution ends: %+v %v", events, err)
	}
}

func TestReadingInitializationIsolatesBrokenChat(t *testing.T) {
	ctx := context.Background()
	db, m := newRecoveryTestService(t, t.TempDir())
	m.queryIndexes.reading = controllers.NewChatReadingController(db)
	m.readingRepair = newReadingRepair()
	m.beforeRecovery = func(ctx context.Context, module Module) error { return module.InitializeReadingBaseline(ctx) }
	healthy, broken := "91919191919191919191919191919196", "91919191919191919191919191919197"
	workspace := t.TempDir()
	created := eventlog.AppendInput{Type: constants.ChatEventTypeCreated, Data: recoveryTestData(t, eventlog.ChatCreatedData{WakerID: "reader", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}})}
	if _, err := m.logStore.Create(healthy, []eventlog.AppendInput{created, {Type: constants.ChatEventTypeUserMessageCreated, MessageID: "historical-report", Data: recoveryTestData(t, eventlog.MessageCreatedData{Role: constants.MessageRoleUser, Actor: "team_member", Content: "历史"})}}); err != nil {
		t.Fatal(err)
	}
	path, err := m.logStore.LogPath(broken)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if m.ReadingIncompleteReason(nil) != "isolated" || m.ReadingIncompleteReason([]string{healthy}) != "" {
		t.Fatal("one corrupt log hid all healthy reading")
	}
	if exists, err := m.queryIndexes.reading.HasCursor(ctx, constants.LocalReaderID, broken); err != nil || exists {
		t.Fatalf("damaged log gained a fake cursor: %v %v", exists, err)
	}
	state, err := m.queryIndexes.reading.Get(ctx, constants.LocalReaderID, healthy)
	if err != nil || state.ThroughSequence != 2 || state.UnreadMessageCount != 0 {
		t.Fatalf("healthy history was lost: %+v %v", state, err)
	}
}

func TestCatchUpReadingRejectsLostAuthority(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%v", remove), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := conversation.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			db, m := newRecoveryTestService(t, root, store)
			m.queryIndexes.reading = controllers.NewChatReadingController(db)
			m.readingRepair = newReadingRepair()
			id := "91919191919191919191919191919198"
			workspace := t.TempDir()
			if _, err := m.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{ID: id, WakerID: "reader", WorkspaceDirectory: workspace, AdditionalWorkingDirectories: []string{workspace}}}}); err != nil {
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
			if err := m.AppendParticipantMessages(ctx, id, []ParticipantMessageInput{{ID: "report", Actor: string(constants.ChannelMessageActorMemberReport), Content: "报告", SourceWakerID: "member-waker", SourceChatID: "member-chat"}}); err != nil {
				t.Fatal(err)
			}
			if remove {
				err = os.Remove(path)
			} else {
				err = os.Truncate(path, info.Size())
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = m.CatchUpReading(ctx, id); !errors.Is(err, status_error.ChatLogCorrupted) {
				t.Fatalf("authority loss accepted: %v", err)
			}
			if got := m.ReadingIncompleteReason([]string{id}); got != "isolated" {
				t.Fatalf("authority loss is untracked: %q", got)
			}
			m.readingRepair = newReadingRepair()
			if err = m.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if got := m.ReadingIncompleteReason([]string{id}); got != "isolated" {
				t.Fatalf("authority loss cleared at startup: %q", got)
			}
		})
	}
}
