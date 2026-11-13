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
	ctx.Set("ally.auth.subject", "web:local")
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
