package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	agentapi "github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/requestidempotency"
	channeltool "github.com/JieWaZi/NeuxBot/server/internal/teamcapability"
	"github.com/JieWaZi/NeuxBot/server/internal/uploads"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestStartSettlesUnstartableLeadInputAndDispatchesNext(t *testing.T) {
	for _, cause := range []status_error.StatusError{status_error.RuntimeUnavailable, status_error.MessageAttachmentInvalid} {
		t.Run(cause.Key(), func(t *testing.T) {
			ctx := t.Context()
			sut, thread, _, captured := newDelegationDispatchService(t)
			sut.Close()
			for index, id := range []string{"broken-input", "next-input"} {
				input := execution.WorkInput{Actor: "user", SubmissionID: id, Content: id}
				identity, err := requestidempotency.New(id, []any{input, false})
				if err != nil {
					t.Fatal(err)
				}
				if err := sut.threads.CreateLeadInput(ctx, models.ChannelLeadInput{
					ID: LeadInputID(thread.ID, id), ThreadID: thread.ID,
					Payload: input, RequestHash: identity.Hash,
					Status: constants.ChannelInputStatusPending, CreatedAt: int64(index + 1),
				}); err != nil {
					t.Fatal(err)
				}
			}
			stub := &leadInputRejectionExecution{postTurnExecutionFake: captured, cause: cause}
			sut.executions = stub
			if err := sut.Start(ctx); err != nil {
				t.Fatalf("负责人输入拒绝导致团队服务启动失败: %v", err)
			}
			if err := sut.ReconcileThread(ctx, thread.ID); err != nil {
				t.Fatal(err)
			}
			broken, found, err := sut.threads.GetLeadInput(ctx, LeadInputID(thread.ID, "broken-input"))
			if err != nil || !found || broken.Status != constants.ChannelInputStatusFailed || broken.ErrorCode != cause.Key() || broken.LeadExecutionID != "" {
				t.Fatalf("未接纳输入没有独立失败终态: %+v found=%t error=%v", broken, found, err)
			}
			next, found, err := sut.threads.GetLeadInput(ctx, LeadInputID(thread.ID, "next-input"))
			if err != nil || !found || next.LeadExecutionID == "" || captured.startCount() != 1 {
				t.Fatalf("后续输入未派发一次: %+v found=%t starts=%d error=%v", next, found, captured.startCount(), err)
			}
			replay := SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{SubmissionID: "broken-input", Content: "broken-input"}}
			if _, err := sut.sendMessage(ctx, replay); !errors.Is(err, cause) {
				t.Fatalf("同键重试未返回原失败: %v", err)
			}
			if captured.startCount() != 1 {
				t.Fatal("重试派发了额外执行")
			}
			events, err := sut.threads.ListEvents(ctx, thread.ID, "", 10)
			if err != nil || len(events) != 1 || events[0].Type != constants.ChannelEventLeadInputFailed {
				t.Fatalf("队列失败未产生唯一的持久事件: %+v %v", events, err)
			}
			var payload map[string]any
			if err := json.Unmarshal(events[0].Payload, &payload); err != nil || payload["reportedBySend"] != false {
				t.Fatalf("异步失败应通过团队事件报告: %s %v", events[0].Payload, err)
			}
		})
	}
}

func TestLeadInputRejectionReturnedBySendSkipsDuplicateNotice(t *testing.T) {
	ctx := t.Context()
	sut, thread, _, captured := newDelegationDispatchService(t)
	sut.executions = &leadInputRejectionExecution{postTurnExecutionFake: captured, cause: status_error.MessageAttachmentInvalid}
	_, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{
		Actor: "user", SubmissionID: "broken-input", Content: "broken input",
	}})
	if !errors.Is(err, status_error.MessageAttachmentInvalid) {
		t.Fatalf("发送未返回即时拒绝: %v", err)
	}
	events, err := sut.threads.ListEvents(ctx, thread.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type != constants.ChannelEventLeadInputFailed {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil || payload["reportedBySend"] != true {
			t.Fatalf("发送响应已承载失败，持久事件应避免重复提示: %s %v", event.Payload, err)
		}
		return
	}
	t.Fatal("缺少原输入的持久失败事件")
}

func TestLeadInputWaitsForTemporaryNativeAdmissionContention(t *testing.T) {
	ctx := t.Context()
	sut, thread, _, captured := newDelegationDispatchService(t)
	sut.Close()
	sut.executions = &leadInputRejectionExecution{postTurnExecutionFake: captured, cause: agentapi.ErrTurnAdmissionBusy}
	result, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{
		Actor: "user", SubmissionID: "broken-input", Content: "waiting for the runtime",
	}})
	if err != nil || result.QueuedInput == nil || result.QueuedInput.Status != constants.ChannelInputStatusPending {
		t.Fatalf("临时占用应保留同一输入等待派发: %+v %v", result, err)
	}
	sut.executions = captured
	if err := sut.Start(ctx); err != nil {
		t.Fatal(err)
	}
	queued, found, err := sut.threads.GetLeadInput(ctx, result.QueuedInput.ID)
	if err != nil || !found || queued.LeadExecutionID == "" || queued.ErrorCode != "" || captured.startCount() != 1 {
		t.Fatalf("占用解除后没有派发原输入: %+v found=%t starts=%d error=%v", queued, found, captured.startCount(), err)
	}
}

func TestLeadWorkflowQueueDispatchesTheOriginallySelectedRevision(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.Close()
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID, ActiveExecutionID: "busy"}}
	currentRevision := "original"
	sut.freezeWorkflowSelection = func(_ context.Context, selected *workinput.WorkflowReference) (*workinput.WorkflowReference, error) {
		return &workinput.WorkflowReference{ID: selected.ID, RevisionID: currentRevision, Name: "Flow"}, nil
	}
	request := SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{SubmissionID: "same-input", Workflow: workinput.SelectWorkflow("flow")}}
	initial, err := sut.sendMessage(ctx, request)
	if err != nil || initial.QueuedInput == nil || initial.Execution != nil {
		t.Fatalf("busy lead should accept the workflow into its queue: %+v %v", initial, err)
	}
	currentRevision = "newer"
	replayed, err := sut.sendMessage(ctx, request)
	if err != nil || replayed.QueuedInput == nil || replayed.QueuedInput.ID != initial.QueuedInput.ID {
		t.Fatalf("new revision changed the original queue receipt: %+v %v", replayed, err)
	}
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID}}
	if err := sut.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if captured.startCount() != 1 {
		t.Fatalf("dispatched %d executions", captured.startCount())
	}
	actual := captured.firstStart()
	if actual.Chat.ChatID != thread.LeadChatID || actual.Input.Workflow == nil || actual.Input.Workflow.RevisionID != "original" || actual.Input.Content != "" {
		t.Fatalf("queue changed origin, selection or user text: %+v", actual)
	}
}

