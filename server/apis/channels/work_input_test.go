package channels

import (
	"encoding/json"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestChannelHTTPInputKeepsOneExistingSubmissionIdentity(t *testing.T) {
	var continuation SendChannelMessage
	if err := json.Unmarshal([]byte(`{"submissionId":" team-input ","text":"相同正文"}`), &continuation.Body); err != nil {
		t.Fatal(err)
	}
	if got := continuation.Body.workInput().SubmissionID; got != "team-input" {
		t.Fatalf("team continuation input identity = %q", got)
	}
	var creation CreateChannelThreadRequest
	if err := json.Unmarshal([]byte(`{"clientRequestId":" first-team-input ","text":"相同正文"}`), &creation); err != nil {
		t.Fatal(err)
	}
	if got := creation.workInput().SubmissionID; got != "first-team-input" {
		t.Fatalf("team initial input identity = %q", got)
	}
}

func TestChannelHTTPInputKeepsReplyIdentityAndAttachmentProvenance(t *testing.T) {
	var request SendChannelMessageRequest
	if err := json.Unmarshal([]byte(`{"replyToMessageId":" report-id ","attachmentOnly":true,"text":"请查看附件。"}`), &request); err != nil {
		t.Fatal(err)
	}
	input := request.workInput()
	if input.ReplyToMessageID != "report-id" || !input.AttachmentOnly || input.Content != "请查看附件。" {
		t.Fatalf("团队输入转换丢失引用或附件来源: %+v", input)
	}
}

func TestChannelHTTPInputKeepsCodeReferencePromptSource(t *testing.T) {
	for _, request := range []interface {
		workInput() execution.WorkInput
	}{&CreateChannelThreadRequest{}, &SendChannelMessageRequest{}} {
		if err := json.Unmarshal([]byte(`{"text":"请查看引用的代码片段。","codeReferenceOnly":true,"codeReferences":[{"path":"/work/example.go"}]}`), request); err != nil {
			t.Fatal(err)
		}
		input := request.workInput()
		if !input.CodeReferenceOnly || input.Content != "请查看引用的代码片段。" || len(input.CodeReferences) != 1 {
			t.Fatalf("团队输入转换丢失代码引用来源或执行正文: %+v", input)
		}
	}
}

func TestChannelWorkflowSelectionSurvivesFirstAndLaterInputs(t *testing.T) {
	var first CreateChannelThreadRequest
	var next SendChannelMessageRequest
	payload := []byte(`{"clientRequestId":"first","submissionId":"next","workflowId":"flow"}`)
	if err := json.Unmarshal(payload, &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &next); err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{first.workInput(), next.workInput()} {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["workflow"]) != `{"id":"flow"}` {
			t.Fatalf("team input lost workflow selection: %s", encoded)
		}
	}
}
