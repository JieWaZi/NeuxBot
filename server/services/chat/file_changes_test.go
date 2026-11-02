package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
)

func TestFileChangesReadKeepsActivePendingAndStartupRecoversOnlyItsOriginalBaseline(t *testing.T) {
	service, chatID := forkFixture(t)
	chat, err := service.LoadExecutionChat(context.Background(), chatID)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(chat.WorkspaceDirectory, "observed.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	recording, err := service.BeginFileChanges(context.Background(), chat, "after", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 先验证公开 pending 的正常读取与活动采集恢复都不改写它。
	if err = recording.PublishPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	summary, err := service.GetMessageFileChanges(context.Background(), chatID, "after")
	if err != nil || summary.State != "pending" {
		t.Fatalf("a normal read mutated active capture: %+v %v", summary, err)
	}
	if err := os.WriteFile(file, []byte("after-restart\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = service.recoverFileChanges(context.Background()); err != nil {
		t.Fatal(err)
	}
	summary, err = service.GetMessageFileChanges(context.Background(), chatID, "after")
	if err != nil || summary.State != "pending" || summary.Version != 1 {
		t.Fatalf("recovery took over an active capture: %+v %v", summary, err)
	}
	directory, err := service.logStore.FileChangesDirectory(chatID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "changes", "records", summary.ID, "record.json")
	pending, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 释放测试进程中的租约，再恢复崩溃前的原始磁盘现场：真实退出只释放 OS 租约，
	// 不会执行 Close 提交的 incomplete 事件，也不会取得结束侧快照。
	if err := recording.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pending, 0600); err != nil {
		t.Fatal(err)
	}
	if err = service.recoverFileChanges(context.Background()); err != nil {
		t.Fatal(err)
	}
	summary, err = service.GetMessageFileChanges(context.Background(), chatID, "after")
	if err != nil || summary.State != "incomplete" || summary.Version != 2 || summary.TotalFiles != 0 {
		t.Fatalf("recovery invented final contents: %+v %v", summary, err)
	}
	records, err := service.logStore.ReadFileChangesReference(chatID, "after")
	if err != nil {
		t.Fatal(err)
	}
	var latest eventlog.FileChangesData
	for _, record := range records {
		if record.Type == constants.ChatEventTypeFileChanges {
			if err = json.Unmarshal(record.Data, &latest); err != nil {
				t.Fatal(err)
			}
		}
	}
	if latest.State != "incomplete" || latest.Version != 2 {
		t.Fatalf("authority retained stale pending: %+v", latest)
	}
}

func TestLateFileChangesAdvanceUnreadVersionWithoutReopeningExecution(t *testing.T) {
	ctx := context.Background()
	service, chatID := forkFixture(t)
	if _, err := service.logStore.Append(chatID, workResultRound("readable", constants.RuntimeEventTypeTextDelta, "answer")); err != nil {
		t.Fatal(err)
	}
	database, recovered := newRecoveryTestService(t, service.logStore.Root())
	results := controllers.NewWorkResultController(database)
	if err := results.EnableTracking(ctx, 1); err != nil {
		t.Fatal(err)
	}
	recovered.queryIndexes.results = results
	if err := recovered.Start(ctx); err != nil {
		t.Fatal(err)
	}
	chat, err := recovered.LoadExecutionChat(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	recording, err := recovered.BeginFileChanges(ctx, chat, "readable", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recording.Close()
	if err = recording.PublishPending(ctx); err != nil {
		t.Fatal(err)
	}
	first, found, err := results.Get(ctx, constants.WorkResultOwnerKindChat, chatID)
	if err != nil || !found {
		t.Fatalf("initial readable result missing: %+v %v", first, err)
	}
	if _, err = results.MarkViewed(ctx, first.OwnerKind, first.OwnerID, first.LatestVersion, first.AvailableAt+1); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(chat.WorkspaceDirectory, "result.txt"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = recording.Finish(ctx); err != nil {
		t.Fatal(err)
	}
	latest, _, err := results.Get(ctx, first.OwnerKind, first.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.LatestVersion <= first.LatestVersion || latest.ViewedVersion != first.LatestVersion || latest.Reference.FileChangeRecordID != recording.capture.Summary().ID || latest.Reference.FileChangeRecordVersion != 2 {
		t.Fatalf("late delivery cleared precise read state: %+v", latest)
	}
	if _, err = results.MarkViewed(ctx, latest.OwnerKind, latest.OwnerID, first.LatestVersion, latest.AvailableAt+1); err != nil {
		t.Fatal(err)
	}
	after, _, _ := results.Get(ctx, first.OwnerKind, first.OwnerID)
	if after.ViewedVersion != first.LatestVersion {
		t.Fatal("stale read confirmed newer file version")
	}
	current, err := recovered.GetChatSummary(ctx, chatID)
	if err != nil || current.Chat.ActiveExecutionID != "" || current.Chat.LastEndedExecutionStatus != constants.ExecutionStatusCompleted {
		t.Fatalf("late file outcome reopened execution: %+v %v", current, err)
	}
}

func TestForkClonesOnlySelectedHistoricalFileRecordAndSurvivesSourceDeletion(t *testing.T) {
	ctx := context.Background()
	service, parent := forkFixture(t)
	chat, err := service.LoadExecutionChat(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(chat.WorkspaceDirectory, "branch-file.txt")
	if err = os.WriteFile(file, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	recording, err := service.BeginFileChanges(ctx, chat, "before", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = recording.PublishPending(ctx); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = recording.Finish(ctx); err != nil {
		t.Fatal(err)
	}
	source, err := service.GetMessageFileChanges(ctx, parent, "before")
	if err != nil {
		t.Fatal(err)
	}
	child, err := service.ForkChat(ctx, ForkInput{ChatID: parent, ExecutionID: "before", ClientRequestID: "file-copy"}, NativeFork{Execute: func(context.Context, NativeForkInput) (string, error) { return "independent-child", nil }})
	if err != nil {
		t.Fatal(err)
	}
	history, err := service.ReadMessageHistory(ctx, child, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	inherited := ""
	for _, message := range history.Resources {
		if message.FileChangeRecord != nil {
			inherited = message.ID
		}
	}
	if inherited == "" {
		t.Fatal("fork lost late file result beyond original terminal boundary")
	}
	if err = service.DeleteChat(ctx, parent); err != nil {
		t.Fatal(err)
	}
	copied, err := service.GetMessageFileChanges(ctx, child, inherited)
	if err != nil || copied.ID != source.ID || copied.TotalFiles != 1 {
		t.Fatalf("child depends on deleted source: %+v %v", copied, err)
	}
	diff, err := service.GetMessageFileChange(ctx, child, inherited, source.Files[0].AbsolutePath)
	if err != nil || diff.Comparison == nil {
		t.Fatalf("copied immutable diff missing: %+v %v", diff, err)
	}
}

func TestFileResultReadDoesNotExposeUncommittedCompletionAndPreservesExactPath(t *testing.T) {
	ctx := context.Background()
	service, chatID := forkFixture(t)
	chat, err := service.LoadExecutionChat(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	recording, err := service.BeginFileChanges(ctx, chat, "after", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recording.Close()
	if err = recording.PublishPending(ctx); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(chat.WorkspaceDirectory, "space and\ncontrol.txt ")
	if err = os.WriteFile(target, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	completed, err := recording.capture.Finish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.GetMessageFileChanges(ctx, chatID, "after")
	if err != nil || pending.State != "pending" || len(pending.Files) != 0 || pending.Version != 1 {
		t.Fatalf("uncommitted final capture leaked: %+v %v", pending, err)
	}
	if err = recording.publish(ctx, *completed); err != nil {
		t.Fatal(err)
	}
	ready, err := service.GetMessageFileChanges(ctx, chatID, "after")
	if err != nil || len(ready.Files) != 1 {
		t.Fatalf("committed result missing: %+v %v", ready, err)
	}
	exact := ready.Files[0].AbsolutePath
	diff, err := service.GetMessageFileChange(ctx, chatID, "after", exact)
	if err != nil || diff.Path != exact || diff.Comparison == nil {
		t.Fatalf("exact filename changed at API boundary: %+v %v", diff, err)
	}
}