func TestTeamReplyKeepsLeadQueueWhenAMemberIsMentioned(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.Close()
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID, ActiveExecutionID: "busy"}}
	sut.chats = &testTeamReplyChat{Service: chats}
	work := execution.WorkInput{SubmissionID: "member-report-reply", Content: "请核对这份报告", ReplyToMessageID: "committed-report"}
	request := SendMessageInput{ThreadID: thread.ID, MentionWakerID: "member", Input: work}
	queued, err := sut.sendMessage(ctx, request)
	if err != nil || queued.QueuedInput == nil || queued.Task != nil || queued.Execution != nil {
		t.Fatalf("引用在负责人忙碌时没有进入主队列: %+v %v", queued, err)
	}
	replayed, err := sut.sendMessage(ctx, request)
	if err != nil || replayed.QueuedInput == nil || replayed.QueuedInput.ID != queued.QueuedInput.ID {
		t.Fatalf("同键续发没有保留队列身份: %+v %v", replayed, err)
	}
	stored, found, err := sut.threads.GetLeadInput(ctx, queued.QueuedInput.ID)
	if err != nil || !found || stored.Payload.ReplyToMessageID != "committed-report" || stored.DirectExecution {
		t.Fatalf("队列没有保留原报告和负责人协调模式: %+v %v", stored, err)
	}
	request.Input.ReplyToMessageID = "foreign-report"
	if _, err := sut.sendMessage(ctx, request); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("改变引用沿用了另一个已接纳输入: %v", err)
	}
	request.Input.SubmissionID = "foreign-reply"
	if _, err := sut.sendMessage(ctx, request); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("主对话队列接纳了范围以外的报告: %v", err)
	}
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID}}
	if err := sut.Start(ctx); err != nil {
		t.Fatal(err)
	}
	actual := captured.firstStart()
	if captured.startCount() != 1 || actual.Chat.ChatID != thread.LeadChatID || actual.Input.Content != work.Content ||
		actual.TeamReplyThreadID != thread.ID || actual.Input.ReplyToMessageID != work.ReplyToMessageID {
		t.Fatalf("恢复后没有向负责人派发原输入: %+v starts=%d", actual, captured.startCount())
	}
}

func TestQueuedTeamReplyRetainsUploadedBytesAcrossRecovery(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.Close()
	sut.chats = &testTeamReplyChat{Service: chats}
	sut.managedFilesDirectory = t.TempDir()
	layout, err := chatstorage.NewLayout(sut.managedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := layout.TeamRoot(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureThreadExecutionWorkspace(directory, thread.ID, thread.LeadWakerID); err != nil {
		t.Fatal(err)
	}
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID, ActiveExecutionID: "busy"}}
	store, err := uploads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	const subject, name = "web:test", "证据.txt"
	contents := []string{"第一份上传内容", "第二份上传内容"}
	var inputs []execution.AttachmentInput
	for _, content := range contents {
		ref, err := store.Stage(ctx, subject, name, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, execution.AttachmentInput{SourcePath: ref})
	}
	request := SendMessageInput{
		ThreadID: thread.ID,
		Input: execution.WorkInput{
			SubmissionID: "queued-reply-upload", Content: "请结合附件核对", ReplyToMessageID: "committed-report",
			Attachments: inputs,
		},
		ClaimAttachments: func(input *execution.WorkInput) (func(bool), error) {
			refs := make([]string, 0, len(input.Attachments))
			for _, attachment := range input.Attachments {
				refs = append(refs, attachment.SourcePath)
			}
			paths, release, err := store.Claim(subject, refs, input.SubmissionID)
			if err == nil {
				for index, path := range paths {
					input.Attachments[index].SourcePath = path
				}
			}
			return release, err
		},
	}
	interrupted := request
	interrupted.AdmissionBoundary = func(context.Context, func(context.Context) error) error {
		return status_error.OperationCanceled
	}
	if _, err := sut.sendMessage(ctx, interrupted); !errors.Is(err, status_error.OperationCanceled) {
		t.Fatalf("接纳中断 = %v", err)
	}
	queueDirectory := filepath.Join(directory, constants.ChannelLeadInputAttachmentsDirectory, LeadInputID(thread.ID, request.Input.SubmissionID))
	if _, err := os.Stat(queueDirectory); !os.IsNotExist(err) {
		t.Fatalf("接纳失败后仍留有队列附件 = %v", err)
	}
	queued, err := sut.sendMessage(ctx, request)
	if err != nil || queued.QueuedInput == nil || queued.Execution != nil {
		t.Fatalf("负责人忙碌时的队列接纳 = %+v, %v", queued, err)
	}
	store.Close()
	replayed, err := sut.sendMessage(ctx, request)
	if err != nil || replayed.QueuedInput == nil || replayed.QueuedInput.ID != queued.QueuedInput.ID {
		t.Fatalf("上传租约消失后的同键重放 = %+v, %v", replayed, err)
	}
	sut.executions = &uploadedReplyExecution{postTurnExecutionFake: captured, check: func(intent execution.WorkIntent) {
		attachments := intent.Input.Attachments
		if len(attachments) != len(contents) {
			t.Fatalf("恢复派发附件 = %+v", attachments)
		}
		for index, attachment := range attachments {
			if filepath.Base(attachment.SourcePath) != name {
				t.Fatalf("恢复派发时原文件名改变 = %+v", attachment)
			}
			if data, err := os.ReadFile(attachment.SourcePath); err != nil || string(data) != contents[index] {
				t.Fatalf("恢复派发附件内容 = %q, %v", data, err)
			}
		}
	}}
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID}}
	if err := sut.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if captured.startCount() != 1 {
		t.Fatalf("恢复派发次数 = %d", captured.startCount())
	}
	if _, err := os.Stat(queueDirectory); !os.IsNotExist(err) {
		t.Fatalf("Chat 接纳后仍留有队列附件 = %v", err)
	}
}

