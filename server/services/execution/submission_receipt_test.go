package execution

import (
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	agentcap "github.com/JieWaZi/NeuxBot/server/internal/agent/capability"

	"context"
	"encoding/json"
	"errors"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"os"
	"path/filepath"
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

func TestSubmissionIdentityProjectsBeforeExecutionAndReceipt(t *testing.T) {
	root := t.TempDir()
	manager := newMessageTestService(t, root, &fakeRuntime{})
	intent := WorkIntent{
		ClientRequestID: "first-input",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:           WorkInput{Actor: "user", Content: "相同正文"},
		Options:         WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
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
				snapshot, _, err := live.Apply("input-scope", agentapi.Event{Type: agentapi.EventTypeTextDelta, Data: json.RawMessage(`{"text":"new"}`)})
				if err != nil || snapshot.Message != nil || snapshot.Live == nil || snapshot.Live.MessageID != accepted.Execution.ID {
					t.Fatalf("live increment lost the same message identity: %+v error=%v", snapshot.Live, err)
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
	if len(runtime.requests) != 2 {
		t.Fatal("replay started duplicate Runtime")
	}
	intent.Input.Content = "changed"
	if _, err := manager.execution.Start(ctx, intent); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changed content reused submission identity: %v", err)
	}
}

func TestContextInstructionsStayOutOfUserMessageAndTitle(t *testing.T) {
	for scenario, business := range map[string]string{"user": "你是谁", "workflow": "核验原始资料", "void-worker": "保存业务文件"} {
		t.Run(scenario, func(t *testing.T) {
			actor := "workflow"
			if scenario == "user" {
				actor = "user"
			}
			runtime := &fakeRuntime{}
			root := t.TempDir()
			manager := newMessageTestService(t, root, runtime)
			// 使用真实能力准备链路，只有最终原生消费者由同步 Runtime 替代。
			preparation := agentPreparationFixture(t)
			core := manager.execution.(*service)
			core.agent, core.agentEndpoint, core.agentBinary = preparation.agent, preparation.agentEndpoint, preparation.agentBinary
			core.agentSessions = preparation.agentSessions
			instruction := workinput.Instruction{Name: "DELIVERY.md", Content: "结果自动发送到已批准的接收方。"}
			var capabilities []ToolCapability
			workingDirectory := t.TempDir()
			if scenario == "void-worker" {
				instruction = workinput.Instruction{Name: constants.WorkflowWorkspaceInstructionFileName, Content: "本次业务成果放运行目录。"}
			}
			if scenario == "workflow" {
				// 两种合法来源都超过默认项目文档预算，结果契约不能依赖静态文件的尾部。
				if err := manager.profiles.UpdateWakerDocument(context.Background(), "waker-1", constants.ProfileDocumentKindIdentity, strings.Repeat("profile context\n", 4096), nowMillis()); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(workingDirectory, "AGENTS.md"), []byte(strings.Repeat("project context\n", 4096)), 0o600); err != nil {
					t.Fatal(err)
				}
				instruction = workinput.Instruction{Name: constants.WorkflowResultsInstructionFileName, Content: `Read workflow.results.schema, then invoke workflow.results.submit with a stable requestId. Submit the actual JSON value matching {"type":"object","properties":{"verified":{"type":"boolean"}},"required":["verified"]}. Final chat text is not a result submission.`}
				capabilities = []ToolCapability{{ID: "workflow.result", Name: "工作流结果", Bindings: []agentcap.Binding{agentPreparationBinding(t, "workflow.results.schema"), agentPreparationBinding(t, "workflow.results.submit")}}}
			}
			accepted, err := manager.execution.Start(context.Background(), WorkIntent{
				ClientRequestID: "context-only", ExecutionID: util.New(),
				Chat:      ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: workingDirectory}},
				Input:     WorkInput{Actor: actor, Content: business, ContextInstructions: []workinput.Instruction{instruction}},
				Options:   WorkOptions{RuntimeID: agentapi.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto, SessionPolicy: constants.RuntimeSessionPolicyNew, ToolCapabilities: capabilities},
				Admission: constants.ExecutionAdmissionCommitBeforeDispatch,
			})
			if err != nil {
				t.Fatal(err)
			}
			waitForChatTerminal(t, manager, accepted.Chat.ID)
			if accepted.Chat.Title != business || len(runtime.requests) != 1 {
				t.Fatalf("执行说明污染标题或未到达模型: %+v %+v", accepted.Chat, runtime.requests)
			}
			request := runtime.requests[0]
			if !strings.HasPrefix(request.Message.Text, business+"\n\n") || !strings.Contains(request.Message.Text, "<neuxbot-execution-context>") {
				t.Fatalf("真实能力准备丢失业务输入或本轮执行上下文: %s", request.Message.Text)
			}
			found := false
			staticBytes := 0
			for _, actual := range request.Instructions {
				staticBytes += len(actual.Content)
				if actual.Name == instruction.Name && actual.Content == instruction.Content {
					found = true
				}
			}
			if scenario == "workflow" {
				if !strings.Contains(request.Message.Text, instruction.Content) {
					t.Fatal("结果契约与完整 Schema 仅存在于可能截断的静态文件，未进入本轮模型输入")
				}
				if found || strings.Count(request.Message.Text, instruction.Content) != 1 || staticBytes <= 32<<10 {
					t.Fatalf("结果契约未从超预算静态指令中完整移入唯一动态输入: found=%v staticBytes=%d", found, staticBytes)
				}
			} else if !found || strings.Contains(request.Message.Text, "<workflow-results-context>") {
				t.Fatal("普通聊天或无结构化结果的 Worker 上下文发生变化")
			}
			store, err := eventlog.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			records, err := store.ReadAll(accepted.Chat.ID)
			if err != nil {
				t.Fatal(err)
			}
			found = false
			for _, record := range records {
				if record.Type != constants.ChatEventTypeUserMessageCreated || record.MessageID != accepted.Execution.UserMessageID {
					continue
				}
				var data eventlog.MessageCreatedData
				if err := json.Unmarshal(record.Data, &data); err != nil {
					t.Fatal(err)
				}
				if data.Content != business || data.Actor != actor {
					t.Fatalf("宿主上下文进入权威用户消息: %+v", data)
				}
				found = true
			}
			if !found {
				t.Fatal("缺少权威用户消息")
			}
			messages, err := messageview.ReplayPublicMessages(records)
			if err != nil {
				t.Fatal(err)
			}
			found = false
			for _, message := range messages {
				if message.ID == accepted.Execution.UserMessageID {
					if message.Content != business {
						t.Fatalf("宿主上下文进入公开聊天消息: %+v", message)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("缺少公开用户消息")
			}
		})
	}
}
