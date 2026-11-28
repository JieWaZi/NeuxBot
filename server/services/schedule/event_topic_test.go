package schedule

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestEventTopicSerializesPendingEvaluationAndCancellation(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)
	service := New(Config{EvaluateEvent: func(ctx context.Context, _ models.AutomaticTask, event eventsource.Event) (bool, error) {
		entered <- event.ID
		select {
		case <-release:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}})
	first := eventsource.Event{ID: "review-1", Topic: "github:demo/repo:pull:42"}
	second := first
	second.ID = "review-2"
	go func() { _, _ = service.evaluateEventTopic(t.Context(), models.AutomaticTask{}, first) }()
	select {
	case id := <-entered:
		if id != first.ID {
			t.Fatalf("first evaluator received %q", id)
		}
	case <-t.Context().Done():
		t.Fatal("first evaluator did not start")
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := service.evaluateEventTopic(ctx, models.AutomaticTask{}, second)
		result <- err
	}()
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting same-topic evaluation did not respect cancellation: %v", err)
	}
	select {
	case id := <-entered:
		t.Fatalf("second evaluation entered while its topic remained held: %q", id)
	default:
	}
}

func TestEventFollowUpFencesRevisedPolicyAndRetainsFallbackRun(t *testing.T) {
	ctx := t.Context()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "event-follow-up.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	wakers := controllers.NewWakerController(db.DB)
	if err := wakers.Create(ctx, models.Waker{ID: "owner", Name: "Owner"}); err != nil {
		t.Fatal(err)
	}
	source := &fixtureEventSource{}
	boundary := &configurationAdmissionExecutions{states: map[string]chat.InitialExecutionState{}}
	calls := 0
	conf := Config{
		Tasks: controllers.NewAutomaticTaskController(db.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
		Events: controllers.NewAutomaticEventController(db.DB), EventAdapter: source, Wakers: wakers,
		Executions: boundary, Chats: configurationAdmissionChats{executions: boundary}, Now: func() time.Time { return time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC) },
		EventFollowUp: func(_ context.Context, _ models.AutomaticTask, observation models.AutomaticEventObservation) (EventFollowUpResult, error) {
			calls++
			if observation.Event.ID == "review:old" {
				return EventFollowUpResult{}, status_error.AutomaticTaskBusy
			}
			return EventFollowUpResult{ErrorCode: status_error.WorkTicketCannotContinue.Key()}, nil
		},
	}
	s := New(conf)
	s.ctx = ctx
	scope := RuleScope{WakerID: "owner", RuntimeID: constants.RuntimeIDCodex}
	sourceConfig := eventsource.Config{Source: eventsource.GitHubPullRequests, Repository: "demo/repo",
		Kinds: []string{eventsource.PullReviewed}, TrackDiscussion: true, Keyword: "original"}
	rule := persistLegacyEventRule(t, s, "existing-follow-up", sourceConfig)
	check := func() EventRuleView {
		t.Helper()
		view, err := s.CheckEventRule(ctx, scope.WakerID, rule.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	check()
	oldReview := eventsource.Event{ID: "review:old", Topic: "github:demo/repo:pull:42", Kind: eventsource.PullReviewed, Title: "original review",
		URL: "https://github.com/demo/repo/pull/42"}
	source.events = []eventsource.Event{oldReview}
	view := check()
	if len(view.Observations) != 1 || view.Observations[0].Disposition != "pending" || calls != 1 || len(boundary.intents) != 0 {
		t.Fatalf("active work did not retain the pending event: %+v calls=%d", view, calls)
	}
	sourceConfig.Keyword = "revised"
	if _, err := s.UpdateConversationRule(ctx, scope, rule.TaskID, ConversationRulePatch{Event: &sourceConfig}); err != nil {
		t.Fatal(err)
	}
	if err := s.drainEventRule(ctx, rule.TaskID); err != nil {
		t.Fatal(err)
	}
	view = check()
	if len(view.Observations) != 1 || view.Observations[0].Disposition != "superseded" || calls != 1 || len(boundary.intents) != 0 {
		t.Fatalf("superseded review reached the original or a new worker: %+v calls=%d", view, calls)
	}
	review := oldReview
	review.ID, review.Title = "review:new", "revised review"
	source.events = append(source.events, review)
	view = check()
	var matched models.AutomaticEventObservation
	for _, row := range view.Observations {
		if row.Event.ID == review.ID {
			matched = row
		}
	}
	if calls != 2 || len(boundary.intents) != 1 || matched.RunID == "" || matched.ErrorCode != status_error.WorkTicketCannotContinue.Key() {
		t.Fatalf("fallback was not retained with the actual routine run: %+v calls=%d intents=%d", matched, calls, len(boundary.intents))
	}
}
