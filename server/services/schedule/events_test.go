package schedule

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/eventsource"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

type fixtureEventSource struct {
	// events 是本次可重放的有界观察窗口。
	events []eventsource.Event
	// err 模拟来源检查失败。
	err error
}

type fixtureEventPush struct{}

func fixturePushSignature(body []byte) string {
	mac := hmac.New(sha256.New, []byte("fixture-secret"))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (fixtureEventPush) Verify(_ context.Context, _ eventsource.Config, body []byte, signature string) ([]eventsource.Event, error) {
	if !hmac.Equal([]byte(signature), []byte(fixturePushSignature(body))) {
		return nil, status_error.AgentContextUnauthorized
	}
	var events []eventsource.Event
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (f *fixtureEventSource) Observe(context.Context, eventsource.Config) ([]eventsource.Event, error) {
	return f.events, f.err
}

func persistLegacyEventRule(t *testing.T, s *Service, id string, config eventsource.Config) ConversationRuleReceipt {
	t.Helper()
	normalized, err := eventsource.Normalize(config)
	if err != nil {
		t.Fatal(err)
	}
	now := s.config.Now().UnixMilli()
	task := models.AutomaticTask{
		ID: id, WakerID: "owner", CreatedAt: now, UpdatedAt: now,
		Revision: 1, ConfigurationVersion: 1, TriggerVersion: 1,
		AutomaticTaskConfiguration: models.AutomaticTaskConfiguration{
			Name: "PR 观察", Instruction: "汇报变更", Enabled: true,
			Trigger: constants.AutomaticTaskTriggerEvent, Event: &normalized,
			TargetKind: constants.AutomaticTaskTargetKindWaker,
			RuntimeID:  constants.RuntimeIDCodex, AccountID: "system", PermissionLevel: constants.PermissionLevelDefault,
			ReportDestination: constants.AutomaticReportDestinationMainConversation, ReportPolicy: constants.AutomaticReportPolicyAlways,
		},
	}
	if err := s.config.Tasks.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	return ruleReceipt(task.ID)
}

func TestEventRuleBaselinesDispatchesAndRecordsRealChecks(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "events.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	wakers := controllers.NewWakerController(db.DB)
	if err := wakers.Create(t.Context(), models.Waker{ID: "owner", Name: "owner"}); err != nil {
		t.Fatal(err)
	}
	pr := func(id, title string) eventsource.Event {
		return eventsource.Event{ID: id, Topic: "github:demo/repo:pull:" + id, Kind: eventsource.PullOpened, Title: title, URL: "https://github.com/demo/repo/pull/" + id}
	}
	source := &fixtureEventSource{events: []eventsource.Event{pr("41", "review baseline")}}
	boundary := &configurationAdmissionExecutions{states: map[string]chat.InitialExecutionState{}}
	now := time.Date(2026, 11, 30, 8, 0, 0, 0, time.UTC)
	s := New(Config{
		Tasks: controllers.NewAutomaticTaskController(db.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
		Events: controllers.NewAutomaticEventController(db.DB), EventAdapter: source, EventPushAdapter: fixtureEventPush{}, Wakers: wakers,
		Executions: boundary, Chats: configurationAdmissionChats{executions: boundary}, Now: func() time.Time { return now },
	})
	s.ctx = t.Context()
	config := eventsource.Config{Source: eventsource.GitHubPullRequests, Repository: "demo/repo", Keyword: "review"}
	if _, err := s.SaveConfiguration(t.Context(), "owner", "", 0, models.AutomaticTaskConfiguration{
		Name: "New old-style event", Instruction: "Report", Trigger: constants.AutomaticTaskTriggerEvent, Event: &config,
	}, false); !util.HasStatusCode(err, status_error.BadRequest) {
		t.Fatalf("new standalone event was accepted: %v", err)
	}
	receipt := persistLegacyEventRule(t, s, "existing-event", config)
	check := func(want string, intents int) {
		t.Helper()
		now = now.Add(time.Second)
		view, err := s.CheckEventRule(t.Context(), "owner", receipt.TaskID)
		if err != nil || view.Cursor.Outcome != want || len(boundary.intents) != intents {
			t.Fatalf("check expected %s / %d: %+v intents %d error %v", want, intents, view, len(boundary.intents), err)
		}
	}
	check("baseline", 0)
	source.events = append(source.events, pr("42", "unrelated"), pr("43", "requires review"))
	check("matched", 1)
	check("empty", 1)
	source.events = append(source.events, pr("44", "requires review"))
	check("matched", 1)
	for id, state := range boundary.states {
		state.InitialExecution.Status = constants.ExecutionStatusCompleted
		state.Chat.ActiveExecutionID = ""
		boundary.states[id] = state
	}
	check("empty", 1)
	s.pollEventRules(t.Context())
	if len(boundary.intents) != 2 {
		t.Fatalf("background did not dispatch the now-unblocked observation: %d", len(boundary.intents))
	}
	source.err = status_error.WorkspaceGitHubRequestFailed
	check("failed", 2)
	result, err := s.Get(t.Context(), "owner", receipt.TaskID)
	if err != nil || result.EventState == nil || result.EventState.Cursor.Outcome != "failed" ||
		result.LastRun == nil || result.LastRun.SourceEvent == nil || result.LastRun.SourceEvent.ID != "44" {
		t.Fatalf("rule details lost observed failure or accepted event: %+v %v", result, err)
	}
	for id, state := range boundary.states {
		state.InitialExecution.Status = constants.ExecutionStatusCompleted
		state.Chat.ActiveExecutionID = ""
		boundary.states[id] = state
	}
	source.err = nil
	test, err := s.TestEventRule(t.Context(), "owner", receipt.TaskID, "explicit-test")
	if err != nil || test.Source != constants.AutomaticRunSourceEventTest ||
		test.EventObservationID != "" || test.SourceEvent == nil || test.SourceEvent.ID != "44" ||
		len(boundary.intents) != 3 {
		t.Fatalf("explicit test used wrong sample or dispatch: %+v %v", test, err)
	}
	replay, err := s.TestEventRule(t.Context(), "owner", receipt.TaskID, "explicit-test")
	if err != nil || test.ID != replay.ID || len(boundary.intents) != 3 {
		t.Fatalf("explicit test replay failed: %+v %v", replay, err)
	}
	if _, err := s.CheckEventRule(t.Context(), "other-owner", receipt.TaskID); !errors.Is(err, status_error.AutomaticTaskNotFound) {
		t.Fatalf("other owner check: %v", err)
	}
	body, err := json.Marshal([]eventsource.Event{pr("45", "review from push")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptPushedEvents(t.Context(), "owner", receipt.TaskID, body, "bad-signature"); !errors.Is(err, status_error.AgentContextUnauthorized) {
		t.Fatalf("invalid signed delivery was accepted: %v", err)
	}
	result, err = s.Get(t.Context(), "owner", receipt.TaskID)
	if err != nil || len(result.EventState.Observations) != 4 {
		t.Fatalf("unverified source changed durable observations: %+v %v", result, err)
	}
	for range 2 {
		if err := s.AcceptPushedEvents(t.Context(), "owner", receipt.TaskID, body, fixturePushSignature(body)); err != nil {
			t.Fatal(err)
		}
	}
	result, err = s.Get(t.Context(), "owner", receipt.TaskID)
	if err != nil || len(result.EventState.Observations) != 5 || result.EventState.Observations[0].Event.ID != "45" ||
		result.EventState.Observations[0].Disposition != "pending" || len(boundary.intents) != 3 {
		t.Fatalf("verified acknowledgment did not preserve one durable pending observation: %+v %v", result, err)
	}
}