func TestQueuedMemberInputRetainsUploadedBytes(t *testing.T) {
	for _, followUp := range []bool{false, true} {
		label := "new task"
		if followUp {
			label = "follow-up"
		}
		t.Run(label, func(t *testing.T) {
			ctx := t.Context()
			sut, thread, chats, captured := newDelegationDispatchService(t)
			sut.Close()
			sut.managedFilesDirectory = t.TempDir()
			layout, err := chatstorage.NewLayout(sut.managedFilesDirectory)
			if err != nil {
				t.Fatal(err)
			}
			directory, err := layout.TeamRoot(thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := workspace.EnsureThreadExecutionWorkspace(directory, thread.ID, thread.LeadWakerID); err != nil {
				t.Fatal(err)
			}
			const memberChatID = "member-chat"
			var attempt models.ChannelTaskAttempt
			if followUp {
				initial, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, MentionWakerID: "member",
					Input: execution.WorkInput{Content: "initial task"}})
				if err != nil || initial.Task == nil || initial.Task.Status != constants.ChannelTaskStatusInProgress {
					t.Fatalf("initial member task = %+v, %v", initial, err)
				}
				attempt, err = sut.tasks.GetCurrentAttempt(ctx, initial.Task.ID, initial.Task.CurrentFence)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				chats.summaries[memberChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: memberChatID, ActiveExecutionID: "occupied"}}
			}
			store, err := uploads.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(store.Close)
			const content, subject, name = "member attachment bytes", "web:test", "成员附件.txt"
			ref, err := store.Stage(ctx, subject, name, strings.NewReader(content))
			if err != nil {
				t.Fatal(err)
			}
			queued, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, MentionWakerID: "member",
				Input: execution.WorkInput{SubmissionID: "upload-member-input", Content: "请读取附件", Attachments: []execution.AttachmentInput{{SourcePath: ref}}},
				ClaimAttachments: func(input *execution.WorkInput) (func(bool), error) {
					paths, release, err := store.Claim(subject, []string{input.Attachments[0].SourcePath}, input.SubmissionID)
					if err == nil {
						input.Attachments[0].SourcePath = paths[0]
					}
					return release, err
				}})
			if err != nil || queued.Task == nil {
				t.Fatalf("queued member input = %+v, %v", queued, err)
			}
			store.Close()
			var memberAttachment string
			sut.executions = &uploadedReplyExecution{postTurnExecutionFake: captured, check: func(intent execution.WorkIntent) {
				if intent.Chat.ChatID != memberChatID {
					return
				}
				if len(intent.Input.Attachments) != 1 {
					t.Fatalf("member attachment dispatch = %+v", intent.Input.Attachments)
				}
				memberAttachment = intent.Input.Attachments[0].SourcePath
				if data, err := os.ReadFile(memberAttachment); err != nil || string(data) != content || filepath.Base(memberAttachment) != name {
					t.Fatalf("member attachment dispatch = %q, %v", data, err)
				}
			}}
			if followUp {
				if err := sut.Start(ctx); err != nil {
					t.Fatal(err)
				}
				if err := sut.handleAttemptTerminal(attempt, messageview.Message{Status: constants.ExecutionStatusCompleted}); err != nil {
					t.Fatal(err)
				}
				if memberAttachment == "" {
					t.Fatal("member follow-up was not dispatched")
				}
				if _, err := os.Stat(memberAttachment); !os.IsNotExist(err) {
					t.Fatalf("follow-up still owns the copy after Chat admission: %v", err)
				}
				return
			}
			chats.summaries[memberChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: memberChatID}}
			if err := sut.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if memberAttachment == "" {
				t.Fatal("queued member task was not recovered")
			}
			current, err := sut.tasks.Get(ctx, queued.Task.ID)
			if err != nil || current.Status != constants.ChannelTaskStatusInProgress {
				t.Fatalf("dispatched task = %+v, %v", current, err)
			}
			attempt, err = sut.tasks.GetCurrentAttempt(ctx, current.ID, current.CurrentFence)
			if err != nil {
				t.Fatal(err)
			}
			if err := sut.handleAttemptTerminal(attempt, messageview.Message{Status: constants.ExecutionStatusFailed}); err != nil {
				t.Fatal(err)
			}
			memberAttachment = ""
			if err := sut.retryTask(ctx, current.ID, "", ""); err != nil {
				t.Fatal(err)
			}
			if memberAttachment == "" {
				t.Fatal("member retry did not dispatch the original uploaded file")
			}
			current, err = sut.tasks.Get(ctx, current.ID)
			if err != nil {
				t.Fatal(err)
			}
			attempt, err = sut.tasks.GetCurrentAttempt(ctx, current.ID, current.CurrentFence)
			if err != nil {
				t.Fatal(err)
			}
			if err := sut.handleAttemptTerminal(attempt, messageview.Message{Status: constants.ExecutionStatusCompleted}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(memberAttachment); !os.IsNotExist(err) {
				t.Fatalf("completed task still owns its upload copy: %v", err)
			}
		})
	}
}

func TestTeamStartupReclaimsUnownedAndSettledQueueAttachments(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, _ := newDelegationDispatchService(t)
	sut.Close()
	sut.managedFilesDirectory = t.TempDir()
	layout, err := chatstorage.NewLayout(sut.managedFilesDirectory)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := layout.TeamRoot(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureThreadExecutionWorkspace(directory, thread.ID, thread.LeadWakerID); err != nil {
		t.Fatal(err)
	}
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID, ActiveExecutionID: "busy"}}
	file := filepath.Join(t.TempDir(), "queued.txt")
	if err := os.WriteFile(file, []byte("queue attachment"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged := map[constants.ChannelInputStatus]string{}
	for _, status := range []constants.ChannelInputStatus{"", constants.ChannelInputStatusFailed, constants.ChannelInputStatusCancelled,
		constants.ChannelInputStatusDelivering, constants.ChannelInputStatusConsumed, constants.ChannelInputStatusPending} {
		inputID := LeadInputID(thread.ID, "stage-"+string(status))
		prepared, err := workspace.StageQueuedAttachments(directory, thread.LeadChatID, inputID, []workspace.AttachmentInput{{SourcePath: file}})
		if err != nil {
			t.Fatal(err)
		}
		staged[status] = prepared.RuntimePaths[0]
		if status != "" {
			if err := sut.threads.CreateLeadInput(ctx, models.ChannelLeadInput{ID: inputID, ThreadID: thread.ID, Status: status,
				Payload: execution.WorkInput{Actor: "user", Content: "queued input"}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	queueDirectory := filepath.Join(directory, constants.ChannelLeadInputAttachmentsDirectory)
	incomplete, err := os.MkdirTemp(queueDirectory, "attachment-staging-"+LeadInputID(thread.ID, "incomplete")+"-")
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(queueDirectory, LeadInputID(thread.ID, "unmanaged"))
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := sut.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(incomplete); !os.IsNotExist(err) {
		t.Errorf("未发布的临时目录仍然存在 = %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("无归属的目录被移除 = %v", err)
	}
	for status, path := range staged {
		if _, err := os.Stat(path); status == constants.ChannelInputStatusPending && err != nil || status != constants.ChannelInputStatusPending && !os.IsNotExist(err) {
			t.Errorf("恢复后的 %q 附件文件状态 = %v", status, err)
		}
	}
}

type uploadedReplyExecution struct {
	// postTurnExecutionFake 提供团队派发和终态测试端口。
	*postTurnExecutionFake
	// check 在 Chat 接纳前核对队列交付的文件原件。
	check func(execution.WorkIntent)
}

// Start 在模拟 Chat 接纳前读取附件，覆盖副本回收与接纳的顺序。
func (f *uploadedReplyExecution) Start(ctx context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	f.check(intent)
	return f.postTurnExecutionFake.Start(ctx, intent)
}

func TestTeamCodeReferencePromptKeepsItsSourceThroughTheLeadQueue(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.Close()
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID, ActiveExecutionID: "busy"}}
	request := SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{
		SubmissionID: "lead-code-reference-prompt", Content: "请查看引用的代码片段。", CodeReferenceOnly: true,
		CodeReferences: []execution.CodeReferenceInput{{Path: "/work/example.go"}},
	}}
	queued, err := sut.sendMessage(ctx, request)
	if err != nil || queued.QueuedInput == nil || queued.Execution != nil {
		t.Fatalf("代码引用输入没有进入负责人队列: %+v %v", queued, err)
	}
	stored, found, err := sut.threads.GetLeadInput(ctx, queued.QueuedInput.ID)
	if err != nil || !found || !stored.Payload.CodeReferenceOnly {
		t.Fatalf("负责人队列丢失正文来源: %+v %v", stored, err)
	}
	request.Input.CodeReferenceOnly = false
	if _, err := sut.sendMessage(ctx, request); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("改变正文来源沿用了另一个已排队输入: %v", err)
	}
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID}}
	if err := sut.Start(ctx); err != nil {
		t.Fatal(err)
	}
	actual := captured.firstStart()
	if !actual.Input.CodeReferenceOnly || actual.Input.Content != request.Input.Content || len(actual.Input.CodeReferences) != 1 {
		t.Fatalf("恢复负责人输入时丢失代码引用来源或正文: %+v", actual.Input)
	}
}

type testTeamReplyChat struct {
	// Service 提供引用查询之外的团队聊天端口。
	chatservice.Service
}

// ReadTeamReplyTarget 验证团队提交只查询主 Chat 中的原报告。
func (*testTeamReplyChat) ReadTeamReplyTarget(_ context.Context, threadID, chatID, messageID string) (chatservice.ReplyTarget, error) {
	if threadID != "thread" || chatID != "lead-chat" || messageID != "committed-report" {
		return chatservice.ReplyTarget{}, status_error.ChatReplyTargetUnavailable
	}
	return chatservice.ReplyTarget{}, nil
}

