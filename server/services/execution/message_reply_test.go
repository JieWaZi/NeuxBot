package execution

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chat/messageview"
	chatservice "github.com/JieWaZi/NeuxBot/server/services/chat"
)

func TestReplyAdmissionPublishesTheQuoteAndDeliversTheAuthoritativeBody(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	target := "原文\n\n" + strings.Repeat("谈", 5000) + "只在截断尾部出现"
	first := createReplyTestChat(t, manager, "reply-source", target, false)
	intent := replyTestIntent(first.Chat.ID, first.Execution.UserMessageID)
	accepted, err := manager.execution.Start(t.Context(), intent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	page, err := manager.chats.(chatservice.Module).ReadMessageTimeline(t.Context(), first.Chat.ID, 0, 0, 1)
	if err != nil || len(page.Resources) != 1 {
		t.Fatalf("latest history page: %+v, %v", page, err)
	}
	page, err = manager.chats.(chatservice.Module).ReadMessageTimeline(t.Context(), first.Chat.ID, page.NextBeforeSequence, 0, 1)
	if err != nil || len(page.Resources) != 1 || page.Resources[0].ID != accepted.Execution.UserMessageID {
		t.Fatalf("reply history page: %+v, %v", page, err)
	}
	user := page.Resources[0]
	if user.Content != "这轮问题" || user.ReplyTo == nil || user.ReplyTo.MessageID != first.Execution.UserMessageID ||
		!strings.HasPrefix(user.ReplyTo.Preview, "原文\n\n谈") || !strings.HasSuffix(user.ReplyTo.Preview, "…") ||
		utf8.RuneCountInString(user.ReplyTo.Preview) > 160 {
		t.Fatalf("invalid public reply: %+v", user.ReplyTo)
	}
	var wire struct {
		// Input 保存实际 app-server 的有序输入部件。
		Input []struct {
			// Text 是单个正文或事实部件的文本。
			Text string `json:"text"`
		} `json:"input"`
	}
	calls := manager.nativeCalls(t, "turn/start")
	if len(calls) != 2 || json.Unmarshal(calls[1], &wire) != nil || len(wire.Input) != 2 || wire.Input[0].Text != "这轮问题" {
		t.Fatalf("native input did not keep its body and facts separate: %s", calls)
	}
	var got struct {
		// Role 是引用的原消息作者角色。
		Role string `json:"role"`
		// Text 是可供模型参考的摘录。
		Text string `json:"text"`
		// Truncated 表示原消息还有未送达的文本。
		Truncated bool `json:"truncated"`
	}
	block := strings.TrimSpace(wire.Input[1].Text)
	fact := strings.TrimSuffix(strings.TrimPrefix(block, "<neuxbot-turn-context>\n"), "\n</neuxbot-turn-context>")
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fact), &rawFields); err != nil {
		t.Fatalf("invalid context fact block: %s", block)
	}
	if err := json.Unmarshal(rawFields["Reply target"], &got); err != nil {
		t.Fatalf("missing structured reply fact: %s", block)
	}
	if got.Role != "user" || !got.Truncated || !strings.HasPrefix(got.Text, "原文\n\n谈") ||
		strings.Contains(got.Text, "只在截断尾部出现") || !utf8.ValidString(got.Text) ||
		utf8.RuneCountInString(got.Text) > 4096 || len(got.Text) <= len(user.ReplyTo.Preview) {
		t.Fatalf("invalid native reply excerpt: %+v", got)
	}
	if replay, err := manager.execution.Start(t.Context(), intent); err != nil || !replay.Replayed || replay.Execution.ID != accepted.Execution.ID {
		t.Fatalf("same reply did not replay: %+v, %v", replay, err)
	}
	otherTarget := replyTestIntent(first.Chat.ID, first.Execution.ID)
	if _, err := manager.execution.Start(t.Context(), otherTarget); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("different reply target reused accepted submission: %v", err)
	}
	plainTurn := replyTestIntent(first.Chat.ID, "")
	plainTurn.Input.SubmissionID = "plain-after-reply"
	if _, err := manager.execution.Start(t.Context(), plainTurn); err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	calls = manager.nativeCalls(t, "turn/start")
	if len(calls) != 3 || strings.Contains(string(calls[2]), "Reply target") {
		t.Fatalf("plain continuation inherited a prior reply target: %s", calls)
	}
}

func TestReplyAdmissionRejectsUnavailableTargetsWithoutStartingRuntime(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	first := createReplyTestChat(t, manager, "reply-destination", "当前对话", false)
	other := createReplyTestChat(t, manager, "reply-other", "另一个对话", false)
	hidden := createReplyTestChat(t, manager, "reply-hidden", "后台对话", true)
	for _, tc := range []struct {
		// name 标识本次不可引用的原因。
		name string
		// chatID 是尝试续发的 Chat。
		chatID string
		// messageID 是作为原消息提交的标识。
		messageID string
	}{
		{"missing", first.Chat.ID, "message-does-not-exist"},
		{"cross-chat", first.Chat.ID, other.Execution.UserMessageID},
		{"hidden chat", hidden.Chat.ID, hidden.Execution.UserMessageID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manager.execution.Start(t.Context(), replyTestIntent(tc.chatID, tc.messageID))
			if !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
				t.Fatalf("unavailable target = %v", err)
			}
		})
	}
	if calls := manager.nativeCalls(t, "turn/start"); len(calls) != 3 {
		t.Fatalf("rejected reply reached the Runtime: %s", calls)
	}
}

