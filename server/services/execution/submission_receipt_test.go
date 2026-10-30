package execution

import (
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"

	"context"
	"encoding/json"
	"errors"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"reflect"
	"strings"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventlog"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/eventstream"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
)

func TestSubmissionIdentityPreservesEntryRequestAndWorkflowLength(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir(), &fakeRuntime{})
	intent := WorkIntent{
		ClientRequestID: "internal-resource-creation",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:           WorkInput{Actor: "user", SubmissionID: strings.Repeat("w", 128), Content: "工作流输入"},
		Options:         WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
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

func TestSubmissionIdentityProjectsBeforeExecutionAndReceipt(t *testing.T) {
	root := t.TempDir()
	manager := newMessageTestService(t, root, &fakeRuntime{})
	intent := WorkIntent{
		ClientRequestID: "first-input",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:           WorkInput{Actor: "user", Content: "相同正文"},
		Options:         WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
	}
	store, err := eventlog.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for index, submissionID := range []string{"first-input", "continued-input", ""} {
		if index > 0 {
			intent.ClientRequestID = ""
			intent.Input.SubmissionID = submissionID
			if submissionID == "" {
				intent.Input.Actor = "workflow"
			}
		}
		accepted, err := manager.execution.Start(context.Background(), intent)
		if err != nil {
			t.Fatal(err)
		}
		waitForChatTerminal(t, manager, accepted.Chat.ID)
		if accepted.Execution.SubmissionID != submissionID {
			t.Fatalf("accepted identity = %q, want original request %q", accepted.Execution.SubmissionID, submissionID)
		}
		records, err := store.ReadAll(accepted.Chat.ID)
		if err != nil {
			t.Fatal(err)
		}
		adapter := eventstream.NewAdapter()
		foundUser, foundAssistant := false, false
		for position, record := range records {
			event, err := adapter.Apply(record)
			if err != nil {
				t.Fatal(err)
			}
			if record.MessageID == accepted.Execution.UserMessageID {
				foundUser = true
				var stored map[string]any
				if err := json.Unmarshal(record.Data, &stored); err != nil {
					t.Fatal(err)
				}
				if got, _ := stored["submissionId"].(string); got != submissionID {
					t.Fatalf("user fact lost request identity before execution creation: got=%q want=%q", got, submissionID)
				}
				if _, present := stored["submissionId"]; submissionID == "" && present {
					t.Fatal("programmatic input acquired a synthesized submission identity")
				}
				if record.ExecutionID != "" || position+1 >= len(records) || records[position+1].Type != constants.ChatEventTypeExecutionCreated {
					t.Fatal("user identity must be available independently before execution.created")
				}
			}
			if event.Message == nil || (event.Message.ID != accepted.Execution.UserMessageID && event.Message.ID != accepted.Execution.ID) {
				continue
			}
			payload, err := json.Marshal(event.Message)
			if err != nil {
				t.Fatal(err)
			}
			var public map[string]any
			if err := json.Unmarshal(payload, &public); err != nil {
				t.Fatal(err)
			}
			if got, _ := public["submissionId"].(string); got != submissionID {
				t.Fatalf("public message %s identity=%q want=%q", event.Message.ID, got, submissionID)
			}
			if _, present := public["submissionId"]; submissionID == "" && present {
				t.Fatal("public projection synthesized an absent submission identity")
			}
			if event.Message.ID == accepted.Execution.ID {
				foundAssistant = true
			}
			// HTTP 历史与 WS 在同一权威游标使用同一个安全消息投影。
			history, err := messageview.ReplayPublicMessages(records[:position+1])
			if err != nil {
				t.Fatal(err)
			}
			matched := false
			for _, message := range history {
				if message.ID == event.Message.ID {
					matched = reflect.DeepEqual(message, *event.Message)
				}
			}
			if !matched {
				t.Fatal("history and public event projected different message facts")
			}
			if record.Type == constants.ChatEventTypeExecutionStarted {
				live, err := eventstream.NewLiveAdapter(records[:position+1], accepted.Execution.ID)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := live.Snapshot()
				if err != nil || snapshot.Message == nil || snapshot.Message.SubmissionID != submissionID {
					t.Fatalf("live snapshot lost the same input identity: %+v error=%v", snapshot.Message, err)
				}
			}
		}
		if !foundUser || !foundAssistant {
			t.Fatal("missing user or assistant projection")
		}
		intent.Chat = ChatTarget{ChatID: accepted.Chat.ID}
	}
}

func TestContinuationSubmissionReceiptSurvivesJournalReloadAndRejectsChangedInput(t *testing.T) {
	runtime := &fakeRuntime{}
	manager := newMessageTestService(t, t.TempDir(), runtime)
	ctx := context.Background()
	first, err := manager.execution.Start(ctx, WorkIntent{ClientRequestID: "first", ExecutionID: util.New(), Chat: ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}}, Input: WorkInput{Actor: "user", Content: "开始"}, Options: WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	intent := WorkIntent{ExecutionID: util.New(), Chat: ChatTarget{ChatID: first.Chat.ID}, Input: WorkInput{Actor: "user", SubmissionID: "platform-message", Content: "整理结果"}, Options: WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyContinue}, Admission: constants.ExecutionAdmissionCommitBeforeDispatch}
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
		Options:   WorkOptions{RuntimeID: runtimeconfig.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
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
