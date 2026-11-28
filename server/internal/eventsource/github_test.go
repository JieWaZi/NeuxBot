package eventsource

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGitHubSourceUsesStablePullReviewAndPassingCheckIdentities(t *testing.T) {
	var repository string
	source := GitHubSource{ReadPulls: func(_ context.Context, name string) (json.RawMessage, error) {
		repository = name
		return json.RawMessage(`[
		 {"number":42,"title":"   Fix\n parser  ","isDraft":false,"headRefOid":"a99",
		  "reviews":[{"id":"review-1","state":"APPROVED","submittedAt":"2026-11-27T01:30:00Z"}],
		  "statusCheckRollup":[{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS"}]},
		 {"number":41,"title":"Draft","isDraft":true,"headRefOid":"b81",
		  "reviews":[{"id":"pending","state":"PENDING"}],
		  "statusCheckRollup":[{"__typename":"StatusContext","context":"ci","state":"FAILURE"}]}
		]`), nil
	}}
	config, err := Normalize(Config{Source: GitHubPullRequests, Repository: "https://github.com/Example/Project.git", Kinds: []string{PullOpened, PullReviewed, ChecksPassed}})
	if err != nil {
		t.Fatal(err)
	}
	events, err := source.Observe(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if repository != "example/project" || len(events) != 4 {
		t.Fatalf("repository=%q events=%+v", repository, events)
	}
	for i, kind := range []string{PullOpened, PullReviewed, ChecksPassed, PullOpened} {
		if events[i].Kind != kind || !events[i].Valid() {
			t.Fatalf("event %d = %+v", i, events[i])
		}
	}
	if events[0].Topic != "github:example/project:pull:42" || events[0].URL != "https://github.com/example/project/pull/42" || events[0].Title != "Fix parser" || !events[3].Draft {
		t.Fatalf("unexpected event metadata: %+v", events)
	}
	again, err := source.Observe(context.Background(), config)
	if err != nil || again[1].ID != events[1].ID || again[2].ID != events[2].ID {
		t.Fatalf("identities changed: %+v, %v", again, err)
	}
}

func TestGitHubRuleRejectsArbitraryHostsAndUnknownEventKinds(t *testing.T) {
	for _, config := range []Config{
		{Source: GitHubPullRequests, Repository: "https://example.invalid/owner/repo"},
		{Source: GitHubPullRequests, Repository: "--repo/test"},
		{Source: GitHubPullRequests, Repository: "owner/repo/extra"},
		{Source: GitHubPullRequests, Repository: "owner/repo", Kinds: []string{"arbitrary"}},
	} {
		if _, err := Normalize(config); err == nil {
			t.Errorf("accepted invalid configuration %+v", config)
		}
	}
}
