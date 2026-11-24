package reviewstate

import (
	"strings"
	"testing"
	"time"
)

func TestReviewWindowCountsOnceAndPreservesTriggeredTail(t *testing.T) {
	now := time.Unix(10000, 0)
	turns := []Turn{
		{Sequence: 3, UserID: "u1", User: strings.Repeat("x", 6500), At: now.Add(-16 * time.Minute), Memory: true, Skill: true},
		{Sequence: 6, UserID: "u2", User: "next", At: now.Add(-16 * time.Minute), Memory: true},
		{Sequence: 9, UserID: "u3", User: "tail", At: now.Add(-16 * time.Minute), Skill: true},
	}
	if ReadyThrough(turns[:2], 0, DefaultEveryTurns, false, now) != 0 {
		t.Fatal("one turn eligible for both features must not count twice")
	}
	if got := ReadyThrough(turns, 0, DefaultEveryTurns, false, now); got != 9 {
		t.Fatalf("triggered through = %d", got)
	}
	if got := ReadyThrough(turns[2:], 9, DefaultEveryTurns, true, now); got != 9 {
		t.Fatalf("a previously triggered tail was stranded: %d", got)
	}
}

func TestEvidenceDoesNotExposeSkillEvidenceToMemoryOnly(t *testing.T) {
	turns := []Turn{
		{Sequence: 1, UserID: "message1", User: "Please call me T", Assistant: "sensitive assistant content", Memory: true},
		{Sequence: 2, UserID: "message2", User: "Use this technique", Assistant: "technique", Skill: true},
	}
	got := BuildEvidence(turns)
	if got[0].Assistant != "" || got[1].Assistant != "technique" || got[1].Memory {
		t.Fatalf("unexpected outbound projection: %#v", got)
	}
}

func TestEvidenceRedactsSecretsAndBoundsUTF8(t *testing.T) {
	input := strings.Repeat("例", 5000) + " token=private-token"
	got := BuildEvidence([]Turn{{Sequence: 1, UserID: "id1", User: input, Skill: true},
		{Sequence: 2, UserID: "id2", User: "token=unguessable-credential", Memory: true}})
	if len(got) != 2 || strings.Contains(got[1].User, "unguessable-credential") || len(got[0].User) > 6144 {
		t.Fatalf("unsafe or oversized evidence returned: %d", len(got))
	}
}

func TestEvidenceRedactsOpaqueAuthorizationAndQuotedValues(t *testing.T) {
	cases := []struct{ input, secret string }{
		{"Authorization: Bearer opaque_for_review", "opaque_for_review"},
		{"Authorization: Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{`{"password":"prefix\" suffix-credential"}`, "suffix-credential"},
		{"Cookie: session=opaque_value", "opaque_value"},
		{`password="quoted value with spaces"`, "quoted value with spaces"},
		{`"api_key": "quoted_credential"`, "quoted_credential"},
	}
	for _, tc := range cases {
		if strings.Contains(Redact(tc.input), tc.secret) {
			t.Error("credential was not removed")
		}
	}
}
