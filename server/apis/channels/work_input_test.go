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