func TestWorkflowOnlyTeamFirstSendUsesTheSelectedName(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.chatGroups = &leaderTurnChatGroupFake{chats: chats}
	sut.freezeWorkflowSelection = func(_ context.Context, selected *workinput.WorkflowReference) (*workinput.WorkflowReference, error) {
		return &workinput.WorkflowReference{ID: selected.ID, RevisionID: "published", Name: "独立检查流程"}, nil
	}
	result, err := sut.createThread(ctx, CreateThreadInput{ChannelID: thread.ChannelID, ClientRequestID: "new-workflow-thread", Input: execution.WorkInput{Workflow: workinput.SelectWorkflow("flow")}})
	if err != nil || result.Thread.Title != "独立检查流程" || result.LeadExecution == nil || len(result.Tasks) != 0 {
		t.Fatalf("workflow-only first send did not establish the named lead conversation: %+v %v", result, err)
	}
	actual := captured.firstStart()
	if actual.Chat.ChatID != result.Thread.LeadChatID || actual.Input.Workflow == nil || actual.Input.Workflow.RevisionID != "published" || actual.Input.Content != "" {
		t.Fatalf("lead first turn did not preserve the original workflow-only input: %+v", actual)
	}
	if instructions := actual.Input.ContextInstructions; len(instructions) == 0 || !strings.Contains(instructions[len(instructions)-1].Content, "This is a direct-execution turn") {
		t.Fatalf("selected workflow lost its direct-execution instruction: %+v", instructions)
	}
}

// directedPluginReferenceFixture 返回被点名成员的固定插件展示版本。
type directedPluginReferenceFixture struct {
	// PluginController 保留本用例不使用的插件端口。
	controllers.PluginController
	// memberID 是允许读取安装引用的唯一成员。
	memberID string
}

func (f directedPluginReferenceFixture) InstalledReferences(_ context.Context, wakerID string) ([]models.PluginReference, error) {
	if wakerID != f.memberID {
		return nil, status_error.PluginNotReady
	}
	return []models.PluginReference{{ID: "member-plugin", Name: "Member plugin", VersionID: "member-plugin-v1", Version: "1.0.0"}}, nil
}

func TestDirectedFirstTeamInputKeepsMemberPluginAsMessageReference(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.chatGroups = &leaderTurnChatGroupFake{chats: chats}
	members, err := sut.threads.ListMembers(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberID := ""
	for _, member := range members {
		if !member.IsLead {
			memberID = member.WakerID
			break
		}
	}
	sut.plugins = directedPluginReferenceFixture{memberID: memberID}
	result, err := sut.createThread(ctx, CreateThreadInput{
		ChannelID: thread.ChannelID, ClientRequestID: "member-plugin-first", MentionWakerID: memberID,
		Input: execution.WorkInput{Content: "Prepare the report", PluginIDs: []string{"member-plugin"}},
	})
	if err != nil || len(result.Tasks) != 1 {
		t.Fatalf("member plugin first send was not accepted: %+v %v", result, err)
	}
	actual := captured.firstStart()
	if len(actual.Input.PluginIDs) != 0 || len(actual.MessagePlugins) != 1 || actual.MessagePlugins[0].VersionID != "member-plugin-v1" {
		t.Fatalf("lead lost the member reference or received its plugin capability: %+v", actual)
	}
	if input := result.Tasks[0].DirectInput; input == nil || len(input.PluginIDs) != 1 || input.PluginIDs[0] != "member-plugin" {
		t.Fatalf("member task lost its own plugin selection: %+v", input)
	}
}

func TestDirectedFirstTeamInputKeepsUploadedAttachmentForDeferredMember(t *testing.T) {
	ctx := t.Context()
	sut, existing, chats, captured := newDelegationDispatchService(t)
	sut.Close()
	sut.managedFilesDirectory = t.TempDir()
	sut.chatGroups = &leaderTurnChatGroupFake{chats: chats, managedFilesDirectory: sut.managedFilesDirectory, busyMember: "member"}
	store, err := uploads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	const subject, content = "web:test", "uploaded first message bytes"
	ref, err := store.Stage(ctx, subject, "first.txt", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	req := CreateThreadInput{ChannelID: existing.ChannelID, ClientRequestID: "first-upload", MentionWakerID: "member",
		Input: execution.WorkInput{SubmissionID: "first-upload", Content: "Read the file", Attachments: []execution.AttachmentInput{{SourcePath: ref}}},
		ClaimAttachments: func(input *execution.WorkInput) (func(bool), error) {
			paths, release, err := store.Claim(subject, []string{input.Attachments[0].SourcePath}, input.SubmissionID)
			if err == nil {
				input.Attachments[0].SourcePath = paths[0]
			}
			return release, err
		}}
	var delivered []string
	sut.executions = &uploadedReplyExecution{postTurnExecutionFake: captured, check: func(intent execution.WorkIntent) {
		if len(intent.Input.Attachments) == 0 {
			return
		}
		data, err := os.ReadFile(intent.Input.Attachments[0].SourcePath)
		if err != nil || string(data) != content {
			t.Fatalf("first message attachment for %s = %q, %v", intent.Chat.ChatID, data, err)
		}
		delivered = append(delivered, intent.Chat.ChatID)
	}}
	created, err := sut.createThread(ctx, req)
	if err != nil || len(created.Tasks) != 1 || created.Tasks[0].Status != constants.ChannelTaskStatusPending {
		t.Fatalf("first member task = %+v, %v", created, err)
	}
	store.Close()
	replay, err := sut.createThread(ctx, req)
	if err != nil || !replay.Replayed || replay.Thread.ID != created.Thread.ID {
		t.Fatalf("first upload replay after upload cleanup = %+v, %v", replay, err)
	}
	member, err := sut.threads.GetMember(ctx, created.Thread.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	chats.summaries[member.ChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: member.ChatID}}
	if _, err := sut.schedule(ctx, created.Thread.ID); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 || delivered[0] != created.Thread.LeadChatID || delivered[1] != member.ChatID {
		t.Fatalf("attachment dispatches = %v", delivered)
	}
}

func TestLeadInputSendDoesNotReturnAnEarlierSubmissionsExecution(t *testing.T) {
	ctx := t.Context()
	sut, thread, _, captured := newDelegationDispatchService(t)
	earlier, err := sut.enqueueLeadInput(ctx, thread, SendMessageInput{Input: execution.WorkInput{
		Actor: "user", SubmissionID: "earlier-input", Content: "earlier input",
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{
		Actor: "user", SubmissionID: "newer-input", Content: "newer input",
	}})
	if err != nil || result.QueuedInput == nil || result.QueuedInput.ID != LeadInputID(thread.ID, "newer-input") ||
		result.QueuedInput.Status != constants.ChannelInputStatusPending || result.Execution != nil {
		t.Fatalf("新输入提前取得了其他输入的执行收据: %+v %v", result, err)
	}
	previous, found, err := sut.threads.GetLeadInput(ctx, earlier.ID)
	if err != nil || !found || previous.LeadExecutionID == "" || captured.startCount() != 1 {
		t.Fatalf("队首先前的输入没有按顺序派发: %+v found=%t starts=%d error=%v", previous, found, captured.startCount(), err)
	}
}

type leadInputRejectionExecution struct {
	// postTurnExecutionFake 接纳其余队列输入并记录派发身份。
	*postTurnExecutionFake
	// cause 是第一条输入在接纳前的确定性拒绝。
	cause error
	// beforeRejection 可以让已提交的 Chat 收据先于错误返回可见。
	beforeRejection func()
}

// Start 让队首返回指定错误，并保留其余输入的正常接纳边界。
func (e *leadInputRejectionExecution) Start(ctx context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	if intent.Input.SubmissionID == "broken-input" {
		if e.beforeRejection != nil {
			e.beforeRejection()
		}
		return execution.AcceptedExecution{}, e.cause
	}
	return e.postTurnExecutionFake.Start(ctx, intent)
}

func TestLeadInputAdmissionReceiptWinsOverStartError(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	submissionID := "broken-input"
	inputID := LeadInputID(thread.ID, submissionID)
	executionID := (requestidempotency.Identity{ID: submissionID}).ResourceID("lead_execution", thread.ID)
	sut.executions = &leadInputRejectionExecution{
		postTurnExecutionFake: captured, cause: status_error.RuntimeUnavailable,
		beforeRejection: func() {
			chats.mutex.Lock()
			defer chats.mutex.Unlock()
			chats.executions = map[string]models.Execution{
				executionID: {ID: executionID, ChatID: thread.LeadChatID, SubmissionID: submissionID, Status: constants.ExecutionStatusRunning},
			}
		},
	}
	result, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "已接纳输入", SubmissionID: submissionID}})
	if err != nil || result.Execution == nil || result.Execution.ID != executionID {
		t.Fatalf("派发错误覆盖了原接纳收据: %+v %v", result, err)
	}
	queued, found, err := sut.threads.GetLeadInput(ctx, inputID)
	if err != nil || !found || queued.Status != constants.ChannelInputStatusDelivering || queued.LeadExecutionID != executionID || queued.ErrorCode != "" {
		t.Fatalf("原收据未进入正常执行结算: %+v found=%t error=%v", queued, found, err)
	}
}

