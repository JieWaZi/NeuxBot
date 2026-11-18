package chat

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"os"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

func TestRetentionProtectionRecoveryAndExpiredDeletion(t *testing.T) {
	ctx := t.Context()
	db, chats, _ := newDeleteTestService(t)
	id := "abcdefabcdefabcdefabcdefabcdefab"
	cwd, err := chats.storage.AgentWorkspace("partner")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureAgentWorkspace(cwd, "partner"); err != nil {
		t.Fatal(err)
	}
	created, err := chats.CreateDormantChats(ctx, []DormantChat{{Chat: models.Chat{ID: id, WakerID: "partner", WorkspaceDirectory: cwd, AdditionalWorkingDirectories: []string{t.TempDir()}}}})
	if err != nil {
		t.Fatal(err)
	}
	activity := created[0].CreatedAt
	cutoff := activity + 1
	candidates := func() []controllers.ChatRetentionCandidate {
		t.Helper()
		rows, err := chats.ListRetentionCandidates(ctx, cutoff, 0, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	initial := candidates()
	if len(initial) != 1 || initial[0].LastActivityAt != activity {
		t.Fatalf("initial candidates: %+v", initial)
	}
	yes := func(context.Context, string) (bool, error) { return true, nil }
	if err := chats.SetRetentionProtection(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := chats.RenameChat(ctx, id, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Chat{}).Where("id = ?", id).Updates(map[string]any{"last_activity_at": 0, "retention_protected": false}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := chats.recovery.reconcileChat(ctx, id); err != nil {
		t.Fatal(err)
	}
	summary, err := chats.GetChatSummary(ctx, id)
	if err != nil || !summary.Chat.RetentionProtected || summary.Chat.LastActivityAt != activity {
		t.Fatalf("backfill at log tail: %+v %v", summary, err)
	}
	if rows := candidates(); len(rows) != 0 {
		t.Fatalf("protected candidate: %+v", rows)
	}
	if ok, err := chats.DeleteChatIfExpired(ctx, initial[0], cutoff, yes); ok || err != nil {
		t.Fatalf("protected delete: %v %v", ok, err)
	}
	if err := chats.SetRetentionProtection(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	current := candidates()[0]
	if ok, err := chats.DeleteChatIfExpired(ctx, initial[0], cutoff, yes); ok || err != nil {
		t.Fatalf("stale delete: %v %v", ok, err)
	}
	release, err := chatstorage.AcquireOperation(chats.storage.Root(), id)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := chats.DeleteChatIfExpired(ctx, current, cutoff, yes); ok || err != nil {
		t.Fatalf("busy delete: %v %v", ok, err)
	}
	if n, err := chats.CountRetentionCandidates(ctx, cutoff); err != nil || n != 0 {
		t.Fatalf("admission visible in candidate count: %d %v", n, err)
	}
	release()
	if n, err := chats.CountRetentionCandidates(ctx, cutoff); err != nil || n != 1 {
		t.Fatalf("released admission missing: %d %v", n, err)
	}
	if ok, err := chats.DeleteChatIfExpired(ctx, current, cutoff, func(context.Context, string) (bool, error) { return false, nil }); ok || err != nil {
		t.Fatalf("revoked delete: %v %v", ok, err)
	}
	link := models.IMConversationLink{ID: "link", BindingID: "binding", ChatID: id}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := chats.DeleteChatIfExpired(ctx, current, cutoff, yes); ok || err != nil {
		t.Fatalf("linked delete: %v %v", ok, err)
	}
	if err := db.Delete(&link).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := chats.DeleteChatIfExpired(ctx, current, cutoff, yes); !ok || err != nil {
		t.Fatalf("expired delete: %v %v", ok, err)
	}
	if _, err := os.Stat(cwd); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionActivityUsesOriginalCheckpointTime(t *testing.T) {
	rows := []eventlog.Record{
		{ChatID: "id", Type: constants.ChatEventTypeCreated, Sequence: 1, RecordedAt: 10, Data: marshalData(eventlog.ChatCreatedData{WakerID: "partner"})},
		{ChatID: "id", MessageID: "native", Sequence: 2, RecordedAt: 20},
		{ChatID: "id", Type: constants.ChatEventTypeTitleUpdated, Sequence: 3, RecordedAt: 30, Data: marshalData(eventlog.ChatTitleUpdatedData{Title: "new"})},
	}
	state, err := replayChatState(rows)
	if err != nil || state.chat.LastActivityAt != 20 {
		t.Fatalf("native checkpoint activity: %+v %v", state.chat, err)
	}
}

func TestRetentionKeepsPublishedForkAndRecoversCommittedPurge(t *testing.T) {
	ctx := t.Context()
	chats, parent := forkFixture(t)
	if _, err := chats.recovery.reconcileChat(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := chats.SetRetentionProtection(ctx, parent, true); err != nil {
		t.Fatal(err)
	}
	child, err := chats.ForkChat(ctx, ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "retention-fork"}, NativeFork{Execute: func(context.Context, NativeForkInput) (agent.NativeBinding, error) {
		return forkTestBinding("native-child"), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := chats.GetChatSummary(ctx, child)
	if err != nil || summary.Chat.RetentionProtected {
		t.Fatalf("child inherited retention flag: %+v %v", summary, err)
	}
	if err := chats.SetRetentionProtection(ctx, parent, false); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().Add(time.Hour).UnixMilli()
	rows, err := chats.ListRetentionCandidatesByIDs(ctx, cutoff, []string{parent})
	if err != nil || len(rows) != 1 {
		t.Fatalf("parent candidate: %+v %v", rows, err)
	}
	chats.deletions.SetChatCleanup(func(map[string]string) error { return status_error.ManagedWorkspaceError })
	yes := func(context.Context, string) (bool, error) { return true, nil }
	if deleted, err := chats.DeleteChatIfExpired(ctx, rows[0], cutoff, yes); !deleted || !errors.Is(err, status_error.ManagedWorkspaceError) {
		t.Fatalf("postcommit purge status: %v %v", deleted, err)
	}
	intents, err := chats.deletionIntents.List(ctx)
	if err != nil || len(intents) != 1 || intents[0].State != "committed" {
		t.Fatalf("pending purge intents: %+v %v", intents, err)
	}
	chats.deletions.SetChatCleanup(nil)
	if err = chats.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer chats.Close()
	intents, err = chats.deletionIntents.List(ctx)
	if err != nil || len(intents) != 0 {
		t.Fatalf("recovered intents: %+v %v", intents, err)
	}
	cwd, err := chats.storage.ChatRoot(child)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(cwd, "attachments", "before.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionRechecksNativeProgressAfterIdleClose(t *testing.T) {
	ctx := t.Context()
	chats, id := forkFixture(t)
	cutoff := time.Now().Add(time.Hour).UnixMilli()
	if _, err := chats.recovery.reconcileChat(ctx, id); err != nil {
		t.Fatal(err)
	}
	rows, err := chats.ListRetentionCandidatesByIDs(ctx, cutoff, []string{id})
	if err != nil || len(rows) != 1 {
		t.Fatalf("candidate: %+v %v", rows, err)
	}
	chats.closeNativeThread = func(context.Context, string) error { t.Fatal("force close during retention"); return nil }
	chats.closeIdleNativeThread = func(_ context.Context, id string) error {
		_, err := chats.logStore.Append(id, []eventlog.AppendInput{{Type: constants.ChatEventTypeTitleUpdated, Data: marshalData(eventlog.ChatTitleUpdatedData{Title: "late"})}})
		return err
	}
	ok, err := chats.DeleteChatIfExpired(ctx, rows[0], cutoff, func(context.Context, string) (bool, error) {
		t.Fatal("authorization requested for stale cursor")
		return true, nil
	})
	if err != nil || ok {
		t.Fatalf("stale cursor: %v %v", ok, err)
	}
}
