package chats

import (
	"encoding/json"
	"testing"
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