func TestPendingLeadInputRecoversReceiptWithoutStartingAgain(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	submissionID := "already-accepted"
	executionID := (requestidempotency.Identity{ID: submissionID}).ResourceID("lead_execution", thread.ID)
	chats.executions = map[string]models.Execution{
		executionID: {ID: executionID, ChatID: thread.LeadChatID, SubmissionID: submissionID, Status: constants.ExecutionStatusRunning},
	}
	result, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "恢复原输入", SubmissionID: submissionID}})
	if err != nil || result.Execution == nil || result.Execution.ID != executionID || captured.startCount() != 0 {
		t.Fatalf("已接纳但缺关联的输入重复调用了派发：%+v starts=%d error=%v", result, captured.startCount(), err)
	}
}

func TestLeadExecutionSurvivesFailedInitialQueueHandoff(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.threads = &unwritableLeadInputHandoff{ChannelThreadController: sut.threads}
	submissionID := "committed-input"
	executionID := (requestidempotency.Identity{ID: submissionID}).ResourceID("lead_execution", thread.ID)
	chats.terminalEvents = map[string]chatservice.PublicEvent{
		thread.LeadChatID: {Message: &messageview.Message{ExecutionID: executionID, Role: constants.MessageRoleAssistant, Status: constants.ExecutionStatusCompleted}},
	}
	result, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "已接纳的原执行", SubmissionID: submissionID}})
	if err != nil || result.Execution == nil || result.Execution.ID != executionID {
		t.Fatalf("关联写入失败不应覆盖已接纳执行：%+v %v", result, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		queued, found, err := sut.threads.GetLeadInput(ctx, LeadInputID(thread.ID, submissionID))
		if err != nil || !found {
			t.Fatalf("读取已接纳输入失败: %+v %v", queued, err)
		}
		if queued.Status == constants.ChannelInputStatusConsumed {
			if queued.LeadExecutionID != executionID || captured.startCount() != 1 || captured.cancelCount() != 0 {
				t.Fatalf("原执行被取消或重做: %+v starts=%d cancels=%d", queued, captured.startCount(), captured.cancelCount())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("原执行 watcher 未从权威终态补齐队列关联")
}

type unwritableLeadInputHandoff struct {
	// ChannelThreadController 负责存储除初次关联写入以外的队列生命周期。
	controllers.ChannelThreadController
}

// UpdateLeadInputs 模拟执行接纳后初次关联写入遇到数据库故障。
func (c *unwritableLeadInputHandoff) UpdateLeadInputs(ctx context.Context, ids []string, status constants.ChannelInputStatus, executionID string, updatedAt int64) error {
	if status == constants.ChannelInputStatusDelivering {
		return status_error.DatabaseBusy
	}
	return c.ChannelThreadController.UpdateLeadInputs(ctx, ids, status, executionID, updatedAt)
}

func TestStartDefersLeadInputWhenSubmissionReceiptReadFails(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	sut.Close()
	queued, err := sut.enqueueLeadInput(ctx, thread, SendMessageInput{Input: execution.WorkInput{Actor: "user", Content: "读取暂时失败", SubmissionID: "broken-input"}})
	if err != nil {
		t.Fatal(err)
	}
	sut.chats = &unreadableLeadInputReceipt{Service: chats}
	sut.executions = &leadInputRejectionExecution{postTurnExecutionFake: captured, cause: status_error.RuntimeUnavailable}
	if err := sut.Start(ctx); err != nil {
		t.Fatalf("回执暂时无法读取导致团队启动失败: %v", err)
	}
	pending, found, err := sut.threads.GetLeadInput(ctx, queued.ID)
	if err != nil || !found || pending.Status != constants.ChannelInputStatusPending || pending.ErrorCode != "" {
		t.Fatalf("未知接纳结果没有交给队列继续恢复: %+v found=%t error=%v", pending, found, err)
	}
}

type unreadableLeadInputReceipt struct {
	// Service 提供回执读取以外的真实业务替身。
	chatservice.Service
}

// ReadSubmission 让接纳确认出现暂时的数据库故障。
func (*unreadableLeadInputReceipt) ReadSubmission(context.Context, string, string) (*models.Execution, error) {
	return nil, status_error.DatabaseBusy
}

func TestQueuedLeadInputDispatchesAfterTransientFailure(t *testing.T) {
	ctx := t.Context()
	sut, thread, _, captured := newDelegationDispatchService(t)
	captured.setStartError(status_error.DatabaseBusy)
	input := SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "处理新增要求", SubmissionID: "queued-input"}}
	if _, err := sut.sendMessage(ctx, input); !hasStatusErrorCode(err, status_error.DatabaseBusy.Code()) {
		t.Fatalf("首次派发没有遇到预期的暂时故障：%v", err)
	}
	inputID := LeadInputID(thread.ID, input.Input.SubmissionID)
	queued, exists, err := sut.threads.GetLeadInput(ctx, inputID)
	if err != nil || !exists || queued.Status != constants.ChannelInputStatusPending {
		t.Fatalf("派发失败后缺少已入队输入：%+v %v", queued, err)
	}
	captured.setStartError(nil)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, exists, err := sut.threads.GetLeadInput(ctx, inputID)
		if err != nil || !exists {
			t.Fatalf("无法读取待恢复输入：%+v %v", current, err)
		}
		if current.Status == constants.ChannelInputStatusDelivering {
			if captured.startCount() != 1 || captured.firstStart().Input.SubmissionID != input.Input.SubmissionID || current.LeadExecutionID != captured.firstStart().ExecutionID {
				t.Fatalf("后台派发改变原输入身份或重复派发：%+v starts=%d", current, captured.startCount())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("暂时故障消失后仍未派发已入队输入")
}

func TestFailedLeadTurnConsumesOriginalQueuedInput(t *testing.T) {
	ctx := t.Context()
	sut, thread, _, captured := newDelegationDispatchService(t)
	result, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, MentionWakerID: thread.LeadWakerID, Input: execution.WorkInput{Content: "直接执行"}})
	if err != nil || result.Execution == nil || result.QueuedInput == nil {
		t.Fatalf("负责人续发未接纳：%+v %v", result, err)
	}
	if captured.firstStart().Input.SubmissionID != result.QueuedInput.ID {
		t.Errorf("缺少输入标识的旧入口未使用队列身份：%+v", captured.firstStart().Input)
	}
	event := chatservice.PublicEvent{ExecutionID: result.Execution.ID, ExecutionStatus: constants.ExecutionStatusFailed}
	handled, err := sut.completeLeadExecution(ctx, thread, result.Execution.ID, nil, []string{result.QueuedInput.ID}, event)
	if err != nil || !handled {
		t.Fatalf("未处理已接纳输入的失败终态：%v", err)
	}
	finished, exists, err := sut.threads.GetLeadInput(ctx, result.QueuedInput.ID)
	if err != nil || !exists || finished.Status != constants.ChannelInputStatusConsumed || finished.LeadExecutionID != result.Execution.ID || captured.startCount() != 1 {
		t.Fatalf("已接纳的失败输入被重新派发：%+v starts=%d error=%v", finished, captured.startCount(), err)
	}
}

func TestQueuedLeadInputHasDrainOwnerWhileChatIsBusy(t *testing.T) {
	ctx := t.Context()
	sut, thread, chats, captured := newDelegationDispatchService(t)
	chats.mutex.Lock()
	chats.summaries[thread.LeadChatID] = chatservice.ChatSummary{Chat: models.Chat{ID: thread.LeadChatID, ActiveExecutionID: "desktop-turn"}}
	chats.mutex.Unlock()

	result, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "等负责人空闲后处理"}})
	if err != nil || result.Execution != nil || result.QueuedInput == nil || captured.startCount() != 0 {
		t.Fatalf("负责人忙碌时应只接纳队列输入: %+v %v", result, err)
	}
	sut.watchersMutex.Lock()
	_, registered := sut.watchers["recovery:"+thread.ID]
	sut.watchersMutex.Unlock()
	if !registered {
		t.Fatal("普通 Chat 回合进行中，团队已接纳队列但未登记派发监听")
	}
}

