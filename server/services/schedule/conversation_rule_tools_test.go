package schedule

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent/capability"
	workflowstate "github.com/JieWaZi/NeuxBot/server/internal/workflow/state"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/workflow"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestTimedRuleCommandsRetainCommittedScheduleAcrossEditsAndReplays(t *testing.T) {
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "rules.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := t.Context()
	wakers := controllers.NewWakerController(db.DB)
	if err := wakers.Create(ctx, models.Waker{ID: "owner", Name: "Planner"}); err != nil {
		t.Fatal(err)
	}
	tasks := controllers.NewAutomaticTaskController(db.DB)
	service := New(Config{
		Tasks: tasks, Transactions: controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB)),
		Wakers: wakers, Executions: &configurationAdmissionExecutions{},
		Now: func() time.Time { return time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC) },
	})
	bindings := service.ConversationRuleBindings(controllers.NewAgentCommandController(db.DB), "origin", func(context.Context) (RuleScope, error) {
		return RuleScope{WakerID: "owner", RuntimeID: constants.RuntimeIDCodex, Timezone: "Asia/Shanghai"}, nil
	})
	call := func(operation, id string, params map[string]any) capability.Result {
		t.Helper()
		for _, binding := range bindings {
			if binding.Operation == operation {
				result, err := binding.Call(ctx, capability.Request{Operation: operation, RequestID: id, Params: params})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
		}
		t.Fatalf("missing binding %s", operation)
		return capability.Result{}
	}
	timeFor := func(day int) int64 {
		return time.Date(2026, 11, day, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60)).UnixMilli()
	}
	create := map[string]any{"name": "Reminder", "instruction": "Review the report", "schedule": map[string]any{"kind": "once", "onceAt": "2026-11-28T09:00"}}
	created := call(capability.RuleCreate, "create", create)
	taskID := created.Data.(map[string]any)["taskId"].(string)
	update := map[string]any{"taskId": taskID, "schedule": map[string]any{"kind": "once", "onceAt": "2026-11-29T09:00"}}
	edited := call(capability.RuleUpdate, "update", update)
	replayed := call(capability.RuleCreate, "create", create)

	assertSnapshot := func(result capability.Result, want int64, updated bool) {
		t.Helper()
		card, ok := result.Data.(map[string]any)["automaticTaskReceipt"].(map[string]any)
		if !ok || card["taskId"] != taskID || card["name"] != "Reminder" || card["wakerName"] != "Planner" ||
			card["instructionPreview"] != "Review the report" || card["nextRunAt"] != float64(want) ||
			(card["updated"] == true) != updated {
			t.Fatalf("committed rule snapshot = %+v, want task %s at %d (updated=%v)", result, taskID, want, updated)
		}
	}
	assertSnapshot(created, timeFor(28), false)
	assertSnapshot(edited, timeFor(29), true)
	assertSnapshot(replayed, timeFor(28), false)
	if !replayed.Replayed || replayed.CommandID != created.CommandID || edited.CommandID == created.CommandID {
		t.Fatalf("receipt identities: created=%+v edited=%+v replayed=%+v", created, edited, replayed)
	}
	task, err := tasks.Get(ctx, taskID)
	if err != nil || task.ConfigurationVersion != 2 || task.NextRunAt == nil || *task.NextRunAt != timeFor(29) {
		t.Fatalf("stored rule = %+v, err = %v", task, err)
	}
}

