package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"

	"context"
	"errors"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

func TestSubmissionIdentityPreservesEntryRequestAndWorkflowLength(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	intent := WorkIntent{
		ClientRequestID: "internal-resource-creation",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:           WorkInput{Actor: "user", SubmissionID: strings.Repeat("w", 128), Content: "工作流输入"},
		Options:         WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
	}
	for index := 0; index < 2; index++ {
		accepted, err := manager.execution.Start(context.Background(), intent)
		if err != nil {
			t.Fatal(err)
		}
		waitForChatTerminal(t, manager, accepted.Chat.ID)
		if accepted.Execution.SubmissionID != intent.Input.SubmissionID {
			t.Fatal("entry input identity was replaced by internal creation identity")
		}
		detail, err := manager.chats.(chatservice.Module).ReadMessageHistory(context.Background(), accepted.Chat.ID, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, message := range detail.Resources {
			if message.ID == accepted.Execution.ID || message.ID == accepted.Execution.UserMessageID {
				if message.SubmissionID != intent.Input.SubmissionID {
					t.Fatal("entry identity was lost by user or assistant projection")
				}
				found++
			}
		}
		if found != 2 {
			t.Fatal("missing user or assistant entry input projection")
		}
		intent.ClientRequestID = ""
		intent.Chat = ChatTarget{ChatID: accepted.Chat.ID}
		intent.Input.SubmissionID = strings.Repeat("c", 128)
	}
	intent.Input.SubmissionID = strings.Repeat("x", 129)
	if _, err := manager.execution.Start(context.Background(), intent); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("oversized input identity = %v", err)
	}
	intent.Chat = ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}
	intent.ClientRequestID = strings.Repeat("x", 65)
	intent.Input.SubmissionID = ""
	if _, err := manager.execution.Start(context.Background(), intent); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("ordinary creation identity limit changed: %v", err)
	}
}

func TestContinuationSubmissionReceiptSurvivesJournalReloadAndRejectsChangedInput(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := context.Background()
	first, err := manager.execution.Start(ctx, WorkIntent{ClientRequestID: "first", ExecutionID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "开始"}, Options: WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	intent := WorkIntent{ExecutionID: util.New(), Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", SubmissionID: "platform-message", Content: "整理结果"}, Options: WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
	accepted, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	// 强制重新读取持久事实，重放不能依赖 Runtime 内存或 IM 接纳标记。
	core := manager.execution.(*service)
	receipt, err := core.journal.ReadSubmission(ctx, first.Chat.ID, "platform-message")
	if err != nil || receipt == nil || receipt.ID != accepted.Execution.ID {
		t.Fatalf("submission absent from journal: %+v %v", receipt, err)
	}
	intent.ExecutionID = util.New()
	replayed, err := manager.execution.Start(ctx, intent)
	if err != nil || !replayed.Replayed || replayed.Execution.ID != accepted.Execution.ID {
		t.Fatalf("continuation replay: %+v %v", replayed, err)
	}
	if len(manager.nativeCalls(t, "turn/start")) != 2 {
		t.Fatal("replay started duplicate Runtime")
	}
	intent.Input.Content = "changed"
	if _, err := manager.execution.Start(ctx, intent); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed content reused submission identity: %v", err)
	}
}

func TestSteerRelationSurvivesExecutionAndHistoryReplay(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	accepted, err := manager.execution.Start(context.Background(), WorkIntent{
		ClientRequestID: "steer-test",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:           WorkInput{Actor: "user", Content: "追加要求", SteerExecutionID: "previous"},
		Options:         WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	detail, err := manager.chats.(chatservice.Module).ReadMessageHistory(context.Background(), accepted.Chat.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range detail.Resources {
		if message.ID == accepted.Execution.ID {
			if message.SteerExecutionID != "previous" {
				t.Fatalf("steer relation lost: %+v", message)
			}
			return
		}
	}
	t.Fatal("missing assistant message")
}