func TestRecoveryDrainExitsForDeletedThread(t *testing.T) {
	ctx := t.Context()
	sut, thread, _, _ := newDelegationDispatchService(t)
	if err := sut.threads.Delete(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	sut.watchRecoveryDrain(thread.ID)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sut.watchersMutex.Lock()
		_, registered := sut.watchers["recovery:"+thread.ID]
		sut.watchersMutex.Unlock()
		if !registered {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("已删除团队对话的队列恢复仍在运行")
}

func TestTeamAdmittedNativeDefaultsSurviveEnvironmentChanges(t *testing.T) {
	on, off := true, false
	for _, fast := range []*bool{nil, &on, &off} {
		for _, route := range []string{"lead", "member", "notification"} {
			t.Run(route, func(t *testing.T) {
				fixture := newDeletionFixture(t)
				ctx := t.Context()
				wakers := controllers.NewWakerController(fixture.db)
				createChannelTestWakers(t, ctx, wakers, "lead", "member")
				projects := controllers.NewProjectController(fixture.db)
				storage := t.TempDir()
				journal, err := chatservice.New(chatservice.Config{StorageDirectory: storage, Projects: projects,
					QueryIndexes: chatservice.QueryIndexDependencies{Chats: controllers.NewChatController(fixture.db), Transactions: fixture.service.transactions, DeletionIntents: controllers.NewChatDeletionIntentController(fixture.db)},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(journal.Close)
				environments := controllers.NewRuntimeEnvironmentController(fixture.db)
				environment := models.RuntimeEnvironment{DefaultFast: fast, ID: "environment", RuntimeID: "codex", BaseRuntimeID: "codex", Enabled: true, Detection: agentapi.DetectionResult{State: agentapi.DetectionStateReady,
					Models: []agentapi.ModelOption{{ID: "future-model", ReasoningOptions: []agentapi.ReasoningOption{{ID: "high"}}}},
				}}
				if err := environments.Ensure(ctx, environment); err != nil {
					t.Fatal(err)
				}
				// 只使用真实 Core 的配置解析入口，不创建原生连接或调用模型。
				resolver, err := execution.New(execution.Config{Journal: journal, Threads: &agentapi.ThreadLifecycle{}, ResolveAccount: func(_ context.Context, request agentapi.AccountRequest, _ string) (agentapi.AccountRequest, error) {
					return request, nil
				},
					Wakers: wakers, Profiles: controllers.NewProfileDocumentController(fixture.db), Projects: projects, RuntimeEnvironments: environments, StorageDirectory: storage,
					RuntimeSummaries: []agentapi.RuntimeSummary{{ID: "codex", BaseRuntimeID: "codex", InstructionFileName: "AGENTS.md", SkillDirectoryName: ".agents"}},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(resolver.Close)
				admitted, err := resolver.ResolveConfiguration(ctx, execution.ExecutionConfiguration{RuntimeID: "codex", PermissionLevel: constants.PermissionLevelDefault})
				if err != nil || admitted.Model != "" || admitted.Reasoning != "" {
					t.Fatalf("原配置不是合法原生默认: %+v %v", admitted, err)
				}
				if err := fixture.channels.Create(ctx, models.Channel{ID: "team", Name: "原生默认团队", LeadWakerID: "lead"}, []models.ChannelMember{{ChannelID: "team", WakerID: "lead"}, {ChannelID: "team", WakerID: "member"}}); err != nil {
					t.Fatal(err)
				}
				thread := models.ChannelThread{ID: "thread", ChannelID: "team", LeadWakerID: "lead", LeadChatID: "lead-chat", ReadyAt: testTimestamp(1), MaxParallel: 2, MaxTasks: 10}
				members := []models.ChannelThreadMember{{ThreadID: thread.ID, WakerID: "lead", ChatID: "lead-chat", IsLead: true}, {ThreadID: thread.ID, WakerID: "member", ChatID: "member-chat", Position: 1}}
				for index := range members {
					members[index].RuntimeID, members[index].AccountID, members[index].Model, members[index].Reasoning, members[index].PermissionLevel = admitted.RuntimeID, admitted.AccountID, admitted.Model, admitted.Reasoning, admitted.PermissionLevel
					members[index].Fast = admitted.Fast
				}
				if err := fixture.threads.Create(ctx, thread, members); err != nil {
					t.Fatal(err)
				}
				environment.DefaultModel, environment.DefaultReasoning = "future-model", "high"
				laterFast := fast == nil || !*fast
				environment.DefaultFast = &laterFast
				if err := environments.UpdateConfiguration(ctx, environment); err != nil {
					t.Fatal(err)
				}
				chats := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{"lead-chat": {Chat: models.Chat{ID: "lead-chat"}}, "member-chat": {Chat: models.Chat{ID: "member-chat"}}}}
				captured := &postTurnExecutionFake{}
				sut := fixture.service
				sut.wakers, sut.tasks = wakers, controllers.NewChannelTaskController(fixture.db)
				sut.chats, sut.executions, sut.capabilities = chats, captured, channeltool.NewRegistry()
				t.Cleanup(sut.capabilities.Close)
				switch route {
				case "lead":
					_, err = sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, Input: execution.WorkInput{Content: "继续原工作"}})
				case "member":
					_, err = sut.CreateTask(ctx, CreateTaskInput{ThreadID: thread.ID, SenderWakerID: "lead", AssigneeWakerID: "member", Subject: "继续原工作", Description: "使用原生默认执行"})
				case "notification":
					task := models.ChannelTask{ID: "reported-task", ThreadID: thread.ID, AssigneeWakerID: "member", Subject: "成员结果", Status: constants.ChannelTaskStatusCompleted, Result: "已核验", CurrentFence: 1}
					if err := sut.tasks.Create(ctx, task, nil); err != nil {
						t.Fatal(err)
					}
					if err := sut.tasks.CreateAttempt(ctx, models.ChannelTaskAttempt{ID: "reported-attempt", TaskID: task.ID, ChatID: "member-chat", ExecutionID: "reported-execution", AssigneeWakerID: "member", Fence: 1, Reason: constants.ChannelAttemptReasonInitial, Status: constants.ExecutionStatusCompleted}); err != nil {
						t.Fatal(err)
					}
					if err := sut.tasks.CreateNotification(ctx, models.ChannelLeadNotification{ID: "notification", ThreadID: thread.ID, TaskID: task.ID, AttemptID: "reported-attempt", Payload: task.Result, Status: constants.ChannelNotificationStatusPending}); err != nil {
						t.Fatal(err)
					}
					err = sut.drainNotifications(ctx, thread.ID)
				}
				if err != nil || captured.startCount() != 1 {
					t.Fatalf("团队派发 count=%d error=%v", captured.startCount(), err)
				}
				options := captured.firstStart().Options
				if (options.Fast == nil) != (fast == nil) || fast != nil && *options.Fast != *fast {
					t.Fatalf("派发改变 Fast: %+v expected=%v", options, fast)
				}
				effective, err := resolver.ResolveConfiguration(ctx, execution.ExecutionConfiguration{Resolved: options.ConfigurationResolved, RuntimeID: options.RuntimeID, AccountID: options.AccountID, Model: options.Model, Reasoning: options.Reasoning, Fast: options.Fast, PermissionLevel: options.PermissionLevel})
				if err != nil || effective.Model != "" || effective.Reasoning != "" || effective.AccountID != constants.AiAccountSystemID {
					t.Fatalf("已接纳原生空选择被新环境默认值覆盖: %+v error=%v", effective, err)
				}
			})
		}
	}
}

// leaderTurnChatGroupFake 提供首发路由测试使用的成员 Chat，不启动真实 Runtime。
type leaderTurnChatGroupFake struct {
	// chats 保存成员 Chat 摘要，供负责人繁忙检查和重启恢复读取。
	chats *postTurnChatFake
	// managedFilesDirectory 在附件用例中创建真实归属的团队工作区。
	managedFilesDirectory string
	// busyMember 模拟暂时无法接纳的成员。
	busyMember string
}

// PrepareChatGroup 为每位成员返回独立 Chat，并模拟首发执行期间负责人繁忙。
func (f *leaderTurnChatGroupFake) PrepareChatGroup(
	_ context.Context,
	intent execution.PrepareChatGroupIntent,
) (execution.PreparedChatGroup, error) {
	if f.managedFilesDirectory != "" {
		layout, err := chatstorage.NewLayout(f.managedFilesDirectory)
		if err != nil {
			return execution.PreparedChatGroup{}, err
		}
		directory, err := layout.TeamRoot(intent.ThreadID)
		if err != nil {
			return execution.PreparedChatGroup{}, err
		}
		if err := workspace.EnsureThreadExecutionWorkspace(directory, intent.ThreadID, intent.Scope.WakerID); err != nil {
			return execution.PreparedChatGroup{}, err
		}
	}
	f.chats.mutex.Lock()
	defer f.chats.mutex.Unlock()
	result := execution.PreparedChatGroup{}
	for _, member := range intent.Members {
		chat := models.Chat{
			ID: member.ChatID, WakerID: member.WakerID, Title: intent.Title,
			WorkspaceDirectory: intent.Scope.WorkingDirectory,
		}
		result.Chats = append(result.Chats, chat)
		if member.WakerID == intent.Scope.WakerID {
			chat.ActiveExecutionID = "initial-execution"
		} else if member.WakerID == f.busyMember {
			chat.ActiveExecutionID = "occupied"
		}
		f.chats.summaries[chat.ID] = chatservice.ChatSummary{Chat: chat}
	}
	return result, nil
}

// TestLeaderTurnModeSurvivesFirstSendAndQueueRecovery 验证首发与重启恢复均向执行层传递本轮意图。
func TestLeaderTurnModeSurvivesFirstSendAndQueueRecovery(t *testing.T) {
	for _, initialDirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial_direct=%t", initialDirect), func(t *testing.T) {
			database, err := dbhelper.NewDB(conf.DB{
				Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "leader-turn.db"), Migrate: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := database.DB.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			ctx := context.Background()
			channels := controllers.NewChannelController(database.DB)
			threads := controllers.NewChannelThreadController(database.DB)
			tasks := controllers.NewChannelTaskController(database.DB)
			wakers := controllers.NewWakerController(database.DB)
			leadID := "11111111111111111111111111111111"
			memberID := "22222222222222222222222222222222"
			channelID := "33333333333333333333333333333333"
			createChannelTestWakers(t, ctx, wakers, leadID, memberID)
			if err := channels.Create(ctx, models.Channel{
				ID: channelID, Name: "回合路由团队", LeadWakerID: leadID,
			}, []models.ChannelMember{
				{ChannelID: channelID, WakerID: leadID, RuntimeID: "codex", AccountID: "lead-account", PermissionLevel: constants.PermissionLevelAuto},
				{ChannelID: channelID, WakerID: memberID, RuntimeID: "codex", PermissionLevel: constants.PermissionLevelAuto, Position: 1},
			}); err != nil {
				t.Fatal(err)
			}
			chats := &postTurnChatFake{summaries: make(map[string]chatservice.ChatSummary)}
			executions := &postTurnExecutionFake{}
			newService := func() *service {
				return &service{
					channels: channels, threads: threads, tasks: tasks, wakers: wakers,
					transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB)),
					chats:        chats, executions: executions, chatGroups: &leaderTurnChatGroupFake{chats: chats},
					watchers: make(map[string]struct{}), capabilities: channeltool.NewRegistry(),
				}
			}
			sut := newService()
			if err := sut.Start(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sut.Close)
			mention := ""
			if initialDirect {
				mention = leadID
			}
			input := execution.WorkInput{Actor: "user", Content: "修改项目文件并验证"}
			created, err := sut.createThread(ctx, CreateThreadInput{
				ChannelID: channelID, ClientRequestID: "first-send", WorkingDirectory: t.TempDir(),
				MentionWakerID: mention, Input: input,
			})
			if err != nil {
				t.Fatal(err)
			}
			assertLeaderTurnIntent(t, executions.firstStart(), input.Content, initialDirect)
			if created.Members[0].AccountID != "lead-account" {
				t.Fatalf("account was not frozen in thread: %+v", created.Members)
			}
			if len(created.Tasks) != 0 {
				t.Fatalf("lead input created tasks: %+v", created.Tasks)
			}

			// 下一条使用相反模式且正文完全相同，必须只依赖结构化提及，不受历史回合影响。
			mention = leadID
			if initialDirect {
				mention = ""
			}
			queued, err := sut.sendMessage(ctx, SendMessageInput{
				ThreadID: created.Thread.ID, MentionWakerID: mention, Input: input,
			})
			if err != nil {
				t.Fatal(err)
			}
			if queued.QueuedInput == nil || queued.Execution != nil || queued.Task != nil || executions.startCount() != 1 {
				t.Fatalf("busy lead input was not queued: %+v", queued)
			}
			sut.Close()
			chats.mutex.Lock()
			summary := chats.summaries[created.Thread.LeadChatID]
			summary.Chat.ActiveExecutionID = ""
			chats.summaries[created.Thread.LeadChatID] = summary
			chats.mutex.Unlock()
			recovered := newService()
			if err := recovered.Start(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(recovered.Close)
			if executions.startCount() != 2 {
				t.Fatalf("recovered executions = %d, want 2", executions.startCount())
			}
			assertLeaderTurnIntent(t, executions.startAt(1), input.Content, !initialDirect)
		})
	}
}

