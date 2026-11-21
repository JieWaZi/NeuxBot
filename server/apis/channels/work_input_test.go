package channels

import (
	"encoding/json"
	"testing"
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
