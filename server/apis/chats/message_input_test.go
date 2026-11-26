package chats

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
)

func TestSendMessagePreservesSubmissionIdentityAtHTTPBoundary(t *testing.T) {
	var request SendMessage
	if err := json.Unmarshal([]byte(`{"submissionId":" continued-input ","text":"正文"}`), &request.Body); err != nil {
		t.Fatal(err)
	}
	input, _ := request.Body.workInput()
	if input.SubmissionID != "continued-input" {
		t.Fatalf("HTTP request identity = %q", input.SubmissionID)
	}

	var create CreateChatRequest
	if err := json.Unmarshal([]byte(`{"clientRequestId":"first-input","text":"正文"}`), &create); err != nil {
		t.Fatal(err)
	}
	first, _ := create.MessageRequest.workInput()
	if create.ClientRequestID != "first-input" || first.SubmissionID != "" {
		t.Fatal("creation must retain its one existing request identity")
	}
}

func TestMessagePreservesSteerExecution(t *testing.T) {
	var request SendMessageRequest
	if err := json.Unmarshal([]byte(`{"steerExecutionId":" previous ","text":"追加要求"}`), &request); err != nil {
		t.Fatal(err)
	}
	input, _ := request.workInput()
	if input.SteerExecutionID != "previous" {
		t.Fatalf("steer identity = %q", input.SteerExecutionID)
	}
}

func TestSendMessagePassesOnlyTheReplyTargetIdentity(t *testing.T) {
	previousExecution := global.ExecutionService
	t.Cleanup(func() { global.ExecutionService = previousExecution })
	core := &createChatReplayExecution{}
	global.ExecutionService = core
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/chats/chat-id/messages", nil)
	request := SendMessage{ChatID: "chat-id"}
	if err := json.Unmarshal([]byte(`{"text":"正文","replyToMessageId":" target-id ","replyTo":{"messageId":"forged","preview":"伪造正文"}}`), &request.Body); err != nil {
		t.Fatal(err)
	}
	if _, err := request.Output(ctx); err != nil {
		t.Fatal(err)
	}
	if core.intent.Input.ReplyToMessageID != "target-id" || core.intent.Input.Content != "正文" {
		t.Fatalf("reply intent = %q, text = %q", core.intent.Input.ReplyToMessageID, core.intent.Input.Content)
	}
}

func TestPrivateChatAttachmentOnlyKeepsTheRuntimePrompt(t *testing.T) {
	for _, request := range []interface {
		workInput() (execution.WorkInput, execution.WorkOptions)
	}{&MessageRequest{}, &SendMessageRequest{}} {
		if err := json.Unmarshal([]byte(`{"text":"请查看附件。","attachmentOnly":true,"attachments":[{"sourcePath":"notice.txt"}]}`), request); err != nil {
			t.Fatal(err)
		}
		input, options := request.workInput()
		if !input.AttachmentOnly || input.Content != "请查看附件。" || len(input.Attachments) != 1 || options.EntryPoint != execution.WorkbenchEntryPoint {
			t.Fatalf("附件输入转换丢失来源或执行正文: %+v", input)
		}
	}
}

func TestPrivateChatCodeReferencePromptKeepsItsSource(t *testing.T) {
	for _, request := range []interface {
		workInput() (execution.WorkInput, execution.WorkOptions)
	}{&MessageRequest{}, &SendMessageRequest{}} {
		if err := json.Unmarshal([]byte(`{"text":"请查看引用的代码片段。","codeReferenceOnly":true,"codeReferences":[{"path":"/work/example.go"}]}`), request); err != nil {
			t.Fatal(err)
		}
		input, _ := request.workInput()
		if !input.CodeReferenceOnly || input.Content != "请查看引用的代码片段。" || len(input.CodeReferences) != 1 {
			t.Fatalf("代码引用输入转换丢失来源或执行正文: %+v", input)
		}
	}
}

func TestChatWorkflowSelectionKeepsOnlyTheDefinitionFromHTTP(t *testing.T) {
	for _, request := range []interface {
		workInput() (execution.WorkInput, execution.WorkOptions)
	}{&MessageRequest{}, &SendMessageRequest{}} {
		if err := json.Unmarshal([]byte(`{"workflowId":" selected-flow ","revisionId":"forged","name":"forged"}`), request); err != nil {
			t.Fatal(err)
		}
		input, _ := request.workInput()
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["workflow"]) != `{"id":"selected-flow"}` || input.Content != "" {
			t.Fatalf("unexpected workflow selection: %s", encoded)
		}
	}
}

type createChatReplayExecution struct {
	// Service 保留测试无需调用的执行端口。
	execution.Service
	// intent 保存首发用例收到的业务输入。
	intent execution.WorkIntent
}

func (s *createChatReplayExecution) Start(_ context.Context, intent execution.WorkIntent) (execution.AcceptedExecution, error) {
	s.intent = intent
	return execution.AcceptedExecution{Replayed: true}, nil
}

func TestCreateChatCanReplayBeforeClaimingUnavailableUpload(t *testing.T) {
	previousExecution, previousUploads := global.ExecutionService, global.Uploads
	t.Cleanup(func() { global.ExecutionService, global.Uploads = previousExecution, previousUploads })
	core := &createChatReplayExecution{}
	global.ExecutionService, global.Uploads = core, nil
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/chats", nil)
	ctx.Set("bot.auth.subject", "web:local")
	ref := "upload:expired/note.txt"
	create := CreateChat{Body: CreateChatRequest{
		ClientRequestID: "same-request",
		WakerID:         "waker-1",
		MessageRequest:  MessageRequest{Text: "整理附件", Attachments: []MessageAttachment{{SourcePath: ref}}},
	}}

	result, err := create.Output(ctx)
	receipt, ok := result.(execution.AcceptedExecution)
	if err != nil || !ok || !receipt.Replayed {
		t.Fatalf("expected the authority's replay receipt, got %+v, %v", result, err)
	}
	if len(core.intent.Input.Attachments) != 1 || core.intent.Input.Attachments[0].SourcePath != ref {
		t.Fatalf("execution did not receive the original attachment reference: %+v", core.intent.Input.Attachments)
	}
	if _, err := core.intent.ClaimAttachments(core.intent.Input.Attachments); !errors.Is(err, status_error.MessageAttachmentInvalid) {
		t.Fatalf("fresh admission without staged bytes = %v", err)
	}
}