// assertLeaderTurnIntent 检查执行边界保留原文和权限，并只包含当前回合的路由意图。
func assertLeaderTurnIntent(t *testing.T, intent execution.WorkIntent, content string, direct bool) {
	t.Helper()
	if intent.Input.Content != content || intent.Options.AccountID != "lead-account" || intent.Options.PermissionLevel != constants.PermissionLevelAuto || intent.Options.WorkspaceReadOnly {
		t.Fatalf("lead input or permission changed: %+v", intent)
	}
	instructions := intent.Options.SupplementalInstructions
	if len(instructions) != 1 || instructions[0].Name != constants.TeamInstructionFileName {
		t.Fatalf("lead instructions = %+v", instructions)
	}
	want, unwanted := "This is a coordination turn", "This is a direct-execution turn"
	if direct {
		want, unwanted = unwanted, want
	}
	if strings.Contains(instructions[0].Content, "## Current Lead Turn") {
		t.Fatal("current turn mode polluted stable TEAM")
	}
	context := intent.Input.ContextInstructions
	if len(context) == 0 || !strings.Contains(context[len(context)-1].Content, want) || strings.Contains(context[len(context)-1].Content, unwanted) {
		t.Fatalf("direct=%t current input lost mode: %+v", direct, context)
	}
}

