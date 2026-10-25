package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
)

func TestContinuationSubmissionReceiptSurvivesJournalReloadAndRejectsChangedInput(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	ctx := context.Background()
	first, err := manager.execution.Start(ctx, WorkIntent{ClientRequestID: "first", ExecutionID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "开始"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	intent := WorkIntent{ExecutionID: util.New(), Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", SubmissionID: "platform-message", Content: "整理结果"}, Options: WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
	accepted, err := manager.execution.Start(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	// 强制重新读取持久事实，重放不能依赖 Runtime 内存或 IM 接纳标记。
	core := manager.execution.(*service)
	detail, err := core.journal.LoadExecutionContext(ctx, first.Chat.ID)
	if err != nil || detail.SubmittedExecutions["platform-message"].ID != accepted.Execution.ID {
		t.Fatalf("submission absent from journal: %+v %v", detail.SubmittedExecutions, err)
	}
	intent.ExecutionID = util.New()
	replayed, err := manager.execution.Start(ctx, intent)
	if err != nil || !replayed.Replayed || replayed.Execution.ID != accepted.Execution.ID {
		t.Fatalf("continuation replay: %+v %v", replayed, err)
	}
	if len(runtime.requests) != 2 {
		t.Fatal("replay started duplicate Runtime")
	}
	intent.Input.Content = "changed"
	if _, err := manager.execution.Start(ctx, intent); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed content reused submission identity: %v", err)
	}
}

func TestContextInstructionsStayOutOfUserMessageAndTitle(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	accepted, err := manager.execution.Start(context.Background(), WorkIntent{
		ClientRequestID: "context-only", ExecutionID: util.New(),
		Chat:      ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:     WorkInput{Actor: "user", Content: "你是谁", ContextInstructions: []workinput.Instruction{{Name: "DELIVERY.md", Content: "结果自动发送到已批准的接收方。"}}},
		Options:   WorkOptions{RuntimeID: runtimes.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
		Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	if accepted.Chat.Title != "你是谁" || len(runtime.requests) != 1 || runtime.requests[0].Message.Text != "你是谁" {
		t.Fatalf("执行说明污染消息或标题: %+v %+v", accepted.Chat, runtime.requests)
	}
	found := false
	for _, instruction := range runtime.requests[0].Instructions {
		if instruction.Name == "DELIVERY.md" && instruction.Content == "结果自动发送到已批准的接收方。" {
			found = true
		}
	}
	if !found {
		t.Fatal("执行上下文未传给智能体")
	}
}