func TestConversationSchedulesOwnedWorkflowWithOneTaskCard(t *testing.T) {
	ctx := t.Context()
	db, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "workflow-rules.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	tx := controllers.NewTransactionController(dbhelper.NewGormTransactionManager(db.DB))
	tasks := controllers.NewAutomaticTaskController(db.DB)
	store := controllers.NewWorkflowController(db.DB)
	runs := controllers.NewWorkflowRunController(db.DB)
	workflows := workflow.New(workflow.Config{Store: store, Runs: runs, State: workflowstate.New(store, runs, tx), Transactions: tx, DataDirectory: t.TempDir()})
	const source = `export const meta={sdk:'bot.workflow',title:'Inventory check',phases:[{id:'watch',title:'Watch',detail:'Check for changes',canExit:true}]}; return '[NEUX_NO_CHANGES]';`
	createWorkflow := func(owner string) (string, string) {
		t.Helper()
		draft, err := workflows.CreateDefinition(ctx, workflow.CreateDefinitionCommand{RequestID: owner, CreationWakerID: owner})
		if err != nil {
			t.Fatal(err)
		}
		saved, _, err := workflows.SaveSource(ctx, workflow.SaveSourceCommand{DefinitionID: draft.ID, RequestID: "save", Source: source, SDK: constants.WorkflowSDKIdentifier})
		if err != nil {
			t.Fatal(err)
		}
		return saved.DefinitionID, saved.RevisionID
	}
	id, revision := createWorkflow("owner")
	foreignID, foreignRevision := createWorkflow("other")
	wakers := controllers.NewWakerController(db.DB)
	if err := wakers.Create(ctx, models.Waker{ID: "owner", Name: "Planner"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 27, 1, 59, 59, 0, time.UTC)
	service := New(Config{Tasks: tasks, Transactions: tx, Wakers: wakers, Workflows: workflows, WorkflowRuns: runs, Now: func() time.Time { return now }})
	service.ctx = ctx
	owner := RuleScope{WakerID: "owner", Timezone: "Asia/Shanghai", PermissionLevel: constants.PermissionLevelAuto, WorkspaceReadOnly: true}
	bindings := service.ConversationRuleBindings(controllers.NewAgentCommandController(db.DB), "origin", func(context.Context) (RuleScope, error) { return owner, nil })
	call := func(requestID, flow, version string) (capability.Result, error) {
		params := map[string]any{
			"name": "Monitor inventory", "schedule": map[string]any{"kind": "daily", "hour": 10, "minute": 0},
			"reportPolicy": "changes_only", "workflow": map[string]any{"id": flow, "revisionId": version},
		}
		op, _ := capability.Find(capability.RuleCreate)
		if err := op.Validate(params); err != nil {
			return capability.Result{}, err
		}
		for _, binding := range bindings {
			if binding.Operation == op.ID {
				return binding.Call(ctx, capability.Request{Operation: op.ID, RequestID: requestID, Params: params})
			}
		}
		t.Fatal("missing create binding")
		return capability.Result{}, nil
	}
	if _, err := call("foreign", foreignID, foreignRevision); !errors.Is(err, status_error.AgentOperationForbidden) {
		t.Fatalf("foreign target: %v", err)
	}
	created, err := call("schedule", id, revision)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := call("schedule", id, revision)
	taskID := created.Data.(map[string]any)["taskId"].(string)
	if err != nil || !replay.Replayed || replay.CommandID != created.CommandID || replay.Data.(map[string]any)["taskId"] != taskID {
		t.Fatalf("second task or missing receipt: %+v %v", replay, err)
	}
	card := created.Data.(map[string]any)["automaticTaskReceipt"].(map[string]any)
	if card["workflowName"] != "Inventory check" {
		t.Fatalf("task card identifies the wrong target: %+v", card)
	}
	task, err := tasks.Get(ctx, taskID)
	if err != nil || task.WorkflowRevisionID != revision || task.Trigger != constants.AutomaticTaskTriggerSchedule ||
		task.ReportPolicy != constants.AutomaticReportPolicyChangesOnly || task.WorkflowReportRecipient == nil || task.WorkflowReportRecipient.SubjectID != owner.WakerID || task.NextRunAt == nil {
		t.Fatalf("schedule did not freeze its workflow and report: %+v %v", task, err)
	}
	list, err := service.ListConversationRules(ctx, owner)
	if err != nil || len(list) != 1 || list[0].TaskID != taskID {
		t.Fatalf("conversation cannot find the same rule: %+v %v", list, err)
	}
	now = now.Add(time.Second)
	for range 2 {
		if err := service.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	automaticRuns, _, err := tasks.ListRuns(ctx, taskID, 5, 0)
	if err != nil || len(automaticRuns) != 1 || automaticRuns[0].WorkflowRunID == "" {
		t.Fatalf("scheduled opportunity did not start exactly one workflow: %+v %v", automaticRuns, err)
	}
	workflowRun, err := runs.GetRun(ctx, automaticRuns[0].WorkflowRunID)
	if err != nil || !workflowRun.WorkerPermissionsLimited || workflowRun.RevisionID != revision || workflowRun.ConclusionPolicy != constants.AutomaticReportPolicyChangesOnly ||
		workflowRun.ConclusionRecipient == nil || *workflowRun.ConclusionRecipient != *task.WorkflowReportRecipient ||
		workflowRun.Defaults.PermissionLevel != owner.PermissionLevel || !workflowRun.Defaults.WorkspaceReadOnly {
		t.Fatalf("workflow received the wrong frozen configuration: %+v %v", workflowRun, err)
	}
	if _, err := service.UpdateConversationRule(ctx, RuleScope{WakerID: "other"}, taskID, ConversationRulePatch{Enabled: ptr(false)}); !errors.Is(err, status_error.AutomaticTaskNotFound) {
		t.Fatalf("another conversation changed the rule: %v", err)
	}
	channels := controllers.NewChannelController(db.DB)
	service.config.Channels = channels
	if err := channels.Create(ctx, models.Channel{ID: "team", Name: "Release team", LeadWakerID: owner.WakerID},
		[]models.ChannelMember{{ChannelID: "team", WakerID: owner.WakerID}}); err != nil {
		t.Fatal(err)
	}
	team := owner
	team.ChannelID = "team"
	teamRule, err := service.CreateConversationRule(ctx, team, ConversationRulePatch{
		Name: ptr("Team inventory check"), Schedule: &task.Schedule, Workflow: &ConversationRuleWorkflow{ID: id, RevisionID: revision},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := channels.UpdateLead(ctx, team.ChannelID, "other", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListConversationRules(ctx, team); !errors.Is(err, status_error.AgentContextUnauthorized) {
		t.Fatalf("former lead still manages the team plan: %v", err)
	}
	team.WakerID = "other"
	list, err = service.ListConversationRules(ctx, team)
	if err != nil || len(list) != 1 || list[0].TaskID != teamRule.TaskID {
		t.Fatalf("new lead lost the team's scheduled workflow: %+v %v", list, err)
	}
	if err := store.DeleteDefinition(ctx, id, revision); err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		if binding.Operation == capability.RuleUpdate {
			if _, err := binding.Call(ctx, capability.Request{Operation: capability.RuleUpdate, RequestID: "pause",
				Params: map[string]any{"taskId": taskID, "enabled": false}}); err != nil {
				t.Fatalf("workflow removal prevented pausing its task: %v", err)
			}
		}
	}
	paused, err := tasks.Get(ctx, taskID)
	if err != nil || paused.Enabled || paused.NextRunAt != nil {
		t.Fatalf("pause did not settle the same task: %+v %v", paused, err)
	}
}