// TestAutomaticTeamSourceFollowsOpenRun 验证自动运行封闭后负责人和成员的新工作不会复用历史调用身份。
func TestAutomaticTeamSourceFollowsOpenRun(t *testing.T) {
	for _, closed := range []bool{false, true} {
		for _, route := range []string{"lead", "direct_member", "lead_created_member", "notification"} {
			t.Run(fmt.Sprintf("closed=%t/%s", closed, route), func(t *testing.T) {
				ctx := t.Context()
				db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "source.db"), Migrate: true})
				if err != nil {
					t.Fatal(err)
				}
				pool, _ := db.DB.DB()
				t.Cleanup(func() { _ = pool.Close() })
				channels := controllers.NewChannelController(db.DB)
				threads := controllers.NewChannelThreadController(db.DB)
				tasks := controllers.NewChannelTaskController(db.DB)
				runs := controllers.NewAutomaticTaskController(db.DB)
				wakers := controllers.NewWakerController(db.DB)
				createChannelTestWakers(t, ctx, wakers, "lead-waker", "member-waker")
				if err := channels.Create(ctx, models.Channel{ID: "team", Name: "来源测试", LeadWakerID: "lead-waker"}, []models.ChannelMember{{ChannelID: "team", WakerID: "lead-waker"}, {ChannelID: "team", WakerID: "member-waker"}}); err != nil {
					t.Fatal(err)
				}
				thread := models.ChannelThread{ID: "thread", ChannelID: "team", LeadWakerID: "lead-waker", LeadChatID: "lead-chat", AutomaticRunID: "automatic-run", ReadyAt: testTimestamp(1), MaxParallel: 2, MaxTasks: 10}
				if err := threads.Create(ctx, thread, []models.ChannelThreadMember{
					{ThreadID: thread.ID, WakerID: "lead-waker", ChatID: "lead-chat", IsLead: true, RuntimeID: "codex"},
					{ThreadID: thread.ID, WakerID: "member-waker", ChatID: "member-chat", RuntimeID: "codex"},
				}); err != nil {
					t.Fatal(err)
				}
				if err := runs.CreateRun(ctx, models.AutomaticRun{ID: "automatic-run", TaskID: "automatic-task", ThreadID: thread.ID}); err != nil {
					t.Fatal(err)
				}
				chats := &postTurnChatFake{summaries: map[string]chatservice.ChatSummary{
					"lead-chat": {Chat: models.Chat{ID: "lead-chat"}}, "member-chat": {Chat: models.Chat{ID: "member-chat"}},
				}}
				executions := &postTurnExecutionFake{}
				sut := &service{channels: channels, threads: threads, tasks: tasks, wakers: wakers, chats: chats, executions: executions,
					transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), capabilities: channeltool.NewRegistry(), automaticRun: runs.GetRun,
				}
				// 模拟原调度器在用户新输入前冻结已经结束的团队运行；实际来源读取仍访问真实仓库。
				sut.beforeUserInput = func(ctx context.Context, _ string) error {
					if closed {
						return runs.CloseTeamRun(ctx, "automatic-run", constants.AutomaticRunStateCompleted, testTimestamp(5))
					}
					return nil
				}
				switch route {
				case "lead", "direct_member":
					mention := ""
					if route == "direct_member" {
						mention = "member-waker"
					}
					if _, err := sut.sendMessage(ctx, SendMessageInput{ThreadID: thread.ID, MentionWakerID: mention, Input: execution.WorkInput{Content: "新的用户工作"}}); err != nil {
						t.Fatal(err)
					}
				case "lead_created_member":
					// 原自动内部的负责人派发与用户续聊负责人派发都走正式任务路径。
					if _, err := sut.CreateTask(ctx, CreateTaskInput{ThreadID: thread.ID, SenderWakerID: thread.LeadWakerID, AssigneeWakerID: "member-waker", Subject: "成员工作", Description: "读取知识并报告"}); err != nil {
						t.Fatal(err)
					}
				case "notification":
					if closed {
						if err := runs.CloseTeamRun(ctx, "automatic-run", constants.AutomaticRunStateCompleted, testTimestamp(5)); err != nil {
							t.Fatal(err)
						}
					}
					task := models.ChannelTask{ID: "reported-task", ThreadID: thread.ID, AssigneeWakerID: "member-waker", Subject: "成员结果", Status: constants.ChannelTaskStatusCompleted, Result: "已核验", CurrentFence: 1}
					if err := tasks.Create(ctx, task, nil); err != nil {
						t.Fatal(err)
					}
					attempt := models.ChannelTaskAttempt{ID: "reported-attempt", TaskID: task.ID, ChatID: "member-chat", ExecutionID: "reported-execution", AssigneeWakerID: "member-waker", Fence: 1, Reason: constants.ChannelAttemptReasonInitial, Status: constants.ExecutionStatusCompleted}
					if err := tasks.CreateAttempt(ctx, attempt); err != nil {
						t.Fatal(err)
					}
					if err := tasks.CreateNotification(ctx, models.ChannelLeadNotification{ID: "notification", ThreadID: thread.ID, TaskID: task.ID, AttemptID: attempt.ID, Payload: task.Result, Status: constants.ChannelNotificationStatusPending}); err != nil {
						t.Fatal(err)
					}
					if err := sut.drainNotifications(ctx, thread.ID); err != nil {
						t.Fatal(err)
					}
				}
				if executions.startCount() != 1 {
					t.Fatalf("execution count=%d", executions.startCount())
				}
				want := thread.AutomaticRunID
				if closed {
					want = ""
				}
				if got := executions.firstStart().AutomaticRunID; got != want {
					t.Fatalf("AutomaticRunID=%q, want %q", got, want)
				}
				stored, err := threads.Get(ctx, thread.ID)
				if err != nil || stored.AutomaticRunID != thread.AutomaticRunID {
					t.Fatalf("historical thread source changed: %+v %v", stored, err)
				}
			})
		}
	}
}
