package schedule

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/chat"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestConversationReportsFreezeAndDeliverOneReceiptPerOpportunity(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "reports.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	wakers := controllers.NewWakerController(db.DB)
	for _, id := range []string{"owner", "other"} {
		if err := wakers.Create(t.Context(), models.Waker{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	boundary := &configurationAdmissionExecutions{states: map[string]chat.InitialExecutionState{}}
	now := time.Date(2026, 11, 30, 0, 59, 59, 0, time.UTC)
	published := map[string][]string{}
	failPublish := map[string]bool{}
	finalReads := 0
	conf := Config{
		Tasks: controllers.NewAutomaticTaskController(db.DB), Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)), Wakers: wakers,
		Executions: boundary, Chats: configurationAdmissionChats{executions: boundary}, Now: func() time.Time { return now },
		ReadReport: func(_ context.Context, _ models.AutomaticRun) (string, error) {
			finalReads++
			return ReportNoChanges, nil
		},
		PublishReport: func(_ context.Context, run models.AutomaticRun, body string) (string, error) {
			if failPublish[run.ID] {
				delete(failPublish, run.ID)
				return "", status_error.DatabaseBusy
			}
			published[run.ID] = append(published[run.ID], string(run.Configuration.ReportDestination), body)
			return "/automatic-tasks/" + run.TaskID, nil
		},
	}
	service := New(conf)
	service.ctx = t.Context()
	owner := RuleScope{WakerID: "owner", RuntimeID: constants.RuntimeIDCodex}
	other := RuleScope{WakerID: "other", RuntimeID: constants.RuntimeIDCodex}
	rule := models.AutomaticTaskSchedule{Kind: constants.AutomaticScheduleKindWeekly, Timezone: "Asia/Shanghai", Weekday: 1, Hour: 9}
	weekly, err := service.CreateConversationRule(t.Context(), owner, ConversationRulePatch{Name: ptr("业务周报"), Instruction: ptr("整理业务动态"), Schedule: &rule})
	if err != nil || weekly.TaskID == "" || weekly.Href != "/automatic-tasks/"+weekly.TaskID {
		t.Fatalf("create receipt = %+v, %v", weekly, err)
	}
	dailySchedule := rule
	dailySchedule.Kind = constants.AutomaticScheduleKindDaily
	daily, err := service.CreateConversationRule(t.Context(), owner, ConversationRulePatch{
		Name: ptr("变更日报"), Instruction: ptr("整理新的动态"), Schedule: &dailySchedule, ReportPolicy: ptr(constants.AutomaticReportPolicyChangesOnly),
	})
	if err != nil || daily.TaskID == weekly.TaskID {
		t.Fatalf("second rule = %+v, %v", daily, err)
	}
	if _, err := service.UpdateConversationRule(t.Context(), other, weekly.TaskID, ConversationRulePatch{Enabled: ptr(false)}); !errors.Is(err, status_error.AutomaticTaskNotFound) {
		t.Fatalf("other owner update: %v", err)
	}
	list, err := service.ListConversationRules(t.Context(), owner)
	if err != nil || len(list) != 2 {
		t.Fatalf("owned rules = %+v, %v", list, err)
	}
	now = now.Add(time.Second)
	for range 2 {
		if err := service.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(boundary.intents) != 2 || len(published) != 0 {
		t.Fatalf("opportunities = %d, published = %v", len(boundary.intents), published)
	}
	for _, intent := range boundary.intents {
		sent := intent.Input
		if sent.Content != "整理业务动态" && sent.Content != "整理新的动态" {
			t.Fatalf("displayed task input contains host instructions: %q", sent.Content)
		}
		if len(sent.ContextInstructions) != 1 || !strings.Contains(sent.ContextInstructions[0].Content, constants.ConversationMessagePostOperation) || strings.Contains(sent.ContextInstructions[0].Content, ReportNoChanges) {
			t.Fatalf("automatic reporting guidance did not reach the model separately: %+v", sent.ContextInstructions)
		}
	}
	if _, err := service.UpdateConversationRule(t.Context(), owner, weekly.TaskID, ConversationRulePatch{ReportDestination: ptr(constants.AutomaticReportDestinationTopic)}); err != nil {
		t.Fatal(err)
	}
	for id, state := range boundary.states {
		state.InitialExecution.Status = constants.ExecutionStatusCompleted
		state.Chat.ActiveExecutionID = ""
		boundary.states[id] = state
	}
	restarted := New(conf)
	restarted.ctx = t.Context()
	for range 2 {
		if err := restarted.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	first, err := restarted.GetConversationRule(t.Context(), owner, weekly.TaskID)
	if err != nil || len(first.Runs) != 1 || first.Runs[0].ReportState != constants.AutomaticReportStateUnavailable || first.Runs[0].ReportHref != "" || len(published) != 0 {
		t.Fatalf("frozen weekly receipt = %+v, published = %+v, %v", first, published, err)
	}
	second, err := restarted.GetConversationRule(t.Context(), owner, daily.TaskID)
	if err != nil || len(second.Runs) != 1 || second.Runs[0].ReportState != "quiet" || len(published) != 0 {
		t.Fatalf("quiet daily receipt = %+v, published = %+v, %v", second, published, err)
	}
	manual := func(key string) RunView {
		t.Helper()
		task, err := conf.Tasks.Get(t.Context(), weekly.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		view, err := restarted.Admit(t.Context(), AdmissionCommand{
			Task: task, Source: constants.AutomaticRunSourceManual, OccurrenceKey: "manual:" + key, RequestID: key, NextRunAt: task.NextRunAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	firstManual, queued := manual("first"), manual("second")
	if queued.ID == firstManual.ID || queued.State != constants.AutomaticRunStatePending ||
		!queued.AwaitingDispatch || len(boundary.intents) != 3 || manual("second").ID != queued.ID {
		t.Fatalf("manual opportunities did not retain unique submission identities: first=%+v second=%+v", firstManual, queued)
	}
	setCompleted := func(id string) {
		state := boundary.states[id]
		state.InitialExecution.Status = constants.ExecutionStatusCompleted
		state.Chat.ActiveExecutionID = ""
		boundary.states[id] = state
	}
	post := func(run RunView, stalled bool) {
		t.Helper()
		state := boundary.states[run.ChatID]
		if foreign, err := restarted.AgentReportBinding(t.Context(), state.Chat, "different-execution", run.ID); err != nil || foreign.Call != nil {
			t.Fatalf("unrelated execution received a report grant: %+v, %v", foreign, err)
		}
		binding, err := restarted.AgentReportBinding(t.Context(), state.Chat, run.ExecutionID, run.ID)
		if err != nil || binding.Call == nil {
			t.Fatalf("report grant = %+v, %v", binding, err)
		}
		req := capability.Request{RequestID: "one-report", Params: map[string]any{"content": "真实报告"}}
		first, err := binding.Call(t.Context(), req)
		if stalled {
			if !errors.Is(err, status_error.DatabaseBusy) {
				t.Fatalf("failed publish lost its error: %v", err)
			}
			if _, err := binding.Receipt(t.Context(), req.RequestID); !errors.Is(err, status_error.AgentReceiptNotFound) {
				t.Fatalf("unconfirmed offer claimed it was delivered: %v", err)
			}
		} else {
			if err != nil || first.CommandID == "" {
				t.Fatalf("agent report = %+v, %v", first, err)
			}
			saved, err := binding.Receipt(t.Context(), req.RequestID)
			if err != nil || saved.CommandID != first.CommandID || !saved.Replayed {
				t.Fatalf("agent report receipt = %+v, %v", saved, err)
			}
			if again, err := binding.Call(t.Context(), req); err != nil || again.CommandID != first.CommandID || !again.Replayed {
				t.Fatalf("retry created a second delivery: %+v, %v", again, err)
			}
		}
		req.Params["content"] = "不同正文"
		if _, err := binding.Call(t.Context(), req); !errors.Is(err, status_error.AgentCommandRequestConflict) {
			t.Fatalf("changed post was not rejected: %v", err)
		}
	}
	post(firstManual, false)
	setCompleted(firstManual.ChatID)
	for range 2 {
		if err := restarted.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	secondManual, err := conf.Tasks.GetRun(t.Context(), queued.ID)
	if err != nil || secondManual.AwaitingDispatch || secondManual.ChatID == "" || len(boundary.intents) != 4 {
		t.Fatalf("queued opportunity was not readmitted exactly once: %+v %v", secondManual, err)
	}
	failPublish[secondManual.ID] = true
	post(RunView{AutomaticRun: secondManual}, true)
	setCompleted(secondManual.ChatID)
	restarted = New(conf)
	restarted.ctx = t.Context()
	if err := restarted.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{firstManual.ID, secondManual.ID} {
		if values := published[id]; len(values) != 2 || values[0] != string(constants.AutomaticReportDestinationTopic) || values[1] != "真实报告" {
			t.Fatalf("manual report did not use updated destination: %+v", values)
		}
	}
	if finalReads != 0 {
		t.Fatalf("explicit report fell back to reading chat prose %d times", finalReads)
	}
	failed := manual("failure")
	state := boundary.states[failed.ChatID]
	state.InitialExecution.Status = constants.ExecutionStatusFailed
	state.Chat.ActiveExecutionID = ""
	boundary.states[failed.ChatID] = state
	if err := restarted.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if values := published[failed.ID]; len(values) != 2 || !strings.Contains(values[1], "执行失败") || finalReads != 0 {
		t.Fatalf("failed run did not report its actual terminal state: %+v", values)
	}
	bindings := restarted.ConversationRuleBindings(controllers.NewAgentCommandController(db.DB), "chat:execution", func(context.Context) (RuleScope, error) { return owner, nil })
	operations := map[string]capability.Binding{}
	for _, binding := range bindings {
		operations[binding.Operation] = binding
	}
	update := capability.Request{Operation: capability.RuleUpdate, RequestID: "pause-rule", Params: map[string]any{"taskId": weekly.TaskID, "enabled": false}}
	paused, err := operations[update.Operation].Call(t.Context(), update)
	if err != nil || paused.CommandID == "" {
		t.Fatalf("pause did not commit a command receipt: %+v %v", paused, err)
	}
	replay, err := operations[update.Operation].Call(t.Context(), update)
	if err != nil || !replay.Replayed || replay.CommandID != paused.CommandID {
		t.Fatalf("replayed pause: %+v %v", replay, err)
	}
	task, err := restarted.Get(t.Context(), owner.WakerID, weekly.TaskID)
	if err != nil || task.Enabled {
		t.Fatalf("management read disagreed with conversation pause: %+v %v", task, err)
	}
	update.RequestID, update.Params["enabled"] = "resume-rule", true
	if _, err := operations[update.Operation].Call(t.Context(), update); err != nil {
		t.Fatal(err)
	}
	task, err = restarted.Get(t.Context(), owner.WakerID, weekly.TaskID)
	if err != nil || !task.Enabled || task.NextRunAt == nil {
		t.Fatalf("management read disagreed with conversation resume: %+v %v", task, err)
	}
}

func ptr[T any](value T) *T { return &value }