func TestReplyPreviewIsIndependentOfModelExcerptTruncation(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	const visible = "第一段\n\n第二段完整正文"
	first := createReplyTestChat(t, manager, "padded-reply-source", strings.Repeat(" ", constants.ChatReplyExcerptLimit-4)+visible, false)
	target, err := manager.chats.(chatservice.Journal).ReadReplyTarget(t.Context(), first.Chat.ID, first.Execution.UserMessageID)
	if err != nil || target.Reference.Preview != visible {
		t.Fatalf("public reply preview was cut by the model's input limit: %q, %v", target.Reference.Preview, err)
	}
}

func TestReplyAdmissionUsesTerminalAssistantResponse(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	first := createReplyTestChat(t, manager, "reply-assistant", "初始问题", false)
	accepted, err := manager.execution.Start(t.Context(), replyTestIntent(first.Chat.ID, first.Execution.ID))
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	calls := manager.nativeCalls(t, "turn/start")
	if len(calls) != 2 || !strings.Contains(string(calls[1]), `\"role\":\"assistant\",\"text\":\"fixture reply\"`) {
		t.Fatalf("terminal assistant body absent from reply turn: %s", calls)
	}
	detail, err := manager.chats.(chatservice.Module).ReadMessageHistory(t.Context(), first.Chat.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var reply *messageview.Message
	for index := range detail.Resources {
		if detail.Resources[index].ID == accepted.Execution.UserMessageID {
			reply = &detail.Resources[index]
		}
	}
	if reply == nil || reply.ReplyTo == nil || reply.ReplyTo.Preview != "fixture reply" {
		t.Fatalf("terminal assistant preview was not persisted: %+v", reply)
	}
	quoted, err := json.Marshal(reply.ReplyTo)
	if err != nil || !strings.Contains(string(quoted), `"role":"assistant"`) {
		t.Fatalf("terminal assistant role was not persisted: %s, %v", quoted, err)
	}
}

func TestReplyAdmissionDistinguishesAttachmentPromptFromTypedText(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := t.Context()
	file := filepath.Join(t.TempDir(), "notice.txt")
	if err := os.WriteFile(file, []byte("附件"), 0600); err != nil {
		t.Fatal(err)
	}
	input := WorkInput{Actor: "user", Content: "请查看附件。", AttachmentOnly: true, Attachments: []AttachmentInput{{SourcePath: file}}}
	firstIntent := WorkIntent{
		ClientRequestID: "synthetic-attachment-prompt",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1"}},
		Input:           input,
		Options:         WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
	}
	first, err := manager.execution.Start(ctx, firstIntent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	module := manager.chats.(chatservice.Module)
	page, err := module.ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
	if err != nil || len(page.Resources) != 2 || !page.Resources[0].AttachmentOnly || page.Resources[0].Content != firstIntent.Input.Content {
		t.Fatalf("attachment-only input provenance was not projected: %+v, %v", page.Resources, err)
	}
	invalid := replyTestIntent(first.Chat.ID, "")
	invalid.Input.AttachmentOnly = true
	if _, err := manager.execution.Start(ctx, invalid); !errors.Is(err, status_error.MessageAttachmentInvalid) {
		t.Fatalf("attachment-only without an attachment was accepted: %v", err)
	}
	if _, err := manager.execution.Start(ctx, replyTestIntent(first.Chat.ID, first.Execution.UserMessageID)); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("synthetic attachment prompt is not a reply target: %v", err)
	}
	typed := replyTestIntent(first.Chat.ID, "")
	typed.Input.Content = firstIntent.Input.Content
	typed.Input.Attachments = firstIntent.Input.Attachments
	typed.Input.SubmissionID = "typed-attachment-prompt"
	second, err := manager.execution.Start(ctx, typed)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	changed := typed
	changed.Input.AttachmentOnly = true
	if _, err := manager.execution.Start(ctx, changed); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changing attachment provenance reused the old submission: %v", err)
	}
	if _, err := manager.execution.Start(ctx, replyTestIntent(first.Chat.ID, second.Execution.UserMessageID)); err != nil {
		t.Fatalf("same text typed by the user was rejected: %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
}

func TestReplyAdmissionDistinguishesCodeReferencePromptFromTypedText(t *testing.T) {
	manager := newMessageTestService(t, t.TempDir())
	ctx := t.Context()
	directory := t.TempDir()
	file := filepath.Join(directory, "example.txt")
	if err := os.WriteFile(file, []byte("代码引用示例"), 0600); err != nil {
		t.Fatal(err)
	}
	const prompt = "请查看引用的代码片段。"
	var firstInput WorkInput
	if err := json.Unmarshal([]byte(`{"actor":"user","content":"请查看引用的代码片段。","codeReferenceOnly":true}`), &firstInput); err != nil {
		t.Fatal(err)
	}
	firstInput.CodeReferences = []CodeReferenceInput{{Path: file}}
	options := WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto}
	firstIntent := WorkIntent{
		ClientRequestID: "synthetic-code-reference-prompt",
		Chat:            ChatTarget{Create: &CreateChatSpec{WakerID: "waker-1", WorkingDirectory: directory}},
		Input:           firstInput,
		Options:         options,
	}
	first, err := manager.execution.Start(ctx, firstIntent)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if _, err := manager.execution.Start(ctx, replyTestIntent(first.Chat.ID, first.Execution.UserMessageID)); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("synthetic code-reference message was accepted as a reply target: %v", err)
	}
	page, err := manager.chats.(chatservice.Module).ReadMessageHistory(ctx, first.Chat.ID, 0, 10)
	if err != nil || len(page.Resources) != 2 || page.Resources[0].Content != prompt {
		t.Fatalf("code-reference body was not projected: %+v, %v", page.Resources, err)
	}
	view, err := json.Marshal(page.Resources[0])
	if err != nil || !strings.Contains(string(view), `"codeReferenceOnly":true`) {
		t.Fatalf("code-reference provenance was not projected: %s, %v", view, err)
	}
	if calls := manager.nativeCalls(t, "turn/start"); len(calls) != 1 || !strings.Contains(string(calls[0]), prompt) {
		t.Fatalf("native runtime did not receive the code-reference prompt: %s", calls)
	}
	if replay, err := manager.execution.Start(ctx, firstIntent); err != nil || !replay.Replayed || replay.Execution.ID != first.Execution.ID {
		t.Fatalf("first input did not replay with the original provenance: %+v, %v", replay, err)
	}
	changedFirst := firstIntent
	changedFirst.Input.CodeReferenceOnly = false
	if _, err := manager.execution.Start(ctx, changedFirst); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changing code-reference provenance reused the old first submission: %v", err)
	}
	invalid := replyTestIntent(first.Chat.ID, "")
	invalid.Input.CodeReferenceOnly = true
	if _, err := manager.execution.Start(ctx, invalid); !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("code-reference prompt without a code reference was accepted: %v", err)
	}
	withAttachment := replyTestIntent(first.Chat.ID, "")
	withAttachment.Input = firstInput
	withAttachment.Input.SubmissionID = "synthetic-attachment-and-code-reference-prompt"
	withAttachment.Input.Attachments = []AttachmentInput{{SourcePath: file}}
	second, err := manager.execution.Start(ctx, withAttachment)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if _, err := manager.execution.Start(ctx, replyTestIntent(first.Chat.ID, second.Execution.UserMessageID)); !errors.Is(err, status_error.ChatReplyTargetUnavailable) {
		t.Fatalf("synthetic code-reference message with an attachment was accepted as a reply target: %v", err)
	}
	typed := withAttachment
	typed.Input = WorkInput{Actor: "user", Content: prompt, Attachments: withAttachment.Input.Attachments, CodeReferences: firstInput.CodeReferences}
	typed.Input.SubmissionID = withAttachment.Input.SubmissionID
	if _, err := manager.execution.Start(ctx, typed); !errors.Is(err, status_error.CreateRequestConflict) {
		t.Fatalf("changing code-reference provenance reused the old submission: %v", err)
	}
	typed.Input.SubmissionID = "typed-code-reference-prompt"
	third, err := manager.execution.Start(ctx, typed)
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
	if _, err := manager.execution.Start(ctx, replyTestIntent(first.Chat.ID, third.Execution.UserMessageID)); err != nil {
		t.Fatalf("same text typed by the user was rejected: %v", err)
	}
	waitForChatTerminal(t, manager, first.Chat.ID)
}

func createReplyTestChat(t *testing.T, manager messageTestService, id, content string, hidden bool) AcceptedExecution {
	t.Helper()
	accepted, err := manager.execution.Start(t.Context(), WorkIntent{
		ClientRequestID: id,
		Chat: ChatTarget{Create: &CreateChatSpec{
			WakerID: "waker-1", HiddenFromWakerHistory: hidden,
		}},
		Input:   WorkInput{Actor: "user", Content: content},
		Options: WorkOptions{RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForChatTerminal(t, manager, accepted.Chat.ID)
	return accepted
}

func replyTestIntent(chatID, messageID string) WorkIntent {
	return WorkIntent{
		Chat:    ChatTarget{ChatID: chatID},
		Input:   WorkInput{Actor: "user", SubmissionID: "reply-submission", Content: "这轮问题", ReplyToMessageID: messageID},
		Options: WorkOptions{EntryPoint: WorkbenchEntryPoint, RuntimeID: constants.RuntimeIDCodex, PermissionLevel: constants.PermissionLevelAuto},
	}
}
